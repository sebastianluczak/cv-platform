package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/streadway/amqp"
)

const (
	maxRetries     = 3
	storageTimeout = 2 * time.Minute
	confirmTimeout = 10 * time.Second
)

var jobIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type redactionJob struct {
	ID        string `json:"id"`
	Bucket    string `json:"bucket"`
	ObjectKey string `json:"object_key"`
}

type statusEvent struct {
	JobID     string    `json:"job_id"`
	Stage     string    `json:"stage"`
	Message   string    `json:"message"`
	Attempt   int       `json:"attempt"`
	UpdatedAt time.Time `json:"updated_at"`
}

func run() error {
	rabbitMQURL := strings.TrimSpace(getenv("RABBITMQ_URL"))
	if rabbitMQURL == "" {
		return fmt.Errorf("RABBITMQ_URL is not set")
	}
	queueName := strings.TrimSpace(getenv("QUEUE_NAME"))
	if queueName == "" {
		return fmt.Errorf("QUEUE_NAME is not set")
	}

	conn, err := amqp.Dial(rabbitMQURL)
	if err != nil {
		return fmt.Errorf("connect to RabbitMQ: %w", err)
	}
	defer conn.Close()

	workQueue := queueName + ".pii"
	failedQueue := workQueue + ".failed"
	statusQueue := queueName + ".events"
	consumerChannel, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open RabbitMQ consumer channel: %w", err)
	}
	defer consumerChannel.Close()
	for _, name := range []string{workQueue, failedQueue, statusQueue} {
		if _, err := consumerChannel.QueueDeclare(name, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare queue %q: %w", name, err)
		}
	}
	if err := consumerChannel.Qos(1, 0, false); err != nil {
		return fmt.Errorf("configure RabbitMQ prefetch: %w", err)
	}
	deliveries, err := consumerChannel.Consume(workQueue, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume queue %q: %w", workQueue, err)
	}

	publisherChannel, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open RabbitMQ publisher channel: %w", err)
	}
	defer publisherChannel.Close()
	if err := publisherChannel.Confirm(false); err != nil {
		return fmt.Errorf("enable publisher confirms: %w", err)
	}
	confirmations := publisherChannel.NotifyPublish(make(chan amqp.Confirmation, 1))
	storageClient, config := newStorageClient()
	log.Printf("PII stripper consuming queue %q; bucket: %q", workQueue, config.bucket)

	for delivery := range deliveries {
		if err := handleDelivery(
			consumerChannel, publisherChannel, confirmations, delivery,
			workQueue, failedQueue, statusQueue, storageClient, config.bucket,
		); err != nil {
			log.Printf("Handle redaction delivery: %v", err)
		}
	}
	return fmt.Errorf("RabbitMQ delivery channel closed")
}

func handleDelivery(
	consumerChannel, publisherChannel *amqp.Channel,
	confirmations <-chan amqp.Confirmation,
	delivery amqp.Delivery,
	workQueue, failedQueue, statusQueue string,
	storageClient *s3.Client,
	bucket string,
) error {
	var job redactionJob
	if err := json.Unmarshal(delivery.Body, &job); err != nil {
		return moveToFailedQueue(consumerChannel, publisherChannel, confirmations, delivery, failedQueue, statusQueue, "", 0, err)
	}
	if !validJob(job, bucket) {
		return moveToFailedQueue(
			consumerChannel, publisherChannel, confirmations, delivery, failedQueue, statusQueue,
			job.ID, retryCount(delivery.Headers)+1, fmt.Errorf("invalid redaction job reference"),
		)
	}

	attempt := retryCount(delivery.Headers) + 1
	publishStatusBestEffort(publisherChannel, confirmations, statusQueue, statusEvent{
		JobID: job.ID, Stage: "redacting", Message: "Removing common personal identifiers.", Attempt: attempt,
	})
	ctx, cancel := context.WithTimeout(context.Background(), storageTimeout)
	err := redactResult(ctx, storageClient, bucket, job)
	cancel()
	if err == nil {
		if err := publishStatus(publisherChannel, confirmations, statusQueue, statusEvent{
			JobID: job.ID, Stage: "completed", Message: "Redacted resume text is ready.", Attempt: attempt,
		}); err != nil {
			_ = delivery.Nack(false, true)
			return fmt.Errorf("publish completed status for job %s: %w", job.ID, err)
		}
		if err := delivery.Ack(false); err != nil {
			return fmt.Errorf("acknowledge redaction job %s: %w", job.ID, err)
		}
		log.Printf("Redacted job %s; output stored at %s/results/%s.redacted.txt", job.ID, bucket, job.ID)
		return nil
	}

	retry := retryCount(delivery.Headers)
	if retry < maxRetries {
		publishStatusBestEffort(publisherChannel, confirmations, statusQueue, statusEvent{
			JobID: job.ID, Stage: "redacting", Message: "Redaction could not be completed; retrying.", Attempt: retry + 2,
		})
		headers := copyHeaders(delivery.Headers)
		headers["x-retry-count"] = int32(retry + 1)
		headers["x-last-error"] = errorText(err)
		if publishErr := publishAndConfirm(publisherChannel, confirmations, workQueue, delivery.Body, headers); publishErr != nil {
			_ = delivery.Nack(false, true)
			return fmt.Errorf("retry redaction job %s: %w", job.ID, publishErr)
		}
		if ackErr := delivery.Ack(false); ackErr != nil {
			return fmt.Errorf("acknowledge retried redaction job %s: %w", job.ID, ackErr)
		}
		log.Printf("Redaction job %s failed (attempt %d/%d); queued for retry: %v", job.ID, retry+1, maxRetries+1, err)
		return nil
	}

	return moveToFailedQueue(
		consumerChannel, publisherChannel, confirmations, delivery, failedQueue, statusQueue,
		job.ID, attempt, err,
	)
}

func redactResult(ctx context.Context, client *s3.Client, bucket string, job redactionJob) error {
	if !validJob(job, bucket) {
		return fmt.Errorf("invalid redaction job reference")
	}
	text, err := readResult(ctx, client, bucket, job.ObjectKey)
	if err != nil {
		return err
	}
	redacted, err := redactText(text)
	if err != nil {
		return fmt.Errorf("redact extracted text: %w", err)
	}
	outputKey := "results/" + job.ID + ".redacted.txt"
	if err := writeRedactedResult(ctx, client, bucket, outputKey, redacted); err != nil {
		return err
	}
	return nil
}

func validJob(job redactionJob, bucket string) bool {
	return jobIDPattern.MatchString(job.ID) &&
		job.Bucket == bucket &&
		job.ObjectKey == "results/"+job.ID+".txt"
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
			JobID: jobID, Stage: "failed", Message: "We couldn't redact this resume.", Attempt: attempt,
		}); err != nil {
			_ = delivery.Nack(false, true)
			return fmt.Errorf("publish failed status for job %s: %w", jobID, err)
		}
	}
	if err := delivery.Ack(false); err != nil {
		return fmt.Errorf("acknowledge failed redaction job: %w", err)
	}
	log.Printf("Redaction job moved to %s: %v", failedQueue, cause)
	return nil
}

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
		return nil
	case <-timer.C:
		return fmt.Errorf("timed out waiting for RabbitMQ confirmation")
	}
}

func retryCount(headers amqp.Table) int {
	switch count := headers["x-retry-count"].(type) {
	case int:
		return count
	case int32:
		return int(count)
	case int64:
		return int(count)
	default:
		return 0
	}
}

func copyHeaders(headers amqp.Table) amqp.Table {
	copy := make(amqp.Table, len(headers)+2)
	for key, value := range headers {
		copy[key] = value
	}
	return copy
}

func errorText(err error) string {
	message := strings.TrimSpace(err.Error())
	if len(message) > 1024 {
		return message[:1024]
	}
	return message
}

func getenv(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}
