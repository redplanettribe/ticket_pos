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

// The Answer Reminder (#317, ADR 0044; #328, parent #322, ADR 0046): the mail
// telling somebody that a Ticket still owes an Answer, and pointing them at the
// surface where they can give it.
//
// IT CHASES THE PERSON WHO ACTUALLY KNOWS THE ANSWER, which is what #328 changed
// and the only thing it changed. ADR 0044 addressed this mail to the buyer
// "because there is nobody else to address"; ADR 0046 gave a Ticket a Holder who
// accepts by mail, so there is now — and chasing the buyer for a Ticket somebody
// else accepted nags the one person this whole feature established does not know
// their friends' t-shirt sizes. The Holder for an `accepted` Ticket, the buyer
// for every other Ticket on the Sale, and a Sale with a mix produces both.
//
// THE RATIONING IS PER TICKET SINCE #328, and the two facts it is easiest to
// confuse are worth separating before reading anything below. A LEDGER ROW IS A
// TICKET CHASED, never a message sent: one mail to a buyer about four Tickets
// writes four rows. So remindersFor (a whole Sale) counts Tickets and grows with
// the size of the sale, while remindersForTicket is what the cap of two is
// actually about.
//
// THIS FILE IS WHERE THE RATIONING MEETS REAL ROWS. The rule is stated in Go
// (catalog.MayRemind, unit-tested clause by clause) and again in SQL
// (repository.answerReminderRation), because the sweep has to be able to skip
// what it may not mail inside the database. Neither may be changed alone, and
// these tests are what notices: every one of them drives the real endpoint
// against a real Postgres and asserts on the mail that came out the other end.
//
// TIME MOVES BY MOVING THE CLOCK, never by sleeping and never by backdating a
// ledger row: `sent_at` is written from the catalog service's clock, so moving
// that clock forward ages a reminder exactly as the calendar would. A test that
// reached into SQL to age a send would be asserting against its own UPDATE.
//
// WHO IS MAILED IS THE ASSERTION, so most of these count messages rather than
// reading them — and since #328 the assertion is as much about WHICH captured
// list a message landed in. "The buyer was not chased about a Ticket somebody
// else accepted" is a fact about the buyer's list being short, and no field of
// any message can state it. What one says is pinned in
// internal/platform/email_answerreminder_test.go, where the rendered words are.

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
// One reminder to the buyer of a two-Ticket sale writes TWO rows here, because
// the allowance it spends is each Ticket's. How many messages went out is what
// the captured sender says, and the two figures are deliberately different
// things.
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

// reminderFixture is an upcoming Event with one required Ticket Question and one
// imported Ticket Sale of two Tickets whose buyer has answered nothing.
//
// AN IMPORTED SALE, deliberately. Its Tickets start out owing everything because
// nobody ever put the questions to that buyer — there is no checkout form on a
// spreadsheet import — which is the honest state of the debt and the case this
// mail exists for. It also gives the sale no Sale Locale, so the language falls
// through to the recipient's own record, which is the second step of ADR 0033's
// chain and the one this mail will usually take.
func reminderFixture(t *testing.T, env *testEnv) (sessionID, eventID, ticketTypeID, saleID, batchID, questionID string) {
	t.Helper()
	sessionID = orgAdminSession(t, env)
	eventID = createDraftEvent(t, env, sessionID, "Answer Fest", "answer-fest-reminders")
	ticketTypeID = createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2000, 50)

	// The doors open in thirty days. The reminder is silent once an Event has
	// started, so every test here needs a start and needs it to be ahead.
	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		eventID, env.fixedClock.Add(30*24*time.Hour)); err != nil {
		t.Fatalf("set the Event's start: %v", err)
	}

	batchID = commitBatch(t, env, sessionID, eventID, "batch-reminders", []map[string]any{{
		"customer_email":      "ana@example.com",
		"customer_first_name": "Ana",
		"customer_last_name":  "Lopez",
		"ticket_type_id":      ticketTypeID,
		"quantity":            2,
		"payment_method":      "cash",
		"sold_at":             "2026-07-01T10:00:00Z",
	}})
	if err := env.db.QueryRow(
		`SELECT id FROM ticket_sales WHERE event_id = $1`, eventID,
	).Scan(&saleID); err != nil {
		t.Fatalf("read Ticket Sale: %v", err)
	}

	questionID = createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	}).ID

	// The import's own Sale Confirmation already went out, carrying the receipt's
	// one Outstanding Answers sentence. Clearing the mailbox here means every
	// assertion below is about reminders and nothing else.
	sharedEmail.Reset()
	return sessionID, eventID, ticketTypeID, saleID, batchID, questionID
}

// The mail #317 was about, unchanged by #328 because nothing on this Sale has
// been accepted: a buyer whose Tickets owe a required Answer is written to,
// once, and pointed at their own sale.
//
// THE TWO TICKETS ARE ONE MESSAGE AND TWO LEDGER ROWS, which is the shape
// per-Ticket rationing takes for a buyer. It is what keeps the promise that
// moving the unit down did not make this mail louder for the person it was
// always addressed to.
func TestAnswerReminderMailsTheBuyerOfASaleOwingAnswers(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	_, _, _, saleID, _, _ := reminderFixture(t, env)

	result := sweepAnswerReminders(t, env)
	if result.Sent != 1 || result.Due != 1 {
		t.Fatalf("sweep = %+v, want one Sale due and one mail sent", result)
	}

	reminders := sharedEmail.AnswerRemindersSent()
	if len(reminders) != 1 {
		t.Fatalf("reminders sent = %d, want 1", len(reminders))
	}
	sent := reminders[0]
	if sent.To != "ana@example.com" {
		t.Fatalf("reminder addressed to %q, want the BUYER — there is no other address on this platform to write to", sent.To)
	}
	if sent.EventName != "Answer Fest" {
		t.Fatalf("reminder names event %q, want Answer Fest", sent.EventName)
	}
	if sent.ConfirmationLink == "" {
		t.Fatal("the reminder carries no Confirmation Link: the link is the whole message, and one without it is an instruction its reader cannot follow")
	}
	// The link opens the SALE, which is the page the buyer answers on and copies
	// the per-Ticket Answer Links from. An Answer Link in this mail would make
	// forwarding a t-shirt question the same gesture as forwarding a receipt.
	if !strings.Contains(sent.ConfirmationLink, "confirm") {
		t.Fatalf("reminder link = %q, want the Confirmation Link to the sale's own page", sent.ConfirmationLink)
	}
	// TWO rows for ONE mail: the Sale has two Tickets, both still the buyer's to
	// chase, and each spends its own allowance on the message that covered it.
	if got := remindersFor(t, env, saleID); got != 2 {
		t.Fatalf("ledger rows across the Sale = %d, want 2 — one per Ticket the single mail covered, because the send is only rationed if it was written down", got)
	}
	if len(sharedEmail.HolderAnswerRemindersSent()) != 0 {
		t.Fatal("a Holder was chased on a Sale where nobody has accepted anything: an `assigned` address, let alone an absent one, is still the buyer's to chase")
	}
}

// Nothing is sent while the feature is dark, however often the endpoint is
// called (ADR 0045). This is the inner of the two switches the job ships behind
// — the outer one is the Cloud Scheduler job, created paused — and it is the one
// a curl can get past.
func TestAnswerReminderSendsNothingWhileTheFlagIsOff(t *testing.T) {
	env := setupTest(t)
	// The flag is opened only to author the question, which is the state a
	// deployment that had collected Answers and then closed the flag would be in.
	enableTicketQuestions(t)
	_, _, _, saleID, _, _ := reminderFixture(t, env)
	sharedApp.CatalogService.WithTicketQuestions(false)
	sharedApp.SalesService.WithTicketQuestions(false)

	result := sweepAnswerReminders(t, env)
	if result.Sent != 0 || result.Due != 0 {
		t.Fatalf("sweep = %+v, want a dark deployment to find nobody and mail nobody", result)
	}
	if len(sharedEmail.AnswerRemindersSent()) != 0 {
		t.Fatal("a reminder went out while Ticket Questions were dark: this mail is entirely about a collection the Privacy Policy does not yet describe")
	}
	if got := remindersFor(t, env, saleID); got != 0 {
		t.Fatalf("ledger rows = %d, want 0", got)
	}
}

// AT MOST ONE PER TICKET PER 7 DAYS, which is the rule that makes this a swept
// job rather than something the question editor triggers. Both of this Sale's
// Tickets are the buyer's, so in practice the buyer sees one mail a week — the
// per-Sale behaviour #317 shipped, arrived at through a per-Ticket cap.
//
// The second sweep here stands in for an Organization authoring a second
// question ten minutes later: the sweep sees state rather than events, so
// nothing about a new question buys a new mail.
func TestAnswerReminderIsNotSentTwiceInsideTheWeek(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, saleID, _, _ := reminderFixture(t, env)

	sweepAnswerReminders(t, env)

	// A second question, authored minutes later, owed by the same two Tickets.
	createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "Dietary requirements", "kind": "short_text", "required": true,
	})
	second := sweepAnswerReminders(t, env)
	if second.Sent != 0 || second.Due != 0 {
		t.Fatalf("second sweep = %+v, want silence: an Organization drafting questions must not mail the same buyer twice", second)
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
	if got := len(sharedEmail.AnswerRemindersSent()); got != 2 {
		t.Fatalf("reminders sent = %d, want 2 across the whole week", got)
	}
	// Two mails covering two Tickets each: four rows, and two per Ticket.
	if got := remindersFor(t, env, saleID); got != 4 {
		t.Fatalf("ledger rows across the Sale = %d, want 4 — two mails, each covering both Tickets", got)
	}
}

// AT MOST TWO EVER, and no passage of time buys a third. This is the clause with
// no way back: the mail is transactional and carries no unsubscribe, so the cap
// is the only thing standing between a buyer and an unbounded chase.
func TestAnswerReminderStopsForeverAfterTwo(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	_, _, _, saleID, _, _ := reminderFixture(t, env)

	// The Event is far enough out that the silence-after-start rule never fires
	// inside the weeks this test walks.
	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = (SELECT event_id FROM ticket_sales WHERE id = $1)`,
		saleID, env.fixedClock.Add(120*24*time.Hour)); err != nil {
		t.Fatalf("push the Event's start out: %v", err)
	}

	for week := 0; week < 6; week++ {
		moveClockTo(t, env.fixedClock.Add(time.Duration(week)*catalog.AnswerReminderInterval))
		sweepAnswerReminders(t, env)
	}

	if got := len(sharedEmail.AnswerRemindersSent()); got != catalog.MaxAnswerReminders {
		t.Fatalf("reminders sent over six weeks = %d, want %d — the cap is per Ticket and for its whole life, and this buyer's two Tickets are chased together", got, catalog.MaxAnswerReminders)
	}
	// EVERY TICKET, and not merely the Sale between them. A per-Sale total of
	// four could also be one Ticket chased four times, which is the failure this
	// assertion exists to tell apart.
	for _, ticketID := range ticketIDsOfSale(t, env, saleID) {
		if got := remindersForTicket(t, env, ticketID); got != catalog.MaxAnswerReminders {
			t.Fatalf("Ticket %s has %d ledger rows, want %d — no passage of time buys a third", ticketID, got, catalog.MaxAnswerReminders)
		}
	}
}

// SILENT ONCE THE EVENT HAS STARTED. The debt itself survives the doors opening
// — the Organization's chase list still shows it, deliberately, because "twelve
// people never told us their size" is what somebody reviewing the event came to
// find out — but every route into an Answer is frozen by then, so a reminder
// would be an instruction its reader cannot follow.
func TestAnswerReminderIsSilentOnceTheEventHasStarted(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, _, saleID, _, _ := reminderFixture(t, env)

	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		eventID, env.fixedClock.Add(-time.Hour)); err != nil {
		t.Fatalf("open the doors: %v", err)
	}

	result := sweepAnswerReminders(t, env)
	if result.Sent != 0 || result.Due != 0 || result.DueTotal != 0 {
		t.Fatalf("sweep = %+v, want silence once the Event has started", result)
	}
	if got := remindersFor(t, env, saleID); got != 0 {
		t.Fatalf("ledger rows = %d, want 0", got)
	}

	// The debt is untouched by the silence: #313's list still reports it, which is
	// the property that would break if the rationing had been pushed down into the
	// derivation.
	page := listOutstanding(t, env, sessionID, eventID)
	if page.OutstandingCount == 0 {
		t.Fatal("the Outstanding Answers list emptied when the Event started: the silence belongs to the mail, never to the debt")
	}
}

// A REVERSED SALE IS NEVER CHASED. Its tickets have ceased to exist and its
// money has gone back, so there is nobody to write to about a shirt nobody is
// coming to collect.
func TestAnswerReminderIsNeverSentForAReversedSale(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, _, saleID, batchID, _ := reminderFixture(t, env)

	undoBatch(t, env, sessionID, eventID, batchID)
	sharedEmail.Reset()

	result := sweepAnswerReminders(t, env)
	if result.Sent != 0 || result.Due != 0 || result.DueTotal != 0 {
		t.Fatalf("sweep = %+v, want silence for a reversed Sale", result)
	}
	if len(sharedEmail.AnswerRemindersSent()) != 0 {
		t.Fatal("a reversed Sale's buyer was chased about Answers for tickets they no longer hold")
	}
	if got := remindersFor(t, env, saleID); got != 0 {
		t.Fatalf("ledger rows = %d, want 0", got)
	}
}

// TRANSACTIONAL, NOT MARKETING. The buyer of an imported sale is an unverified
// Customer who has granted nothing: no Marketing Consent, no Follow Digest. The
// reminder arrives anyway, on the same footing as the Sale Confirmation that
// carried the same sentence — and it arrives on the TRANSACTIONAL sender, which
// is what stops it ever leaving the marketing domain (ADR 0030, ADR 0034).
func TestAnswerReminderIsSentRegardlessOfMarketingConsent(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	reminderFixture(t, env)

	var digestEnabled bool
	var consents int
	if err := env.db.QueryRow(
		`SELECT digest_enabled FROM customers WHERE email = 'ana@example.com'`,
	).Scan(&digestEnabled); err != nil {
		t.Fatalf("read the buyer's marketing state: %v", err)
	}
	if err := env.db.QueryRow(
		`SELECT COUNT(*) FROM consent_records cr JOIN customers c ON c.id = cr.customer_id WHERE c.email = 'ana@example.com'`,
	).Scan(&consents); err != nil {
		t.Fatalf("read the buyer's Consent Records: %v", err)
	}
	if digestEnabled || consents != 0 {
		t.Fatalf("the fixture's buyer has marketing state (digest_enabled=%v, consent records=%d): this test only means something against somebody who granted nothing", digestEnabled, consents)
	}

	if result := sweepAnswerReminders(t, env); result.Sent != 1 {
		t.Fatalf("sweep = %+v, want the reminder to reach a buyer who granted no Marketing Consent", result)
	}
}

// WRITTEN IN THE RECIPIENT'S MAIL LOCALE. An imported sale recorded no Sale
// Locale, so the chain falls through to what the Customer's own record
// remembers (ADR 0033) — which is exactly the step this mail will usually take,
// being sent weeks later by a job with no page anywhere near it.
func TestAnswerReminderIsWrittenInTheRecipientsMailLocale(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	reminderFixture(t, env)

	// The buyer signed in on the Spanish Storefront at some point, which is the
	// only thing that writes this column.
	if _, err := env.db.Exec(
		`UPDATE customers SET mail_locale = 'es' WHERE email = 'ana@example.com'`,
	); err != nil {
		t.Fatalf("remember the buyer's language: %v", err)
	}

	sweepAnswerReminders(t, env)
	reminders := sharedEmail.AnswerRemindersSent()
	if len(reminders) != 1 {
		t.Fatalf("reminders sent = %d, want 1", len(reminders))
	}
	if reminders[0].Locale != platform.LocaleES {
		t.Fatalf("reminder locale = %q, want es", reminders[0].Locale)
	}
	if !strings.Contains(reminders[0].Text(), "aún necesitan respuestas") {
		t.Fatalf("reminder body = %q, want the Spanish message", reminders[0].Text())
	}
}

// Nothing owed, nothing said. Once every Ticket has answered every required
// question the Sale leaves the sweep entirely — which is also what makes the
// reminder stop early for the buyer who acts on the first one.
func TestAnswerReminderStopsOnceTheAnswersArrive(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, _, saleID, _, questionID := reminderFixture(t, env)

	sweepAnswerReminders(t, env)
	if got := len(sharedEmail.AnswerRemindersSent()); got != 1 {
		t.Fatalf("reminders sent = %d, want the first one", got)
	}

	// Both Tickets answer the one required question.
	rows, err := env.db.Query(`
		SELECT tk.id
		FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		WHERE l.ticket_sale_id = $1
	`, saleID)
	if err != nil {
		t.Fatalf("read Tickets: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ticketID string
		if err := rows.Scan(&ticketID); err != nil {
			t.Fatalf("scan Ticket: %v", err)
		}
		putAnswer(t, env, sessionID, eventID, ticketID, questionID, map[string]any{"text": "M"})
	}

	// A week later the second reminder would have been due, and there is nothing
	// left to remind anybody about.
	moveClockTo(t, env.fixedClock.Add(catalog.AnswerReminderInterval))
	result := sweepAnswerReminders(t, env)
	if result.Sent != 0 || result.Due != 0 || result.DueTotal != 0 {
		t.Fatalf("sweep = %+v, want silence once the debt is discharged", result)
	}
}

// The sweep is safe to curl by hand, repeatedly, which is how this ships: the
// runbook before the automation. A second call inside the cooldown mails nobody,
// because the ledger row was written before the first response was.
func TestAnswerReminderSweepIsSafeToRunByHandTwice(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	_, _, _, saleID, _, _ := reminderFixture(t, env)

	first := sweepAnswerReminders(t, env)
	second := sweepAnswerReminders(t, env)

	if first.Sent != 1 || second.Sent != 0 {
		t.Fatalf("first = %+v, second = %+v, want one mail and then silence", first, second)
	}
	if second.DueTotal != 0 {
		t.Fatalf("second sweep reports %d still due, want 0 — the backlog is what an operator reads two runs apart", second.DueTotal)
	}
	if got := remindersFor(t, env, saleID); got != 2 {
		t.Fatalf("ledger rows = %d, want 2 — one mail covering both Tickets, written down before the response was", got)
	}
}

// ---------------------------------------------------------------------------
// The Holder's half (#328, parent #322, ADR 0046).
//
// Everything above is #317's mail, still working: nothing on those Sales has
// been accepted, so the buyer is still the only person who can answer. What
// follows is the case ADR 0044 said could not exist.
// ---------------------------------------------------------------------------

// holderReminderFixture is the reminder fixture with Ticket Assignment open and
// the buyer signed in, so a test can hand a Ticket to somebody and have them
// accept it.
//
// ITS MAILBOX IS EMPTY WHEN IT RETURNS. Signing in sends a passcode and
// assigning sends an Assignment mail, and both would otherwise sit in the
// capture beside the reminders these tests count. The reminder assertions are
// about lengths, so a stray message is not a nuisance — it is a wrong answer.
type holderReminderFixture struct {
	staffSession string
	eventID      string
	ticketTypeID string
	saleID       string
	questionID   string
	// ana is the buyer's Customer Session: assignment happens from a buyer
	// surface and nowhere else.
	ana string
	// ticketIDs are the Sale's two Tickets, in ordinal order.
	ticketIDs []string
}

func newHolderReminderFixture(t *testing.T, env *testEnv) holderReminderFixture {
	t.Helper()
	// BOTH FLAGS, EXPLICITLY AND SEPARATELY. Ticket Questions is what makes the
	// debt exist at all; Ticket Assignment is what makes a Holder reachable. They
	// are two deployment switches (ADR 0046) and the suite may never treat them
	// as one — TestTheAnswerReminderFallsBackToTheBuyerWhileTicketAssignmentIsDark
	// closes the second and keeps the first open, which is the whole point of
	// their being separate.
	enableTicketQuestions(t)
	staffSession, eventID, ticketTypeID, saleID, _, questionID := reminderFixture(t, env)
	enableTicketAssignment(t)

	f := holderReminderFixture{
		staffSession: staffSession,
		eventID:      eventID,
		ticketTypeID: ticketTypeID,
		saleID:       saleID,
		questionID:   questionID,
		ticketIDs:    ticketIDsOfSale(t, env, saleID),
	}
	if len(f.ticketIDs) != 2 {
		t.Fatalf("the fixture's Sale has %d Tickets, want 2", len(f.ticketIDs))
	}
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

// holderReminderFor finds the one reminder sent to an address, failing if there
// is not exactly one. The count is half the assertion in most of these tests.
func holderReminderFor(t *testing.T, address string) platform.HolderAnswerReminder {
	t.Helper()
	var found []platform.HolderAnswerReminder
	for _, mail := range sharedEmail.HolderAnswerRemindersSent() {
		if mail.To == address {
			found = append(found, mail)
		}
	}
	if len(found) != 1 {
		t.Fatalf("Holder Answer Reminders to %s = %d, want exactly 1", address, len(found))
	}
	return found[0]
}

// THE MAIL THIS TICKET IS ABOUT: an `accepted` Ticket is chased through its
// HOLDER, and the buyer is not written to at all.
//
// This is the whole of #328 in one test. Ana bought two tickets and handed both
// away; both were accepted; she knows neither t-shirt size and is no longer
// asked for them.
func TestAnswerReminderChasesTheHolderOfAnAcceptedTicket(t *testing.T) {
	env := setupTest(t)
	f := newHolderReminderFixture(t, env)

	acceptTicketAs(t, env, f.ana, f.saleID, f.ticketIDs[0], "carla@example.com")
	acceptTicketAs(t, env, f.ana, f.saleID, f.ticketIDs[1], "diego@example.com")

	result := sweepAnswerReminders(t, env)
	if result.Due != 2 || result.Sent != 2 {
		t.Fatalf("sweep = %+v, want two mails due and two sent — one per accepted Ticket", result)
	}

	// NOT THE BUYER. She has nothing left to chase, and this is the assertion the
	// whole ticket exists for: the mail that used to arrive here was the platform
	// nagging the one person who does not know the answer.
	if got := len(sharedEmail.AnswerRemindersSent()); got != 0 {
		t.Fatalf("the buyer received %d reminders about Tickets somebody else accepted, want 0", got)
	}

	carla := holderReminderFor(t, "carla@example.com")
	holderReminderFor(t, "diego@example.com")

	if len(carla.Tickets) != 1 {
		t.Fatalf("the Holder's reminder lists %d tickets, want exactly the one she accepted", len(carla.Tickets))
	}
	if carla.Tickets[0].EventName != "Answer Fest" || carla.Tickets[0].TicketTypeName != "GA" {
		t.Errorf("the Holder's reminder names event=%q ticket type=%q, want Answer Fest / GA", carla.Tickets[0].EventName, carla.Tickets[0].TicketTypeName)
	}
	// THE LINK IS THE MESSAGE, and it is the Holder's OWN Assignment Link — the
	// surface where they answer their own questions (#326) — never the buyer's
	// Confirmation Link, which opens a whole purchase.
	if carla.Tickets[0].AnswerURL == "" {
		t.Fatal("the Holder's reminder carries no link: a reminder with nothing to open is an instruction its reader cannot follow")
	}
	if !strings.Contains(carla.Tickets[0].AnswerURL, "/accept?token=") {
		t.Fatalf("the Holder's reminder points at %q, want the Storefront's /accept page with a token", carla.Tickets[0].AnswerURL)
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
	for _, forbidden := range []string{"Ana", "Lopez", "ana@example.com", ref, "diego@example.com"} {
		if strings.Contains(rendered, forbidden) {
			t.Errorf("the Holder's Answer Reminder contains %q.\n"+
				"It names the Event, the Ticket Type and their own link and nothing else (ADR 0044, ADR 0046).", forbidden)
		}
	}

	// One ledger row per Ticket, spent by the mail that covered it.
	for _, ticketID := range f.ticketIDs {
		if got := remindersForTicket(t, env, ticketID); got != 1 {
			t.Errorf("Ticket %s has %d ledger rows, want 1", ticketID, got)
		}
	}
}

// ONE MAIL PER HOLDER PER SWEEP (#335). A Holder who accepted BOTH Tickets of
// one Sale gets ONE Answer Reminder listing each owed Ticket with its own
// Assignment Link — never two envelopes in one sweep, which is Story 48's own
// clause ("chase two of us without mailing either twice") and the shape spam
// filters punish. The buyer's side always fanned a Sale's Tickets into one
// message; this is the Holder's side brought level with it.
//
// PER-TICKET RATIONING IS UNCHANGED: each listed Ticket burns its own
// allowance, so the one mail writes two ledger rows. Only the envelope is
// shared.
func TestAnswerReminderMailsAHolderOnceAboutBothTheirTickets(t *testing.T) {
	env := setupTest(t)
	f := newHolderReminderFixture(t, env)

	// Carla accepts BOTH of Ana's Tickets.
	acceptTicketAs(t, env, f.ana, f.saleID, f.ticketIDs[0], "carla@example.com")
	acceptTicketAs(t, env, f.ana, f.saleID, f.ticketIDs[1], "carla@example.com")

	// The backlog agrees with the fan-in BEFORE anything is sent: two owed
	// Tickets, one Holder, ONE mail due — the count an operator compares
	// against `sent` must count what will actually go out.
	if backlog, err := sharedApp.CatalogService.CountAnswerRemindersDue(context.Background()); err != nil {
		t.Fatalf("count the backlog: %v", err)
	} else if backlog != 1 {
		t.Fatalf("backlog = %d, want 1: two Tickets accepted by one Holder are one mail (#335)", backlog)
	}

	result := sweepAnswerReminders(t, env)
	if result.Due != 1 || result.Sent != 1 {
		t.Fatalf("sweep = %+v, want ONE mail due and ONE sent: one Holder, one envelope, however many Tickets (#335)", result)
	}

	// NOT THE BUYER, and not a second envelope to Carla either.
	if got := len(sharedEmail.AnswerRemindersSent()); got != 0 {
		t.Fatalf("the buyer received %d reminders about Tickets somebody else accepted, want 0", got)
	}
	carla := holderReminderFor(t, "carla@example.com")

	// EACH OWED TICKET IS LISTED WITH ITS OWN ASSIGNMENT LINK. An Assignment
	// Link opens exactly one Ticket, so a mail about two carries two distinct
	// links — sharing one would leave a Ticket unanswerable, and a third URL
	// has no business beside a credential that mints an identity.
	if len(carla.Tickets) != 2 {
		t.Fatalf("the Holder's one reminder lists %d tickets, want both of the ones she accepted", len(carla.Tickets))
	}
	links := map[string]bool{}
	for _, ticket := range carla.Tickets {
		if ticket.EventName != "Answer Fest" || ticket.TicketTypeName != "GA" {
			t.Errorf("a listed ticket names event=%q type=%q, want Answer Fest / GA", ticket.EventName, ticket.TicketTypeName)
		}
		if !strings.Contains(ticket.AnswerURL, "/accept?token=") {
			t.Fatalf("a listed ticket points at %q, want its own Assignment Link", ticket.AnswerURL)
		}
		links[ticket.AnswerURL] = true
	}
	if len(links) != 2 {
		t.Fatalf("the two listed tickets share an Assignment Link: each link opens exactly one Ticket, so each must carry its own")
	}
	if got := strings.Count(carla.Text(), "/accept?token="); got != 2 {
		t.Fatalf("the rendered mail carries %d Assignment Links, want exactly 2 — one per listed Ticket", got)
	}

	// ONE ENVELOPE, TWO ALLOWANCES: the caps stayed per Ticket, so the single
	// mail spends one ledger row for each Ticket it covered.
	for _, ticketID := range f.ticketIDs {
		if got := remindersForTicket(t, env, ticketID); got != 1 {
			t.Errorf("Ticket %s has %d ledger rows, want 1: each listed Ticket burns its own allowance", ticketID, got)
		}
	}

	// And the second sweep is silent: both Tickets are inside their week.
	if second := sweepAnswerReminders(t, env); second.Sent != 0 {
		t.Fatalf("second sweep = %+v, want silence — the envelope was shared, the cooldown was not lifted", second)
	}
}

// A SALE WITH A MIX PRODUCES BOTH MAILS, and neither is about the other's
// Tickets. One to the buyer covering what is still theirs to chase, one to the
// Holder about their own — never one message listing everything to everybody,
// which would tell a Holder how many tickets the buyer bought and tell the buyer
// to chase somebody who has already been asked.
func TestAnswerReminderOnAMixedSaleWritesToTheBuyerAndTheHolderSeparately(t *testing.T) {
	env := setupTest(t)
	f := newHolderReminderFixture(t, env)

	// One of the two accepted; the other never assigned at all.
	acceptTicketAs(t, env, f.ana, f.saleID, f.ticketIDs[0], "carla@example.com")

	result := sweepAnswerReminders(t, env)
	if result.Due != 2 || result.Sent != 2 {
		t.Fatalf("sweep = %+v, want two mails: one to the buyer and one to the Holder", result)
	}

	buyerMails := sharedEmail.AnswerRemindersSent()
	if len(buyerMails) != 1 {
		t.Fatalf("buyer reminders = %d, want exactly 1 covering the Ticket still hers to chase", len(buyerMails))
	}
	if buyerMails[0].To != "ana@example.com" {
		t.Fatalf("the buyer's reminder went to %q", buyerMails[0].To)
	}
	holderReminderFor(t, "carla@example.com")

	// THE LEDGER IS WHERE "COVERING ONLY THE TICKETS STILL THEIRS" IS PROVABLE.
	// Each Ticket has exactly one row, and no Ticket has two — which is what a
	// buyer's mail that had swept up the accepted Ticket as well would leave
	// behind, since the Holder's mail spent that Ticket's allowance too.
	for _, ticketID := range f.ticketIDs {
		if got := remindersForTicket(t, env, ticketID); got != 1 {
			t.Errorf("Ticket %s has %d ledger rows, want exactly 1: each Ticket is covered by one mail and one only", ticketID, got)
		}
	}

	// And the second sweep is silent for both of them, on the same seven days.
	if second := sweepAnswerReminders(t, env); second.Sent != 0 {
		t.Fatalf("second sweep = %+v, want silence for buyer and Holder alike", second)
	}
}

// RATIONED PER TICKET, SO A FOUR-TICKET SALE CHASES TWO HOLDERS WITHOUT MAILING
// EITHER TWICE — the acceptance criterion that forced the ledger's unit to move
// (migration 083).
//
// A PER-SALE ALLOWANCE COULD NOT DO THIS AT ALL, and that is the point rather
// than a nicety: the first Holder mailed would have spent the whole Sale's two,
// and the second Holder plus the buyer's two remaining Tickets would have gone
// unwritten-to forever. The failure would have looked like a working feature.
func TestAnswerRemindersAreRationedPerTicketAcrossAFourTicketSale(t *testing.T) {
	env := setupTest(t)
	f := newHolderReminderFixture(t, env)

	// A second buyer on the same Event with FOUR Tickets, which the two-Ticket
	// fixture cannot express. Same import channel, so its Tickets start out
	// owing everything.
	commitBatch(t, env, f.staffSession, f.eventID, "batch-four", []map[string]any{{
		"customer_email":      "fio@example.com",
		"customer_first_name": "Fio",
		"customer_last_name":  "Ruiz",
		"ticket_type_id":      f.ticketTypeID,
		"quantity":            4,
		"payment_method":      "cash",
		"sold_at":             "2026-07-03T10:00:00Z",
	}})
	var fioSaleID string
	if err := env.db.QueryRow(
		`SELECT id FROM ticket_sales WHERE customer_email = 'fio@example.com'`,
	).Scan(&fioSaleID); err != nil {
		t.Fatalf("read Fio's Ticket Sale: %v", err)
	}
	fioTickets := ticketIDsOfSale(t, env, fioSaleID)
	if len(fioTickets) != 4 {
		t.Fatalf("Fio's Sale has %d Tickets, want 4", len(fioTickets))
	}
	fio := customerSignIn(t, env, "fio@example.com")
	sharedEmail.Reset()

	acceptTicketAs(t, env, fio, fioSaleID, fioTickets[0], "gabi@example.com")
	acceptTicketAs(t, env, fio, fioSaleID, fioTickets[1], "hugo@example.com")

	result := sweepAnswerReminders(t, env)

	// FOUR MESSAGES IN ALL: Fio about her two unaccepted Tickets, Gabi, Hugo, and
	// Ana about hers. Three of the five people involved are buyers or Holders on
	// Fio's sale, and NOT ONE of them is written to twice.
	if result.Sent != 4 {
		t.Fatalf("sweep = %+v, want 4 mails: two buyers and two Holders, each written to once", result)
	}
	byAddress := map[string]int{}
	for _, mail := range sharedEmail.AnswerRemindersSent() {
		byAddress[mail.To]++
	}
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

	// Fio's four Tickets each carry one row: two spent by her own mail, one each
	// by the two Holders'.
	for _, ticketID := range fioTickets {
		if got := remindersForTicket(t, env, ticketID); got != 1 {
			t.Errorf("Ticket %s has %d ledger rows, want 1", ticketID, got)
		}
	}
}

// WRITTEN IN THE RECIPIENT'S MAIL LOCALE, AND THE CHAIN IS READ FROM THE OTHER
// END FOR A HOLDER.
//
// This is #325's inversion applied to a second mail (ADR 0033, ADR 0046). Every
// other message about a sale reads the Sale Locale first, because it is
// addressed to the person who made the sale in the language they were reading
// when they made it. A HOLDER IS NOT THAT PERSON: they bought nothing, were
// never on that page, and a Spanish-speaking friend of an English-speaking buyer
// is exactly the case the feature exists to serve.
//
// The test pins both halves at once, which is the only way it means anything:
// ONE sale, ONE sweep, and the two mails come out in different languages.
func TestTheHoldersAnswerReminderReadsTheLocaleChainRecipientFirst(t *testing.T) {
	env := setupTest(t)
	f := newHolderReminderFixture(t, env)
	acceptTicketAs(t, env, f.ana, f.saleID, f.ticketIDs[0], "carla@example.com")

	// The buyer paid on the ENGLISH storefront...
	if _, err := env.db.Exec(`UPDATE ticket_sales SET locale = 'en' WHERE id = $1`, f.saleID); err != nil {
		t.Fatalf("set the Sale Locale: %v", err)
	}
	// ...and both people's records remember SPANISH. For the buyer the sale wins;
	// for the Holder their own record does.
	if _, err := env.db.Exec(
		`UPDATE customers SET mail_locale = 'es' WHERE email IN ('ana@example.com', 'carla@example.com')`,
	); err != nil {
		t.Fatalf("remember both readers' languages: %v", err)
	}

	sweepAnswerReminders(t, env)

	buyerMails := sharedEmail.AnswerRemindersSent()
	if len(buyerMails) != 1 {
		t.Fatalf("buyer reminders = %d, want 1", len(buyerMails))
	}
	if buyerMails[0].Locale != platform.LocaleEN {
		t.Errorf("the buyer's reminder is in %q, want en: their own purchase, in the language they bought in (ADR 0033)", buyerMails[0].Locale)
	}

	carla := holderReminderFor(t, "carla@example.com")
	if carla.Locale != platform.LocaleES {
		t.Fatalf("the Holder's reminder is in %q, want es: they are not party to the sale, so their own Mail Locale outranks it (#325, ADR 0046)", carla.Locale)
	}
	if !strings.Contains(carla.Text(), "aún necesita respuesta") {
		t.Fatalf("the Holder's reminder body = %q, want the Spanish message", carla.Text())
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
	f := newHolderReminderFixture(t, env)
	acceptTicketAs(t, env, f.ana, f.saleID, f.ticketIDs[0], "carla@example.com")

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
	holderReminderFor(t, "carla@example.com")
}

// SILENT ONCE THE EVENT HAS STARTED, for the Holder exactly as for the buyer.
// Every route into an Answer is frozen by then — including the Assignment Link
// this mail would point at, which stops opening at the doors — so a reminder
// would be an instruction its reader cannot follow.
func TestTheHoldersAnswerReminderIsSilentOnceTheEventHasStarted(t *testing.T) {
	env := setupTest(t)
	f := newHolderReminderFixture(t, env)
	acceptTicketAs(t, env, f.ana, f.saleID, f.ticketIDs[0], "carla@example.com")

	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		f.eventID, env.fixedClock.Add(-time.Hour)); err != nil {
		t.Fatalf("open the doors: %v", err)
	}

	result := sweepAnswerReminders(t, env)
	if result.Sent != 0 || result.Due != 0 || result.DueTotal != 0 {
		t.Fatalf("sweep = %+v, want silence once the Event has started, for Holders and buyers alike", result)
	}
	if len(sharedEmail.HolderAnswerRemindersSent()) != 0 || len(sharedEmail.AnswerRemindersSent()) != 0 {
		t.Fatal("somebody was chased after the doors opened")
	}
}

// WITH TICKET ASSIGNMENT DARK, EVERY TICKET IS THE BUYER'S AGAIN — exactly the
// behaviour ADR 0044 shipped, from a deployment that has said nothing about
// assignment.
//
// THE TWO FLAGS MUST STAY INDEPENDENT (ADR 0046), and this is the sweep's half
// of that property: Ticket Questions stays open, so the debt and the mail are
// both live, and closing the newer flag moves only the ADDRESS. It does not
// silence the reminder, which would mean a deployment that killed assignment
// stopped chasing anybody about Tickets that had been accepted while it was on.
func TestTheAnswerReminderFallsBackToTheBuyerWhileTicketAssignmentIsDark(t *testing.T) {
	env := setupTest(t)
	f := newHolderReminderFixture(t, env)
	acceptTicketAs(t, env, f.ana, f.saleID, f.ticketIDs[0], "carla@example.com")

	// The operator kills assignment. Ticket Questions stays open.
	sharedApp.CatalogService.WithTicketAssignment(false)
	sharedApp.SalesService.WithTicketAssignment(false)

	result := sweepAnswerReminders(t, env)
	if result.Sent != 1 {
		t.Fatalf("sweep = %+v, want one mail: a closed flag moves the address, it does not stop the chase", result)
	}
	if len(sharedEmail.HolderAnswerRemindersSent()) != 0 {
		t.Fatal("a Holder was written to while TICKET_ASSIGNMENT_ENABLED was closed: a dark feature answers exactly as a build that never had it (ADR 0045)")
	}
	buyerMails := sharedEmail.AnswerRemindersSent()
	if len(buyerMails) != 1 || buyerMails[0].To != "ana@example.com" {
		t.Fatalf("buyer reminders = %+v, want one to ana@example.com covering both Tickets", buyerMails)
	}
	// BOTH Tickets are hers again, including the accepted one, so both spend an
	// allowance on the one message.
	if got := remindersFor(t, env, f.saleID); got != 2 {
		t.Fatalf("ledger rows across the Sale = %d, want 2 — both Tickets are the buyer's while the flag is closed", got)
	}
}
