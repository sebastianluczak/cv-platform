package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"brodaty.dev/cv-platform/common/rabbitmq"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/streadway/amqp"
)

const (
	maxRetries = 3
	jobTimeout = 2 * time.Minute
)

func consumeInsights(
	deliveries <-chan amqp.Delivery,
	publisherChannel *amqp.Channel,
	confirmations <-chan amqp.Confirmation,
	workQueue, failedQueue, bucket string,
	storageClient *s3.Client,
	modelClient *openRouterClient,
) error {
	for delivery := range deliveries {
		if err := handleInsightDelivery(
			publisherChannel, confirmations, delivery,
			workQueue, failedQueue, bucket, storageClient, modelClient,
		); err != nil {
			log.Printf("Handle role insights delivery.")
		}
	}
	return fmt.Errorf("RabbitMQ role insights delivery channel closed")
}

func handleInsightDelivery(
	publisherChannel *amqp.Channel,
	confirmations <-chan amqp.Confirmation,
	delivery amqp.Delivery,
	workQueue, failedQueue, bucket string,
	storageClient *s3.Client,
	modelClient *openRouterClient,
) error {
	var job insightJob
	if err := json.Unmarshal(delivery.Body, &job); err != nil {
		return moveInsightToFailed(
			publisherChannel, confirmations, delivery, failedQueue, bucket, storageClient,
			insightJob{}, fmt.Errorf("decode role insights job: %w", err),
		)
	}
	if !validInsightJob(job) {
		return moveInsightToFailed(
			publisherChannel, confirmations, delivery, failedQueue, bucket, storageClient,
			job, fmt.Errorf("invalid role insights job reference"),
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	err := processInsightJob(ctx, storageClient, bucket, job, modelClient)
	cancel()
	if err == nil {
		if err := delivery.Ack(false); err != nil {
			return fmt.Errorf("acknowledge role insights %s: %w", job.InsightID, err)
		}
		return nil
	}

	retry := rabbitmq.RetryCount(delivery.Headers, maxRetries)
	if retry < maxRetries {
		headers := rabbitmq.CopyHeaders(delivery.Headers)
		headers["x-retry-count"] = int64(retry + 1)
		headers["x-last-error"] = safeLogField(err.Error())
		if publishErr := rabbitmq.PublishAndConfirm(
			publisherChannel, confirmations, workQueue, delivery.Body, headers, rabbitmq.ConfirmTimeout,
		); publishErr != nil {
			_ = delivery.Nack(false, true)
			return fmt.Errorf("retry role insights %s: %w", job.InsightID, publishErr)
		}
		statusCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		artifact, artifactErr := readInsightArtifact(statusCtx, storageClient, bucket, job.InsightID)
		if artifactErr == nil {
			updateInsightStatus(&artifact, "queued", "Role insight generation encountered a temporary error and will retry.")
			if err := writeInsightArtifact(statusCtx, storageClient, bucket, artifact); err != nil {
				cancel()
				_ = delivery.Nack(false, true)
				return fmt.Errorf("store retry status for role insights %s: %w", job.InsightID, err)
			}
		}
		cancel()
		if artifactErr != nil {
			_ = delivery.Nack(false, true)
			return fmt.Errorf("read retry status for role insights %s: %w", job.InsightID, artifactErr)
		}
		if err := delivery.Ack(false); err != nil {
			return fmt.Errorf("acknowledge retried role insights %s: %w", job.InsightID, err)
		}

		return nil
	}
	return moveInsightToFailed(
		publisherChannel, confirmations, delivery,
		failedQueue, bucket, storageClient, job, err,
	)
}

func processInsightJob(
	ctx context.Context,
	client *s3.Client,
	bucket string,
	job insightJob,
	modelClient *openRouterClient,
) error {
	artifact, err := readInsightArtifact(ctx, client, bucket, job.InsightID)
	if err != nil {
		return fmt.Errorf("load role insights state: %w", err)
	}
	if err := validateInsightArtifact(artifact, job.InsightID); err != nil {
		return err
	}
	if artifact.ResumeJobID != job.ResumeJobID {
		return fmt.Errorf("role insights job resume ID does not match stored request")
	}
	if artifact.Status == "completed" {
		return nil
	}
	updateInsightStatus(&artifact, "processing", "Reviewing the redacted resume with the configured AI provider.")
	artifact.Roles = nil
	if err := writeInsightArtifact(ctx, client, bucket, artifact); err != nil {
		return err
	}
	resumeText, err := readRedactedResume(ctx, client, bucket, job.ResumeJobID)
	if err != nil {
		return err
	}
	roles, err := modelClient.generate(ctx, resumeText)
	if err != nil {
		return err
	}
	artifact.Roles = roles
	updateInsightStatus(&artifact, "completed", "Role insights are ready. Suggestions are advisory and based only on resume text.")
	if err := validateInsightArtifact(artifact, job.InsightID); err != nil {
		return err
	}
	return writeInsightArtifact(ctx, client, bucket, artifact)
}

func validInsightJob(job insightJob) bool {
	return idPattern.MatchString(job.InsightID) && idPattern.MatchString(job.ResumeJobID)
}

func moveInsightToFailed(
	publisherChannel *amqp.Channel,
	confirmations <-chan amqp.Confirmation,
	delivery amqp.Delivery,
	failedQueue, bucket string,
	storageClient *s3.Client,
	job insightJob,
	cause error,
) error {
	headers := rabbitmq.CopyHeaders(delivery.Headers)
	headers["x-last-error"] = safeLogField(cause.Error())
	if err := rabbitmq.PublishAndConfirm(
		publisherChannel, confirmations, failedQueue, delivery.Body, headers, rabbitmq.ConfirmTimeout,
	); err != nil {
		_ = delivery.Nack(false, true)
		return fmt.Errorf("move role insights job to %q: %w", failedQueue, err)
	}
	if idPattern.MatchString(job.InsightID) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		artifact, err := readInsightArtifact(ctx, storageClient, bucket, job.InsightID)
		if err == nil && artifact.ResumeJobID == job.ResumeJobID {
			updateInsightStatus(&artifact, "failed", "Role insights could not be completed.")
			artifact.Roles = nil
			err = writeInsightArtifact(ctx, storageClient, bucket, artifact)
		}
		cancel()
		if err != nil {
			var notFound *insightArtifactNotFound
			if !errors.As(err, &notFound) {
				_ = delivery.Nack(false, true)
				return fmt.Errorf("store failed role insights status %s: %w", job.InsightID, err)
			}
		}
	}
	if err := delivery.Ack(false); err != nil {
		return fmt.Errorf("acknowledge failed role insights job: %w", err)
	}

	return nil
}
