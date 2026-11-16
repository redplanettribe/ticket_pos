package platform

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// lockedBuffer is a log destination written from the request goroutines and
// the reporter's goroutine at once.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) snapshot() *bytes.Buffer {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.NewBuffer(append([]byte(nil), b.buf.Bytes()...))
}

// blockingReporter never returns from ReportError until release is closed.
type blockingReporter struct {
	NoopReporter
	release chan struct{}
}

func (r blockingReporter) ReportError(context.Context, ErrorReport) { <-r.release }

func (r blockingReporter) Flush(time.Duration) bool {
	<-r.release
	return true
}

// failingReporter fails every report.
type failingReporter struct {
	NoopReporter
}

func (failingReporter) ReportError(context.Context, ErrorReport) { panic("reporter is down") }

// panickingServer serves a handler that always panics through the request
// pipeline, reporting to reporter and logging into the returned buffer.
func panickingServer(t *testing.T, reporter Reporter) (*httptest.Server, *lockedBuffer) {
	t.Helper()
	var buf lockedBuffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	server := httptest.NewServer(RequestPipeline(logger, reporter, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))
	t.Cleanup(server.Close)
	return server, &buf
}

func reporterWarnings(t *testing.T, buf *lockedBuffer) int {
	t.Helper()
	n := 0
	for _, line := range logLines(t, buf.snapshot()) {
		if line.str("level") == "WARN" && line.str("msg") == "error reporting failed; further failures are not logged" {
			n++
		}
	}
	return n
}

func getStatus(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestARequestIsNotDelayedByAReporterThatBlocks(t *testing.T) {
	inner := blockingReporter{release: make(chan struct{})}
	defer close(inner.release)
	var logBuf lockedBuffer
	reporter := newAsyncReporter(inner, NewSlogLogger(slog.New(slog.NewJSONHandler(&logBuf, nil)), NoopReporter{}), 1)
	server, _ := panickingServer(t, reporter)

	// The first report blocks the reporter, the second fills its queue, and
	// every one after is dropped: none of them may hold up its request.
	for range 5 {
		start := time.Now()
		if status := getStatus(t, server.URL); status != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", status)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("request took %s behind a blocked reporter", elapsed)
		}
	}
	if got := reporterWarnings(t, &logBuf); got != 1 {
		t.Fatalf("reporter failure logged %d times at Warn, want once (log: %s)", got, logBuf.snapshot())
	}
}

func TestAReporterThatFailsNeitherFailsTheRequestNorLogsPerEvent(t *testing.T) {
	var logBuf lockedBuffer
	reporter := NewAsyncReporter(failingReporter{}, NewSlogLogger(slog.New(slog.NewJSONHandler(&logBuf, nil)), NoopReporter{}))
	server, _ := panickingServer(t, reporter)

	for range 3 {
		// The panic's own 500, answered as it is without a reporter.
		if status := getStatus(t, server.URL); status != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", status)
		}
	}
	reporter.Flush(time.Second)

	if got := reporterWarnings(t, &logBuf); got != 1 {
		t.Fatalf("reporter failure logged %d times at Warn, want once (log: %s)", got, logBuf.snapshot())
	}
}

func TestAsyncReporterFlushKeepsItsBoundWhenTheReporterIsStuck(t *testing.T) {
	for _, tc := range []struct {
		name   string
		report bool
	}{
		{name: "stuck reporting", report: true},
		{name: "stuck flushing", report: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inner := blockingReporter{release: make(chan struct{})}
			defer close(inner.release)
			reporter := NewAsyncReporter(inner, NewSlogLogger(slog.New(slog.DiscardHandler), NoopReporter{}))
			if tc.report {
				reporter.ReportError(context.Background(), ErrorReport{Err: http.ErrHandlerTimeout})
			}

			start := time.Now()
			if reporter.Flush(50 * time.Millisecond) {
				t.Fatal("Flush reported success with the reporter stuck")
			}
			if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
				t.Fatalf("Flush(50ms) took %s", elapsed)
			}
		})
	}
}
