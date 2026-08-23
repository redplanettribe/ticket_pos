package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

// The Reminder sweeps' pacing (#376).
//
// At the Assignment Reminder launch the first forced run lost three of
// eighteen mails to the provider's ten-requests-per-second limit, and a failed
// send leaves no ledger row, so the three were retried a day later. Both
// sweeps sent once per reader in a tight loop. These tests pin the cadence
// INSIDE one run — a gap between consecutive sends, and nothing else: who is
// due, the ledger-after-send and the failed/unrecorded accounting are proved
// in the integration tests and are deliberately untouched here.
//
// The gap is observed through the sleeper rather than a wall clock, so the
// test says exactly what was asked for and takes no real time to say it.

type recordedPause struct {
	after int // how many requests had reached the provider when the pause was asked for
	gap   time.Duration
}

// pacingHarness is the smallest Service that can run both sweeps: a sender that
// counts, a source that serves whatever is handed to it, and a sleeper that
// records instead of sleeping and moves the clock as a real sleep would.
type pacingHarness struct {
	*Service
	sender *pacingSender
	source *pacingSource
	pauses []recordedPause
	clock  time.Time
}

func newPacingHarness(t *testing.T, gap time.Duration) *pacingHarness {
	t.Helper()
	h := &pacingHarness{
		sender: &pacingSender{},
		source: &pacingSource{},
		clock:  time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC),
	}
	svc := New(nil, pacingCustomers{}, h.sender, nil, "https://tickets.example", sales.FeeRates{}, nil, silentLogger{})
	svc.WithClock(func() time.Time { return h.clock })
	svc.WithReminderPacing(gap, func(_ context.Context, d time.Duration) {
		h.pauses = append(h.pauses, recordedPause{after: h.sender.reached, gap: d})
		h.clock = h.clock.Add(d)
	})
	svc.WithAssignmentReminders(h.source)
	svc.WithAnswerReminders(h.source)
	h.Service = svc
	return h
}

func TestAssignmentReminderSweepPacesItsSends(t *testing.T) {
	h := newPacingHarness(t, 120*time.Millisecond)
	for i := 0; i < 5; i++ {
		h.source.assignment = append(h.source.assignment, assignmentCandidate(i))
	}

	result, err := h.SweepAssignmentReminders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Sent != 5 || result.Failed != 0 {
		t.Fatalf("expected 5 sent, got %+v", result)
	}
	// Four gaps between five sends: none before the first, none after the last.
	assertPauses(t, h.pauses, 120*time.Millisecond, 1, 2, 3, 4)
}

func TestAnswerReminderSweepPacesItsSends(t *testing.T) {
	h := newPacingHarness(t, 120*time.Millisecond)
	for i := 0; i < 4; i++ {
		h.source.answer = append(h.source.answer, answerCandidate(i))
	}

	result, err := h.SweepAnswerReminders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Sent != 4 {
		t.Fatalf("expected 4 sent, got %+v", result)
	}
	assertPauses(t, h.pauses, 120*time.Millisecond, 1, 2, 3)
}

// A send the provider refused still counted against its rate limit, so the
// gap follows it as it would a success; a candidate skipped before any request
// was made did not, so no gap follows a skip. The refused mail is counted
// failed exactly as before — pacing adds no retry.
func TestPacingFollowsRequestsNotOutcomes(t *testing.T) {
	h := newPacingHarness(t, 120*time.Millisecond)
	addressless := assignmentCandidate(1)
	addressless.BuyerEmail = ""
	h.source.assignment = []catalog.AssignmentReminderCandidate{
		assignmentCandidate(0),
		addressless,
		assignmentCandidate(2),
		assignmentCandidate(3),
	}
	h.sender.refuse = map[string]bool{"buyer-2@example.com": true}

	result, err := h.SweepAssignmentReminders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Sent != 2 || result.Failed != 1 || result.Skipped != 1 {
		t.Fatalf("expected 2 sent, 1 failed, 1 skipped, got %+v", result)
	}
	// One request; the skip asks for no pause; the refused request is paced
	// like any other; then the last.
	assertPauses(t, h.pauses, 120*time.Millisecond, 1, 2)
}

// The gap is spent inside the budget, never on top of it: when the clock has
// run out the sweep stops rather than sending, and what it did not reach is
// reported as backlog.
func TestPacingStopsOnTheBudgetAndReportsBacklog(t *testing.T) {
	// A gap bigger than the budget lands the second send past the deadline.
	h := newPacingHarness(t, assignmentReminderBudget+time.Second)
	for i := 0; i < 3; i++ {
		h.source.assignment = append(h.source.assignment, assignmentCandidate(i))
	}

	result, err := h.SweepAssignmentReminders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Sent != 1 {
		t.Fatalf("expected the sweep to stop after one send on its budget, got %+v", result)
	}
	if result.Backlog != 2 {
		t.Fatalf("expected the two unreached Sales reported as backlog, got %+v", result)
	}
	if len(h.pauses) != 1 {
		t.Fatalf("expected exactly one pause before the budget was found spent, got %d", len(h.pauses))
	}
}

// A zero gap never asks the sleeper for anything, so a test that wants the
// old cadence can have it without a fake sleeper.
func TestZeroGapNeverPauses(t *testing.T) {
	h := newPacingHarness(t, 0)
	for i := 0; i < 3; i++ {
		h.source.assignment = append(h.source.assignment, assignmentCandidate(i))
	}
	if _, err := h.SweepAssignmentReminders(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.pauses) != 0 {
		t.Fatalf("expected no pauses with a zero gap, got %v", h.pauses)
	}
}

func assertPauses(t *testing.T, got []recordedPause, gap time.Duration, afterRequests ...int) {
	t.Helper()
	if len(got) != len(afterRequests) {
		t.Fatalf("expected %d pauses (after requests %v), got %v", len(afterRequests), afterRequests, got)
	}
	for i, p := range got {
		if p.after != afterRequests[i] || p.gap != gap {
			t.Fatalf("pause %d: expected %v after %d requests, got %v after %d", i, gap, afterRequests[i], p.gap, p.after)
		}
	}
}

func assignmentCandidate(i int) catalog.AssignmentReminderCandidate {
	return catalog.AssignmentReminderCandidate{
		TicketSaleID:      fmt.Sprintf("sale-%d", i),
		BuyerEmail:        fmt.Sprintf("buyer-%d@example.com", i),
		EventName:         "Event",
		EventStartsAt:     time.Date(2026, 9, 1, 19, 0, 0, 0, time.UTC),
		EventEndsAt:       time.Date(2026, 9, 1, 23, 0, 0, 0, time.UTC),
		EventTimezone:     "Europe/Stockholm",
		UnassignedTickets: 1,
		TicketCount:       2,
	}
}

func answerCandidate(i int) catalog.DueAnswerReminder {
	return catalog.DueAnswerReminder{
		TicketSaleID: fmt.Sprintf("sale-%d", i),
		HolderEmail:  fmt.Sprintf("holder-%d@example.com", i),
		TicketIDs:    []string{fmt.Sprintf("ticket-%d", i)},
		Tickets:      []catalog.HolderReminderTicket{{EventName: "Event", TicketTypeName: "Standard"}},
	}
}

// pacingSender counts every request that reached "the provider" and refuses
// the addresses it is told to, standing in for a 429.
type pacingSender struct {
	platform.NoopEmailSender
	reached int
	refuse  map[string]bool
}

func (s *pacingSender) SendAssignmentReminder(_ context.Context, r platform.AssignmentReminder) error {
	s.reached++
	if s.refuse[r.To] {
		return errors.New("429 rate_limit_exceeded")
	}
	return nil
}

func (s *pacingSender) SendHolderAnswerReminder(_ context.Context, r platform.HolderAnswerReminder) error {
	s.reached++
	if s.refuse[r.To] {
		return errors.New("429 rate_limit_exceeded")
	}
	return nil
}

// pacingSource serves the candidates it is handed and counts what it has not
// yet recorded as the backlog.
type pacingSource struct {
	assignment []catalog.AssignmentReminderCandidate
	answer     []catalog.DueAnswerReminder
	recorded   map[string]bool
}

func (s *pacingSource) AssignmentRemindersDue(context.Context, int) ([]catalog.AssignmentReminderCandidate, error) {
	return s.assignment, nil
}

func (s *pacingSource) CountAssignmentRemindersDue(context.Context) (int, error) {
	n := 0
	for _, c := range s.assignment {
		if !s.recorded[c.TicketSaleID] {
			n++
		}
	}
	return n, nil
}

func (s *pacingSource) RecordAssignmentReminderSent(_ context.Context, id string) error {
	if s.recorded == nil {
		s.recorded = map[string]bool{}
	}
	s.recorded[id] = true
	return nil
}

func (s *pacingSource) AnswerRemindersDue(context.Context, int) ([]catalog.DueAnswerReminder, error) {
	return s.answer, nil
}

func (s *pacingSource) CountAnswerRemindersDue(context.Context) (int, error) {
	return len(s.answer), nil
}

func (s *pacingSource) RecordAnswerRemindersSent(context.Context, []string) error {
	return nil
}

type pacingCustomers struct{}

func (pacingCustomers) UpsertForSale(context.Context, *sql.Tx, platform.SaleCustomer, time.Time) (string, error) {
	return "", errors.New("not in this test")
}

func (pacingCustomers) ResolveByEmail(context.Context, string) (string, string, error) {
	return "", "", errors.New("not in this test")
}

func (pacingCustomers) MailLocale(context.Context, string) (string, error) { return "", nil }

func (pacingCustomers) ConfirmationLinkURL(id string, _ time.Time) (string, error) {
	return "https://tickets.example/sales/" + id, nil
}

func (pacingCustomers) ConsentConfirmationLinkURL(context.Context, string) (string, error) {
	return "", errors.New("not in this test")
}

type silentLogger struct{}

func (silentLogger) Info(string, ...any)  {}
func (silentLogger) Warn(string, ...any)  {}
func (silentLogger) Error(string, ...any) {}
