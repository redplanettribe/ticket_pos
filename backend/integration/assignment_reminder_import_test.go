package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// THE ASSIGNMENT REMINDER ON THE `import` SALES CHANNEL (#395, ADR 0055,
// asserted here for #399), on assignment_reminder_test.go's terms: what a buyer
// or an Operator can observe, never the SQL.
//
// WHY THIS CHANNEL GETS ITS OWN FILE OF ASSERTIONS. ADR 0055 widened ADR 0051's
// audience past `online` by moving ONE clause, and it was tempting to say so in
// a comment: the age floor, the cooldown, the lifetime cap and the event-started
// guard are the same clauses in the same shared SQL const, so the online tests
// above "prove" them here too. That is true exactly until somebody splits the
// const, and it is true in a way no failing test would ever announce. #395's
// point was that the import channel is ASSERTED and not inferred, so every
// clause is stated again below against a Sale that was transcribed rather than
// paid for.
//
// THE SALE IS STAGED THROUGH THE REAL SALE IMPORT ENDPOINT, never by hand, so
// what these tests chase is a buyer the platform really did seat on Ticket 1.

// importedReminderFixture is one imported Ticket Sale on a fresh upcoming Event,
// with the clock wherever the helper that built it left it.
type importedReminderFixture struct {
	staffSession string
	eventID      string
	ticketTypeID string
	saleID       string
	// soldAt is the clock the Sale was recorded at; sweepAt is where the clock
	// stands when the fixture returns.
	soldAt  time.Time
	sweepAt time.Time
}

// openImportedReminderEvent publishes an Event 30 days out with Ticket
// Assignment ALREADY OPEN, which is the order that matters: the Self-held Ticket
// is written in the transaction that records the Sale, so a flag opened
// afterwards seats nobody.
func openImportedReminderEvent(t *testing.T, env *testEnv, slug string) (staff, eventID, ticketTypeID string) {
	t.Helper()
	enableTicketAssignment(t)
	staff = orgAdminSession(t, env)
	eventID, ticketTypeID = publishCheckoutEvent(t, env, staff, "Assign Fest", slug, 2000, 50)
	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		eventID, env.fixedClock.Add(30*24*time.Hour)); err != nil {
		t.Fatalf("set the Event's start: %v", err)
	}
	return staff, eventID, ticketTypeID
}

// recordImportedReminderSale imports ONE Sale of `quantity` Tickets at whatever
// the clock currently says, and returns its id. It moves no clock, so a caller
// decides how old the Sale is when it sweeps.
//
// A QUANTITY ON ONE LINE. No import route can produce a multi-LINE Sale — both
// the file template and the Manually Recorded Sale form carry one Ticket Type
// per row — but a quantity above one is exactly the shape this mail is about.
func recordImportedReminderSale(
	t *testing.T, env *testEnv, staff, eventID, ticketTypeID, key, email, firstName string, quantity int,
) string {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports", map[string]any{
		"idempotency_key": key,
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
	return saleID
}

// importedAssignmentReminderFixture records ONE imported Ticket Sale of
// `quantity` Tickets on a fresh upcoming Event and moves the clock two days past
// it, so only the channel differs from newAssignmentReminderFixture's online
// Sale.
func importedAssignmentReminderFixture(
	t *testing.T, env *testEnv, slug, email, firstName string, quantity int,
) importedReminderFixture {
	t.Helper()
	f := importedReminderFixture{soldAt: env.fixedClock}
	f.staffSession, f.eventID, f.ticketTypeID = openImportedReminderEvent(t, env, slug)
	f.saleID = recordImportedReminderSale(t, env, f.staffSession, f.eventID, f.ticketTypeID,
		"import-reminder-batch", email, firstName, quantity)
	f.sweepAt = f.soldAt.Add(48 * time.Hour)
	moveClockTo(t, f.sweepAt)
	sharedEmail.Reset()
	return f
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
	f := importedAssignmentReminderFixture(t, env, "assign-fest-import", "cash@example.com", "Cash", 5)

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
	if got := assignmentRemindersFor(t, env, f.saleID); got != 1 {
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
	f := importedAssignmentReminderFixture(t, env, "assign-fest-import-one", "solo@example.com", "Solo", 1)

	rows := holderRows(t, env, f.saleID)
	if len(rows) != 1 {
		t.Fatalf("the imported Sale has %d Tickets, want 1", len(rows))
	}
	if !rows[0].holder.Valid || rows[0].holder.String != "solo@example.com" {
		t.Fatalf("Ticket 1's holder = %+v, want the buyer: the silence below must be "+
			"'the buyer already holds it', not 'it was imported'", rows[0].holder)
	}

	assertSilentSweep(t, env, f.saleID, "the buyer already holds the Sale's only Ticket")
}

// THE TWO EXCLUSIONS WITH A CHANNEL-SHAPED STORY BEHIND THEM: a Sale on its way
// out, and a buyer who has already named everybody.
func TestAssignmentReminderExclusionsHoldOnAnImportSale(t *testing.T) {
	t.Run("a live reversal", func(t *testing.T) {
		env := setupTest(t)
		f := importedAssignmentReminderFixture(t, env, "assign-fest-import-rev", "cash@example.com", "Cash", 5)

		if _, err := env.db.Exec(`
			INSERT INTO sale_reversals (ticket_sale_id, client_transaction_id, requested_at, status, next_attempt_at)
			VALUES ($1, 'rev-import-1', NOW(), 'in_flight', NOW())
		`, f.saleID); err != nil {
			t.Fatalf("record a reversal: %v", err)
		}
		assertSilentSweep(t, env, f.saleID, "an imported Sale on its way out is not pointed at either")
	})

	t.Run("every Ticket assigned", func(t *testing.T) {
		env := setupTest(t)
		f := importedAssignmentReminderFixture(t, env, "assign-fest-import-done", "cash@example.com", "Cash", 5)

		buyer := customerSignIn(t, env, "cash@example.com")
		for _, ticketID := range ticketIDsOfSale(t, env, f.saleID)[1:] {
			assignTicketOK(t, env, buyer, f.saleID, ticketID, "carla@example.com")
		}
		sharedEmail.Reset()

		assertSilentSweep(t, env, f.saleID, "the imported buyer has named everybody")
	})
}

// THE AGE FLOOR HOLDS ON AN IMPORT SALE. Nothing at 23 hours and 59 minutes past
// the transcription, the mail at exactly 24.
//
// The floor is measured from when the sale was RECORDED here and not from the
// `sold_at` the file carries, which on this fixture is weeks earlier: a
// transcription of a July sale typed in August is new to the buyer in August,
// and chasing them the same minute the Organization finished typing is the nag
// the floor exists to prevent.
func TestAssignmentReminderAgeFloorHoldsOnAnImportSale(t *testing.T) {
	env := setupTest(t)
	staff, eventID, ticketTypeID := openImportedReminderEvent(t, env, "assign-fest-import-floor")
	recordedAt := env.fixedClock
	saleID := recordImportedReminderSale(t, env, staff, eventID, ticketTypeID,
		"import-floor", "cash@example.com", "Cash", 5)
	sharedEmail.Reset()

	moveClockTo(t, recordedAt.Add(24*time.Hour-time.Minute))
	assertSilentSweep(t, env, saleID, "the imported Sale is a minute short of a day old")

	moveClockTo(t, recordedAt.Add(24*time.Hour))
	if result := sweepAssignmentReminders(t, env); result.Sent != 1 {
		t.Fatalf("sweep at exactly 24 hours = %+v, want the imported buyer reminded", result)
	}
	assignmentReminderTo(t, "cash@example.com")
}

// THE SEVEN-DAY COOLDOWN AND THE LIFETIME CAP OF TWO HOLD ON AN IMPORT SALE.
// Nothing at six days and twenty-three hours after the first mail; the second at
// exactly seven days; and then nothing ever again, however long the Event is
// still ahead. The Tickets stay unassigned throughout, so the debt is real on
// every one of those sweeps — it is the permission to say so that is rationed.
func TestAssignmentReminderCooldownAndCapHoldOnAnImportSale(t *testing.T) {
	env := setupTest(t)
	f := importedAssignmentReminderFixture(t, env, "assign-fest-import-ration", "cash@example.com", "Cash", 5)
	// Four months out, so no sweep below is ever silenced by the doors opening
	// instead of by the ration under test.
	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		f.eventID, f.soldAt.Add(120*24*time.Hour)); err != nil {
		t.Fatalf("push the Event out: %v", err)
	}

	if result := sweepAssignmentReminders(t, env); result.Sent != 1 {
		t.Fatalf("first sweep = %+v, want the first mail", result)
	}

	moveClockTo(t, f.sweepAt.Add(7*24*time.Hour-time.Hour))
	sharedEmail.Reset()
	if result := sweepAssignmentReminders(t, env); result.Sent != 0 || result.Due != 0 {
		t.Fatalf("sweep an hour short of the week = %+v, want silence", result)
	}
	if got := len(sharedEmail.AssignmentRemindersSent()); got != 0 {
		t.Fatalf("%d mail(s) inside the cooldown, want none", got)
	}

	moveClockTo(t, f.sweepAt.Add(7*24*time.Hour))
	if result := sweepAssignmentReminders(t, env); result.Sent != 1 {
		t.Fatalf("sweep at exactly seven days = %+v, want the second mail", result)
	}
	assignmentReminderTo(t, "cash@example.com")

	// And never a third. Eight more weekly ticks buy nothing: the mail is
	// transactional and carries no unsubscribe, so the cap is the only thing
	// between an imported buyer and an unbounded chase.
	sharedEmail.Reset()
	for week := 2; week <= 9; week++ {
		moveClockTo(t, f.sweepAt.Add(time.Duration(week)*7*24*time.Hour))
		sweepAssignmentReminders(t, env)
	}
	if got := len(sharedEmail.AssignmentRemindersSent()); got != 0 {
		t.Fatalf("%d further mail(s) after the second, want none: two is the lifetime cap", got)
	}
	if got := assignmentRemindersFor(t, env, f.saleID); got != 2 {
		t.Fatalf("ledger rows for the imported Sale = %d, want exactly 2", got)
	}
}

// SILENT ONCE THE EVENT HAS STARTED, on an import Sale as on an online one: the
// choice no longer matters, and the Tickets are as unassigned as they will ever
// be.
func TestAssignmentReminderIsSilentOnceTheEventHasStartedOnAnImportSale(t *testing.T) {
	env := setupTest(t)
	f := importedAssignmentReminderFixture(t, env, "assign-fest-import-started", "cash@example.com", "Cash", 5)

	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		f.eventID, f.sweepAt.Add(-time.Hour)); err != nil {
		t.Fatalf("open the doors: %v", err)
	}
	assertSilentSweep(t, env, f.saleID, "the doors have opened on the imported Sale's Event")
}

// NOTHING ON AN IMPORT SALE WHILE TICKET_ASSIGNMENT_ENABLED IS CLOSED.
//
// STAGED THE HARD WAY ROUND, and that is the whole point of it. The Sale is
// recorded while the flag is OPEN, so the buyer really does hold Ticket 1 and
// four Tickets really are nobody's — a Sale that is due in every respect — and
// THEN the flag is closed. A build that gated only the seating and not the sweep
// would pass a test that imported with the flag already dark, because such a
// Sale has no Self-held Ticket to be short of. Here the debt exists and the mail
// must still not go: with the feature closed there is no page on which to
// assign, so a reminder would be an instruction its reader cannot follow.
func TestAssignmentReminderSendsNothingOnAnImportSaleWhileTicketAssignmentIsOff(t *testing.T) {
	env := setupTest(t)
	f := importedAssignmentReminderFixture(t, env, "assign-fest-import-dark", "cash@example.com", "Cash", 5)

	held := 0
	for _, row := range holderRows(t, env, f.saleID) {
		if row.holder.Valid {
			held++
			if row.holder.String != "cash@example.com" {
				t.Fatalf("a Ticket is held by %q, want the buyer", row.holder.String)
			}
		}
	}
	if held != 1 {
		t.Fatalf("%d of the imported Sale's Tickets are held, want exactly 1 (the buyer's own): "+
			"this test means nothing unless the Sale is genuinely due before the flag closes", held)
	}

	closeTicketAssignment(t)
	assertSilentSweep(t, env, f.saleID, "Ticket Assignment is closed")

	enableTicketAssignment(t)
	if result := sweepAssignmentReminders(t, env); result.Sent != 1 {
		t.Fatalf("sweep with the flag open = %+v, want the same imported Sale reminded: "+
			"the flag gates the sweep, not the Sale", result)
	}
	assignmentReminderTo(t, "cash@example.com")
}

// SENDING STAYS PACED ACROSS A SWEEP OF IMPORT SALES.
//
// The launch that #376 came out of lost three of eighteen mails to the
// provider's ten-per-second limit, and a refused send leaves no ledger row, so
// those three were retried a day later. The gap is asserted here on the channel
// a catch-up sweep is largest on: an Organization that has just imported its
// back catalogue is precisely who fills a batch.
//
// OBSERVED THROUGH THE SLEEPER rather than a wall clock, so the test says
// exactly what was asked for and spends none of it.
func TestAssignmentReminderPacesItsSendsAcrossImportSales(t *testing.T) {
	env := setupTest(t)
	staff, eventID, ticketTypeID := openImportedReminderEvent(t, env, "assign-fest-import-paced")
	recordedAt := env.fixedClock
	for _, buyer := range []struct{ key, email, name string }{
		{"import-paced-1", "cash@example.com", "Cash"},
		{"import-paced-2", "bea@example.com", "Bea"},
		{"import-paced-3", "cris@example.com", "Cris"},
	} {
		recordImportedReminderSale(t, env, staff, eventID, ticketTypeID, buyer.key, buyer.email, buyer.name, 5)
	}
	moveClockTo(t, recordedAt.Add(48*time.Hour))
	sharedEmail.Reset()

	var pauses []time.Duration
	const gap = 120 * time.Millisecond
	sharedApp.SalesService.WithReminderPacing(gap, func(_ context.Context, d time.Duration) {
		pauses = append(pauses, d)
	})
	// Restored to a REAL sleep and not to the recorder, because the service is
	// shared across this package: a test that left a no-op sleeper installed
	// would silently take the pacing away from every test that ran after it.
	t.Cleanup(func() {
		sharedApp.SalesService.WithReminderPacing(gap, func(ctx context.Context, d time.Duration) {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
			case <-timer.C:
			}
		})
	})

	if result := sweepAssignmentReminders(t, env); result.Sent != 3 || result.Failed != 0 {
		t.Fatalf("sweep = %+v, want all three imported buyers reminded", result)
	}

	// Two gaps between three sends: none before the first, none after the last.
	if len(pauses) != 2 {
		t.Fatalf("the sweep paused %d time(s) for three sends, want 2 — one between each "+
			"consecutive pair, and none spent on either end", len(pauses))
	}
	for _, d := range pauses {
		if d != gap {
			t.Errorf("pause = %v, want the configured %v", d, gap)
		}
	}
}

// THE REMINDER IS WRITTEN IN THE SALE LOCALE ON AN IMPORT SALE TOO, and falls to
// the buyer's remembered Mail Locale when the Sale recorded none — which is what
// an imported Sale ordinarily does, since it was produced by no page.
//
// THE FALLBACK IS THE REAL CASE HERE and is asserted first for that reason: a
// transcription has no Locale to record, so almost every imported buyer is
// written to in whatever their own record remembers. The Sale Locale half is
// asserted second because the column exists on this channel and a Sale
// Correction or a later import route could fill it, and if it is filled it must
// outrank the remembered language exactly as it does online.
func TestAssignmentReminderOnAnImportSaleFollowsTheSaleLocaleThenTheMailLocale(t *testing.T) {
	env := setupTest(t)
	f := importedAssignmentReminderFixture(t, env, "assign-fest-import-locale", "cash@example.com", "Cash", 5)

	var locale *string
	if err := env.db.QueryRow(`SELECT locale FROM ticket_sales WHERE id = $1`, f.saleID).Scan(&locale); err != nil {
		t.Fatalf("read the imported Sale's Locale: %v", err)
	}
	if locale != nil {
		t.Fatalf("the imported Sale recorded Locale %q; it was produced by no page and must record none", *locale)
	}
	if _, err := env.db.Exec(`UPDATE customers SET mail_locale = 'es' WHERE email = 'cash@example.com'`); err != nil {
		t.Fatalf("remember the buyer's language: %v", err)
	}

	sweepAssignmentReminders(t, env)
	sent := assignmentReminderTo(t, "cash@example.com")
	if sent.Locale != platform.LocaleES {
		t.Fatalf("reminder locale = %q, want es from the buyer's Mail Locale when the Sale recorded none", sent.Locale)
	}
	if !strings.Contains(sent.Text(), "4 de sus 5 entradas aún no tienen dirección") {
		t.Fatalf("reminder body = %q, want the Spanish message", sent.Text())
	}

	// And with a Sale Locale on the row, that wins: this mail is about the
	// purchase, so it is written in the language the purchase was recorded in.
	if _, err := env.db.Exec(`DELETE FROM assignment_reminders`); err != nil {
		t.Fatalf("clear the ledger: %v", err)
	}
	if _, err := env.db.Exec(`UPDATE ticket_sales SET locale = 'en' WHERE id = $1`, f.saleID); err != nil {
		t.Fatalf("set the Sale Locale: %v", err)
	}
	sharedEmail.Reset()
	sweepAssignmentReminders(t, env)
	if sent := assignmentReminderTo(t, "cash@example.com"); sent.Locale != platform.LocaleEN {
		t.Fatalf("reminder locale = %q, want en: the Sale Locale outranks the remembered one", sent.Locale)
	}
}

// TRANSACTIONAL, NOT MARKETING, ON THE IMPORT CHANNEL — where the point is
// sharpest, because an imported buyer has consented to NOTHING. They never
// signed in, never saw a checkout dialog and were never asked; the Customer
// behind them was minted by the transcription. A build that gated this mail on
// Marketing Consent would mail no imported buyer at all, and the roster hole
// ADR 0055 is about would go on being filled by presumption.
func TestAssignmentReminderReachesAnImportedBuyerWhoConsentedToNothing(t *testing.T) {
	env := setupTest(t)
	importedAssignmentReminderFixture(t, env, "assign-fest-import-consent", "cash@example.com", "Cash", 5)

	var records int
	if err := env.db.QueryRow(`
		SELECT COUNT(*) FROM consent_records cr JOIN customers c ON c.id = cr.customer_id
		WHERE c.email = 'cash@example.com'
	`).Scan(&records); err != nil {
		t.Fatalf("read the imported buyer's Consent Records: %v", err)
	}
	if records != 0 {
		t.Fatalf("the imported buyer has %d Consent Record(s): this test only means something "+
			"against somebody who was never asked", records)
	}

	if result := sweepAssignmentReminders(t, env); result.Sent != 1 {
		t.Fatalf("sweep = %+v, want the reminder to reach a buyer who granted nothing", result)
	}
	assignmentReminderTo(t, "cash@example.com")
}
