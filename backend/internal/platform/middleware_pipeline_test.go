package platform

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform/apperror"
)

// logLine is one JSON line the request pipeline's logger wrote.
type logLine map[string]any

func (l logLine) str(key string) string {
	s, _ := l[key].(string)
	return s
}

func (l logLine) status() int {
	n, _ := l["status"].(float64)
	return int(n)
}

// pipelineUnderTest wraps next in the request pipeline, writing the log as JSON
// lines into the returned buffer.
func pipelineUnderTest(next http.Handler) (http.Handler, *bytes.Buffer) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return RequestPipeline(logger, next), &buf
}

func logLines(t *testing.T, buf *bytes.Buffer) []logLine {
	t.Helper()
	var lines []logLine
	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if raw == "" {
			continue
		}
		var line logLine
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("log line is not JSON: %v (%q)", err, raw)
		}
		lines = append(lines, line)
	}
	return lines
}

// requestLogLine returns the one access-log line the pipeline wrote.
func requestLogLine(t *testing.T, buf *bytes.Buffer) logLine {
	t.Helper()
	var found []logLine
	for _, line := range logLines(t, buf) {
		if line.str("msg") == "request" {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("got %d request log lines, want exactly 1 (log: %s)", len(found), buf.String())
	}
	return found[0]
}

func envelopeRequestID(t *testing.T, body []byte) string {
	t.Helper()
	var env Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("body is not an envelope: %v (%q)", err, body)
	}
	return env.RequestID
}

func TestRequestPipelineGivesARequestWithoutAnIDOneEverywhereItIsReported(t *testing.T) {
	var seenByHandler string
	handler, buf := pipelineUnderTest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenByHandler = RequestID(r.Context())
		_ = WriteSuccess(w, seenByHandler, http.StatusOK, map[string]string{"ok": "yes"})
	}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil))

	if seenByHandler == "" {
		t.Fatal("handler saw an empty request id on its context")
	}
	if got := rec.Header().Get("X-Request-ID"); got != seenByHandler {
		t.Fatalf("X-Request-ID = %q, want %q", got, seenByHandler)
	}
	if got := envelopeRequestID(t, rec.Body.Bytes()); got != seenByHandler {
		t.Fatalf("envelope request_id = %q, want %q", got, seenByHandler)
	}
	line := requestLogLine(t, buf)
	if got := line.str("request_id"); got != seenByHandler {
		t.Fatalf("request log request_id = %q, want %q", got, seenByHandler)
	}
	if line.str("level") != "INFO" || line.status() != http.StatusOK {
		t.Fatalf("request log level=%q status=%d, want INFO 200", line.str("level"), line.status())
	}
}

func TestRequestPipelineKeepsAWellFormedInboundRequestID(t *testing.T) {
	const inbound = "6f1c2a4e-9b7d-4c3e-8a21-5d0f3b9e7c12"
	var seenByHandler string
	handler, buf := pipelineUnderTest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenByHandler = RequestID(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil)
	req.Header.Set("X-Request-ID", inbound)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if seenByHandler != inbound {
		t.Fatalf("handler request id = %q, want the inbound %q", seenByHandler, inbound)
	}
	if got := rec.Header().Get("X-Request-ID"); got != inbound {
		t.Fatalf("X-Request-ID = %q, want %q", got, inbound)
	}
	if got := requestLogLine(t, buf).str("request_id"); got != inbound {
		t.Fatalf("request log request_id = %q, want %q", got, inbound)
	}
}

// The id is written into every log line and echoed in a response header, so an
// inbound value is only adopted when it looks like an id. Anything else is
// replaced rather than repaired.
func TestRequestPipelineReplacesAMalformedInboundRequestID(t *testing.T) {
	for name, inbound := range map[string]string{
		"blank":        "   ",
		"control char": "abc\x00def",
		"space":        "two words",
		"too long":     strings.Repeat("a", 129),
	} {
		t.Run(name, func(t *testing.T) {
			var seenByHandler string
			handler, _ := pipelineUnderTest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seenByHandler = RequestID(r.Context())
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil)
			req.Header.Set("X-Request-ID", inbound)

			handler.ServeHTTP(httptest.NewRecorder(), req)

			if seenByHandler == "" || seenByHandler == inbound || seenByHandler == strings.TrimSpace(inbound) {
				t.Fatalf("request id = %q, want a freshly generated one in place of %q", seenByHandler, inbound)
			}
		})
	}
}

func TestRequestPipelineReportsARecoveredPanicUnderTheRequestsID(t *testing.T) {
	handler, buf := pipelineUnderTest(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	id := envelopeRequestID(t, rec.Body.Bytes())
	if id == "" || rec.Header().Get("X-Request-ID") != id {
		t.Fatalf("envelope request_id = %q, header = %q; want the same non-empty id", id, rec.Header().Get("X-Request-ID"))
	}
	var panicLine logLine
	for _, line := range logLines(t, buf) {
		if line.str("msg") == "panic recovered" {
			panicLine = line
		}
	}
	if panicLine == nil || panicLine.str("request_id") != id {
		t.Fatalf("panic log line = %v, want one carrying request_id %q", panicLine, id)
	}
	line := requestLogLine(t, buf)
	if line.str("request_id") != id || line.status() != http.StatusInternalServerError || line.str("level") != "ERROR" {
		t.Fatalf("request log = %v, want ERROR status 500 under request_id %q", line, id)
	}
}

// A streamed download that fails after its first byte aborts with
// http.ErrAbortHandler. The abort still reaches the server, and the request is
// still logged: at WARN, with the status that had already gone out.
func TestRequestPipelineLogsAnAbortedStreamAtWarnWithTheStatusSent(t *testing.T) {
	var seenByHandler string
	handler, buf := pipelineUnderTest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenByHandler = RequestID(r.Context())
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("PK partial"))
		panic(http.ErrAbortHandler)
	}))
	rec := httptest.NewRecorder()

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/export", nil))
	}()

	if recovered != http.ErrAbortHandler {
		t.Fatalf("recovered %v, want http.ErrAbortHandler to reach the server", recovered)
	}
	if got := rec.Body.String(); got != "PK partial" {
		t.Fatalf("body = %q, want only the handler's own bytes", got)
	}
	line := requestLogLine(t, buf)
	if line.str("level") != "WARN" {
		t.Fatalf("request log level = %q, want WARN (%v)", line.str("level"), line)
	}
	if line.status() != http.StatusOK {
		t.Fatalf("request log status = %d, want the 200 that was sent", line.status())
	}
	if line.str("request_id") == "" || line.str("request_id") != seenByHandler {
		t.Fatalf("request log request_id = %q, want %q", line.str("request_id"), seenByHandler)
	}
	if aborted, _ := line["aborted"].(bool); !aborted {
		t.Fatalf("request log = %v, want aborted=true", line)
	}
}

// Aborting before anything was written sends no status at all, and the log line
// says so rather than claiming a 200 that never went out.
func TestRequestPipelineLogsAnAbortBeforeTheFirstByteWithNoStatus(t *testing.T) {
	handler, buf := pipelineUnderTest(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	func() {
		defer func() { _ = recover() }()
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/export", nil))
	}()

	line := requestLogLine(t, buf)
	if line.str("level") != "WARN" || line.status() != 0 {
		t.Fatalf("request log = %v, want WARN with status 0", line)
	}
}

// An abort is logged at WARN even when the status already sent was a 5xx: the
// abort is the event the line reports.
func TestRequestPipelineLogsAnAbortAtWarnWhateverStatusWasSent(t *testing.T) {
	handler, buf := pipelineUnderTest(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		panic(http.ErrAbortHandler)
	}))

	func() {
		defer func() { _ = recover() }()
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/export", nil))
	}()

	line := requestLogLine(t, buf)
	if line.str("level") != "WARN" || line.status() != http.StatusServiceUnavailable {
		t.Fatalf("request log = %v, want WARN with status 503", line)
	}
}

// unwrappingWriter stands for a middleware between the pipeline and a handler
// that wraps the response writer the way the pipeline's own recorder does.
type unwrappingWriter struct{ http.ResponseWriter }

func (u unwrappingWriter) Unwrap() http.ResponseWriter { return u.ResponseWriter }

// AN EXPECTED DOMAIN REFUSAL IS NEVER AN ERROR IN THE LOG, whatever its status.
// A response written from a mapped domain error - a 503 HOLDER_EXPORT_BUSY, a
// 404 EVENT_NOT_FOUND - is the platform answering as designed, so the request
// line is WARN, also through a middleware that wraps the writer.
func TestRequestPipelineLogsAMappedDomainErrorAtWarnWhateverItsStatus(t *testing.T) {
	for _, tc := range []struct {
		name   string
		code   string
		status int
		wrap   bool
	}{
		{"a 503 capacity refusal", "HOLDER_EXPORT_BUSY", http.StatusServiceUnavailable, false},
		{"a 503 capacity refusal through a wrapped writer", "HOLDER_EXPORT_BUSY", http.StatusServiceUnavailable, true},
		{"a 404", "EVENT_NOT_FOUND", http.StatusNotFound, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, buf := pipelineUnderTest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.wrap {
					w = unwrappingWriter{w}
				}
				_ = WriteDomainError(w, RequestID(r.Context()), apperror.New(tc.code, "refused", nil))
			}))
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/export", nil))

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			line := requestLogLine(t, buf)
			if line.str("level") != "WARN" || line.status() != tc.status {
				t.Fatalf("request log = %v, want WARN with status %d", line, tc.status)
			}
		})
	}
}

// An error that is not a mapped domain error is a 500 nobody designed, and that
// IS an error: WriteDomainError's INTERNAL_ERROR, and a 5xx a handler writes
// itself, both log at ERROR.
func TestRequestPipelineLogsAnUnmappedFailureAtError(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(w http.ResponseWriter, r *http.Request)
	}{
		{"an unmapped error", func(w http.ResponseWriter, r *http.Request) {
			_ = WriteDomainError(w, RequestID(r.Context()), errors.New("connection refused"))
		}},
		{"a 500 written by hand", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, buf := pipelineUnderTest(http.HandlerFunc(tc.write))

			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/export", nil))

			line := requestLogLine(t, buf)
			if line.str("level") != "ERROR" || line.status() != http.StatusInternalServerError {
				t.Fatalf("request log = %v, want ERROR with status 500", line)
			}
		})
	}
}

// Streaming handlers reach the connection through http.ResponseController, so
// the pipeline's response wrapper must not hide the writer underneath it.
func TestRequestPipelineLeavesTheResponseControllerWorking(t *testing.T) {
	var flushErr error
	handler, _ := pipelineUnderTest(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("first"))
		flushErr = http.NewResponseController(w).Flush()
	}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/export", nil))

	if flushErr != nil {
		t.Fatalf("Flush through the pipeline: %v", flushErr)
	}
	if !rec.Flushed {
		t.Fatal("the recorder underneath was never flushed")
	}
}
