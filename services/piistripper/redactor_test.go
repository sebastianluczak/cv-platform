package main

import (
	"strings"
	"testing"
)

func TestRedactTextReplacesCommonIdentifiers(t *testing.T) {
	input := "Contact Jane Doe at jane.doe@example.com or +1 (415) 555-2671. She lives at 10 Main Street. Portfolio: https://example.com/jane"
	got, err := redactText(input)
	if err != nil {
		t.Fatalf("redactText() error = %v", err)
	}

	for _, identifier := range []string{"Jane Doe", "jane.doe@example.com", "+1 (415) 555-2671", "10 Main Street", "https://example.com/jane"} {
		if strings.Contains(got, identifier) {
			t.Errorf("redacted output still contains %q: %s", identifier, got)
		}
	}
	for _, placeholder := range []string{"[PERSON]", "[EMAIL]", "[PHONE]", "[ADDRESS]", "[URL]"} {
		if !strings.Contains(got, placeholder) {
			t.Errorf("redacted output does not contain %q: %s", placeholder, got)
		}
	}
}

func TestRedactTextPreservesUnmatchedTextAndUnicode(t *testing.T) {
	input := "Experienced Go engineer — built reliable systems."
	got, err := redactText(input)
	if err != nil {
		t.Fatalf("redactText() error = %v", err)
	}
	if got != input {
		t.Fatalf("redactText() = %q, want %q", got, input)
	}
}

func TestRedactTextHandlesMultilineAndOverlappingMatches(t *testing.T) {
	input := "Email: jane@example.com\nCall: 415-555-2671\n"
	got, err := redactText(input)
	if err != nil {
		t.Fatalf("redactText() error = %v", err)
	}
	want := "Email: [EMAIL]\nCall: [PHONE]\n"
	if got != want {
		t.Fatalf("redactText() = %q, want %q", got, want)
	}
}

func TestEntityPlaceholder(t *testing.T) {
	for input, want := range map[string]string{
		"PERSON": "PERSON",
		"ORG":    "ORGANIZATION",
		"GPE":    "",
		"LOC":    "",
		"FAC":    "",
		"DATE":   "",
	} {
		if got := entityPlaceholder(input); got != want {
			t.Errorf("entityPlaceholder(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidJobRequiresExpectedObjectReference(t *testing.T) {
	const bucket = "cv-platform-uploads"
	job := redactionJob{
		ID:        "e02c7e0c-71cb-4eeb-9ed7-0f7206bf6f62",
		Bucket:    bucket,
		ObjectKey: "results/e02c7e0c-71cb-4eeb-9ed7-0f7206bf6f62.txt",
	}
	if !validJob(job, bucket) {
		t.Fatal("expected valid redaction job")
	}

	for name, invalid := range map[string]redactionJob{
		"wrong bucket":  {ID: job.ID, Bucket: "other", ObjectKey: job.ObjectKey},
		"arbitrary key": {ID: job.ID, Bucket: bucket, ObjectKey: "uploads/" + job.ID},
		"invalid ID":    {ID: "../other", Bucket: bucket, ObjectKey: "results/../other.txt"},
	} {
		t.Run(name, func(t *testing.T) {
			if validJob(invalid, bucket) {
				t.Fatal("expected job to be rejected")
			}
		})
	}
}
