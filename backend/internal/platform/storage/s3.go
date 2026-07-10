package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Config holds S3-compatible object storage settings.
type S3Config struct {
	Endpoint  string
	PublicURL string
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
}

// S3Storage implements ObjectStorage against an S3-compatible endpoint (MinIO, AWS S3).
type S3Storage struct {
	client  *s3.Client
	presign *s3.PresignClient
	cfg     S3Config
}

// NewS3Storage constructs an S3-compatible storage client.
func NewS3Storage(cfg S3Config) (*S3Storage, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf("S3 endpoint is required")
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("S3 bucket is required")
	}
	if strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, fmt.Errorf("S3 credentials are required")
	}

	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = "us-east-1"
	}

	publicURL := strings.TrimRight(strings.TrimSpace(cfg.PublicURL), "/")
	if publicURL == "" {
		publicURL = strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	}

	endpoint := strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")

	resolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...any) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL:               endpoint,
			HostnameImmutable: true,
		}, nil
	})

	awsCfg := aws.Config{
		Region:                      region,
		Credentials:                 credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		EndpointResolverWithOptions: resolver,
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = true
	})

	return &S3Storage{
		client:  client,
		presign: s3.NewPresignClient(client),
		cfg: S3Config{
			Endpoint:  endpoint,
			PublicURL: publicURL,
			AccessKey: cfg.AccessKey,
			SecretKey: cfg.SecretKey,
			Bucket:    cfg.Bucket,
			Region:    region,
		},
	}, nil
}

// PresignPut returns a presigned PUT URL for the given object key.
func (s *S3Storage) PresignPut(ctx context.Context, key, contentType string, expires time.Duration) (string, error) {
	if expires <= 0 {
		expires = 15 * time.Minute
	}

	result, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.cfg.Bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(expires))
	if err != nil {
		return "", fmt.Errorf("presign put object: %w", err)
	}
	return result.URL, nil
}

// PublicURL returns the browser-accessible URL for an object key.
func (s *S3Storage) PublicURL(key string) string {
	key = strings.TrimPrefix(key, "/")
	return fmt.Sprintf("%s/%s/%s", s.cfg.PublicURL, s.cfg.Bucket, key)
}
