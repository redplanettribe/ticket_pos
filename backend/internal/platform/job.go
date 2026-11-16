package platform

import (
	"bytes"
	"context"
	"net/http"
	"time"
)

// reportTagJob is the tag every report made inside a job route carries: the
// job's name, which is also its monitor's slug (ADR 0079).
const reportTagJob = "job"

// JobRoute is next served as the scheduled job named job: it checks in as
// started when the route is entered, and as ok or failed, with the time the
// run took, when it ends (ADR 0079). Everything reported from inside the run
// carries the job tag, equal to the name.
//
// A run fails when it answers a 5xx or panics. A 4xx is the platform refusing
// as designed, as the Sale Invoice drain answers while its flag is closed, and
// the run checks in ok.
//
// Cloud Run may freeze an instance between requests, so the run's check-ins
// and reports are flushed, within ShutdownFlushTimeout, before the route
// answers: the response is held until then. A reporter that blocks delays the
// answer by no more than the bound and never changes it.
//
// The handler knows nothing of this, nor of whether a monitor expects the
// job: every job checks in, and a check-in nobody expects is dropped by the
// vendor. With no DSN the reporter is NoopReporter and nothing happens.
func JobRoute(reporter Reporter, job string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tagJob(ctx, job)
		started := time.Now()
		checkIn := reporter.CheckIn(ctx, job)
		held := &heldResponse{ResponseWriter: w}

		answered := false
		defer func() {
			if answered {
				return
			}
			// The run panicked (or its goroutine exited). The panic is
			// reported here, from where its stack is still the frame that
			// panicked, so it is flushed with the check-in; the request
			// pipeline's recovery then logs it and answers the 500, and does
			// not report it again (one failed request, one event).
			recovered := recover()
			if recovered != nil && recovered != http.ErrAbortHandler {
				reportRequestFailure(ctx, reporter, ErrorReport{
					Err:       recoveredPanic{value: recovered},
					Stack:     panicStack(),
					RequestID: RequestID(ctx),
				})
			}
			checkIn.Finish(CheckInFailed, time.Since(started))
			reporter.Flush(ShutdownFlushTimeout)
			if recovered != nil {
				panic(recovered)
			}
		}()

		next.ServeHTTP(held, r)
		answered = true

		status := CheckInOK
		if held.status >= http.StatusInternalServerError {
			status = CheckInFailed
		}
		checkIn.Finish(status, time.Since(started))
		reporter.Flush(ShutdownFlushTimeout)
		held.send()
	})
}

// tagJob records that the request on ctx is the run of job, so its reports
// carry the job tag. Outside a request it does nothing.
func tagJob(ctx context.Context, job string) {
	reports := requestReportsOn(ctx)
	if reports == nil {
		return
	}
	reports.mu.Lock()
	defer reports.mu.Unlock()
	reports.tags[reportTagJob] = job
}

// heldResponse holds a job route's status and body until the run has been
// flushed. Headers are set on the writer underneath, which sends nothing
// before its WriteHeader.
type heldResponse struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (h *heldResponse) WriteHeader(status int) {
	// 1xx responses are interim; the status held is the final one.
	if h.status == 0 && status >= http.StatusOK {
		h.status = status
	}
}

func (h *heldResponse) Write(b []byte) (int, error) {
	if h.status == 0 {
		h.status = http.StatusOK
	}
	return h.body.Write(b)
}

// Unwrap lets the error mapping reach the request pipeline's recorder
// underneath, so a 5xx the run answers is reported (reportServerError).
func (h *heldResponse) Unwrap() http.ResponseWriter {
	return h.ResponseWriter
}

// send writes what was held: a handler that wrote nothing answers 200.
func (h *heldResponse) send() {
	if h.status == 0 {
		h.status = http.StatusOK
	}
	h.ResponseWriter.WriteHeader(h.status)
	_, _ = h.ResponseWriter.Write(h.body.Bytes())
}
