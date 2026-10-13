package main

import (
	"fmt"
	"html"
	"log"
	"net/http"

	"brodaty.dev/cv-platform/queue"
	"brodaty.dev/cv-platform/uploader"
	"brodaty.dev/cv-platform/uploader/buckets"
)

func main() {

	http.HandleFunc("/upload", uploader.UploadHandler)

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
