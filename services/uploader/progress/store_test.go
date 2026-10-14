package progress

import (
	"testing"
	"time"

	"brodaty.dev/cv-platform/uploader/queue"
)

func TestStorePublishesUpdatesAndRejectsStaleQueuedEvent(t *testing.T) {
	store := NewStore()
	queued := queue.StatusEvent{JobID: "job-1", Stage: "queued", UpdatedAt: time.Now()}
	if !store.Update(queued) {
		t.Fatal("expected queued update to be accepted")
	}

	snapshot, updates, unsubscribe, ok := store.Subscribe("job-1")
	if !ok || snapshot.Stage != "queued" {
		t.Fatalf("unexpected initial snapshot: %+v, found=%v", snapshot, ok)
	}
	defer unsubscribe()

	processing := queue.StatusEvent{JobID: "job-1", Stage: "processing", UpdatedAt: time.Now()}
	if !store.Update(processing) {
		t.Fatal("expected processing update to be accepted")
	}
	if got := <-updates; got.Stage != "processing" {
		t.Fatalf("got stage %q, want processing", got.Stage)
	}
	redacting := queue.StatusEvent{JobID: "job-1", Stage: "redacting", UpdatedAt: time.Now()}
	if !store.Update(redacting) {
		t.Fatal("expected redacting update to be accepted")
	}
	if got := <-updates; got.Stage != "redacting" {
		t.Fatalf("got stage %q, want redacting", got.Stage)
	}
	if store.Update(queued) {
		t.Fatal("stale queued update should not replace current progress")
	}
}

func TestStoreKeepsTerminalStatus(t *testing.T) {
	store := NewStore()
	if !store.Update(queue.StatusEvent{JobID: "job-1", Stage: "completed"}) {
		t.Fatal("expected completed update to be accepted")
	}
	if store.Update(queue.StatusEvent{JobID: "job-1", Stage: "processing"}) {
		t.Fatal("terminal status should not be replaced")
	}
}
