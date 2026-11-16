package server_test

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/reportertest"
	"github.com/peter/ticket_pos/backend/internal/server"
)

// Every scheduled job checks in through one wrapper (#767, ADR 0079):
// server.CheckInJobs, the Router registerInternalRoutes registers through.
// These tests register a tenth route through it, as a new internal route
// would be, and serve it behind the real request pipeline: no handler here
// has a line of check-in code. The nine real routes are driven over HTTP in
// integration/job_checkins_test.go.

const (
	tenthPattern = "POST /api/v1/internal/tenth/run"
	tenthPath    = "/api/v1/internal/tenth/run"
	tenthJob     = "ticket-pos-tenth"
)

// serveTenthJob serves handler as the tenth job, behind the request pipeline,
// both reporting to fake.
func serveTenthJob(t *testing.T, fake *reportertest.Reporter, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	jobs := server.CheckInJobs(mux, fake, map[string]string{tenthPattern: tenthJob})
	jobs.HandleFunc(tenthPattern, handler)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(platform.RequestPipeline(logger, fake, mux))
	t.Cleanup(srv.Close)
	return srv
}

func postTenth(t *testing.T, srv *httptest.Server) *http.Response {
	t.Helper()
	resp, err := http.Post(srv.URL+tenthPath, "application/json", nil)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp
}

// assertCheckIns asserts the run checked in as started and then as want, and
// returns the ending check-in.
func assertCheckIns(t *testing.T, fake *reportertest.Reporter, job string, want platform.CheckInStatus) reportertest.CheckIn {
	t.Helper()
	checkIns := fake.CheckIns()
	if len(checkIns) != 2 {
		t.Fatalf("got %d check-ins, want started then ended: %+v", len(checkIns), checkIns)
	}
	if checkIns[0].Job != job || checkIns[0].Status != platform.CheckInInProgress {
		t.Errorf("first check-in = %+v, want %s in progress", checkIns[0], job)
	}
	if checkIns[1].Job != job || checkIns[1].Status != want {
		t.Errorf("last check-in = %+v, want %s %s", checkIns[1], job, want)
	}
	return checkIns[1]
}

func TestATenthRouteRegisteredThroughTheWrapperChecksInOKWithItsDuration(t *testing.T) {
	fake := reportertest.New()
	srv := serveTenthJob(t, fake, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(30 * time.Millisecond)
		_ = platform.WriteSuccess(w, platform.RequestID(r.Context()), http.StatusOK, map[string]int{"done": 1})
	})

	resp := postTenth(t, srv)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	ended := assertCheckIns(t, fake, tenthJob, platform.CheckInOK)
	if ended.Duration < 30*time.Millisecond || ended.Duration > 5*time.Second {
		t.Errorf("duration = %s, want the run's time (at least 30ms)", ended.Duration)
	}
	if flushes := fake.Flushes(); len(flushes) != 1 || flushes[0] != platform.ShutdownFlushTimeout {
		t.Errorf("flushes = %v, want one of %s before the answer", flushes, platform.ShutdownFlushTimeout)
	}
	if reports := fake.Errors(); len(reports) != 0 {
		t.Errorf("a successful run reported: %+v", reports)
	}
}

func TestAJobThatAnswersAServerErrorChecksInFailedAndItsReportCarriesTheJobTag(t *testing.T) {
	fake := reportertest.New()
	srv := serveTenthJob(t, fake, func(w http.ResponseWriter, r *http.Request) {
		_ = platform.WriteDomainError(w, platform.RequestID(r.Context()), errors.New("the queue table is gone"))
	})

	resp := postTenth(t, srv)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	assertCheckIns(t, fake, tenthJob, platform.CheckInFailed)
	reports := fake.Errors()
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1: %+v", len(reports), reports)
	}
	if got := reports[0].Tags["job"]; got != tenthJob {
		t.Errorf("job tag = %q, want the monitor slug %q", got, tenthJob)
	}
	if reports[0].RequestID != resp.Header.Get(platform.RequestIDHeader) {
		t.Errorf("report request id = %q, want the run's", reports[0].RequestID)
	}
}

func TestAnErrorLoggedInsideAJobCarriesTheJobTag(t *testing.T) {
	fake := reportertest.New()
	logger := platform.NewSlogLogger(slog.New(slog.NewTextHandler(io.Discard, nil)), fake)
	srv := serveTenthJob(t, fake, func(w http.ResponseWriter, r *http.Request) {
		logger.Error(r.Context(), "gave up on one item", "error", errors.New("provider refused"))
		_ = platform.WriteSuccess(w, platform.RequestID(r.Context()), http.StatusOK, nil)
	})

	postTenth(t, srv)
	assertCheckIns(t, fake, tenthJob, platform.CheckInOK)
	reports := fake.Errors()
	if len(reports) != 1 || reports[0].Tags["job"] != tenthJob {
		t.Fatalf("reports = %+v, want one carrying job=%s", reports, tenthJob)
	}
}

func panicsInsideTheJob() {
	panic("the sweep's clock broke")
}

func TestAJobThatPanicsChecksInFailedAndIsReportedOnceFromThePanickingFrame(t *testing.T) {
	fake := reportertest.New()
	srv := serveTenthJob(t, fake, func(http.ResponseWriter, *http.Request) {
		panicsInsideTheJob()
	})

	resp := postTenth(t, srv)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want the pipeline's 500", resp.StatusCode)
	}
	assertCheckIns(t, fake, tenthJob, platform.CheckInFailed)
	if flushes := fake.Flushes(); len(flushes) != 1 {
		t.Errorf("flushes = %v, want one before the answer", flushes)
	}
	reports := fake.Errors()
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want exactly 1: %+v", len(reports), reports)
	}
	report := reports[0]
	if got := report.Err.Error(); got != "panic: the sweep's clock broke" {
		t.Errorf("reported %q, want the panic", got)
	}
	if report.Tags["job"] != tenthJob {
		t.Errorf("job tag = %q, want %q", report.Tags["job"], tenthJob)
	}
	if len(report.Stack) == 0 || !strings.HasSuffix(report.Stack[0].Function, ".panicsInsideTheJob") {
		t.Errorf("stack starts at %v, want the frame that panicked", stackHead(report.Stack))
	}
}

func stackHead(stack []runtime.Frame) string {
	if len(stack) == 0 {
		return "nothing"
	}
	return fmt.Sprintf("%s (%s:%d)", stack[0].Function, stack[0].File, stack[0].Line)
}

// A 4xx is the platform refusing as designed, such as the Sale Invoice drain
// answering 404 while its flag is closed: the run did what it should.
func TestAJobThatRefusesWithAClientErrorChecksInOK(t *testing.T) {
	fake := reportertest.New()
	srv := serveTenthJob(t, fake, func(w http.ResponseWriter, r *http.Request) {
		_ = platform.WriteHandlerError(w, platform.RequestID(r.Context()), http.StatusNotFound, "TENTH_UNAVAILABLE", "closed", nil)
	})

	if resp := postTenth(t, srv); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	assertCheckIns(t, fake, tenthJob, platform.CheckInOK)
}

func TestAScheduledRouteWithNoJobNameCannotBeRegistered(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil || !strings.Contains(fmt.Sprint(recovered), "POST /api/v1/internal/eleventh/run") {
			t.Fatalf("recovered %v, want a panic naming the route", recovered)
		}
	}()
	jobs := server.CheckInJobs(http.NewServeMux(), reportertest.New(), map[string]string{tenthPattern: tenthJob})
	jobs.Handle("POST /api/v1/internal/eleventh/run", nothing)
}

// The names are the contract with the monitors Terraform declares: nine, one
// per Cloud Scheduler job, each that job's name without its environment.
func TestTheNineJobNamesAreTheSchedulerJobsWithoutTheirEnvironment(t *testing.T) {
	want := map[string]string{
		"POST /api/v1/internal/reversals/drain":             "ticket-pos-reversal-reconciler",
		"POST /api/v1/internal/sale-invoices/drain":         "ticket-pos-sale-invoice-drainer",
		"POST /api/v1/internal/follow-digests/enqueue":      "ticket-pos-follow-digest-enqueue",
		"POST /api/v1/internal/follow-digests/drain":        "ticket-pos-follow-digest-drain",
		"POST /api/v1/internal/checkout-answers/purge":      "ticket-pos-answer-purge",
		"POST /api/v1/internal/answer-reminders/sweep":      "ticket-pos-answer-reminder",
		"POST /api/v1/internal/assignment-reminders/sweep":  "ticket-pos-assignment-reminder",
		"POST /api/v1/internal/owed-assignment-mails/sweep": "ticket-pos-owed-assignment-mail",
		"POST /api/v1/internal/holder-addresses/purge":      "ticket-pos-holder-address-purge",
	}
	got := server.ScheduledJobs()
	if len(got) != len(want) {
		t.Fatalf("got %d jobs, want %d: %v", len(got), len(want), got)
	}
	for pattern, job := range want {
		if got[pattern] != job {
			t.Errorf("%s runs %q, want %q", pattern, got[pattern], job)
		}
	}
}
