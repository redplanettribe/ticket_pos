package platform_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/reportertest"
)

// Inbound trace headers as a Next app sends them: trace id, the calling
// span, and its sampling decision.
const (
	inboundTraceID   = "771a43a4192642f0b136d5159a501700"
	inboundSampled   = inboundTraceID + "-b7ad6b7169203331-1"
	inboundUnsampled = inboundTraceID + "-b7ad6b7169203331-0"
	inboundNoDecided = inboundTraceID + "-b7ad6b7169203331"
	inboundBaggage   = "sentry-trace_id=" + inboundTraceID + ",sentry-sample_rate=0.1"
)

const (
	checkoutPattern = "POST /checkout/{slug}"
	healthPattern   = "GET /health"
	ordinaryPattern = "GET /events/{id}"
)

// tracingServer serves three routes, a checkout always traced, a health check
// never traced and an ordinary one, through the traced request pipeline with
// the fake as reporter and random as the ordinary draw.
func tracingServer(t *testing.T, reporter platform.Reporter, random float64) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	mux.HandleFunc(checkoutPattern, ok)
	mux.HandleFunc(healthPattern, ok)
	mux.HandleFunc(ordinaryPattern, ok)
	sampling := platform.TraceSampling{
		Always: map[string]string{checkoutPattern: "checkout"},
		Never:  map[string]bool{healthPattern: true},
		Random: func() float64 { return random },
	}
	server := httptest.NewServer(platform.TracedRequestPipeline(discardLogger(), reporter, sampling, mux))
	t.Cleanup(server.Close)
	return server
}

func send(t *testing.T, server *httptest.Server, method, path, sentryTrace string) {
	t.Helper()
	req, _ := http.NewRequest(method, server.URL+path, nil)
	if sentryTrace != "" {
		req.Header.Set(platform.SentryTraceHeader, sentryTrace)
		req.Header.Set(platform.BaggageHeader, inboundBaggage)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	_ = resp.Body.Close()
}

// onlySpan is the one span the fake recorded.
func onlySpan(t *testing.T, fake *reportertest.Reporter) *reportertest.Span {
	t.Helper()
	spans := fake.Spans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	return spans[0]
}

// A REQUEST IS ONE SPAN, NAMED BY ITS ROUTE PATTERN (#768). Never the raw
// path, which carries ids; finished, with the status it answered.
func TestARequestIsOneSpanNamedByItsRoutePattern(t *testing.T) {
	fake := reportertest.New()
	server := tracingServer(t, fake, 0)

	send(t, server, http.MethodGet, "/events/0b6f4a52-3c1e-4c8e-9d43-2f1a7e5b9c10", "")

	span := onlySpan(t, fake)
	if span.Options.Name != ordinaryPattern || span.Options.Op != "http.server" {
		t.Errorf("span = %q %q, want http.server %q", span.Options.Op, span.Options.Name, ordinaryPattern)
	}
	if span.Parent != nil {
		t.Error("the request span has a parent; it starts the trace on this process")
	}
	if !span.Finished || span.HTTPStatus != http.StatusNoContent {
		t.Errorf("finished = %v, status = %d; want finished with 204", span.Finished, span.HTTPStatus)
	}
}

func TestARequestNoRouteMatchesIsAnUnroutedSpan(t *testing.T) {
	fake := reportertest.New()
	server := tracingServer(t, fake, 0)

	send(t, server, http.MethodGet, "/no/such/route", "")

	span := onlySpan(t, fake)
	if span.Options.Name != "unrouted" || span.HTTPStatus != http.StatusNotFound {
		t.Errorf("span = %q answering %d, want unrouted answering 404", span.Options.Name, span.HTTPStatus)
	}
}

// A REQUEST CARRYING TRACE HEADERS CONTINUES THAT TRACE; ONE WITHOUT STARTS
// ITS OWN (#768).
func TestARequestWithTraceHeadersContinuesThatTraceAndOneWithoutStartsItsOwn(t *testing.T) {
	fake := reportertest.New()
	server := tracingServer(t, fake, 0)

	send(t, server, http.MethodGet, "/events/1", inboundSampled)
	send(t, server, http.MethodGet, "/events/2", "")

	spans := fake.Spans()
	if len(spans) != 2 {
		t.Fatalf("spans = %d, want 2", len(spans))
	}
	if spans[0].Options.SentryTrace != inboundSampled || spans[0].Options.Baggage != inboundBaggage {
		t.Errorf("continued span = %+v, want the inbound sentry-trace and baggage", spans[0].Options)
	}
	if spans[1].Options.SentryTrace != "" || spans[1].Options.Baggage != "" {
		t.Errorf("new trace span = %+v, want no trace to continue", spans[1].Options)
	}
}

// SAMPLING IS DECIDED ONCE, AT THE HEAD (#768, ADR 0079): a checkout always,
// a health check never, whatever the Next app decided; any other request as
// the Next app decided, or one in ten by the draw.
func TestSamplingIsDecidedAtTheHead(t *testing.T) {
	cases := []struct {
		name        string
		method      string
		path        string
		sentryTrace string
		random      float64
		sampled     bool
		flow        string
	}{
		{"a checkout the draw would drop", http.MethodPost, "/checkout/fest", "", 0.99, true, "checkout"},
		{"a checkout the Next app did not sample", http.MethodPost, "/checkout/fest", inboundUnsampled, 0.99, true, "checkout"},
		{"a health check the draw would keep", http.MethodGet, "/health", "", 0, false, ""},
		{"a health check the Next app sampled", http.MethodGet, "/health", inboundSampled, 0, false, ""},
		{"an ordinary request drawn under one in ten", http.MethodGet, "/events/1", "", 0.09, true, ""},
		{"an ordinary request drawn at one in ten", http.MethodGet, "/events/1", "", 0.1, false, ""},
		{"an ordinary request drawn over one in ten", http.MethodGet, "/events/1", "", 0.5, false, ""},
		{"an ordinary request the Next app sampled", http.MethodGet, "/events/1", inboundSampled, 0.99, true, ""},
		{"an ordinary request the Next app did not sample", http.MethodGet, "/events/1", inboundUnsampled, 0, false, ""},
		{"an ordinary request whose trace has no decision, drawn in", http.MethodGet, "/events/1", inboundNoDecided, 0.05, true, ""},
		{"an ordinary request whose trace has no decision, drawn out", http.MethodGet, "/events/1", inboundNoDecided, 0.95, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := reportertest.New()
			server := tracingServer(t, fake, tc.random)

			send(t, server, tc.method, tc.path, tc.sentryTrace)

			span := onlySpan(t, fake)
			if span.Options.Sampled != tc.sampled {
				t.Errorf("sampled = %v, want %v", span.Options.Sampled, tc.sampled)
			}
			if span.Tags["flow"] != tc.flow {
				t.Errorf("flow tag = %q, want %q", span.Tags["flow"], tc.flow)
			}
		})
	}
}

// The default pipeline names no route always or never traced: every request
// is ordinary.
func TestTheDefaultPipelineTracesEveryRequestAsOrdinary(t *testing.T) {
	fake := reportertest.New()
	mux := http.NewServeMux()
	mux.HandleFunc(healthPattern, func(http.ResponseWriter, *http.Request) {})
	server := httptest.NewServer(platform.RequestPipeline(discardLogger(), fake, mux))
	t.Cleanup(server.Close)

	send(t, server, http.MethodGet, "/health", inboundSampled)

	if span := onlySpan(t, fake); !span.Options.Sampled || span.Options.Name != healthPattern {
		t.Errorf("span = %q sampled %v, want %q sampled as the Next app decided", span.Options.Name, span.Options.Sampled, healthPattern)
	}
}

// requestIDAtSpanStart records the request id on the context each span is
// started under.
type requestIDAtSpanStart struct {
	*reportertest.Reporter
	ids []string
}

func (r *requestIDAtSpanStart) StartSpan(ctx context.Context, options platform.SpanOptions) (context.Context, platform.Span) {
	r.ids = append(r.ids, platform.RequestID(ctx))
	return r.Reporter.StartSpan(ctx, options)
}

// THE REQUEST ID STAYS OUTERMOST (#768): the request span is started under
// the id, so the id is there for everything inside it.
func TestTheRequestSpanStartsInsideTheRequestID(t *testing.T) {
	reporter := &requestIDAtSpanStart{Reporter: reportertest.New()}
	server := tracingServer(t, reporter, 0)

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/events/1", nil)
	req.Header.Set(platform.RequestIDHeader, "req-traced-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()

	if len(reporter.ids) != 1 || reporter.ids[0] != "req-traced-1" {
		t.Errorf("request ids at span start = %q, want [req-traced-1]", reporter.ids)
	}
}

// A PANIC FINISHES THE REQUEST SPAN WITH THE 500 IT ANSWERED.
func TestAPanickingRequestsSpanIsFinishedWithItsStatus(t *testing.T) {
	fake := reportertest.New()
	mux := http.NewServeMux()
	mux.HandleFunc(ordinaryPattern, func(http.ResponseWriter, *http.Request) { panic("the clock broke") })
	server := httptest.NewServer(platform.RequestPipeline(discardLogger(), fake, mux))
	t.Cleanup(server.Close)

	send(t, server, http.MethodGet, "/events/1", "")

	span := onlySpan(t, fake)
	if !span.Finished || span.HTTPStatus != http.StatusInternalServerError {
		t.Errorf("finished = %v, status = %d; want finished with 500", span.Finished, span.HTTPStatus)
	}
}
