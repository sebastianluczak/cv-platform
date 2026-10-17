package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"
)

// LogEntry represents a structured log entry.
type LogEntry struct {
	Timestamp string      `json:"timestamp"`
	Level     string      `json:"level"`
	Operation string      `json:"operation"`
	Message   string      `json:"message"`
	Details   interface{} `json:"details,omitempty"`
}

// writeLog outputs a structured log entry as JSON.
func writeLog(entry LogEntry) {
	entry.Timestamp = time.Now().UTC().Format(time.RFC3339)
	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("ERROR: Failed to marshal log entry: %v", err)
		return
	}
	fmt.Fprintln(os.Stderr, string(data))
}

// LogStartup logs service startup with configuration summary.
func LogStartup(config Config) {
	writeLog(LogEntry{
		Level:     "INFO",
		Operation: "startup",
		Message:   "Datacleaner service initialized",
		Details: map[string]interface{}{
			"queue_name":                  config.QueueName,
			"retention_uploads_days":      config.RetentionUploads,
			"retention_redacted_days":     config.RetentionRedacted,
			"retention_failed_queue_days": config.RetentionFailed,
			"cleanup_interval_minutes":    config.CleanupInterval,
			"dry_run":                     config.DryRun,
		},
	})
}

// LogCleanupStart logs the start of a cleanup cycle.
func LogCleanupStart(interval time.Duration) {
	writeLog(LogEntry{
		Level:     "INFO",
		Operation: "cleanup_cycle_start",
		Message:   "Starting cleanup cycle",
		Details: map[string]interface{}{
			"interval_seconds": interval.Seconds(),
		},
	})
}

// LogS3ObjectsScanned logs S3 scan results.
func LogS3ObjectsScanned(count int, totalSize int64) {
	writeLog(LogEntry{
		Level:     "INFO",
		Operation: "s3_scan_complete",
		Message:   "Completed S3 scan for candidate deletions",
		Details: map[string]interface{}{
			"objects_found": count,
			"total_size":    totalSize,
		},
	})
}

// LogS3ObjectDeleted logs each S3 object deletion.
func LogS3ObjectDeleted(key string, age time.Duration, dryRun bool) {
	details := map[string]interface{}{
		"key":      key,
		"age_days": float64(age.Hours()) / 24.0,
		"dry_run":  dryRun,
	}
	message := "Deleted expired S3 object"
	if dryRun {
		message = "[DRY RUN] Would delete expired S3 object"
	}
	writeLog(LogEntry{
		Level:     "INFO",
		Operation: "s3_object_deleted",
		Message:   message,
		Details:   details,
	})
}

// LogS3CleanupSummary logs a summary of S3 cleanup pass.
func LogS3CleanupSummary(result cleanupResult, duration time.Duration, dryRun bool) {
	writeLog(LogEntry{
		Level:     "INFO",
		Operation: "s3_cleanup_summary",
		Message:   "S3 cleanup pass completed",
		Details: map[string]interface{}{
			"objects_deleted":      result.deleted,
			"objects_would_delete": result.wouldDelete,
			"failures":             result.failures,
			"duration_seconds":     duration.Seconds(),
			"dry_run":              dryRun,
		},
	})
}

// LogRabbitMQCleanupSummary logs a summary of RabbitMQ cleanup.
func LogRabbitMQCleanupSummary(result cleanupResult, duration time.Duration, dryRun bool) {
	writeLog(LogEntry{
		Level:     "INFO",
		Operation: "rabbitmq_cleanup_summary",
		Message:   "RabbitMQ cleanup pass completed",
		Details: map[string]interface{}{
			"messages_purged":      result.deleted,
			"messages_would_purge": result.wouldDelete,
			"failures":             result.failures,
			"duration_seconds":     duration.Seconds(),
			"dry_run":              dryRun,
		},
	})
}

// LogError logs errors with context.
func LogError(operation string, err error) {
	writeLog(LogEntry{
		Level:     "ERROR",
		Operation: operation,
		Message:   fmt.Sprintf("Error during %s: %v", operation, err),
		Details: map[string]interface{}{
			"error": err.Error(),
		},
	})
}

// LogCleanupCycle logs overall cycle summary with metrics.
func LogCleanupCycle(duration time.Duration, s3, rabbitMQ cleanupResult) {
	writeLog(LogEntry{
		Level:     "INFO",
		Operation: "cleanup_cycle_complete",
		Message:   "Cleanup cycle completed",
		Details: map[string]interface{}{
			"total_duration_seconds":        duration.Seconds(),
			"s3_objects_deleted":            s3.deleted,
			"s3_objects_would_delete":       s3.wouldDelete,
			"rabbitmq_messages_purged":      rabbitMQ.deleted,
			"rabbitmq_messages_would_purge": rabbitMQ.wouldDelete,
			"failures":                      s3.failures + rabbitMQ.failures,
		},
	})
}

// LogStartupError logs startup errors.
func LogStartupError(msg string, err error) {
	writeLog(LogEntry{
		Level:     "ERROR",
		Operation: "startup_error",
		Message:   msg,
		Details: map[string]interface{}{
			"error": err.Error(),
		},
	})
}
