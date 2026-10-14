package queue

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
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
		conn.Close()
		return nil, nil, fmt.Errorf("failed to open a channel: %w", err)
	}

	_, err = ch.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, nil, fmt.Errorf("failed to declare queue %q: %w", queueName, err)
	}
	if err := ch.Confirm(false); err != nil {
		ch.Close()
		conn.Close()
		return nil, nil, fmt.Errorf("failed to enable publisher confirms: %w", err)
	}

	publisher := &Publisher{
		channel:       ch,
		queueName:     queueName,
		confirmations: ch.NotifyPublish(make(chan amqp.Confirmation, 1)),
	}
	log.Printf("Connected to RabbitMQ and declared queue: %s", queueName)
	return conn, publisher, nil
}

func ConsumeStatusEvents(conn *amqp.Connection, queueName string) (*amqp.Channel, <-chan amqp.Delivery, error) {
	channel, err := conn.Channel()
	if err != nil {
		return nil, nil, fmt.Errorf("open RabbitMQ status channel: %w", err)
	}
	if _, err := channel.QueueDeclare(queueName, true, false, false, false, nil); err != nil {
		channel.Close()
		return nil, nil, fmt.Errorf("declare status queue %q: %w", queueName, err)
	}
	deliveries, err := channel.Consume(queueName, "", false, false, false, false, nil)
	if err != nil {
		channel.Close()
		return nil, nil, fmt.Errorf("consume status queue %q: %w", queueName, err)
	}
	return channel, deliveries, nil
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

	log.Printf("Published extraction job %s for %s/%s", message.ID, message.Bucket, message.ObjectKey)
	return nil
}
