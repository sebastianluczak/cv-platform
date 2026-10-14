package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	maxResultSize = 10 << 20
	maxOutputSize = 1 << 20
)

func readRedactedResult(ctx context.Context, client *s3.Client, bucket, key string) (string, error) {
	object, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", fmt.Errorf("get redacted result %q: %w", key, err)
	}
	defer object.Body.Close()

	content, err := io.ReadAll(io.LimitReader(object.Body, maxResultSize+1))
	if err != nil {
		return "", fmt.Errorf("read redacted result %q: %w", key, err)
	}
	if len(content) > maxResultSize {
		return "", fmt.Errorf("redacted result exceeds %d bytes", maxResultSize)
	}
	return string(content), nil
}

func writeMatchResult(ctx context.Context, client *s3.Client, bucket, key string, result MatchResult) error {
	content, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode match result: %w", err)
	}
	if len(content) > maxOutputSize {
		return fmt.Errorf("match result exceeds %d bytes", maxOutputSize)
	}
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		ContentType:   aws.String("application/json"),
		ContentLength: aws.Int64(int64(len(content))),
		Body:          bytes.NewReader(content),
	})
	if err != nil {
		return fmt.Errorf("store match result %q: %w", key, err)
	}
	return nil
}
