package main

import (
	"time"
)

// TestTimeFixtures provides consistent time values for testing age filtering
type TestTimeFixtures struct {
	Now                 time.Time
	OneHourAgo          time.Time
	OneDayAgo           time.Time
	ThreeDaysAgo        time.Time
	SevenDaysAgo        time.Time
	EightDaysAgo        time.Time
	ThirtyDaysAgo       time.Time
	SixtyDaysAgo        time.Time
}

// NewTestTimeFixtures creates a fresh set of time fixtures for testing
func NewTestTimeFixtures() TestTimeFixtures {
	now := time.Now()
	return TestTimeFixtures{
		Now:           now,
		OneHourAgo:    now.Add(-1 * time.Hour),
		OneDayAgo:     now.Add(-1 * 24 * time.Hour),
		ThreeDaysAgo:  now.Add(-3 * 24 * time.Hour),
		SevenDaysAgo:  now.Add(-7 * 24 * time.Hour),
		EightDaysAgo:  now.Add(-8 * 24 * time.Hour),
		ThirtyDaysAgo: now.Add(-30 * 24 * time.Hour),
		SixtyDaysAgo:  now.Add(-60 * 24 * time.Hour),
	}
}

// ConfigBuilder is a fluent builder for Config in tests
type ConfigBuilder struct {
	cfg Config
}

// NewConfigBuilder creates a new config builder with sensible defaults
func NewConfigBuilder() *ConfigBuilder {
	return &ConfigBuilder{
		cfg: Config{
			RabbitMQURL:        "amqp://localhost:5672",
			QueueName:          "test-queue",
			S3Region:           "us-east-1",
			S3AccessKeyID:      "testkey",
			S3SecretAccessKey:  "testsecret",
			S3Endpoint:         "http://localhost:9000",
			S3Bucket:           "test-bucket",
			RetentionUploads:   7,
			RetentionRedacted:  7,
			RetentionFailed:    7,
			CleanupInterval:    1,
			DryRun:             false,
		},
	}
}

// WithRetentionUploads sets the retention period for uploads
func (cb *ConfigBuilder) WithRetentionUploads(days int) *ConfigBuilder {
	cb.cfg.RetentionUploads = days
	return cb
}

// WithRetentionRedacted sets the retention period for redacted results
func (cb *ConfigBuilder) WithRetentionRedacted(days int) *ConfigBuilder {
	cb.cfg.RetentionRedacted = days
	return cb
}

// WithRetentionFailed sets the retention period for failed queue messages
func (cb *ConfigBuilder) WithRetentionFailed(days int) *ConfigBuilder {
	cb.cfg.RetentionFailed = days
	return cb
}

// WithDryRun enables or disables dry-run mode
func (cb *ConfigBuilder) WithDryRun(dryRun bool) *ConfigBuilder {
	cb.cfg.DryRun = dryRun
	return cb
}

// WithCleanupInterval sets the cleanup interval
func (cb *ConfigBuilder) WithCleanupInterval(minutes int) *ConfigBuilder {
	cb.cfg.CleanupInterval = minutes
	return cb
}

// WithS3Bucket sets the S3 bucket name
func (cb *ConfigBuilder) WithS3Bucket(bucket string) *ConfigBuilder {
	cb.cfg.S3Bucket = bucket
	return cb
}

// WithQueueName sets the RabbitMQ queue name
func (cb *ConfigBuilder) WithQueueName(name string) *ConfigBuilder {
	cb.cfg.QueueName = name
	return cb
}

// Build returns the constructed Config
func (cb *ConfigBuilder) Build() Config {
	return cb.cfg
}

// IsExpired checks if a timestamp is older than the given retention period (in seconds)
func IsExpired(lastModified time.Time, retentionSeconds int64) bool {
	cutoffTime := time.Now().Add(-time.Duration(retentionSeconds) * time.Second)
	return lastModified.Before(cutoffTime)
}

// DaysToSeconds converts days to seconds
func DaysToSeconds(days int) int64 {
	return int64(days) * 24 * 60 * 60
}

// SecondsToDays converts seconds to days
func SecondsToDays(seconds int64) int {
	return int(seconds / (24 * 60 * 60))
}
