package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"brodaty.dev/cv-platform/common/prometheus"
	"brodaty.dev/cv-platform/uploader/buckets"
	"brodaty.dev/cv-platform/uploader/progress"
	"brodaty.dev/cv-platform/uploader/queue"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/streadway/amqp"
)

const (
	maxUploadSize  = 10 << 20 // 10 MiB
	maxRequestSize = maxUploadSize + (64 << 10)
)

func uploadHandler(storageClient *s3.Client, bucket string, publisher *queue.Publisher, statusStore *progress.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
		file, fileHeader, err := r.FormFile("file")
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, "File exceeds the 10 MiB upload limit", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "Invalid upload", http.StatusBadRequest)
			return
		}
		defer file.Close()
		if fileHeader.Size > maxUploadSize {
			http.Error(w, "File exceeds the 10 MiB upload limit", http.StatusRequestEntityTooLarge)
			return
		}

		header, err := io.ReadAll(io.LimitReader(file, 512))
		if err != nil {
			http.Error(w, "Could not read uploaded file", http.StatusBadRequest)
			return
		}
		contentType := http.DetectContentType(header)
		if !supportedContentType(contentType) {
			http.Error(w, "Only PDF, PNG, JPEG, TIFF, and BMP files are supported", http.StatusUnsupportedMediaType)
			return
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			log.Printf("Rewind uploaded file: %v", err)
			http.Error(w, "Could not process uploaded file", http.StatusInternalServerError)
			return
		}

		jobID, err := newJobID()
		if err != nil {
			log.Printf("Generate upload ID: %v", err)
			http.Error(w, "Could not process upload", http.StatusInternalServerError)
			return
		}
		objectKey := "uploads/" + jobID
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		if err := buckets.PutObject(ctx, storageClient, bucket, objectKey, contentType, fileHeader.Size, file); err != nil {
			log.Printf("Store upload %s: %v", jobID, err)
			http.Error(w, "Could not store uploaded file", http.StatusInternalServerError)
			return
		}
		job := queue.ExtractionJob{
			ID:          jobID,
			Bucket:      bucket,
			ObjectKey:   objectKey,
			ContentType: contentType,
		}
		if err := publisher.PublishMessage(job); err != nil {
			log.Printf("Publish for stored object %s/%s was not confirmed: %v", bucket, objectKey, err)
			http.Error(w, "Could not queue file for extraction", http.StatusServiceUnavailable)
			return
		}
		statusStore.Update(queue.StatusEvent{
			JobID:   jobID,
			Stage:   "queued",
			Message: "Your resume is in the processing queue.",
		})

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		if err := json.NewEncoder(w).Encode(map[string]string{
			"job_id": jobID,
			"status": "queued",
		}); err != nil {
			log.Printf("Write upload response for %s: %v", jobID, err)
		}
	}
}

func consumeStatusEvents(deliveries <-chan amqp.Delivery, store *progress.Store) {
	for delivery := range deliveries {
		var event queue.StatusEvent
		if err := json.Unmarshal(delivery.Body, &event); err != nil {
			log.Printf("Decode processing status event: %v", err)
		} else if !store.Update(event) {
			log.Printf("Ignore invalid or stale processing status for job %q at stage %q", event.JobID, event.Stage)
		}
		if err := delivery.Ack(false); err != nil {
			log.Printf("Acknowledge processing status event: %v", err)
		}
	}
	log.Printf("RabbitMQ status event stream closed")
}

func progressHandler(store *progress.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		jobID := r.URL.Query().Get("job_id")
		snapshot, updates, unsubscribe, ok := store.Subscribe(jobID)
		if !ok {
			http.Error(w, "Processing job not found", http.StatusNotFound)
			return
		}
		defer unsubscribe()

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming is not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		fmt.Fprint(w, "retry: 3000\n\n")
		writeProgressEvent(w, snapshot)
		flusher.Flush()
		if snapshot.Stage == "completed" || snapshot.Stage == "failed" {
			return
		}

		heartbeat := time.NewTicker(20 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case event, open := <-updates:
				if !open {
					return
				}
				writeProgressEvent(w, event)
				flusher.Flush()
				if event.Stage == "completed" || event.Stage == "failed" {
					return
				}
			case <-heartbeat.C:
				fmt.Fprint(w, ": keep-alive\n\n")
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	}
}

func writeProgressEvent(w io.Writer, event queue.StatusEvent) {
	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("Encode processing status event for %s: %v", event.JobID, err)
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", payload)
}

func supportedContentType(contentType string) bool {
	switch contentType {
	case "application/pdf", "image/png", "image/jpeg", "image/tiff", "image/bmp":
		return true
	default:
		return false
	}
}

func newJobID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("generate random ID: %w", err)
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16]), nil
}

func main() {
	go func() {
		if err := prometheus.StartMetricsServer("METRICS_PORT_UPLOADER"); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			log.Printf("Metrics server failed: %v", err)
		}
	}()

	storageClient, bucketConfig := buckets.NewClient()
	if err := buckets.EnsureBucket(context.Background(), storageClient, bucketConfig.Bucket); err != nil {
		log.Fatalf("Initialize object storage: %v", err)
	}
	log.Printf("Using object storage bucket: %s", bucketConfig.Bucket)

	conn, publisher, err := queue.ConnectToQueue()
	if err != nil {
		log.Fatalf("Connect to RabbitMQ: %v", err)
	}
	defer conn.Close()
	defer publisher.Close()

	mux := http.NewServeMux()
	statusStore := progress.NewStore()
	mux.HandleFunc("/upload", uploadHandler(storageClient, bucketConfig.Bucket, publisher, statusStore))
	mux.HandleFunc("/results", resultHandler(storageClient, bucketConfig.Bucket))
	statusQueue := os.Getenv("QUEUE_NAME") + ".events"
	statusChannel, statusEvents, err := queue.ConsumeStatusEvents(conn, statusQueue)
	if err != nil {
		log.Fatalf("Connect to RabbitMQ status queue: %v", err)
	}
	defer statusChannel.Close()
	go consumeStatusEvents(statusEvents, statusStore)
	mux.HandleFunc("/events", progressHandler(statusStore))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		payload := r.URL.Path[1:]
		if payload == "" {
			payload = "Hello, World!"
		}
		// #nosec G706 -- Request fields have CR/LF removed by safeLogField.
		log.Printf("Received request: %s %s", safeLogField(r.Method), safeLogField(r.URL.Path))
		fmt.Fprintf(w, "<h1>%s</h1>", html.EscapeString(payload))
	}))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	// #nosec G706 -- The configured address has CR/LF removed by safeLogField.
	log.Printf("Starting uploader on %s", safeLogField(addr))
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	// SSE responses are long-lived, so leave WriteTimeout unset.
	log.Fatal(server.ListenAndServe())
}

func safeLogField(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.ReplaceAll(value, "\n", " ")
}
