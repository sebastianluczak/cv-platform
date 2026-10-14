// Package objectstore configures the S3-compatible client shared by services.
package objectstore

import (
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Config struct {
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Endpoint        string
	Bucket          string
}

// NewClient creates an S3 client from the shared RustFS environment settings.
func NewClient() (*s3.Client, Config) {
	config := configFromEnv(os.Getenv)
	client := s3.NewFromConfig(aws.Config{
		Region: config.Region,
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(
			config.AccessKeyID,
			config.SecretAccessKey,
			"",
		)),
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(config.Endpoint)
		options.UsePathStyle = true
	})
	return client, config
}

func configFromEnv(getenv func(string) string) Config {
	return Config{
		Region:          envOrDefault(getenv, "RUSTFS_REGION", "us-east-1"),
		AccessKeyID:     envOrDefault(getenv, "RUSTFS_ACCESS_KEY_ID", "rustfsadmin"),
		SecretAccessKey: envOrDefault(getenv, "RUSTFS_SECRET_ACCESS_KEY", "rustfsadmin"),
		Endpoint:        envOrDefault(getenv, "RUSTFS_ENDPOINT_URL", "http://localhost:9000"),
		Bucket:          envOrDefault(getenv, "RUSTFS_BUCKET", "cv-platform-uploads"),
	}
}

func envOrDefault(getenv func(string) string, name, fallback string) string {
	if value := strings.TrimSpace(getenv(name)); value != "" {
		return value
	}
	return fallback
}
