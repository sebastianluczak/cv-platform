package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunCleanupCycleWith_ReportsPartialResultsAndErrors(t *testing.T) {
	s3Called := false
	rabbitMQCalled := false
	s3Err := errors.New("S3 list failed")
	rabbitMQErr := errors.New("failed queue unavailable")

	var cycleErr error
	output := captureStderr(func() {
		cycleErr = runCleanupCycleWith(
			context.Background(),
			time.Minute,
			func(context.Context) (cleanupResult, error) {
				s3Called = true
				return cleanupResult{deleted: 2, failures: 1}, s3Err
			},
			func(context.Context) (cleanupResult, error) {
				rabbitMQCalled = true
				return cleanupResult{deleted: 3, failures: 2}, rabbitMQErr
			},
		)
	})

	if !s3Called || !rabbitMQCalled {
		t.Fatalf("cleanup calls: S3=%v RabbitMQ=%v, want both attempted", s3Called, rabbitMQCalled)
	}
	if !errors.Is(cycleErr, s3Err) || !errors.Is(cycleErr, rabbitMQErr) {
		t.Fatalf("runCleanupCycleWith() error = %v, want both cleanup errors", cycleErr)
	}

	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var entry LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("invalid log entry %q: %v", line, err)
		}
		if entry.Operation != "cleanup_cycle_complete" {
			continue
		}
		details, ok := entry.Details.(map[string]interface{})
		if !ok {
			t.Fatalf("cleanup cycle details have type %T", entry.Details)
		}
		if got := details["s3_objects_deleted"]; got != float64(2) {
			t.Errorf("s3_objects_deleted = %v, want 2", got)
		}
		if got := details["s3_objects_would_delete"]; got != float64(0) {
			t.Errorf("s3_objects_would_delete = %v, want 0", got)
		}
		if got := details["rabbitmq_messages_purged"]; got != float64(3) {
			t.Errorf("rabbitmq_messages_purged = %v, want 3", got)
		}
		if got := details["rabbitmq_messages_would_purge"]; got != float64(0) {
			t.Errorf("rabbitmq_messages_would_purge = %v, want 0", got)
		}
		if got := details["failures"]; got != float64(3) {
			t.Errorf("failures = %v, want 3", got)
		}
		return
	}
	t.Fatal("cleanup cycle summary was not logged")
}
