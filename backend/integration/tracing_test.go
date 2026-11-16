package integration

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform/reportertest"
	"github.com/peter/ticket_pos/backend/internal/server"
)

// startTracedApp boots an app tracing to fake whose draw for an ordinary
// request is random.
func startTracedApp(t *testing.T, fake *reportertest.Reporter, random float64) *testEnv {
	t.Helper()
	var now atomic.Int64
	now.Store(fixedClock.UnixNano())
	env, _ := startAppReportingTo(t, fake, &now, server.WithTraceRandom(func() float64 { return random }))
	return env
}

// requestSpans are the http.server spans the fake recorded.
func requestSpans(fake *reportertest.Reporter) []*reportertest.Span {
	var spans []*reportertest.Span
	for _, span := range fake.Spans() {
		if span.Options.Op == "http.server" {
			spans = append(spans, span)
		}
	}
	return spans
}

// A QUERY RUN DURING A REQUEST IS A CHILD SPAN CARRYING ITS STATEMENT, AND
// NEVER ITS ARGUMENTS (#768, ADR 0079). The Storefront's Organization page is
// read by a slug the request supplies, which the pool's tracer is handed as
// an argument and never sends: the request is one span named by its route
// pattern, its queries are spans under it with their statement text, and the
// slug appears in no span.
func TestAQueryDuringARequestIsAChildSpanWithItsStatementAndNoArguments(t *testing.T) {
	setupTest(t)
	fake := reportertest.New()
	env := startTracedApp(t, fake, 0)
	const seeded = "seeded-slug-ana-perez-7f3a9"

	resp, _ := env.get(t, "/api/v1/public/organizations/"+seeded, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for an Organization nobody has", resp.StatusCode)
	}

	requests := requestSpans(fake)
	if len(requests) != 1 {
		t.Fatalf("request spans = %d, want 1", len(requests))
	}
	request := requests[0]
	if request.Options.Name != "GET /api/v1/public/organizations/{slug}" || !request.Finished || request.HTTPStatus != http.StatusNotFound {
		t.Errorf("request span = %q finished %v answering %d, want the route pattern, finished, 404", request.Options.Name, request.Finished, request.HTTPStatus)
	}

	var queries int
	for _, span := range fake.Spans() {
		for _, text := range append([]string{span.Options.Name, span.Options.SentryTrace, span.Options.Baggage}, tagTexts(span.Tags)...) {
			if strings.Contains(text, seeded) {
				t.Errorf("span %q carries the seeded argument in %q", span.Options.Name, text)
			}
		}
		if span.Options.Op != "db.sql" {
			continue
		}
		queries++
		if span.Parent != request {
			t.Errorf("query span %q is not under the request span", span.Options.Name)
		}
		if !strings.Contains(strings.ToUpper(span.Options.Name), "SELECT") || !span.Finished || span.Tags["db.system"] != "postgresql" {
			t.Errorf("query span = %q finished %v tags %v, want a finished postgresql statement", span.Options.Name, span.Finished, span.Tags)
		}
	}
	if queries == 0 {
		t.Fatal("no query span under the request")
	}
	// Only a query run under a request is a span: none is a trace of its own.
	if spans := fake.Spans(); len(spans) != queries+1 {
		t.Errorf("spans = %d, want the request span and its %d queries", len(spans), queries)
	}
}

func tagTexts(tags map[string]string) []string {
	var texts []string
	for key, value := range tags {
		texts = append(texts, key, value)
	}
	return texts
}

// CHECKOUT, PAYMENT AND SRI INVOICING ARE ALWAYS TRACED, A HEALTH CHECK NEVER
// IS, AND THE REST ONE IN TEN (#768, ADR 0079). The draw is injected: the
// checkout, payment and SRI invoicing requests are made with a draw and a Next
// app decision that would drop them, the health check with a draw and a
// decision that would keep it, and an ordinary request once on each side of
// one in ten. A request's answer does not matter: the decision is taken at
// the head, before the route runs.
func TestCheckoutPaymentAndSRIInvoicingAreAlwaysTracedAndAHealthCheckNever(t *testing.T) {
	setupTest(t)
	const notSampled = "771a43a4192642f0b136d5159a501700-b7ad6b7169203331-0"
	const sampled = "771a43a4192642f0b136d5159a501700-b7ad6b7169203331-1"
	cases := []struct {
		name        string
		method      string
		path        string
		sentryTrace string
		random      float64
		pattern     string
		sampled     bool
		flow        string
	}{
		{"checkout", http.MethodPost, "/api/v1/customer/organizations/fest-org/events/fest/checkout", notSampled, 0.99,
			"POST /api/v1/customer/organizations/{slug}/events/{eventSlug}/checkout", true, "checkout"},
		{"payment", http.MethodPost, "/api/v1/public/checkout/ctx-unknown/confirm", notSampled, 0.99,
			"POST /api/v1/public/checkout/{clientTransactionId}/confirm", true, "payment"},
		{"sri invoicing", http.MethodPost, "/api/v1/internal/sale-invoices/drain", notSampled, 0.99,
			"POST /api/v1/internal/sale-invoices/drain", true, "sri_invoicing"},
		{"health check", http.MethodGet, "/health", sampled, 0,
			"GET /health", false, ""},
		{"ordinary, drawn in", http.MethodGet, "/api/v1/public/tags", "", 0.05,
			"GET /api/v1/public/tags", true, ""},
		{"ordinary, drawn out", http.MethodGet, "/api/v1/public/tags", "", 0.5,
			"GET /api/v1/public/tags", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := reportertest.New()
			env := startTracedApp(t, fake, tc.random)
			req, _ := http.NewRequest(tc.method, env.server.URL+tc.path, nil)
			if tc.sentryTrace != "" {
				req.Header.Set("sentry-trace", tc.sentryTrace)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("%s %s: %v", tc.method, tc.path, err)
			}
			_ = resp.Body.Close()

			requests := requestSpans(fake)
			if len(requests) != 1 {
				t.Fatalf("request spans = %d, want 1", len(requests))
			}
			span := requests[0]
			if span.Options.Name != tc.pattern || span.Options.Sampled != tc.sampled || span.Tags["flow"] != tc.flow {
				t.Errorf("span = %q sampled %v flow %q, want %q sampled %v flow %q",
					span.Options.Name, span.Options.Sampled, span.Tags["flow"], tc.pattern, tc.sampled, tc.flow)
			}
			if span.Options.SentryTrace != tc.sentryTrace {
				t.Errorf("span continues %q, want %q", span.Options.SentryTrace, tc.sentryTrace)
			}
		})
	}
}
