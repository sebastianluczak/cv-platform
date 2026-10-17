package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"brodaty.dev/cv-platform/common/prometheus"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	go func() {
		if err := prometheus.StartMetricsServer("METRICS_PORT_MARKETINSIGHTS"); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			log.Printf("Metrics server failed: %v", err)
		}
	}()

	dataFile := strings.TrimSpace(os.Getenv("MARKET_DATA_FILE"))
	if dataFile == "" {
		dataFile = "data/market.json"
	}
	store, err := NewStore(dataFile)
	if err != nil {
		return fmt.Errorf("load market observations: %w", err)
	}
	port := strings.TrimSpace(os.Getenv("MARKETINSIGHTS_PORT"))
	if port == "" {
		port = "8084"
	}
	importToken := os.Getenv("MARKET_IMPORT_TOKEN")
	if importToken != "" && len([]byte(importToken)) < 32 {
		return fmt.Errorf("MARKET_IMPORT_TOKEN must be at least 32 bytes when set")
	}
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           NewAPI(store, importToken),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()
	log.Printf("Market insights listening on :%s", port)
	log.Printf("Market data store: %s", store.path)
	log.Printf("Market data stored insights: %v", store.Snapshot())

	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve market insights API: %w", err)
		}
		return nil
	case <-signals.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			return fmt.Errorf("shut down market insights API: %w", err)
		}
		return nil
	}
}
