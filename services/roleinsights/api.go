package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"brodaty.dev/cv-platform/common/rabbitmq"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/streadway/amqp"
)

const maxAPIBodySize = 4 << 10

var idPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type insightRequest struct {
	ResumeJobID string `json:"resume_job_id"`
	Consent     bool   `json:"consent"`
}

type insightJob struct {
	InsightID   string `json:"insight_id"`
	ResumeJobID string `json:"resume_job_id"`
}

type roleSuggestion struct {
	Title            string   `json:"title"`
	FitSummary       string   `json:"fit_summary"`
	Responsibilities []string `json:"responsibilities"`
}

type insightArtifact struct {
	Version      int              `json:"version"`
	InsightID    string           `json:"insight_id"`
	ResumeJobID  string           `json:"resume_job_id"`
	Status       string           `json:"status"`
	ConsentGiven bool             `json:"consent_given"`
	ConsentedAt  time.Time        `json:"consented_at"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
	Message      string           `json:"message"`
	Roles        []roleSuggestion `json:"roles,omitempty"`
}

type confirmedPublisher struct {
	mu            sync.Mutex
	channel       *amqp.Channel
	confirmations <-chan amqp.Confirmation
}

func (publisher *confirmedPublisher) publish(queue string, body []byte, headers amqp.Table) error {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	return rabbitmq.PublishAndConfirm(
		publisher.channel, publisher.confirmations, queue, body, headers, rabbitmq.ConfirmTimeout,
	)
}

func newRoleInsightsHandler(
	storageClient *s3.Client,
	bucket, workQueue string,
	publisher *confirmedPublisher,
) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /insights", func(w http.ResponseWriter, r *http.Request) {
		createInsightHandler(w, r, storageClient, bucket, workQueue, publisher)
	})
	mux.HandleFunc("GET /insights/{insightID}", func(w http.ResponseWriter, r *http.Request) {
		getInsightHandler(w, r, storageClient, bucket, r.PathValue("insightID"))
	})
	return mux
}

func createInsightHandler(
	w http.ResponseWriter,
	r *http.Request,
	storageClient *s3.Client,
	bucket, workQueue string,
	publisher *confirmedPublisher,
) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAPIBodySize)
	content, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(content) {
		writeAPIError(w, http.StatusBadRequest, "request body must be valid UTF-8 and within the size limit")
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var request insightRequest
	if err := decoder.Decode(&request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeAPIError(w, http.StatusBadRequest, "request body must contain exactly one JSON object")
		return
	}
	if !idPattern.MatchString(request.ResumeJobID) {
		writeAPIError(w, http.StatusBadRequest, "resume_job_id must be a UUID")
		return
	}
	if !request.Consent {
		writeAPIError(w, http.StatusBadRequest, "consent to external AI processing is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	resumeText, err := readRedactedResume(ctx, storageClient, bucket, request.ResumeJobID)
	if err != nil {
		var notFound *insightArtifactNotFound
		if errors.As(err, &notFound) {
			writeAPIError(w, http.StatusNotFound, "redacted resume not found")
			return
		}
		log.Printf("Read redacted resume for role insights: %s", safeLogField(err.Error()))
		writeAPIError(w, http.StatusBadGateway, "could not retrieve redacted resume")
		return
	}
	if strings.TrimSpace(resumeText) == "" {
		writeAPIError(w, http.StatusConflict, "redacted resume is empty")
		return
	}

	insightID, err := newUUID()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not create role insights request")
		return
	}
	now := time.Now().UTC()
	artifact := insightArtifact{
		Version: 1, InsightID: insightID, ResumeJobID: request.ResumeJobID,
		Status: "queued", CreatedAt: now, UpdatedAt: now,
		ConsentGiven: true, ConsentedAt: now,
		Message: "Role insights are queued.",
	}
	if err := writeInsightArtifact(ctx, storageClient, bucket, artifact); err != nil {
		log.Printf("Store queued role insights %s: %s", safeLogField(insightID), safeLogField(err.Error()))
		writeAPIError(w, http.StatusBadGateway, "could not create role insights request")
		return
	}
	body, err := json.Marshal(insightJob{InsightID: insightID, ResumeJobID: request.ResumeJobID})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not encode role insights request")
		return
	}
	if err := publisher.publish(workQueue, body, nil); err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "role insights service is unavailable")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"insight_id": insightID, "status": "queued"})
}

func getInsightHandler(w http.ResponseWriter, r *http.Request, storageClient *s3.Client, bucket, insightID string) {
	if !idPattern.MatchString(insightID) {
		writeAPIError(w, http.StatusBadRequest, "insight ID must be a UUID")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	artifact, err := readInsightArtifact(ctx, storageClient, bucket, insightID)
	if err != nil {
		var notFound *insightArtifactNotFound
		if errors.As(err, &notFound) {
			writeAPIError(w, http.StatusNotFound, "role insights not found")
			return
		}
		writeAPIError(w, http.StatusBadGateway, "could not retrieve role insights")
		return
	}
	if err := validateInsightArtifact(artifact, insightID); err != nil {
		writeAPIError(w, http.StatusBadGateway, "invalid role insights stored")
		return
	}
	writeJSON(w, http.StatusOK, artifact)
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("Write HTTP JSON response: %v", err)
	}
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate UUID: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func safeLogField(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.ReplaceAll(value, "\n", " ")
}
