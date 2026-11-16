package sri_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/invoicing/sri"
	"github.com/peter/ticket_pos/backend/internal/platform/reportertest"
)

// AN SRI CALL IN A SAMPLED REQUEST IS A CHILD SPAN (#769), named by the web
// service's method, host and path.
func TestAnSRICallIsAChildSpan(t *testing.T) {
	authority := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(`<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body/></soap:Envelope>`))
	}))
	t.Cleanup(authority.Close)
	client := sri.NewClient(sri.EnvironmentTest, sri.WithBaseURL(authority.URL))
	fake := reportertest.New()

	request := fake.InSampledRequest(func(ctx context.Context) {
		// The stand-in's answer is empty; the call is what is traced.
		_, _ = client.AuthorizationComprobante(ctx, "1234567890")
	})

	children := fake.ChildrenOf(request)
	if len(children) != 1 {
		t.Fatalf("child spans = %d, want the SRI call", len(children))
	}
	want := "POST " + authority.URL + sri.AuthorizationPath
	if span := children[0]; span.Options.Op != "http.client" || span.Options.Name != want || !span.Finished || span.HTTPStatus != http.StatusOK {
		t.Errorf("span = %s %q finished %v status %d, want http.client %q finished with 200",
			span.Options.Op, span.Options.Name, span.Finished, span.HTTPStatus, want)
	}
}
