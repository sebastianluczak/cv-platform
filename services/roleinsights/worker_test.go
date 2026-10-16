package main

import (
	"strings"
	"testing"
	"time"
)

func TestValidInsightJobRequiresUUIDs(t *testing.T) {
	if !validInsightJob(insightJob{InsightID: testInsightID, ResumeJobID: testResumeJobID}) {
		t.Fatal("expected UUID references to be valid")
	}
	for _, job := range []insightJob{
		{InsightID: "bad", ResumeJobID: testResumeJobID},
		{InsightID: testInsightID, ResumeJobID: "bad"},
		{},
	} {
		if validInsightJob(job) {
			t.Errorf("expected invalid job: %+v", job)
		}
	}
}

func TestSafeLogFieldRemovesLineBreaks(t *testing.T) {
	if got := safeLogField("first\r\nsecond"); got != "first  second" {
		t.Fatalf("safeLogField() = %q, want CR/LF removed", got)
	}
}

func TestValidateInsightArtifactRequiresConsentAndValidRoles(t *testing.T) {
	now := time.Now().UTC()
	artifact := insightArtifact{
		Version: 1, InsightID: testInsightID, ResumeJobID: testResumeJobID,
		Status: "completed", ConsentGiven: true, ConsentedAt: now,
		CreatedAt: now, UpdatedAt: now,
		Roles: []roleSuggestion{{
			Title: "Cook", FitSummary: "Resume mentions kitchen work.",
			Responsibilities: []string{"Prepare meals", "Maintain food safety"},
		}},
	}
	if err := validateInsightArtifact(artifact, testInsightID); err != nil {
		t.Fatal(err)
	}

	artifact.ConsentGiven = false
	if err := validateInsightArtifact(artifact, testInsightID); err == nil {
		t.Fatal("expected artifact without recorded consent to be rejected")
	}
	artifact.ConsentGiven = true
	artifact.Status = "queued"
	if err := validateInsightArtifact(artifact, testInsightID); err == nil {
		t.Fatal("expected queued artifact with suggestions to be rejected")
	}
	artifact.Status = "completed"
	artifact.Roles[0].Responsibilities = nil
	if err := validateInsightArtifact(artifact, testInsightID); err == nil {
		t.Fatal("expected role without responsibilities to be rejected")
	}
	artifact.Roles[0].Responsibilities = []string{"Prepare meals"}
	artifact.Roles = append(artifact.Roles, roleSuggestion{
		Title: "cook", FitSummary: "Duplicate role", Responsibilities: []string{"Cook"},
	})
	if err := validateInsightArtifact(artifact, testInsightID); err == nil {
		t.Fatal("expected duplicate titles to be rejected")
	}
}

func TestValidateRoleSuggestionsBoundsCountAndText(t *testing.T) {
	if err := validateRoleSuggestions([]roleSuggestion{{
		Title: "Cook", FitSummary: "Kitchen experience", Responsibilities: []string{"Prepare meals"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := validateRoleSuggestions(nil); err == nil {
		t.Fatal("expected empty role list to be rejected")
	}
	if err := validateRoleSuggestions([]roleSuggestion{{
		Title: strings.Repeat("x", maxRoleTextSize+1), FitSummary: "Fit",
		Responsibilities: []string{"Work"},
	}}); err == nil {
		t.Fatal("expected oversized title to be rejected")
	}
	roles := make([]roleSuggestion, maxRoleCount+1)
	for index := range roles {
		roles[index] = roleSuggestion{Title: string(rune('A' + index)), FitSummary: "Fit", Responsibilities: []string{"Work"}}
	}
	if err := validateRoleSuggestions(roles); err == nil {
		t.Fatal("expected too many roles to be rejected")
	}
}
