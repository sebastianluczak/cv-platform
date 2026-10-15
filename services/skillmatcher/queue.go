package main

import (
	"fmt"
	"log"
	"strings"

	"brodaty.dev/cv-platform/common/rabbitmq"
	"github.com/streadway/amqp"
)

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

func safeLogField(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.ReplaceAll(value, "\n", " ")
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > 1024 {
		return message[:1024]
	}
	return message
}
