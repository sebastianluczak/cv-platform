package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"brodaty.dev/cv-platform/extractor/storage"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/streadway/amqp"
)

const (
	maxRetries     = 3
	jobTimeout     = 5 * time.Minute
	confirmTimeout = 10 * time.Second
)

type statusEvent struct {
	JobID     string    `json:"job_id"`
	Stage     string    `json:"stage"`
	Message   string    `json:"message"`
	Attempt   int       `json:"attempt"`
	UpdatedAt time.Time `json:"updated_at"`
}

type extractionJob struct {
	ID          string `json:"id"`
	Bucket      string `json:"bucket"`
	ObjectKey   string `json:"object_key"`
	ContentType string `json:"content_type"`
}

type redactionJob struct {
	ID        string `json:"id"`
	Bucket    string `json:"bucket"`
	ObjectKey string `json:"object_key"`
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	for _, tool := range []string{"pdftotext", "pdfinfo", "pdftoppm"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("required PDF tool %q not found: %w", tool, err)
		}
	}

	rabbitMQURL := os.Getenv("RABBITMQ_URL")
	if rabbitMQURL == "" {
		return fmt.Errorf("RABBITMQ_URL is not set")
	}
	queueName := os.Getenv("QUEUE_NAME")
	if queueName == "" {
		return fmt.Errorf("QUEUE_NAME is not set")
	}

	conn, err := amqp.Dial(rabbitMQURL)
	if err != nil {
		return fmt.Errorf("connect to RabbitMQ: %w", err)
	}
	defer conn.Close()

	channel, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open RabbitMQ channel: %w", err)
	}
	defer channel.Close()

	if _, err := channel.QueueDeclare(queueName, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare queue %q: %w", queueName, err)
	}
	failedQueue := queueName + ".failed"
	if _, err := channel.QueueDeclare(failedQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare failed-job queue %q: %w", failedQueue, err)
	}
	redactionQueue := queueName + ".pii"
	if _, err := channel.QueueDeclare(redactionQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare redaction queue %q: %w", redactionQueue, err)
	}
	statusChannel, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open RabbitMQ status channel: %w", err)
	}
	defer statusChannel.Close()
	statusQueue := queueName + ".events"
	if _, err := statusChannel.QueueDeclare(statusQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare status queue %q: %w", statusQueue, err)
	}
	if err := statusChannel.Confirm(false); err != nil {
		return fmt.Errorf("enable RabbitMQ status publisher confirms: %w", err)
	}
	statusConfirmations := statusChannel.NotifyPublish(make(chan amqp.Confirmation, 1))
	if err := channel.Qos(1, 0, false); err != nil {
		return fmt.Errorf("configure RabbitMQ prefetch: %w", err)
	}
	if err := channel.Confirm(false); err != nil {
		return fmt.Errorf("enable RabbitMQ publisher confirms: %w", err)
	}
	confirmations := channel.NotifyPublish(make(chan amqp.Confirmation, 1))

	deliveries, err := channel.Consume(queueName, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume queue %q: %w", queueName, err)
	}

	storageClient, config := storage.NewClient()
	log.Printf("Extractor consuming queue %q; input/output bucket: %q", queueName, config.Bucket)
	for delivery := range deliveries {
		if err := handleDelivery(channel, confirmations, statusChannel, statusConfirmations, delivery, queueName, failedQueue, redactionQueue, statusQueue, storageClient, config.Bucket); err != nil {
			log.Printf("Handle RabbitMQ delivery: %v", err)
		}
	}
	return fmt.Errorf("RabbitMQ delivery channel closed")
}

func handleDelivery(
	channel *amqp.Channel,
	confirmations <-chan amqp.Confirmation,
	statusChannel *amqp.Channel,
	statusConfirmations <-chan amqp.Confirmation,
	delivery amqp.Delivery,
	queueName, failedQueue, redactionQueue, statusQueue string,
	storageClient *s3.Client,
	bucket string,
) error {
	var job extractionJob
	if err := json.Unmarshal(delivery.Body, &job); err != nil {
		return moveToFailedQueue(channel, confirmations, delivery, failedQueue, fmt.Errorf("decode job: %w", err))
	}

	attempt := retryCount(delivery.Headers) + 1
	publishStatusBestEffort(statusChannel, statusConfirmations, statusQueue, statusEvent{
		JobID: job.ID, Stage: "processing", Message: "We're reading your resume.", Attempt: attempt,
	})
	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	err := processJob(ctx, storageClient, bucket, job)
	cancel()
	if err == nil {
		redaction, err := json.Marshal(redactionJob{
			ID: job.ID, Bucket: bucket, ObjectKey: "results/" + job.ID + ".txt",
		})
		if err != nil {
			_ = delivery.Nack(false, true)
			return fmt.Errorf("encode redaction job %s: %w", job.ID, err)
		}
		if err := publishAndConfirm(channel, confirmations, redactionQueue, redaction, nil); err != nil {
			_ = delivery.Nack(false, true)
			return fmt.Errorf("queue redaction job %s: %w", job.ID, err)
		}
		publishStatusBestEffort(statusChannel, statusConfirmations, statusQueue, statusEvent{
			JobID: job.ID, Stage: "redacting", Message: "Removing common personal identifiers.", Attempt: attempt,
		})
		if err := delivery.Ack(false); err != nil {
			return fmt.Errorf("acknowledge job %s: %w", job.ID, err)
		}
		log.Printf("Extracted job %s; result stored at %s/results/%s.txt", job.ID, bucket, job.ID)
		return nil
	}

	retry := retryCount(delivery.Headers)
	if retry < maxRetries {
		publishStatusBestEffort(statusChannel, statusConfirmations, statusQueue, statusEvent{
			JobID: job.ID, Stage: "retrying", Message: "We hit a snag and are trying again.", Attempt: retry + 2,
		})
		headers := copyHeaders(delivery.Headers)
		headers["x-retry-count"] = int32(retry + 1)
		headers["x-last-error"] = errorText(err)
		if publishErr := publishAndConfirm(channel, confirmations, queueName, delivery.Body, headers); publishErr != nil {
			_ = delivery.Nack(false, true)
			return fmt.Errorf("retry job %s: %w", job.ID, publishErr)
		}
		if ackErr := delivery.Ack(false); ackErr != nil {
			return fmt.Errorf("acknowledge retried job %s: %w", job.ID, ackErr)
		}
		log.Printf("Job %s failed (attempt %d/%d); queued for retry: %v", job.ID, retry+1, maxRetries+1, err)
		return nil
	}

	publishStatusBestEffort(statusChannel, statusConfirmations, statusQueue, statusEvent{
		JobID: job.ID, Stage: "failed", Message: "We couldn't process this resume.", Attempt: retry + 1,
	})
	return moveToFailedQueue(channel, confirmations, delivery, failedQueue, err)
}

func processJob(ctx context.Context, client *s3.Client, bucket string, job extractionJob) error {
	if job.ID == "" || job.Bucket != bucket || job.ObjectKey != "uploads/"+job.ID {
		return fmt.Errorf("invalid extraction job reference")
	}
	inputPath, cleanup, err := storage.Download(ctx, client, job.Bucket, job.ObjectKey)
	if err != nil {
		return err
	}
	defer cleanup()

	text, err := extractText(ctx, inputPath, job.ContentType)
	if err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("no text extracted from file")
	}
	resultKey := "results/" + job.ID + ".txt"
	if err := storage.Upload(ctx, client, job.Bucket, resultKey, "text/plain; charset=utf-8", int64(len(text)), strings.NewReader(text)); err != nil {
		return fmt.Errorf("store extracted text: %w", err)
	}
	return nil
}

func moveToFailedQueue(
	channel *amqp.Channel,
	confirmations <-chan amqp.Confirmation,
	delivery amqp.Delivery,
	failedQueue string,
	cause error,
) error {
	headers := copyHeaders(delivery.Headers)
	headers["x-last-error"] = errorText(cause)
	if err := publishAndConfirm(channel, confirmations, failedQueue, delivery.Body, headers); err != nil {
		_ = delivery.Nack(false, true)
		return fmt.Errorf("move failed job to %q: %w", failedQueue, err)
	}
	if err := delivery.Ack(false); err != nil {
		return fmt.Errorf("acknowledge failed job: %w", err)
	}
	log.Printf("Job moved to %s: %v", failedQueue, cause)
	return nil
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
