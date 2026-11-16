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
	"sync/atomic"
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
	// The transport is built before the AsyncReporter it reports failures to,
	// which wraps the client built on it. A failure before that is not lost
	// silently by a nil dereference on the SDK's goroutine: it is logged.
	var async atomic.Pointer[platform.AsyncReporter]
	deliveries := &failureReportingTransport{next: transport, failed: func(err error) {
		if r := async.Load(); r != nil {
			r.Failed(err)
			return
		}
		logger.Warn("error reporting failed", "error", err)
	}}

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
		HTTPTransport: deliveries,
	})
	if err != nil {
		logger.Warn("error reporting disabled: SENTRY_DSN refused", "error", err)
		return platform.NoopReporter{}
	}
	reporter := platform.NewAsyncReporter(&sentryReporter{client: client}, logger)
	async.Store(reporter)
	return reporter
}

// stripPersonalData is the last thing an event or transaction passes before it
// leaves the process (ADR 0079): the request is dropped, the user keeps only
// its opaque id, and every string that could carry text, platform.ScrubText
// runs on. The request and the user's other fields are never attached; this
// is the net under that.
func stripPersonalData(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	event.Request = nil
	event.User = sentry.User{ID: event.User.ID}
	event.Message = platform.ScrubText(event.Message)
	event.Transaction = platform.ScrubURL(event.Transaction)
	for i := range event.Fingerprint {
		event.Fingerprint[i] = platform.ScrubText(event.Fingerprint[i])
	}
	for i := range event.Exception {
		exception := &event.Exception[i]
		exception.Type = platform.ScrubText(exception.Type)
		exception.Value = platform.ScrubText(exception.Value)
		if exception.Mechanism != nil {
			exception.Mechanism.Data = scrubMap(exception.Mechanism.Data)
		}
		scrubStacktrace(exception.Stacktrace)
	}
	for i := range event.Threads {
		scrubStacktrace(event.Threads[i].Stacktrace)
	}
	for _, breadcrumb := range event.Breadcrumbs {
		breadcrumb.Message = platform.ScrubText(breadcrumb.Message)
		breadcrumb.Data = scrubMap(breadcrumb.Data)
	}
	event.Tags = scrubTags(event.Tags)
	for key, context := range event.Contexts {
		event.Contexts[key] = scrubMap(context)
	}
	for _, span := range event.Spans {
		span.Description = platform.ScrubText(span.Description)
		span.Tags = scrubTags(span.Tags)
		span.Data = scrubMap(span.Data)
	}
	return event
}

// scrubStacktrace drops each frame's local variables, where a value of any
// kind could be, and scrubs its paths.
func scrubStacktrace(stacktrace *sentry.Stacktrace) {
	if stacktrace == nil {
		return
	}
	for i := range stacktrace.Frames {
		frame := &stacktrace.Frames[i]
		frame.Vars = nil
		frame.AbsPath = platform.ScrubURL(frame.AbsPath)
		frame.Filename = platform.ScrubURL(frame.Filename)
	}
}

func scrubTags(tags map[string]string) map[string]string {
	for key, value := range tags {
		tags[key] = platform.ScrubText(value)
	}
	return tags
}

// scrubMap scrubs the strings of a free-form map, at any depth. A key that
// names a URL loses everything from its query string on.
func scrubMap(data map[string]any) map[string]any {
	for key, value := range data {
		if text, ok := value.(string); ok && urlKeys[key] {
			data[key] = platform.ScrubURL(text)
			continue
		}
		data[key] = scrubValue(value)
	}
	return data
}

// urlKeys are the keys of span and breadcrumb data whose whole value is a URL,
// as the Next apps' scrubber names them.
var urlKeys = map[string]bool{"url": true, "from": true, "to": true, "http.url": true, "url.full": true}

func scrubValue(value any) any {
	switch v := value.(type) {
	case string:
		return platform.ScrubText(v)
	case map[string]any:
		return scrubMap(v)
	case []any:
		for i := range v {
			v[i] = scrubValue(v[i])
		}
		return v
	case []string:
		for i := range v {
			v[i] = platform.ScrubText(v[i])
		}
		return v
	case nil, bool, int, int32, int64, uint, uint32, uint64, float32, float64:
		return v
	default:
		// A value of a type this cannot see into is sent as scrubbed text.
		return platform.ScrubText(fmt.Sprint(v))
	}
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
// global hub. It runs behind an AsyncReporter: error reports and check-ins on
// its worker goroutine, Flush on a goroutine of its own, spans on the caller's.
// The SDK client is safe for that concurrent use.
type sentryReporter struct {
	client *sentry.Client
}

func (r *sentryReporter) ReportError(ctx context.Context, report platform.ErrorReport) {
	// One exception, its text scrubbed and its type the error's class: words
	// that carry nothing of the error's own text, such as "postgres 23505".
	// The SDK's own exception chain would send every wrapped error's raw text
	// a second time.
	err := platform.ScrubError(report.Err)
	value := ""
	if err != nil {
		value = err.Error()
	}
	event := sentry.NewEvent()
	event.Level = sentry.LevelError
	event.Exception = []sentry.Exception{{
		Type:  platform.ErrorClass(report.Err),
		Value: value,
		// The stack is where the error happened, not this goroutine.
		Stacktrace: stacktrace(report.Stack),
	}}
	scope := r.scope(ctx)
	scope.SetTags(report.Tags)
	if report.RequestID != "" {
		scope.SetTag("request_id", report.RequestID)
	}
	if report.UserID != "" {
		scope.SetUser(sentry.User{ID: report.UserID})
	}
	r.client.CaptureEvent(event, &sentry.EventHint{Context: ctx, OriginalException: err}, scope)
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
