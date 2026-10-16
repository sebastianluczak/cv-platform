package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

const (
	maxResumeTextSize = 10 << 20
	maxArtifactSize   = 1 << 20
	maxRoleCount      = 5
	maxRoleTextSize   = 2000
)

type insightArtifactNotFound struct{}

func (insightArtifactNotFound) Error() string {
	return "artifact not found"
}

func writeInsightArtifact(ctx context.Context, client *s3.Client, bucket string, artifact insightArtifact) error {
	content, err := json.Marshal(artifact)
	if err != nil {
		return fmt.Errorf("encode role insights artifact: %w", err)
	}
	if len(content) > maxArtifactSize {
		return fmt.Errorf("role insights artifact exceeds %d bytes", maxArtifactSize)
	}
	key := "results/" + artifact.InsightID + ".insights.json"
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
		ContentType:   aws.String("application/json"),
		ContentLength: aws.Int64(int64(len(content))),
		Body:          bytes.NewReader(content),
	})
	if err != nil {
		return fmt.Errorf("store role insights artifact %q: %w", key, err)
	}
	return nil
}

func readInsightArtifact(ctx context.Context, client *s3.Client, bucket, insightID string) (insightArtifact, error) {
	key := "results/" + insightID + ".insights.json"
	content, err := readObject(ctx, client, bucket, key, maxArtifactSize)
	if err != nil {
		return insightArtifact{}, err
	}
	var artifact insightArtifact
	if err := json.Unmarshal(content, &artifact); err != nil {
		return insightArtifact{}, fmt.Errorf("decode role insights artifact %q: %w", key, err)
	}
	return artifact, nil
}

func readRedactedResume(ctx context.Context, client *s3.Client, bucket, resumeJobID string) (string, error) {
	key := "results/" + resumeJobID + ".redacted.txt"
	content, err := readObject(ctx, client, bucket, key, maxResumeTextSize)
	if err != nil {
		return "", fmt.Errorf("read redacted resume: %w", err)
	}
	if !utf8.Valid(content) {
		return "", fmt.Errorf("redacted resume is not valid UTF-8")
	}
	return string(content), nil
}

func readObject(ctx context.Context, client *s3.Client, bucket, key string, maxSize int64) ([]byte, error) {
	object, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var apiError smithy.APIError
		if errors.As(err, &apiError) && (apiError.ErrorCode() == "NoSuchKey" || apiError.ErrorCode() == "NotFound") {
			return nil, &insightArtifactNotFound{}
		}
		return nil, fmt.Errorf("get object %q: %w", key, err)
	}
	defer object.Body.Close()
	content, err := io.ReadAll(io.LimitReader(object.Body, maxSize+1))
	if err != nil {
		return nil, fmt.Errorf("read object %q: %w", key, err)
	}
	if int64(len(content)) > maxSize {
		return nil, fmt.Errorf("object %q exceeds %d bytes", key, maxSize)
	}
	return content, nil
}

func validateInsightArtifact(artifact insightArtifact, insightID string) error {
	if artifact.Version != 1 || artifact.InsightID != insightID ||
		!idPattern.MatchString(artifact.ResumeJobID) || !artifact.ConsentGiven ||
		artifact.ConsentedAt.IsZero() || artifact.CreatedAt.IsZero() || artifact.UpdatedAt.IsZero() {
		return fmt.Errorf("role insights artifact has invalid identity or consent")
	}
	switch artifact.Status {
	case "queued", "processing", "completed", "failed":
	default:
		return fmt.Errorf("role insights artifact has invalid status %q", artifact.Status)
	}
	if artifact.Status != "completed" {
		if len(artifact.Roles) != 0 {
			return fmt.Errorf("non-completed role insights artifact contains suggestions")
		}
		return nil
	}
	return validateRoleSuggestions(artifact.Roles)
}

func validateRoleSuggestions(roles []roleSuggestion) error {
	if len(roles) == 0 || len(roles) > maxRoleCount {
		return fmt.Errorf("completed role insights artifact has invalid number of suggestions")
	}
	seen := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		title := strings.TrimSpace(role.Title)
		summary := strings.TrimSpace(role.FitSummary)
		if title == "" || len(title) > maxRoleTextSize || summary == "" ||
			len(summary) > maxRoleTextSize || !utf8.ValidString(title) || !utf8.ValidString(summary) ||
			len(role.Responsibilities) == 0 || len(role.Responsibilities) > 5 {
			return fmt.Errorf("completed role insights artifact contains an invalid suggestion")
		}
		key := strings.ToLower(title)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("completed role insights artifact contains duplicate titles")
		}
		seen[key] = struct{}{}
		for _, responsibility := range role.Responsibilities {
			if text := strings.TrimSpace(responsibility); text == "" ||
				len(text) > maxRoleTextSize || !utf8.ValidString(text) {
				return fmt.Errorf("completed role insights artifact contains an invalid responsibility")
			}
		}
	}
	return nil
}

func updateInsightStatus(artifact *insightArtifact, status, message string) {
	artifact.Status = status
	artifact.Message = message
	artifact.UpdatedAt = time.Now().UTC()
}
