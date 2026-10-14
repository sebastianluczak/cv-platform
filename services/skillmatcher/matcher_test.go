package main

import (
	"reflect"
	"testing"
	"time"
)

func TestScoreTextFindsRoleSignalsAndKeepsCategoryOrder(t *testing.T) {
	result := scoreText("job-1", "Backend engineer using Go, PostgreSQL, Docker, Kubernetes and Terraform.", time.Unix(1, 0))
	if len(result.Categories) != 8 {
		t.Fatalf("expected 8 categories; got %d", len(result.Categories))
	}
	if result.Categories[0].Category != "backend" || result.Categories[0].Score != 2 {
		t.Fatalf("unexpected backend score: %+v", result.Categories[0])
	}
	if result.Categories[1].Category != "devops" || result.Categories[1].Score != 2 {
		t.Fatalf("unexpected DevOps score: %+v", result.Categories[1])
	}
	if result.Categories[2].Category != "management" || result.Categories[2].Score != 1 {
		t.Fatalf("unexpected management score: %+v", result.Categories[2])
	}
	if !reflect.DeepEqual(result.Categories[0].Evidence, []string{"backend", "go", "postgresql"}) {
		t.Fatalf("unexpected backend evidence: %v", result.Categories[0].Evidence)
	}
}

func TestPatternMatchingUsesWholeTerms(t *testing.T) {
	if patternMatches("go", "governance and golang") {
		t.Fatal("short pattern matched inside unrelated words")
	}
	if !patternMatches("go", "Worked with Go and services") {
		t.Fatal("expected case-insensitive whole-word match")
	}
	if !patternMatches("ci/cd", "Owned CI/CD pipelines") {
		t.Fatal("expected punctuation-bearing phrase to match")
	}
}

func TestScoreEvidenceCount(t *testing.T) {
	tests := []struct {
		count int
		score int
	}{
		{count: 0, score: 1},
		{count: 1, score: 1},
		{count: 2, score: 2},
		{count: 4, score: 3},
		{count: 6, score: 4},
		{count: 8, score: 5},
		{count: 20, score: 5},
	}
	for _, test := range tests {
		if got := scoreEvidenceCount(test.count); got != test.score {
			t.Errorf("scoreEvidenceCount(%d) = %d; want %d", test.count, got, test.score)
		}
	}
}
