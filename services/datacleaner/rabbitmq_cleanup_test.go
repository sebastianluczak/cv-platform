package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/streadway/amqp"
)

type recordedAcknowledgement struct {
	action  string
	tag     uint64
	requeue bool
}

type acknowledgementRecorder struct {
	actions []recordedAcknowledgement
	err     error
}

func (a *acknowledgementRecorder) Ack(tag uint64, _ bool) error {
	a.actions = append(a.actions, recordedAcknowledgement{action: "ack", tag: tag})
	return a.err
}

func (a *acknowledgementRecorder) Nack(tag uint64, _ bool, requeue bool) error {
	a.actions = append(a.actions, recordedAcknowledgement{action: "nack", tag: tag, requeue: requeue})
	return a.err
}

func (a *acknowledgementRecorder) Reject(tag uint64, requeue bool) error {
	a.actions = append(a.actions, recordedAcknowledgement{action: "reject", tag: tag, requeue: requeue})
	return a.err
}

type cleanupChannelStub struct {
	declareErr    error
	consumeErr    error
	declareErrors map[string]error
	consumeErrors map[string]error
	deliveries    <-chan amqp.Delivery
	declared      []string
	consumed      []string
}

func (c *cleanupChannelStub) QueueDeclare(name string, _, _, _, _ bool, _ amqp.Table) (amqp.Queue, error) {
	c.declared = append(c.declared, name)
	if err := c.declareErrors[name]; err != nil {
		return amqp.Queue{}, err
	}
	if c.declareErr != nil {
		return amqp.Queue{}, c.declareErr
	}
	return amqp.Queue{Name: name}, nil
}

func (c *cleanupChannelStub) Consume(queue, _ string, _, _, _, _ bool, _ amqp.Table) (<-chan amqp.Delivery, error) {
	c.consumed = append(c.consumed, queue)
	if err := c.consumeErrors[queue]; err != nil {
		return nil, err
	}
	if c.consumeErr != nil {
		return nil, c.consumeErr
	}
	return c.deliveries, nil
}

func TestCleanupFailedQueue_AcknowledgesExpiredAndRequeuesRecent(t *testing.T) {
	expiredAck := &acknowledgementRecorder{}
	recentAck := &acknowledgementRecorder{}
	deliveries := make(chan amqp.Delivery, 2)
	deliveries <- amqp.Delivery{
		Acknowledger: expiredAck,
		DeliveryTag:  1,
		Timestamp:    time.Now().Add(-48 * time.Hour),
	}
	deliveries <- amqp.Delivery{
		Acknowledger: recentAck,
		DeliveryTag:  2,
		Timestamp:    time.Now().Add(-time.Hour),
	}
	close(deliveries)

	channel := &cleanupChannelStub{deliveries: deliveries}
	result, err := cleanupFailedQueue(context.Background(), channel, "jobs.extraction.failed", 24*3600, false)
	if err != nil {
		t.Fatalf("cleanupFailedQueue() error = %v", err)
	}
	if result.deleted != 1 {
		t.Errorf("cleanupFailedQueue() deleted = %d, want 1", result.deleted)
	}
	if !reflect.DeepEqual(expiredAck.actions, []recordedAcknowledgement{{action: "ack", tag: 1}}) {
		t.Errorf("expired message acknowledgements = %#v, want ack for delivery 1", expiredAck.actions)
	}
	if !reflect.DeepEqual(recentAck.actions, []recordedAcknowledgement{{action: "nack", tag: 2, requeue: true}}) {
		t.Errorf("recent message acknowledgements = %#v, want requeue nack for delivery 2", recentAck.actions)
	}
	if !reflect.DeepEqual(channel.declared, []string{"jobs.extraction.failed"}) ||
		!reflect.DeepEqual(channel.consumed, []string{"jobs.extraction.failed"}) {
		t.Errorf("broker queue selection: declared=%v consumed=%v", channel.declared, channel.consumed)
	}
}

func TestCleanupFailedQueue_DryRunRetainsExpiredMessage(t *testing.T) {
	acknowledger := &acknowledgementRecorder{}
	deliveries := make(chan amqp.Delivery, 1)
	deliveries <- amqp.Delivery{
		Acknowledger: acknowledger,
		DeliveryTag:  7,
		Timestamp:    time.Now().Add(-48 * time.Hour),
	}
	close(deliveries)

	result, err := cleanupFailedQueue(context.Background(), &cleanupChannelStub{deliveries: deliveries}, "jobs.pii.failed", 24*3600, true)
	if err != nil {
		t.Fatalf("cleanupFailedQueue() error = %v", err)
	}
	if result.deleted != 0 || result.wouldDelete != 1 {
		t.Errorf("cleanupFailedQueue() result = %#v, want 1 would-be purge and no deletion", result)
	}
	if !reflect.DeepEqual(acknowledger.actions, []recordedAcknowledgement{{action: "nack", tag: 7, requeue: true}}) {
		t.Errorf("dry-run acknowledgements = %#v, want only a requeue nack", acknowledger.actions)
	}
}

func TestCleanupFailedQueue_MissingOrInvalidTimestampIsRetained(t *testing.T) {
	tests := []struct {
		name    string
		headers amqp.Table
		body    []byte
	}{
		{name: "missing timestamp"},
		{
			name:    "invalid timestamp",
			headers: amqp.Table{"x-publish-time": "not-a-time"},
			body:    []byte(`{"timestamp":`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acknowledger := &acknowledgementRecorder{}
			deliveries := make(chan amqp.Delivery, 1)
			deliveries <- amqp.Delivery{
				Acknowledger: acknowledger,
				DeliveryTag:  9,
				Headers:      tt.headers,
				Body:         tt.body,
			}
			close(deliveries)

			result, err := cleanupFailedQueue(context.Background(), &cleanupChannelStub{deliveries: deliveries}, "jobs.skills.failed", 24*3600, false)
			if err != nil {
				t.Fatalf("cleanupFailedQueue() error = %v", err)
			}
			if result.deleted != 0 {
				t.Errorf("cleanupFailedQueue() deleted = %d, want 0", result.deleted)
			}
			if !reflect.DeepEqual(acknowledger.actions, []recordedAcknowledgement{{action: "nack", tag: 9, requeue: true}}) {
				t.Errorf("timestamp-less message acknowledgements = %#v, want requeue nack", acknowledger.actions)
			}
		})
	}
}

func TestParseMessageTimestamp_ExtractsTimestamp(t *testing.T) {
	want := time.Date(2024, time.January, 15, 10, 30, 0, 0, time.FixedZone("offset", 3600))
	tests := []struct {
		name     string
		delivery amqp.Delivery
	}{
		{
			name:     "delivery timestamp",
			delivery: amqp.Delivery{Timestamp: want},
		},
		{
			name: "header timestamp",
			delivery: amqp.Delivery{
				Headers: amqp.Table{"x-publish-time": want.Format(time.RFC3339)},
			},
		},
		{
			name: "body timestamp",
			delivery: amqp.Delivery{
				Body: []byte(fmt.Sprintf(`{"timestamp":%q}`, want.Format(time.RFC3339))),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMessageTimestamp(tt.delivery)
			if err != nil {
				t.Fatalf("parseMessageTimestamp() error = %v", err)
			}
			if !got.Equal(want) {
				t.Errorf("parseMessageTimestamp() = %v, want %v", got, want)
			}
		})
	}
}

func TestCleanupFailedQueue_Cancellation(t *testing.T) {
	deliveries := make(chan amqp.Delivery)
	channel := &cleanupChannelStub{deliveries: deliveries}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := cleanupFailedQueue(ctx, channel, "jobs.offers.failed", 24*3600, false)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cleanupFailedQueue() error = %v, want context.Canceled", err)
	}
	if result.deleted != 0 || result.failures != 1 {
		t.Errorf("cleanupFailedQueue() result = %#v, want one failure and no deletions", result)
	}
	if !reflect.DeepEqual(channel.consumed, []string{"jobs.offers.failed"}) {
		t.Errorf("consumed queues = %v, want [jobs.offers.failed]", channel.consumed)
	}
}

func TestCleanupFailedQueue_BrokerOperationFailures(t *testing.T) {
	tests := []struct {
		name       string
		declareErr error
		consumeErr error
		wantOp     string
	}{
		{name: "queue declaration", declareErr: errors.New("declare failed"), wantOp: "declare queue"},
		{name: "consume", consumeErr: errors.New("consume failed"), wantOp: "consume from queue"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &cleanupChannelStub{
				declareErr: tt.declareErr,
				consumeErr: tt.consumeErr,
			}
			_, err := cleanupFailedQueue(context.Background(), channel, "jobs.aiweight.failed", 24*3600, false)
			if err == nil || !strings.Contains(err.Error(), tt.wantOp) {
				t.Errorf("cleanupFailedQueue() error = %v, want operation context %q", err, tt.wantOp)
			}
			if !reflect.DeepEqual(channel.declared, []string{"jobs.aiweight.failed"}) {
				t.Errorf("declared queues = %v, want [jobs.aiweight.failed]", channel.declared)
			}
			wantConsumed := []string{"jobs.aiweight.failed"}
			if tt.declareErr != nil {
				wantConsumed = nil
			}
			if !reflect.DeepEqual(channel.consumed, wantConsumed) {
				t.Errorf("consumed queues = %v, want %v", channel.consumed, wantConsumed)
			}
		})
	}
}

func TestCleanupRabbitMQ_SelectsFailedQueues(t *testing.T) {
	deliveries := make(chan amqp.Delivery)
	close(deliveries)
	channel := &cleanupChannelStub{deliveries: deliveries}
	config := Config{QueueName: "jobs", RetentionFailed: 1}

	if _, err := CleanupRabbitMQ(context.Background(), channel, config); err != nil {
		t.Fatalf("CleanupRabbitMQ() error = %v", err)
	}

	want := []string{
		"jobs.failed",
		"jobs.pii.failed",
		"jobs.skills.failed",
		"jobs.offers.failed",
		"jobs.aiweight.failed",
		"jobs.roleinsights.failed",
	}
	if !reflect.DeepEqual(channel.declared, want) || !reflect.DeepEqual(channel.consumed, want) {
		t.Errorf("broker queue selection: declared=%v consumed=%v, want %v", channel.declared, channel.consumed, want)
	}
}

func TestCleanupRabbitMQ_ContinuesAfterQueueFailure(t *testing.T) {
	deliveries := make(chan amqp.Delivery)
	close(deliveries)
	channel := &cleanupChannelStub{
		deliveries:    deliveries,
		declareErrors: map[string]error{"jobs.pii.failed": errors.New("declare failed")},
	}
	config := Config{QueueName: "jobs", RetentionFailed: 1}

	result, err := CleanupRabbitMQ(context.Background(), channel, config)
	if err == nil || !strings.Contains(err.Error(), "jobs.pii.failed") {
		t.Fatalf("CleanupRabbitMQ() error = %v, want failed queue in error", err)
	}
	if result.failures != 1 {
		t.Errorf("CleanupRabbitMQ() failures = %d, want 1", result.failures)
	}
	wantDeclared := []string{
		"jobs.failed",
		"jobs.pii.failed",
		"jobs.skills.failed",
		"jobs.offers.failed",
		"jobs.aiweight.failed",
		"jobs.roleinsights.failed",
	}
	wantConsumed := []string{
		"jobs.failed",
		"jobs.skills.failed",
		"jobs.offers.failed",
		"jobs.aiweight.failed",
		"jobs.roleinsights.failed",
	}
	if !reflect.DeepEqual(channel.declared, wantDeclared) || !reflect.DeepEqual(channel.consumed, wantConsumed) {
		t.Errorf("cleanup after queue error: declared=%v consumed=%v", channel.declared, channel.consumed)
	}
}

func TestCleanupFailedQueue_AcknowledgementFailureIsReturned(t *testing.T) {
	acknowledger := &acknowledgementRecorder{err: errors.New("ack failed")}
	deliveries := make(chan amqp.Delivery, 1)
	deliveries <- amqp.Delivery{
		Acknowledger: acknowledger,
		DeliveryTag:  3,
		Timestamp:    time.Now().Add(-48 * time.Hour),
	}
	close(deliveries)

	result, err := cleanupFailedQueue(context.Background(), &cleanupChannelStub{deliveries: deliveries}, "jobs.failed", 24*3600, false)
	if err == nil || !strings.Contains(err.Error(), "acknowledge expired message") {
		t.Fatalf("cleanupFailedQueue() error = %v, want acknowledgement error", err)
	}
	if result.deleted != 0 || result.failures != 1 {
		t.Errorf("cleanupFailedQueue() result = %#v, want one failure and no deletion", result)
	}
}
