package integration

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/reportertest"
)

// EVERY SCHEDULED JOB CHECKS IN (#767, ADR 0079). Each of the ten routes Cloud
// Scheduler calls checks in as started and then as ok or failed, under the
// name its monitor carries, and flushes before it answers. The names are the
// contract with the monitors Terraform declares, so they are spelled out here
// rather than read back from the code under test.
var scheduledJobRoutes = []struct {
	path string
	job  string
}{
	{"/api/v1/internal/reversals/drain", "ticket-pos-reversal-reconciler"},
	// Answers 404 SALE_INVOICING_UNAVAILABLE while its flag is closed, as it
	// is here: a refusal as designed, which checks in ok.
	{"/api/v1/internal/sale-invoices/drain", "ticket-pos-sale-invoice-drainer"},
	{"/api/v1/internal/follow-digests/enqueue", "ticket-pos-follow-digest-enqueue"},
	{"/api/v1/internal/follow-digests/drain", "ticket-pos-follow-digest-drain"},
	{"/api/v1/internal/checkout-answers/purge", "ticket-pos-answer-purge"},
	{"/api/v1/internal/answer-reminders/sweep", "ticket-pos-answer-reminder"},
	{"/api/v1/internal/assignment-reminders/sweep", "ticket-pos-assignment-reminder"},
	{"/api/v1/internal/owed-assignment-mails/sweep", "ticket-pos-owed-assignment-mail"},
	{"/api/v1/internal/holder-addresses/purge", "ticket-pos-holder-address-purge"},
	{"/api/v1/internal/platform-trends/close", "ticket-pos-daily-close"},
}

func TestEachScheduledJobChecksInStartedThenOK(t *testing.T) {
	setupTest(t)
	for _, route := range scheduledJobRoutes {
		t.Run(route.job, func(t *testing.T) {
			fake := reportertest.New()
			var now atomic.Int64
			now.Store(fixedClock.UnixNano())
			reporting, _ := startAppReportingTo(t, fake, &now)

			resp, body := reporting.post(t, route.path, nil, nil)
			if resp.StatusCode >= http.StatusInternalServerError {
				t.Fatalf("status = %d error=%+v, want the run to succeed", resp.StatusCode, body.Error)
			}

			checkIns := fake.CheckIns()
			if len(checkIns) != 2 {
				t.Fatalf("got %d check-ins, want started then ok: %+v", len(checkIns), checkIns)
			}
			if checkIns[0] != (reportertest.CheckIn{Job: route.job, Status: platform.CheckInInProgress}) {
				t.Errorf("first check-in = %+v, want %s in progress", checkIns[0], route.job)
			}
			if checkIns[1].Job != route.job || checkIns[1].Status != platform.CheckInOK {
				t.Errorf("last check-in = %+v, want %s ok", checkIns[1], route.job)
			}
			if checkIns[1].Duration <= 0 {
				t.Errorf("the ok check-in carries no duration: %+v", checkIns[1])
			}
			if flushes := fake.Flushes(); len(flushes) != 1 || flushes[0] != platform.ShutdownFlushTimeout {
				t.Errorf("flushes = %v, want one bounded by %s", flushes, platform.ShutdownFlushTimeout)
			}
		})
	}
}

// A run that cannot reach its database answers 500: it checks in failed, and
// what it reports names the job by its monitor slug.
func TestAScheduledJobThatFailsChecksInFailedAndItsReportCarriesTheJobTag(t *testing.T) {
	setupTest(t)
	const job = "ticket-pos-reversal-reconciler"
	fake := reportertest.New()
	var now atomic.Int64
	now.Store(fixedClock.UnixNano())
	reporting, app := startAppReportingTo(t, fake, &now)
	app.DB.Pool.Close()

	resp, _ := reporting.post(t, "/api/v1/internal/reversals/drain", nil, nil)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}

	checkIns := fake.CheckIns()
	if len(checkIns) != 2 || checkIns[1].Job != job || checkIns[1].Status != platform.CheckInFailed {
		t.Fatalf("check-ins = %+v, want %s started then failed", checkIns, job)
	}
	if checkIns[1].Duration <= 0 {
		t.Errorf("the failed check-in carries no duration: %+v", checkIns[1])
	}
	reports := fake.Errors()
	if len(reports) == 0 {
		t.Fatal("the failed run reported nothing")
	}
	for _, report := range reports {
		if report.Tags["job"] != job {
			t.Errorf("report %q has job tag %q, want %q", report.Err, report.Tags["job"], job)
		}
	}
}

// blockingReporter is a vendor reporter that never comes back: every call
// waits for the test to end.
type blockingReporter struct{ release chan struct{} }

func (b blockingReporter) ReportError(context.Context, platform.ErrorReport) { <-b.release }

func (b blockingReporter) StartSpan(ctx context.Context, _ platform.SpanOptions) (context.Context, platform.Span) {
	return platform.NoopReporter{}.StartSpan(ctx, platform.SpanOptions{})
}

func (b blockingReporter) CheckIn(context.Context, string) platform.JobCheckIn {
	<-b.release
	return platform.NoopReporter{}.CheckIn(context.Background(), "")
}

func (b blockingReporter) Flush(time.Duration) bool {
	<-b.release
	return false
}

// The flush comes before the answer and is bounded: behind the AsyncReporter
// production wraps the vendor in, a reporter that blocks holds the answer back
// for the bound and no longer, and the run still answers as it would have.
func TestAScheduledJobWhoseReporterBlocksAnswersWithinTheFlushBound(t *testing.T) {
	setupTest(t)
	inner := blockingReporter{release: make(chan struct{})}
	t.Cleanup(func() { close(inner.release) })
	logger := platform.NewSlogLogger(slog.New(slog.NewTextHandler(io.Discard, nil)), platform.NoopReporter{})
	var now atomic.Int64
	now.Store(fixedClock.UnixNano())
	reporting, _ := startAppReportingTo(t, platform.NewAsyncReporter(inner, logger), &now)

	started := time.Now()
	resp, body := reporting.post(t, "/api/v1/internal/checkout-answers/purge", nil, nil)
	elapsed := time.Since(started)

	if resp.StatusCode != http.StatusOK || body.Error != nil {
		t.Fatalf("status = %d error=%+v, want the run's own 200", resp.StatusCode, body.Error)
	}
	if elapsed < platform.ShutdownFlushTimeout {
		t.Errorf("answered after %s, before the flush's bound of %s: the answer did not wait for the flush", elapsed, platform.ShutdownFlushTimeout)
	}
	if limit := platform.ShutdownFlushTimeout + 1500*time.Millisecond; elapsed > limit {
		t.Errorf("answered after %s, want no more than the bound (%s) and the run itself", elapsed, platform.ShutdownFlushTimeout)
	}
}
