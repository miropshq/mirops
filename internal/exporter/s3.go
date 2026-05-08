package exporter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/miropshq/mirops/internal/analysis"
)

// S3Exporter uploads the analysis report to an S3 bucket.
// Credentials are resolved in order:
//  1. Static credentials from accessKeyID/secretAccessKey (fallback secret)
//  2. IRSA / instance profile / environment variables (automatic when empty)
type S3Exporter struct {
	Bucket          string
	Region          string
	Key             string
	AccessKeyID     string
	SecretAccessKey string
}

func (e *S3Exporter) Export(report *analysis.Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling report: %w", err)
	}

	ctx := context.Background()

	var cfg aws.Config
	if e.AccessKeyID != "" && e.SecretAccessKey != "" {
		cfg, err = awsconfig.LoadDefaultConfig(ctx,
			awsconfig.WithRegion(e.Region),
			awsconfig.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider(e.AccessKeyID, e.SecretAccessKey, ""),
			),
		)
	} else {
		// IRSA / instance profile / env vars
		cfg, err = awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(e.Region))
	}
	if err != nil {
		return fmt.Errorf("loading AWS config: %w", err)
	}

	client := s3.NewFromConfig(cfg)
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(e.Bucket),
		Key:         aws.String(e.Key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("uploading report to S3 s3://%s/%s: %w", e.Bucket, e.Key, err)
	}

	return nil
}

func (e *S3Exporter) Location() string {
	return fmt.Sprintf("s3://%s/%s", e.Bucket, e.Key)
}
