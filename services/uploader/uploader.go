package main

import (
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"

	"brodaty.dev/cv-platform/uploader/buckets"
	"brodaty.dev/cv-platform/uploader/queue"
)

const maxUploadSize = 10 << 20 // 10 MiB

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Limit the entire request body.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	// Extract the uploaded file from multipart/form-data.
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Invalid upload", http.StatusBadRequest)
		return
	}
	defer file.Close()

	uploadDir := "./uploads"
	if err := os.MkdirAll(uploadDir, 0750); err != nil {
		http.Error(w, "Storage unavailable", http.StatusInternalServerError)
		return
	}

	// Generate a unique filename; don't trust the client's filename.
	dst, err := os.CreateTemp(uploadDir, "upload-*")
	if err != nil {
		http.Error(w, "Could not create file", http.StatusInternalServerError)
		return
	}

	dstPath := dst.Name()
	defer dst.Close()

	// Copy the uploaded contents to disk.
	if _, err := io.Copy(dst, file); err != nil {
		_ = os.Remove(dstPath)
		http.Error(w, "Could not save file", http.StatusInternalServerError)
		return
	}

	log.Printf("Uploaded %q (%d bytes) to %s",
		header.Filename, header.Size, dstPath)

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "Upload successful")
}

func main() {

	http.HandleFunc("/upload", uploadHandler)

	// This is a wildcard. Nice.
	http.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		payload := r.URL.Path[1:]
		if payload == "" {
			payload = "Hello, World!"
		}
		log.Println("Received request:", r.Method, r.URL.Path)
		fmt.Fprintf(w, "<h1>%s</h1>", html.EscapeString(payload))
	}))

	log.Println("Starting server on http://localhost:8080")

	buckets.ListOurBuckets()

	// Now AI stuff
	// aiweighter.SendToWeighter("What is 2+2?") <- THIS WORKS!!!

	// Now queue. It is not microservice without RabbitMq xD
	conn, channel, err := queue.ConnectToQueue()
	if err != nil {
		log.Fatal("Failed to connect to queue:", err)
	}
	queue.PublishMessage(channel, "Hello from main.go!")
	defer conn.Close()
	log.Fatal(http.ListenAndServe(":8080", nil))
}
