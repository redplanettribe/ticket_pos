package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Answer Reminder (#317, ADR 0044; #328, parent #322, ADR 0046; #347,
// parent #342, ADR 0049): the mail telling the Holder of a Ticket that it still
// owes an Answer, and pointing them at the Customer Area where they give it.
//
// IT CHASES THE PERSON WHO HOLDS THE TICKET, AND NOBODY ELSE. ADR 0044
// addressed this mail to the buyer "because there is nobody else to address";
// ADR 0046 gave a Ticket a Holder who accepts by mail; ADR 0049 ruled that only
// the Holder answers, so only the Holder is chased. The buyer is a Holder too —
// of exactly one Ticket, the Self-held one they hold by paying (ADR 0048) — and
// is written to about that Ticket and no other. An `unassigned` Ticket, an
// `assigned` one nobody accepted, and one the Holder Address Purge has been
// through are chased by NOBODY: the buyer's debt on such a Ticket is an
// assignment, not an Answer, and nudging that is a different mail ADR 0049
// explicitly left unbuilt.
//
// THE RATIONING IS PER TICKET SINCE #328, and the two facts it is easiest to
// confuse are worth separating before reading anything below. A LEDGER ROW IS A
// TICKET CHASED, never a message sent: one mail to a Holder of two Tickets
// writes two rows. So remindersFor (a whole Sale) counts Tickets and grows with
// how many of the Sale's Tickets are held, while remindersForTicket is what
// the cap of two is actually about.
//
// THIS FILE IS WHERE THE RATIONING MEETS REAL ROWS. The rule is stated in Go
// (catalog.MayRemind and catalog.AnswerReminderRecipient, unit-tested clause
// by clause) and again in SQL (repository.answerReminderRation), because the
// sweep has to be able to skip what it may not mail inside the database.
// Neither may be changed alone, and these tests are what notices: every one of
// them drives the real endpoint against a real Postgres and asserts on the mail
// that came out the other end.
//
// TIME MOVES BY MOVING THE CLOCK, never by sleeping and never by backdating a
// ledger row: `sent_at` is written from the catalog service's clock, so moving
// that clock forward ages a reminder exactly as the calendar would. A test that
// reached into SQL to age a send would be asserting against its own UPDATE.
//
// WHO IS MAILED IS THE ASSERTION, so most of these count messages rather than
// reading them. "Nobody was chased about the unassigned Ticket" is a fact about
// the captured list being short, and no field of any message can state it.
// What one says is pinned in internal/platform/email_answerreminder_test.go,
// where the rendered words are.

// sweepResult is what one run of the sweep reports. A tally rather than a bare
// 200, for the reason the purge's and the Reversal Reconciler's are: the
// endpoint is the runbook as much as the automation's entry point.
type sweepResult struct {
	Due        int `json:"due"`
	Sent       int `json:"sent"`
	Skipped    int `json:"skipped"`
	Failed     int `json:"failed"`
	Unrecorded int `json:"unrecorded"`
	DueTotal   int `json:"due_total"`
}

// sweepAnswerReminders runs one tick and asserts only that the endpoint
// answered.
//
// It takes no credential, on the same terms as drainReversals and
// purgeAbandonedAnswers: the endpoint is authenticated by Cloud Run IAM before
// the request reaches the API (ADR 0008), which is infrastructure this suite
// does not run. What is exercised here is the behaviour behind that gate.
func sweepAnswerReminders(t *testing.T, env *testEnv) sweepResult {
	t.Helper()
	resp, body := env.post(t, "/api/v1/internal/answer-reminders/sweep", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sweep status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	var out sweepResult
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode sweep result: %v", err)
	}
	return out
}

// moveClockTo moves every clock the reminder reads to one moment.
//
// BOTH SERVICES, because the decision is split across them exactly as the
// deployment splits it: catalog rations against its clock and stamps the ledger
// with it, sales bounds the run with its own. Two clocks that could drift apart
// would make "a week has passed" and "when the last one was sent" disagree,
// which is a state no deployment can be in.
func moveClockTo(t *testing.T, at time.Time) {
	t.Helper()
	sharedApp.CatalogService.WithClock(func() time.Time { return at })
	sharedApp.SalesService.WithClock(func() time.Time { return at })
}

// remindersFor counts the ledger rows every Ticket of one Ticket Sale has
// between them.
//
// IT COUNTS TICKETS CHASED AND NOT MAILS SENT, which is the distinction #328
// introduced and the one every assertion using this helper has to keep straight.
// One reminder to a Holder of two Tickets writes TWO rows here, because the
// allowance it spends is each Ticket's. How many messages went out is what the
// captured sender says, and the two figures are deliberately different things.
//
// It reaches the Sale through the Tickets rather than through a column, because
// migration 083 dropped `answer_reminders.ticket_sale_id`: the Sale is not the
// unit of anything here any more, and a second key that could disagree with the
// first is a key not worth keeping.
func remindersFor(t *testing.T, env *testEnv, ticketSaleID string) int {
	t.Helper()
	var count int
	if err := env.db.QueryRow(`
		SELECT COUNT(*)
		FROM answer_reminders ar
		JOIN tickets tk ON tk.id = ar.ticket_id
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		WHERE l.ticket_sale_id = $1
	`, ticketSaleID).Scan(&count); err != nil {
		t.Fatalf("count answer reminders: %v", err)
	}
	return count
}

// remindersForTicket counts ONE Ticket's ledger rows: the figure
// catalog.MaxAnswerReminders is actually a cap on.
//
// THIS IS THE HELPER THE RATIONING TESTS REACH FOR. A per-Sale count can read as
// two when one Ticket has had both of its and another has had none, which is a
// state the cap permits and which says nothing at all about whether anybody was
// mailed twice.
func remindersForTicket(t *testing.T, env *testEnv, ticketID string) int {
	t.Helper()
	var count int
	if err := env.db.QueryRow(
		`SELECT COUNT(*) FROM answer_reminders WHERE ticket_id = $1`, ticketID,
	).Scan(&count); err != nil {
		t.Fatalf("count answer reminders for Ticket %s: %v", ticketID, err)
	}
	return count
}

// reminderFixture is an upcoming published Event with one required Ticket
// Question, sold ONLINE to a buyer who answered nothing: two Tickets, the first
// of which the buyer holds by paying (ADR 0048) and the second of which nobody
// holds.
//
// AN ONLINE SALE, deliberately, where #317's fixture was an import. Since ADR
// 0049 a Ticket nobody holds is chased by nobody, and an imported Sale has no
// Self-held Ticket — so the only Sale on which this mail can reach a buyer at
// all is one they paid for online. The buyer skipped the checkout's question,
// which is the honest state of the debt and the case this mail exists for.
//
// BOTH FLAGS ARE OPEN, EXPLICITLY AND SEPARATELY. Ticket Questions is what
// makes the debt exist at all; Ticket Assignment is what makes anybody hold a
// Ticket. They are two deployment switches (ADR 0046) and the suite may never
// treat them as one — TestAnswerReminderChasesNobodyWhileTicketAssignmentIsDark
// closes the second and keeps the first open.
type reminderFixture struct {
	staffSession string
	eventID      string
	ticketTypeID string
	saleID       string
	questionID   string
	// selfHeldID is the buyer's own Ticket (ordinal 1); otherID is the Sale's
	// second Ticket, `unassigned` until a test hands it to somebody.
	selfHeldID string
	otherID    string
	// ana is the buyer's Customer Session: assignment happens from a buyer
	// surface and nowhere else, and so does answering a held Ticket.
	ana string
}

func newReminderFixture(t *testing.T, env *testEnv) reminderFixture {
	t.Helper()
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	return sellReminderFixture(t, env)
}

// sellReminderFixture is the fixture's purchase, split out so that the
// dark-flag test can make the same sale with Ticket Assignment closed.
//
// ITS MAILBOX IS EMPTY WHEN IT RETURNS. The checkout sends a Sale Confirmation
// and signing in sends a passcode, and both would otherwise sit in the capture
// beside the reminders these tests count. The reminder assertions are about
// lengths, so a stray message is not a nuisance — it is a wrong answer.
func sellReminderFixture(t *testing.T, env *testEnv) reminderFixture {
	t.Helper()
	f := reminderFixture{}
	f.staffSession = orgAdminSession(t, env)
	f.eventID, f.ticketTypeID = publishCheckoutEvent(t, env, f.staffSession, "Answer Fest", "answer-fest-reminders", 2000, 50)

	// The doors open in thirty days. The reminder is silent once an Event has
	// started, so every test here needs a start and needs it to be ahead.
	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		f.eventID, env.fixedClock.Add(30*24*time.Hour)); err != nil {
		t.Fatalf("set the Event's start: %v", err)
	}

	f.questionID = createTicketQuestion(t, env, f.staffSession, f.eventID, f.ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	}).ID

	begun := beginCheckoutOK(t, env, "test-org", "answer-fest-reminders",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(f.ticketTypeID, 2)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	f.saleID = saleIDOfPayment(t, env, begun.ClientTransactionID)
	ids := ticketIDsOfSale(t, env, f.saleID)
	if len(ids) != 2 {
		t.Fatalf("the fixture's Sale has %d Tickets, want 2", len(ids))
	}
	f.selfHeldID, f.otherID = ids[0], ids[1]

	f.ana = customerSignIn(t, env, "ana@example.com")
	sharedEmail.Reset()
	return f
}

// acceptTicketAs walks one Ticket all the way to `accepted`: the buyer names an
// address, the platform writes to it, and the person there clicks.
//
// IT GOES THROUGH THE REAL FLOW rather than writing accepted_at by hand, and
// that is not ceremony. `accepted` means a Verified Customer exists, minted by a
// click, and a test that UPDATEd the column would produce a state the database's
// own CHECKs allow and the product cannot reach — then assert that the reminder
// handled it. It also leaves the Assignment mail in the capture, which is the
// only place a token has ever been obtainable in this package.
//
// It resets the mailbox on the way out, for the fixture's reason.
func acceptTicketAs(t *testing.T, env *testEnv, session, saleID, ticketID, address string) {
	t.Helper()
	assignTicketOK(t, env, session, saleID, ticketID, address)
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, address)))
	sharedEmail.Reset()
}

// reminderFor finds the one reminder sent to an address, failing if there is
// not exactly one. The count is half the assertion in most of these tests.
func reminderFor(t *testing.T, address string) platform.HolderAnswerReminder {
	t.Helper()
	var found []platform.HolderAnswerReminder
	for _, mail := range sharedEmail.HolderAnswerRemindersSent() {
		if mail.To == address {
			found = append(found, mail)
		}
	}
	if len(found) != 1 {
		t.Fatalf("Answer Reminders to %s = %d, want exactly 1", address, len(found))
	}
	return found[0]
}

// assertNoReminders is the assertion this ticket exists for, made in several
// tests because it is the one property a reader will assume rather than check:
// a reminder that did not go out leaves no message and no ledger row.
func assertNoReminders(t *testing.T, env *testEnv, saleID, why string) {
	t.Helper()
	if got := sharedEmail.HolderAnswerRemindersSent(); len(got) != 0 {
		t.Fatalf("%d Answer Reminder(s) went out to %v; want none: %s", len(got), addressesOf(got), why)
	}
	if got := remindersFor(t, env, saleID); got != 0 {
		t.Fatalf("ledger rows across the Sale = %d, want 0: %s", got, why)
	}
}

func addressesOf(mails []platform.HolderAnswerReminder) []string {
	out := make([]string, 0, len(mails))
	for _, mail := range mails {
		out = append(out, mail.To)
	}
	return out
}

// THE BUYER IS CHASED AS A HOLDER, ABOUT THEIR OWN TICKET AND NO OTHER. Ana
// bought two, holds the first by paying, and has handed the second to nobody.
// One mail, listing one Ticket, pointing at her Customer Area; one ledger row,
// on the Ticket she holds — and nothing at all on the one she does not.
func TestAnswerReminderMailsTheBuyerAboutTheirSelfHeldTicketOnly(t *testing.T) {
	env := setupTest(t)
	f := newReminderFixture(t, env)

	result := sweepAnswerReminders(t, env)
	if result.Sent != 1 || result.Due != 1 {
		t.Fatalf("sweep = %+v, want one mail due and one sent: the buyer, about the one Ticket they hold", result)
	}

	sent := reminderFor(t, "ana@example.com")
	if len(sent.Tickets) != 1 {
		t.Fatalf("the buyer's reminder lists %d tickets, want exactly their Self-held one — the unassigned Ticket is an assignment debt, not an Answer debt", len(sent.Tickets))
	}
	if sent.Tickets[0].EventName != "Answer Fest" || sent.Tickets[0].TicketTypeName != "GA" {
		t.Errorf("the reminder names event=%q ticket type=%q, want Answer Fest / GA", sent.Tickets[0].EventName, sent.Tickets[0].TicketTypeName)
	}
	// THE LINK IS THE MESSAGE, and it is the Customer Area — the page whose
	// held-ticket panel is where every Holder answers (ADR 0049). Not a
	// Confirmation Link, which opens a whole purchase; not an Assignment Link,
	// which is a credential; and not an Answer Link, which is retired.
	if sent.CustomerAreaURL == "" {
		t.Fatal("the reminder carries no link: a reminder with nothing to open is an instruction its reader cannot follow")
	}
	if !strings.HasSuffix(sent.CustomerAreaURL, "/tickets") || strings.Contains(sent.Text(), "token=") {
		t.Fatalf("the reminder points at %q, want the Storefront's /tickets page and no token anywhere in the mail", sent.CustomerAreaURL)
	}

	// ONE ROW FOR ONE MAIL, on the Ticket it covered. The unassigned Ticket
	// spent nothing, because nobody was written to about it.
	if got := remindersForTicket(t, env, f.selfHeldID); got != 1 {
		t.Fatalf("the Self-held Ticket has %d ledger rows, want 1 — the send is only rationed if it was written down", got)
	}
	if got := remindersForTicket(t, env, f.otherID); got != 0 {
		t.Fatalf("the unassigned Ticket has %d ledger rows, want 0: nobody was chased about it, so nothing was spent on it", got)
	}
}

// AN UNASSIGNED TICKET IS CHASED BY NOBODY — not the buyer, not anybody — and
// neither is one whose address was purged before anybody accepted. This is
// the assertion #347 exists for: the mail that used to arrive here was the
// platform nagging the one person who is not allowed to answer.
//
// The buyer answers their own Ticket first, so the only debt left on the Sale
// is the unheld Ticket's, and the sweep's every figure reads zero: not due,
// not deferred, not counted in the backlog. THE DEBT ITSELF IS UNTOUCHED by
// the silence — the Organization's chase list still shows it, which is what
// would break if "has a recipient" had been pushed down into the derivation.
func TestAnswerReminderNeverChasesAnUnassignedTicket(t *testing.T) {
	env := setupTest(t)
	f := newReminderFixture(t, env)
	answerHeldTicketOK(t, env, f.ana, f.selfHeldID, f.questionID, map[string]any{"text": "M"})

	result := sweepAnswerReminders(t, env)
	if result.Sent != 0 || result.Due != 0 || result.DueTotal != 0 {
		t.Fatalf("sweep = %+v, want silence: an unassigned Ticket has nobody to chase, and is not due to anybody", result)
	}
	assertNoReminders(t, env, f.saleID, "an unassigned Ticket is an assignment debt, not an Answer debt, and this mail does not chase it")

	// A PURGED TICKET READS UNASSIGNED AGAIN. The Holder Address Purge takes an
	// address nobody accepted (migration 081), leaving the Ticket exactly as
	// the purge leaves it; with nobody holding it, nobody is chased.
	assignTicketOK(t, env, f.ana, f.saleID, f.otherID, "carla@example.com")
	sharedEmail.Reset()
	if _, err := env.db.Exec(`
		UPDATE tickets SET holder_email = NULL, assigned_at = NULL, holder_address_purged_at = $2
		WHERE id = $1
	`, f.otherID, env.fixedClock); err != nil {
		t.Fatalf("purge the address: %v", err)
	}
	if purged := sweepAnswerReminders(t, env); purged.Sent != 0 || purged.Due != 0 || purged.DueTotal != 0 {
		t.Fatalf("sweep after the purge = %+v, want silence: a purged Ticket is held by nobody", purged)
	}
	assertNoReminders(t, env, f.saleID, "a purged never-accepted Ticket has no Holder to write to")

	page := listOutstanding(t, env, f.staffSession, f.eventID)
	if page.OutstandingCount == 0 {
		t.Fatal("the Outstanding Answers list emptied because nobody could be chased: the silence belongs to the mail, never to the debt")
	}
}

// AN ASSIGNED-BUT-UNACCEPTED TICKET IS CHASED BY NOBODY EITHER. The address on
// it belongs to somebody who has agreed to nothing — ignoring the Assignment
// mail IS the decline (ADR 0046) — and the buyer can no more answer for it than
// for an unassigned one.
func TestAnswerReminderNeverChasesAnUnacceptedAssignment(t *testing.T) {
	env := setupTest(t)
	f := newReminderFixture(t, env)
	answerHeldTicketOK(t, env, f.ana, f.selfHeldID, f.questionID, map[string]any{"text": "M"})
	assignTicketOK(t, env, f.ana, f.saleID, f.otherID, "carla@example.com")
	sharedEmail.Reset()

	result := sweepAnswerReminders(t, env)
	if result.Sent != 0 || result.Due != 0 || result.DueTotal != 0 {
		t.Fatalf("sweep = %+v, want silence: an address nobody accepted is not a recipient", result)
	}
	assertNoReminders(t, env, f.saleID, "a Ticket assigned and never accepted has no Holder, and its named address has agreed to hear nothing")
}

// Nothing is sent while the feature is dark, however often the endpoint is
// called (ADR 0045). This is the inner of the two switches the job ships behind
// — the outer one is the Cloud Scheduler job, created paused — and it is the one
// a curl can get past.
func TestAnswerReminderSendsNothingWhileTheFlagIsOff(t *testing.T) {
	env := setupTest(t)
	// The flag is opened only to author the question and make the sale, which
	// is the state a deployment that had collected Answers and then closed the
	// flag would be in.
	f := newReminderFixture(t, env)
	sharedApp.CatalogService.WithTicketQuestions(false)
	sharedApp.SalesService.WithTicketQuestions(false)

	result := sweepAnswerReminders(t, env)
	if result.Sent != 0 || result.Due != 0 {
		t.Fatalf("sweep = %+v, want a dark deployment to find nobody and mail nobody", result)
	}
	assertNoReminders(t, env, f.saleID, "this mail is entirely about a collection the Privacy Policy does not yet describe")
}

// WITH TICKET ASSIGNMENT DARK, NOBODY HOLDS ANYTHING, SO NOBODY IS CHASED. A
// deployment that never opened assignment mints no Self-held Ticket (ADR 0048)
// and accepts nothing, so every Ticket is unheld and the sweep has no
// recipient for any of them. The "mail the buyer instead" fallback that used
// to cover this case is gone with ADR 0049: with Ticket Questions open and
// Ticket Assignment closed, the debt exists and is visible to the
// Organization, and the mail simply does not chase it.
//
// THE TWO FLAGS MUST STAY INDEPENDENT (ADR 0046), and this is the sweep's half
// of that property: Ticket Questions stays open, so the debt is live, and the
// newer flag's being closed is what leaves it with no Holder.
func TestAnswerReminderChasesNobodyWhileTicketAssignmentIsDark(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	f := sellReminderFixture(t, env)

	var accepted int
	if err := env.db.QueryRow(
		`SELECT COUNT(*) FROM tickets WHERE accepted_at IS NOT NULL`,
	).Scan(&accepted); err != nil {
		t.Fatalf("count held Tickets: %v", err)
	}
	if accepted != 0 {
		t.Fatalf("%d Ticket(s) are held with Ticket Assignment dark; this test only means something when nobody holds anything", accepted)
	}

	result := sweepAnswerReminders(t, env)
	if result.Sent != 0 || result.Due != 0 || result.DueTotal != 0 {
		t.Fatalf("sweep = %+v, want silence: with assignment dark no Ticket has a Holder, and a Ticket without a Holder is chased by nobody", result)
	}
	assertNoReminders(t, env, f.saleID, "a dark feature answers as a build that never had it, and a build without Holders chases nobody")

	page := listOutstanding(t, env, f.staffSession, f.eventID)
	if page.OutstandingCount == 0 {
		t.Fatal("the debt vanished with the assignment flag: the silence belongs to the mail, never to the debt")
	}
}

// AT MOST ONE PER TICKET PER 7 DAYS, which is the rule that makes this a swept
// job rather than something the question editor triggers.
//
// The second sweep here stands in for an Organization authoring a second
// question ten minutes later: the sweep sees state rather than events, so
// nothing about a new question buys a new mail.
func TestAnswerReminderIsNotSentTwiceInsideTheWeek(t *testing.T) {
	env := setupTest(t)
	f := newReminderFixture(t, env)

	sweepAnswerReminders(t, env)

	// A second question, authored minutes later, owed by the same Ticket.
	createTicketQuestion(t, env, f.staffSession, f.eventID, f.ticketTypeID, map[string]any{
		"label": "Dietary requirements", "kind": "short_text", "required": true,
	})
	second := sweepAnswerReminders(t, env)
	if second.Sent != 0 || second.Due != 0 {
		t.Fatalf("second sweep = %+v, want silence: an Organization drafting questions must not mail the same person twice", second)
	}

	// Six days on, still inside the week.
	moveClockTo(t, env.fixedClock.Add(6*24*time.Hour))
	if sixth := sweepAnswerReminders(t, env); sixth.Sent != 0 {
		t.Fatalf("sweep six days later = %+v, want silence", sixth)
	}

	// Seven days on, the second reminder is due.
	moveClockTo(t, env.fixedClock.Add(catalog.AnswerReminderInterval))
	if seventh := sweepAnswerReminders(t, env); seventh.Sent != 1 {
		t.Fatalf("sweep seven days later = %+v, want the second reminder", seventh)
	}
	if got := len(sharedEmail.HolderAnswerRemindersSent()); got != 2 {
		t.Fatalf("reminders sent = %d, want 2 across the whole week", got)
	}
	// Two mails covering one Ticket each: two rows, both on the held Ticket.
	if got := remindersForTicket(t, env, f.selfHeldID); got != 2 {
		t.Fatalf("the Self-held Ticket has %d ledger rows, want 2 — one per mail", got)
	}
	if got := remindersForTicket(t, env, f.otherID); got != 0 {
		t.Fatalf("the unassigned Ticket has %d ledger rows, want 0", got)
	}
}

// AT MOST TWO EVER, and no passage of time buys a third. This is the clause with
// no way back: the mail is transactional and carries no unsubscribe, so the cap
// is the only thing standing between a Holder and an unbounded chase.
func TestAnswerReminderStopsForeverAfterTwo(t *testing.T) {
	env := setupTest(t)
	f := newReminderFixture(t, env)

	// The Event is far enough out that the silence-after-start rule never fires
	// inside the weeks this test walks.
	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		f.eventID, env.fixedClock.Add(120*24*time.Hour)); err != nil {
		t.Fatalf("push the Event's start out: %v", err)
	}

	for week := 0; week < 6; week++ {
		moveClockTo(t, env.fixedClock.Add(time.Duration(week)*catalog.AnswerReminderInterval))
		sweepAnswerReminders(t, env)
	}

	if got := len(sharedEmail.HolderAnswerRemindersSent()); got != catalog.MaxAnswerReminders {
		t.Fatalf("reminders sent over six weeks = %d, want %d — the cap is per Ticket and for its whole life", got, catalog.MaxAnswerReminders)
	}
	if got := remindersForTicket(t, env, f.selfHeldID); got != catalog.MaxAnswerReminders {
		t.Fatalf("the Self-held Ticket has %d ledger rows, want %d — no passage of time buys a third", got, catalog.MaxAnswerReminders)
	}
	if got := remindersForTicket(t, env, f.otherID); got != 0 {
		t.Fatalf("the unassigned Ticket has %d ledger rows, want 0", got)
	}
}

// SILENT ONCE THE EVENT HAS STARTED. The debt itself survives the doors opening
// — the Organization's chase list still shows it, deliberately, because "twelve
// people never told us their size" is what somebody reviewing the event came to
// find out — but every route into an Answer is frozen by then, so a reminder
// would be an instruction its reader cannot follow.
func TestAnswerReminderIsSilentOnceTheEventHasStarted(t *testing.T) {
	env := setupTest(t)
	f := newReminderFixture(t, env)

	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		f.eventID, env.fixedClock.Add(-time.Hour)); err != nil {
		t.Fatalf("open the doors: %v", err)
	}

	result := sweepAnswerReminders(t, env)
	if result.Sent != 0 || result.Due != 0 || result.DueTotal != 0 {
		t.Fatalf("sweep = %+v, want silence once the Event has started", result)
	}
	assertNoReminders(t, env, f.saleID, "the doors have opened")

	// The debt is untouched by the silence: #313's list still reports it, which is
	// the property that would break if the rationing had been pushed down into the
	// derivation.
	page := listOutstanding(t, env, f.staffSession, f.eventID)
	if page.OutstandingCount == 0 {
		t.Fatal("the Outstanding Answers list emptied when the Event started: the silence belongs to the mail, never to the debt")
	}
}

// A REVERSED SALE IS NEVER CHASED. Its tickets have ceased to exist and its
// money has gone back, so there is nobody to write to about a shirt nobody is
// coming to collect.
//
// Reversed in SQL, on held_ticket_answers_test.go's terms: the customer's own
// reversal route is about money and its window, not the state this test needs.
func TestAnswerReminderIsNeverSentForAReversedSale(t *testing.T) {
	env := setupTest(t)
	f := newReminderFixture(t, env)

	if _, err := env.db.Exec(`UPDATE ticket_sales SET status = 'reversed' WHERE id = $1`, f.saleID); err != nil {
		t.Fatalf("reverse the Sale: %v", err)
	}

	result := sweepAnswerReminders(t, env)
	if result.Sent != 0 || result.Due != 0 || result.DueTotal != 0 {
		t.Fatalf("sweep = %+v, want silence for a reversed Sale", result)
	}
	assertNoReminders(t, env, f.saleID, "a reversed Sale's Tickets have ceased to exist")
}

// TRANSACTIONAL, NOT MARKETING. The buyer granted no Marketing Consent and has
// no Follow Digest. The reminder arrives anyway, on the same footing as the
// Sale Confirmation that carried the same sentence — and it arrives on the
// TRANSACTIONAL sender, which is what stops it ever leaving the marketing
// domain (ADR 0030, ADR 0034).
func TestAnswerReminderIsSentRegardlessOfMarketingConsent(t *testing.T) {
	env := setupTest(t)
	newReminderFixture(t, env)

	var digestEnabled bool
	var marketing int
	if err := env.db.QueryRow(
		`SELECT digest_enabled FROM customers WHERE email = 'ana@example.com'`,
	).Scan(&digestEnabled); err != nil {
		t.Fatalf("read the buyer's marketing state: %v", err)
	}
	if err := env.db.QueryRow(`
		SELECT COUNT(*) FROM consent_records cr JOIN customers c ON c.id = cr.customer_id
		WHERE c.email = 'ana@example.com' AND cr.marketing_consent = TRUE
	`).Scan(&marketing); err != nil {
		t.Fatalf("read the buyer's Consent Records: %v", err)
	}
	if digestEnabled || marketing != 0 {
		t.Fatalf("the fixture's buyer has marketing state (digest_enabled=%v, marketing grants=%d): this test only means something against somebody who granted nothing", digestEnabled, marketing)
	}

	if result := sweepAnswerReminders(t, env); result.Sent != 1 {
		t.Fatalf("sweep = %+v, want the reminder to reach a buyer who granted no Marketing Consent", result)
	}
	reminderFor(t, "ana@example.com")
}

// WRITTEN IN THE RECIPIENT'S MAIL LOCALE. The Sale recorded no Sale Locale, so
// the chain falls through to what the Customer's own record remembers (ADR
// 0033) — which is exactly the step this mail will usually take, being sent
// weeks later by a job with no page anywhere near it.
func TestAnswerReminderIsWrittenInTheRecipientsMailLocale(t *testing.T) {
	env := setupTest(t)
	f := newReminderFixture(t, env)

	if _, err := env.db.Exec(`UPDATE ticket_sales SET locale = NULL WHERE id = $1`, f.saleID); err != nil {
		t.Fatalf("forget the Sale Locale: %v", err)
	}
	// The buyer signed in on the Spanish Storefront at some point, which is the
	// only thing that writes this column.
	if _, err := env.db.Exec(
		`UPDATE customers SET mail_locale = 'es' WHERE email = 'ana@example.com'`,
	); err != nil {
		t.Fatalf("remember the buyer's language: %v", err)
	}

	sweepAnswerReminders(t, env)
	sent := reminderFor(t, "ana@example.com")
	if sent.Locale != platform.LocaleES {
		t.Fatalf("reminder locale = %q, want es", sent.Locale)
	}
	if !strings.Contains(sent.Text(), "aún necesita respuesta") {
		t.Fatalf("reminder body = %q, want the Spanish message", sent.Text())
	}
}

// Nothing owed, nothing said. Once the held Ticket has answered every required
// question the Sale leaves the sweep entirely — which is also what makes the
// reminder stop early for the reader who acts on the first one.
func TestAnswerReminderStopsOnceTheAnswersArrive(t *testing.T) {
	env := setupTest(t)
	f := newReminderFixture(t, env)

	sweepAnswerReminders(t, env)
	if got := len(sharedEmail.HolderAnswerRemindersSent()); got != 1 {
		t.Fatalf("reminders sent = %d, want the first one", got)
	}

	// The buyer answers from their own held-ticket panel, which is the surface
	// the mail pointed them at.
	answerHeldTicketOK(t, env, f.ana, f.selfHeldID, f.questionID, map[string]any{"text": "M"})

	// A week later the second reminder would have been due, and there is nothing
	// left to remind anybody about: the other Ticket still owes, and has nobody
	// to ask.
	moveClockTo(t, env.fixedClock.Add(catalog.AnswerReminderInterval))
	result := sweepAnswerReminders(t, env)
	if result.Sent != 0 || result.Due != 0 || result.DueTotal != 0 {
		t.Fatalf("sweep = %+v, want silence once the held Ticket's debt is discharged", result)
	}
}

// The sweep is safe to curl by hand, repeatedly, which is how this ships: the
// runbook before the automation. A second call inside the cooldown mails nobody,
// because the ledger row was written before the first response was.
func TestAnswerReminderSweepIsSafeToRunByHandTwice(t *testing.T) {
	env := setupTest(t)
	f := newReminderFixture(t, env)

	first := sweepAnswerReminders(t, env)
	second := sweepAnswerReminders(t, env)

	if first.Sent != 1 || second.Sent != 0 {
		t.Fatalf("first = %+v, second = %+v, want one mail and then silence", first, second)
	}
	if second.DueTotal != 0 {
		t.Fatalf("second sweep reports %d still due, want 0 — the backlog is what an operator reads two runs apart", second.DueTotal)
	}
	if got := remindersFor(t, env, f.saleID); got != 1 {
		t.Fatalf("ledger rows = %d, want 1 — one mail covering the one held Ticket, written down before the response was", got)
	}
}

// ---------------------------------------------------------------------------
// Named Holders (#328, parent #322, ADR 0046), on ADR 0049's terms.
//
// Everything above is about the one Ticket the buyer holds. What follows is
// the Ticket they hand on: the moment somebody accepts it, that person is its
// Holder, and is chased about it on exactly the terms the buyer is about theirs.
// ---------------------------------------------------------------------------

// EACH HOLDER IS CHASED ABOUT THEIR OWN TICKET ONLY. Ana holds the first
// Ticket by paying and hands the second to Carla, who accepts. Two mails, one
// Ticket each — Ana about hers, Carla about hers — and Carla's names nothing
// about Ana or the purchase.
func TestAnswerReminderChasesEachHolderAboutTheirOwnTicketOnly(t *testing.T) {
	env := setupTest(t)
	f := newReminderFixture(t, env)
	acceptTicketAs(t, env, f.ana, f.saleID, f.otherID, "carla@example.com")

	result := sweepAnswerReminders(t, env)
	if result.Due != 2 || result.Sent != 2 {
		t.Fatalf("sweep = %+v, want two mails due and two sent — one per held Ticket, to its own Holder", result)
	}

	ana := reminderFor(t, "ana@example.com")
	carla := reminderFor(t, "carla@example.com")
	if len(ana.Tickets) != 1 || len(carla.Tickets) != 1 {
		t.Fatalf("the buyer's reminder lists %d tickets and the Holder's %d, want exactly one each: nobody is asked for somebody else's answer", len(ana.Tickets), len(carla.Tickets))
	}
	if carla.Tickets[0].EventName != "Answer Fest" || carla.Tickets[0].TicketTypeName != "GA" {
		t.Errorf("the Holder's reminder names event=%q ticket type=%q, want Answer Fest / GA", carla.Tickets[0].EventName, carla.Tickets[0].TicketTypeName)
	}
	// THE LINK IS THE CUSTOMER AREA, where the Holder's own held-ticket panel
	// lives (ADR 0049) — never the buyer's Confirmation Link, which opens a
	// whole purchase, and no longer an Assignment Link, which is a credential
	// this mail has no business re-minting.
	if !strings.HasSuffix(carla.CustomerAreaURL, "/tickets") || strings.Contains(carla.Text(), "token=") {
		t.Fatalf("the Holder's reminder points at %q, want the Storefront's /tickets page and no token in the mail", carla.CustomerAreaURL)
	}

	// AND IT NAMES NOTHING ABOUT THE PURCHASE — asserted on the words the
	// recipient actually reads, because that is where a widening would land. ADR
	// 0044's disclosure rule, carried over by ADR 0046 and applied to an inbox: a
	// Holder is a Verified Customer, and being one buys nobody a fact about
	// somebody else's purchase.
	var ref string
	if err := env.db.QueryRow(`SELECT confirmation_ref FROM ticket_sales WHERE id = $1`, f.saleID).Scan(&ref); err != nil {
		t.Fatalf("read the Sale Confirmation reference: %v", err)
	}
	rendered := carla.Subject() + "\n" + carla.Text()
	for _, forbidden := range []string{"Ana", "Lopez", "ana@example.com", ref} {
		if strings.Contains(rendered, forbidden) {
			t.Errorf("the Holder's Answer Reminder contains %q.\n"+
				"It names the Event, the Ticket Type and the Customer Area and nothing else (ADR 0044, ADR 0046).", forbidden)
		}
	}

	// One ledger row per Ticket, spent by the mail that covered it.
	for _, ticketID := range []string{f.selfHeldID, f.otherID} {
		if got := remindersForTicket(t, env, ticketID); got != 1 {
			t.Errorf("Ticket %s has %d ledger rows, want 1", ticketID, got)
		}
	}
}

// ONE MAIL PER HOLDER PER SWEEP (#335). A Holder of BOTH Tickets of one Sale
// gets ONE Answer Reminder listing both — never two envelopes in one sweep,
// which is Story 48's own clause ("chase two of us without mailing either
// twice") and the shape spam filters punish.
//
// Ana gives her own Ticket away as well (Story 14: a Self-held Ticket handed on
// becomes an ordinary assigned row), so she holds nothing and is mailed about
// nothing. PER-TICKET RATIONING IS UNCHANGED: each listed Ticket burns its own
// allowance, so the one mail writes two ledger rows. Only the envelope is
// shared.
func TestAnswerReminderMailsAHolderOnceAboutBothTheirTickets(t *testing.T) {
	env := setupTest(t)
	f := newReminderFixture(t, env)

	acceptTicketAs(t, env, f.ana, f.saleID, f.selfHeldID, "carla@example.com")
	acceptTicketAs(t, env, f.ana, f.saleID, f.otherID, "carla@example.com")

	// The backlog agrees with the fan-in BEFORE anything is sent: two owed
	// Tickets, one Holder, ONE mail due — the count an operator compares
	// against `sent` must count what will actually go out.
	if backlog, err := sharedApp.CatalogService.CountAnswerRemindersDue(context.Background()); err != nil {
		t.Fatalf("count the backlog: %v", err)
	} else if backlog != 1 {
		t.Fatalf("backlog = %d, want 1: two Tickets held by one person are one mail (#335)", backlog)
	}

	result := sweepAnswerReminders(t, env)
	if result.Due != 1 || result.Sent != 1 {
		t.Fatalf("sweep = %+v, want ONE mail due and ONE sent: one Holder, one envelope, however many Tickets (#335)", result)
	}
	carla := reminderFor(t, "carla@example.com")
	if got := sharedEmail.HolderAnswerRemindersSent(); len(got) != 1 {
		t.Fatalf("reminders went to %v, want Carla alone: the buyer holds nothing now and is chased about nothing", addressesOf(got))
	}

	// EACH OWED TICKET IS LISTED, AND ONE ADDRESS OPENS BOTH: the Customer Area
	// shows every Ticket the signed-in address holds, so the mail carries it
	// once (ADR 0049).
	if len(carla.Tickets) != 2 {
		t.Fatalf("the Holder's one reminder lists %d tickets, want both of the ones she accepted", len(carla.Tickets))
	}
	for _, ticket := range carla.Tickets {
		if ticket.EventName != "Answer Fest" || ticket.TicketTypeName != "GA" {
			t.Errorf("a listed ticket names event=%q type=%q, want Answer Fest / GA", ticket.EventName, ticket.TicketTypeName)
		}
	}
	if got := strings.Count(carla.Text(), "http"); got != 1 || !strings.Contains(carla.Text(), carla.CustomerAreaURL) {
		t.Fatalf("the rendered mail carries %d links, want exactly 1 — the Customer Area, once:\n%s", got, carla.Text())
	}

	// ONE ENVELOPE, TWO ALLOWANCES: the caps stayed per Ticket, so the single
	// mail spends one ledger row for each Ticket it covered.
	for _, ticketID := range []string{f.selfHeldID, f.otherID} {
		if got := remindersForTicket(t, env, ticketID); got != 1 {
			t.Errorf("Ticket %s has %d ledger rows, want 1: each listed Ticket burns its own allowance", ticketID, got)
		}
	}

	// And the second sweep is silent: both Tickets are inside their week.
	if second := sweepAnswerReminders(t, env); second.Sent != 0 {
		t.Fatalf("second sweep = %+v, want silence — the envelope was shared, the cooldown was not lifted", second)
	}
}

// A SALE WITH A MIX MAILS EACH HOLDER AND SKIPS THE REST. Four Tickets: the
// buyer's own, one accepted by Carla, one assigned to Diego who never clicked,
// one assigned to nobody. Two mails — Ana and Carla, one Ticket each — and
// nothing whatsoever for the other two, which is the whole of ADR 0049's
// audience rule in one Sale.
func TestAnswerReminderOnAMixedSaleWritesToEachHolderAndNobodyElse(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	staffSession := orgAdminSession(t, env)
	eventID, ticketTypeID := publishCheckoutEvent(t, env, staffSession, "Answer Fest", "answer-fest-mixed", 2000, 50)
	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		eventID, env.fixedClock.Add(30*24*time.Hour)); err != nil {
		t.Fatalf("set the Event's start: %v", err)
	}
	createTicketQuestion(t, env, staffSession, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})
	begun := beginCheckoutOK(t, env, "test-org", "answer-fest-mixed",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(ticketTypeID, 4)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	saleID := saleIDOfPayment(t, env, begun.ClientTransactionID)
	tickets := ticketIDsOfSale(t, env, saleID)
	if len(tickets) != 4 {
		t.Fatalf("the Sale has %d Tickets, want 4", len(tickets))
	}
	ana := customerSignIn(t, env, "ana@example.com")
	sharedEmail.Reset()

	acceptTicketAs(t, env, ana, saleID, tickets[1], "carla@example.com")
	assignTicketOK(t, env, ana, saleID, tickets[2], "diego@example.com")
	sharedEmail.Reset()

	result := sweepAnswerReminders(t, env)
	if result.Due != 2 || result.Sent != 2 || result.DueTotal != 0 {
		t.Fatalf("sweep = %+v, want two mails — the two Holders — and nothing left over", result)
	}
	ana1 := reminderFor(t, "ana@example.com")
	carla := reminderFor(t, "carla@example.com")
	if len(ana1.Tickets) != 1 || len(carla.Tickets) != 1 {
		t.Fatalf("the buyer's reminder lists %d tickets and the Holder's %d, want one each", len(ana1.Tickets), len(carla.Tickets))
	}
	for _, mail := range sharedEmail.HolderAnswerRemindersSent() {
		if mail.To == "diego@example.com" {
			t.Fatal("an address that never accepted was written to: ignoring the Assignment mail IS the decline (ADR 0046)")
		}
	}

	// THE LEDGER IS WHERE "ONLY THE HELD TICKETS" IS PROVABLE: one row on each
	// Ticket somebody holds, none on the two nobody does.
	want := []int{1, 1, 0, 0}
	for i, ticketID := range tickets {
		if got := remindersForTicket(t, env, ticketID); got != want[i] {
			t.Errorf("Ticket #%d has %d ledger rows, want %d", i+1, got, want[i])
		}
	}

	// And the second sweep is silent for everybody, on the same seven days.
	if second := sweepAnswerReminders(t, env); second.Sent != 0 {
		t.Fatalf("second sweep = %+v, want silence for every Holder alike", second)
	}
}

// RATIONED PER TICKET, SO A FOUR-TICKET SALE CHASES THREE HOLDERS WITHOUT
// MAILING ANY OF THEM TWICE — the acceptance criterion that forced the ledger's
// unit to move (migration 083).
//
// A PER-SALE ALLOWANCE COULD NOT DO THIS AT ALL, and that is the point rather
// than a nicety: the first Holder mailed would have spent the whole Sale's two,
// and the buyer plus the second Holder would have gone unwritten-to forever.
// The failure would have looked like a working feature.
func TestAnswerRemindersAreRationedPerTicketAcrossAFourTicketSale(t *testing.T) {
	env := setupTest(t)
	f := newReminderFixture(t, env)

	// A second buyer on the same Event with FOUR Tickets, which the two-Ticket
	// fixture cannot express.
	begun := beginCheckoutOK(t, env, "test-org", "answer-fest-reminders",
		checkoutBody("fio@example.com", "Fio", "Ruiz", cartLine(f.ticketTypeID, 4)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	fioSaleID := saleIDOfPayment(t, env, begun.ClientTransactionID)
	fioTickets := ticketIDsOfSale(t, env, fioSaleID)
	if len(fioTickets) != 4 {
		t.Fatalf("Fio's Sale has %d Tickets, want 4", len(fioTickets))
	}
	fio := customerSignIn(t, env, "fio@example.com")
	sharedEmail.Reset()

	acceptTicketAs(t, env, fio, fioSaleID, fioTickets[1], "gabi@example.com")
	acceptTicketAs(t, env, fio, fioSaleID, fioTickets[2], "hugo@example.com")

	result := sweepAnswerReminders(t, env)

	// FOUR MESSAGES IN ALL: Ana about her own Ticket, Fio about hers, Gabi,
	// Hugo. Three of the four people involved are Holders on Fio's sale, and
	// NOT ONE of them is written to twice — and Fio's fourth Ticket, which
	// nobody holds, is chased by nobody.
	if result.Sent != 4 {
		t.Fatalf("sweep = %+v, want 4 mails: four Holders, each written to once", result)
	}
	byAddress := map[string]int{}
	for _, mail := range sharedEmail.HolderAnswerRemindersSent() {
		byAddress[mail.To]++
	}
	want := map[string]int{
		"ana@example.com":  1,
		"fio@example.com":  1,
		"gabi@example.com": 1,
		"hugo@example.com": 1,
	}
	for address, wantCount := range want {
		if byAddress[address] != wantCount {
			t.Errorf("%s received %d reminders, want %d — per-Ticket rationing chases several people without mailing any of them twice", address, byAddress[address], wantCount)
		}
	}
	if len(byAddress) != len(want) {
		t.Errorf("reminders reached %d addresses, want %d: %v", len(byAddress), len(want), byAddress)
	}

	// Fio's held Tickets each carry one row — her own, Gabi's, Hugo's — and the
	// unassigned fourth carries none.
	wantRows := []int{1, 1, 1, 0}
	for i, ticketID := range fioTickets {
		if got := remindersForTicket(t, env, ticketID); got != wantRows[i] {
			t.Errorf("Fio's Ticket #%d has %d ledger rows, want %d", i+1, got, wantRows[i])
		}
	}
}

// WRITTEN IN THE RECIPIENT'S MAIL LOCALE, AND THE CHAIN IS READ FROM THE
// RECIPIENT'S END.
//
// This is #325's inversion applied to this mail (ADR 0033, ADR 0046). Every
// other message about a sale reads the Sale Locale first, because it is
// addressed to the person who made the sale in the language they were reading
// when they made it. A HOLDER IS NOT THAT PERSON: they bought nothing, were
// never on that page, and a Spanish-speaking friend of an English-speaking buyer
// is exactly the case the feature exists to serve. Since ADR 0049 there is one
// reminder and it reads recipient-first for everybody, the buyer's own
// Self-held Ticket included — for whom the two agree in every ordinary case,
// and whose own record is the honest answer when they do not.
func TestTheAnswerReminderReadsTheLocaleChainRecipientFirst(t *testing.T) {
	env := setupTest(t)
	f := newReminderFixture(t, env)
	acceptTicketAs(t, env, f.ana, f.saleID, f.otherID, "carla@example.com")

	// The buyer paid on the ENGLISH storefront...
	if _, err := env.db.Exec(`UPDATE ticket_sales SET locale = 'en' WHERE id = $1`, f.saleID); err != nil {
		t.Fatalf("set the Sale Locale: %v", err)
	}
	// ...the Holder's record remembers SPANISH, and the buyer's ENGLISH.
	if _, err := env.db.Exec(
		`UPDATE customers SET mail_locale = 'es' WHERE email = 'carla@example.com'`,
	); err != nil {
		t.Fatalf("remember the Holder's language: %v", err)
	}
	if _, err := env.db.Exec(
		`UPDATE customers SET mail_locale = 'en' WHERE email = 'ana@example.com'`,
	); err != nil {
		t.Fatalf("remember the buyer's language: %v", err)
	}

	sweepAnswerReminders(t, env)

	carla := reminderFor(t, "carla@example.com")
	if carla.Locale != platform.LocaleES {
		t.Fatalf("the Holder's reminder is in %q, want es: they are not party to the sale, so their own Mail Locale outranks it (#325, ADR 0046)", carla.Locale)
	}
	if !strings.Contains(carla.Text(), "aún necesita respuesta") {
		t.Fatalf("the Holder's reminder body = %q, want the Spanish message", carla.Text())
	}
	// The buyer's record and their sale agree, as they do in every ordinary
	// case, and the one mail is in that language.
	ana := reminderFor(t, "ana@example.com")
	if ana.Locale != platform.LocaleEN {
		t.Errorf("the buyer's reminder is in %q, want en", ana.Locale)
	}
}

// TRANSACTIONAL, NOT MARKETING, AND THIS IS THE READER IT MATTERS MOST FOR. A
// Holder accepted a ticket and opted into NOTHING — accepting grants no consent
// of any kind (ADR 0046) — so a mail about the ticket they hold must not be
// gated behind a marketing switch they never touched. It arrives on the
// transactional sender, which is what keeps it off the marketing identity
// entirely (ADR 0030, ADR 0034).
func TestTheHoldersAnswerReminderIsSentRegardlessOfMarketingConsent(t *testing.T) {
	env := setupTest(t)
	f := newReminderFixture(t, env)
	acceptTicketAs(t, env, f.ana, f.saleID, f.otherID, "carla@example.com")

	var digestEnabled bool
	var consents int
	if err := env.db.QueryRow(
		`SELECT digest_enabled FROM customers WHERE email = 'carla@example.com'`,
	).Scan(&digestEnabled); err != nil {
		t.Fatalf("read the Holder's marketing state: %v", err)
	}
	if err := env.db.QueryRow(
		`SELECT COUNT(*) FROM consent_records cr JOIN customers c ON c.id = cr.customer_id WHERE c.email = 'carla@example.com'`,
	).Scan(&consents); err != nil {
		t.Fatalf("read the Holder's Consent Records: %v", err)
	}
	if digestEnabled || consents != 0 {
		t.Fatalf("the Holder has marketing state (digest_enabled=%v, consent records=%d): accepting a ticket must grant nothing, and this test only means something against somebody who granted nothing", digestEnabled, consents)
	}

	sweepAnswerReminders(t, env)
	reminderFor(t, "carla@example.com")
}

// SILENT ONCE THE EVENT HAS STARTED, for a named Holder exactly as for the
// buyer. Every route into an Answer is frozen by then — the held-ticket panel
// this mail would point at stops accepting edits at the doors — so a reminder
// would be an instruction its reader cannot follow.
func TestTheHoldersAnswerReminderIsSilentOnceTheEventHasStarted(t *testing.T) {
	env := setupTest(t)
	f := newReminderFixture(t, env)
	acceptTicketAs(t, env, f.ana, f.saleID, f.otherID, "carla@example.com")

	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		f.eventID, env.fixedClock.Add(-time.Hour)); err != nil {
		t.Fatalf("open the doors: %v", err)
	}

	result := sweepAnswerReminders(t, env)
	if result.Sent != 0 || result.Due != 0 || result.DueTotal != 0 {
		t.Fatalf("sweep = %+v, want silence once the Event has started, for every Holder alike", result)
	}
	assertNoReminders(t, env, f.saleID, "somebody was chased after the doors opened")
}
