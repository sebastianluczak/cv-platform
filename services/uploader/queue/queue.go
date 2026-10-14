package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/streadway/amqp"
)

type Publisher struct {
	channel       *amqp.Channel
	queueName     string
	confirmations <-chan amqp.Confirmation
	mu            sync.Mutex
	failed        error
}

func (p *Publisher) Close() error {
	return p.channel.Close()
}

type ExtractionJob struct {
	ID          string `json:"id"`
	Bucket      string `json:"bucket"`
	ObjectKey   string `json:"object_key"`
	ContentType string `json:"content_type"`
}

type StatusEvent struct {
	JobID     string    `json:"job_id"`
	Stage     string    `json:"stage"`
	Message   string    `json:"message"`
	Attempt   int       `json:"attempt"`
	UpdatedAt time.Time `json:"updated_at"`
}

func ConnectToQueue() (*amqp.Connection, *Publisher, error) {
	rabbitMQURL := os.Getenv("RABBITMQ_URL")
	if rabbitMQURL == "" {
		return nil, nil, fmt.Errorf("RABBITMQ_URL is not set")
	}
	queueName := os.Getenv("QUEUE_NAME")
	if queueName == "" {
		return nil, nil, fmt.Errorf("QUEUE_NAME is not set")
	}

	conn, err := amqp.Dial(rabbitMQURL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		return nil, nil, errors.Join(
			fmt.Errorf("failed to open a channel: %w", err),
			closeWithError("connection", conn),
		)
	}

	_, err = ch.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		return nil, nil, errors.Join(
			fmt.Errorf("failed to declare queue %q: %w", queueName, err),
			closeWithError("channel", ch),
			closeWithError("connection", conn),
		)
	}
	if err := ch.Confirm(false); err != nil {
		return nil, nil, errors.Join(
			fmt.Errorf("failed to enable publisher confirms: %w", err),
			closeWithError("channel", ch),
			closeWithError("connection", conn),
		)
	}

	publisher := &Publisher{
		channel:       ch,
		queueName:     queueName,
		confirmations: ch.NotifyPublish(make(chan amqp.Confirmation, 1)),
	}
	// #nosec G706 -- The configured queue name has CR/LF removed by safeLogField.
	log.Printf("Connected to RabbitMQ and declared queue: %s", safeLogField(queueName))
	return conn, publisher, nil
}

func ConsumeStatusEvents(conn *amqp.Connection, queueName string) (*amqp.Channel, <-chan amqp.Delivery, error) {
	channel, err := conn.Channel()
	if err != nil {
		return nil, nil, fmt.Errorf("open RabbitMQ status channel: %w", err)
	}
	if _, err := channel.QueueDeclare(queueName, true, false, false, false, nil); err != nil {
		return nil, nil, errors.Join(
			fmt.Errorf("declare status queue %q: %w", queueName, err),
			closeWithError("status channel", channel),
		)
	}
	deliveries, err := channel.Consume(queueName, "", false, false, false, false, nil)
	if err != nil {
		return nil, nil, errors.Join(
			fmt.Errorf("consume status queue %q: %w", queueName, err),
			closeWithError("status channel", channel),
		)
	}
	return channel, deliveries, nil
}

func closeWithError(name string, resource interface{ Close() error }) error {
	if err := resource.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	return nil
}

func (p *Publisher) PublishMessage(message ExtractionJob) error {
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode extraction job: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failed != nil {
		return fmt.Errorf("RabbitMQ publisher is unavailable: %w", p.failed)
	}

	err = p.channel.Publish(
		"",
		p.queueName,
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         body,
		},
	)
	if err != nil {
		p.failed = err
		return fmt.Errorf("failed to publish message: %w", err)
	}

	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case confirmation, ok := <-p.confirmations:
		if !ok {
			p.failed = fmt.Errorf("RabbitMQ closed the publisher confirmation channel")
			return p.failed
		}
		if !confirmation.Ack {
			p.failed = fmt.Errorf("RabbitMQ rejected the extraction job")
			return p.failed
		}
	case <-timer.C:
		p.failed = fmt.Errorf("timed out waiting for RabbitMQ to confirm the extraction job")
		return p.failed
	}

	log.Printf("Published extraction job %s for %s/%s",
		safeLogField(message.ID), safeLogField(message.Bucket), safeLogField(message.ObjectKey))
	return nil
}

func safeLogField(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.ReplaceAll(value, "\n", " ")
}
