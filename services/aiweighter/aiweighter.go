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

const defaultModel = "nvidia/nemotron-3-super-120b-a12b:free"

func run() error {
	rabbitMQURL := strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	if rabbitMQURL == "" {
		return fmt.Errorf("RABBITMQ_URL is not set")
	}
	queueName := strings.TrimSpace(os.Getenv("QUEUE_NAME"))
	if queueName == "" {
		return fmt.Errorf("QUEUE_NAME is not set")
	}
	apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		return fmt.Errorf("OPENROUTER_API_KEY is not set")
	}
	model := strings.TrimSpace(os.Getenv("OPENROUTER_MODEL"))
	if model == "" {
		model = defaultModel
	}
	port := strings.TrimSpace(os.Getenv("AIWEIGHTER_PORT"))
	if port == "" {
		port = "8082"
	}

	connection, err := amqp.Dial(rabbitMQURL)
	if err != nil {
		return fmt.Errorf("connect to RabbitMQ: %w", err)
	}
	defer connection.Close()

	workQueue := queueName + ".aiweight"
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
		return fmt.Errorf("open worker publisher channel: %w", err)
	}
	defer workerPublisherChannel.Close()
	if err := workerPublisherChannel.Confirm(false); err != nil {
		return fmt.Errorf("enable worker publisher confirms: %w", err)
	}
	workerConfirmations := workerPublisherChannel.NotifyPublish(make(chan amqp.Confirmation, 1))

	apiPublisherChannel, err := connection.Channel()
	if err != nil {
		return fmt.Errorf("open API publisher channel: %w", err)
	}
	defer apiPublisherChannel.Close()
	if err := apiPublisherChannel.Confirm(false); err != nil {
		return fmt.Errorf("enable API publisher confirms: %w", err)
	}
	apiConfirmations := apiPublisherChannel.NotifyPublish(make(chan amqp.Confirmation, 1))

	storageClient, storageConfig := objectstore.NewClient()
	modelClient := newOpenRouterClient(apiKey, model)
	handler := newAIweighterHandler(
		storageClient, storageConfig.Bucket, workQueue,
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
		workerErr <- consumeWeights(
			deliveries, workerPublisherChannel, workerConfirmations,
			workQueue, failedQueue, storageConfig.Bucket, storageClient, modelClient,
		)
	}()
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe()
	}()
	log.Printf("AIweighter listening on port %q; queue %q; bucket %q; model %q",
		safeLogField(port), safeLogField(workQueue), safeLogField(storageConfig.Bucket), safeLogField(model))

	select {
	case err := <-serverErr:
		if err != http.ErrServerClosed {
			return fmt.Errorf("serve AIweighter HTTP API: %w", err)
		}
		return nil
	case err := <-workerErr:
		_ = server.Close()
		return err
	}
}
