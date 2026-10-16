package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testResumeJobID = "123e4567-e89b-12d3-a456-426614174000"
const testInsightID = "223e4567-e89b-12d3-a456-426614174000"

func TestCreateInsightRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"invalid resume ID", `{"resume_job_id":"bad","consent":true}`},
		{"missing consent", `{"resume_job_id":"` + testResumeJobID + `"}`},
		{"consent declined", `{"resume_job_id":"` + testResumeJobID + `","consent":false}`},
		{"unknown field", `{"resume_job_id":"` + testResumeJobID + `","consent":true,"extra":1}`},
		{"multiple JSON values", `{"resume_job_id":"` + testResumeJobID + `","consent":true}{}`},
	}
	handler := newRoleInsightsHandler(nil, "", "", nil)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/insights", strings.NewReader(test.body))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected HTTP 400; got %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestInsightEndpointsRejectUnsupportedMethodsAndIDs(t *testing.T) {
	handler := newRoleInsightsHandler(nil, "", "", nil)
	request := httptest.NewRequest(http.MethodGet, "/insights", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected HTTP 405; got %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/insights/not-a-uuid", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 for invalid insight ID; got %d", response.Code)
	}
}

func TestNewUUIDReturnsRFC4122V4(t *testing.T) {
	id, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	if !idPattern.MatchString(id) || id[14] != '4' {
		t.Fatalf("expected UUID v4; got %q", id)
	}
}
