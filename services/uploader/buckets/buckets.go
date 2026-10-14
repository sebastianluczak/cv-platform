package buckets

import (
	"context"
	"errors"
	"fmt"
	"io"

	"brodaty.dev/cv-platform/uploader/buckets/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func NewClient() (*s3.Client, config.BucketConfig) {
	bucketConfig := config.GetBucketConfig()

	cfg := aws.Config{
		Region:      bucketConfig.Region,
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(bucketConfig.AccessKeyID, bucketConfig.SecretAccessKey, "")),
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(bucketConfig.Endpoint)
		o.UsePathStyle = true
	})
	return client, bucketConfig
}

func EnsureBucket(ctx context.Context, client *s3.Client, bucket string) error {
	resp, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return fmt.Errorf("list buckets: %w", err)
	}
	for _, b := range resp.Buckets {
		if aws.ToString(b.Name) == bucket {
			return nil
		}
	}

	_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	var alreadyOwned *types.BucketAlreadyOwnedByYou
	if err != nil && !errors.As(err, &alreadyOwned) {
		return fmt.Errorf("create bucket %q: %w", bucket, err)
	}
	return nil
}

func PutObject(ctx context.Context, client *s3.Client, bucket, key, contentType string, contentLength int64, body io.Reader) error {
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(contentLength),
	})
	if err != nil {
		return fmt.Errorf("put object %q: %w", key, err)
	}
	return nil
}
