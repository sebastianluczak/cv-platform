package main

import (
	"errors"
	"log"
	"net/http"

	"brodaty.dev/cv-platform/common/prometheus"
)

func main() {
	go func() {
		if err := prometheus.StartMetricsServer("METRICS_PORT_ROLEINSIGHTS"); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			log.Printf("Metrics server failed: %v", err)
		}
	}()

	if err := run(); err != nil {
		log.Fatal(err)
	}
}
