// Package reportertest is a capturing platform.Reporter for tests: it keeps
// what would have left the process, so a test asserts on reports, spans and
// check-ins and never on how a vendor SDK was called (ADR 0079).
package reportertest

import (
	"context"
	"maps"
	"sync"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Reporter records everything reported to it. The zero value is ready to use
// and safe for concurrent use.
type Reporter struct {
	mu       sync.Mutex
	errors   []platform.ErrorReport
	spans    []*Span
	checkIns []CheckIn
	flushes  []time.Duration
}

var _ platform.Reporter = (*Reporter)(nil)

// New returns an empty capturing Reporter.
func New() *Reporter {
	return &Reporter{}
}

// Span is a span as the fake recorded it.
type Span struct {
	reporter *Reporter
	// Options are what the span was started with.
	Options platform.SpanOptions
	// Parent is the span it was started under, nil for a transaction.
	Parent     *Span
	Tags       map[string]string
	HTTPStatus int
	Finished   bool
}

// CheckIn is one check-in as the fake recorded it.
type CheckIn struct {
	Job    string
	Status platform.CheckInStatus
	// Duration is the time since the start, for an ok or failed check-in.
	Duration time.Duration
}

type spanKey struct{}

func (r *Reporter) ReportError(_ context.Context, report platform.ErrorReport) {
	report.Tags = maps.Clone(report.Tags)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors = append(r.errors, report)
}

func (r *Reporter) StartSpan(ctx context.Context, options platform.SpanOptions) (context.Context, platform.Span) {
	parent, _ := ctx.Value(spanKey{}).(*Span)
	span := &Span{reporter: r, Options: options, Parent: parent, Tags: map[string]string{}}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans = append(r.spans, span)
	return context.WithValue(ctx, spanKey{}, span), span
}

func (s *Span) SetTag(key, value string) {
	s.reporter.mu.Lock()
	defer s.reporter.mu.Unlock()
	s.Tags[key] = value
}

func (s *Span) SetHTTPStatus(status int) {
	s.reporter.mu.Lock()
	defer s.reporter.mu.Unlock()
	s.HTTPStatus = status
}

func (s *Span) Finish() {
	s.reporter.mu.Lock()
	defer s.reporter.mu.Unlock()
	s.Finished = true
}

func (r *Reporter) CheckIn(_ context.Context, job string) platform.JobCheckIn {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkIns = append(r.checkIns, CheckIn{Job: job, Status: platform.CheckInInProgress})
	return &jobCheckIn{reporter: r, job: job, started: time.Now()}
}

type jobCheckIn struct {
	reporter *Reporter
	job      string
	started  time.Time
}

func (c *jobCheckIn) Finish(status platform.CheckInStatus) {
	c.reporter.mu.Lock()
	defer c.reporter.mu.Unlock()
	c.reporter.checkIns = append(c.reporter.checkIns, CheckIn{Job: c.job, Status: status, Duration: time.Since(c.started)})
}

func (r *Reporter) Flush(timeout time.Duration) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushes = append(r.flushes, timeout)
	return true
}

// Errors returns the error reports received, in order.
func (r *Reporter) Errors() []platform.ErrorReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]platform.ErrorReport(nil), r.errors...)
}

// Spans returns the spans started, in order. Read their fields once the code
// under test is done with them.
func (r *Reporter) Spans() []*Span {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*Span(nil), r.spans...)
}

// CheckIns returns the check-ins received, in order.
func (r *Reporter) CheckIns() []CheckIn {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]CheckIn(nil), r.checkIns...)
}

// Flushes returns the timeout of every Flush, in order.
func (r *Reporter) Flushes() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.flushes...)
}
