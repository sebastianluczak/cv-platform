package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"brodaty.dev/cv-platform/uploader/progress"
	"brodaty.dev/cv-platform/uploader/queue"
)

func TestProgressHandlerStreamsLatestTerminalStatus(t *testing.T) {
	store := progress.NewStore()
	if !store.Update(queue.StatusEvent{
		JobID:   "job-1",
		Stage:   "completed",
		Message: "Resume text extraction is complete.",
	}) {
		t.Fatal("expected status update to be accepted")
	}

	request := httptest.NewRequest(http.MethodGet, "/events?job_id=job-1", nil)
	response := httptest.NewRecorder()
	progressHandler(store).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("got HTTP status %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("got content type %q, want text/event-stream", got)
	}
	if !strings.Contains(response.Body.String(), `"stage":"completed"`) {
		t.Fatalf("SSE response did not contain the current completed status: %s", response.Body.String())
	}
}

func TestProgressHandlerReturnsNotFoundForUnknownJob(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/events?job_id=missing", nil)

	progressHandler(progress.NewStore()).ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("got HTTP status %d, want %d", response.Code, http.StatusNotFound)
	}
}
