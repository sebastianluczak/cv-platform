// Package rabbitmq contains shared publishing and retry helpers for the services.
package rabbitmq

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/streadway/amqp"
)

// ConfirmTimeout is the shared timeout for status and job-message publishes.
const ConfirmTimeout = 10 * time.Second

// Publisher is the publish operation used by RabbitMQ channels.
type Publisher interface {
	Publish(exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error
}

// StatusEvent is the common status message sent through the events queue.
type StatusEvent struct {
	JobID     string    `json:"job_id"`
	Stage     string    `json:"stage"`
	Message   string    `json:"message"`
	Attempt   int       `json:"attempt"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PublishAndConfirm publishes a persistent JSON message and waits for its broker confirmation.
func PublishAndConfirm(
	channel Publisher,
	confirmations <-chan amqp.Confirmation,
	routingKey string,
	body []byte,
	headers amqp.Table,
	timeout time.Duration,
) error {
	if err := channel.Publish(
		"",
		routingKey,
		true,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Headers:      headers,
			Body:         body,
		},
	); err != nil {
		return fmt.Errorf("publish to %q: %w", routingKey, err)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case confirmation, ok := <-confirmations:
		if !ok {
			return fmt.Errorf("RabbitMQ closed the publisher confirmation channel")
		}
		if !confirmation.Ack {
			return fmt.Errorf("RabbitMQ rejected publish to %q", routingKey)
		}
	case <-timer.C:
		return fmt.Errorf("timed out waiting for RabbitMQ confirmation")
	}
	return nil
}

// PublishStatus publishes a status event and waits for its broker confirmation.
func PublishStatus(
	channel Publisher,
	confirmations <-chan amqp.Confirmation,
	statusQueue string,
	event StatusEvent,
) error {
	event.UpdatedAt = time.Now().UTC()
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode %s status: %w", event.Stage, err)
	}
	return PublishAndConfirm(channel, confirmations, statusQueue, body, nil, ConfirmTimeout)
}

// PublishStatusBestEffort logs failures instead of returning them.
func PublishStatusBestEffort(
	channel Publisher,
	confirmations <-chan amqp.Confirmation,
	statusQueue string,
	event StatusEvent,
) {
	if event.JobID == "" {
		return
	}
	if err := PublishStatus(channel, confirmations, statusQueue, event); err != nil {
		log.Printf("Publish %s status for job %s: %v", event.Stage, event.JobID, err)
	}
}

// RetryCount returns a bounded retry count from RabbitMQ headers.
func RetryCount(headers amqp.Table, maxRetries int) int {
	if headers == nil {
		return 0
	}
	switch value := headers["x-retry-count"].(type) {
	case int:
		return boundedRetryCount(int64(value), maxRetries)
	case int32:
		return boundedRetryCount(int64(value), maxRetries)
	case int64:
		return boundedRetryCount(value, maxRetries)
	default:
		return 0
	}
}

// CopyHeaders returns an independent copy of headers.
func CopyHeaders(headers amqp.Table) amqp.Table {
	copied := make(amqp.Table, len(headers)+2)
	for key, value := range headers {
		copied[key] = value
	}
	return copied
}

func boundedRetryCount(count int64, maxRetries int) int {
	if count <= 0 {
		return 0
	}
	if count > int64(maxRetries) {
		return maxRetries + 1
	}
	return int(count)
}
