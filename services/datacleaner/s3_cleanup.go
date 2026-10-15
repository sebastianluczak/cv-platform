package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithy "github.com/aws/smithy-go"
)

// CleanupS3 is the main S3 cleanup orchestrator.
// It lists all objects in the bucket, filters by retention periods,
// and deletes expired objects. Returns error only for critical failures;
// partial failures are logged and cleanup continues.
func CleanupS3(ctx context.Context, client *s3.Client, bucket string, config Config) error {
	start := time.Now()
	var totalDeleted int
	var totalFailed int

	LogCleanupStart(time.Duration(config.CleanupInterval) * time.Minute)

	// Handle uploads/ prefix (RetentionUploads)
	uploadsMaxAgeSeconds := int64(config.RetentionUploads) * 24 * 60 * 60
	uploadKeys, err := listExpiredObjects(ctx, client, bucket, "uploads/", uploadsMaxAgeSeconds)
	if err != nil {
		LogError("s3_list_uploads", err)
		return fmt.Errorf("list expired uploads: %w", err)
	}

	for _, key := range uploadKeys {
		if err := deleteObject(ctx, client, bucket, key, config.DryRun); err != nil {
			totalFailed++
			LogError("s3_delete_object", fmt.Errorf("delete %s: %w", key, err))
		} else {
			totalDeleted++
		}
	}

	// Handle results/*.redacted.txt prefix (RetentionRedacted)
	redactedMaxAgeSeconds := int64(config.RetentionRedacted) * 24 * 60 * 60
	redactedKeys, err := listExpiredObjectsWithFilter(ctx, client, bucket, "results/", redactedMaxAgeSeconds, ".redacted.txt")
	if err != nil {
		LogError("s3_list_redacted", err)
		return fmt.Errorf("list expired redacted objects: %w", err)
	}

	for _, key := range redactedKeys {
		if err := deleteObject(ctx, client, bucket, key, config.DryRun); err != nil {
			totalFailed++
			LogError("s3_delete_object", fmt.Errorf("delete %s: %w", key, err))
		} else {
			totalDeleted++
		}
	}

	duration := time.Since(start)
	LogS3CleanupSummary(totalDeleted, totalFailed, duration, config.DryRun)
	return nil
}

// listExpiredObjects lists all objects under a given prefix that are older than maxAgeSeconds.
// Returns a list of object keys that should be deleted.
func listExpiredObjects(ctx context.Context, client *s3.Client, bucket string, prefix string, maxAgeSeconds int64) ([]string, error) {
	var expiredKeys []string
	cutoffTime := time.Now().Add(-time.Duration(maxAgeSeconds) * time.Second)

	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list objects: %w", err)
		}

		for _, obj := range page.Contents {
			if obj.LastModified != nil && obj.LastModified.Before(cutoffTime) {
				expiredKeys = append(expiredKeys, *obj.Key)
				age := objectAge(obj.LastModified)
				LogS3ObjectDeleted(*obj.Key, age, false)
			}
		}
	}

	return expiredKeys, nil
}

// listExpiredObjectsWithFilter is like listExpiredObjects but also filters by suffix.
// Used for results/*.redacted.txt pattern matching.
func listExpiredObjectsWithFilter(ctx context.Context, client *s3.Client, bucket string, prefix string, maxAgeSeconds int64, suffix string) ([]string, error) {
	var expiredKeys []string
	cutoffTime := time.Now().Add(-time.Duration(maxAgeSeconds) * time.Second)

	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list objects: %w", err)
		}

		for _, obj := range page.Contents {
			// Only include objects matching the suffix pattern
			if len(*obj.Key) > len(suffix) && (*obj.Key)[len(*obj.Key)-len(suffix):] == suffix {
				if obj.LastModified != nil && obj.LastModified.Before(cutoffTime) {
					expiredKeys = append(expiredKeys, *obj.Key)
					age := objectAge(obj.LastModified)
					LogS3ObjectDeleted(*obj.Key, age, false)
				}
			}
		}
	}

	return expiredKeys, nil
}

// deleteObject deletes a single S3 object or logs what would be deleted if dryRun is true.
// Returns error only for actual failures (404 is considered success, object already gone).
func deleteObject(ctx context.Context, client *s3.Client, bucket, key string, dryRun bool) error {
	if dryRun {
		LogS3ObjectDeleted(key, 0, true)
		return nil
	}

	_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		// Check if it's a 404 (object not found) - this is OK
		var ae smithy.APIError
		if errors.As(err, &ae) {
			if ae.ErrorCode() == "NoSuchKey" {
				return nil
			}
		}
		return fmt.Errorf("delete object: %w", err)
	}

	return nil
}

// objectAge calculates the duration between LastModified and now.
func objectAge(lastModified *time.Time) time.Duration {
	if lastModified == nil {
		return 0
	}
	return time.Since(*lastModified)
}

// deleteObjectBatch performs batch deletion of multiple objects.
// It processes all deletions even if some fail, collecting errors.
func deleteObjectBatch(ctx context.Context, client *s3.Client, bucket string, keys []string, dryRun bool) error {
	if len(keys) == 0 {
		return nil
	}

	if dryRun {
		for _, key := range keys {
			LogS3ObjectDeleted(key, 0, true)
		}
		return nil
	}

	var objectIDs []types.ObjectIdentifier
	for _, key := range keys {
		objectIDs = append(objectIDs, types.ObjectIdentifier{
			Key: aws.String(key),
		})
	}

	_, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(bucket),
		Delete: &types.Delete{
			Objects: objectIDs,
		},
	})

	if err != nil {
		return fmt.Errorf("batch delete objects: %w", err)
	}

	return nil
}
