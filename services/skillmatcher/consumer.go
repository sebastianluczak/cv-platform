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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/streadway/amqp"
)

const (
	maxRetries     = 3
	jobTimeout     = 2 * time.Minute
	confirmTimeout = 10 * time.Second
)

var jobIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type matchingJob struct {
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

type storageConfig struct {
	endpoint string
	region   string
	bucket   string
}

func run() error {
	rabbitMQURL := strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	if rabbitMQURL == "" {
		return fmt.Errorf("RABBITMQ_URL is not set")
	}
	queueName := strings.TrimSpace(os.Getenv("QUEUE_NAME"))
	if queueName == "" {
		return fmt.Errorf("QUEUE_NAME is not set")
	}

	conn, err := amqp.Dial(rabbitMQURL)
	if err != nil {
		return fmt.Errorf("connect to RabbitMQ: %w", err)
	}
	defer conn.Close()

	workQueue := queueName + ".skills"
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
	log.Printf("Skill matcher consuming queue %q; bucket: %q", workQueue, config.bucket)
	for delivery := range deliveries {
		if err := handleDelivery(
			consumerChannel, publisherChannel, confirmations, delivery,
			workQueue, failedQueue, statusQueue, storageClient, config.bucket,
		); err != nil {
			log.Printf("Handle skill matching delivery: %v", err)
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
	var job matchingJob
	if err := json.Unmarshal(delivery.Body, &job); err != nil {
		return moveToFailedQueue(
			consumerChannel, publisherChannel, confirmations, delivery,
			failedQueue, statusQueue, "", 0, fmt.Errorf("decode matching job: %w", err),
		)
	}
	if !validJob(job, bucket) {
		return moveToFailedQueue(
			consumerChannel, publisherChannel, confirmations, delivery,
			failedQueue, statusQueue, job.ID, retryCount(delivery.Headers)+1,
			fmt.Errorf("invalid matching job reference"),
		)
	}

	attempt := retryCount(delivery.Headers) + 1
	publishStatusBestEffort(publisherChannel, confirmations, statusQueue, statusEvent{
		JobID: job.ID, Stage: "matching", Message: "Comparing resume experience with IT role profiles.", Attempt: attempt,
	})
	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	err := matchJob(ctx, storageClient, bucket, job)
	cancel()
	if err == nil {
		if err := publishStatus(publisherChannel, confirmations, statusQueue, statusEvent{
			JobID: job.ID, Stage: "completed", Message: "Role profile scores are ready.", Attempt: attempt,
		}); err != nil {
			_ = delivery.Nack(false, true)
			return fmt.Errorf("publish completed status for job %s: %w", job.ID, err)
		}
		if err := delivery.Ack(false); err != nil {
			return fmt.Errorf("acknowledge matching job %s: %w", job.ID, err)
		}
		log.Printf("Matched job %s; scores stored at %s/results/%s.skills.json", job.ID, bucket, job.ID)
		return nil
	}

	retry := retryCount(delivery.Headers)
	if retry < maxRetries {
		publishStatusBestEffort(publisherChannel, confirmations, statusQueue, statusEvent{
			JobID: job.ID, Stage: "matching", Message: "Matching could not be completed; retrying.", Attempt: retry + 2,
		})
		headers := copyHeaders(delivery.Headers)
		headers["x-retry-count"] = int32(retry + 1)
		headers["x-last-error"] = errorText(err)
		if publishErr := publishAndConfirm(publisherChannel, confirmations, workQueue, delivery.Body, headers); publishErr != nil {
			_ = delivery.Nack(false, true)
			return fmt.Errorf("retry matching job %s: %w", job.ID, publishErr)
		}
		if ackErr := delivery.Ack(false); ackErr != nil {
			return fmt.Errorf("acknowledge retried matching job %s: %w", job.ID, ackErr)
		}
		log.Printf("Matching job %s failed (attempt %d/%d); queued for retry: %v", job.ID, retry+1, maxRetries+1, err)
		return nil
	}

	return moveToFailedQueue(
		consumerChannel, publisherChannel, confirmations, delivery,
		failedQueue, statusQueue, job.ID, attempt, err,
	)
}

func matchJob(ctx context.Context, client *s3.Client, bucket string, job matchingJob) error {
	if !validJob(job, bucket) {
		return fmt.Errorf("invalid matching job reference")
	}
	text, err := readRedactedResult(ctx, client, bucket, job.ObjectKey)
	if err != nil {
		return err
	}
	result := scoreText(job.ID, text, time.Now())
	if err := writeMatchResult(ctx, client, bucket, "results/"+job.ID+".skills.json", result); err != nil {
		return err
	}
	return nil
}

func validJob(job matchingJob, bucket string) bool {
	return jobIDPattern.MatchString(job.ID) &&
		job.Bucket == bucket &&
		job.ObjectKey == "results/"+job.ID+".redacted.txt"
}

func newStorageClient() (*s3.Client, storageConfig) {
	config := storageConfig{
		endpoint: envOrDefault("RUSTFS_ENDPOINT_URL", "http://localhost:9000"),
		region:   envOrDefault("RUSTFS_REGION", "us-east-1"),
		bucket:   envOrDefault("RUSTFS_BUCKET", "cv-platform-uploads"),
	}
	client := s3.NewFromConfig(aws.Config{
		Region: config.region,
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(
			envOrDefault("RUSTFS_ACCESS_KEY_ID", "rustfsadmin"),
			envOrDefault("RUSTFS_SECRET_ACCESS_KEY", "rustfsadmin"),
			"",
		)),
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(config.endpoint)
		options.UsePathStyle = true
	})
	return client, config
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
