package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"brodaty.dev/cv-platform/common/rabbitmq"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/streadway/amqp"
)

const (
	maxRetries    = 3
	jobTimeout    = 2 * time.Minute
	statusTimeout = 10 * time.Second
)

func consumeOffers(
	deliveries <-chan amqp.Delivery,
	publisherChannel *amqp.Channel,
	confirmations <-chan amqp.Confirmation,
	workQueue, failedQueue, bucket string,
	storageClient *s3.Client,
) error {
	for delivery := range deliveries {
		if err := handleOfferDelivery(
			publisherChannel, confirmations, delivery,
			workQueue, failedQueue, bucket, storageClient,
		); err != nil {
			log.Printf("Handle offer matching delivery: %v", err)
		}
	}
	return fmt.Errorf("RabbitMQ offer delivery channel closed")
}

func handleOfferDelivery(
	publisherChannel *amqp.Channel,
	confirmations <-chan amqp.Confirmation,
	delivery amqp.Delivery,
	workQueue, failedQueue, bucket string,
	storageClient *s3.Client,
) error {
	var job offerJob
	if err := json.Unmarshal(delivery.Body, &job); err != nil {
		return moveOfferToFailed(publisherChannel, confirmations, delivery, failedQueue, bucket, storageClient, offerJob{}, fmt.Errorf("decode offer job: %w", err))
	}
	if !validOfferJob(job) {
		return moveOfferToFailed(publisherChannel, confirmations, delivery, failedQueue, bucket, storageClient, job, fmt.Errorf("invalid offer job reference"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	err := processOfferJob(ctx, storageClient, bucket, job)
	cancel()
	if err == nil {
		if err := delivery.Ack(false); err != nil {
			return fmt.Errorf("acknowledge offer match %s: %w", job.MatchID, err)
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
			return fmt.Errorf("retry offer match %s: %w", job.MatchID, publishErr)
		}
		statusCtx, cancel := context.WithTimeout(context.Background(), statusTimeout)
		artifact, artifactErr := readMatchArtifact(statusCtx, storageClient, bucket, job.MatchID)
		if artifactErr == nil {
			artifact.Status = "queued"
			artifact.UpdatedAt = time.Now().UTC()
			artifact.Message = "Offer comparison encountered a temporary error and will retry."
			if err := writeMatchArtifact(statusCtx, storageClient, bucket, artifact); err != nil {
				cancel()
				_ = delivery.Nack(false, true)
				return fmt.Errorf("store retry status for offer match %s: %w", job.MatchID, err)
			}
		}
		cancel()
		if artifactErr != nil {
			_ = delivery.Nack(false, true)
			return fmt.Errorf("read retry status for offer match %s: %w", job.MatchID, artifactErr)
		}
		if err := delivery.Ack(false); err != nil {
			return fmt.Errorf("acknowledge retried offer match %s: %w", job.MatchID, err)
		}
		log.Printf("Offer match %s failed (attempt %d/%d); queued for retry: %s",
			job.MatchID, retry+1, maxRetries+1, safeLogField(err.Error()))
		return nil
	}
	return moveOfferToFailed(
		publisherChannel, confirmations, delivery,
		failedQueue, bucket, storageClient, job, err,
	)
}

func processOfferJob(ctx context.Context, client *s3.Client, bucket string, job offerJob) error {
	artifact, err := readMatchArtifact(ctx, client, bucket, job.MatchID)
	if err != nil {
		return fmt.Errorf("load offer match state: %w", err)
	}
	artifact.Status = "processing"
	artifact.UpdatedAt = time.Now().UTC()
	artifact.Message = "Extracting required skills from the offer and comparing them with the resume."
	artifact.Percent = nil
	artifact.RequiredCount = 0
	artifact.MatchedCount = 0
	artifact.MatchedSkills = nil
	artifact.MissingSkills = nil

	offerText := job.OfferText
	if job.OfferURL != "" {
		offerText, err = fetchOfferText(ctx, job.OfferURL)
		if err != nil {
			return err
		}
	}
	offerText = strings.TrimSpace(offerText)
	if offerText == "" || len(offerText) > maxOfferTextBytes || !utf8.ValidString(offerText) {
		return fmt.Errorf("offer text is empty, invalid, or too large")
	}
	artifact.OfferText = offerText
	if err := writeMatchArtifact(ctx, client, bucket, artifact); err != nil {
		return err
	}

	required, _ := requiredSkills(offerText)
	if len(required) == 0 {
		artifact.Status = "unscorable"
		artifact.UpdatedAt = time.Now().UTC()
		artifact.Message = "No required skill signals could be identified in this offer."
		return writeMatchArtifact(ctx, client, bucket, artifact)
	}
	candidate, err := readCandidateSkills(ctx, client, bucket, job.ResumeJobID)
	if err != nil {
		return err
	}
	percent, matched, missing, scored := requiredSkillPercent(required, candidate)
	if !scored {
		artifact.Status = "unscorable"
		artifact.UpdatedAt = time.Now().UTC()
		artifact.Message = "No required skill signals could be identified in this offer."
		return writeMatchArtifact(ctx, client, bucket, artifact)
	}
	artifact.Status = "completed"
	artifact.UpdatedAt = time.Now().UTC()
	artifact.Message = "Required-skill coverage is ready."
	artifact.Percent = &percent
	artifact.RequiredCount = len(required)
	artifact.MatchedCount = len(matched)
	artifact.MatchedSkills = matched
	artifact.MissingSkills = missing
	return writeMatchArtifact(ctx, client, bucket, artifact)
}

func validOfferJob(job offerJob) bool {
	if !matchIDPattern.MatchString(job.MatchID) || !matchIDPattern.MatchString(job.ResumeJobID) {
		return false
	}
	hasText := job.OfferText != ""
	hasURL := job.OfferURL != ""
	if hasText == hasURL {
		return false
	}
	if hasText {
		return strings.TrimSpace(job.OfferText) != "" &&
			len(job.OfferText) <= maxOfferTextBytes &&
			utf8.ValidString(job.OfferText)
	}
	return len(job.OfferURL) <= maxOfferURLLength && validPublicURLForInput(job.OfferURL)
}

func moveOfferToFailed(
	publisherChannel *amqp.Channel,
	confirmations <-chan amqp.Confirmation,
	delivery amqp.Delivery,
	failedQueue, bucket string,
	storageClient *s3.Client,
	job offerJob,
	cause error,
) error {
	headers := rabbitmq.CopyHeaders(delivery.Headers)
	headers["x-last-error"] = safeLogField(cause.Error())
	if err := rabbitmq.PublishAndConfirm(
		publisherChannel, confirmations, failedQueue, delivery.Body, headers, rabbitmq.ConfirmTimeout,
	); err != nil {
		_ = delivery.Nack(false, true)
		return fmt.Errorf("move offer job to %q: %w", failedQueue, err)
	}
	if matchIDPattern.MatchString(job.MatchID) {
		ctx, cancel := context.WithTimeout(context.Background(), statusTimeout)
		artifact, err := readMatchArtifact(ctx, storageClient, bucket, job.MatchID)
		if err == nil {
			artifact.Status = "failed"
			artifact.UpdatedAt = time.Now().UTC()
			artifact.Message = "Offer comparison could not be completed."
			err = writeMatchArtifact(ctx, storageClient, bucket, artifact)
		}
		cancel()
		if err != nil {
			var notFound *matchArtifactNotFound
			if !errors.As(err, &notFound) {
				_ = delivery.Nack(false, true)
				return fmt.Errorf("store failed status for offer match %s: %w", job.MatchID, err)
			}
		}
	}
	if err := delivery.Ack(false); err != nil {
		return fmt.Errorf("acknowledge failed offer job: %w", err)
	}
	log.Printf("Offer job moved to %s: %s", safeLogField(failedQueue), safeLogField(cause.Error()))
	return nil
}

func safeLogField(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.ReplaceAll(value, "\n", " ")
}
