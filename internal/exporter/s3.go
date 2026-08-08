package exporter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

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

// loadConfig resolves AWS credentials: static keys when provided, otherwise the default chain
// (IRSA / instance profile / env vars).
func (e *S3Exporter) loadConfig(ctx context.Context) (aws.Config, error) {
	if e.AccessKeyID != "" && e.SecretAccessKey != "" {
		return awsconfig.LoadDefaultConfig(ctx,
			awsconfig.WithRegion(e.Region),
			awsconfig.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider(e.AccessKeyID, e.SecretAccessKey, ""),
			),
		)
	}
	return awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(e.Region))
}

func (e *S3Exporter) Export(report *analysis.Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling report: %w", err)
	}

	ctx := context.Background()
	cfg, err := e.loadConfig(ctx)
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

func (e *S3Exporter) Read() ([]byte, error) {
	ctx := context.Background()
	cfg, err := e.loadConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	client := s3.NewFromConfig(cfg)
	out, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(e.Bucket),
		Key:    aws.String(e.Key),
	})
	if err != nil {
		return nil, fmt.Errorf("reading report from S3 s3://%s/%s: %w", e.Bucket, e.Key, err)
	}
	defer func() { _ = out.Body.Close() }()
	return io.ReadAll(out.Body)
}

func (e *S3Exporter) Location() string {
	return fmt.Sprintf("s3://%s/%s", e.Bucket, e.Key)
}
