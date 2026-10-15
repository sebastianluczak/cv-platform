package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// captureStderr redirects stderr to capture log output
func captureStderr(fn func()) string {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	fn()

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestLogEntry_JSON(t *testing.T) {

	entry := LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     "INFO",
		Operation: "test_operation",
		Message:   "Test message",
		Details: map[string]interface{}{
			"key": "value",
		},
	}

	// Marshal to JSON to verify it's valid
	data, err := json.Marshal(entry)
	if err != nil {
		t.Errorf("Failed to marshal LogEntry to JSON: %v", err)
	}

	if len(data) == 0 {
		t.Errorf("Marshaled JSON is empty")
	}

	// Unmarshal to verify roundtrip
	var unmarshaled LogEntry
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Errorf("Failed to unmarshal JSON: %v", err)
	}

	if unmarshaled.Level != "INFO" || unmarshaled.Operation != "test_operation" {
		t.Errorf("Roundtrip JSON mismatch: got %+v", unmarshaled)
	}
}

func TestLogStartup_Format(t *testing.T) {

	cfg := Config{
		QueueName:              "test-queue",
		RetentionUploads:       7,
		RetentionRedacted:      7,
		RetentionFailed:        7,
		CleanupInterval:        1,
		DryRun:                 false,
		S3Region:               "us-east-1",
		S3Endpoint:             "http://localhost:9000",
		S3Bucket:               "test-bucket",
		S3AccessKeyID:          "test-key",
		S3SecretAccessKey:      "test-secret",
		RabbitMQURL:            "amqp://localhost",
	}

	output := captureStderr(func() {
		LogStartup(cfg)
	})

	if !strings.Contains(output, "startup") {
		t.Errorf("Expected 'startup' operation in log, got: %s", output)
	}

	if !strings.Contains(output, "test-queue") {
		t.Errorf("Expected queue name in log, got: %s", output)
	}

	// Verify JSON validity
	var logData map[string]interface{}
	if err := json.Unmarshal([]byte(output), &logData); err != nil {
		t.Errorf("Log output is not valid JSON: %v\nOutput: %s", err, output)
	}

	// Check expected fields
	expectedFields := []string{"timestamp", "level", "operation", "message", "details"}
	for _, field := range expectedFields {
		if _, ok := logData[field]; !ok {
			t.Errorf("Missing expected field %q in log entry", field)
		}
	}
}

func TestLogError_WithError(t *testing.T) {

	testErr := "test error message"
	testOp := "test_operation"

	output := captureStderr(func() {
		LogError(testOp, errorf(testErr))
	})

	if !strings.Contains(output, "ERROR") {
		t.Errorf("Expected 'ERROR' level in log, got: %s", output)
	}

	if !strings.Contains(output, testOp) {
		t.Errorf("Expected operation %q in log, got: %s", testOp, output)
	}

	if !strings.Contains(output, testErr) {
		t.Errorf("Expected error message %q in log, got: %s", testErr, output)
	}

	// Verify JSON validity
	var logData map[string]interface{}
	if err := json.Unmarshal([]byte(output), &logData); err != nil {
		t.Errorf("Log output is not valid JSON: %v", err)
	}

	level, ok := logData["level"].(string)
	if !ok || level != "ERROR" {
		t.Errorf("Expected level ERROR, got: %v", level)
	}
}

func TestLogCleanupStart_Format(t *testing.T) {

	interval := 5 * time.Minute

	output := captureStderr(func() {
		LogCleanupStart(interval)
	})

	if !strings.Contains(output, "cleanup_cycle_start") {
		t.Errorf("Expected 'cleanup_cycle_start' operation in log, got: %s", output)
	}

	var logData map[string]interface{}
	if err := json.Unmarshal([]byte(output), &logData); err != nil {
		t.Errorf("Log output is not valid JSON: %v", err)
	}

	// Verify details contains interval_seconds
	details, ok := logData["details"].(map[string]interface{})
	if !ok {
		t.Errorf("Expected details object in log")
	} else if intervalSeconds, ok := details["interval_seconds"].(float64); !ok || intervalSeconds != 300.0 {
		t.Errorf("Expected interval_seconds to be 300, got: %v", intervalSeconds)
	}
}

func TestLogS3ObjectsScanned_Format(t *testing.T) {

	objectCount := 42
	totalSize := int64(1024 * 1024 * 100) // 100 MB

	output := captureStderr(func() {
		LogS3ObjectsScanned(objectCount, totalSize)
	})

	if !strings.Contains(output, "s3_scan_complete") {
		t.Errorf("Expected 's3_scan_complete' operation in log")
	}

	var logData map[string]interface{}
	if err := json.Unmarshal([]byte(output), &logData); err != nil {
		t.Errorf("Log output is not valid JSON: %v", err)
	}

	details, _ := logData["details"].(map[string]interface{})
	if found, ok := details["objects_found"].(float64); !ok || found != float64(objectCount) {
		t.Errorf("Expected objects_found to be %d, got: %v", objectCount, found)
	}
}

func TestLogS3ObjectDeleted_Format(t *testing.T) {

	key := "uploads/test-job-id"
	age := 8 * 24 * time.Hour

	tests := []struct {
		name   string
		dryRun bool
		msgKey string
	}{
		{"Normal deletion", false, "Deleted expired S3 object"},
		{"Dry-run deletion", true, "[DRY RUN] Would delete expired S3 object"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureStderr(func() {
				LogS3ObjectDeleted(key, age, tt.dryRun)
			})

			if !strings.Contains(output, "s3_object_deleted") {
				t.Errorf("Expected 's3_object_deleted' operation in log")
			}

			if !strings.Contains(output, tt.msgKey) {
				t.Errorf("Expected message containing %q, got: %s", tt.msgKey, output)
			}

			var logData map[string]interface{}
			if err := json.Unmarshal([]byte(output), &logData); err != nil {
				t.Errorf("Log output is not valid JSON: %v", err)
			}

			details, _ := logData["details"].(map[string]interface{})
			if objKey, ok := details["key"].(string); !ok || objKey != key {
				t.Errorf("Expected key to be %q, got: %v", key, objKey)
			}

			if dryRun, ok := details["dry_run"].(bool); !ok || dryRun != tt.dryRun {
				t.Errorf("Expected dry_run to be %v, got: %v", tt.dryRun, dryRun)
			}
		})
	}
}

func TestLogS3CleanupSummary_Format(t *testing.T) {

	deleted := 50
	failed := 2
	duration := 30 * time.Second
	dryRun := false

	output := captureStderr(func() {
		LogS3CleanupSummary(deleted, failed, duration, dryRun)
	})

	if !strings.Contains(output, "s3_cleanup_summary") {
		t.Errorf("Expected 's3_cleanup_summary' operation in log")
	}

	var logData map[string]interface{}
	if err := json.Unmarshal([]byte(output), &logData); err != nil {
		t.Errorf("Log output is not valid JSON: %v", err)
	}

	details, _ := logData["details"].(map[string]interface{})
	if objDeleted, ok := details["objects_deleted"].(float64); !ok || objDeleted != float64(deleted) {
		t.Errorf("Expected objects_deleted to be %d, got: %v", deleted, objDeleted)
	}

	if failures, ok := details["failures"].(float64); !ok || failures != float64(failed) {
		t.Errorf("Expected failures to be %d, got: %v", failed, failures)
	}

	if dur, ok := details["duration_seconds"].(float64); !ok || dur != 30.0 {
		t.Errorf("Expected duration_seconds to be 30, got: %v", dur)
	}
}

func TestLogRabbitMQCleanupSummary_Format(t *testing.T) {

	purged := 25
	failed := 1
	duration := 15 * time.Second
	dryRun := true

	output := captureStderr(func() {
		LogRabbitMQCleanupSummary(purged, failed, duration, dryRun)
	})

	if !strings.Contains(output, "rabbitmq_cleanup_summary") {
		t.Errorf("Expected 'rabbitmq_cleanup_summary' operation in log")
	}

	var logData map[string]interface{}
	if err := json.Unmarshal([]byte(output), &logData); err != nil {
		t.Errorf("Log output is not valid JSON: %v", err)
	}

	details, _ := logData["details"].(map[string]interface{})
	if msgPurged, ok := details["messages_purged"].(float64); !ok || msgPurged != float64(purged) {
		t.Errorf("Expected messages_purged to be %d, got: %v", purged, msgPurged)
	}

	if dryRunVal, ok := details["dry_run"].(bool); !ok || dryRunVal != dryRun {
		t.Errorf("Expected dry_run to be %v, got: %v", dryRun, dryRunVal)
	}
}

func TestLogCleanupCycle_Format(t *testing.T) {

	duration := 45 * time.Second
	s3Deleted := 100
	mqPurged := 50

	output := captureStderr(func() {
		LogCleanupCycle(duration, s3Deleted, mqPurged)
	})

	if !strings.Contains(output, "cleanup_cycle_complete") {
		t.Errorf("Expected 'cleanup_cycle_complete' operation in log")
	}

	var logData map[string]interface{}
	if err := json.Unmarshal([]byte(output), &logData); err != nil {
		t.Errorf("Log output is not valid JSON: %v", err)
	}

	details, _ := logData["details"].(map[string]interface{})
	if totalDuration, ok := details["total_duration_seconds"].(float64); !ok || totalDuration != 45.0 {
		t.Errorf("Expected total_duration_seconds to be 45, got: %v", totalDuration)
	}
}

func TestLogStartupError_Format(t *testing.T) {

	msg := "Configuration loading failed"
	err := errorf("invalid configuration value")

	output := captureStderr(func() {
		LogStartupError(msg, err)
	})

	if !strings.Contains(output, "startup_error") {
		t.Errorf("Expected 'startup_error' operation in log")
	}

	if !strings.Contains(output, msg) {
		t.Errorf("Expected message %q in log", msg)
	}

	var logData map[string]interface{}
	if err := json.Unmarshal([]byte(output), &logData); err != nil {
		t.Errorf("Log output is not valid JSON: %v", err)
	}

	level, _ := logData["level"].(string)
	if level != "ERROR" {
		t.Errorf("Expected level ERROR for startup error")
	}
}

func TestLogEntry_WithoutDetails(t *testing.T) {

	entry := LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     "INFO",
		Operation: "test",
		Message:   "test message",
		Details:   nil,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Errorf("Failed to marshal LogEntry without Details: %v", err)
	}

	var unmarshaled LogEntry
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Errorf("Failed to unmarshal JSON: %v", err)
	}

	// Details should be omitted when nil (due to omitempty tag)
	if unmarshaled.Details != nil && unmarshaled.Details != "" {
		t.Logf("Details should be omitted for nil values (got %v)", unmarshaled.Details)
	}
}

func TestLogTimestampFormat(t *testing.T) {

	output := captureStderr(func() {
		LogCleanupStart(1 * time.Minute)
	})

	var logData map[string]interface{}
	if err := json.Unmarshal([]byte(output), &logData); err != nil {
		t.Errorf("Failed to parse log as JSON: %v", err)
	}

	timestampStr, ok := logData["timestamp"].(string)
	if !ok {
		t.Errorf("timestamp field should be a string")
	}

	// Verify it's a valid RFC3339 timestamp
	_, err := time.Parse(time.RFC3339, timestampStr)
	if err != nil {
		t.Errorf("timestamp %q is not valid RFC3339 format: %v", timestampStr, err)
	}
}

// Helper to create an error (since errors.New is not typically used in tests here)
func errorf(msg string) error {
	return &testError{msg: msg}
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
