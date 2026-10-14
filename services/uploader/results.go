package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

const maxMatchResultSize = 1 << 20

var resultJobIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var matchResultCategories = []string{
	"backend",
	"devops",
	"management",
	"architecture",
	"qa",
	"frontend",
	"data",
	"security",
	"product",
	"mobile",
	"embedded",
	"platform",
	"support",
	"business",
}
var legacyMatchResultCategories = matchResultCategories[:8]

type storedMatchResult struct {
	Version    int       `json:"version"`
	JobID      string    `json:"job_id"`
	ScoredAt   time.Time `json:"scored_at"`
	Categories []struct {
		Category string   `json:"category"`
		Score    int      `json:"score"`
		Evidence []string `json:"evidence"`
	} `json:"categories"`
}

func resultHandler(storageClient *s3.Client, bucket string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		jobID := r.URL.Query().Get("job_id")
		if !resultJobIDPattern.MatchString(jobID) {
			http.Error(w, "Invalid job ID", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		object, err := storageClient.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String("results/" + jobID + ".skills.json"),
		})
		if err != nil {
			var apiError smithy.APIError
			if errors.As(err, &apiError) && (apiError.ErrorCode() == "NoSuchKey" || apiError.ErrorCode() == "NotFound") {
				http.Error(w, "Match result not found", http.StatusNotFound)
				return
			}
			log.Printf("Read match result for job %s: %v", jobID, err)
			http.Error(w, "Could not retrieve match result", http.StatusBadGateway)
			return
		}
		defer object.Body.Close()

		content, err := io.ReadAll(io.LimitReader(object.Body, maxMatchResultSize+1))
		if err != nil {
			log.Printf("Read match result body for job %s: %v", jobID, err)
			http.Error(w, "Could not retrieve match result", http.StatusBadGateway)
			return
		}
		if len(content) > maxMatchResultSize || !validMatchResult(content, jobID) {
			http.Error(w, "Invalid match result stored", http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if _, err := w.Write(content); err != nil {
			log.Printf("Write match result for job %s: %v", jobID, err)
		}
	}
}

func validMatchResult(content []byte, jobID string) bool {
	var result storedMatchResult
	if err := json.Unmarshal(content, &result); err != nil {
		return false
	}
	categories := matchResultCategories
	if len(result.Categories) == len(legacyMatchResultCategories) {
		categories = legacyMatchResultCategories
	}
	if result.Version != 1 || result.JobID != jobID || result.ScoredAt.IsZero() || len(result.Categories) != len(categories) {
		return false
	}
	for index, category := range result.Categories {
		if category.Category != categories[index] ||
			category.Score < 1 || category.Score > 5 || category.Evidence == nil {
			return false
		}
		for _, evidence := range category.Evidence {
			if evidence == "" {
				return false
			}
		}
	}
	return true
}
