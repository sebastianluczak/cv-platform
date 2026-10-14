package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/streadway/amqp"
)

func publishStatusBestEffort(
	channel *amqp.Channel,
	confirmations <-chan amqp.Confirmation,
	statusQueue string,
	event statusEvent,
) {
	if event.JobID == "" {
		return
	}
	event.UpdatedAt = time.Now().UTC()
	body, err := json.Marshal(event)
	if err == nil {
		err = publishAndConfirm(channel, confirmations, statusQueue, body, nil)
	}
	if err != nil {
		log.Printf("Publish %s status for job %s: %v", event.Stage, event.JobID, err)
	}
}

func publishStatus(
	channel *amqp.Channel,
	confirmations <-chan amqp.Confirmation,
	statusQueue string,
	event statusEvent,
) error {
	event.UpdatedAt = time.Now().UTC()
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode %s status: %w", event.Stage, err)
	}
	return publishAndConfirm(channel, confirmations, statusQueue, body, nil)
}

func publishAndConfirm(
	channel *amqp.Channel,
	confirmations <-chan amqp.Confirmation,
	routingKey string,
	body []byte,
	headers amqp.Table,
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
	timer := time.NewTimer(confirmTimeout)
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

func moveToFailedQueue(
	consumerChannel, publisherChannel *amqp.Channel,
	confirmations <-chan amqp.Confirmation,
	delivery amqp.Delivery,
	failedQueue, statusQueue, jobID string, attempt int,
	cause error,
) error {
	headers := copyHeaders(delivery.Headers)
	headers["x-last-error"] = errorText(cause)
	if err := publishAndConfirm(publisherChannel, confirmations, failedQueue, delivery.Body, headers); err != nil {
		_ = delivery.Nack(false, true)
		return fmt.Errorf("move job to %q: %w", failedQueue, err)
	}
	if jobIDPattern.MatchString(jobID) {
		if err := publishStatus(publisherChannel, confirmations, statusQueue, statusEvent{
			JobID: jobID, Stage: "failed", Message: "We couldn't match this resume to role profiles.", Attempt: attempt,
		}); err != nil {
			_ = delivery.Nack(false, true)
			return fmt.Errorf("publish failed status for job %s: %w", jobID, err)
		}
	}
	if err := delivery.Ack(false); err != nil {
		return fmt.Errorf("acknowledge failed matching job: %w", err)
	}
	// #nosec G706 -- Dynamic fields have CR/LF removed by safeLogField.
	log.Printf("Matching job moved to %s: %s", safeLogField(failedQueue), safeLogField(fmt.Sprint(cause)))
	return nil
}

func retryCount(headers amqp.Table) int {
	if headers == nil {
		return 0
	}
	switch value := headers["x-retry-count"].(type) {
	case int:
		return boundedRetryCount(int64(value))
	case int32:
		return boundedRetryCount(int64(value))
	case int64:
		return boundedRetryCount(value)
	default:
		return 0
	}
}

func boundedRetryCount(count int64) int {
	if count <= 0 {
		return 0
	}
	if count > maxRetries {
		return maxRetries + 1
	}
	return int(count)
}

func copyHeaders(headers amqp.Table) amqp.Table {
	copied := make(amqp.Table, len(headers)+2)
	for key, value := range headers {
		copied[key] = value
	}
	return copied
}

func safeLogField(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.ReplaceAll(value, "\n", " ")
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
