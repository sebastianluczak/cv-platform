package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const testResumeID = "123e4567-e89b-12d3-a456-426614174000"
const testMatchID = "223e4567-e89b-12d3-a456-426614174000"

func TestCreateMatchRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"invalid resume ID", `{"resume_job_id":"bad","offer_text":"Go"}`},
		{"missing offer", `{"resume_job_id":"` + testResumeID + `"}`},
		{"both offer sources", `{"resume_job_id":"` + testResumeID + `","offer_text":"Go","offer_url":"https://example.com/job"}`},
		{"unsafe URL", `{"resume_job_id":"` + testResumeID + `","offer_url":"http://127.0.0.1/job"}`},
		{"unknown field", `{"resume_job_id":"` + testResumeID + `","offer_text":"Go","unknown":true}`},
	}
	handler := newOfferMatcherHandler(nil, "", "", nil)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/matches", strings.NewReader(test.body))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected HTTP 400; got %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestMatchEndpointsRejectUnsupportedMethodsAndIDs(t *testing.T) {
	handler := newOfferMatcherHandler(nil, "", "", nil)
	request := httptest.NewRequest(http.MethodGet, "/matches", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected HTTP 405 for unsupported POST route method; got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/matches/not-a-uuid", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 for invalid match ID; got %d", response.Code)
	}
}

func TestDecodeCandidateSkillsValidatesIdentityAndCollectsEvidence(t *testing.T) {
	content := []byte(`{
		"version":1,
		"job_id":"` + testResumeID + `",
		"scored_at":"2026-10-15T09:00:00Z",
		"categories":[
			{"category":"backend","score":3,"evidence":["go","postgresql"]},
			{"category":"devops","score":1,"evidence":[]}
		]
	}`)
	skills, err := decodeCandidateSkills(content, testResumeID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(skills, []string{"go", "postgresql"}) {
		t.Fatalf("unexpected candidate evidence: %v", skills)
	}

	if _, err := decodeCandidateSkills(content, testMatchID); err == nil {
		t.Fatal("expected mismatched resume ID to be rejected")
	}
	if _, err := decodeCandidateSkills(bytes.Repeat([]byte("x"), maxStoredMatchSize+1), testResumeID); err == nil {
		t.Fatal("expected oversized candidate result to be rejected")
	}
}

func TestValidOfferJobRequiresOneBoundedSourceAndUUIDs(t *testing.T) {
	valid := offerJob{MatchID: testMatchID, ResumeJobID: testResumeID, OfferText: "Go"}
	if !validOfferJob(valid) {
		t.Fatal("expected valid offer text job")
	}
	for _, job := range []offerJob{
		{MatchID: testMatchID, ResumeJobID: testResumeID, OfferText: "Go", OfferURL: "https://example.com/job"},
		{MatchID: "bad", ResumeJobID: testResumeID, OfferText: "Go"},
		{MatchID: testMatchID, ResumeJobID: testResumeID, OfferURL: "http://10.0.0.1/job"},
		{MatchID: testMatchID, ResumeJobID: testResumeID, OfferText: strings.Repeat("x", maxOfferTextBytes+1)},
	} {
		if validOfferJob(job) {
			t.Errorf("expected invalid offer job: %+v", job)
		}
	}
}

func TestNewUUIDReturnsRFC4122V4(t *testing.T) {
	id, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	if !matchIDPattern.MatchString(id) || id[14] != '4' {
		t.Fatalf("expected UUID v4; got %q", id)
	}
}

func TestPublicMatchArtifactDoesNotExposeRetainedOfferText(t *testing.T) {
	artifact := matchArtifact{
		Version: 1, MatchID: testMatchID, ResumeJobID: testResumeID,
		Status: "completed", OfferText: "private job description",
	}
	content, err := json.Marshal(publicMatchArtifact(artifact))
	if err != nil {
		t.Fatal(err)
	}
	storedContent, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(storedContent), `"offer_text":"private job description"`) {
		t.Fatalf("stored offer match artifact did not retain offer text: %s", storedContent)
	}
	if strings.Contains(string(content), "offer_text") || strings.Contains(string(content), "private job description") {
		t.Fatalf("public offer match response exposed private offer text: %s", content)
	}
}
