package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Answer Reminder (#317, ADR 0044): the mail telling a buyer that Tickets on
// their Ticket Sale still owe Answers, and pointing them back at their sale to
// give them or to pass the Answer Links on.
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
// reading them. What one says is pinned in
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

// remindersFor counts the ledger rows one Ticket Sale has, which is the fact the
// rationing is computed from and the only thing the mail leaves behind.
func remindersFor(t *testing.T, env *testEnv, ticketSaleID string) int {
	t.Helper()
	var count int
	if err := env.db.QueryRow(
		`SELECT COUNT(*) FROM answer_reminders WHERE ticket_sale_id = $1`, ticketSaleID,
	).Scan(&count); err != nil {
		t.Fatalf("count answer reminders: %v", err)
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

// The mail this ticket is about: a buyer whose Tickets owe a required Answer is
// written to, once, and pointed at their own sale.
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
	if got := remindersFor(t, env, saleID); got != 1 {
		t.Fatalf("ledger rows = %d, want 1: the send is only rationed if it was written down", got)
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

// AT MOST ONE PER TICKET SALE PER 7 DAYS, which is the rule that makes this a
// swept job rather than something the question editor triggers.
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
	if got := remindersFor(t, env, saleID); got != 2 {
		t.Fatalf("ledger rows = %d, want 2", got)
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
		t.Fatalf("reminders sent over six weeks = %d, want %d — the cap is per Ticket Sale and for its whole life", got, catalog.MaxAnswerReminders)
	}
	if got := remindersFor(t, env, saleID); got != catalog.MaxAnswerReminders {
		t.Fatalf("ledger rows = %d, want %d", got, catalog.MaxAnswerReminders)
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
	if got := remindersFor(t, env, saleID); got != 1 {
		t.Fatalf("ledger rows = %d, want 1", got)
	}
}
