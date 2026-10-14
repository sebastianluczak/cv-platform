package rabbitmq

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/streadway/amqp"
)

type testPublisher struct {
	routingKey string
	message    amqp.Publishing
	err        error
}

func (p *testPublisher) Publish(_, routingKey string, _, _ bool, message amqp.Publishing) error {
	p.routingKey = routingKey
	p.message = message
	return p.err
}

func TestPublishAndConfirm(t *testing.T) {
	tests := []struct {
		name          string
		publisherErr  error
		confirmation  <-chan amqp.Confirmation
		timeout       time.Duration
		wantErr       string
		wantPublished bool
	}{
		{
			name:          "acknowledged",
			confirmation:  confirmationChannel(true),
			timeout:       time.Second,
			wantPublished: true,
		},
		{
			name:          "rejected",
			confirmation:  confirmationChannel(false),
			timeout:       time.Second,
			wantErr:       "RabbitMQ rejected publish",
			wantPublished: true,
		},
		{
			name:          "confirmation channel closed",
			confirmation:  closedConfirmationChannel(),
			timeout:       time.Second,
			wantErr:       "closed the publisher confirmation channel",
			wantPublished: true,
		},
		{
			name:          "confirmation timeout",
			timeout:       time.Millisecond,
			wantErr:       "timed out waiting",
			wantPublished: true,
		},
		{
			name:          "publish error",
			publisherErr:  errors.New("channel failure"),
			timeout:       time.Second,
			wantErr:       "publish to \"work\": channel failure",
			wantPublished: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			publisher := &testPublisher{err: test.publisherErr}
			headers := amqp.Table{"x-retry-count": int64(2)}
			err := PublishAndConfirm(publisher, test.confirmation, "work", []byte("job"), headers, test.timeout)
			if test.wantErr == "" && err != nil {
				t.Fatalf("PublishAndConfirm() error = %v", err)
			}
			if test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
				t.Fatalf("PublishAndConfirm() error = %v, want containing %q", err, test.wantErr)
			}
			if (publisher.routingKey != "") != test.wantPublished {
				t.Fatalf("published = %t, want %t", publisher.routingKey != "", test.wantPublished)
			}
			if test.wantPublished && (publisher.routingKey != "work" ||
				publisher.message.ContentType != "application/json" ||
				publisher.message.DeliveryMode != amqp.Persistent ||
				string(publisher.message.Body) != "job" ||
				publisher.message.Headers["x-retry-count"] != int64(2)) {
				t.Fatalf("unexpected published message: key=%q message=%+v", publisher.routingKey, publisher.message)
			}
		})
	}
}

func TestPublishStatusSetsUTCUpdateTime(t *testing.T) {
	confirmations := confirmationChannel(true)
	publisher := &testPublisher{}
	if err := PublishStatus(publisher, confirmations, "events", StatusEvent{
		JobID: "job-1", Stage: "completed", Message: "done", Attempt: 1,
	}); err != nil {
		t.Fatalf("PublishStatus() error = %v", err)
	}
	if publisher.routingKey != "events" {
		t.Fatalf("routing key = %q, want events", publisher.routingKey)
	}
	var event StatusEvent
	if err := json.Unmarshal(publisher.message.Body, &event); err != nil {
		t.Fatalf("decode published event: %v", err)
	}
	if event.JobID != "job-1" || event.Stage != "completed" || event.Message != "done" || event.Attempt != 1 {
		t.Fatalf("unexpected status event: %+v", event)
	}
	if event.UpdatedAt.IsZero() || event.UpdatedAt.Location() != time.UTC {
		t.Fatalf("UpdatedAt = %v, want a non-zero UTC timestamp", event.UpdatedAt)
	}
}

func confirmationChannel(ack bool) <-chan amqp.Confirmation {
	confirmations := make(chan amqp.Confirmation, 1)
	confirmations <- amqp.Confirmation{Ack: ack}
	return confirmations
}

func closedConfirmationChannel() <-chan amqp.Confirmation {
	confirmations := make(chan amqp.Confirmation)
	close(confirmations)
	return confirmations
}
