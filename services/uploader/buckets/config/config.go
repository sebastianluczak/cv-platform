package config

import (
	"log"
	"os"
)

type BucketConfig struct {
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Endpoint        string
	Bucket          string
}

func GetBucketConfig() BucketConfig {
	region := os.Getenv("RUSTFS_REGION")
	if region == "" {
		log.Default().Println("RUSTFS_REGION not set, defaulting to us-east-1")
		region = "us-east-1"
	}
	accessKeyID := os.Getenv("RUSTFS_ACCESS_KEY_ID")
	if accessKeyID == "" {
		log.Default().Println("RUSTFS_ACCESS_KEY_ID not set, defaulting to rustfsadmin")
		accessKeyID = "rustfsadmin"
	}
	secretAccessKey := os.Getenv("RUSTFS_SECRET_ACCESS_KEY")
	if secretAccessKey == "" {
		log.Default().Println("RUSTFS_SECRET_ACCESS_KEY not set, defaulting to rustfsadmin")
		secretAccessKey = "rustfsadmin"
	}
	endpoint := os.Getenv("RUSTFS_ENDPOINT_URL")
	if endpoint == "" {
		log.Default().Println("RUSTFS_ENDPOINT_URL not set, defaulting to http://localhost:9000")
		endpoint = "http://localhost:9000"
	}
	bucket := os.Getenv("RUSTFS_BUCKET")
	if bucket == "" {
		bucket = "cv-platform-uploads"
	}

	return BucketConfig{
		Region:          region,
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
		Endpoint:        endpoint,
		Bucket:          bucket,
	}
}
