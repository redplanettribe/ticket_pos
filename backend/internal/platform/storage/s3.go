package storage

import (
	"context"
	"fmt"
	"io"
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
	presign *s3.PresignClient
	client  *s3.Client
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

	// Presigned URLs must use the browser-reachable host. Signing is local, so the
	// presign client does not need network access to the public endpoint.
	presignClient, err := newS3Client(publicURL, cfg.AccessKey, cfg.SecretKey, region)
	if err != nil {
		return nil, err
	}

	// Server-side writes go over the internal endpoint, which is the one the
	// backend can actually reach (the public host may only resolve for browsers).
	client, err := newS3Client(endpoint, cfg.AccessKey, cfg.SecretKey, region)
	if err != nil {
		return nil, err
	}

	return &S3Storage{
		presign: s3.NewPresignClient(presignClient),
		client:  client,
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

func newS3Client(endpoint, accessKey, secretKey, region string) (*s3.Client, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return nil, fmt.Errorf("S3 endpoint is required")
	}

	resolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...any) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL:               endpoint,
			HostnameImmutable: true,
		}, nil
	})

	awsCfg := aws.Config{
		Region:                      region,
		Credentials:                 credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		EndpointResolverWithOptions: resolver,
	}

	return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = true
	}), nil
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

// Put writes an object directly from the server over the internal endpoint.
func (s *S3Storage) Put(ctx context.Context, key, contentType string, body io.Reader) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.cfg.Bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
		Body:        body,
	})
	if err != nil {
		return fmt.Errorf("put object: %w", err)
	}
	return nil
}

// PublicURL returns the browser-accessible URL for an object key.
func (s *S3Storage) PublicURL(key string) string {
	key = strings.TrimPrefix(key, "/")
	return fmt.Sprintf("%s/%s/%s", s.cfg.PublicURL, s.cfg.Bucket, key)
}
