package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/streadway/amqp"
)

// failedQueueSuffixes defines the failed queue name suffixes to clean
var failedQueueSuffixes = []string{
	".failed",
	".pii.failed",
	".skills.failed",
	".offers.failed",
	".aiweight.failed",
	".roleinsights.failed",
}

type rabbitMQCleanupChannel interface {
	QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error)
	Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error)
}

// CleanupRabbitMQ orchestrates cleanup of failed message queues in RabbitMQ.
// It processes each failed queue, removes messages older than the configured
// retention period, and tracks metrics.
func CleanupRabbitMQ(ctx context.Context, channel rabbitMQCleanupChannel, config Config) (cleanupResult, error) {
	startTime := time.Now()
	var result cleanupResult
	var cleanupErrors []error

	maxAgeSeconds := int64(config.RetentionFailed) * 24 * 3600

	for _, suffix := range failedQueueSuffixes {
		queueName := config.QueueName + suffix

		queueResult, err := cleanupFailedQueue(ctx, channel, queueName, maxAgeSeconds, config.DryRun)
		result.add(queueResult)
		if err != nil {
			log.Printf("Error cleaning up queue %q: %v", queueName, err)
			cleanupErrors = append(cleanupErrors, err)
			continue
		}
	}

	duration := time.Since(startTime)
	LogRabbitMQCleanupSummary(result, duration, config.DryRun)

	return result, errors.Join(cleanupErrors...)
}

// cleanupFailedQueue processes messages in a single failed queue, purging those
// older than maxAge. It uses a non-destructive consume approach with requeue in
// dry-run mode and ack in normal mode.
func cleanupFailedQueue(ctx context.Context, channel rabbitMQCleanupChannel, queueName string, maxAgeSeconds int64, dryRun bool) (cleanupResult, error) {
	_, err := channel.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		return cleanupResult{failures: 1}, fmt.Errorf("declare queue %q: %w", queueName, err)
	}

	// Start consuming from the queue with auto-ack disabled
	deliveries, err := channel.Consume(queueName, "", false, false, false, false, nil)
	if err != nil {
		return cleanupResult{failures: 1}, fmt.Errorf("consume from queue %q: %w", queueName, err)
	}

	var result cleanupResult
	var cleanupErrors []error
	maxAgeTime := time.Now().Add(-time.Duration(maxAgeSeconds) * time.Second)

	// Consume messages with a context-aware loop
	for {
		select {
		case <-ctx.Done():
			result.failures++
			cleanupErrors = append(cleanupErrors, ctx.Err())
			return result, errors.Join(cleanupErrors...)

		case delivery, ok := <-deliveries:
			if !ok {
				// Channel closed
				return result, errors.Join(cleanupErrors...)
			}

			// Parse message timestamp
			msgTime, err := parseMessageTimestamp(delivery)
			if err != nil {
				log.Printf("Failed to parse timestamp from message in queue %q: %v, skipping message", queueName, err)
				// Nack and requeue to avoid stuck messages
				if nackErr := delivery.Nack(false, true); nackErr != nil {
					log.Printf("Failed to nack message in queue %q: %v", queueName, nackErr)
					result.failures++
					cleanupErrors = append(cleanupErrors, fmt.Errorf("requeue message with invalid timestamp in queue %q: %w", queueName, nackErr))
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
						result.failures++
						cleanupErrors = append(cleanupErrors, fmt.Errorf("requeue expired message during dry-run in queue %q: %w", queueName, nackErr))
					} else {
						result.wouldDelete++
					}
				} else {
					// Acknowledge (remove) the old message
					if ackErr := delivery.Ack(false); ackErr != nil {
						log.Printf("Failed to acknowledge message in queue %q: %v", queueName, ackErr)
						result.failures++
						cleanupErrors = append(cleanupErrors, fmt.Errorf("acknowledge expired message in queue %q: %w", queueName, ackErr))
						continue
					}
					log.Printf("Purged message from queue %q (age: %.2f days)", queueName, age.Hours()/24.0)
					result.deleted++
				}
			} else {
				// Message is too new, requeue it
				if nackErr := delivery.Nack(false, true); nackErr != nil {
					log.Printf("Failed to requeue recent message in queue %q: %v", queueName, nackErr)
					result.failures++
					cleanupErrors = append(cleanupErrors, fmt.Errorf("requeue recent message in queue %q: %w", queueName, nackErr))
				}
			}

			// Process only a reasonable batch per cycle to avoid timeout
			// Use a small pagination limit to be responsive to context cancellation
			if result.deleted+result.wouldDelete >= 1000 {
				log.Printf("Stopping cleanup of queue %q after processing 1000+ messages", queueName)
				return result, errors.Join(cleanupErrors...)
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
