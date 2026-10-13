package buckets

import (
	"context"
	"fmt"
	"log"

	"brodaty.dev/cv-platform/uploader/buckets/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func ListOurBuckets() {
	config := config.GetBucketConfig()

	cfg := aws.Config{
		Region:      config.Region,
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(config.AccessKeyID, config.SecretAccessKey, "")),
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(config.Endpoint)
		o.UsePathStyle = true
	})
	ctx := context.Background()

	resp, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		log.Fatalf("list buckets failed: %v", err)
	}
	fmt.Println("Buckets:")
	for _, b := range resp.Buckets {
		fmt.Println(" -", *b.Name)
	}
	if len(resp.Buckets) == 0 {
		fmt.Println("No buckets found, creating one...")
		_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
			Bucket: aws.String("cv-platform-uploads"),
		})
		if err != nil {
			log.Fatalf("create bucket failed: %v", err)
		}
		fmt.Println("Bucket created: cv-platform-uploads")
	}
}
