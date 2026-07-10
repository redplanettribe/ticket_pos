package storage

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPresignPutUsesPublicURL(t *testing.T) {
	storage, err := NewS3Storage(S3Config{
		Endpoint:  "http://minio:9000",
		PublicURL: "http://localhost:9002",
		AccessKey: "minio",
		SecretKey: "minio123",
		Bucket:    "ticket-pos",
		Region:    "us-east-1",
	})
	if err != nil {
		t.Fatalf("NewS3Storage: %v", err)
	}

	uploadURL, err := storage.PresignPut(context.Background(), "covers/org/event/file.png", "image/png", time.Minute)
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}

	if !strings.HasPrefix(uploadURL, "http://localhost:9002/ticket-pos/") {
		t.Fatalf("upload URL should use public host, got %q", uploadURL)
	}
	if strings.Contains(uploadURL, "minio:9000") {
		t.Fatalf("upload URL must not use internal host, got %q", uploadURL)
	}
}

func TestPublicURL(t *testing.T) {
	storage, err := NewS3Storage(S3Config{
		Endpoint:  "http://minio:9000",
		PublicURL: "http://localhost:9002",
		AccessKey: "minio",
		SecretKey: "minio123",
		Bucket:    "ticket-pos",
		Region:    "us-east-1",
	})
	if err != nil {
		t.Fatalf("NewS3Storage: %v", err)
	}

	got := storage.PublicURL("covers/org/event/file.png")
	want := "http://localhost:9002/ticket-pos/covers/org/event/file.png"
	if got != want {
		t.Fatalf("PublicURL=%q want %q", got, want)
	}
}
