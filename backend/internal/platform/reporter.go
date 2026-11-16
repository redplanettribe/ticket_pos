package platform

import (
	"context"
	"runtime"
	"strings"
	"time"
)

// Reporter is the one seam between the backend and its error reporting vendor
// (ADR 0079). The pipeline, the Logger and the job routes report through it;
// only the package internal/platform/reporter knows the vendor is Sentry, and
// no other package imports its SDK.
//
// No method may wait on the network. The vendor-backed Reporter is always
// wrapped in an AsyncReporter, which takes error reports and check-ins off the
// caller's goroutine and swallows the inner reporter's failures after logging
// the first one at Warn.
//
// With no DSN configured the Reporter is NoopReporter and nothing
// vendor-related is constructed: that is the local stack, CI, the test suites
// and unattended agent runs.
type Reporter interface {
	// ReportError reports err with its context. It never blocks on delivery.
	ReportError(ctx context.Context, report ErrorReport)
	// StartSpan starts a span as a child of the span on ctx, or a new
	// transaction when ctx carries none, and returns the context carrying it.
	// The caller finishes the span.
	StartSpan(ctx context.Context, options SpanOptions) (context.Context, Span)
	// CheckIn checks the job in as started, and returns the check-in that
	// reports how it ended.
	CheckIn(ctx context.Context, job string) JobCheckIn
	// Flush waits up to timeout for what has been reported to be delivered,
	// and reports whether it was.
	Flush(timeout time.Duration) bool
}

// ErrorReport is one error as reported: the error, where it happened, and the
// ids that place it. Context travels as ids only; never a name, slug, email or
// identity number (ADR 0079).
type ErrorReport struct {
	Err error
	// Stack is the stack the error happened on, innermost frame first. For a
	// recovered panic it starts at the frame that panicked. Left empty, the
	// AsyncReporter fills in the stack of whoever called ReportError.
	Stack []runtime.Frame
	// RequestID is the request's X-Request-ID, the join back to Cloud Logging;
	// empty outside a request.
	RequestID string
	// UserID is the opaque internal id of the person the request acted for:
	// a staff Member id or a Customer id, never more of them (SetReportActor).
	// Empty when nobody is known.
	UserID string
	// Tags are id-only context: the route pattern, the audience, and the
	// Organization, Event and Ticket Sale ids a request concerns (reportPlacement).
	Tags map[string]string
}

// SpanOptions describe a span to start.
type SpanOptions struct {
	// Op is the kind of span, such as "http.server", "db.sql" or
	// "http.client".
	Op string
	// Name is what the span is: a route pattern, a statement without its
	// arguments, or a method, host and path without a query string.
	Name string
	// SentryTrace and Baggage are the inbound trace headers a request span
	// continues the caller's trace from. Empty starts a new trace.
	SentryTrace string
	Baggage     string
	// Sampled is whether a span that starts a trace on this process, a
	// request span, is traced: decided once, at the head (TraceSampling). A
	// span started under another inherits its decision and ignores this.
	Sampled bool
}

// Span is a started span.
type Span interface {
	// SetTag sets an id-only tag on the span.
	SetTag(key, value string)
	// SetHTTPStatus records the status a request answered, or an outbound
	// call received.
	SetHTTPStatus(status int)
	// Finish ends the span.
	Finish()
}

// CheckInStatus is how a job run ended.
type CheckInStatus string

const (
	CheckInInProgress CheckInStatus = "in_progress"
	CheckInOK         CheckInStatus = "ok"
	CheckInFailed     CheckInStatus = "error"
)

// JobCheckIn is a job run that has checked in as started.
type JobCheckIn interface {
	// Finish checks the run in as ended with status, CheckInOK or
	// CheckInFailed, with the time the run took. The caller measures it,
	// since the AsyncReporter hands the check-ins over later.
	Finish(status CheckInStatus, duration time.Duration)
}

// ShutdownFlushTimeout bounds every flush: on graceful shutdown, after a
// startup failure, and before a job route answers (ADR 0079). Cloud Run allows
// ten seconds between SIGTERM and SIGKILL, and may freeze an instance between
// requests.
const ShutdownFlushTimeout = 2 * time.Second

// ReportStartupFailure reports a failure that stops the process, at startup or
// when the server stops on its own, and flushes it with ShutdownFlushTimeout, so the report is out before the
// process exits.
func ReportStartupFailure(reporter Reporter, err error) {
	reporter.ReportError(context.Background(), ErrorReport{Err: err})
	reporter.Flush(ShutdownFlushTimeout)
}

// NoopReporter is the Reporter of a process with no DSN configured. It reports
// nothing and constructs nothing.
type NoopReporter struct{}

func (NoopReporter) ReportError(context.Context, ErrorReport) {}

func (NoopReporter) StartSpan(ctx context.Context, _ SpanOptions) (context.Context, Span) {
	return ctx, noopSpan{}
}

func (NoopReporter) CheckIn(context.Context, string) JobCheckIn { return noopCheckIn{} }

func (NoopReporter) Flush(time.Duration) bool { return true }

type noopSpan struct{}

func (noopSpan) SetTag(string, string) {}
func (noopSpan) SetHTTPStatus(int)     {}
func (noopSpan) Finish()               {}

type noopCheckIn struct{}

func (noopCheckIn) Finish(CheckInStatus, time.Duration) {}

// stackFrom returns the calling goroutine's stack, innermost first, skipping
// skip frames above the caller of stackFrom.
func stackFrom(skip int) []runtime.Frame {
	pcs := make([]uintptr, 64)
	n := runtime.Callers(skip+2, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	var stack []runtime.Frame
	for {
		frame, more := frames.Next()
		stack = append(stack, frame)
		if !more {
			return stack
		}
	}
}

// panicStack returns, from inside a deferred recover, the stack of the frame
// that panicked: everything above runtime.gopanic is the recovery itself, and
// the runtime frames just below it (runtime.panicmem and runtime.sigpanic for
// a nil dereference, runtime.panicdivide, ...) are how the runtime raised it.
func panicStack() []runtime.Frame {
	return stackOrPanicStack(1)
}

// stackOrPanicStack returns the stack of the caller skip frames above the
// caller of stackOrPanicStack or, when that is a deferred call running while a
// panic unwinds, the stack of the frame that panicked (see panicStack).
func stackOrPanicStack(skip int) []runtime.Frame {
	stack := stackFrom(skip + 1)
	for i, frame := range stack {
		if frame.Function != "runtime.gopanic" {
			continue
		}
		stack = stack[i+1:]
		for len(stack) > 1 && strings.HasPrefix(stack[0].Function, "runtime.") {
			stack = stack[1:]
		}
		return stack
	}
	return stack
}

// reportInRequest reports, and marks the request on ctx, if any, as having
// reported. An Error line takes this path: each one is reported.
func reportInRequest(ctx context.Context, reporter Reporter, report ErrorReport) {
	reports := requestReportsOn(ctx)
	if reports != nil {
		reports.reported.Store(true)
	}
	reporter.ReportError(ctx, reports.place(report))
}

// reportRequestFailure reports what ends a request in a 5xx (a panic, an
// unexpected error, a 5xx domain error, or the request line of any other 5xx)
// unless the request has already reported: the Error line that wrote its cause
// down, or the panic recovery before the request line.
func reportRequestFailure(ctx context.Context, reporter Reporter, report ErrorReport) {
	reports := requestReportsOn(ctx)
	if reports != nil && reports.reported.Swap(true) {
		return
	}
	reporter.ReportError(ctx, reports.place(report))
}
