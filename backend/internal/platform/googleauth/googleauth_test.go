package googleauth_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/googleauth"
	"github.com/peter/ticket_pos/backend/internal/platform/reportertest"
)

// A GOOGLE TOKEN EXCHANGE IN A SAMPLED REQUEST IS A CHILD SPAN (#769), named
// by the token endpoint's method, host and path; the code and client secret
// the exchange posts are in no span.
func TestATokenExchangeIsAChildSpan(t *testing.T) {
	google := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id_token":"not-a-jwt"}`))
	}))
	t.Cleanup(google.Close)
	logger := platform.NewSlogLogger(slog.New(slog.DiscardHandler), platform.NoopReporter{})
	client := googleauth.New(googleauth.Credentials{ClientID: "client-id", ClientSecret: "seeded-secret"}, google.URL+"/token", logger)
	fake := reportertest.New()

	request := fake.InSampledRequest(func(ctx context.Context) {
		// The stand-in's token is no JWT, so the exchange is refused after the
		// call; the call is what is traced.
		_, _ = client.VerifiedEmail(ctx, googleauth.Exchange{Code: "seeded-code", CodeVerifier: "v", RedirectURI: "http://storefront.example/cb"})
	})

	children := fake.ChildrenOf(request)
	if len(children) != 1 {
		t.Fatalf("child spans = %d, want the token exchange", len(children))
	}
	span := children[0]
	if span.Options.Op != "http.client" || span.Options.Name != "POST "+google.URL+"/token" || !span.Finished || span.HTTPStatus != http.StatusOK {
		t.Errorf("span = %s %q finished %v status %d, want http.client %q finished with 200",
			span.Options.Op, span.Options.Name, span.Finished, span.HTTPStatus, "POST "+google.URL+"/token")
	}
	for _, value := range span.Tags {
		if strings.Contains(value, "seeded") {
			t.Errorf("span tag %q carries the seeded body", value)
		}
	}
}
