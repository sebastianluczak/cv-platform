package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	// S3 config
	S3Region          string
	S3AccessKeyID     string
	S3SecretAccessKey string
	S3Endpoint        string
	S3Bucket          string

	// RabbitMQ config
	RabbitMQURL string
	QueueName   string

	// Retention periods (in days)
	RetentionUploads  int // original resume files (uploads/<job-id>)
	RetentionRedacted int // redacted results (results/<job-id>.redacted.txt)
	RetentionFailed   int // failed queue messages

	// Cleanup interval (in minutes)
	CleanupInterval int

	// Dry-run mode (log what would be deleted, don't actually delete)
	DryRun bool
}

// LoadConfig loads configuration from environment variables with defaults
func LoadConfig() (Config, error) {
	cfg := Config{}

	// RabbitMQ config
	cfg.RabbitMQURL = strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	if cfg.RabbitMQURL == "" {
		return cfg, fmt.Errorf("RABBITMQ_URL is not set")
	}

	cfg.QueueName = strings.TrimSpace(os.Getenv("QUEUE_NAME"))
	if cfg.QueueName == "" {
		return cfg, fmt.Errorf("QUEUE_NAME is not set")
	}

	// S3 config
	cfg.S3Region = os.Getenv("RUSTFS_REGION")
	if cfg.S3Region == "" {
		cfg.S3Region = "us-east-1"
	}

	cfg.S3AccessKeyID = os.Getenv("RUSTFS_ACCESS_KEY_ID")
	if cfg.S3AccessKeyID == "" {
		cfg.S3AccessKeyID = "rustfsadmin"
	}

	cfg.S3SecretAccessKey = os.Getenv("RUSTFS_SECRET_ACCESS_KEY")
	if cfg.S3SecretAccessKey == "" {
		cfg.S3SecretAccessKey = "rustfsadmin"
	}

	cfg.S3Endpoint = os.Getenv("RUSTFS_ENDPOINT_URL")
	if cfg.S3Endpoint == "" {
		cfg.S3Endpoint = "http://localhost:9000"
	}

	cfg.S3Bucket = os.Getenv("RUSTFS_BUCKET")
	if cfg.S3Bucket == "" {
		cfg.S3Bucket = "cv-platform-uploads"
	}

	// Retention periods
	cfg.RetentionUploads = parseIntEnv("RETENTION_UPLOADS_DAYS", 7)
	if cfg.RetentionUploads <= 0 {
		return cfg, fmt.Errorf("RETENTION_UPLOADS_DAYS must be greater than 0, got %d", cfg.RetentionUploads)
	}

	cfg.RetentionRedacted = parseIntEnv("RETENTION_REDACTED_DAYS", 7)
	if cfg.RetentionRedacted <= 0 {
		return cfg, fmt.Errorf("RETENTION_REDACTED_DAYS must be greater than 0, got %d", cfg.RetentionRedacted)
	}

	cfg.RetentionFailed = parseIntEnv("RETENTION_FAILED_QUEUE_DAYS", 7)
	if cfg.RetentionFailed <= 0 {
		return cfg, fmt.Errorf("RETENTION_FAILED_QUEUE_DAYS must be greater than 0, got %d", cfg.RetentionFailed)
	}

	// Cleanup interval
	cfg.CleanupInterval = parseIntEnv("CLEANUP_INTERVAL_MINUTES", 1)
	if cfg.CleanupInterval <= 0 {
		return cfg, fmt.Errorf("CLEANUP_INTERVAL_MINUTES must be greater than 0, got %d", cfg.CleanupInterval)
	}

	// Dry-run mode
	cfg.DryRun = parseBoolEnv("DRY_RUN", false)

	return cfg, nil
}

// parseIntEnv parses an integer from environment variable, returning default if not set or invalid
func parseIntEnv(key string, defaultValue int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	intValue, err := strconv.Atoi(value)
	if err != nil {
		log.Printf("Invalid %s value %q, using default %d", key, value, defaultValue)
		return defaultValue
	}
	return intValue
}

// parseBoolEnv parses a boolean from environment variable, returning default if not set or invalid
func parseBoolEnv(key string, defaultValue bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	if value == "false" || value == "0" || value == "no" {
		return false
	}
	if value == "true" || value == "1" || value == "yes" {
		return true
	}
	log.Printf("Invalid %s value %q, using default %v", key, value, defaultValue)
	return defaultValue
}
