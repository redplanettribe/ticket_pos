package platform

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strings"
)

// OrdinaryTraceRate is the share of ordinary requests traced: one in ten
// (ADR 0079).
const OrdinaryTraceRate = 0.1

// SentryTraceHeader and BaggageHeader carry a trace from the Next app that
// started it: the trace id, the calling span and its sampling decision, and
// the trace's sampling context.
const (
	SentryTraceHeader = "sentry-trace"
	BaggageHeader     = "baggage"
)

// TraceSampling decides, once per trace at its head, whether a request is
// traced (ADR 0079):
//
//  1. a route in Never, a health check, never is, whatever the caller decided;
//  2. a route in Always always is, whatever the caller decided, so the flows
//     that matter most are never missing from the sample;
//  3. any other request arriving with a sampling decision in its sentry-trace
//     header keeps it: the Next app that started the trace decided;
//  4. any other request is traced with probability OrdinaryTraceRate.
//
// The routes are named by their ServeMux pattern. The server names them once,
// in its list of traced routes.
type TraceSampling struct {
	// Always maps each pattern always traced to the flow the server names it
	// part of, the request span's "flow" tag.
	Always map[string]string
	// Never holds the patterns never traced.
	Never map[string]bool
	// Random returns a number in [0, 1) for rule 4. Nil is math/rand/v2's
	// Float64; a test injects its own.
	Random func() float64
}

// decide is whether a request routed by pattern, arriving with the
// sentry-trace header sentryTrace, is traced, and the flow it belongs to.
func (s TraceSampling) decide(pattern, sentryTrace string) (sampled bool, flow string) {
	if s.Never[pattern] {
		return false, ""
	}
	if flow, ok := s.Always[pattern]; ok {
		return true, flow
	}
	if sampled, ok := propagatedSampling(sentryTrace); ok {
		return sampled, ""
	}
	random := s.Random
	if random == nil {
		random = rand.Float64
	}
	return random() < OrdinaryTraceRate, ""
}

// propagatedSampling reads the sampling decision a sentry-trace header
// carries, "<trace id>-<span id>-<1|0>", and whether it carries one: the
// decision is optional, and a header without one leaves it to this process.
func propagatedSampling(sentryTrace string) (sampled, ok bool) {
	parts := strings.Split(strings.TrimSpace(sentryTrace), "-")
	if len(parts) != 3 {
		return false, false
	}
	switch parts[2] {
	case "1":
		return true, true
	case "0":
		return false, true
	}
	return false, false
}

// routeMatcher is what *http.ServeMux offers: the pattern a request will be
// routed by, before it is.
type routeMatcher interface {
	Handler(r *http.Request) (h http.Handler, pattern string)
}

type requestSpanKey struct{}

// tracingMiddleware starts one span per request, a transaction named by the
// route pattern the request is about to be routed by ("unrouted" when none
// matches), continuing the trace of the sentry-trace and baggage headers
// when the request carries them and starting a new one otherwise. Sampling is
// decided here, at the head (TraceSampling), and every span started under
// the request inherits it.
//
// It sits inside the request id and outside the request log, so every report
// the request makes, the request line's included, is made under the span and
// links to its trace. The request log records the status the span answered
// (setRequestSpanStatus). With the NoopReporter, which is what no DSN means,
// no span is created.
func tracingMiddleware(reporter Reporter, sampling TraceSampling, routes routeMatcher, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pattern := ""
		if routes != nil {
			_, pattern = routes.Handler(r)
		}
		name := pattern
		if name == "" {
			name = unroutedPattern
		}
		sentryTrace := r.Header.Get(SentryTraceHeader)
		sampled, flow := sampling.decide(pattern, sentryTrace)
		ctx, span := reporter.StartSpan(r.Context(), SpanOptions{
			Op:          "http.server",
			Name:        name,
			SentryTrace: sentryTrace,
			Baggage:     r.Header.Get(BaggageHeader),
			Sampled:     sampled,
		})
		defer span.Finish()
		if flow != "" {
			span.SetTag("flow", flow)
		}
		ctx = context.WithValue(ctx, requestSpanKey{}, span)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requestSpan is the request span on ctx, if any.
func requestSpan(ctx context.Context) (Span, bool) {
	span, ok := ctx.Value(requestSpanKey{}).(Span)
	return span, ok
}

// setRequestSpanStatus records on the request span, if ctx carries one, the
// status the request answered.
func setRequestSpanStatus(ctx context.Context, status int) {
	if span, ok := requestSpan(ctx); ok {
		span.SetHTTPStatus(status)
	}
}

// startChildSpan starts a span under the request span on ctx. Outside a
// request it starts nothing and returns ctx and nil: a query at startup or in
// a background loop is not the start of a trace.
func startChildSpan(ctx context.Context, reporter Reporter, options SpanOptions) (context.Context, Span) {
	if _, ok := requestSpan(ctx); !ok {
		return ctx, nil
	}
	return reporter.StartSpan(ctx, options)
}
