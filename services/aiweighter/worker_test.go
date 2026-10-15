package main

import (
	"strings"
	"testing"
	"time"
)

func TestValidWeighJobRequiresUUIDs(t *testing.T) {
	if !validWeighJob(weighJob{WeighID: testWeighID, MatchID: testMatchID}) {
		t.Fatal("expected UUID references to be valid")
	}
	for _, job := range []weighJob{
		{WeighID: "bad", MatchID: testMatchID},
		{WeighID: testWeighID, MatchID: "bad"},
		{},
	} {
		if validWeighJob(job) {
			t.Errorf("expected invalid job: %+v", job)
		}
	}
}

func TestValidateWeighArtifactRequiresMarkdownForCompletedOnly(t *testing.T) {
	now := time.Now().UTC()
	artifact := weighArtifact{
		Version: 1, WeighID: testWeighID, MatchID: testMatchID, ResumeJobID: "123e4567-e89b-12d3-a456-426614174000",
		Status: "completed", ConsentGiven: true, ConsentedAt: now,
		CreatedAt: now, UpdatedAt: now, Markdown: "# Resume",
	}
	if err := validateWeighArtifact(artifact, testWeighID); err != nil {
		t.Fatal(err)
	}
	artifact.Markdown = ""
	if err := validateWeighArtifact(artifact, testWeighID); err == nil {
		t.Fatal("expected completed artifact without Markdown to be rejected")
	}
	artifact.Status = "queued"
	artifact.Markdown = "# Resume"
	if err := validateWeighArtifact(artifact, testWeighID); err == nil {
		t.Fatal("expected queued artifact with Markdown to be rejected")
	}
	artifact.Status = "completed"
	artifact.Markdown = strings.Repeat("x", maxMarkdownSize+1)
	if err := validateWeighArtifact(artifact, testWeighID); err == nil {
		t.Fatal("expected oversized Markdown to be rejected")
	}
	artifact.Markdown = "# Resume"
	artifact.ConsentGiven = false
	if err := validateWeighArtifact(artifact, testWeighID); err == nil {
		t.Fatal("expected artifact without recorded consent to be rejected")
	}
}
