package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"brodaty.dev/cv-platform/common/objectstore"
	"brodaty.dev/cv-platform/common/prometheus"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/streadway/amqp"
)

func main() {
	go func() {
		if err := prometheus.StartMetricsServer("METRICS_PORT_DATACLEANER"); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			log.Printf("Metrics server failed: %v", err)
		}
	}()

	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := LoadConfig()
	if err != nil {
		LogStartupError("Failed to load configuration", err)
		return fmt.Errorf("load config: %w", err)
	}

	LogStartup(cfg)

	// Connect to RabbitMQ
	conn, err := amqp.Dial(cfg.RabbitMQURL)
	if err != nil {
		LogStartupError("Failed to connect to RabbitMQ", err)
		return fmt.Errorf("connect to RabbitMQ: %w", err)
	}
	defer conn.Close()

	// Initialize S3 client
	storageClient, _ := objectstore.NewClient()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	// Main cleanup loop
	ticker := time.NewTicker(time.Duration(cfg.CleanupInterval) * time.Minute)
	defer ticker.Stop()

	log.Println("Datacleaner started, waiting for cleanup intervals...")

	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			if err := runCleanupCycle(ctx, storageClient, conn, cfg); err != nil {
				LogError("cleanup_cycle", err)
			}
			cancel()

		case <-sigChan:
			log.Println("Received shutdown signal, gracefully exiting...")
			return nil
		}
	}
}

func runCleanupCycle(ctx context.Context, s3Client interface{}, conn *amqp.Connection, cfg Config) error {
	cycleStart := time.Now()
	s3Deleted := 0
	mqPurged := 0

	LogCleanupStart(time.Duration(cfg.CleanupInterval) * time.Minute)

	// Cast s3Client to *s3.Client for S3 cleanup
	s3Cli, ok := s3Client.(*s3.Client)
	if !ok {
		LogError("cleanup_s3", fmt.Errorf("invalid s3 client type"))
		return fmt.Errorf("invalid s3 client type")
	}

	// Run S3 cleanup
	if err := CleanupS3(ctx, s3Cli, cfg.S3Bucket, cfg); err != nil {
		LogError("s3_cleanup", err)
	}

	// TODO: Implement RabbitMQ failed queue cleanup logic
	// - Connect to failed queues: <QUEUE_NAME>.*.failed
	// - Process failed messages older than RetentionFailed
	// - Archive or delete expired failed messages
	if err := cleanupFailedQueues(ctx, conn, cfg); err != nil {
		LogError("rabbitmq_cleanup", err)
	}

	duration := time.Since(cycleStart)
	LogCleanupCycle(duration, s3Deleted, mqPurged)
	return nil
}

func cleanupFailedQueues(ctx context.Context, conn *amqp.Connection, cfg Config) error {
	// Create a channel for RabbitMQ operations
	channel, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open RabbitMQ channel for cleanup: %w", err)
	}
	defer channel.Close()

	// Call the RabbitMQ cleanup orchestrator
	if err := CleanupRabbitMQ(ctx, channel, cfg); err != nil {
		return fmt.Errorf("RabbitMQ cleanup: %w", err)
	}

	return nil
}
