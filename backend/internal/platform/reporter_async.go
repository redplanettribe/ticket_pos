package platform

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"
)

// defaultReportQueue bounds the error reports and check-ins waiting for the
// inner reporter. A reporter that falls this far behind is failing, and what
// does not fit is dropped rather than waited for.
const defaultReportQueue = 256

// errReportQueueFull is the failure an AsyncReporter logs when its inner
// reporter stops keeping up.
var errReportQueueFull = errors.New("error reporter is not keeping up; reports are being dropped")

// AsyncReporter stands between the code that reports and the reporter that
// delivers, so that reporting never costs a request (ADR 0079).
//
// Error reports and check-ins are queued for one goroutine that hands them to
// the inner reporter in order. A full queue drops the report instead of
// waiting. A panic in the inner reporter is recovered. The first failure of
// either kind, and the first the inner reporter declares through Failed, is
// logged once at Warn; every later failure is swallowed silently, so a
// reporter that is down does not write one line per event.
//
// Spans cannot be queued, since the caller needs the span's context back at
// once; they are passed straight through, with panics recovered, and the
// inner reporter's span methods must do no I/O.
type AsyncReporter struct {
	inner  Reporter
	logger Logger
	queue  chan func()
	warned sync.Once
}

// NewAsyncReporter wraps inner, logging its first failure through logger.
func NewAsyncReporter(inner Reporter, logger Logger) *AsyncReporter {
	return newAsyncReporter(inner, logger, defaultReportQueue)
}

func newAsyncReporter(inner Reporter, logger Logger, queueSize int) *AsyncReporter {
	r := &AsyncReporter{inner: inner, logger: logger, queue: make(chan func(), queueSize)}
	go r.work()
	return r
}

func (r *AsyncReporter) work() {
	for job := range r.queue {
		r.guard(job)
	}
}

// guard runs f, turning a panic into the reporter's failure.
func (r *AsyncReporter) guard(f func()) {
	defer func() {
		if rec := recover(); rec != nil {
			r.Failed(fmt.Errorf("error reporter panicked: %v", rec))
		}
	}()
	f()
}

// Failed logs the reporter's first failure at Warn and swallows the rest. The
// vendor-backed reporter calls it when a delivery fails.
func (r *AsyncReporter) Failed(err error) {
	r.warned.Do(func() {
		r.logger.Warn("error reporting failed; further failures are not logged", "error", err)
	})
}

// enqueue queues job, or drops it when the queue is full.
func (r *AsyncReporter) enqueue(job func()) {
	select {
	case r.queue <- job:
	default:
		r.Failed(errReportQueueFull)
	}
}

// ReportError queues the report. A report without a stack gets the stack of
// the caller, taken here, since the inner reporter runs on another goroutine.
func (r *AsyncReporter) ReportError(ctx context.Context, report ErrorReport) {
	if len(report.Stack) == 0 {
		report.Stack = stackFrom(1)
	}
	report.Tags = maps.Clone(report.Tags)
	r.enqueue(func() { r.inner.ReportError(ctx, report) })
}

func (r *AsyncReporter) StartSpan(ctx context.Context, options SpanOptions) (spanCtx context.Context, span Span) {
	spanCtx, span = ctx, noopSpan{}
	r.guard(func() {
		innerCtx, innerSpan := r.inner.StartSpan(ctx, options)
		spanCtx, span = innerCtx, guardedSpan{span: innerSpan, reporter: r}
	})
	return spanCtx, span
}

type guardedSpan struct {
	span     Span
	reporter *AsyncReporter
}

func (s guardedSpan) SetTag(key, value string) {
	s.reporter.guard(func() { s.span.SetTag(key, value) })
}

func (s guardedSpan) SetHTTPStatus(status int) {
	s.reporter.guard(func() { s.span.SetHTTPStatus(status) })
}

func (s guardedSpan) Finish() {
	s.reporter.guard(s.span.Finish)
}

// CheckIn queues the started check-in. Its Finish is queued behind it, so the
// inner reporter sees the two in order on its one goroutine.
func (r *AsyncReporter) CheckIn(ctx context.Context, job string) JobCheckIn {
	checkIn := &queuedCheckIn{reporter: r}
	r.enqueue(func() { checkIn.inner = r.inner.CheckIn(ctx, job) })
	return checkIn
}

type queuedCheckIn struct {
	reporter *AsyncReporter
	// inner is written and read only on the reporter's goroutine.
	inner JobCheckIn
}

func (c *queuedCheckIn) Finish(status CheckInStatus, duration time.Duration) {
	c.reporter.enqueue(func() {
		// A start dropped from a full queue leaves nothing to finish.
		if c.inner != nil {
			c.inner.Finish(status, duration)
		}
	})
}

// Flush waits, within timeout, for everything queued before it to reach the
// inner reporter, and then for the inner reporter to deliver it in what is
// left of timeout. A reporter that is stuck does not stretch the bound.
func (r *AsyncReporter) Flush(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	drained := make(chan struct{})
	select {
	case r.queue <- func() { close(drained) }:
	case <-timer.C:
		return false
	}
	select {
	case <-drained:
	case <-timer.C:
		return false
	}

	remaining := time.Until(deadline)
	if remaining <= 0 {
		return false
	}
	done := make(chan bool, 1)
	go func() {
		flushed := false
		r.guard(func() { flushed = r.inner.Flush(remaining) })
		done <- flushed
	}()
	select {
	case flushed := <-done:
		return flushed
	case <-timer.C:
		return false
	}
}
