package service

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

// The Owed Assignment Mail sweep's loop and its deployment contract (#671,
// parent #665, ADR 0076).
//
// What the sweep SENDS - to whom, what it drops, the ledger, the retry - is
// proved over HTTP against a real database in
// integration/owed_assignment_mail_test.go, and the judgement on one owed mail
// in internal/catalog/owed_assignment_mail_test.go. What is here is the cadence
// of one run, observed through the sleeper, and the half of the job that lives
// in Terraform.

// owedStep is one thing the run did, in order: a claim with its outcome, or a
// pause.
type owedStep string

// scriptedOwedMails hands the sweep the outcomes it is scripted with, one per
// claim, then NoneDue; it records every claim in the shared step log.
type scriptedOwedMails struct {
	outcomes []catalog.OwedAssignmentMailOutcome
	claims   int
	err      error
	backlog  int
	steps    *[]owedStep
}

func (s *scriptedOwedMails) SendNextOwedAssignmentMail(context.Context) (catalog.OwedAssignmentMailOutcome, error) {
	*s.steps = append(*s.steps, "claim")
	if s.err != nil {
		return catalog.OwedAssignmentMailNoneDue, s.err
	}
	if s.claims >= len(s.outcomes) {
		return catalog.OwedAssignmentMailNoneDue, nil
	}
	outcome := s.outcomes[s.claims]
	s.claims++
	return outcome, nil
}

func (s *scriptedOwedMails) CountOwedAssignmentMailsDue(context.Context) (int, error) {
	return s.backlog, nil
}

type owedSweepHarness struct {
	*Service
	mails *scriptedOwedMails
	steps []owedStep
	clock time.Time
}

func newOwedSweepHarness(t *testing.T, gap time.Duration, outcomes ...catalog.OwedAssignmentMailOutcome) *owedSweepHarness {
	t.Helper()
	h := &owedSweepHarness{clock: time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)}
	h.mails = &scriptedOwedMails{outcomes: outcomes, steps: &h.steps}
	svc := New(nil, pacingCustomers{}, &pacingSender{}, nil, "https://tickets.example", sales.FeeRates{}, nil, silentLogger{})
	svc.WithClock(func() time.Time { return h.clock })
	svc.WithReminderPacing(gap, func(_ context.Context, d time.Duration) {
		h.steps = append(h.steps, owedStep("pause "+d.String()))
		h.clock = h.clock.Add(d)
	})
	svc.WithOwedAssignmentMails(h.mails)
	h.Service = svc
	return h
}

func (h *owedSweepHarness) assertSteps(t *testing.T, want ...owedStep) {
	t.Helper()
	got := make([]string, len(h.steps))
	for i, step := range h.steps {
		got[i] = string(step)
	}
	wantText := make([]string, len(want))
	for i, step := range want {
		wantText[i] = string(step)
	}
	if strings.Join(got, ", ") != strings.Join(wantText, ", ") {
		t.Fatalf("the run went\n  %s\nwant\n  %s", strings.Join(got, ", "), strings.Join(wantText, ", "))
	}
}

const (
	owedSent       = catalog.OwedAssignmentMailSent
	owedUnrecorded = catalog.OwedAssignmentMailSentUnrecorded
	owedFailed     = catalog.OwedAssignmentMailFailed
	owedDropped    = catalog.OwedAssignmentMailDropped
)

// A NINE-TICKET CHECKOUT IS SENT AS NINE PACED REQUESTS, not a burst: the gap
// the Reminder sweeps keep separates every request from the claim after it.
// The claim that finds nothing due is preceded by one gap too, since the run
// cannot know it is the last until it asks.
func TestTheOwedMailSweepPacesItsSends(t *testing.T) {
	h := newOwedSweepHarness(t, 120*time.Millisecond, owedSent, owedSent, owedSent)

	result, err := h.SweepOwedAssignmentMails(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Sent != 3 {
		t.Fatalf("result = %+v, want 3 sent", result)
	}
	h.assertSteps(t,
		"claim", "pause 120ms", "claim", "pause 120ms", "claim", "pause 120ms", "claim")
}

// What the provider rations is requests: a refused send is paced like a sent
// one, and a dropped mail - which asked the provider nothing - costs no pause.
func TestTheOwedMailSweepPacesRequestsNotOutcomes(t *testing.T) {
	h := newOwedSweepHarness(t, 120*time.Millisecond, owedDropped, owedFailed, owedDropped, owedUnrecorded, owedDropped)

	result, err := h.SweepOwedAssignmentMails(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Sent != 1 || result.Unrecorded != 1 || result.Failed != 1 || result.Dropped != 3 {
		t.Fatalf("result = %+v, want 1 sent (unrecorded), 1 failed, 3 dropped", result)
	}
	h.assertSteps(t,
		"claim",                // dropped: no request
		"claim",                // failed: one request
		"pause 120ms", "claim", // dropped
		"claim",                // unrecorded: a second request
		"pause 120ms", "claim", // dropped
		"claim") // none due
}

// A run touches at most a batch of owed mails, dropped or sent, and leaves the
// rest to the next tick.
func TestTheOwedMailSweepStopsAtItsBatch(t *testing.T) {
	outcomes := make([]catalog.OwedAssignmentMailOutcome, owedAssignmentMailBatch+10)
	for i := range outcomes {
		outcomes[i] = owedSent
	}
	h := newOwedSweepHarness(t, 0, outcomes...)
	h.mails.backlog = 10

	result, err := h.SweepOwedAssignmentMails(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Sent != owedAssignmentMailBatch || result.Backlog != 10 {
		t.Fatalf("result = %+v, want a batch of %d sent and the backlog reported", result, owedAssignmentMailBatch)
	}
}

// The budget stops a run before a claim, never after one: what is claimed is
// sent, and what is not reached stays owed.
func TestTheOwedMailSweepStopsOnItsBudget(t *testing.T) {
	h := newOwedSweepHarness(t, owedAssignmentMailBudget+time.Second, owedSent, owedSent, owedSent)

	result, err := h.SweepOwedAssignmentMails(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Sent != 1 {
		t.Fatalf("result = %+v, want the run stopped on its budget after one send", result)
	}
	if h.mails.claims != 1 {
		t.Fatalf("%d claims, want 1 - nothing may be claimed past the budget", h.mails.claims)
	}
}

// A claim that fails is a run that cannot see what is owed, and says so.
func TestAFailedClaimFailsTheRun(t *testing.T) {
	h := newOwedSweepHarness(t, 0)
	h.mails.err = errors.New("connection refused")
	if _, err := h.SweepOwedAssignmentMails(context.Background()); err == nil {
		t.Fatal("a run whose claim failed reported success")
	}
}

// An unwired seam sends nothing, which is how a mail job must fail.
func TestAnUnwiredOwedMailSweepSendsNothing(t *testing.T) {
	svc := New(nil, pacingCustomers{}, &pacingSender{}, nil, "https://tickets.example", sales.FeeRates{}, nil, silentLogger{})
	result, err := svc.SweepOwedAssignmentMails(context.Background())
	if err != nil || *result != (OwedAssignmentMailSweepResult{}) {
		t.Fatalf("result = %+v, err = %v, want zeros", result, err)
	}
}

// --- The Terraform half ---------------------------------------------------------

// The sweep is driven by its own job at the pinned, un-aimable route: nothing
// in the URL may name a Ticket, a Sale or a moment.
func TestTheOwedMailSweepEndpointCannotBeAimed(t *testing.T) {
	source := terraformFile(t, filepath.Join(terraformModuleDir(), "owed_assignment_mail.tf"))
	uri := regexp.MustCompile(`(?m)^\s*uri\s*=\s*"([^"]*)"`).FindStringSubmatch(source)
	if uri == nil {
		t.Fatal("the owed mail sweep job has no http_target uri")
	}
	const want = "${google_cloud_run_v2_service.api.uri}/api/v1/internal/owed-assignment-mails/sweep"
	if uri[1] != want {
		t.Fatalf("sweep uri = %q, want %q", uri[1], want)
	}
	if !regexp.MustCompile(`(?m)^\s*retry_count\s*=\s*0\s*$`).MatchString(source) {
		t.Fatal("the sweep's retry_count is not 0: the next tick is a minute away, and a retried run whose ledger writes were failing sends the same mails again")
	}
}

// Pausing is an apply, never a console click the next apply undoes, and the
// job exists while paused.
func TestTheOwedMailSweepIsPausableWithoutADeploy(t *testing.T) {
	source := terraformFile(t, filepath.Join(terraformModuleDir(), "owed_assignment_mail.tf"))
	if !regexp.MustCompile(`(?m)^\s*paused\s*=\s*!var\.owed_assignment_mail_enabled\s*$`).MatchString(source) {
		t.Fatal("the sweep's `paused` is not wired to !var.owed_assignment_mail_enabled")
	}
	if regexp.MustCompile(`(?m)^\s*count\s*=`).MatchString(source) {
		t.Fatal("the sweep's resources are conditional on `count`: disabling must pause the job, never delete it")
	}
}

// PRODUCTION RUNS IT. Unlike the Reminders, nothing else ever tells a Holder
// named at a Named Tickets checkout that they hold a Ticket: paused, every
// such mail waits forever. So production's tfvars must switch it on.
func TestTheOwedMailSweepRunsInProduction(t *testing.T) {
	tfvars := terraformFile(t, filepath.Join(terraformModuleDir(), "..", "..", "envs", "prod", "terraform.tfvars"))
	if !regexp.MustCompile(`(?m)^\s*owed_assignment_mail_enabled\s*=\s*true\s*$`).MatchString(tfvars) {
		t.Fatal("production does not enable the owed mail sweep: every Holder named at a Named Tickets checkout would go untold")
	}
}

// The deadline chain, and the one term this job adds: a budget under the
// per-minute cadence, so two scheduled runs never overlap and never pace their
// requests independently into twice the provider's rate.
//
//	owedAssignmentMailBudget  <  attempt_deadline  <  api_request_timeout_seconds
//	owedAssignmentMailBudget  <  one minute
func TestTheOwedMailSweepDeadlineChainHolds(t *testing.T) {
	moduleVariables := filepath.Join(terraformModuleDir(), "variables.tf")
	deadline := terraformSecondsDefault(t, moduleVariables, "owed_assignment_mail_attempt_deadline_seconds")
	requestTimeout := terraformSecondsDefault(t, moduleVariables, "api_request_timeout_seconds")

	if owedAssignmentMailBudget >= deadline {
		t.Fatalf("the run budget %v is not inside Cloud Scheduler's attempt deadline %v", owedAssignmentMailBudget, deadline)
	}
	if deadline >= requestTimeout {
		t.Fatalf("Cloud Scheduler's attempt deadline %v is not inside Cloud Run's request timeout %v", deadline, requestTimeout)
	}
	block := terraformVariableBlock(t, moduleVariables, "owed_assignment_mail_schedule")
	if !regexp.MustCompile(`(?m)^\s*default\s*=\s*"\* \* \* \* \*"\s*$`).MatchString(block) {
		t.Fatalf("owed_assignment_mail_schedule does not default to every minute:\n%s", block)
	}
	if owedAssignmentMailBudget >= time.Minute {
		t.Fatalf("the run budget %v is not under the one-minute cadence: two scheduled runs could overlap", owedAssignmentMailBudget)
	}
	if owedAssignmentMailBatch <= 0 {
		t.Fatalf("owedAssignmentMailBatch = %d: a run that claims nothing sends nothing", owedAssignmentMailBatch)
	}
}
