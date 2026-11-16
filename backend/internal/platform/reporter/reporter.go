// Package reporter builds the API's platform.Reporter. It is the one package
// that imports sentry-go (ADR 0079): the vendor is swappable here and nowhere
// else, and TestOnlyThisPackageImportsTheSentrySDK keeps it that way.
//
// Only cmd/server builds a reporter. The migrate job and the operator CLIs do
// not import this package.
package reporter

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"slices"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// serverName stands in for the host's name, which the SDK reports by default
// and which on a developer's machine that opted in can be a person's name.
const serverName = "ticket-pos-api"

// tracesSampleRate is the share of traces kept: one request in ten (ADR 0079).
const tracesSampleRate = 0.1

// New returns the reporter cfg describes. With no DSN it is
// platform.NoopReporter and nothing of the SDK is constructed. Otherwise it is
// the SDK client behind a platform.AsyncReporter, whose first failure,
// including a failed delivery, is logged through logger at Warn.
//
// A DSN the SDK refuses is logged at Warn and leaves the process on the no-op:
// error reporting is never the reason the API does not start.
func New(cfg platform.ReporterConfig, logger platform.Logger) platform.Reporter {
	if cfg.DSN == "" {
		return platform.NoopReporter{}
	}
	return newWithTransport(cfg, logger, http.DefaultTransport)
}

func newWithTransport(cfg platform.ReporterConfig, logger platform.Logger, transport http.RoundTripper) platform.Reporter {
	var async *platform.AsyncReporter
	failures := &failureReportingTransport{next: transport, failed: func(err error) { async.Failed(err) }}

	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:              cfg.DSN,
		Environment:      cfg.Environment,
		Release:          cfg.Release,
		ServerName:       serverName,
		EnableTracing:    true,
		TracesSampleRate: tracesSampleRate,
		// No personal data is collected by default: no request bodies,
		// cookies, headers, query strings or user fields (ADR 0079).
		DataCollection: &sentry.DataCollection{
			UserInfo:    sentry.Set(false),
			Cookies:     &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
			HTTPHeaders: &sentry.HeaderCollectionConfig{Request: &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff}, Response: &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff}},
			HTTPBodies:  []sentry.BodyType{},
			QueryParams: &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
		},
		BeforeSend:            stripPersonalData,
		BeforeSendTransaction: stripPersonalData,
		// Delivery goes through the SDK's own asynchronous transport, over
		// this RoundTripper, which turns a failed delivery into the
		// reporter's failure.
		HTTPTransport: failures,
	})
	if err != nil {
		logger.Warn("error reporting disabled: SENTRY_DSN refused", "error", err)
		return platform.NoopReporter{}
	}
	async = platform.NewAsyncReporter(&sentryReporter{client: client}, logger)
	return async
}

// stripPersonalData drops what an event may carry about a person beyond an
// opaque id. The request is never attached; this is the net under that.
func stripPersonalData(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	event.Request = nil
	event.User = sentry.User{ID: event.User.ID}
	return event
}

// failureReportingTransport is the SDK's HTTP transport, reporting a failed
// delivery, a transport error or a refusal, to failed.
type failureReportingTransport struct {
	next   http.RoundTripper
	failed func(error)
}

func (t *failureReportingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	switch {
	case err != nil:
		t.failed(fmt.Errorf("delivering to Sentry: %w", err))
	case resp.StatusCode >= http.StatusBadRequest:
		t.failed(fmt.Errorf("delivering to Sentry: status %d", resp.StatusCode))
	}
	return resp, err
}

// sentryReporter is platform.Reporter over one SDK client, never the SDK's
// global hub. It runs behind an AsyncReporter, on its goroutine.
type sentryReporter struct {
	client *sentry.Client
}

func (r *sentryReporter) ReportError(ctx context.Context, report platform.ErrorReport) {
	event := r.client.EventFromException(report.Err, sentry.LevelError)
	// The exception's stack is where the error happened, not this goroutine.
	if n := len(event.Exception); n > 0 {
		event.Exception[n-1].Stacktrace = stacktrace(report.Stack)
	}
	scope := r.scope(ctx)
	scope.SetTags(report.Tags)
	if report.RequestID != "" {
		scope.SetTag("request_id", report.RequestID)
	}
	r.client.CaptureEvent(event, &sentry.EventHint{Context: ctx, OriginalException: report.Err}, scope)
}

// scope is a fresh scope, or a copy of the scope of the hub on ctx, so that a
// report made inside a request span joins its trace.
func (r *sentryReporter) scope(ctx context.Context) *sentry.Scope {
	if hub := sentry.GetHubFromContext(ctx); hub != nil {
		return hub.Scope().Clone()
	}
	return sentry.NewScope()
}

// stacktrace converts a platform stack, innermost first, into Sentry's,
// outermost first, without the runtime's and the test runner's frames.
func stacktrace(stack []runtime.Frame) *sentry.Stacktrace {
	frames := make([]sentry.Frame, 0, len(stack))
	for _, frame := range slices.Backward(stack) {
		f := sentry.NewFrame(frame)
		if f.Module == "runtime" || f.Module == "testing" {
			continue
		}
		frames = append(frames, f)
	}
	if len(frames) == 0 {
		return nil
	}
	return &sentry.Stacktrace{Frames: frames}
}

func (r *sentryReporter) StartSpan(ctx context.Context, options platform.SpanOptions) (context.Context, platform.Span) {
	if !sentry.HasHubOnContext(ctx) {
		ctx = sentry.SetHubOnContext(ctx, sentry.NewHub(r.client, sentry.NewScope()))
	}
	spanOptions := []sentry.SpanOption{sentry.WithDescription(options.Name)}
	if sentry.TransactionFromContext(ctx) == nil {
		spanOptions = append(spanOptions, sentry.WithTransactionName(options.Name))
		if options.SentryTrace != "" {
			spanOptions = append(spanOptions, sentry.ContinueFromHeaders(options.SentryTrace, options.Baggage))
		}
	}
	span := sentry.StartSpan(ctx, options.Op, spanOptions...)
	return span.Context(), sentrySpan{span: span}
}

type sentrySpan struct {
	span *sentry.Span
}

func (s sentrySpan) SetTag(key, value string) { s.span.SetTag(key, value) }

func (s sentrySpan) SetHTTPStatus(status int) {
	s.span.Status = sentry.HTTPtoSpanStatus(status)
	s.span.SetData("http.response.status_code", status)
}

func (s sentrySpan) Finish() { s.span.Finish() }

func (r *sentryReporter) CheckIn(_ context.Context, job string) platform.JobCheckIn {
	id := r.client.CaptureCheckIn(&sentry.CheckIn{MonitorSlug: job, Status: sentry.CheckInStatusInProgress}, nil, nil)
	return &sentryCheckIn{client: r.client, job: job, id: id, started: time.Now()}
}

type sentryCheckIn struct {
	client  *sentry.Client
	job     string
	id      *sentry.EventID
	started time.Time
}

func (c *sentryCheckIn) Finish(status platform.CheckInStatus) {
	checkIn := &sentry.CheckIn{
		MonitorSlug: c.job,
		Status:      sentry.CheckInStatusOK,
		Duration:    time.Since(c.started),
	}
	if status != platform.CheckInOK {
		checkIn.Status = sentry.CheckInStatusError
	}
	if c.id != nil {
		checkIn.ID = *c.id
	}
	c.client.CaptureCheckIn(checkIn, nil, nil)
}

func (r *sentryReporter) Flush(timeout time.Duration) bool {
	return r.client.Flush(timeout)
}
