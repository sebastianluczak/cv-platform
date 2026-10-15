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

	"brodaty.dev/cv-platform/common/skillpatterns"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

const (
	maxResumeTextSize  = 10 << 20
	maxOfferTextSize   = 100 << 10
	maxArtifactSize    = 1 << 20
	maxMarkdownSize    = 256 << 10
	maxSkillCategories = 64
)

type artifactNotFound struct{}

func (artifactNotFound) Error() string {
	return "artifact not found"
}

type offerMatchArtifact struct {
	Version     int    `json:"version"`
	MatchID     string `json:"match_id"`
	ResumeJobID string `json:"resume_job_id"`
	Status      string `json:"status"`
	OfferText   string `json:"offer_text"`
}

func writeWeighArtifact(ctx context.Context, client *s3.Client, bucket string, artifact weighArtifact) error {
	content, err := json.Marshal(artifact)
	if err != nil {
		return fmt.Errorf("encode AI resume artifact: %w", err)
	}
	if len(content) > maxArtifactSize {
		return fmt.Errorf("AI resume artifact exceeds %d bytes", maxArtifactSize)
	}
	key := "results/" + artifact.WeighID + ".ai.json"
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
		ContentType:   aws.String("application/json"),
		ContentLength: aws.Int64(int64(len(content))),
		Body:          bytes.NewReader(content),
	})
	if err != nil {
		return fmt.Errorf("store AI resume artifact %q: %w", key, err)
	}
	return nil
}

func readWeighArtifact(ctx context.Context, client *s3.Client, bucket, weighID string) (weighArtifact, error) {
	key := "results/" + weighID + ".ai.json"
	content, err := readObject(ctx, client, bucket, key, maxArtifactSize)
	if err != nil {
		return weighArtifact{}, err
	}
	var artifact weighArtifact
	if err := json.Unmarshal(content, &artifact); err != nil {
		return weighArtifact{}, fmt.Errorf("decode AI resume artifact %q: %w", key, err)
	}
	switch artifact.Status {
	case "queued", "processing", "completed", "failed":
	default:
		return weighArtifact{}, fmt.Errorf("AI resume artifact has invalid status %q", artifact.Status)
	}
	if artifact.Version != 1 || !idPattern.MatchString(artifact.WeighID) ||
		!idPattern.MatchString(artifact.MatchID) || !idPattern.MatchString(artifact.ResumeJobID) {
		return weighArtifact{}, fmt.Errorf("AI resume artifact has invalid identity")
	}
	return artifact, nil
}

func readOfferMatch(ctx context.Context, client *s3.Client, bucket, matchID string) (offerMatchArtifact, error) {
	key := "results/" + matchID + ".offer.json"
	content, err := readObject(ctx, client, bucket, key, maxArtifactSize)
	if err != nil {
		return offerMatchArtifact{}, err
	}
	var artifact offerMatchArtifact
	if err := json.Unmarshal(content, &artifact); err != nil {
		return offerMatchArtifact{}, fmt.Errorf("decode offer match artifact %q: %w", key, err)
	}
	if artifact.Version != 1 || artifact.MatchID != matchID ||
		!idPattern.MatchString(artifact.ResumeJobID) ||
		(artifact.Status != "completed" && artifact.Status != "queued" && artifact.Status != "processing" &&
			artifact.Status != "unscorable" && artifact.Status != "failed") {
		return offerMatchArtifact{}, fmt.Errorf("offer match artifact has invalid identity or status")
	}
	if len(artifact.OfferText) > maxOfferTextSize || !utf8.ValidString(artifact.OfferText) ||
		(artifact.Status == "completed" && strings.TrimSpace(artifact.OfferText) == "") {
		return offerMatchArtifact{}, fmt.Errorf("offer match artifact contains invalid or oversized offer text")
	}
	return artifact, nil
}

func loadResumeInputs(
	ctx context.Context,
	client *s3.Client,
	bucket string,
	match offerMatchArtifact,
) (string, string, error) {
	if match.Status != "completed" || strings.TrimSpace(match.OfferText) == "" {
		return "", "", fmt.Errorf("offer match is not complete or has no retained offer text")
	}
	resumeKey := "results/" + match.ResumeJobID + ".redacted.txt"
	resumeBytes, err := readObject(ctx, client, bucket, resumeKey, maxResumeTextSize)
	if err != nil {
		return "", "", fmt.Errorf("read redacted resume: %w", err)
	}
	if !utf8.Valid(resumeBytes) || strings.TrimSpace(string(resumeBytes)) == "" {
		return "", "", fmt.Errorf("redacted resume is empty or not valid UTF-8")
	}

	skillsKey := "results/" + match.ResumeJobID + ".skills.json"
	skillsBytes, err := readObject(ctx, client, bucket, skillsKey, maxArtifactSize)
	if err != nil {
		return "", "", fmt.Errorf("read resume skill result: %w", err)
	}
	var skills skillpatterns.MatchResult
	if err := json.Unmarshal(skillsBytes, &skills); err != nil {
		return "", "", fmt.Errorf("decode resume skill result: %w", err)
	}
	if skills.Version != 1 || skills.JobID != match.ResumeJobID || skills.ScoredAt.IsZero() ||
		len(skills.Categories) == 0 || len(skills.Categories) > maxSkillCategories {
		return "", "", fmt.Errorf("resume skill result has invalid identity or shape")
	}
	categories := make(map[string]struct{}, len(skills.Categories))
	for _, category := range skills.Categories {
		name := strings.TrimSpace(category.Category)
		if name == "" || category.Score < 1 || category.Score > 5 || category.Evidence == nil {
			return "", "", fmt.Errorf("resume skill result contains an invalid category")
		}
		if _, exists := categories[name]; exists {
			return "", "", fmt.Errorf("resume skill result contains duplicate categories")
		}
		categories[name] = struct{}{}
		for _, evidence := range category.Evidence {
			if strings.TrimSpace(evidence) == "" {
				return "", "", fmt.Errorf("resume skill result contains empty evidence")
			}
		}
	}
	return string(resumeBytes), string(skillsBytes), nil
}

func readObject(ctx context.Context, client *s3.Client, bucket, key string, maxSize int64) ([]byte, error) {
	object, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var apiError smithy.APIError
		if errors.As(err, &apiError) && (apiError.ErrorCode() == "NoSuchKey" || apiError.ErrorCode() == "NotFound") {
			return nil, &artifactNotFound{}
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

func validateWeighArtifact(artifact weighArtifact, weighID string) error {
	if artifact.Version != 1 || artifact.WeighID != weighID || !idPattern.MatchString(artifact.MatchID) ||
		!idPattern.MatchString(artifact.ResumeJobID) || !artifact.ConsentGiven || artifact.ConsentedAt.IsZero() ||
		artifact.CreatedAt.IsZero() || artifact.UpdatedAt.IsZero() {
		return fmt.Errorf("AI resume artifact has invalid identity or shape")
	}
	if artifact.Status == "completed" && (strings.TrimSpace(artifact.Markdown) == "" ||
		len(artifact.Markdown) > maxMarkdownSize || !utf8.ValidString(artifact.Markdown)) {
		return fmt.Errorf("completed AI resume artifact has invalid Markdown")
	}
	if artifact.Status != "completed" && artifact.Markdown != "" {
		return fmt.Errorf("non-completed AI resume artifact contains Markdown")
	}
	return nil
}

func updateArtifactStatus(artifact *weighArtifact, status, message string) {
	artifact.Status = status
	artifact.Message = message
	artifact.UpdatedAt = time.Now().UTC()
}
