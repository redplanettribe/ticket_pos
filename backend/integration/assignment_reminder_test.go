package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Assignment Reminder sweep (#362, parent #361, ADR 0051), exercised
// through the internal endpoint with the fake mail sender, on
// answer_reminder_test.go's terms: what a buyer or Operator can observe — which
// mails left, to whom, saying what, and what the endpoint reported — never the
// SQL or the call graph.
//
// THIS FILE PROVES THE FIRST MAIL GOES AND THE EXCLUSIONS HOLD. The rationing
// (24h / 7d / max 2) and the batch limit have their own tickets and their own
// files in this package; the fixture and helpers below are theirs to reuse.
//
// FIXTURE SHAPE. An Event 30 days out, one ONLINE Sale of two Tickets by Ana —
// her Self-held Ticket (ADR 0048) and one unassigned — with Ticket Assignment
// ON, and the clock moved two days past the Sale so the 24-hour floor is
// behind it. Every test starts from "one mail is due" and takes one fact away.

// assignmentSweepResult is the sweep's response envelope, counts only.
type assignmentSweepResult struct {
	Due        int `json:"due"`
	Sent       int `json:"sent"`
	Skipped    int `json:"skipped"`
	Failed     int `json:"failed"`
	Unrecorded int `json:"unrecorded"`
	Backlog    int `json:"backlog"`
}

// sweepAssignmentReminders hits the sweep as Cloud Scheduler would: no body, no
// parameters, no credential of the application's.
func sweepAssignmentReminders(t *testing.T, env *testEnv) assignmentSweepResult {
	t.Helper()
	resp, body := env.post(t, "/api/v1/internal/assignment-reminders/sweep", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sweep status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	var out assignmentSweepResult
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode sweep result: %v", err)
	}
	return out
}

// assignmentRemindersFor counts the ledger rows for one Ticket Sale.
func assignmentRemindersFor(t *testing.T, env *testEnv, ticketSaleID string) int {
	t.Helper()
	var count int
	if err := env.db.QueryRow(
		`SELECT COUNT(*) FROM assignment_reminders WHERE ticket_sale_id = $1`, ticketSaleID,
	).Scan(&count); err != nil {
		t.Fatalf("count assignment reminders: %v", err)
	}
	return count
}

// assignmentReminderFixture is one online two-Ticket Sale with one Ticket
// unassigned, on an upcoming Event, a couple of days old.
type assignmentReminderFixture struct {
	staffSession string
	eventID      string
	ticketTypeID string
	saleID       string
	// selfHeldID is Ana's own Ticket, `accepted` by paying (ADR 0048); otherID
	// is the one still nobody's.
	selfHeldID string
	otherID    string
	// ana is Ana's Customer Session, for assigning.
	ana string
	// soldAt is the fixed clock the Sale was recorded at; sweepAt is where the
	// clock stands when the fixture returns, 48 hours later.
	soldAt  time.Time
	sweepAt time.Time
}

// newAssignmentReminderFixture opens Ticket Assignment, sells the Sale and
// moves the clock two days on. The Event starts 30 days after the Sale.
func newAssignmentReminderFixture(t *testing.T, env *testEnv) assignmentReminderFixture {
	t.Helper()
	enableTicketAssignment(t)
	return sellAssignmentReminderFixture(t, env, "ana@example.com", "Ana", 2)
}

// sellAssignmentReminderFixture sells ONE online Sale of `quantity` Tickets to
// the given buyer on a fresh Event and advances the clock past the 24-hour
// floor. It does not touch the feature flag, so a test can sell with the flag
// in whatever state it needs.
func sellAssignmentReminderFixture(t *testing.T, env *testEnv, email, firstName string, quantity int) assignmentReminderFixture {
	t.Helper()
	f := assignmentReminderFixture{soldAt: env.fixedClock}
	f.staffSession = orgAdminSession(t, env)
	f.eventID, f.ticketTypeID = publishCheckoutEvent(t, env, f.staffSession, "Assign Fest", "assign-fest-reminders", 2000, 50)
	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		f.eventID, env.fixedClock.Add(30*24*time.Hour)); err != nil {
		t.Fatalf("set the Event's start: %v", err)
	}
	f.saleID = sellAssignmentReminderSale(t, env, f.ticketTypeID, "assign-fest-reminders", email, firstName, quantity)
	ids := ticketIDsOfSale(t, env, f.saleID)
	if len(ids) != quantity {
		t.Fatalf("the fixture's Sale has %d Tickets, want %d", len(ids), quantity)
	}
	f.selfHeldID = ids[0]
	if quantity > 1 {
		f.otherID = ids[1]
	}
	f.ana = customerSignIn(t, env, email)
	f.sweepAt = env.fixedClock.Add(48 * time.Hour)
	moveClockTo(t, f.sweepAt)
	sharedEmail.Reset()
	return f
}

// sellAssignmentReminderSale checks out one more online Sale on the fixture's
// Event and returns its id. The clock is wherever the caller left it, so a
// second Sale sold after moveClockTo is younger than the first.
func sellAssignmentReminderSale(t *testing.T, env *testEnv, ticketTypeID, slug, email, firstName string, quantity int) string {
	t.Helper()
	begun := beginCheckoutOK(t, env, "test-org", slug,
		checkoutBody(email, firstName, "Lopez", cartLine(ticketTypeID, quantity)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	return saleIDOfPayment(t, env, begun.ClientTransactionID)
}

// assignmentReminderTo returns the one Assignment Reminder sent to an address,
// failing on zero or several.
func assignmentReminderTo(t *testing.T, address string) platform.AssignmentReminder {
	t.Helper()
	var found []platform.AssignmentReminder
	for _, mail := range sharedEmail.AssignmentRemindersSent() {
		if mail.To == address {
			found = append(found, mail)
		}
	}
	if len(found) != 1 {
		t.Fatalf("Assignment Reminders to %s = %d, want exactly 1", address, len(found))
	}
	return found[0]
}

// assertNoAssignmentReminders asserts that nobody was written to and nothing
// was recorded against the Sale.
func assertNoAssignmentReminders(t *testing.T, env *testEnv, saleID, why string) {
	t.Helper()
	if got := sharedEmail.AssignmentRemindersSent(); len(got) != 0 {
		addresses := make([]string, 0, len(got))
		for _, mail := range got {
			addresses = append(addresses, mail.To)
		}
		t.Fatalf("%d Assignment Reminder(s) went out to %v; want none: %s", len(got), addresses, why)
	}
	if got := assignmentRemindersFor(t, env, saleID); got != 0 {
		t.Fatalf("ledger rows for the Sale = %d, want 0: %s", got, why)
	}
}

// assertSilentSweep runs the sweep and asserts it found nothing due, sent
// nothing and reports no backlog.
func assertSilentSweep(t *testing.T, env *testEnv, saleID, why string) {
	t.Helper()
	result := sweepAssignmentReminders(t, env)
	if result.Sent != 0 || result.Due != 0 || result.Backlog != 0 {
		t.Fatalf("sweep = %+v, want silence: %s", result, why)
	}
	assertNoAssignmentReminders(t, env, saleID, why)
}

// THE TRACER BULLET. A two-Ticket online Sale, one Ticket still nobody's, two
// days old, Event a month away: the buyer gets ONE mail naming the Event, the
// tally, a fresh Confirmation Link to the Sale and the ADR 0047 disclosure —
// and the ledger records the send against the Sale.
func TestAssignmentReminderMailsTheBuyerOfASaleWithAnUnassignedTicket(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentReminderFixture(t, env)

	result := sweepAssignmentReminders(t, env)
	if result.Due != 1 || result.Sent != 1 || result.Skipped != 0 || result.Failed != 0 || result.Unrecorded != 0 {
		t.Fatalf("sweep = %+v, want one mail due and one sent", result)
	}
	if result.Backlog != 0 {
		t.Fatalf("backlog = %d after the send, want 0: the one Sale is now inside its cooldown", result.Backlog)
	}

	sent := assignmentReminderTo(t, "ana@example.com")
	if sent.FirstName != "Ana" || sent.EventName != "Assign Fest" {
		t.Errorf("the reminder greets %q about %q, want Ana / Assign Fest", sent.FirstName, sent.EventName)
	}
	if sent.UnassignedTickets != 1 || sent.TotalTickets != 2 {
		t.Errorf("tally = %d of %d, want 1 of 2: the Self-held Ticket is the buyer's and counts as assigned", sent.UnassignedTickets, sent.TotalTickets)
	}
	if sent.EventTimezone != "America/Guayaquil" || !sent.EventStartsAt.Equal(f.soldAt.Add(30*24*time.Hour)) {
		t.Errorf("event start = %v in %q, want the Event's own start and zone", sent.EventStartsAt, sent.EventTimezone)
	}
	if !sent.SaleCreatedAt.Equal(f.soldAt) {
		t.Errorf("sale created at = %v, want %v: #364's go-live sentence is keyed on it", sent.SaleCreatedAt, f.soldAt)
	}
	if sent.ConfirmationLink == "" || !strings.Contains(sent.ConfirmationLink, "/tickets/confirm?token=") {
		t.Fatalf("the reminder carries %q, want a Confirmation Link to the Sale", sent.ConfirmationLink)
	}
	if !strings.Contains(sent.Text(), sent.ConfirmationLink) {
		t.Fatal("the rendered mail does not carry its own Confirmation Link")
	}
	// The link opens THIS Sale: redeeming it yields a Customer Session scoped to
	// it, which is the only observable proof the token was minted for the right
	// Sale.
	token := sent.ConfirmationLink[strings.Index(sent.ConfirmationLink, "token=")+len("token="):]
	redeemConfirmationLinkOK(t, env, token, "")
	if text := sent.Text(); !strings.Contains(text, "organizer will see the address") {
		t.Fatalf("the mail does not carry the ADR 0047 disclosure:\n%s", text)
	}

	if got := assignmentRemindersFor(t, env, f.saleID); got != 1 {
		t.Fatalf("ledger rows for the Sale = %d, want 1 — the send is only rationed if it was written down", got)
	}
}

// A SINGLE-TICKET SALE IS NEVER REMINDED: its one Ticket is the buyer's own
// Self-held Ticket, and there is nobody to name.
func TestAssignmentReminderIsNeverSentForASingleTicketSale(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	f := sellAssignmentReminderFixture(t, env, "ana@example.com", "Ana", 1)

	assertSilentSweep(t, env, f.saleID, "a single-Ticket Sale has nobody to assign")
}

// A REVERSED SALE IS NEVER REMINDED, whether the status moved or a reversal
// row exists: its Tickets have ceased to exist.
func TestAssignmentReminderIsNeverSentForAReversedSale(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentReminderFixture(t, env)

	if _, err := env.db.Exec(`UPDATE ticket_sales SET status = 'reversed' WHERE id = $1`, f.saleID); err != nil {
		t.Fatalf("reverse the Sale: %v", err)
	}
	assertSilentSweep(t, env, f.saleID, "a reversed Sale's Tickets have ceased to exist")

	// A reversal ROW with the status still active — the window between the
	// reversal being recorded and the Sale voided — is refused on its own.
	if _, err := env.db.Exec(`UPDATE ticket_sales SET status = 'active' WHERE id = $1`, f.saleID); err != nil {
		t.Fatalf("restore the status: %v", err)
	}
	if _, err := env.db.Exec(`
		INSERT INTO sale_reversals (ticket_sale_id, client_transaction_id, requested_at, status, next_attempt_at)
		VALUES ($1, 'rev-1', NOW(), 'in_flight', NOW())
	`, f.saleID); err != nil {
		t.Fatalf("record a reversal: %v", err)
	}
	assertSilentSweep(t, env, f.saleID, "a Sale with a reversal row is on its way out")
}

// importedAssignmentReminderFixture records ONE imported Ticket Sale of
// `quantity` Tickets on a fresh upcoming Event and moves the clock two days
// past it, so only the channel differs from newAssignmentReminderFixture's
// online Sale.
//
// IT GOES THROUGH THE REAL SALE IMPORT ENDPOINT, with a quantity on one line.
// No import route can produce a multi-LINE Sale — both the file template and
// the Manually Recorded Sale form carry one Ticket Type per row — but a
// quantity above one is exactly the shape this mail is about, and staging it
// for real is what proves the buyer holds Ticket 1 (ADR 0055) rather than a
// hand-written row asserting they do.
//
// Ticket Assignment is opened BEFORE the import, because the Self-held Ticket
// is written in the transaction that records the Sale.
func importedAssignmentReminderFixture(t *testing.T, env *testEnv, slug, email, firstName string, quantity int) string {
	t.Helper()
	enableTicketAssignment(t)
	staff := orgAdminSession(t, env)
	eventID, ticketTypeID := publishCheckoutEvent(t, env, staff, "Assign Fest", slug, 2000, 50)
	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		eventID, env.fixedClock.Add(30*24*time.Hour)); err != nil {
		t.Fatalf("set the Event's start: %v", err)
	}
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports", map[string]any{
		"idempotency_key": "import-reminder-batch",
		"source":          "direct",
		"sales": []map[string]any{
			{"customer_email": email, "customer_first_name": firstName, "customer_last_name": "Lopez",
				"ticket_type_id": ticketTypeID, "quantity": quantity,
				"payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		},
	}, authHeader(staff))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("import status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var saleID string
	if err := env.db.QueryRow(
		`SELECT id FROM ticket_sales WHERE channel = 'import' AND customer_email = $1`, email,
	).Scan(&saleID); err != nil {
		t.Fatalf("find the imported Sale: %v", err)
	}
	if got := len(ticketIDsOfSale(t, env, saleID)); got != quantity {
		t.Fatalf("the imported Sale has %d Tickets, want %d", got, quantity)
	}
	moveClockTo(t, env.fixedClock.Add(48*time.Hour))
	sharedEmail.Reset()
	return saleID
}

// A MULTI-TICKET IMPORT SALE IS REMINDED (ADR 0055, widening ADR 0051's
// audience past `online`). The buyer holds Ticket 1 and nobody is named for the
// rest, which is precisely the debt this mail exists for. Asking somebody
// holding Tickets to name who is coming is a deliberate exception to ADR 0050's
// "an imported buyer is mailed nothing by default": that rule is about
// transactional mail they did not ask for, and this is the only way a roster
// hole is filled truthfully rather than by presumption.
func TestAssignmentReminderMailsTheBuyerOfAMultiTicketImportSale(t *testing.T) {
	env := setupTest(t)
	saleID := importedAssignmentReminderFixture(t, env, "assign-fest-import", "cash@example.com", "Cash", 5)

	result := sweepAssignmentReminders(t, env)
	if result.Due != 1 || result.Sent != 1 || result.Skipped != 0 || result.Failed != 0 {
		t.Fatalf("sweep = %+v, want one mail due and one sent for the imported Sale", result)
	}

	sent := assignmentReminderTo(t, "cash@example.com")
	if sent.FirstName != "Cash" || sent.EventName != "Assign Fest" {
		t.Errorf("the reminder greets %q about %q, want Cash / Assign Fest", sent.FirstName, sent.EventName)
	}
	if sent.UnassignedTickets != 4 || sent.TotalTickets != 5 {
		t.Errorf("tally = %d of %d, want 4 of 5: the buyer's own Ticket 1 counts as assigned", sent.UnassignedTickets, sent.TotalTickets)
	}
	if got := assignmentRemindersFor(t, env, saleID); got != 1 {
		t.Fatalf("ledger rows for the imported Sale = %d, want 1", got)
	}
}

// A SINGLE-TICKET IMPORT SALE IS SILENT BECAUSE THE BUYER ALREADY HOLDS IT —
// not because it was imported. ADR 0055 repealed the channel exclusion; what
// keeps this Sale quiet is ADR 0051's "more than one Ticket" clause, which the
// Self-held Ticket satisfies away. The test asserts the holder to say so: were
// the reason the channel again, this fixture would be silent with Ticket 1 held
// by nobody.
func TestAssignmentReminderIsSilentForASingleTicketImportSaleTheBuyerHolds(t *testing.T) {
	env := setupTest(t)
	saleID := importedAssignmentReminderFixture(t, env, "assign-fest-import-one", "solo@example.com", "Solo", 1)

	rows := holderRows(t, env, saleID)
	if len(rows) != 1 {
		t.Fatalf("the imported Sale has %d Tickets, want 1", len(rows))
	}
	if !rows[0].holder.Valid || rows[0].holder.String != "solo@example.com" {
		t.Fatalf("Ticket 1's holder = %+v, want the buyer: the silence below must be "+
			"'the buyer already holds it', not 'it was imported'", rows[0].holder)
	}

	assertSilentSweep(t, env, saleID, "the buyer already holds the Sale's only Ticket")
}

// THE REST OF THE RATION IS THE SAME RATION ON `import`. Only the channel
// clause moved, in both the SQL and the Go rule, so the age floor, the 7-day
// cooldown, the lifetime cap and the event-started guard are literally the same
// clauses the online tests above prove. The two exclusions with a channel-shaped
// story behind them — a Sale on its way out, and a buyer who has already acted
// — are worth stating on an imported Sale in their own right.
func TestAssignmentReminderExclusionsHoldOnAnImportSale(t *testing.T) {
	t.Run("a live reversal", func(t *testing.T) {
		env := setupTest(t)
		saleID := importedAssignmentReminderFixture(t, env, "assign-fest-import-rev", "cash@example.com", "Cash", 5)

		if _, err := env.db.Exec(`
			INSERT INTO sale_reversals (ticket_sale_id, client_transaction_id, requested_at, status, next_attempt_at)
			VALUES ($1, 'rev-import-1', NOW(), 'in_flight', NOW())
		`, saleID); err != nil {
			t.Fatalf("record a reversal: %v", err)
		}
		assertSilentSweep(t, env, saleID, "an imported Sale on its way out is not pointed at either")
	})

	t.Run("every Ticket assigned", func(t *testing.T) {
		env := setupTest(t)
		saleID := importedAssignmentReminderFixture(t, env, "assign-fest-import-done", "cash@example.com", "Cash", 5)

		buyer := customerSignIn(t, env, "cash@example.com")
		for _, ticketID := range ticketIDsOfSale(t, env, saleID)[1:] {
			assignTicketOK(t, env, buyer, saleID, ticketID, "carla@example.com")
		}
		sharedEmail.Reset()

		assertSilentSweep(t, env, saleID, "the imported buyer has named everybody")
	})
}

// NOTHING ONCE EVERY TICKET IS ASSIGNED: the buyer has acted, and the platform
// stops. Assigned — an address named, not yet accepted — is enough, because
// the debt this mail is about is the address, not the acceptance.
func TestAssignmentReminderStopsOnceEveryTicketIsAssigned(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentReminderFixture(t, env)

	assignTicketOK(t, env, f.ana, f.saleID, f.otherID, "carla@example.com")
	sharedEmail.Reset()

	assertSilentSweep(t, env, f.saleID, "every Ticket has an address")
}

// A REASSIGNED SELF-HELD TICKET COUNTS AS ASSIGNED (ADR 0048 via ADR 0051): the
// buyer gave their own Ticket to somebody, so it has an address. With the other
// Ticket assigned too, the Sale is silent; with only the Self-held one moved,
// the tally still counts one.
func TestAssignmentReminderCountsAReassignedSelfHeldTicketAsAssigned(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentReminderFixture(t, env)

	assignTicketOK(t, env, f.ana, f.saleID, f.selfHeldID, "carla@example.com")
	sharedEmail.Reset()

	result := sweepAssignmentReminders(t, env)
	if result.Sent != 1 {
		t.Fatalf("sweep = %+v, want one mail: the other Ticket is still nobody's", result)
	}
	if sent := assignmentReminderTo(t, "ana@example.com"); sent.UnassignedTickets != 1 || sent.TotalTickets != 2 {
		t.Fatalf("tally = %d of %d, want 1 of 2: the reassigned Self-held Ticket has an address", sent.UnassignedTickets, sent.TotalTickets)
	}
}

// SILENT ONCE THE EVENT HAS STARTED, read as an instant: the choice no longer
// matters.
func TestAssignmentReminderIsSilentOnceTheEventHasStarted(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentReminderFixture(t, env)

	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		f.eventID, f.sweepAt.Add(-time.Hour)); err != nil {
		t.Fatalf("open the doors: %v", err)
	}
	assertSilentSweep(t, env, f.saleID, "the doors have opened")
}

// NOTHING WHILE TICKET_ASSIGNMENT_ENABLED IS OFF: with the feature dark there is
// no page on which to assign, so a reminder would be an instruction its reader
// cannot follow. The Sale is sold with the flag OFF and the sweep is run with
// it off; the same Sale becomes due the moment it opens.
func TestAssignmentReminderSendsNothingWhileTicketAssignmentIsOff(t *testing.T) {
	env := setupTest(t)
	f := sellAssignmentReminderFixture(t, env, "ana@example.com", "Ana", 2)

	assertSilentSweep(t, env, f.saleID, "Ticket Assignment is off")

	enableTicketAssignment(t)
	if result := sweepAssignmentReminders(t, env); result.Sent != 1 {
		t.Fatalf("sweep with the flag on = %+v, want the same Sale reminded: the flag gates the sweep, not the Sale", result)
	}
}

// TRANSACTIONAL, NOT MARKETING. The buyer granted no Marketing Consent; the
// reminder arrives anyway, on the Sale Confirmation's footing (ADR 0034).
func TestAssignmentReminderIsSentRegardlessOfMarketingConsent(t *testing.T) {
	env := setupTest(t)
	// Declining at the door, which is where consent is asked since ADR 0054: the
	// buyer this reminder must reach is one who granted nothing.
	buyerDecliningMarketing(t, env, "ana@example.com")
	newAssignmentReminderFixture(t, env)

	var marketing int
	if err := env.db.QueryRow(`
		SELECT COUNT(*) FROM consent_records cr JOIN customers c ON c.id = cr.customer_id
		WHERE c.email = 'ana@example.com' AND cr.marketing_consent = TRUE
	`).Scan(&marketing); err != nil {
		t.Fatalf("read the buyer's Consent Records: %v", err)
	}
	if marketing != 0 {
		t.Fatalf("the fixture's buyer granted Marketing Consent %d time(s): this test only means something against somebody who granted nothing", marketing)
	}

	if result := sweepAssignmentReminders(t, env); result.Sent != 1 {
		t.Fatalf("sweep = %+v, want the reminder to reach a buyer who granted no Marketing Consent", result)
	}
	assignmentReminderTo(t, "ana@example.com")
}

// THE SALE LOCALE WINS over the buyer's remembered Mail Locale (ADR 0033): this
// mail is about the purchase, so it is written in the language the purchase
// was made in. The fallback to the Mail Locale is proved by forgetting the Sale
// Locale.
func TestAssignmentReminderIsWrittenInTheSaleLocaleFirst(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentReminderFixture(t, env)

	if _, err := env.db.Exec(`UPDATE ticket_sales SET locale = 'es' WHERE id = $1`, f.saleID); err != nil {
		t.Fatalf("set the Sale Locale: %v", err)
	}
	if _, err := env.db.Exec(`UPDATE customers SET mail_locale = 'en' WHERE email = 'ana@example.com'`); err != nil {
		t.Fatalf("remember the buyer's language: %v", err)
	}

	sweepAssignmentReminders(t, env)
	sent := assignmentReminderTo(t, "ana@example.com")
	if sent.Locale != platform.LocaleES {
		t.Fatalf("reminder locale = %q, want es: the Sale Locale outranks the remembered one for the buyer's own purchase", sent.Locale)
	}
	if !strings.Contains(sent.Text(), "aún no tiene dirección") {
		t.Fatalf("reminder body = %q, want the Spanish message", sent.Text())
	}

	// And with no Sale Locale, the remembered language carries.
	if _, err := env.db.Exec(`DELETE FROM assignment_reminders`); err != nil {
		t.Fatalf("clear the ledger: %v", err)
	}
	if _, err := env.db.Exec(`UPDATE ticket_sales SET locale = NULL WHERE id = $1`, f.saleID); err != nil {
		t.Fatalf("forget the Sale Locale: %v", err)
	}
	if _, err := env.db.Exec(`UPDATE customers SET mail_locale = 'es' WHERE email = 'ana@example.com'`); err != nil {
		t.Fatalf("remember the buyer's language: %v", err)
	}
	sharedEmail.Reset()
	sweepAssignmentReminders(t, env)
	if sent := assignmentReminderTo(t, "ana@example.com"); sent.Locale != platform.LocaleES {
		t.Fatalf("reminder locale = %q, want es from the buyer's Mail Locale when the Sale recorded none", sent.Locale)
	}
}
