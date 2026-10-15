package skillpatterns

import (
	"reflect"
	"testing"
	"time"
)

func TestScoreTextPreservesExistingEvidenceAndScores(t *testing.T) {
	result := ScoreText("job-1", "Backend engineer using Go, PostgreSQL, Docker, Kubernetes and Terraform.", time.Unix(1, 0))
	if len(result.Categories) != 14 {
		t.Fatalf("expected 14 categories; got %d", len(result.Categories))
	}
	if result.Categories[0].Category != "backend" || result.Categories[0].Score != 3 {
		t.Fatalf("unexpected backend score: %+v", result.Categories[0])
	}
	if result.Categories[1].Category != "devops" || result.Categories[1].Score != 2 {
		t.Fatalf("unexpected DevOps score: %+v", result.Categories[1])
	}
	if !reflect.DeepEqual(result.Categories[0].Evidence, []string{"backend", "go", "postgresql", "backend engineer"}) {
		t.Fatalf("unexpected backend evidence: %v", result.Categories[0].Evidence)
	}
}

func TestPatternsInTextReturnsDistinctKnownSignals(t *testing.T) {
	patterns := PatternsInText("Worked with SQL and PostgreSQL; also SQL.")
	counts := make(map[string]int)
	for _, pattern := range patterns {
		counts[pattern]++
	}
	if counts["sql"] != 1 || counts["postgresql"] != 1 {
		t.Fatalf("expected distinct matching skill patterns; got %v", patterns)
	}
	if MatchesPattern("go", "governance and golang") {
		t.Fatal("short pattern matched inside unrelated words")
	}
}
