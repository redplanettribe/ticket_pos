package reporter

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

const (
	nextTraceID  = "771a43a4192642f0b136d5159a501700"
	nextSpanID   = "b7ad6b7169203331"
	tracedRoute  = "POST /api/v1/public/checkout/{clientTransactionId}/confirm"
	ordinaryPath = "/api/v1/public/events"
)

// tracedAPI serves a failing checkout confirmation, always traced, and an
// ordinary route, through the traced pipeline over the real SDK reporting to
// the ingest stand-in. The ordinary draw never samples.
//
// When hold is not nil, the confirmation keeps its query open until hold is
// closed.
func tracedAPI(t *testing.T, in *ingest, hold chan struct{}) (*httptest.Server, platform.Reporter) {
	t.Helper()
	reporter := New(platform.ReporterConfig{DSN: in.dsn(), Environment: "development"}, platform.NewSlogLogger(slog.New(slog.DiscardHandler), platform.NoopReporter{}))
	logger := platform.NewSlogLogger(slog.New(slog.DiscardHandler), reporter)
	mux := http.NewServeMux()
	mux.HandleFunc(tracedRoute, func(w http.ResponseWriter, r *http.Request) {
		// A child span whose statement text carries an email, as a literal
		// in SQL would: the scrubber runs on every span.
		// The query is still open when the request reports: the report is
		// made under the request's context and links to the request span.
		_, span := reporter.StartSpan(r.Context(), platform.SpanOptions{Op: "db.sql", Name: "SELECT id FROM customers WHERE email = 'ana.perez@example.com'"})
		logger.Error(r.Context(), "could not confirm the payment", "error", errors.New("provider unreachable"))
		if hold != nil {
			<-hold
		}
		span.Finish()
		_ = platform.WriteDomainError(w, platform.RequestID(r.Context()), errors.New("provider unreachable"))
	})
	mux.HandleFunc("GET "+ordinaryPath, func(w http.ResponseWriter, r *http.Request) {
		logger.Error(r.Context(), "could not list events", "error", errors.New("database unreachable"))
		w.WriteHeader(http.StatusInternalServerError)
	})
	sampling := platform.TraceSampling{
		Always: map[string]string{tracedRoute: "payment"},
		Random: func() float64 { return 0.99 },
	}
	api := httptest.NewServer(platform.TracedRequestPipeline(slog.New(slog.DiscardHandler), reporter, sampling, mux))
	t.Cleanup(api.Close)
	return api, reporter
}

func call(t *testing.T, api *httptest.Server, method, path, sentryTrace string) {
	t.Helper()
	req, _ := http.NewRequest(method, api.URL+path, nil)
	if sentryTrace != "" {
		req.Header.Set(platform.SentryTraceHeader, sentryTrace)
		req.Header.Set(platform.BaggageHeader, "sentry-trace_id="+nextTraceID+",sentry-sample_rate=0.1,sentry-sampled=false")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	_ = resp.Body.Close()
}

func traceContext(t *testing.T, item map[string]any) map[string]any {
	t.Helper()
	contexts, _ := item["contexts"].(map[string]any)
	trace, ok := contexts["trace"].(map[string]any)
	if !ok {
		t.Fatalf("no trace context in %v", item)
	}
	return trace
}

// A SAMPLED REQUEST CONTINUES THE NEXT APP'S TRACE, AND ITS EVENT LINKS TO IT
// (#768). The confirmation arrives with the Next app's trace headers, its
// decision "not sampled"; the route is always traced, so it is. One
// transaction leaves, named by the route pattern, in the Next app's trace
// under the Next app's span; the event the request reports carries the same
// trace and the request span, though the request's query was still open when
// it reported; the statement's email is scrubbed.
func TestASampledRequestContinuesTheTraceAndItsEventLinksToIt(t *testing.T) {
	in := newIngest(t, http.StatusOK)
	hold := make(chan struct{})
	api, reporter := tracedAPI(t, in, hold)

	done := make(chan struct{})
	go func() {
		defer close(done)
		call(t, api, http.MethodPost, "/api/v1/public/checkout/ctx-123/confirm", nextTraceID+"-"+nextSpanID+"-0")
	}()
	// The event leaves while the query is still open.
	deadline := time.Now().Add(5 * time.Second)
	for events, _ := in.received(); len(events) == 0; events, _ = in.received() {
		if time.Now().After(deadline) {
			t.Fatal("no event while the query was open")
		}
		reporter.Flush(100 * time.Millisecond)
	}
	close(hold)
	<-done
	if !reporter.Flush(5 * time.Second) {
		t.Fatal("not flushed")
	}

	transactions := in.receivedTransactions()
	if len(transactions) != 1 {
		t.Fatalf("transactions = %d, want 1", len(transactions))
	}
	transaction := transactions[0]
	if transaction["transaction"] != tracedRoute {
		t.Errorf("transaction name = %v, want %q", transaction["transaction"], tracedRoute)
	}
	trace := traceContext(t, transaction)
	if trace["trace_id"] != nextTraceID || trace["parent_span_id"] != nextSpanID || trace["op"] != "http.server" {
		t.Errorf("transaction trace = %v, want trace %s under span %s, op http.server", trace, nextTraceID, nextSpanID)
	}
	if tags, _ := transaction["tags"].(map[string]any); tags["flow"] != "payment" {
		t.Errorf("transaction tags = %v, want flow payment", transaction["tags"])
	}
	spans, _ := transaction["spans"].([]any)
	if len(spans) != 1 {
		t.Fatalf("child spans = %d, want 1", len(spans))
	}
	child, _ := spans[0].(map[string]any)
	if child["description"] != "SELECT id FROM customers WHERE email = '[email]'" || child["parent_span_id"] != trace["span_id"] {
		t.Errorf("child span = %v, want the scrubbed statement under the request span", child)
	}

	events, _ := in.received()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	eventTrace := traceContext(t, events[0])
	if eventTrace["trace_id"] != nextTraceID || eventTrace["span_id"] != trace["span_id"] {
		t.Errorf("event trace = %v, want trace %s, span %v", eventTrace, nextTraceID, trace["span_id"])
	}
}

// A REQUEST WITHOUT TRACE HEADERS STARTS ITS OWN TRACE (#768).
func TestARequestWithoutTraceHeadersStartsItsOwnTrace(t *testing.T) {
	in := newIngest(t, http.StatusOK)
	api, reporter := tracedAPI(t, in, nil)

	call(t, api, http.MethodPost, "/api/v1/public/checkout/ctx-123/confirm", "")
	if !reporter.Flush(5 * time.Second) {
		t.Fatal("not flushed")
	}

	transactions := in.receivedTransactions()
	if len(transactions) != 1 {
		t.Fatalf("transactions = %d, want 1", len(transactions))
	}
	trace := traceContext(t, transactions[0])
	traceID, _ := trace["trace_id"].(string)
	if len(traceID) != 32 || traceID == nextTraceID || trace["parent_span_id"] != nil {
		t.Errorf("trace = %v, want a new trace with no parent", trace)
	}
	events, _ := in.received()
	if len(events) != 1 || traceContext(t, events[0])["trace_id"] != traceID {
		t.Errorf("events = %v, want one in trace %s", events, traceID)
	}
}

// AN UNSAMPLED REQUEST SENDS NO TRANSACTION. The Next app's decision and the
// draw both say no; its event still leaves.
func TestAnUnsampledRequestSendsNoTransaction(t *testing.T) {
	in := newIngest(t, http.StatusOK)
	api, reporter := tracedAPI(t, in, nil)

	call(t, api, http.MethodGet, ordinaryPath, nextTraceID+"-"+nextSpanID+"-0")
	call(t, api, http.MethodGet, ordinaryPath, "")
	if !reporter.Flush(5 * time.Second) {
		t.Fatal("not flushed")
	}

	if transactions := in.receivedTransactions(); len(transactions) != 0 {
		t.Errorf("transactions = %v, want none", transactions)
	}
	if events, _ := in.received(); len(events) != 2 {
		t.Errorf("events = %d, want 2", len(events))
	}
}
