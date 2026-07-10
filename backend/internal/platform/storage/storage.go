package storage

import (
	"context"
	"time"
)

// ObjectStorage provides S3-compatible presigned uploads and public URLs.
type ObjectStorage interface {
	PresignPut(ctx context.Context, key, contentType string, expires time.Duration) (uploadURL string, err error)
	PublicURL(key string) string
}

// CoverUploadResult is returned by the cover presign endpoint.
type CoverUploadResult struct {
	UploadURL string `json:"upload_url"`
	ObjectKey string `json:"object_key"`
	PublicURL string `json:"public_url"`
}
