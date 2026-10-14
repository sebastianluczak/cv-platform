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

type extractionJob struct {
	ID          string `json:"id"`
	Bucket      string `json:"bucket"`
	ObjectKey   string `json:"object_key"`
	ContentType string `json:"content_type"`
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
		if err := handleDelivery(channel, confirmations, delivery, queueName, failedQueue, storageClient, config.Bucket); err != nil {
			log.Printf("Handle RabbitMQ delivery: %v", err)
		}
	}
	return fmt.Errorf("RabbitMQ delivery channel closed")
}

func handleDelivery(
	channel *amqp.Channel,
	confirmations <-chan amqp.Confirmation,
	delivery amqp.Delivery,
	queueName, failedQueue string,
	storageClient *s3.Client,
	bucket string,
) error {
	var job extractionJob
	if err := json.Unmarshal(delivery.Body, &job); err != nil {
		return moveToFailedQueue(channel, confirmations, delivery, failedQueue, fmt.Errorf("decode job: %w", err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	err := processJob(ctx, storageClient, bucket, job)
	cancel()
	if err == nil {
		if err := delivery.Ack(false); err != nil {
			return fmt.Errorf("acknowledge job %s: %w", job.ID, err)
		}
		log.Printf("Extracted job %s; result stored at %s/results/%s.txt", job.ID, bucket, job.ID)
		return nil
	}

	attempt := retryCount(delivery.Headers)
	if attempt < maxRetries {
		headers := copyHeaders(delivery.Headers)
		headers["x-retry-count"] = int32(attempt + 1)
		headers["x-last-error"] = errorText(err)
		if publishErr := publishAndConfirm(channel, confirmations, queueName, delivery.Body, headers); publishErr != nil {
			_ = delivery.Nack(false, true)
			return fmt.Errorf("retry job %s: %w", job.ID, publishErr)
		}
		if ackErr := delivery.Ack(false); ackErr != nil {
			return fmt.Errorf("acknowledge retried job %s: %w", job.ID, ackErr)
		}
		log.Printf("Job %s failed (attempt %d/%d); queued for retry: %v", job.ID, attempt+1, maxRetries+1, err)
		return nil
	}

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
