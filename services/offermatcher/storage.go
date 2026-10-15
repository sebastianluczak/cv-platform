package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"brodaty.dev/cv-platform/common/skillpatterns"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

const maxStoredMatchSize = 1 << 20

func writeMatchArtifact(ctx context.Context, client *s3.Client, bucket string, artifact matchArtifact) error {
	content, err := json.Marshal(artifact)
	if err != nil {
		return fmt.Errorf("encode offer match artifact: %w", err)
	}
	if len(content) > maxStoredMatchSize {
		return fmt.Errorf("offer match artifact exceeds %d bytes", maxStoredMatchSize)
	}
	key := "results/" + artifact.MatchID + ".offer.json"
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
		ContentType:   aws.String("application/json"),
		ContentLength: aws.Int64(int64(len(content))),
		Body:          bytes.NewReader(content),
	})
	if err != nil {
		return fmt.Errorf("store offer match artifact %q: %w", key, err)
	}
	return nil
}

type matchArtifactNotFound struct{}

func (matchArtifactNotFound) Error() string {
	return "offer match artifact not found"
}

func readMatchArtifact(ctx context.Context, client *s3.Client, bucket, matchID string) (matchArtifact, error) {
	key := "results/" + matchID + ".offer.json"
	object, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		var apiError smithy.APIError
		if errors.As(err, &apiError) && (apiError.ErrorCode() == "NoSuchKey" || apiError.ErrorCode() == "NotFound") {
			return matchArtifact{}, &matchArtifactNotFound{}
		}
		return matchArtifact{}, fmt.Errorf("get offer match artifact %q: %w", key, err)
	}
	defer object.Body.Close()
	content, err := io.ReadAll(io.LimitReader(object.Body, maxStoredMatchSize+1))
	if err != nil {
		return matchArtifact{}, fmt.Errorf("read offer match artifact %q: %w", key, err)
	}
	if len(content) > maxStoredMatchSize {
		return matchArtifact{}, fmt.Errorf("offer match artifact exceeds %d bytes", maxStoredMatchSize)
	}
	var artifact matchArtifact
	if err := json.Unmarshal(content, &artifact); err != nil {
		return matchArtifact{}, fmt.Errorf("decode offer match artifact %q: %w", key, err)
	}
	switch artifact.Status {
	case "queued", "processing", "completed", "unscorable", "failed":
	default:
		return matchArtifact{}, fmt.Errorf("offer match artifact has invalid status %q", artifact.Status)
	}
	return artifact, nil
}

func readCandidateSkills(ctx context.Context, client *s3.Client, bucket, resumeJobID string) ([]string, error) {
	key := "results/" + resumeJobID + ".skills.json"
	object, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		return nil, fmt.Errorf("get resume skill result %q: %w", key, err)
	}
	defer object.Body.Close()
	content, err := io.ReadAll(io.LimitReader(object.Body, maxStoredMatchSize+1))
	if err != nil {
		return nil, fmt.Errorf("read resume skill result %q: %w", key, err)
	}
	if len(content) > maxStoredMatchSize {
		return nil, fmt.Errorf("resume skill result exceeds %d bytes", maxStoredMatchSize)
	}
	return decodeCandidateSkills(content, resumeJobID)
}

func decodeCandidateSkills(content []byte, resumeJobID string) ([]string, error) {
	if len(content) > maxStoredMatchSize {
		return nil, fmt.Errorf("resume skill result exceeds %d bytes", maxStoredMatchSize)
	}
	var result skillpatterns.MatchResult
	if err := json.Unmarshal(content, &result); err != nil {
		return nil, fmt.Errorf("decode resume skill result for job %q: %w", resumeJobID, err)
	}
	if result.Version != 1 || result.JobID != resumeJobID || result.ScoredAt.IsZero() ||
		len(result.Categories) == 0 || len(result.Categories) > 64 {
		return nil, fmt.Errorf("resume skill result has invalid identity or shape")
	}
	candidateSet := make(map[string]struct{})
	var candidate []string
	categorySet := make(map[string]struct{}, len(result.Categories))
	for _, category := range result.Categories {
		name := strings.TrimSpace(category.Category)
		if name == "" || category.Score < 1 || category.Score > 5 || category.Evidence == nil {
			return nil, fmt.Errorf("resume skill result contains an invalid category")
		}
		if _, exists := categorySet[name]; exists {
			return nil, fmt.Errorf("resume skill result contains duplicate categories")
		}
		categorySet[name] = struct{}{}
		for _, evidence := range category.Evidence {
			evidence = strings.TrimSpace(evidence)
			if evidence == "" {
				return nil, fmt.Errorf("resume skill result contains empty evidence")
			}
			evidence = strings.ToLower(evidence)
			if _, exists := candidateSet[evidence]; !exists {
				candidateSet[evidence] = struct{}{}
				candidate = append(candidate, evidence)
			}
		}
	}
	return candidate, nil
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}
