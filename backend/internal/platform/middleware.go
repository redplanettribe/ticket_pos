package platform

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// RequestPipeline wraps the API's route handler in the middleware every request
// passes through, in the one order that works:
//
//  1. the request id, outermost, so everything inside can report it: the
//     request log line, a recovered panic's log line and its 500 envelope;
//  2. the request log, which sees the request only after the id is on its
//     context, and which writes its line even when the handler aborts;
//  3. panic recovery, innermost, so the 500 it writes is the status the request
//     log records.
//
// The three are unexported so that nothing can assemble them in another order.
// That is how the request log came to read `"request_id":""` for every request:
// the log wrapped the id middleware, so the id was attached to a request the log
// line never saw.
func RequestPipeline(logger *slog.Logger, next http.Handler) http.Handler {
	return requestIDMiddleware(loggingMiddleware(logger, recoverMiddleware(logger, next)))
}

// RequestIDHeader carries a request's id, inbound from a BFF and outbound on
// every response.
const RequestIDHeader = "X-Request-ID"

// maxRequestIDLength bounds an inbound id; the BFFs send 36-character UUIDs.
const maxRequestIDLength = 128

// requestIDMiddleware gives every request an id, puts it on the request context
// (read it back with RequestID) and echoes it in X-Request-ID.
//
// An inbound X-Request-ID is adopted when it looks like an id: the BFFs mint one
// per call so their logs and ours join up, and per ADR 0008 nothing but a BFF
// reaches this process. Anything else, a blank header included, is replaced by a
// fresh UUID rather than repaired, because the id is written into every log line
// and echoed in a response header.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get(RequestIDHeader))
		if !wellFormedRequestID(requestID) {
			requestID = uuid.NewString()
		}

		ctx := WithRequestID(r.Context(), requestID)
		w.Header().Set(RequestIDHeader, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// wellFormedRequestID accepts 1 to maxRequestIDLength characters drawn from
// ASCII letters, digits and "-_.:".
func wellFormedRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLength {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.', c == ':':
		default:
			return false
		}
	}
	return true
}

// loggingMiddleware writes one "request" line per request with its id, method,
// path, status and duration.
//
// The line is written even when the handler aborts with http.ErrAbortHandler,
// which is how a streamed download that fails after its first byte ends: at
// WARN, with aborted=true and the status that had already been sent (0 when
// nothing had). The abort is then passed on so the server still drops the
// connection.
//
// A CLIENT THAT GAVE UP IS NOT A SERVER ERROR. When the client had already
// cancelled the request by the time its response was decided - its first
// status written, or the handler returning without one - the line is INFO with
// client_gone=true and status 499, whatever the handler wrote. Its database
// call fails with the cancelled context and would otherwise read as a 500 at
// ERROR with no cause, for a request nobody was waiting on. WriteDomainError
// writes no envelope at all then.
//
// Otherwise a 5xx is logged at ERROR, except a capacity refusal the platform
// makes as designed, such as the 503 HOLDER_EXPORT_BUSY: a domain error that
// declares a Retry-After (domainRetryAfter), logged at WARN. The declaration
// is what decides, never the header read back off the response, so a handler
// setting Retry-After by hand cannot quieten a failure. An unmapped error's
// line carries error_class (ErrorClass), so an ERROR says what kind of failure
// it was without carrying the error's text. Everything else, a 4xx included,
// is logged at INFO.
func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, ctx: r.Context()}
		defer func() {
			aborted := recover()
			level := slog.LevelInfo
			status := rec.status
			clientGone := false
			switch {
			case aborted != nil:
				// WARN whatever status went out: the abort is the event.
				level = slog.LevelWarn
			case rec.clientGone || (status == 0 && rec.clientCancelled()):
				// INFO, as set above.
				clientGone = true
				status = statusClientClosedRequest
			case status == 0:
				// A handler that returns without writing sends an implicit 200.
				status = http.StatusOK
			case status < http.StatusInternalServerError:
				// INFO, as set above.
			case rec.capacityRefusal:
				level = slog.LevelWarn
			default:
				level = slog.LevelError
			}
			attrs := []any{
				"request_id", RequestID(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"duration_ms", time.Since(start).Milliseconds(),
			}
			if aborted != nil {
				attrs = append(attrs, "aborted", true)
			}
			if clientGone {
				attrs = append(attrs, "client_gone", true)
			}
			if rec.errorClass != "" {
				attrs = append(attrs, "error_class", rec.errorClass)
			}
			logger.Log(r.Context(), level, "request", attrs...)
			if aborted != nil {
				panic(aborted)
			}
		}()
		next.ServeHTTP(rec, r)
	})
}

// statusClientClosedRequest is the status the request log records for a
// request whose client went away before its response was decided. It is
// nginx's 499, which no client ever receives: it names what happened, since
// no status was sent to anybody.
const statusClientClosedRequest = 499

// statusRecorder remembers what the request log needs to know about a
// response: the status a handler sent (0 means none yet), and what
// WriteDomainError learned about the error behind it.
type statusRecorder struct {
	http.ResponseWriter
	// ctx is the request's, to tell whether the client has gone.
	ctx    context.Context
	status int
	// clientGone is whether the client had cancelled the request when its
	// status was decided.
	clientGone bool
	// capacityRefusal is set by WriteDomainError for a domain error that
	// declares a Retry-After (domainRetryAfter).
	capacityRefusal bool
	// errorClass is set by WriteDomainError for an unmapped error.
	errorClass string
}

func (r *statusRecorder) WriteHeader(status int) {
	// 1xx responses are interim; the status worth logging is the final one.
	if r.status == 0 && status >= http.StatusOK {
		r.decide(status)
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.decide(http.StatusOK)
	}
	return r.ResponseWriter.Write(b)
}

// decide records the response's status, and whether its client had already
// gone when it was chosen.
func (r *statusRecorder) decide(status int) {
	r.status = status
	r.clientGone = r.clientCancelled()
}

// clientCancelled reports whether the client has cancelled the request. The
// server cancels a request's context when its connection closes; a deadline
// the server itself imposed is context.DeadlineExceeded, and is not this.
func (r *statusRecorder) clientCancelled() bool {
	return r.ctx != nil && errors.Is(r.ctx.Err(), context.Canceled)
}

// Unwrap lets http.ResponseController reach the writer underneath, so a
// streaming handler can still flush and set deadlines through this wrapper: the
// Holder Export sets one so a client that stops reading is let go (ADR 0075).
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// requestRecorder finds the request log's recorder under w, or nil when w is
// not served through RequestPipeline (a handler unit test's recorder).
func requestRecorder(w http.ResponseWriter) *statusRecorder {
	for {
		if rec, ok := w.(*statusRecorder); ok {
			return rec
		}
		unwrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return nil
		}
		w = unwrapper.Unwrap()
	}
}

// ClientIPHeader carries the end user's IP address as derived by the BFF that
// proxied the request. It is deliberately not X-Forwarded-For.
//
// Only a Next.js BFF of ours may set it. Do not forward it from an inbound
// request and do not let a handler read X-Forwarded-For instead — see ClientIP.
const ClientIPHeader = "X-BFF-Client-IP"

// ClientIP returns the client IP to attribute a request to, for rate limiting.
//
// WHY THIS DOES NOT READ X-Forwarded-For — do not "simplify" it back:
//
// X-Forwarded-For is client-supplied data. Google's frontend *appends* to any
// value the caller sent rather than replacing it, and the Cloud Load Balancing
// documentation states outright that it does not verify anything preceding the
// entries it adds. So the leftmost entry — the conventional "real client" slot —
// is whatever the browser typed. Reading it let anyone rotate a fabricated
// address per request and erase the per-IP OTP allowance entirely. Cloud Run's
// own documentation does not pin the composition down, so we assume the
// pessimistic case: attacker-controlled content can be present anywhere to the
// left of the hops our own infrastructure added.
//
// ClientIPHeader is trustworthy where X-Forwarded-For is not, and the reason is
// not visible in this file: per ADR 0008 the API runs with unauthenticated
// invocation disabled, so Cloud Run rejects every caller that cannot present an
// OIDC ID token holding roles/run.invoker. No browser reaches this process. The
// only senders are the Staff and Storefront BFFs, each of which sets the header
// itself from the forwarding chain and never forwards a browser's copy.
//
// A missing or malformed value falls back to the transport peer address. In
// production that is Google's frontend, which buckets all traffic together and
// therefore tightens the limit rather than loosening it; locally, where nothing
// sets forwarding headers at all, it is the developer's own address, which is
// the behaviour that has always applied.
func ClientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get(ClientIPHeader)); ip != "" {
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	return remoteAddrHost(r.RemoteAddr)
}

func remoteAddrHost(remoteAddr string) string {
	addr := strings.TrimSpace(remoteAddr)
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// BearerToken extracts the token from Authorization: Bearer <token>.
func BearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(auth, prefix))
}

// recoverMiddleware converts panics into 500 responses.
func recoverMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				// A handler that has already committed a response and cannot
				// finish it (a streamed download failing mid-way) aborts the
				// connection on purpose. Writing an error envelope after its
				// bytes would only corrupt them, so the abort goes on to the
				// server, which closes the connection.
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				logger.Error("panic recovered", "request_id", RequestID(r.Context()), "panic", rec)
				_ = WriteHandlerError(w, RequestID(r.Context()), http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
