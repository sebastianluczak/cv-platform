package main

import (
	"bytes"
	"context"
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

const maxAPIBodySize = 128 << 10

var matchIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type matchRequest struct {
	ResumeJobID string `json:"resume_job_id"`
	OfferText   string `json:"offer_text,omitempty"`
	OfferURL    string `json:"offer_url,omitempty"`
}

type offerJob struct {
	MatchID     string `json:"match_id"`
	ResumeJobID string `json:"resume_job_id"`
	OfferText   string `json:"offer_text,omitempty"`
	OfferURL    string `json:"offer_url,omitempty"`
}

type matchArtifact struct {
	Version       int       `json:"version"`
	MatchID       string    `json:"match_id"`
	ResumeJobID   string    `json:"resume_job_id"`
	Status        string    `json:"status"`
	OfferSource   string    `json:"offer_source"`
	OfferText     string    `json:"offer_text,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Message       string    `json:"message,omitempty"`
	Percent       *int      `json:"percent,omitempty"`
	RequiredCount int       `json:"required_count,omitempty"`
	MatchedCount  int       `json:"matched_count,omitempty"`
	MatchedSkills []string  `json:"matched_skills,omitempty"`
	MissingSkills []string  `json:"missing_skills,omitempty"`
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

func newMatchID() (string, error) {
	id, err := newUUID()
	if err != nil {
		return "", fmt.Errorf("generate offer match ID: %w", err)
	}
	return id, nil
}

func newOfferMatcherHandler(
	storageClient *s3.Client,
	bucket, workQueue string,
	publisher *confirmedPublisher,
) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /matches", func(w http.ResponseWriter, r *http.Request) {
		createMatchHandler(w, r, storageClient, bucket, workQueue, publisher)
	})
	mux.HandleFunc("GET /matches/{matchID}", func(w http.ResponseWriter, r *http.Request) {
		getMatchHandler(w, r, storageClient, bucket, r.PathValue("matchID"))
	})
	return mux
}

func createMatchHandler(
	w http.ResponseWriter,
	r *http.Request,
	storageClient *s3.Client,
	bucket, workQueue string,
	publisher *confirmedPublisher,
) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAPIBodySize)
	content, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(content) {
		writeAPIError(w, http.StatusBadRequest, "request body must be valid UTF-8 and within the size limit")
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var request matchRequest
	if err := decoder.Decode(&request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeAPIError(w, http.StatusBadRequest, "request body must contain exactly one JSON object")
		return
	}
	if !matchIDPattern.MatchString(request.ResumeJobID) {
		writeAPIError(w, http.StatusBadRequest, "resume_job_id must be a UUID")
		return
	}

	request.OfferText = strings.TrimSpace(request.OfferText)
	request.OfferURL = strings.TrimSpace(request.OfferURL)
	hasText := request.OfferText != ""
	hasURL := request.OfferURL != ""
	if hasText == hasURL {
		writeAPIError(w, http.StatusBadRequest, "provide exactly one of offer_text or offer_url")
		return
	}
	source := "text"
	if hasText {
		if len(request.OfferText) > maxOfferTextBytes || !utf8.ValidString(request.OfferText) {
			writeAPIError(w, http.StatusBadRequest, "offer_text must be valid UTF-8 and no larger than 100 KiB")
			return
		}
	} else {
		source = "url"
		if len(request.OfferURL) > maxOfferURLLength || !validPublicURLForInput(request.OfferURL) {
			writeAPIError(w, http.StatusBadRequest, "offer_url must be a public HTTP or HTTPS URL on the default port")
			return
		}
	}

	matchID, err := newMatchID()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not create offer match")
		return
	}
	now := time.Now().UTC()
	artifact := matchArtifact{
		Version: 1, MatchID: matchID, ResumeJobID: request.ResumeJobID,
		Status: "queued", OfferSource: source, CreatedAt: now, UpdatedAt: now,
		Message: "Offer comparison is queued.",
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := writeMatchArtifact(ctx, storageClient, bucket, artifact); err != nil {
		log.Printf("Store queued offer match %s: %v", matchID, err)
		writeAPIError(w, http.StatusBadGateway, "could not store offer match")
		return
	}
	job := offerJob{
		MatchID: matchID, ResumeJobID: request.ResumeJobID,
		OfferText: request.OfferText, OfferURL: request.OfferURL,
	}
	body, err := json.Marshal(job)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not encode offer match")
		return
	}
	if err := publisher.publish(workQueue, body, nil); err != nil {
		log.Printf("Publish offer match %s: %v", matchID, err)
		writeAPIError(w, http.StatusServiceUnavailable, "offer matching service is unavailable")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"match_id": matchID, "status": "queued"})
}

func getMatchHandler(w http.ResponseWriter, r *http.Request, storageClient *s3.Client, bucket, matchID string) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !matchIDPattern.MatchString(matchID) {
		writeAPIError(w, http.StatusBadRequest, "match ID must be a UUID")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	artifact, err := readMatchArtifact(ctx, storageClient, bucket, matchID)
	if err != nil {
		var notFound *matchArtifactNotFound
		if errors.As(err, &notFound) {
			writeAPIError(w, http.StatusNotFound, "offer match not found")
			return
		}
		log.Printf("Read offer match %s: %v", matchID, err)
		writeAPIError(w, http.StatusBadGateway, "could not retrieve offer match")
		return
	}
	if artifact.MatchID != matchID || artifact.Version != 1 {
		writeAPIError(w, http.StatusBadGateway, "invalid offer match stored")
		return
	}
	writeJSON(w, http.StatusOK, publicMatchArtifact(artifact))
}

func publicMatchArtifact(artifact matchArtifact) matchArtifact {
	artifact.OfferText = ""
	return artifact
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
