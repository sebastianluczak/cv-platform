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

type weighRequest struct {
	MatchID string `json:"match_id"`
	Consent bool   `json:"consent"`
}

type weighJob struct {
	WeighID string `json:"weigh_id"`
	MatchID string `json:"match_id"`
}

type weighArtifact struct {
	Version      int       `json:"version"`
	WeighID      string    `json:"weigh_id"`
	MatchID      string    `json:"match_id"`
	ResumeJobID  string    `json:"resume_job_id"`
	Status       string    `json:"status"`
	ConsentGiven bool      `json:"consent_given"`
	ConsentedAt  time.Time `json:"consented_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Message      string    `json:"message"`
	Markdown     string    `json:"markdown,omitempty"`
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

func newAIweighterHandler(
	storageClient *s3.Client,
	bucket, workQueue string,
	publisher *confirmedPublisher,
) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /weights", func(w http.ResponseWriter, r *http.Request) {
		createWeightHandler(w, r, storageClient, bucket, workQueue, publisher)
	})
	mux.HandleFunc("GET /weights/{weighID}", func(w http.ResponseWriter, r *http.Request) {
		getWeightHandler(w, r, storageClient, bucket, r.PathValue("weighID"))
	})
	return mux
}

func createWeightHandler(
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
	var request weighRequest
	if err := decoder.Decode(&request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeAPIError(w, http.StatusBadRequest, "request body must contain exactly one JSON object")
		return
	}
	if !idPattern.MatchString(request.MatchID) {
		writeAPIError(w, http.StatusBadRequest, "match_id must be a UUID")
		return
	}
	if !request.Consent {
		writeAPIError(w, http.StatusBadRequest, "consent to external AI processing is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	match, err := readOfferMatch(ctx, storageClient, bucket, request.MatchID)
	if err != nil {
		var notFound *artifactNotFound
		if errors.As(err, &notFound) {
			writeAPIError(w, http.StatusNotFound, "offer match not found")
			return
		}
		log.Printf("Read offer match %s for AIweighter: %v", request.MatchID, err)
		writeAPIError(w, http.StatusBadGateway, "could not retrieve offer match")
		return
	}
	if match.Status != "completed" || strings.TrimSpace(match.OfferText) == "" {
		writeAPIError(w, http.StatusConflict, "a completed offer match with retained offer text is required")
		return
	}

	weighID, err := newUUID()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not create AI resume request")
		return
	}
	now := time.Now().UTC()
	artifact := weighArtifact{
		Version: 1, WeighID: weighID, MatchID: request.MatchID,
		ResumeJobID: match.ResumeJobID, Status: "queued", CreatedAt: now, UpdatedAt: now,
		ConsentGiven: true, ConsentedAt: now,
		Message: "AI resume generation is queued.",
	}
	if err := writeWeighArtifact(ctx, storageClient, bucket, artifact); err != nil {
		log.Printf("Store queued AI resume %s: %v", weighID, err)
		writeAPIError(w, http.StatusBadGateway, "could not create AI resume request")
		return
	}
	body, err := json.Marshal(weighJob{WeighID: weighID, MatchID: request.MatchID})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not encode AI resume request")
		return
	}
	if err := publisher.publish(workQueue, body, nil); err != nil {
		log.Printf("Publish AI resume %s: %v", weighID, err)
		writeAPIError(w, http.StatusServiceUnavailable, "AIweighter is unavailable")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"weigh_id": weighID, "status": "queued"})
}

func getWeightHandler(w http.ResponseWriter, r *http.Request, storageClient *s3.Client, bucket, weighID string) {
	if !idPattern.MatchString(weighID) {
		writeAPIError(w, http.StatusBadRequest, "weigh ID must be a UUID")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	artifact, err := readWeighArtifact(ctx, storageClient, bucket, weighID)
	if err != nil {
		var notFound *artifactNotFound
		if errors.As(err, &notFound) {
			writeAPIError(w, http.StatusNotFound, "AI resume not found")
			return
		}
		// #nosec G706 -- Dynamic fields are CR/LF-sanitized by safeLogField.
		log.Printf("Read AI resume %s: %s", safeLogField(weighID), safeLogField(err.Error()))
		writeAPIError(w, http.StatusBadGateway, "could not retrieve AI resume")
		return
	}
	if artifact.WeighID != weighID || artifact.Version != 1 {
		writeAPIError(w, http.StatusBadGateway, "invalid AI resume stored")
		return
	}
	if err := validateWeighArtifact(artifact, weighID); err != nil {
		writeAPIError(w, http.StatusBadGateway, "invalid AI resume stored")
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
