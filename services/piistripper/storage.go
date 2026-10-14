package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const maxResultSize = 10 << 20

func readResult(ctx context.Context, client *s3.Client, bucket, key string) (string, error) {
	object, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", fmt.Errorf("get extraction result %q: %w", key, err)
	}
	defer object.Body.Close()

	content, err := io.ReadAll(io.LimitReader(object.Body, maxResultSize+1))
	if err != nil {
		return "", fmt.Errorf("read extraction result %q: %w", key, err)
	}
	if len(content) > maxResultSize {
		return "", fmt.Errorf("extraction result exceeds %d bytes", maxResultSize)
	}
	return string(content), nil
}

func writeRedactedResult(ctx context.Context, client *s3.Client, bucket, key, content string) error {
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		ContentType:   aws.String("text/plain; charset=utf-8"),
		ContentLength: aws.Int64(int64(len(content))),
		Body:          strings.NewReader(content),
	})
	if err != nil {
		return fmt.Errorf("store redacted result %q: %w", key, err)
	}
	return nil
}
