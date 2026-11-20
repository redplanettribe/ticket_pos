package server

import (
	"fmt"
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// scheduledJobs names the job each scheduler-invoked route runs. The name is
// the job's monitor slug and the job tag on its reports (ADR 0079).
//
// THIS LIST IS A CONTRACT WITH TERRAFORM: the cron monitor declared beside each
// Cloud Scheduler job in terraform/modules/ticket-pos must carry the same slug,
// or its check-ins are dropped and the monitor alerts that the job never ran.
// Each name is the Cloud Scheduler job's name without its environment prefix,
// so "production-ticket-pos-answer-purge" checks in as "ticket-pos-answer-purge".
// The "ticket-pos-" prefix keeps the slugs apart from the other product's in
// the same Sentry organization.
var scheduledJobs = map[string]string{
	"POST /api/v1/internal/reversals/drain":             "ticket-pos-reversal-reconciler",
	"POST /api/v1/internal/sale-invoices/drain":         "ticket-pos-sale-invoice-drainer",
	"POST /api/v1/internal/follow-digests/enqueue":      "ticket-pos-follow-digest-enqueue",
	"POST /api/v1/internal/follow-digests/drain":        "ticket-pos-follow-digest-drain",
	"POST /api/v1/internal/checkout-answers/purge":      "ticket-pos-answer-purge",
	"POST /api/v1/internal/answer-reminders/sweep":      "ticket-pos-answer-reminder",
	"POST /api/v1/internal/assignment-reminders/sweep":  "ticket-pos-assignment-reminder",
	"POST /api/v1/internal/owed-assignment-mails/sweep": "ticket-pos-owed-assignment-mail",
	"POST /api/v1/internal/holder-addresses/purge":      "ticket-pos-holder-address-purge",
	"POST /api/v1/internal/platform-trends/close":       "ticket-pos-daily-close",
}

// CheckInJobs wraps mux so that every route registered on it is served through
// platform.JobRoute, as the job jobs names for its pattern: the route checks in
// with no code in its handler. registerInternalRoutes passes scheduledJobs; a
// test passes its own, to register a route the real list does not have. A route jobs has no name for cannot be registered: it
// panics here, at construction, so a scheduled route cannot ship without its
// check-ins.
func CheckInJobs(mux Router, reporter platform.Reporter, jobs map[string]string) Router {
	return jobsRouter{next: mux, reporter: reporter, jobs: jobs}
}

type jobsRouter struct {
	next     Router
	reporter platform.Reporter
	jobs     map[string]string
}

func (j jobsRouter) Handle(pattern string, handler http.Handler) {
	job, ok := j.jobs[pattern]
	if !ok {
		panic(fmt.Sprintf("server: scheduled route %q has no job name; add it to scheduledJobs", pattern))
	}
	j.next.Handle(pattern, platform.JobRoute(j.reporter, job, handler))
}

func (j jobsRouter) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	j.Handle(pattern, http.HandlerFunc(handler))
}
