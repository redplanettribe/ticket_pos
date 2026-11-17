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
		Dsn:         cfg.DSN,
		Environment: cfg.Environment,
		Release:     cfg.Release,
		ServerName:  serverName,
		// Every transaction is started with the decision the platform took
		// at the head of the trace (platform.TraceSampling), which overrides
		// the SDK's own rate: with none, nothing is kept.
		EnableTracing:    true,
		TracesSampleRate: 0,
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
//
// What the event shares with the hub's scope (breadcrumbs, fingerprint,
// tags, contexts) is replaced by scrubbed copies, never edited in place,
// since another goroutine may be reading the scope.
func stripPersonalData(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	event.Request = nil
	event.User = sentry.User{ID: event.User.ID}
	event.Message = platform.ScrubText(event.Message)
	event.Transaction = platform.ScrubURL(event.Transaction)
	event.Fingerprint = scrubStrings(event.Fingerprint)
	for i := range event.Exception {
		exception := &event.Exception[i]
		exception.Type = platform.ScrubText(exception.Type)
		exception.Value = platform.ScrubText(exception.Value)
		if exception.Mechanism != nil {
			mechanism := *exception.Mechanism
			mechanism.Data = scrubMap(mechanism.Data)
			exception.Mechanism = &mechanism
		}
		exception.Stacktrace = scrubStacktrace(exception.Stacktrace)
	}
	for i := range event.Threads {
		event.Threads[i].Stacktrace = scrubStacktrace(event.Threads[i].Stacktrace)
	}
	if event.Breadcrumbs != nil {
		breadcrumbs := make([]*sentry.Breadcrumb, len(event.Breadcrumbs))
		for i, breadcrumb := range event.Breadcrumbs {
			scrubbed := *breadcrumb
			scrubbed.Message = platform.ScrubText(breadcrumb.Message)
			scrubbed.Data = scrubMap(breadcrumb.Data)
			breadcrumbs[i] = &scrubbed
		}
		event.Breadcrumbs = breadcrumbs
	}
	event.Tags = scrubTags(event.Tags)
	if event.Contexts != nil {
		contexts := make(map[string]sentry.Context, len(event.Contexts))
		for key, values := range event.Contexts {
			contexts[key] = scrubMap(values)
		}
		event.Contexts = contexts
	}
	// A transaction's spans are finished and belong to it alone.
	for _, span := range event.Spans {
		span.Name = platform.ScrubText(span.Name)
		span.Description = platform.ScrubText(span.Description)
		span.Tags = scrubTags(span.Tags)
		span.Data = scrubMap(span.Data)
	}
	return event
}

// scrubStacktrace is stacktrace with each frame's local variables dropped,
// where a value of any kind could be, and its paths scrubbed.
func scrubStacktrace(stacktrace *sentry.Stacktrace) *sentry.Stacktrace {
	if stacktrace == nil {
		return nil
	}
	scrubbed := *stacktrace
	scrubbed.Frames = make([]sentry.Frame, len(stacktrace.Frames))
	for i, frame := range stacktrace.Frames {
		frame.Vars = nil
		frame.AbsPath = platform.ScrubURL(frame.AbsPath)
		frame.Filename = platform.ScrubURL(frame.Filename)
		scrubbed.Frames[i] = frame
	}
	return &scrubbed
}

func scrubTags(tags map[string]string) map[string]string {
	if tags == nil {
		return nil
	}
	scrubbed := make(map[string]string, len(tags))
	for key, value := range tags {
		scrubbed[key] = platform.ScrubText(value)
	}
	return scrubbed
}

func scrubStrings(texts []string) []string {
	if texts == nil {
		return nil
	}
	scrubbed := make([]string, len(texts))
	for i, text := range texts {
		scrubbed[i] = platform.ScrubText(text)
	}
	return scrubbed
}

// scrubMap is a scrubbed copy of a free-form map, at any depth. A key that
// names a URL loses everything from its query string on.
func scrubMap(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	scrubbed := make(map[string]any, len(data))
	for key, value := range data {
		if droppedKeys[key] {
			continue
		}
		if text, ok := value.(string); ok && urlKeys[key] {
			scrubbed[key] = platform.ScrubURL(text)
			continue
		}
		scrubbed[key] = scrubValue(value)
	}
	return scrubbed
}

// urlKeys are the keys of span and breadcrumb data whose whole value is a URL,
// as the Next apps' scrubber names them.
var urlKeys = map[string]bool{"url": true, "from": true, "to": true, "http.url": true, "url.full": true}

// droppedKeys hold a query string or fragment already split off its URL,
// which has no "?" left to mark it, so they are not sent at all.
var droppedKeys = map[string]bool{"url.query": true, "url.fragment": true, "http.query": true, "http.fragment": true}

func scrubValue(value any) any {
	switch v := value.(type) {
	case string:
		return platform.ScrubText(v)
	case map[string]any:
		return scrubMap(v)
	case []any:
		scrubbed := make([]any, len(v))
		for i := range v {
			scrubbed[i] = scrubValue(v[i])
		}
		return scrubbed
	case []string:
		return scrubStrings(v)
	case nil, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
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
// report made inside a request span joins its trace. The span it links to is
// the one on ctx, the span the report was made under: the hub's scope holds
// whichever span of the request started last, a query still running on
// another goroutine included, and this runs later, on the AsyncReporter's.
func (r *sentryReporter) scope(ctx context.Context) *sentry.Scope {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		return sentry.NewScope()
	}
	scope := hub.Scope().Clone()
	if span := sentry.SpanFromContext(ctx); span != nil {
		scope.SetSpan(span)
	}
	return scope
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
		sampled := sentry.SampledFalse
		if options.Sampled {
			sampled = sentry.SampledTrue
		}
		spanOptions = append(spanOptions,
			sentry.WithTransactionName(options.Name),
			sentry.WithTransactionSource(sentry.SourceRoute),
			// The decision taken at the head; it wins over the propagated
			// one, which the platform has already weighed.
			sentry.WithSpanSampled(sampled),
		)
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
	return &sentryCheckIn{client: r.client, job: job, id: id}
}

type sentryCheckIn struct {
	client *sentry.Client
	job    string
	id     *sentry.EventID
}

func (c *sentryCheckIn) Finish(status platform.CheckInStatus, duration time.Duration) {
	checkIn := &sentry.CheckIn{
		MonitorSlug: c.job,
		Status:      sentry.CheckInStatusOK,
		Duration:    duration,
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
