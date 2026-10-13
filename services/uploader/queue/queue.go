package queue

import (
	"fmt"
	"log"
	"os"

	"github.com/streadway/amqp"
)

var (
	rabbitMQURL = os.Getenv("RABBITMQ_URL")
	queueName   = os.Getenv("QUEUE_NAME")
)

func ConnectToQueue() (*amqp.Connection, *amqp.Channel, error) {
	conn, err := amqp.Dial(rabbitMQURL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("failed to open a channel: %w", err)
	}

	_, err = ch.QueueDeclare(
		queueName,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, nil, fmt.Errorf("failed to declare a queue: %w", err)
	}

	log.Printf("Connected to RabbitMQ and declared queue: %s", queueName)
	return conn, ch, nil
}

func PublishMessage(ch *amqp.Channel, message string) error {
	err := ch.Publish(
		"",
		queueName,
		false,
		false,
		amqp.Publishing{
			ContentType: "text/plain",
			Body:        []byte(message),
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish a message: %w", err)
	}

	log.Printf("Published message to queue %s: %s", queueName, message)
	return nil
}
