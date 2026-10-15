package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/streadway/amqp"
)

// failedQueueSuffixes defines the failed queue name suffixes to clean
var failedQueueSuffixes = []string{
	".failed",          // extraction failures
	".pii.failed",      // redaction failures
	".skills.failed",   // skill matching failures
	".offers.failed",   // offer matching failures
	".aiweight.failed", // AI weighting failures
}

// CleanupRabbitMQ orchestrates cleanup of failed message queues in RabbitMQ.
// It processes each failed queue, removes messages older than the configured
// retention period, and tracks metrics.
func CleanupRabbitMQ(ctx context.Context, channel *amqp.Channel, config Config) error {
	startTime := time.Now()
	totalPurged := 0
	totalErrors := 0

	maxAgeSeconds := int64(config.RetentionFailed) * 24 * 3600

	for _, suffix := range failedQueueSuffixes {
		queueName := config.QueueName + suffix

		purged, err := cleanupFailedQueue(ctx, channel, queueName, maxAgeSeconds, config.DryRun)
		if err != nil {
			log.Printf("Error cleaning up queue %q: %v", queueName, err)
			totalErrors++
			// Continue to next queue even on error
			continue
		}

		totalPurged += purged
	}

	duration := time.Since(startTime)
	LogRabbitMQCleanupSummary(totalPurged, totalErrors, duration, config.DryRun)

	return nil
}

// cleanupFailedQueue processes messages in a single failed queue, purging those
// older than maxAge. It uses a non-destructive consume approach with requeue in
// dry-run mode and ack in normal mode.
func cleanupFailedQueue(ctx context.Context, channel *amqp.Channel, queueName string, maxAgeSeconds int64, dryRun bool) (int, error) {
	// Attempt to declare the queue; if it doesn't exist, skip it gracefully
	_, err := channel.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		// Queue doesn't exist, which is fine; skip it
		log.Printf("Queue %q does not exist or could not be declared, skipping: %v", queueName, err)
		return 0, nil
	}

	// Start consuming from the queue with auto-ack disabled
	deliveries, err := channel.Consume(queueName, "", false, false, false, false, nil)
	if err != nil {
		return 0, fmt.Errorf("consume from queue %q: %w", queueName, err)
	}

	purgedCount := 0
	maxAgeTime := time.Now().Add(-time.Duration(maxAgeSeconds) * time.Second)

	// Consume messages with a context-aware loop
	for {
		select {
		case <-ctx.Done():
			return purgedCount, ctx.Err()

		case delivery, ok := <-deliveries:
			if !ok {
				// Channel closed
				return purgedCount, nil
			}

			// Parse message timestamp
			msgTime, err := parseMessageTimestamp(delivery)
			if err != nil {
				log.Printf("Failed to parse timestamp from message in queue %q: %v, skipping message", queueName, err)
				// Nack and requeue to avoid stuck messages
				if nackErr := delivery.Nack(false, true); nackErr != nil {
					log.Printf("Failed to nack message in queue %q: %v", queueName, nackErr)
				}
				continue
			}

			// Check if message is old enough to purge
			if msgTime.Before(maxAgeTime) {
				age := time.Since(msgTime)
				if dryRun {
					log.Printf("[DRY RUN] Would purge message from queue %q (age: %.2f days)", queueName, age.Hours()/24.0)
					// Nack and requeue in dry-run so message stays for next inspection
					if nackErr := delivery.Nack(false, true); nackErr != nil {
						log.Printf("Failed to nack message in queue %q during dry-run: %v", queueName, nackErr)
					}
				} else {
					// Acknowledge (remove) the old message
					if ackErr := delivery.Ack(false); ackErr != nil {
						log.Printf("Failed to acknowledge message in queue %q: %v", queueName, ackErr)
						// Don't count this as purged
						continue
					}
					log.Printf("Purged message from queue %q (age: %.2f days)", queueName, age.Hours()/24.0)
				}
				purgedCount++
			} else {
				// Message is too new, requeue it
				if nackErr := delivery.Nack(false, true); nackErr != nil {
					log.Printf("Failed to requeue recent message in queue %q: %v", queueName, nackErr)
				}
			}

			// Process only a reasonable batch per cycle to avoid timeout
			// Use a small pagination limit to be responsive to context cancellation
			if purgedCount >= 1000 {
				log.Printf("Stopping cleanup of queue %q after processing 1000+ messages", queueName)
				return purgedCount, nil
			}
		}
	}
}

// parseMessageTimestamp extracts the timestamp from an amqp.Delivery.
// It first tries to use the Timestamp field directly. If that's zero or not available,
// it attempts to extract from message headers. If all else fails, it returns
// the current time as a conservative default.
func parseMessageTimestamp(delivery amqp.Delivery) (time.Time, error) {
	// Try to use the Timestamp field directly
	if !delivery.Timestamp.IsZero() {
		return delivery.Timestamp, nil
	}

	// Try to extract from headers if available
	if delivery.Headers != nil {
		if headerVal, ok := delivery.Headers["x-publish-time"]; ok {
			if timeStr, ok := headerVal.(string); ok {
				if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
					return t, nil
				}
			}
		}

		// Try common timestamp header names
		for _, headerName := range []string{"timestamp", "x-timestamp", "published-at"} {
			if headerVal, ok := delivery.Headers[headerName]; ok {
				if timeStr, ok := headerVal.(string); ok {
					if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
						return t, nil
					}
				}
			}
		}
	}

	// Attempt to extract from message body as JSON
	var msgBody map[string]interface{}
	if err := json.Unmarshal(delivery.Body, &msgBody); err == nil {
		if tsVal, ok := msgBody["timestamp"]; ok {
			if timeStr, ok := tsVal.(string); ok {
				if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
					return t, nil
				}
			}
		}
	}

	// Final fallback: if we have a PublishingTime in amqp.Publishing info,
	// but that's not available in Delivery, so we log a warning and use now
	log.Printf("No timestamp found in message; using current time as fallback")
	return time.Now(), nil
}
