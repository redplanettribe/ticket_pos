package platform

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// RequestIDMiddleware assigns or forwards X-Request-ID and attaches it to the request context.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = newRequestID()
		}

		ctx := WithRequestID(r.Context(), requestID)
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// LoggingMiddleware logs each request with method, path, status, and duration.
func LoggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		logger.Info("request",
			"request_id", RequestID(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Unwrap hands http.ResponseController the connection's own writer, without
// which a handler behind this middleware cannot set a write deadline or flush:
// the Holder Export sets one so a client that stops reading is let go (ADR 0075).
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b[:])
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

// RecoverMiddleware converts panics into 500 responses.
func RecoverMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
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
