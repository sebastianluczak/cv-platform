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
	"time"

	"brodaty.dev/cv-platform/uploader/buckets"
	"brodaty.dev/cv-platform/uploader/queue"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	maxUploadSize  = 10 << 20 // 10 MiB
	maxRequestSize = maxUploadSize + (64 << 10)
)

func uploadHandler(storageClient *s3.Client, bucket string, publisher *queue.Publisher) http.HandlerFunc {
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
	mux.HandleFunc("/upload", uploadHandler(storageClient, bucketConfig.Bucket, publisher))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		payload := r.URL.Path[1:]
		if payload == "" {
			payload = "Hello, World!"
		}
		log.Println("Received request:", r.Method, r.URL.Path)
		fmt.Fprintf(w, "<h1>%s</h1>", html.EscapeString(payload))
	}))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	log.Printf("Starting uploader on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
