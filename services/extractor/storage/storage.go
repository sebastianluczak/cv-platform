package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Config struct {
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Endpoint        string
	Bucket          string
}

func NewClient() (*s3.Client, Config) {
	cfg := Config{
		Region:          envOrDefault("RUSTFS_REGION", "us-east-1"),
		AccessKeyID:     envOrDefault("RUSTFS_ACCESS_KEY_ID", "rustfsadmin"),
		SecretAccessKey: envOrDefault("RUSTFS_SECRET_ACCESS_KEY", "rustfsadmin"),
		Endpoint:        envOrDefault("RUSTFS_ENDPOINT_URL", "http://localhost:9000"),
		Bucket:          envOrDefault("RUSTFS_BUCKET", "cv-platform-uploads"),
	}

	client := s3.NewFromConfig(aws.Config{
		Region:      cfg.Region,
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")),
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(cfg.Endpoint)
		options.UsePathStyle = true
	})
	return client, cfg
}

func Download(ctx context.Context, client *s3.Client, bucket, key string) (string, func(), error) {
	object, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", nil, fmt.Errorf("get object %q: %w", key, err)
	}
	defer object.Body.Close()

	file, err := os.CreateTemp("", "cv-extractor-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary input file: %w", err)
	}
	cleanup := func() {
		_ = os.Remove(file.Name())
	}
	if _, err := io.Copy(file, object.Body); err != nil {
		closeErr := file.Close()
		cleanup()
		if closeErr != nil {
			return "", nil, errors.Join(
				fmt.Errorf("download object %q: %w", key, err),
				fmt.Errorf("close temporary input file: %w", closeErr),
			)
		}
		return "", nil, fmt.Errorf("download object %q: %w", key, err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close temporary input file: %w", err)
	}
	return file.Name(), cleanup, nil
}

func Upload(ctx context.Context, client *s3.Client, bucket, key, contentType string, contentLength int64, body io.Reader) error {
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(contentLength),
		Body:          body,
	})
	if err != nil {
		return fmt.Errorf("put object %q: %w", key, err)
	}
	return nil
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
