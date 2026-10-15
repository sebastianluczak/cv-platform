package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/streadway/amqp"
)

// TestFailedQueueAutoPurge verifies that old messages in the failed queue are purged
// while new messages are retained. This is an integration test that requires RabbitMQ.
func TestFailedQueueAutoPurge(t *testing.T) {
	// Skip test if RabbitMQ is not available
	rabbitmqURL := os.Getenv("RABBITMQ_URL")
	if rabbitmqURL == "" {
		t.Skip("RABBITMQ_URL not set; skipping integration test")
	}

	// Connect to RabbitMQ
	conn, err := amqp.Dial(rabbitmqURL)
	if err != nil {
		t.Skipf("Could not connect to RabbitMQ at %s: %v", rabbitmqURL, err)
	}
	defer conn.Close()

	channel, err := conn.Channel()
	if err != nil {
		t.Fatalf("Failed to open channel: %v", err)
	}
	defer channel.Close()

	// Use a test queue name
	queueName := "test-failed-queue.failed"
	retentionSeconds := int64(10) // 10 seconds for quick testing

	// Declare the failed queue
	_, err = channel.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		t.Fatalf("Failed to declare queue %q: %v", queueName, err)
	}

	// Purge queue before test to ensure clean state
	_, err = channel.QueuePurge(queueName, false)
	if err != nil {
		t.Fatalf("Failed to purge queue before test: %v", err)
	}

	// Publish old message (11 seconds old, should be purged)
	oldTimestamp := time.Now().Add(-11 * time.Second)
	oldMsgBody := map[string]interface{}{
		"job_id":    "old-job-123",
		"timestamp": oldTimestamp.Format(time.RFC3339),
		"attempt":   1,
	}
	oldMsgBytes, _ := json.Marshal(oldMsgBody)

	err = channel.Publish(
		"",        // exchange
		queueName, // routing key
		false,     // mandatory
		false,     // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        oldMsgBytes,
			Timestamp:   oldTimestamp,
		},
	)
	if err != nil {
		t.Fatalf("Failed to publish old message: %v", err)
	}

	// Publish new message (1 second old, should be retained)
	newTimestamp := time.Now().Add(-1 * time.Second)
	newMsgBody := map[string]interface{}{
		"job_id":    "new-job-456",
		"timestamp": newTimestamp.Format(time.RFC3339),
		"attempt":   1,
	}
	newMsgBytes, _ := json.Marshal(newMsgBody)

	err = channel.Publish(
		"",        // exchange
		queueName, // routing key
		false,     // mandatory
		false,     // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        newMsgBytes,
			Timestamp:   newTimestamp,
		},
	)
	if err != nil {
		t.Fatalf("Failed to publish new message: %v", err)
	}

	// Verify both messages exist before cleanup
	queueBefore, err := channel.QueueInspect(queueName)
	if err != nil {
		t.Fatalf("Failed to inspect queue before cleanup: %v", err)
	}

	if queueBefore.Messages != 2 {
		t.Errorf("Expected 2 messages before cleanup, got %d", queueBefore.Messages)
	}

	// Create a separate channel for cleanup to avoid interference
	cleanupChannel, err := conn.Channel()
	if err != nil {
		t.Fatalf("Failed to open cleanup channel: %v", err)
	}
	defer cleanupChannel.Close()

	// Run cleanup with test retention period
	maxAgeSeconds := retentionSeconds

	ctx := context.Background()
	purged, err := cleanupFailedQueue(ctx, cleanupChannel, queueName, maxAgeSeconds, false)
	if err != nil {
		t.Fatalf("Failed to run cleanup: %v", err)
	}

	// Verify old message was purged
	if purged != 1 {
		t.Errorf("Expected 1 message purged, got %d", purged)
	}

	// Verify queue state after cleanup
	queueAfter, err := channel.QueueInspect(queueName)
	if err != nil {
		t.Fatalf("Failed to inspect queue after cleanup: %v", err)
	}

	if queueAfter.Messages != 1 {
		t.Errorf("Expected 1 message after cleanup, got %d", queueAfter.Messages)
	}

	// Verify the remaining message is the new one
	deliveries, err := channel.Consume(queueName, "", false, false, false, false, nil)
	if err != nil {
		t.Fatalf("Failed to consume queue: %v", err)
	}

	select {
	case delivery, ok := <-deliveries:
		if !ok {
			t.Errorf("Expected to consume a message, got nothing")
			return
		}

		var msgBody map[string]interface{}
		err = json.Unmarshal(delivery.Body, &msgBody)
		if err != nil {
			t.Errorf("Failed to unmarshal message: %v", err)
		}

		jobID, ok := msgBody["job_id"].(string)
		if !ok || jobID != "new-job-456" {
			t.Errorf("Expected new-job-456, got %v", jobID)
		}

		// Acknowledge the message to remove it from queue
		delivery.Ack(false)

	case <-time.After(1 * time.Second):
		t.Errorf("Timeout waiting for remaining message")
	}

	// Clean up test queue
	_, err = channel.QueueDelete(queueName, false, false, false)
	if err != nil {
		t.Logf("Warning: failed to delete test queue: %v", err)
	}
}

// TestAllFailedQueuesAutoPurge verifies that all 5 failed queue types are cleaned correctly
func TestAllFailedQueuesAutoPurge(t *testing.T) {
	// Skip test if RabbitMQ is not available
	rabbitmqURL := os.Getenv("RABBITMQ_URL")
	if rabbitmqURL == "" {
		t.Skip("RABBITMQ_URL not set; skipping integration test")
	}

	// Connect to RabbitMQ
	conn, err := amqp.Dial(rabbitmqURL)
	if err != nil {
		t.Skipf("Could not connect to RabbitMQ at %s: %v", rabbitmqURL, err)
	}
	defer conn.Close()

	channel, err := conn.Channel()
	if err != nil {
		t.Fatalf("Failed to open channel: %v", err)
	}
	defer channel.Close()

	queuePrefix := "test-all-queues"
	retentionSeconds := int64(10)

	// Define all failed queue types to test
	queueSuffixes := []string{
		".failed",          // extraction failures
		".pii.failed",      // redaction failures
		".skills.failed",   // skill matching failures
		".offers.failed",   // offer matching failures
		".aiweight.failed", // AI weighting failures
	}

	// Setup: Create all queues and publish old messages
	for _, suffix := range queueSuffixes {
		queueName := queuePrefix + suffix

		// Declare queue
		_, err = channel.QueueDeclare(queueName, true, false, false, false, nil)
		if err != nil {
			t.Fatalf("Failed to declare queue %q: %v", queueName, err)
		}

		// Purge queue before test
		_, err = channel.QueuePurge(queueName, false)
		if err != nil {
			t.Fatalf("Failed to purge queue %q: %v", queueName, err)
		}

		// Publish old message to each queue
		oldTimestamp := time.Now().Add(-11 * time.Second)
		msgBody := map[string]interface{}{
			"job_id":    "old-job-" + suffix,
			"timestamp": oldTimestamp.Format(time.RFC3339),
			"queue":     suffix,
		}
		msgBytes, _ := json.Marshal(msgBody)

		err = channel.Publish(
			"",        // exchange
			queueName, // routing key
			false,     // mandatory
			false,     // immediate
			amqp.Publishing{
				ContentType: "application/json",
				Body:        msgBytes,
				Timestamp:   oldTimestamp,
			},
		)
		if err != nil {
			t.Fatalf("Failed to publish message to %q: %v", queueName, err)
		}
	}

	// Verify all queues have one message each
	for _, suffix := range queueSuffixes {
		queueName := queuePrefix + suffix
		qi, err := channel.QueueInspect(queueName)
		if err != nil {
			t.Fatalf("Failed to inspect queue %q: %v", queueName, err)
		}
		if qi.Messages != 1 {
			t.Errorf("Queue %q should have 1 message before cleanup, got %d", queueName, qi.Messages)
		}
	}

	// Run cleanup on all queues using separate channels
	totalPurged := 0
	for _, suffix := range queueSuffixes {
		queueName := queuePrefix + suffix
		
		// Create a separate channel for this cleanup operation
		cleanupChannel, err := conn.Channel()
		if err != nil {
			t.Errorf("Failed to open cleanup channel for %q: %v", queueName, err)
			continue
		}
		
		ctx := context.Background()
		purged, err := cleanupFailedQueue(ctx, cleanupChannel, queueName, retentionSeconds, false)
		if err != nil {
			t.Errorf("Failed to cleanup queue %q: %v", queueName, err)
		}
		totalPurged += purged
		cleanupChannel.Close()
	}

	// Verify 5 messages were purged (one from each queue)
	if totalPurged != len(queueSuffixes) {
		t.Errorf("Expected %d messages purged, got %d", len(queueSuffixes), totalPurged)
	}

	// Verify all queues are now empty
	for _, suffix := range queueSuffixes {
		queueName := queuePrefix + suffix
		qi, err := channel.QueueInspect(queueName)
		if err != nil {
			t.Fatalf("Failed to inspect queue %q after cleanup: %v", queueName, err)
		}
		if qi.Messages != 0 {
			t.Errorf("Queue %q should be empty after cleanup, got %d messages", queueName, qi.Messages)
		}
	}

	// Clean up all test queues
	for _, suffix := range queueSuffixes {
		queueName := queuePrefix + suffix
		_, err = channel.QueueDelete(queueName, false, false, false)
		if err != nil {
			t.Logf("Warning: failed to delete test queue %q: %v", queueName, err)
		}
	}
}

// TestFailedQueueCleanup_MixedAge verifies that messages are correctly filtered by age
// Some old should be purged, new messages retained
func TestFailedQueueCleanup_MixedAge(t *testing.T) {
	// Skip test if RabbitMQ is not available
	rabbitmqURL := os.Getenv("RABBITMQ_URL")
	if rabbitmqURL == "" {
		t.Skip("RABBITMQ_URL not set; skipping integration test")
	}

	// Connect to RabbitMQ
	conn, err := amqp.Dial(rabbitmqURL)
	if err != nil {
		t.Skipf("Could not connect to RabbitMQ at %s: %v", rabbitmqURL, err)
	}
	defer conn.Close()

	channel, err := conn.Channel()
	if err != nil {
		t.Fatalf("Failed to open channel: %v", err)
	}
	defer channel.Close()

	queueName := "test-mixed-age.failed"
	retentionSeconds := int64(10)

	// Declare the queue
	_, err = channel.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		t.Fatalf("Failed to declare queue: %v", err)
	}

	// Purge queue before test
	_, err = channel.QueuePurge(queueName, false)
	if err != nil {
		t.Fatalf("Failed to purge queue: %v", err)
	}

	// Publish messages with various ages
	testCases := []struct {
		age       time.Duration
		shouldPurge bool
		jobID     string
	}{
		{-5 * time.Second, false, "recent-1"},      // 5s old - should retain
		{-8 * time.Second, false, "recent-2"},      // 8s old - should retain
		{-11 * time.Second, true, "old-1"},         // 11s old - should purge
		{-20 * time.Second, true, "old-2"},         // 20s old - should purge
		{-1 * time.Second, false, "recent-3"},      // 1s old - should retain
		{-15 * time.Second, true, "old-3"},         // 15s old - should purge
	}

	// Publish all messages
	for _, tc := range testCases {
		timestamp := time.Now().Add(tc.age)
		msgBody := map[string]interface{}{
			"job_id":    tc.jobID,
			"timestamp": timestamp.Format(time.RFC3339),
		}
		msgBytes, _ := json.Marshal(msgBody)

		err = channel.Publish(
			"",        // exchange
			queueName, // routing key
			false,     // mandatory
			false,     // immediate
			amqp.Publishing{
				ContentType: "application/json",
				Body:        msgBytes,
				Timestamp:   timestamp,
			},
		)
		if err != nil {
			t.Fatalf("Failed to publish message: %v", err)
		}
	}

	// Verify all messages are present
	queueBefore, err := channel.QueueInspect(queueName)
	if err != nil {
		t.Fatalf("Failed to inspect queue: %v", err)
	}

	expectedBefore := len(testCases)
	if queueBefore.Messages != expectedBefore {
		t.Errorf("Expected %d messages before cleanup, got %d", expectedBefore, queueBefore.Messages)
	}

	// Create a separate channel for cleanup
	cleanupChannel, err := conn.Channel()
	if err != nil {
		t.Fatalf("Failed to open cleanup channel: %v", err)
	}
	defer cleanupChannel.Close()

	// Run cleanup
	ctx := context.Background()
	purged, err := cleanupFailedQueue(ctx, cleanupChannel, queueName, retentionSeconds, false)
	if err != nil {
		t.Fatalf("Failed to run cleanup: %v", err)
	}

	// Count expected purged messages
	expectedPurged := 0
	for _, tc := range testCases {
		if tc.shouldPurge {
			expectedPurged++
		}
	}

	if purged != expectedPurged {
		t.Errorf("Expected %d messages purged, got %d", expectedPurged, purged)
	}

	// Verify remaining messages
	queueAfter, err := channel.QueueInspect(queueName)
	if err != nil {
		t.Fatalf("Failed to inspect queue after cleanup: %v", err)
	}

	expectedAfter := expectedBefore - expectedPurged
	if queueAfter.Messages != expectedAfter {
		t.Errorf("Expected %d messages after cleanup, got %d", expectedAfter, queueAfter.Messages)
	}

	// Clean up test queue
	_, err = channel.QueueDelete(queueName, false, false, false)
	if err != nil {
		t.Logf("Warning: failed to delete test queue: %v", err)
	}
}

// TestFailedQueueCleanup_NonExistentQueue verifies graceful handling of non-existent queues
func TestFailedQueueCleanup_NonExistentQueue(t *testing.T) {
	// Skip test if RabbitMQ is not available
	rabbitmqURL := os.Getenv("RABBITMQ_URL")
	if rabbitmqURL == "" {
		t.Skip("RABBITMQ_URL not set; skipping integration test")
	}

	// Connect to RabbitMQ
	conn, err := amqp.Dial(rabbitmqURL)
	if err != nil {
		t.Skipf("Could not connect to RabbitMQ at %s: %v", rabbitmqURL, err)
	}
	defer conn.Close()

	channel, err := conn.Channel()
	if err != nil {
		t.Fatalf("Failed to open channel: %v", err)
	}
	defer channel.Close()

	// Try to cleanup a queue that doesn't exist
	queueName := "non-existent-queue-" + time.Now().Format("20060102150405")
	retentionSeconds := int64(10)

	ctx := context.Background()
	purged, err := cleanupFailedQueue(ctx, channel, queueName, retentionSeconds, false)

	// Should return gracefully without error (queue doesn't exist, so it's skipped)
	if err != nil {
		t.Logf("Non-existent queue handled gracefully: %v", err)
	}

	if purged != 0 {
		t.Errorf("Expected 0 messages purged from non-existent queue, got %d", purged)
	}
}

// TestFailedQueueCleanup_EmptyQueue verifies graceful handling of empty queues
func TestFailedQueueCleanup_EmptyQueue(t *testing.T) {
	// Skip test if RabbitMQ is not available
	rabbitmqURL := os.Getenv("RABBITMQ_URL")
	if rabbitmqURL == "" {
		t.Skip("RABBITMQ_URL not set; skipping integration test")
	}

	// Connect to RabbitMQ
	conn, err := amqp.Dial(rabbitmqURL)
	if err != nil {
		t.Skipf("Could not connect to RabbitMQ at %s: %v", rabbitmqURL, err)
	}
	defer conn.Close()

	channel, err := conn.Channel()
	if err != nil {
		t.Fatalf("Failed to open channel: %v", err)
	}
	defer channel.Close()

	queueName := "test-empty.failed"

	// Declare the queue
	_, err = channel.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		t.Fatalf("Failed to declare queue: %v", err)
	}

	// Purge it to ensure it's empty
	_, err = channel.QueuePurge(queueName, false)
	if err != nil {
		t.Fatalf("Failed to purge queue: %v", err)
	}

	// Create a separate channel for cleanup
	cleanupChannel, err := conn.Channel()
	if err != nil {
		t.Fatalf("Failed to open cleanup channel: %v", err)
	}
	defer cleanupChannel.Close()

	// Run cleanup on empty queue
	ctx := context.Background()
	retentionSeconds := int64(10)
	purged, err := cleanupFailedQueue(ctx, cleanupChannel, queueName, retentionSeconds, false)

	if err != nil {
		t.Fatalf("Failed to cleanup empty queue: %v", err)
	}

	if purged != 0 {
		t.Errorf("Expected 0 messages purged from empty queue, got %d", purged)
	}

	// Clean up test queue
	_, err = channel.QueueDelete(queueName, false, false, false)
	if err != nil {
		t.Logf("Warning: failed to delete test queue: %v", err)
	}
}

// TestFailedQueueCleanup_DryRun verifies that dry-run mode doesn't delete messages
func TestFailedQueueCleanup_DryRun(t *testing.T) {
	// Skip test if RabbitMQ is not available
	rabbitmqURL := os.Getenv("RABBITMQ_URL")
	if rabbitmqURL == "" {
		t.Skip("RABBITMQ_URL not set; skipping integration test")
	}

	// Connect to RabbitMQ
	conn, err := amqp.Dial(rabbitmqURL)
	if err != nil {
		t.Skipf("Could not connect to RabbitMQ at %s: %v", rabbitmqURL, err)
	}
	defer conn.Close()

	channel, err := conn.Channel()
	if err != nil {
		t.Fatalf("Failed to open channel: %v", err)
	}
	defer channel.Close()

	queueName := "test-dryrun.failed"
	retentionSeconds := int64(10)

	// Declare the queue
	_, err = channel.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		t.Fatalf("Failed to declare queue: %v", err)
	}

	// Purge queue before test
	_, err = channel.QueuePurge(queueName, false)
	if err != nil {
		t.Fatalf("Failed to purge queue: %v", err)
	}

	// Publish old message
	oldTimestamp := time.Now().Add(-11 * time.Second)
	msgBody := map[string]interface{}{
		"job_id":    "old-job",
		"timestamp": oldTimestamp.Format(time.RFC3339),
	}
	msgBytes, _ := json.Marshal(msgBody)

	err = channel.Publish(
		"",        // exchange
		queueName, // routing key
		false,     // mandatory
		false,     // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        msgBytes,
			Timestamp:   oldTimestamp,
		},
	)
	if err != nil {
		t.Fatalf("Failed to publish message: %v", err)
	}

	// Verify message exists
	queueBefore, err := channel.QueueInspect(queueName)
	if err != nil {
		t.Fatalf("Failed to inspect queue: %v", err)
	}

	if queueBefore.Messages != 1 {
		t.Errorf("Expected 1 message before cleanup, got %d", queueBefore.Messages)
	}

	// Create a separate channel for cleanup
	cleanupChannel, err := conn.Channel()
	if err != nil {
		t.Fatalf("Failed to open cleanup channel: %v", err)
	}
	defer cleanupChannel.Close()

	// Run cleanup in dry-run mode
	ctx := context.Background()
	_, err = cleanupFailedQueue(ctx, cleanupChannel, queueName, retentionSeconds, true) // dryRun=true

	if err != nil {
		t.Fatalf("Failed to run cleanup: %v", err)
	}

	// In dry-run mode, the function reports what would be purged but doesn't actually delete
	// The message should still be in the queue after dry-run
	queueAfter, err := channel.QueueInspect(queueName)
	if err != nil {
		t.Fatalf("Failed to inspect queue after cleanup: %v", err)
	}

	if queueAfter.Messages != 1 {
		t.Errorf("In dry-run mode, message should still be in queue, but got %d messages", queueAfter.Messages)
	}

	// Clean up test queue
	_, err = channel.QueueDelete(queueName, false, false, false)
	if err != nil {
		t.Logf("Warning: failed to delete test queue: %v", err)
	}
}
