package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

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

// THE HOLDER EXPORT'S WRITE DEADLINE IS NOT A CLIENT'S CANCELLATION. The write
// that times out on the deadline the export set fails after its 200 went out,
// and a failed write cancels the request, so by then the context reads as
// cancelled. The export aborts, and its line is still the WARN abort with the
// 200 that was sent: no 499 and no context_canceled.
func TestRequestPipelineLogsTheHolderExportsWriteDeadlineAsAnAbort(t *testing.T) {
	req, cancel := cancelledRequest(http.MethodGet)
	handler, buf := pipelineUnderTest(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("PK partial"))
		cancel() // the write that timed out cancels the request
		panic(http.ErrAbortHandler)
	}))

	func() {
		defer func() { _ = recover() }()
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}()

	line := requestLogLine(t, buf)
	if line.str("level") != "WARN" || line.status() != http.StatusOK || line["client_gone"] != nil {
		t.Fatalf("request log = %v, want WARN with the 200 sent and without client_gone", line)
	}
	if line.str("error_class") != "" {
		t.Fatalf("request log error_class = %q, want none", line.str("error_class"))
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

// THE LEVEL OF A REQUEST LINE FOLLOWS ITS STATUS, NOT WHETHER IT WAS MAPPED.
// A 2xx, 3xx or 4xx is INFO. A 5xx is ERROR - a mapped deployment fault or
// failed commit as much as an unmapped failure - except a capacity refusal the
// platform makes as designed, which declares a Retry-After and is WARN.
func TestRequestPipelineLogsARequestAtTheLevelItsStatusCalls(t *testing.T) {
	domainError := func(code string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			_ = WriteDomainError(w, RequestID(r.Context()), apperror.New(code, "refused", nil))
		}
	}
	for _, tc := range []struct {
		name       string
		handler    http.HandlerFunc
		wantStatus int
		wantLevel  string
	}{
		{"a success", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
		}, http.StatusCreated, "INFO"},
		{"a redirect", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusSeeOther)
		}, http.StatusSeeOther, "INFO"},
		{"a mapped 404", domainError("EVENT_NOT_FOUND"), http.StatusNotFound, "INFO"},
		{"a mapped 409", domainError("CAPACITY_EXCEEDED"), http.StatusConflict, "INFO"},
		{"a mapped 503 capacity refusal", domainError("HOLDER_EXPORT_BUSY"), http.StatusServiceUnavailable, "WARN"},
		{"a mapped 500 failed commit", domainError("PAYMENT_SALE_COMMIT_FAILED"), http.StatusInternalServerError, "ERROR"},
		{"a mapped 503 deployment fault", domainError("CERTIFICATE_KEY_NOT_CONFIGURED"), http.StatusServiceUnavailable, "ERROR"},
		{"an unmapped error", func(w http.ResponseWriter, r *http.Request) {
			_ = WriteDomainError(w, RequestID(r.Context()), errors.New("connection refused"))
		}, http.StatusInternalServerError, "ERROR"},
		{"a 500 written by hand", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}, http.StatusInternalServerError, "ERROR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, buf := pipelineUnderTest(tc.handler)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/export", nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			line := requestLogLine(t, buf)
			if line.str("level") != tc.wantLevel || line.status() != tc.wantStatus {
				t.Fatalf("request log = %v, want %s with status %d", line, tc.wantLevel, tc.wantStatus)
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

// cancelledRequest is a request whose client has gone once cancel is called,
// as the server cancels a request's context when its connection closes.
func cancelledRequest(method string) (*http.Request, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	return httptest.NewRequest(method, "/api/v1/staff/events", nil).WithContext(ctx), cancel
}

// pgconnWriteTimeout is the error pgconn returns when a cancelled context
// interrupts a query it is writing: it sets the connection's deadline in the
// past, and what comes back is only the write's i/o timeout, with no
// context.Canceled anywhere in it.
func pgconnWriteTimeout() error {
	peer := &net.TCPAddr{IP: net.IPv4(10, 0, 0, 7), Port: 5432}
	return fmt.Errorf("list holders: write failed: %w",
		&net.OpError{Op: "write", Net: "tcp", Addr: peer, Err: os.ErrDeadlineExceeded})
}

// socketTimeout is a net.Error whose Timeout() is true and which is not
// os.ErrDeadlineExceeded, as another driver's socket might return.
type socketTimeout struct{}

func (socketTimeout) Error() string   { return "i/o timeout" }
func (socketTimeout) Timeout() bool   { return true }
func (socketTimeout) Temporary() bool { return true }

// A CLIENT THAT GIVES UP IS NOT A SERVER ERROR, but only when giving up is all
// that happened. The request line is INFO with client_gone=true and the 499
// that says the client closed the request when the error the handler reports is
// that cancellation itself, surfacing from the database call it failed, or when
// the handler reports nothing and writes nothing. Nobody is reading, so no
// envelope is written.
//
// The cancellation can surface as a socket's i/o timeout, which is how pgconn
// interrupts a write when its context is cancelled; once the client has gone
// that is the cancellation too, and its class is context_canceled.
func TestRequestPipelineLogsAClientThatOnlyGaveUpAtInfoAs499(t *testing.T) {
	for _, tc := range []struct {
		name      string
		respond   func(w http.ResponseWriter, r *http.Request)
		wantClass string
	}{
		{"the cancellation reported as the error", func(w http.ResponseWriter, r *http.Request) {
			_ = WriteDomainError(w, RequestID(r.Context()), fmt.Errorf("list sales: %w", r.Context().Err()))
		}, "context_canceled"},
		{"pgconn's cancellation as a write's i/o timeout", func(w http.ResponseWriter, r *http.Request) {
			_ = WriteDomainError(w, RequestID(r.Context()), pgconnWriteTimeout())
		}, "context_canceled"},
		{"the cancellation as a socket's net.Error timeout", func(w http.ResponseWriter, r *http.Request) {
			_ = WriteDomainError(w, RequestID(r.Context()), fmt.Errorf("list holders: %w", socketTimeout{}))
		}, "context_canceled"},
		{"nothing reported and nothing written", func(http.ResponseWriter, *http.Request) {}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, cancel := cancelledRequest(http.MethodGet)
			handler, buf := pipelineUnderTest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				cancel() // the client goes away while the handler is working
				tc.respond(w, r)
			}))
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Body.Len() != 0 {
				t.Fatalf("body = %q, want nothing written to a client that has gone", rec.Body.String())
			}
			line := requestLogLine(t, buf)
			if line.str("level") != "INFO" || line.status() != statusClientClosedRequest {
				t.Fatalf("request log = %v, want INFO with status 499", line)
			}
			if gone, _ := line["client_gone"].(bool); !gone {
				t.Fatalf("request log = %v, want client_gone=true", line)
			}
			if line.str("error_class") != tc.wantClass {
				t.Fatalf("request log error_class = %q, want %q", line.str("error_class"), tc.wantClass)
			}
		})
	}
}

// A CLIENT LEAVING NEVER DOWNGRADES WHAT HAPPENED. When the handler's outcome
// is anything other than the cancellation itself, the line keeps the status the
// handler really chose and the level that status earns, and client_gone=true is
// one more fact on it: a genuine failure that met a departed client is still an
// ERROR with its class, a mapped 5xx still names its code, a committed mutation
// still reads as the 201 it was, and a capacity refusal is still a WARN. The
// envelope is written as usual; a write to a gone client fails harmlessly.
func TestRequestPipelineKeepsTheRealOutcomeOfARequestWhoseClientLeft(t *testing.T) {
	domainError := func(code string) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) {
			_ = WriteDomainError(w, RequestID(r.Context()), apperror.New(code, "refused", nil))
		}
	}
	for _, tc := range []struct {
		name         string
		respond      func(w http.ResponseWriter, r *http.Request)
		wantStatus   int
		wantLevel    string
		wantClass    string
		wantBodyCode string
		wantLineCode string
	}{
		{"a Postgres failure", func(w http.ResponseWriter, r *http.Request) {
			_ = WriteDomainError(w, RequestID(r.Context()), fmt.Errorf("commit: %w", &pgconn.PgError{Code: "23505"}))
		}, http.StatusInternalServerError, "ERROR", "postgres 23505", "INTERNAL_ERROR", ""},
		// A context's deadline also reports Timeout(), but it is the server's
		// own clock running out, never the client's cancellation.
		{"the server's own deadline", func(w http.ResponseWriter, r *http.Request) {
			_ = WriteDomainError(w, RequestID(r.Context()), fmt.Errorf("list holders: %w", context.DeadlineExceeded))
		}, http.StatusInternalServerError, "ERROR", "context_deadline_exceeded", "INTERNAL_ERROR", ""},
		{"a mapped 500 failed commit", domainError("PAYMENT_SALE_COMMIT_FAILED"),
			http.StatusInternalServerError, "ERROR", "", "PAYMENT_SALE_COMMIT_FAILED", "PAYMENT_SALE_COMMIT_FAILED"},
		{"a mapped 500 unavailable link", domainError("CONFIRMATION_LINK_UNAVAILABLE"),
			http.StatusInternalServerError, "ERROR", "", "CONFIRMATION_LINK_UNAVAILABLE", "CONFIRMATION_LINK_UNAVAILABLE"},
		{"a mapped 503 deployment fault", domainError("CERTIFICATE_KEY_NOT_CONFIGURED"),
			http.StatusServiceUnavailable, "ERROR", "", "CERTIFICATE_KEY_NOT_CONFIGURED", "CERTIFICATE_KEY_NOT_CONFIGURED"},
		{"a mapped 503 capacity refusal", domainError("HOLDER_EXPORT_BUSY"),
			http.StatusServiceUnavailable, "WARN", "", "HOLDER_EXPORT_BUSY", "HOLDER_EXPORT_BUSY"},
		{"a mapped 404", domainError("EVENT_NOT_FOUND"),
			http.StatusNotFound, "INFO", "", "EVENT_NOT_FOUND", "EVENT_NOT_FOUND"},
		{"a committed mutation", func(w http.ResponseWriter, r *http.Request) {
			_ = WriteSuccess(w, RequestID(r.Context()), http.StatusCreated, map[string]string{"id": "sale"})
		}, http.StatusCreated, "INFO", "", "", ""},
		// A handler-layer error has no domain code to put on the line; its
		// status and level are what identify it.
		{"a 500 written by hand", func(w http.ResponseWriter, r *http.Request) {
			_ = WriteHandlerError(w, RequestID(r.Context()), http.StatusInternalServerError, "INTERNAL_ERROR", "failed", nil)
		}, http.StatusInternalServerError, "ERROR", "", "INTERNAL_ERROR", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, cancel := cancelledRequest(http.MethodPost)
			handler, buf := pipelineUnderTest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				cancel() // the client goes away before the handler answers
				tc.respond(w, r)
			}))
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status written = %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantBodyCode != "" {
				var env Envelope
				if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Error == nil || env.Error.Code != tc.wantBodyCode {
					t.Fatalf("body = %q, want the %s envelope written as usual", rec.Body.String(), tc.wantBodyCode)
				}
			}
			line := requestLogLine(t, buf)
			if line.str("level") != tc.wantLevel || line.status() != tc.wantStatus {
				t.Fatalf("request log = %v, want %s with status %d", line, tc.wantLevel, tc.wantStatus)
			}
			if gone, _ := line["client_gone"].(bool); !gone {
				t.Fatalf("request log = %v, want client_gone=true", line)
			}
			if line.str("error_class") != tc.wantClass {
				t.Fatalf("request log error_class = %q, want %q", line.str("error_class"), tc.wantClass)
			}
			if line.str("error_code") != tc.wantLineCode {
				t.Fatalf("request log error_code = %q, want %q", line.str("error_code"), tc.wantLineCode)
			}
		})
	}
}

// A cancellation the server made itself, with the client still connected, is
// not the client's departure: it is an unmapped 500 at ERROR like any other.
func TestRequestPipelineLogsACancellationWithTheClientStillThereAtError(t *testing.T) {
	handler, buf := pipelineUnderTest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = WriteDomainError(w, RequestID(r.Context()), fmt.Errorf("fan out: %w", context.Canceled))
	}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/staff/events", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	line := requestLogLine(t, buf)
	if line.str("level") != "ERROR" || line.status() != http.StatusInternalServerError || line["client_gone"] != nil {
		t.Fatalf("request log = %v, want ERROR 500 without client_gone", line)
	}
	if line.str("error_class") != "context_canceled" {
		t.Fatalf("request log error_class = %q, want context_canceled", line.str("error_class"))
	}
}

// THE STATUS IS DECIDED ONCE, WHEN IT IS CHOSEN. A response written before the
// client left keeps its status and its level, and does not carry client_gone:
// the connection closing afterwards, before the line is written, cannot re-read
// a 200 as abandoned or a 500 as a client that gave up.
func TestRequestPipelineKeepsTheStatusDecidedBeforeTheClientLeft(t *testing.T) {
	for _, tc := range []struct {
		name       string
		respond    func(w http.ResponseWriter, r *http.Request)
		wantStatus int
		wantLevel  string
	}{
		{"a 200", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}, http.StatusOK, "INFO"},
		{"an unmapped 500", func(w http.ResponseWriter, r *http.Request) {
			_ = WriteDomainError(w, RequestID(r.Context()), errors.New("connection refused"))
		}, http.StatusInternalServerError, "ERROR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, cancel := cancelledRequest(http.MethodGet)
			handler, buf := pipelineUnderTest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tc.respond(w, r)
				cancel() // the connection closes just after the status went out
			}))

			handler.ServeHTTP(httptest.NewRecorder(), req)

			line := requestLogLine(t, buf)
			if line.str("level") != tc.wantLevel || line.status() != tc.wantStatus || line["client_gone"] != nil {
				t.Fatalf("request log = %v, want %s %d without client_gone", line, tc.wantLevel, tc.wantStatus)
			}
		})
	}
}

// A GENUINE UNMAPPED 500 IS AN ERROR, AND SAYS WHAT KIND. The request line
// carries the error's class from ErrorClass, never its text, which could name
// a client's address or a buyer's email. A request that ran out of its own
// deadline is the server's failure, not a client that gave up.
func TestRequestPipelineLogsAnUnmappedErrorAtErrorWithItsClass(t *testing.T) {
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	for _, tc := range []struct {
		name      string
		ctx       context.Context
		err       error
		wantClass string
	}{
		{"a Postgres refusal", context.Background(),
			fmt.Errorf("list for ana@example.com: %w", &pgconn.PgError{Code: "22P02", Message: "invalid input syntax"}),
			"postgres 22P02"},
		{"an error of no known kind", context.Background(),
			fmt.Errorf("list for ana@example.com: %w", errors.New("connection refused")), "*errors.errorString"},
		{"the server's own deadline", expired,
			fmt.Errorf("list for ana@example.com: %w", context.DeadlineExceeded), "context_deadline_exceeded"},
		// Only a client that has gone turns a socket's timeout into its
		// cancellation. With the client still there it is a real timeout, and
		// so it is after the server's own deadline.
		{"a write's i/o timeout with the client still there", context.Background(),
			pgconnWriteTimeout(), "write_deadline_exceeded"},
		{"a write's i/o timeout after the server's own deadline", expired,
			pgconnWriteTimeout(), "write_deadline_exceeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, buf := pipelineUnderTest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = WriteDomainError(w, RequestID(r.Context()), tc.err)
			}))
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/staff/events", nil).WithContext(tc.ctx))

			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", rec.Code)
			}
			line := requestLogLine(t, buf)
			if line.str("level") != "ERROR" || line.status() != http.StatusInternalServerError {
				t.Fatalf("request log = %v, want ERROR with status 500", line)
			}
			if line.str("error_class") != tc.wantClass {
				t.Fatalf("request log error_class = %q, want %q", line.str("error_class"), tc.wantClass)
			}
			if strings.Contains(buf.String(), "ana@example.com") {
				t.Fatalf("the log carries the error's text: %s", buf.String())
			}
		})
	}
}

// THE CAPACITY REFUSAL'S LEVEL IS THE DOMAIN ERROR'S DECLARATION, not a header
// read back off the response. A handler that sets Retry-After by hand on a
// failure has declared nothing, and its 5xx is still an ERROR.
func TestRequestPipelineIgnoresAHandSetRetryAfterWhenChoosingTheLevel(t *testing.T) {
	for _, tc := range []struct {
		name    string
		respond func(w http.ResponseWriter, r *http.Request)
		status  int
	}{
		{"an unmapped error", func(w http.ResponseWriter, r *http.Request) {
			_ = WriteDomainError(w, RequestID(r.Context()), errors.New("connection refused"))
		}, http.StatusInternalServerError},
		{"a mapped 5xx that declares none", func(w http.ResponseWriter, r *http.Request) {
			_ = WriteDomainError(w, RequestID(r.Context()), apperror.New("PAYMENT_SALE_COMMIT_FAILED", "failed", nil))
		}, http.StatusInternalServerError},
		{"a 503 written by hand", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}, http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, buf := pipelineUnderTest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "5")
				tc.respond(w, r)
			}))

			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/export", nil))

			line := requestLogLine(t, buf)
			if line.str("level") != "ERROR" || line.status() != tc.status {
				t.Fatalf("request log = %v, want ERROR with status %d", line, tc.status)
			}
		})
	}
}
