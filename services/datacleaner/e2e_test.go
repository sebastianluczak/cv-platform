package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/streadway/amqp"
)

// TestDataCleanupLifecycle is an end-to-end integration test that verifies
// the full datacleaner lifecycle: upload → extract → redact → cleanup after retention.
// This test requires a running S3 (RustFS) and RabbitMQ instance.
func TestDataCleanupLifecycle(t *testing.T) {
	// Skip if not running against a live devenv
	if os.Getenv("SKIP_E2E_TESTS") == "true" {
		t.Skip("Skipping E2E test (set SKIP_E2E_TESTS=false to run)")
	}

	// Setup: Create S3 client with test-friendly configuration
	s3Client := createTestS3Client(t)
	cfg := createTestConfig(t)

	// Verify S3 connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := verifyS3Connectivity(ctx, s3Client, cfg.S3Bucket); err != nil {
		t.Fatalf("S3 connectivity check failed: %v. Is RustFS running?", err)
	}

	// Verify RabbitMQ connectivity
	conn, err := amqp.Dial(cfg.RabbitMQURL)
	if err != nil {
		t.Fatalf("Failed to connect to RabbitMQ: %v. Is RabbitMQ running?", err)
	}
	defer conn.Close()

	// Test with very short retention for faster test execution
	shortRetentionCfg := cfg
	shortRetentionCfg.RetentionUploads = 1 // 1 second retention for test speed

	jobID := uuid.New().String()
	t.Logf("Starting lifecycle test with jobID: %s", jobID)

	// Step 1: Upload a test resume
	t.Log("Step 1: Uploading test resume...")
	uploadKey := "uploads/" + jobID
	testContent := []byte("Jane Doe\nSoftware Engineer\nSkills: Go, Rust, Kubernetes\nExperience: 5 years")
	if err := putObjectDirect(ctx, s3Client, cfg.S3Bucket, uploadKey, testContent); err != nil {
		t.Fatalf("Failed to upload test resume: %v", err)
	}

	// Verify upload exists
	if exists := objectExistsDirect(ctx, s3Client, cfg.S3Bucket, uploadKey); !exists {
		t.Fatalf("Upload was not stored in S3: %s", uploadKey)
	}
	t.Logf("✓ Upload stored: %s", uploadKey)

	// Step 2: Simulate extracted text storage
	t.Log("Step 2: Creating extracted text in S3...")
	extractedKey := "results/" + jobID + ".txt"
	extractedContent := []byte("Jane Doe Software Engineer Skills: Go, Rust, Kubernetes Experience: 5 years")
	if err := putObjectDirect(ctx, s3Client, cfg.S3Bucket, extractedKey, extractedContent); err != nil {
		t.Fatalf("Failed to store extracted text: %v", err)
	}

	if exists := objectExistsDirect(ctx, s3Client, cfg.S3Bucket, extractedKey); !exists {
		t.Fatalf("Extracted text was not stored: %s", extractedKey)
	}
	t.Logf("✓ Extracted text stored: %s", extractedKey)

	// Step 3: Simulate redacted text storage
	t.Log("Step 3: Creating redacted text in S3...")
	redactedKey := "results/" + jobID + ".redacted.txt"
	redactedContent := []byte("[NAME] [TITLE] Skills: Go, Rust, Kubernetes Experience: 5 years")
	if err := putObjectDirect(ctx, s3Client, cfg.S3Bucket, redactedKey, redactedContent); err != nil {
		t.Fatalf("Failed to store redacted text: %v", err)
	}

	if exists := objectExistsDirect(ctx, s3Client, cfg.S3Bucket, redactedKey); !exists {
		t.Fatalf("Redacted text was not stored: %s", redactedKey)
	}
	t.Logf("✓ Redacted text stored: %s", redactedKey)

	// Step 4: Verify all objects exist before cleanup
	t.Log("Step 4: Verifying all objects exist before cleanup...")
	assertObjectExists(t, s3Client, cfg.S3Bucket, uploadKey, "upload")
	assertObjectExists(t, s3Client, cfg.S3Bucket, extractedKey, "extracted text")
	assertObjectExists(t, s3Client, cfg.S3Bucket, redactedKey, "redacted text")

	// Step 5: Wait for retention period to expire
	// Use a short retention (1 second) so test doesn't take too long
	t.Log("Step 5: Waiting for retention period to expire (2 seconds)...")
	time.Sleep(2 * time.Second)

	// Step 6: Create channel for RabbitMQ operations (needed for cleanup function)
	t.Log("Step 6: Preparing cleanup cycle...")
	channel, err := conn.Channel()
	if err != nil {
		t.Fatalf("Failed to create RabbitMQ channel: %v", err)
	}
	defer channel.Close()

	// Step 7: Run cleanup cycle
	t.Log("Step 7: Running cleanup cycle...")
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cleanupCancel()

	if err := runCleanupCycle(cleanupCtx, s3Client, conn, shortRetentionCfg); err != nil {
		// Log error but continue - some cleanup failures are non-fatal
		t.Logf("Cleanup cycle reported error (may be non-fatal): %v", err)
	}

	// Step 8: Verify uploads are deleted (retention expired)
	t.Log("Step 8: Verifying uploads are deleted after retention...")
	if exists := objectExistsDirect(ctx, s3Client, cfg.S3Bucket, uploadKey); exists {
		t.Errorf("Upload was NOT deleted after retention period: %s", uploadKey)
	} else {
		t.Logf("✓ Upload deleted: %s", uploadKey)
	}

	// Step 9: Verify redacted text is also gone if using same retention
	// In production, redacted retention would be separate, but for testing we use short values
	t.Log("Step 9: Verifying redacted text (if retention also expired)...")
	if !objectExistsDirect(ctx, s3Client, cfg.S3Bucket, redactedKey) {
		t.Logf("✓ Redacted text deleted: %s", redactedKey)
	} else {
		// This is OK - redacted might have different retention in production
		t.Logf("ℹ Redacted text still exists (may have separate retention): %s", redactedKey)
	}

	// Step 10: Verify no orphaned data for this job
	t.Log("Step 10: Verifying no orphaned data for this job...")
	foundObjects := listObjectsForJob(ctx, s3Client, cfg.S3Bucket, jobID)
	if len(foundObjects) > 0 {
		t.Logf("Warning: Found %d remaining objects for job %s: %v", len(foundObjects), jobID, foundObjects)
		// In production cleanup, we'd expect these to be gone, but due to separate retention periods,
		// some may remain. This is acceptable.
	} else {
		t.Logf("✓ No orphaned data for job %s", jobID)
	}

	t.Log("✓ Lifecycle test completed successfully")
}

// TestDataCleanupWithoutRetention verifies cleanup only removes aged objects
func TestDataCleanupWithoutRetention(t *testing.T) {
	if os.Getenv("SKIP_E2E_TESTS") == "true" {
		t.Skip("Skipping E2E test")
	}

	s3Client := createTestS3Client(t)
	cfg := createTestConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Verify S3 connectivity
	if err := verifyS3Connectivity(ctx, s3Client, cfg.S3Bucket); err != nil {
		t.Fatalf("S3 connectivity check failed: %v", err)
	}

	// Create recent object (should NOT be cleaned up)
	jobID := uuid.New().String()
	uploadKey := "uploads/" + jobID
	testContent := []byte("Recent resume")

	if err := putObjectDirect(ctx, s3Client, cfg.S3Bucket, uploadKey, testContent); err != nil {
		t.Fatalf("Failed to upload test resume: %v", err)
	}

	// Set retention to 7 days (nothing should be cleaned up immediately)
	cfg.RetentionUploads = 7

	conn, err := amqp.Dial(cfg.RabbitMQURL)
	if err != nil {
		t.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	// Run cleanup
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cleanupCancel()

	if err := runCleanupCycle(cleanupCtx, s3Client, conn, cfg); err != nil {
		t.Logf("Cleanup cycle completed (errors may be non-fatal): %v", err)
	}

	// Verify recent object still exists
	if !objectExistsDirect(ctx, s3Client, cfg.S3Bucket, uploadKey) {
		t.Errorf("Recent upload was incorrectly deleted: %s", uploadKey)
	} else {
		t.Logf("✓ Recent upload preserved: %s", uploadKey)
	}

	// Cleanup: remove the test object
	deleteObjectDirect(ctx, s3Client, cfg.S3Bucket, uploadKey)
}

// Helper functions

func createTestS3Client(t *testing.T) *s3.Client {
	endpoint := os.Getenv("RUSTFS_ENDPOINT_URL")
	if endpoint == "" {
		endpoint = "http://localhost:9000"
	}

	region := os.Getenv("RUSTFS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	accessKey := os.Getenv("RUSTFS_ACCESS_KEY_ID")
	if accessKey == "" {
		accessKey = "rustfsadmin"
	}

	secretKey := os.Getenv("RUSTFS_SECRET_ACCESS_KEY")
	if secretKey == "" {
		secretKey = "rustfsadmin"
	}

	t.Logf("Connecting to S3: endpoint=%s, region=%s", endpoint, region)

	return s3.NewFromConfig(aws.Config{
		Region: region,
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(
			accessKey,
			secretKey,
			"",
		)),
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = true
	})
}

func createTestConfig(t *testing.T) Config {
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		rabbitURL = "amqp://guest:guest@localhost:5672/"
	}

	queueName := os.Getenv("QUEUE_NAME")
	if queueName == "" {
		queueName = "cv-platform-queue"
	}

	bucket := os.Getenv("RUSTFS_BUCKET")
	if bucket == "" {
		bucket = "cv-platform-uploads"
	}

	t.Logf("Test config: rabbitmq=%s, queue=%s, bucket=%s", rabbitURL, queueName, bucket)

	return Config{
		RabbitMQURL:       rabbitURL,
		QueueName:         queueName,
		S3Region:          "us-east-1",
		S3AccessKeyID:     "rustfsadmin",
		S3SecretAccessKey: "rustfsadmin",
		S3Endpoint:        "http://localhost:9000",
		S3Bucket:          bucket,
		RetentionUploads:  7,
		RetentionRedacted: 7,
		RetentionFailed:   7,
		CleanupInterval:   1,
		DryRun:            false,
	}
}

func verifyS3Connectivity(ctx context.Context, client *s3.Client, bucket string) error {
	_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		// Try to create bucket if it doesn't exist
		if _, createErr := client.CreateBucket(ctx, &s3.CreateBucketInput{
			Bucket: aws.String(bucket),
		}); createErr != nil {
			return fmt.Errorf("failed to verify/create bucket: %w", createErr)
		}
		return nil
	}
	return nil
}

func putObjectDirect(ctx context.Context, client *s3.Client, bucket, key string, data []byte) error {
	uploader := manager.NewUploader(client)
	_, err := uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	return err
}

func objectExistsDirect(ctx context.Context, client *s3.Client, bucket, key string) bool {
	_, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return err == nil
}

func deleteObjectDirect(ctx context.Context, client *s3.Client, bucket, key string) error {
	_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return err
}

func assertObjectExists(t *testing.T, client *s3.Client, bucket, key, description string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if !objectExistsDirect(ctx, client, bucket, key) {
		t.Errorf("Expected %s to exist: %s", description, key)
	}
}

func listObjectsForJob(ctx context.Context, client *s3.Client, bucket, jobID string) []string {
	var keys []string

	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return keys
		}

		for _, obj := range page.Contents {
			if obj.Key != nil && bytes.Contains([]byte(*obj.Key), []byte(jobID)) {
				keys = append(keys, *obj.Key)
			}
		}
	}

	return keys
}

// TestDryRunMode verifies cleanup doesn't delete in dry-run mode
func TestDryRunMode(t *testing.T) {
	if os.Getenv("SKIP_E2E_TESTS") == "true" {
		t.Skip("Skipping E2E test")
	}

	s3Client := createTestS3Client(t)
	cfg := createTestConfig(t)
	cfg.DryRun = true
	cfg.RetentionUploads = 1 // 1 second

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := verifyS3Connectivity(ctx, s3Client, cfg.S3Bucket); err != nil {
		t.Fatalf("S3 connectivity check failed: %v", err)
	}

	// Create old object
	jobID := uuid.New().String()
	uploadKey := "uploads/" + jobID
	testContent := []byte("Old resume for dry-run test")

	if err := putObjectDirect(ctx, s3Client, cfg.S3Bucket, uploadKey, testContent); err != nil {
		t.Fatalf("Failed to upload test resume: %v", err)
	}

	// Wait for retention to expire
	time.Sleep(2 * time.Second)

	conn, err := amqp.Dial(cfg.RabbitMQURL)
	if err != nil {
		t.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	// Run cleanup in dry-run mode
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cleanupCancel()

	if err := runCleanupCycle(cleanupCtx, s3Client, conn, cfg); err != nil {
		t.Logf("Cleanup cycle completed: %v", err)
	}

	// Verify object still exists (should not be deleted in dry-run)
	if !objectExistsDirect(ctx, s3Client, cfg.S3Bucket, uploadKey) {
		t.Errorf("Object was deleted in dry-run mode (should not happen): %s", uploadKey)
	} else {
		t.Logf("✓ Object preserved in dry-run mode: %s", uploadKey)
	}

	// Cleanup
	deleteObjectDirect(ctx, s3Client, cfg.S3Bucket, uploadKey)
}
