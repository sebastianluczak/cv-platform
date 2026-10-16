package main

import (
	"os"
	"strings"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	// Set required env vars, clear retention vars to test defaults
	t.Setenv("RABBITMQ_URL", "amqp://test")
	t.Setenv("QUEUE_NAME", "test-queue")
	t.Setenv("RETENTION_UPLOADS_DAYS", "")
	t.Setenv("RETENTION_REDACTED_DAYS", "")
	t.Setenv("RETENTION_FAILED_QUEUE_DAYS", "")
	t.Setenv("CLEANUP_INTERVAL_MINUTES", "")
	t.Setenv("DRY_RUN", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}

	tests := []struct {
		name  string
		got   interface{}
		want  interface{}
		field string
	}{
		{"S3 Region", cfg.S3Region, "us-east-1", "S3Region"},
		{"S3 Bucket", cfg.S3Bucket, "cv-platform-uploads", "S3Bucket"},
		{"S3 Endpoint", cfg.S3Endpoint, "http://localhost:9000", "S3Endpoint"},
		{"S3 Access Key ID", cfg.S3AccessKeyID, "rustfsadmin", "S3AccessKeyID"},
		{"S3 Secret Access Key", cfg.S3SecretAccessKey, "rustfsadmin", "S3SecretAccessKey"},
		{"RabbitMQ URL", cfg.RabbitMQURL, "amqp://test", "RabbitMQURL"},
		{"Queue Name", cfg.QueueName, "test-queue", "QueueName"},
		{"Retention Uploads", cfg.RetentionUploads, 7, "RetentionUploads"},
		{"Retention Redacted", cfg.RetentionRedacted, 7, "RetentionRedacted"},
		{"Retention Failed", cfg.RetentionFailed, 7, "RetentionFailed"},
		{"Cleanup Interval", cfg.CleanupInterval, 1, "CleanupInterval"},
		{"Dry Run", cfg.DryRun, false, "DryRun"},
	}

	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, tt.got, tt.want)
		}
	}
}

func TestLoadConfig_FromEnvironment(t *testing.T) {
	t.Setenv("RABBITMQ_URL", "amqp://custom-host:5672")
	t.Setenv("QUEUE_NAME", "custom-queue")
	t.Setenv("RUSTFS_REGION", "eu-west-1")
	t.Setenv("RUSTFS_ACCESS_KEY_ID", "custom-access-key")
	t.Setenv("RUSTFS_SECRET_ACCESS_KEY", "custom-secret-key")
	t.Setenv("RUSTFS_ENDPOINT_URL", "https://custom-s3.example.com")
	t.Setenv("RUSTFS_BUCKET", "custom-bucket")
	t.Setenv("RETENTION_UPLOADS_DAYS", "14")
	t.Setenv("RETENTION_REDACTED_DAYS", "30")
	t.Setenv("RETENTION_FAILED_QUEUE_DAYS", "21")
	t.Setenv("CLEANUP_INTERVAL_MINUTES", "5")
	t.Setenv("DRY_RUN", "true")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"RabbitMQ URL", cfg.RabbitMQURL, "amqp://custom-host:5672"},
		{"Queue Name", cfg.QueueName, "custom-queue"},
		{"S3 Region", cfg.S3Region, "eu-west-1"},
		{"S3 Access Key ID", cfg.S3AccessKeyID, "custom-access-key"},
		{"S3 Secret Access Key", cfg.S3SecretAccessKey, "custom-secret-key"},
		{"S3 Endpoint", cfg.S3Endpoint, "https://custom-s3.example.com"},
		{"S3 Bucket", cfg.S3Bucket, "custom-bucket"},
		{"Retention Uploads", cfg.RetentionUploads, 14},
		{"Retention Redacted", cfg.RetentionRedacted, 30},
		{"Retention Failed", cfg.RetentionFailed, 21},
		{"Cleanup Interval", cfg.CleanupInterval, 5},
		{"Dry Run", cfg.DryRun, true},
	}

	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, tt.got, tt.want)
		}
	}
}

func TestLoadConfig_Validation(t *testing.T) {
	tests := []struct {
		name      string
		envSetup  map[string]string
		wantError bool
		errPart   string
	}{
		{
			name: "Missing RABBITMQ_URL",
			envSetup: map[string]string{
				"RABBITMQ_URL": "",
				"QUEUE_NAME":   "test-queue",
			},
			wantError: true,
			errPart:   "RABBITMQ_URL is not set",
		},
		{
			name: "Missing QUEUE_NAME",
			envSetup: map[string]string{
				"RABBITMQ_URL": "amqp://test",
				"QUEUE_NAME":   "",
			},
			wantError: true,
			errPart:   "QUEUE_NAME is not set",
		},
		{
			name: "RetentionUploads must be > 0",
			envSetup: map[string]string{
				"RABBITMQ_URL":           "amqp://test",
				"QUEUE_NAME":             "test-queue",
				"RETENTION_UPLOADS_DAYS": "0",
			},
			wantError: true,
			errPart:   "RETENTION_UPLOADS_DAYS must be greater than 0",
		},
		{
			name: "RetentionRedacted must be > 0",
			envSetup: map[string]string{
				"RABBITMQ_URL":            "amqp://test",
				"QUEUE_NAME":              "test-queue",
				"RETENTION_REDACTED_DAYS": "-1",
			},
			wantError: true,
			errPart:   "RETENTION_REDACTED_DAYS must be greater than 0",
		},
		{
			name: "RetentionFailed must be > 0",
			envSetup: map[string]string{
				"RABBITMQ_URL":                "amqp://test",
				"QUEUE_NAME":                  "test-queue",
				"RETENTION_FAILED_QUEUE_DAYS": "0",
			},
			wantError: true,
			errPart:   "RETENTION_FAILED_QUEUE_DAYS must be greater than 0",
		},
		{
			name: "CleanupInterval must be > 0",
			envSetup: map[string]string{
				"RABBITMQ_URL":             "amqp://test",
				"QUEUE_NAME":               "test-queue",
				"CLEANUP_INTERVAL_MINUTES": "-5",
			},
			wantError: true,
			errPart:   "CLEANUP_INTERVAL_MINUTES must be greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear and set environment
			for key := range tt.envSetup {
				t.Setenv(key, tt.envSetup[key])
			}

			cfg, err := LoadConfig()
			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error, got nil; config: %+v", cfg)
				} else if !strings.Contains(err.Error(), tt.errPart) {
					t.Errorf("Error message mismatch: got %q, want to contain %q", err.Error(), tt.errPart)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestLoadConfig_InvalidValues(t *testing.T) {
	t.Setenv("RABBITMQ_URL", "amqp://test")
	t.Setenv("QUEUE_NAME", "test-queue")

	tests := []struct {
		name        string
		envVar      string
		value       string
		expectError bool
	}{
		{"Invalid int in RETENTION_UPLOADS_DAYS", "RETENTION_UPLOADS_DAYS", "not-a-number", false},
		{"Invalid int in RETENTION_REDACTED_DAYS", "RETENTION_REDACTED_DAYS", "abc", false},
		{"Invalid int in RETENTION_FAILED_QUEUE_DAYS", "RETENTION_FAILED_QUEUE_DAYS", "xyz", false},
		{"Invalid int in CLEANUP_INTERVAL_MINUTES", "CLEANUP_INTERVAL_MINUTES", "12.5", false},
		{"Invalid bool DRY_RUN (true)", "DRY_RUN", "true", false},
		{"Invalid bool DRY_RUN (yes)", "DRY_RUN", "yes", false},
		{"Invalid bool DRY_RUN (1)", "DRY_RUN", "1", false},
		{"Invalid bool DRY_RUN (invalid)", "DRY_RUN", "maybe", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envVar, tt.value)
			t.Setenv("RETENTION_UPLOADS_DAYS", "7")
			t.Setenv("RETENTION_REDACTED_DAYS", "7")
			t.Setenv("RETENTION_FAILED_QUEUE_DAYS", "7")
			t.Setenv("CLEANUP_INTERVAL_MINUTES", "1")

			cfg, err := LoadConfig()
			if tt.expectError && err == nil {
				t.Errorf("Expected error for %s=%q", tt.envVar, tt.value)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error for %s=%q: %v", tt.envVar, tt.value, err)
			}
			// Invalid int values should use defaults, invalid bool uses default
			if err == nil {
				if cfg.RetentionUploads <= 0 && cfg.RetentionRedacted <= 0 && cfg.RetentionFailed <= 0 && cfg.CleanupInterval <= 0 {
					t.Errorf("Expected fallback to defaults for invalid values")
				}
			}
		})
	}
}

func TestParseIntEnv(t *testing.T) {

	t.Run("Valid integer", func(t *testing.T) {
		t.Setenv("TEST_INT", "42")
		result := parseIntEnv("TEST_INT", 10)
		if result != 42 {
			t.Errorf("parseIntEnv: got %d, want 42", result)
		}
	})

	t.Run("Empty string returns default", func(t *testing.T) {
		t.Setenv("TEST_INT", "")
		result := parseIntEnv("TEST_INT", 10)
		if result != 10 {
			t.Errorf("parseIntEnv: got %d, want 10", result)
		}
	})

	t.Run("Invalid value returns default", func(t *testing.T) {
		t.Setenv("TEST_INT", "not-a-number")
		result := parseIntEnv("TEST_INT", 10)
		if result != 10 {
			t.Errorf("parseIntEnv: got %d, want 10", result)
		}
	})

	t.Run("Whitespace is trimmed", func(t *testing.T) {
		t.Setenv("TEST_INT", "  99  ")
		result := parseIntEnv("TEST_INT", 10)
		if result != 99 {
			t.Errorf("parseIntEnv: got %d, want 99", result)
		}
	})

	t.Run("Negative values are accepted", func(t *testing.T) {
		t.Setenv("TEST_INT", "-5")
		result := parseIntEnv("TEST_INT", 10)
		if result != -5 {
			t.Errorf("parseIntEnv: got %d, want -5", result)
		}
	})
}

func TestParseBoolEnv(t *testing.T) {

	tests := []struct {
		name         string
		value        string
		defaultValue bool
		want         bool
	}{
		{"true", "true", false, true},
		{"1", "1", false, true},
		{"yes", "yes", false, true},
		{"false", "false", true, false},
		{"0", "0", true, false},
		{"no", "no", true, false},
		{"empty string", "", false, false},
		{"invalid value defaults to false", "maybe", false, false},
		{"invalid value defaults to true", "maybe", true, true},
		{"whitespace true", "  true  ", false, true},
		{"whitespace false", "  false  ", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envKey := "TEST_BOOL_" + tt.name
			if tt.value != "" {
				t.Setenv(envKey, tt.value)
			} else {
				os.Unsetenv(envKey)
			}
			result := parseBoolEnv(envKey, tt.defaultValue)
			if result != tt.want {
				t.Errorf("parseBoolEnv(%q, %v): got %v, want %v", tt.value, tt.defaultValue, result, tt.want)
			}
		})
	}
}
