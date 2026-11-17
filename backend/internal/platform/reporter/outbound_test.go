package reporter

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// AN OUTBOUND CALL LEAVES AS A CHILD SPAN OF THE REQUEST'S TRANSACTION, AND
// ITS QUERY STRING AND BODY LEAVE NOWHERE (#769). An always-traced request
// calls a third party through the shared constructor with an email in the
// query string and an identity number and a name in the body. Through the
// real SDK to the ingest stand-in: the transaction carries one http.client
// span under the request span, named by method, host and path; nothing seeded
// is anywhere in what was sent; the third party received no trace header.
func TestAnOutboundCallLeavesAsAChildSpanWithoutItsQueryOrBody(t *testing.T) {
	in := newIngest(t, http.StatusOK)
	var mu sync.Mutex
	var thirdPartyHeaders http.Header
	party := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		thirdPartyHeaders = r.Header.Clone()
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(party.Close)

	reporter := New(platform.ReporterConfig{DSN: in.dsn(), Environment: "development"}, platform.NewSlogLogger(slog.New(slog.DiscardHandler), platform.NoopReporter{}))
	client := platform.NewOutboundClient(5 * time.Second)
	mux := http.NewServeMux()
	mux.HandleFunc(tracedRoute, func(w http.ResponseWriter, r *http.Request) {
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost,
			party.URL+"/api/Sale?email=ana.perez%40example.com",
			strings.NewReader(`{"identity":"1710034065","name":"Ana Perez"}`))
		resp, err := client.Do(req)
		if err != nil {
			t.Errorf("outbound call: %v", err)
			return
		}
		_ = resp.Body.Close()
		w.WriteHeader(http.StatusOK)
	})
	sampling := platform.TraceSampling{Always: map[string]string{tracedRoute: "payment"}}
	api := httptest.NewServer(platform.TracedRequestPipeline(slog.New(slog.DiscardHandler), reporter, sampling, mux))
	t.Cleanup(api.Close)

	call(t, api, http.MethodPost, "/api/v1/public/checkout/ctx-123/confirm", "")
	if !reporter.Flush(5 * time.Second) {
		t.Fatal("not flushed")
	}

	transactions := in.receivedTransactions()
	if len(transactions) != 1 {
		t.Fatalf("transactions = %d, want 1", len(transactions))
	}
	trace := traceContext(t, transactions[0])
	spans, _ := transactions[0]["spans"].([]any)
	if len(spans) != 1 {
		t.Fatalf("child spans = %d, want the outbound call", len(spans))
	}
	child, _ := spans[0].(map[string]any)
	if child["op"] != "http.client" || child["description"] != "POST "+party.URL+"/api/Sale" || child["parent_span_id"] != trace["span_id"] {
		t.Errorf("child span = %v, want http.client %q under the request span", child, "POST "+party.URL+"/api/Sale")
	}

	sent, _ := json.Marshal(transactions)
	for _, seeded := range []string{"email=", "ana.perez", "1710034065", "Ana Perez"} {
		if strings.Contains(string(sent), seeded) {
			t.Errorf("the transaction carries %q: %s", seeded, sent)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if thirdPartyHeaders.Get(platform.SentryTraceHeader) != "" || thirdPartyHeaders.Get(platform.BaggageHeader) != "" {
		t.Errorf("the third party received our trace headers: %v", thirdPartyHeaders)
	}
}
