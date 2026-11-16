package service

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/reportertest"
)

// discardStorage accepts every write.
type discardStorage struct{}

func (discardStorage) PresignPut(context.Context, string, string, time.Duration) (string, error) {
	return "", nil
}
func (discardStorage) Put(_ context.Context, _, _ string, body io.Reader) error {
	_, err := io.Copy(io.Discard, body)
	return err
}
func (discardStorage) Delete(context.Context, string) error { return nil }
func (discardStorage) PublicURL(key string) string          { return key }

// A GOOGLE PICTURE FETCH IN A SAMPLED REQUEST IS A CHILD SPAN (#769), named
// by the picture's method, host and path, never its query string. The
// service is the one New builds; nothing here reaches its repository.
func TestAnAvatarFetchIsAChildSpanWithoutItsQueryString(t *testing.T) {
	google := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png"))
	}))
	t.Cleanup(google.Close)
	logger := platform.NewSlogLogger(slog.New(slog.DiscardHandler), platform.NoopReporter{})
	svc := New(nil, nil, logger, ConfirmationLinkConfig{}, nil, nil).WithObjectStorage(discardStorage{})
	fake := reportertest.New()

	request := fake.InSampledRequest(func(ctx context.Context) {
		if _, err := svc.fetchAndStoreAvatar(ctx, "6f1c1f3e-8f55-4d43-9d1c-4f8a8b6c2d10", google.URL+"/a/picture-id?sz=96&email=ana.perez%40example.com"); err != nil {
			t.Errorf("fetch: %v", err)
		}
	})

	children := fake.ChildrenOf(request)
	if len(children) != 1 {
		t.Fatalf("child spans = %d, want the picture fetch", len(children))
	}
	span := children[0]
	want := "GET " + google.URL + "/a/picture-id"
	if span.Options.Op != "http.client" || span.Options.Name != want || !span.Finished || span.HTTPStatus != http.StatusOK {
		t.Errorf("span = %s %q finished %v status %d, want http.client %q finished with 200",
			span.Options.Op, span.Options.Name, span.Finished, span.HTTPStatus, want)
	}
	for _, value := range span.Tags {
		if strings.Contains(value, "ana.perez") || strings.Contains(value, "sz=") {
			t.Errorf("span tag %q carries the query string", value)
		}
	}
}
