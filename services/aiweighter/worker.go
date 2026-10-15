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

func consumeWeights(
	deliveries <-chan amqp.Delivery,
	publisherChannel *amqp.Channel,
	confirmations <-chan amqp.Confirmation,
	workQueue, failedQueue, bucket string,
	storageClient *s3.Client,
	modelClient *openRouterClient,
) error {
	for delivery := range deliveries {
		if err := handleWeightDelivery(
			publisherChannel, confirmations, delivery,
			workQueue, failedQueue, bucket, storageClient, modelClient,
		); err != nil {
			log.Printf("Handle AI resume delivery: %v", err)
		}
	}
	return fmt.Errorf("RabbitMQ AI resume delivery channel closed")
}

func handleWeightDelivery(
	publisherChannel *amqp.Channel,
	confirmations <-chan amqp.Confirmation,
	delivery amqp.Delivery,
	workQueue, failedQueue, bucket string,
	storageClient *s3.Client,
	modelClient *openRouterClient,
) error {
	var job weighJob
	if err := json.Unmarshal(delivery.Body, &job); err != nil {
		return moveWeightToFailed(publisherChannel, confirmations, delivery, failedQueue, bucket, storageClient, weighJob{}, fmt.Errorf("decode AI resume job: %w", err))
	}
	if !validWeighJob(job) {
		return moveWeightToFailed(publisherChannel, confirmations, delivery, failedQueue, bucket, storageClient, job, fmt.Errorf("invalid AI resume job reference"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	err := processWeighJob(ctx, storageClient, bucket, job, modelClient)
	cancel()
	if err == nil {
		if err := delivery.Ack(false); err != nil {
			return fmt.Errorf("acknowledge AI resume %s: %w", job.WeighID, err)
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
			return fmt.Errorf("retry AI resume %s: %w", job.WeighID, publishErr)
		}
		statusCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		artifact, artifactErr := readWeighArtifact(statusCtx, storageClient, bucket, job.WeighID)
		if artifactErr == nil {
			updateArtifactStatus(&artifact, "queued", "AI resume generation encountered a temporary error and will retry.")
			if err := writeWeighArtifact(statusCtx, storageClient, bucket, artifact); err != nil {
				cancel()
				_ = delivery.Nack(false, true)
				return fmt.Errorf("store retry status for AI resume %s: %w", job.WeighID, err)
			}
		}
		cancel()
		if artifactErr != nil {
			_ = delivery.Nack(false, true)
			return fmt.Errorf("read retry status for AI resume %s: %w", job.WeighID, artifactErr)
		}
		if err := delivery.Ack(false); err != nil {
			return fmt.Errorf("acknowledge retried AI resume %s: %w", job.WeighID, err)
		}
		// #nosec G706 -- Dynamic fields are CR/LF-sanitized by safeLogField.
		log.Printf("AI resume %s failed (attempt %d/%d); queued for retry: %s",
			safeLogField(job.WeighID), retry+1, maxRetries+1, safeLogField(err.Error()))
		return nil
	}
	return moveWeightToFailed(
		publisherChannel, confirmations, delivery,
		failedQueue, bucket, storageClient, job, err,
	)
}

func processWeighJob(
	ctx context.Context,
	client *s3.Client,
	bucket string,
	job weighJob,
	modelClient *openRouterClient,
) error {
	artifact, err := readWeighArtifact(ctx, client, bucket, job.WeighID)
	if err != nil {
		return fmt.Errorf("load AI resume state: %w", err)
	}
	if err := validateWeighArtifact(artifact, job.WeighID); err != nil {
		return err
	}
	if artifact.MatchID != job.MatchID {
		return fmt.Errorf("AI resume job match ID does not match stored request")
	}
	if artifact.Status == "completed" {
		return validateWeighArtifact(artifact, job.WeighID)
	}
	updateArtifactStatus(&artifact, "processing", "Preparing the resume and contacting the configured AI provider.")
	artifact.Markdown = ""
	if err := writeWeighArtifact(ctx, client, bucket, artifact); err != nil {
		return err
	}

	match, err := readOfferMatch(ctx, client, bucket, job.MatchID)
	if err != nil {
		return fmt.Errorf("load offer match: %w", err)
	}
	if match.ResumeJobID != artifact.ResumeJobID || match.Status != "completed" {
		return fmt.Errorf("offer match does not match the AI resume request")
	}
	resumeText, skillJSON, err := loadResumeInputs(ctx, client, bucket, match)
	if err != nil {
		return err
	}
	prompt, err := buildPrompt(resumeText, skillJSON, match.OfferText)
	if err != nil {
		return err
	}
	markdown, err := modelClient.generate(ctx, prompt)
	if err != nil {
		return err
	}
	artifact.Markdown = markdown
	updateArtifactStatus(&artifact, "completed", "Your tailored resume is ready. Review it carefully before use.")
	if err := validateWeighArtifact(artifact, job.WeighID); err != nil {
		return err
	}
	return writeWeighArtifact(ctx, client, bucket, artifact)
}

func validWeighJob(job weighJob) bool {
	return idPattern.MatchString(job.WeighID) && idPattern.MatchString(job.MatchID)
}

func moveWeightToFailed(
	publisherChannel *amqp.Channel,
	confirmations <-chan amqp.Confirmation,
	delivery amqp.Delivery,
	failedQueue, bucket string,
	storageClient *s3.Client,
	job weighJob,
	cause error,
) error {
	headers := rabbitmq.CopyHeaders(delivery.Headers)
	headers["x-last-error"] = safeLogField(cause.Error())
	if err := rabbitmq.PublishAndConfirm(
		publisherChannel, confirmations, failedQueue, delivery.Body, headers, rabbitmq.ConfirmTimeout,
	); err != nil {
		_ = delivery.Nack(false, true)
		return fmt.Errorf("move AI resume job to %q: %w", failedQueue, err)
	}
	if idPattern.MatchString(job.WeighID) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		artifact, err := readWeighArtifact(ctx, storageClient, bucket, job.WeighID)
		if err == nil && artifact.MatchID == job.MatchID {
			updateArtifactStatus(&artifact, "failed", "AI resume generation could not be completed.")
			artifact.Markdown = ""
			err = writeWeighArtifact(ctx, storageClient, bucket, artifact)
		}
		cancel()
		if err != nil {
			var notFound *artifactNotFound
			if !errors.As(err, &notFound) {
				_ = delivery.Nack(false, true)
				return fmt.Errorf("store failed AI resume status %s: %w", job.WeighID, err)
			}
		}
	}
	if err := delivery.Ack(false); err != nil {
		return fmt.Errorf("acknowledge failed AI resume job: %w", err)
	}
	// #nosec G706 -- Dynamic fields are CR/LF-sanitized by safeLogField.
	log.Printf("AI resume job moved to %s: %s", safeLogField(failedQueue), safeLogField(cause.Error()))
	return nil
}
