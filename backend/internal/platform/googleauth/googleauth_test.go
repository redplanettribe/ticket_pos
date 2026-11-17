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

	span := fake.OnlyOutboundCall(t, request, "POST "+google.URL+"/token", http.StatusOK)
	for _, value := range span.Tags {
		if strings.Contains(value, "seeded") {
			t.Errorf("span tag %q carries the seeded body", value)
		}
	}
}
