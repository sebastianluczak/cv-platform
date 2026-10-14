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

	"brodaty.dev/cv-platform/common/objectstore"
	"brodaty.dev/cv-platform/common/rabbitmq"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/streadway/amqp"
)

const (
	maxRetries     = 3
	storageTimeout = 2 * time.Minute
)

var jobIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type redactionJob struct {
	ID        string `json:"id"`
	Bucket    string `json:"bucket"`
	ObjectKey string `json:"object_key"`
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
	skillQueue := queueName + ".skills"
	failedQueue := workQueue + ".failed"
	statusQueue := queueName + ".events"
	consumerChannel, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open RabbitMQ consumer channel: %w", err)
	}
	defer consumerChannel.Close()
	for _, name := range []string{workQueue, failedQueue, skillQueue, statusQueue} {
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
	storageClient, config := objectstore.NewClient()
	log.Printf("PII stripper consuming queue %q; bucket: %q", workQueue, config.Bucket)

	for delivery := range deliveries {
		if err := handleDelivery(
			consumerChannel, publisherChannel, confirmations, delivery,
			workQueue, failedQueue, skillQueue, statusQueue, storageClient, config.Bucket,
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
	workQueue, failedQueue, skillQueue, statusQueue string,
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
			job.ID, rabbitmq.RetryCount(delivery.Headers, maxRetries)+1, fmt.Errorf("invalid redaction job reference"),
		)
	}

	attempt := rabbitmq.RetryCount(delivery.Headers, maxRetries) + 1
	rabbitmq.PublishStatusBestEffort(publisherChannel, confirmations, statusQueue, rabbitmq.StatusEvent{
		JobID: job.ID, Stage: "redacting", Message: "Removing common personal identifiers.", Attempt: attempt,
	})
	ctx, cancel := context.WithTimeout(context.Background(), storageTimeout)
	err := redactResult(ctx, storageClient, bucket, job)
	cancel()
	if err == nil {
		skillJob, err := json.Marshal(redactionJob{
			ID: job.ID, Bucket: bucket, ObjectKey: "results/" + job.ID + ".redacted.txt",
		})
		if err != nil {
			_ = delivery.Nack(false, true)
			return fmt.Errorf("encode skill matching job %s: %w", job.ID, err)
		}
		if err := rabbitmq.PublishAndConfirm(publisherChannel, confirmations, skillQueue, skillJob, nil, rabbitmq.ConfirmTimeout); err != nil {
			_ = delivery.Nack(false, true)
			return fmt.Errorf("queue skill matching job %s: %w", job.ID, err)
		}
		rabbitmq.PublishStatusBestEffort(publisherChannel, confirmations, statusQueue, rabbitmq.StatusEvent{
			JobID: job.ID, Stage: "matching", Message: "Redacted resume is ready; comparing role profiles.", Attempt: attempt,
		})
		if err := delivery.Ack(false); err != nil {
			return fmt.Errorf("acknowledge redaction job %s: %w", job.ID, err)
		}
		log.Printf("Redacted job %s and queued role matching; source stored at %s/results/%s.redacted.txt", job.ID, bucket, job.ID)
		return nil
	}

	retry := rabbitmq.RetryCount(delivery.Headers, maxRetries)
	if retry < maxRetries {
		rabbitmq.PublishStatusBestEffort(publisherChannel, confirmations, statusQueue, rabbitmq.StatusEvent{
			JobID: job.ID, Stage: "redacting", Message: "Redaction could not be completed; retrying.", Attempt: retry + 2,
		})
		headers := rabbitmq.CopyHeaders(delivery.Headers)
		headers["x-retry-count"] = int64(retry + 1)
		headers["x-last-error"] = errorText(err)
		if publishErr := rabbitmq.PublishAndConfirm(publisherChannel, confirmations, workQueue, delivery.Body, headers, rabbitmq.ConfirmTimeout); publishErr != nil {
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
	headers := rabbitmq.CopyHeaders(delivery.Headers)
	headers["x-last-error"] = errorText(cause)
	if err := rabbitmq.PublishAndConfirm(publisherChannel, confirmations, failedQueue, delivery.Body, headers, rabbitmq.ConfirmTimeout); err != nil {
		_ = delivery.Nack(false, true)
		return fmt.Errorf("move job to %q: %w", failedQueue, err)
	}
	if jobIDPattern.MatchString(jobID) {
		if err := rabbitmq.PublishStatus(publisherChannel, confirmations, statusQueue, rabbitmq.StatusEvent{
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
