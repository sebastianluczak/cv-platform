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
	s3Cli, ok := s3Client.(*s3.Client)
	if !ok {
		return runCleanupCycleWith(ctx, time.Duration(cfg.CleanupInterval)*time.Minute,
			func(context.Context) (cleanupResult, error) {
				return cleanupResult{failures: 1}, fmt.Errorf("invalid s3 client type")
			},
			func(ctx context.Context) (cleanupResult, error) {
				return cleanupFailedQueues(ctx, conn, cfg)
			},
		)
	}

	return runCleanupCycleWith(ctx, time.Duration(cfg.CleanupInterval)*time.Minute,
		func(ctx context.Context) (cleanupResult, error) {
			return CleanupS3(ctx, s3Cli, cfg.S3Bucket, cfg)
		},
		func(ctx context.Context) (cleanupResult, error) {
			return cleanupFailedQueues(ctx, conn, cfg)
		},
	)
}

func runCleanupCycleWith(
	ctx context.Context,
	interval time.Duration,
	cleanupS3 func(context.Context) (cleanupResult, error),
	cleanupRabbitMQ func(context.Context) (cleanupResult, error),
) error {
	cycleStart := time.Now()
	LogCleanupStart(interval)

	s3Result, s3Err := cleanupS3(ctx)
	if s3Err != nil {
		LogError("s3_cleanup", s3Err)
		if s3Result.failures == 0 {
			s3Result.failures++
		}
	}

	rabbitMQResult, rabbitMQErr := cleanupRabbitMQ(ctx)
	if rabbitMQErr != nil {
		LogError("rabbitmq_cleanup", rabbitMQErr)
		if rabbitMQResult.failures == 0 {
			rabbitMQResult.failures++
		}
	}

	LogCleanupCycle(time.Since(cycleStart), s3Result, rabbitMQResult)
	if s3Err != nil || rabbitMQErr != nil {
		return errors.Join(
			wrapCleanupError("S3 cleanup", s3Err),
			wrapCleanupError("RabbitMQ cleanup", rabbitMQErr),
		)
	}
	return nil
}

func wrapCleanupError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func cleanupFailedQueues(ctx context.Context, conn *amqp.Connection, cfg Config) (cleanupResult, error) {
	// Create a channel for RabbitMQ operations
	channel, err := conn.Channel()
	if err != nil {
		return cleanupResult{failures: 1}, fmt.Errorf("open RabbitMQ channel for cleanup: %w", err)
	}
	defer channel.Close()

	// Call the RabbitMQ cleanup orchestrator
	result, err := CleanupRabbitMQ(ctx, channel, cfg)
	if err != nil {
		return result, fmt.Errorf("RabbitMQ cleanup: %w", err)
	}

	return result, nil
}
