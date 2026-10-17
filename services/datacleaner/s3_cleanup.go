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

type expiredS3Object struct {
	key string
	age time.Duration
}

// CleanupS3 is the main S3 cleanup orchestrator.
// It lists all objects in the bucket, filters by retention periods,
// and deletes expired objects. It returns partial results and any errors.
func CleanupS3(ctx context.Context, client *s3.Client, bucket string, config Config) (cleanupResult, error) {
	start := time.Now()
	var result cleanupResult
	var cleanupErrors []error

	// Handle uploads/ prefix (RetentionUploads)
	uploadsMaxAgeSeconds := int64(config.RetentionUploads) * 24 * 60 * 60
	uploadObjects, err := listExpiredObjects(ctx, client, bucket, "uploads/", uploadsMaxAgeSeconds)
	if err != nil {
		LogError("s3_list_uploads", err)
		result.failures++
		cleanupErrors = append(cleanupErrors, fmt.Errorf("list expired uploads: %w", err))
	} else {
		for _, object := range uploadObjects {
			if err := deleteObject(ctx, client, bucket, object.key, config.DryRun); err != nil {
				result.failures++
				wrappedErr := fmt.Errorf("delete %s: %w", object.key, err)
				LogError("s3_delete_object", wrappedErr)
				cleanupErrors = append(cleanupErrors, wrappedErr)
			} else if config.DryRun {
				LogS3ObjectDeleted(object.key, object.age, true)
				result.wouldDelete++
			} else {
				LogS3ObjectDeleted(object.key, object.age, false)
				result.deleted++
			}
		}
	}

	// Handle results/*.redacted.txt prefix (RetentionRedacted)
	redactedMaxAgeSeconds := int64(config.RetentionRedacted) * 24 * 60 * 60
	redactedObjects, err := listExpiredObjectsWithFilter(ctx, client, bucket, "results/", redactedMaxAgeSeconds, ".redacted.txt")
	if err != nil {
		LogError("s3_list_redacted", err)
		result.failures++
		cleanupErrors = append(cleanupErrors, fmt.Errorf("list expired redacted objects: %w", err))
	} else {
		for _, object := range redactedObjects {
			if err := deleteObject(ctx, client, bucket, object.key, config.DryRun); err != nil {
				result.failures++
				wrappedErr := fmt.Errorf("delete %s: %w", object.key, err)
				LogError("s3_delete_object", wrappedErr)
				cleanupErrors = append(cleanupErrors, wrappedErr)
			} else if config.DryRun {
				LogS3ObjectDeleted(object.key, object.age, true)
				result.wouldDelete++
			} else {
				LogS3ObjectDeleted(object.key, object.age, false)
				result.deleted++
			}
		}
	}

	duration := time.Since(start)
	LogS3CleanupSummary(result, duration, config.DryRun)
	return result, errors.Join(cleanupErrors...)
}

// listExpiredObjects lists all objects under a given prefix that are older than maxAgeSeconds.
// Returns a list of object keys that should be deleted.
func listExpiredObjects(ctx context.Context, client *s3.Client, bucket string, prefix string, maxAgeSeconds int64) ([]expiredS3Object, error) {
	var expiredObjects []expiredS3Object
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
				expiredObjects = append(expiredObjects, expiredS3Object{key: *obj.Key, age: objectAge(obj.LastModified)})
			}
		}
	}

	return expiredObjects, nil
}

// listExpiredObjectsWithFilter is like listExpiredObjects but also filters by suffix.
// Used for results/*.redacted.txt pattern matching.
func listExpiredObjectsWithFilter(ctx context.Context, client *s3.Client, bucket string, prefix string, maxAgeSeconds int64, suffix string) ([]expiredS3Object, error) {
	var expiredObjects []expiredS3Object
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
					expiredObjects = append(expiredObjects, expiredS3Object{key: *obj.Key, age: objectAge(obj.LastModified)})
				}
			}
		}
	}

	return expiredObjects, nil
}

// deleteObject deletes a single S3 object or logs what would be deleted if dryRun is true.
// Returns error only for actual failures (404 is considered success, object already gone).
func deleteObject(ctx context.Context, client *s3.Client, bucket, key string, dryRun bool) error {
	if dryRun {
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
