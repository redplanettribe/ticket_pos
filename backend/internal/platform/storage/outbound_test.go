package storage_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform/reportertest"
	"github.com/peter/ticket_pos/backend/internal/platform/storage"
)

// A STORAGE WRITE IN A SAMPLED REQUEST IS A CHILD SPAN (#769): the S3 SDK is
// given the shared transport through its HTTP client option, and the span is
// named by the method, host and the object's path.
func TestAStorageWriteIsAChildSpan(t *testing.T) {
	bucket := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(bucket.Close)
	store, err := storage.NewS3Storage(storage.S3Config{
		Endpoint: bucket.URL, AccessKey: "minio", SecretKey: "minio123", Bucket: "ticket-pos", Region: "us-east-1",
	})
	if err != nil {
		t.Fatalf("NewS3Storage: %v", err)
	}
	fake := reportertest.New()

	request := fake.InSampledRequest(func(ctx context.Context) {
		if err := store.Put(ctx, "avatars/c1/a.png", "image/png", strings.NewReader("png")); err != nil {
			t.Errorf("put: %v", err)
		}
	})

	fake.OnlyOutboundCall(t, request, "PUT "+bucket.URL+"/ticket-pos/avatars/c1/a.png", http.StatusOK)
}
