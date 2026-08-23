package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	salesservice "github.com/peter/ticket_pos/backend/internal/sales/service"
)

// The Assignment Reminder's RATIONING and BATCHING under repeated runs (#363,
// parent #361, ADR 0051), on assignment_reminder_test.go's fixture and terms.
//
// THAT FILE PROVES THE FIRST MAIL GOES; THIS ONE PROVES IT NEVER NAGS. A buyer
// is written to no sooner than 24 hours after the Sale, no more than once in
// 7 days, never a third time, and not at all once every Ticket has an address.
// The sweep is safe to run by hand — twice in a minute mails nobody the second
// time — drains a backlog oldest Sale first under its batch limit, leaves a
// failed send unrecorded so the buyer is retried, counts a sent-but-unrecorded
// mail honestly, and names nobody in its response or its log.
//
// The seams are the ones the Answer Reminder tests use: the captured mail
// sender's FailWith, the sales service's WithLogger, and — for the batch limit
// and the ledger — the WithAssignmentReminders setter, handed a decorator over
// the real source. Nothing in production changes shape for these tests.

// assignmentReminderSourceDecorator wraps the catalog service the sweep reads
// from, so a test can cap the batch below the production constant without
// selling fifty-one Sales, or make the ledger refuse a row after the mail left.
type assignmentReminderSourceDecorator struct {
	inner     salesservice.AssignmentReminderSource
	batch     int
	ledgerErr error
}

func (d *assignmentReminderSourceDecorator) AssignmentRemindersDue(ctx context.Context, limit int) ([]catalog.AssignmentReminderCandidate, error) {
	if d.batch > 0 && d.batch < limit {
		limit = d.batch
	}
	return d.inner.AssignmentRemindersDue(ctx, limit)
}

func (d *assignmentReminderSourceDecorator) CountAssignmentRemindersDue(ctx context.Context) (int, error) {
	return d.inner.CountAssignmentRemindersDue(ctx)
}

func (d *assignmentReminderSourceDecorator) RecordAssignmentReminderSent(ctx context.Context, ticketSaleID string) error {
	if d.ledgerErr != nil {
		return d.ledgerErr
	}
	return d.inner.RecordAssignmentReminderSent(ctx, ticketSaleID)
}

// withAssignmentReminderSource installs the decorator for one test and restores
// the production wiring afterwards.
func withAssignmentReminderSource(t *testing.T, batch int, ledgerErr error) {
	t.Helper()
	sharedApp.SalesService.WithAssignmentReminders(&assignmentReminderSourceDecorator{
		inner: sharedApp.CatalogService, batch: batch, ledgerErr: ledgerErr,
	})
	t.Cleanup(func() { sharedApp.SalesService.WithAssignmentReminders(sharedApp.CatalogService) })
}

// assignmentReminderAddresses lists who was written to, in order.
func assignmentReminderAddresses() []string {
	mails := sharedEmail.AssignmentRemindersSent()
	out := make([]string, 0, len(mails))
	for _, mail := range mails {
		out = append(out, mail.To)
	}
	return out
}

// NOTHING BEFORE 24 HOURS. The Sale is made at the fixed clock; at 23 hours
// and 59 minutes nothing is due, at exactly 24 hours the mail goes. The buyer
// may still be assigning from the checkout they just left, and a Reminder an
// hour after paying is a nag about something they have not yet had time to do.
func TestAssignmentReminderIsNotSentBeforeTwentyFourHours(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentReminderFixture(t, env)

	moveClockTo(t, f.soldAt.Add(catalog.AssignmentReminderMinSaleAge-time.Minute))
	assertSilentSweep(t, env, f.saleID, "the Sale is a minute short of a day old")

	moveClockTo(t, f.soldAt.Add(catalog.AssignmentReminderMinSaleAge))
	if result := sweepAssignmentReminders(t, env); result.Sent != 1 {
		t.Fatalf("sweep at exactly 24h = %+v, want the first mail", result)
	}
	assignmentReminderTo(t, "ana@example.com")
	if got := assignmentRemindersFor(t, env, f.saleID); got != 1 {
		t.Fatalf("ledger rows = %d, want 1", got)
	}
}

// NOT TWICE INSIDE SEVEN DAYS. After the first mail, nothing at six days and
// twenty-three hours; the second goes at exactly seven days. The Ticket is
// still unassigned throughout, so the debt is real every day — it is the
// permission to say so that is rationed.
func TestAssignmentReminderIsNotSentTwiceInsideTheWeek(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentReminderFixture(t, env)

	if first := sweepAssignmentReminders(t, env); first.Sent != 1 {
		t.Fatalf("first sweep = %+v, want one mail", first)
	}

	moveClockTo(t, f.sweepAt.Add(catalog.AssignmentReminderInterval-time.Hour))
	if early := sweepAssignmentReminders(t, env); early.Sent != 0 || early.Due != 0 || early.Backlog != 0 {
		t.Fatalf("sweep an hour short of the week = %+v, want silence", early)
	}

	moveClockTo(t, f.sweepAt.Add(catalog.AssignmentReminderInterval))
	if second := sweepAssignmentReminders(t, env); second.Sent != 1 {
		t.Fatalf("sweep at exactly seven days = %+v, want the second mail", second)
	}
	if got := len(sharedEmail.AssignmentRemindersSent()); got != 2 {
		t.Fatalf("mails across the week = %d, want 2", got)
	}
	if got := assignmentRemindersFor(t, env, f.saleID); got != 2 {
		t.Fatalf("ledger rows = %d, want 2 — one per mail", got)
	}
}

// NEVER A THIRD. Sweeping weekly for ten weeks against an Event four months
// out sends exactly two; no passage of time buys another. The mail is
// transactional and carries no unsubscribe, so the cap is the only thing
// between a buyer and an unbounded chase.
func TestAssignmentReminderStopsForeverAfterTwo(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentReminderFixture(t, env)

	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		f.eventID, f.soldAt.Add(120*24*time.Hour)); err != nil {
		t.Fatalf("push the Event's start out: %v", err)
	}

	for week := 0; week < 10; week++ {
		moveClockTo(t, f.sweepAt.Add(time.Duration(week)*catalog.AssignmentReminderInterval))
		result := sweepAssignmentReminders(t, env)
		if week >= catalog.MaxAssignmentReminders && (result.Sent != 0 || result.Due != 0 || result.Backlog != 0) {
			t.Fatalf("week %d sweep = %+v, want silence after %d mails", week, result, catalog.MaxAssignmentReminders)
		}
	}

	if got := len(sharedEmail.AssignmentRemindersSent()); got != catalog.MaxAssignmentReminders {
		t.Fatalf("mails over ten weeks = %d, want %d", got, catalog.MaxAssignmentReminders)
	}
	if got := assignmentRemindersFor(t, env, f.saleID); got != catalog.MaxAssignmentReminders {
		t.Fatalf("ledger rows = %d, want %d", got, catalog.MaxAssignmentReminders)
	}
}

// STOPS ONCE EVERY TICKET IS ASSIGNED, even with a second mail still in the
// buyer's allowance. Ana is reminded once, acts on it, and the seven-day tick
// that would have carried the second mail finds nothing due.
func TestAssignmentReminderStopsOnceTheBuyerAssignsAfterTheFirstMail(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentReminderFixture(t, env)

	if first := sweepAssignmentReminders(t, env); first.Sent != 1 {
		t.Fatalf("first sweep = %+v, want one mail", first)
	}
	assignTicketOK(t, env, f.ana, f.saleID, f.otherID, "carla@example.com")
	sharedEmail.Reset()

	moveClockTo(t, f.sweepAt.Add(catalog.AssignmentReminderInterval))
	result := sweepAssignmentReminders(t, env)
	if result.Sent != 0 || result.Due != 0 || result.Backlog != 0 {
		t.Fatalf("sweep a week after assigning = %+v, want silence", result)
	}
	if got := sharedEmail.AssignmentRemindersSent(); len(got) != 0 {
		t.Fatalf("%d mail(s) went out after every Ticket had an address, want none", len(got))
	}
	if got := assignmentRemindersFor(t, env, f.saleID); got != 1 {
		t.Fatalf("ledger rows = %d, want the one from before she acted", got)
	}
}

// SAFE TO RUN BY HAND TWICE. The launch procedure force-runs the job from
// Cloud Scheduler while somebody watches; a retry or an overlapping tick in
// the same minute must mail nobody, and the second response must say so.
func TestAssignmentReminderSweepIsSafeToRunByHandTwice(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentReminderFixture(t, env)

	first := sweepAssignmentReminders(t, env)
	second := sweepAssignmentReminders(t, env)

	if first.Sent != 1 || second.Sent != 0 || second.Due != 0 {
		t.Fatalf("first = %+v, second = %+v, want one mail and then silence", first, second)
	}
	if second.Backlog != 0 {
		t.Fatalf("second sweep reports a backlog of %d, want 0 — the backlog is what an operator reads two runs apart", second.Backlog)
	}
	if got := len(sharedEmail.AssignmentRemindersSent()); got != 1 {
		t.Fatalf("mails across both runs = %d, want 1", got)
	}
	if got := assignmentRemindersFor(t, env, f.saleID); got != 1 {
		t.Fatalf("ledger rows = %d, want 1", got)
	}
}

// THE BATCH LIMIT DRAINS OLDEST SALE FIRST AND REPORTS THE BACKLOG. Three
// Sales an hour apart, a batch of two: Ana and Bea, the older two, are mailed
// and Cris stands in the backlog; the next tick takes Cris and reports none.
// A buyer who has waited longest is served first, and nobody is starved by
// the Sales ahead of them.
func TestAssignmentReminderBatchDrainsOldestSaleFirstAndReportsTheBacklog(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentReminderFixture(t, env)
	moveClockTo(t, f.soldAt.Add(time.Hour))
	beaSale := sellAssignmentReminderSale(t, env, f.ticketTypeID, "assign-fest-reminders", "bea@example.com", "Bea", 2)
	moveClockTo(t, f.soldAt.Add(2*time.Hour))
	crisSale := sellAssignmentReminderSale(t, env, f.ticketTypeID, "assign-fest-reminders", "cris@example.com", "Cris", 2)
	moveClockTo(t, f.sweepAt)
	sharedEmail.Reset()
	withAssignmentReminderSource(t, 2, nil)

	first := sweepAssignmentReminders(t, env)
	if first.Due != 2 || first.Sent != 2 || first.Backlog != 1 {
		t.Fatalf("first sweep = %+v, want due=2 sent=2 backlog=1", first)
	}
	if got, want := assignmentReminderAddresses(), []string{"ana@example.com", "bea@example.com"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("first batch went to %v, want the two oldest Sales %v in order", got, want)
	}
	if got := assignmentRemindersFor(t, env, crisSale); got != 0 {
		t.Fatalf("the youngest Sale has %d ledger rows after the first batch, want 0", got)
	}

	sharedEmail.Reset()
	second := sweepAssignmentReminders(t, env)
	if second.Due != 1 || second.Sent != 1 || second.Backlog != 0 {
		t.Fatalf("second sweep = %+v, want due=1 sent=1 backlog=0", second)
	}
	if got := assignmentReminderAddresses(); strings.Join(got, ",") != "cris@example.com" {
		t.Fatalf("second batch went to %v, want only the Sale left standing", got)
	}
	for _, sale := range []string{f.saleID, beaSale, crisSale} {
		if got := assignmentRemindersFor(t, env, sale); got != 1 {
			t.Fatalf("Sale %s has %d ledger rows, want 1", sale, got)
		}
	}
}

// A FAILED SEND LEAVES NO LEDGER ROW, so the buyer is retried on the next
// tick rather than silently dropped. The ledger records mails that LEFT; a
// provider outage spends none of the buyer's allowance.
func TestAssignmentReminderFailedSendLeavesNoLedgerRowAndIsRetried(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentReminderFixture(t, env)

	sharedEmail.FailWith(errors.New("provider down"))
	failed := sweepAssignmentReminders(t, env)
	if failed.Due != 1 || failed.Failed != 1 || failed.Sent != 0 || failed.Unrecorded != 0 {
		t.Fatalf("sweep during the outage = %+v, want due=1 failed=1 sent=0", failed)
	}
	if failed.Backlog != 1 {
		t.Fatalf("backlog after the outage = %d, want 1 — the buyer is still owed the mail", failed.Backlog)
	}
	assertNoAssignmentReminders(t, env, f.saleID, "the mail never left")

	sharedEmail.FailWith(nil)
	retried := sweepAssignmentReminders(t, env)
	if retried.Sent != 1 || retried.Failed != 0 || retried.Backlog != 0 {
		t.Fatalf("sweep on the next tick = %+v, want the mail to go", retried)
	}
	assignmentReminderTo(t, "ana@example.com")
	if got := assignmentRemindersFor(t, env, f.saleID); got != 1 {
		t.Fatalf("ledger rows = %d, want 1 — the retry, not the outage", got)
	}
}

// A SENT MAIL THAT CANNOT BE RECORDED IS COUNTED AS `unrecorded`. The mail
// left, so it is `sent`; the ledger refused it, so the buyer may be written to
// one more time than the cap allows — and the Operator is told, by a count.
func TestAssignmentReminderCountsASentMailTheLedgerRefused(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentReminderFixture(t, env)
	withAssignmentReminderSource(t, 0, errors.New("ledger refused"))

	result := sweepAssignmentReminders(t, env)
	if result.Due != 1 || result.Sent != 1 || result.Unrecorded != 1 || result.Failed != 0 {
		t.Fatalf("sweep = %+v, want due=1 sent=1 unrecorded=1 failed=0", result)
	}
	assignmentReminderTo(t, "ana@example.com")
	if got := assignmentRemindersFor(t, env, f.saleID); got != 0 {
		t.Fatalf("ledger rows = %d, want 0 — the write was refused", got)
	}
	if result.Backlog != 1 {
		t.Fatalf("backlog = %d, want 1 — an unrecorded Sale is still due, which is the over-mailing the count warns of", result.Backlog)
	}
}

// THE RESPONSE AND THE LOG CARRY COUNTS ONLY. The scheduler's token must not
// become a list of people, and the log aggregator has broader access and
// longer retention than the database: on the success path neither names a
// buyer, an address, a Sale or an Event.
func TestAssignmentReminderSweepNamesNobodyInItsResponseOrLog(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentReminderFixture(t, env)
	logs := withSalesLogger(t)

	resp, body := env.post(t, "/api/v1/internal/assignment-reminders/sweep", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sweep status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	var result assignmentSweepResult
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("decode sweep result: %v", err)
	}
	if result.Sent != 1 {
		t.Fatalf("sweep = %+v, want one mail so the success path is the one inspected", result)
	}

	// The body is exactly the six counts and nothing else.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body.Data, &fields); err != nil {
		t.Fatalf("decode sweep fields: %v", err)
	}
	for _, key := range []string{"due", "sent", "skipped", "failed", "unrecorded", "backlog"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("response lacks %q; body = %s", key, body.Data)
		}
	}
	if len(fields) != 6 {
		t.Fatalf("response carries %d fields, want exactly the six counts; body = %s", len(fields), body.Data)
	}

	identifiers := []string{"ana@example.com", "Ana", "Lopez", f.saleID, f.eventID, "Assign Fest", "assign-fest-reminders", f.selfHeldID, f.otherID}
	for _, id := range identifiers {
		if strings.Contains(string(body.Data), id) {
			t.Fatalf("response names %q; body = %s", id, body.Data)
		}
	}

	swept := logs.only(t, "assignment reminders swept")
	if got := swept.arg(t, "sent"); got != 1 {
		t.Fatalf("log line sent = %v, want 1", got)
	}
	if rendered := logs.rendered(); rendered == "" {
		t.Fatal("nothing was logged")
	} else {
		for _, id := range identifiers {
			if strings.Contains(rendered, id) {
				t.Fatalf("the log names %q:\n%s", id, rendered)
			}
		}
	}
}
