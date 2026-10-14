package main

import (
	"testing"
)

func TestValidJobRequiresExpectedBucketAndRedactedKey(t *testing.T) {
	id := "123e4567-e89b-12d3-a456-426614174000"
	valid := matchingJob{ID: id, Bucket: "resumes", ObjectKey: "results/" + id + ".redacted.txt"}
	if !validJob(valid, "resumes") {
		t.Fatal("expected well-formed matching job to be valid")
	}
	for _, job := range []matchingJob{
		{ID: "../other", Bucket: "resumes", ObjectKey: "results/../other.redacted.txt"},
		{ID: id, Bucket: "other", ObjectKey: "results/" + id + ".redacted.txt"},
		{ID: id, Bucket: "resumes", ObjectKey: "results/" + id + ".txt"},
	} {
		if validJob(job, "resumes") {
			t.Errorf("expected invalid job reference to be rejected: %+v", job)
		}
	}
}
