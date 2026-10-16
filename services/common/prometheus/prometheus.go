package prometheus

import (
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const DefaultMetricsPort = "8050"

func StartMetricsServer(portEnvVar string) error {
	port := os.Getenv(portEnvVar)
	if port == "" {
		port = DefaultMetricsPort
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}

	return server.ListenAndServe()

}
