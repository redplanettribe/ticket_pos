package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The go-live sentence (#364, parent #361, ADR 0051), exercised end to end:
// the sweep reads the Sale's created_at off the ledger's own query and the
// mail decides from it. Two Sales, one each side of
// platform.TicketAssignmentWentLiveAt, sold through the real checkout at the
// clock's time.
//
// The harness's fixed clock (2026-07-07) is itself before the go-live moment,
// so "before" is the fixture as it stands; "after" moves the clock past the
// constant before selling. The fixture then moves the clock a further two
// days on, so the 24-hour floor is behind the Sale either way.

const (
	assignmentGoLiveSentenceEN = "When you bought, tickets could not yet be assigned; now they can."
	assignmentGoLiveSentenceES = "Cuando compró, las entradas aún no se podían asignar; ahora sí."
)

// A Sale made before Ticket Assignment existed: the buyer is told so.
func TestAssignmentReminderTellsAPreFeatureBuyerThatAssigningIsNew(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentReminderFixture(t, env)
	if !f.soldAt.Before(platform.TicketAssignmentWentLiveAt) {
		t.Fatalf("fixture sold at %v, which is not before the go-live moment %v", f.soldAt, platform.TicketAssignmentWentLiveAt)
	}

	if result := sweepAssignmentReminders(t, env); result.Sent != 1 {
		t.Fatalf("sweep = %+v, want one mail", result)
	}
	sent := assignmentReminderTo(t, "ana@example.com")
	if !sent.SaleCreatedAt.Before(platform.TicketAssignmentWentLiveAt) {
		t.Fatalf("sale created at = %v, want before %v", sent.SaleCreatedAt, platform.TicketAssignmentWentLiveAt)
	}
	if text := sent.Text(); !strings.Contains(text, assignmentGoLiveSentenceEN) {
		t.Fatalf("reminder body = %q, want the go-live sentence for a Sale older than the feature", text)
	}
}

// A Sale made once the feature existed: the ordinary mail, with no sentence
// about a history this buyer never lived through.
func TestAssignmentReminderSaysNothingAboutGoLiveToABuyerWhoBoughtAfterIt(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	soldAt := platform.TicketAssignmentWentLiveAt.Add(time.Hour)
	moveClockTo(t, soldAt)
	f := sellAssignmentReminderFixture(t, env, "ana@example.com", "Ana", 2)
	// The fixture dates the Event, and parks the clock, from the harness's
	// fixed clock, which this Sale is weeks past: put the Event a month past
	// the Sale again and the clock two days past it, as the fixture meant to.
	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		f.eventID, soldAt.Add(30*24*time.Hour)); err != nil {
		t.Fatalf("set the Event's start: %v", err)
	}
	moveClockTo(t, soldAt.Add(48*time.Hour))

	if result := sweepAssignmentReminders(t, env); result.Sent != 1 {
		t.Fatalf("sweep = %+v, want one mail", result)
	}
	sent := assignmentReminderTo(t, "ana@example.com")
	if sent.SaleCreatedAt.Before(platform.TicketAssignmentWentLiveAt) {
		t.Fatalf("sale created at = %v, want on or after %v", sent.SaleCreatedAt, platform.TicketAssignmentWentLiveAt)
	}
	text := sent.Text()
	if strings.Contains(text, assignmentGoLiveSentenceEN) || strings.Contains(text, assignmentGoLiveSentenceES) {
		t.Fatalf("reminder body = %q, want no go-live sentence for a Sale made once the feature existed", text)
	}
	if !strings.Contains(text, "1 of your 2 tickets has no address yet") || !strings.Contains(text, sent.ConfirmationLink) {
		t.Fatalf("reminder body = %q, want the ordinary reminder", text)
	}
}
