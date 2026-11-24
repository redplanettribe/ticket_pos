package storage

import (
	"context"
	"io"
	"time"
)

// ObjectStorage provides S3-compatible presigned uploads and public URLs.
//
// Put is the one server-side write: browsers upload through presigned URLs, but
// re-hosting an image the backend fetched itself (a Google Sign-In picture
// seeding a Customer Avatar) has no browser in the loop, and the presigned URL
// is signed for the public endpoint the backend may not be able to reach.
//
// Get is the one server-side read: the email renditions (spec #870) are
// produced from an Event's originals on the server, so the backend must read
// back what a browser uploaded. It reads over the internal endpoint, as Put
// writes, and the caller closes the body.
//
// Delete removes an object the platform no longer references. It exists for
// replace-then-clean-up: a Cover Video or Cover Image that has just been swapped
// out is nothing but bytes on the bill (ADR 0020). Callers commit the database
// first and treat the delete as best-effort — the key is already unreachable, so
// a failure only leaves an orphan.
type ObjectStorage interface {
	PresignPut(ctx context.Context, key, contentType string, expires time.Duration) (uploadURL string, err error)
	Put(ctx context.Context, key, contentType string, body io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	PublicURL(key string) string
}

// CoverUploadResult is returned by the cover presign endpoint.
type CoverUploadResult struct {
	UploadURL string `json:"upload_url"`
	ObjectKey string `json:"object_key"`
	PublicURL string `json:"public_url"`
}
