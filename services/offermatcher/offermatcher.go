package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"brodaty.dev/cv-platform/common/objectstore"
	"github.com/streadway/amqp"
)

func run() error {
	rabbitMQURL := strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	if rabbitMQURL == "" {
		return fmt.Errorf("RABBITMQ_URL is not set")
	}
	queueName := strings.TrimSpace(os.Getenv("QUEUE_NAME"))
	if queueName == "" {
		return fmt.Errorf("QUEUE_NAME is not set")
	}
	port := strings.TrimSpace(os.Getenv("OFFERMATCHER_PORT"))
	if port == "" {
		port = "8081"
	}

	connection, err := amqp.Dial(rabbitMQURL)
	if err != nil {
		return fmt.Errorf("connect to RabbitMQ: %w", err)
	}
	defer connection.Close()

	workQueue := queueName + ".offers"
	failedQueue := workQueue + ".failed"
	consumerChannel, err := connection.Channel()
	if err != nil {
		return fmt.Errorf("open RabbitMQ consumer channel: %w", err)
	}
	defer consumerChannel.Close()
	for _, name := range []string{workQueue, failedQueue} {
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

	workerPublisherChannel, err := connection.Channel()
	if err != nil {
		return fmt.Errorf("open RabbitMQ worker publisher channel: %w", err)
	}
	defer workerPublisherChannel.Close()
	if err := workerPublisherChannel.Confirm(false); err != nil {
		return fmt.Errorf("enable worker publisher confirms: %w", err)
	}
	workerConfirmations := workerPublisherChannel.NotifyPublish(make(chan amqp.Confirmation, 1))

	apiPublisherChannel, err := connection.Channel()
	if err != nil {
		return fmt.Errorf("open RabbitMQ API publisher channel: %w", err)
	}
	defer apiPublisherChannel.Close()
	if err := apiPublisherChannel.Confirm(false); err != nil {
		return fmt.Errorf("enable API publisher confirms: %w", err)
	}
	apiConfirmations := apiPublisherChannel.NotifyPublish(make(chan amqp.Confirmation, 1))

	storageClient, config := objectstore.NewClient()
	handler := newOfferMatcherHandler(
		storageClient, config.Bucket, workQueue,
		&confirmedPublisher{channel: apiPublisherChannel, confirmations: apiConfirmations},
	)
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	workerErr := make(chan error, 1)
	go func() {
		workerErr <- consumeOffers(
			deliveries, workerPublisherChannel, workerConfirmations,
			workQueue, failedQueue, config.Bucket, storageClient,
		)
	}()
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe()
	}()
	// #nosec G706 -- Dynamic fields have CR/LF removed by safeLogField.
	log.Printf("Offer matcher listening on port %q; queue %q; bucket %q",
		safeLogField(port), safeLogField(workQueue), safeLogField(config.Bucket))

	select {
	case err := <-serverErr:
		if err != http.ErrServerClosed {
			return fmt.Errorf("serve offer matcher HTTP API: %w", err)
		}
		return nil
	case err := <-workerErr:
		_ = server.Close()
		return err
	}
}
