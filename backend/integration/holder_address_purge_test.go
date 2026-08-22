package integration

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// An address a friend never accepted gets a guaranteed end (#331, parent #322,
// ADR 0046): the holder address goes when the Event starts, and the Ticket, its
// Answers and the fact that it was assigned all stay.
//
// THIS FILE EXISTS FOR ONE SENTENCE, and every other test here is scaffolding
// around it: the purge takes the ADDRESS and nothing else. Assignment is the one
// place this platform holds contact details for a person who never came here and
// consented to nothing, so the deletion is what makes the feature defensible —
// and a deletion that took the Ticket, or its Answers, or the record that
// somebody was named for it, would be destroying the platform's account of a
// sale in order to honour a promise about a name. That is a record-keeping
// requirement and not an optimisation.
//
// TIME MOVES BY MOVING THE CLOCK, never by sleeping and never by backdating a
// row. The rule is about the EVENT's start, so the Events here are scheduled
// through the staff API in the ordinary way and the clock is walked past them.
// A test that reached into SQL to age an Event would be asserting against its own
// UPDATE rather than against the product.
//
// SQL APPEARS FOR TWO THINGS ONLY, both of which the API deliberately withholds:
// the new purge-marker column, which no surface hands anybody, and the staging of
// an `accepted` Ticket, which is unreachable until #325 lands the Assignment mail
// and the accept flow. Everything a buyer or an operator can observe is observed
// through a request.

// holderPurgeResult is what one run reports. It is a runbook as much as an
// automation result — the Abandoned Answer Purge's reasoning — so an operator
// running it by hand can tell "nothing was due" from "nothing is there".
type holderPurgeResult struct {
	AddressesPurged int    `json:"addresses_purged"`
	EventsPurged    int    `json:"events_purged"`
	PurgedAt        string `json:"purged_at"`
	AddressesHeld   int    `json:"addresses_held"`
}

// purgeHolderAddresses runs one purge tick and asserts only that the endpoint
// answered.
//
// It takes no credential, on the same terms as purgeAbandonedAnswers and
// drainReversals: the endpoint is authenticated by Cloud Run IAM before the
// request reaches the API (ADR 0008), which is infrastructure this suite does not
// run. What is exercised here is the behaviour behind that gate.
func purgeHolderAddresses(t *testing.T, env *testEnv) holderPurgeResult {
	t.Helper()
	resp, body := env.post(t, "/api/v1/internal/holder-addresses/purge", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holder address purge status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	var out holderPurgeResult
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode holder address purge result: %v", err)
	}
	return out
}

// holderAddressPurgedAt reads migration 081's marker: when this Ticket's address
// was taken, or NULL if it never carried one that reached the purge.
//
// SQL because no surface hands it to anybody, and deliberately so — it is the
// platform's own record that a Ticket was assigned, kept once the address is
// gone, and it is not a state. catalog.AssignmentState still reads a purged
// Ticket as `unassigned`, which the tests below assert through the buyer's page.
func holderAddressPurgedAt(t *testing.T, env *testEnv, ticketID string) sql.NullTime {
	t.Helper()
	var at sql.NullTime
	if err := env.db.QueryRow(
		`SELECT holder_address_purged_at FROM tickets WHERE id = $1`, ticketID,
	).Scan(&at); err != nil {
		t.Fatalf("read Ticket %s purge marker: %v", ticketID, err)
	}
	return at
}

// countAnswersOfTicket counts what one Ticket answers.
//
// Asserted through SQL rather than through the buyer's page because the page
// reports a Ticket's Answers alongside its assignment, and the point here is
// that the ROWS survive independently of anything the purge rewrote beside them.
func countAnswersOfTicket(t *testing.T, env *testEnv, ticketID string) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(
		`SELECT COUNT(*) FROM ticket_answers WHERE ticket_id = $1`, ticketID,
	).Scan(&n); err != nil {
		t.Fatalf("count Ticket %s answers: %v", ticketID, err)
	}
	return n
}

// acceptTicketAsCustomer stages the `accepted` state, which is UNREACHABLE
// through the API in this ticket.
//
// #325 is what lands the Assignment mail, the Assignment Link and the click that
// mints a Customer; until it does there is no route by which a Ticket becomes
// `accepted`, and the docs/testing.md rule that SQL is for preconditions the API
// cannot establish is exactly this case. What is being tested is not how a Ticket
// reaches the state — that is #325's test to write — but that the purge leaves a
// Ticket in it entirely alone, which is a property this job must have from the
// day it ships rather than from the day acceptance becomes reachable.
//
// The Customer is minted through the real sign-in flow rather than inserted, so
// the row this points at is an ordinary Customer under ordinary Customer
// retention — which is the whole reason an accepted Ticket is exempt.
func acceptTicketAsCustomer(t *testing.T, env *testEnv, ticketID, email string, at time.Time) {
	t.Helper()
	customerSignIn(t, env, email)

	var customerID string
	if err := env.db.QueryRow(
		`SELECT id FROM customers WHERE email = $1`, email,
	).Scan(&customerID); err != nil {
		t.Fatalf("read Customer %s: %v", email, err)
	}
	res, err := env.db.Exec(`
		UPDATE tickets SET accepted_at = $2, holder_customer_id = $3
		WHERE id = $1 AND holder_email = $4
	`, ticketID, at, customerID, email)
	if err != nil {
		t.Fatalf("stage acceptance of Ticket %s: %v", ticketID, err)
	}
	if affected, _ := res.RowsAffected(); affected != 1 {
		t.Fatalf("staging acceptance touched %d rows, want 1 — the Ticket is not assigned to %s", affected, email)
	}
}

// TestAnUnacceptedHolderAddressIsPurgedWhenTheEventStarts is the acceptance
// criterion, and the assertions about what SURVIVES are the half that matters.
//
// Ana buys two tickets, names two friends, and answers for one of them. Neither
// friend ever accepts, because nothing has mailed them — which is the ordinary
// case and will be for as long as most Tickets go to people who ignore an email.
// The doors open. Both addresses go, and everything else stays exactly where it
// was: the Tickets, the Answer, the Ticket Sale, and the record that these two
// Tickets were assigned to somebody.
func TestAnUnacceptedHolderAddressIsPurgedWhenTheEventStarts(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	enableTicketAssignment(t)
	ana := customerSignIn(t, env, "ana@example.com")

	// One Ticket is answered before it is assigned. A first assignment clears
	// nothing (#324), so the Answer is still there when the purge arrives — and
	// it is the row this test most needs to find intact afterwards.
	resp, body := env.put(t, buyerAnswerPath(f.anaSaleID, f.anaTicketIDs[0], f.sizeQuestion.ID),
		map[string]any{"text": "XL"}, authHeader(ana))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("answer status=%d error=%+v", resp.StatusCode, body.Error)
	}

	assignTicketOK(t, env, ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	// The clock moves a minute between the two writes. On this suite's fixed
	// clock two rows written in one instant are indistinguishable, and an
	// assertion that walks more than one of them becomes a coin toss; a minute
	// costs nothing and makes the two assignments separable facts.
	holdClocksAt(fixedClock.Add(time.Minute))
	assignTicketOK(t, env, ana, f.anaSaleID, f.anaTicketIDs[1], "diego@example.com")

	// Before the doors open the job is due nothing, and SAYS SO IN A WAY THAT IS
	// NOT SILENCE: two addresses are held. This is what distinguishes a run that
	// found nothing due from a scheduler that is not firing at all.
	early := purgeHolderAddresses(t, env)
	if early.AddressesPurged != 0 || early.EventsPurged != 0 {
		t.Fatalf("purge = %+v before the Event started, want zeros — the window closes at the doors, not before", early)
	}
	if early.AddressesHeld != 2 {
		t.Fatalf("purge reports %d addresses held, want 2 — a run that purges nothing is only legible if it says what is waiting", early.AddressesHeld)
	}
	if early.PurgedAt == "" {
		t.Error("purge reports no purged_at: the instant Event starts were compared against is the whole correctness argument and must be readable without a database session")
	}

	// The doors open. The Event started at fixedClock + 30 days.
	holdClocksAt(fixedClock.Add(30*24*time.Hour + time.Hour))

	result := purgeHolderAddresses(t, env)
	if result.AddressesPurged != 2 || result.EventsPurged != 1 {
		t.Fatalf("purge = %+v, want 2 addresses off 1 Event", result)
	}
	if result.AddressesHeld != 0 {
		t.Errorf("purge reports %d addresses still held, want 0", result.AddressesHeld)
	}

	// THE ADDRESSES ARE GONE, and gone from the database rather than merely
	// hidden from a page. A surface that stopped showing them would satisfy every
	// wire assertion and keep the liability.
	for _, ticketID := range f.anaTicketIDs {
		row := readTicketAssignment(t, env, ticketID)
		if row.holderEmail.Valid {
			t.Errorf("Ticket %s still carries holder_email=%q after the purge — this is the whole job",
				ticketID, row.holderEmail.String)
		}
		// assigned_at goes with the address because migration 080 says a pair
		// travels together. What keeps the fact is the marker below.
		if row.assignedAt.Valid {
			t.Errorf("Ticket %s still carries assigned_at after its address went; the pair travels together", ticketID)
		}
		// THE FACT THAT IT WAS ASSIGNED SURVIVES. Without this a purged Ticket
		// would be indistinguishable from one nobody was ever named for, and the
		// platform's own account of what it did to somebody's address would be
		// the thing the deletion destroyed.
		if !holderAddressPurgedAt(t, env, ticketID).Valid {
			t.Errorf("Ticket %s carries no purge marker: the address went and took the record of the assignment with it", ticketID)
		}
	}

	// THE TICKET AND ITS ANSWER SURVIVE. The buyer's page still lists both
	// Tickets, and the one that was answered still answers.
	after := listBuyerTickets(t, env, ana, f.anaSaleID)
	if len(after) != 2 {
		t.Fatalf("the buyer sees %d Tickets after the purge, want 2 — this is a purge of addresses, not of Tickets", len(after))
	}
	answered := findBuyerRow(t, after, f.anaTicketIDs[0])
	if got := buyerAnswerFor(t, answered, f.sizeQuestion.ID); got == nil || got.Text == nil || *got.Text != "XL" {
		t.Fatalf("the purged Ticket answers %+v, want \"XL\" — the Answers are the Organization's record of what it was told and are not this job's to take", got)
	}
	if countAnswersOfTicket(t, env, f.anaTicketIDs[0]) != 1 {
		t.Error("the purge deleted the Ticket's Answer rows")
	}

	// A PURGED TICKET READS `unassigned` TO EVERYBODY WHO LOOKS AT IT, which is
	// the truth: nobody holds it. The marker is a fact for the platform's records
	// and is deliberately not a fourth state on the wire (migration 081).
	for _, row := range after {
		if row.AssignmentState != "unassigned" || row.HolderEmail != "" {
			t.Errorf("Ticket %s reads state=%q holder=%q after the purge, want unassigned and empty",
				row.TicketID, row.AssignmentState, row.HolderEmail)
		}
	}

	// IDEMPOTENT: the second run finds nothing, rather than erroring or reporting
	// the same work twice.
	if again := purgeHolderAddresses(t, env); again.AddressesPurged != 0 || again.EventsPurged != 0 {
		t.Fatalf("second purge = %+v, want zeros — the job is not idempotent", again)
	}
	// And the marker was not rewritten by the run that found nothing.
	if !holderAddressPurgedAt(t, env, f.anaTicketIDs[0]).Valid {
		t.Error("the second run cleared the purge marker")
	}
}

// TestAnAcceptedTicketLosesNothingToThePurge pins the clause whose absence would
// be the most serious bug this job could have.
//
// A Holder who accepted proved that address from their own inbox: they are an
// ordinary Customer, verified, under ordinary Customer retention, and their
// address is on `customers` as well as on the Ticket. Taking it here would strip
// the Organization's guest list of exactly the people who answered — silently,
// and for no privacy gain at all, since the Customer record keeps the address
// anyway.
//
// The unaccepted Ticket of the same Sale is purged in the same run, which is what
// makes this a test about the predicate rather than about the clock.
func TestAnAcceptedTicketLosesNothingToThePurge(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	enableTicketAssignment(t)
	ana := customerSignIn(t, env, "ana@example.com")

	assignTicketOK(t, env, ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	holdClocksAt(fixedClock.Add(time.Minute))
	assignTicketOK(t, env, ana, f.anaSaleID, f.anaTicketIDs[1], "diego@example.com")

	// Carla clicks. Staged in SQL because #325 has not landed the link she would
	// click; see acceptTicketAsCustomer.
	acceptedAt := fixedClock.Add(2 * time.Minute)
	acceptTicketAsCustomer(t, env, f.anaTicketIDs[0], "carla@example.com", acceptedAt)

	// A year after the Event started, which is many times any window anybody
	// could mistake this rule for.
	holdClocksAt(fixedClock.Add(365 * 24 * time.Hour))

	result := purgeHolderAddresses(t, env)
	if result.AddressesPurged != 1 || result.EventsPurged != 1 {
		t.Fatalf("purge = %+v, want 1 address off 1 Event — Diego's went and Carla's did not", result)
	}

	carla := readTicketAssignment(t, env, f.anaTicketIDs[0])
	if !carla.holderEmail.Valid || carla.holderEmail.String != "carla@example.com" {
		t.Fatalf("the accepted Ticket's holder_email = %+v a year on, want carla@example.com.\n"+
			"An accepted Holder is an ordinary Customer under ordinary Customer retention; this job has no claim on them.",
			carla.holderEmail)
	}
	if !carla.acceptedAt.Valid || !carla.holderCustomerID.Valid {
		t.Errorf("the accepted Ticket lost accepted_at=%+v holder_customer_id=%+v — the purge unmade an acceptance",
			carla.acceptedAt, carla.holderCustomerID)
	}
	if !carla.assignedAt.Valid {
		t.Error("the accepted Ticket lost assigned_at")
	}
	if marker := holderAddressPurgedAt(t, env, f.anaTicketIDs[0]); marker.Valid {
		t.Errorf("the accepted Ticket carries a purge marker (%v): nothing was taken from it and nothing should say otherwise", marker.Time)
	}

	// Diego never accepted, so his address went in the same run.
	diego := readTicketAssignment(t, env, f.anaTicketIDs[1])
	if diego.holderEmail.Valid {
		t.Errorf("the unaccepted Ticket still carries holder_email=%q a year after the Event", diego.holderEmail.String)
	}

	// The standing backlog counts the UNACCEPTED only. Carla's address is not a
	// debt this job is ever going to settle, and reporting it here would present
	// a number that only rises beside one this job is supposed to drive down.
	if result.AddressesHeld != 0 {
		t.Errorf("purge reports %d addresses held, want 0 — an accepted Holder's address is not part of this job's backlog", result.AddressesHeld)
	}
}

// TestAnAddressSurvivesUntilItsOwnEventStarts pins where the boundary is, and
// that it is the EVENT's boundary rather than a platform-wide clock.
//
// Without it every assertion above would pass on a purge that took every
// unaccepted address the moment it ran: the tests there all move the clock far
// past a single Event's start, so none of them says anything about a Ticket for
// an Event that has not happened. A purge that ignored `starts_at` would delete
// the addresses of every future Event on the platform on its first tick, and
// nothing else in this repository would notice.
func TestAnAddressSurvivesUntilItsOwnEventStarts(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	enableTicketAssignment(t)
	ana := customerSignIn(t, env, "ana@example.com")

	// A second Event, starting sixty days out — a month after the first one.
	// Fio buys a ticket to it and names a friend.
	laterEventID := createDraftEvent(t, env, f.staffSession, "Later Fest", "later-fest")
	scheduleEvent(t, env, f.staffSession, laterEventID, "Later Fest", "later-fest",
		env.fixedClock.Add(60*24*time.Hour))
	laterTypeID := createTicketTypeWithCapacity(t, env, f.staffSession, laterEventID, "GA", 2000, 50)
	commitBatch(t, env, f.staffSession, laterEventID, "later-fio", []map[string]any{{
		"customer_email": "fio@example.com", "customer_first_name": "Fio", "customer_last_name": "Ruiz",
		"ticket_type_id": laterTypeID, "quantity": 1, "payment_method": "cash",
		"sold_at": "2026-07-03T10:00:00Z",
	}})
	var laterSaleID string
	if err := env.db.QueryRow(
		`SELECT id FROM ticket_sales WHERE customer_email = 'fio@example.com'`,
	).Scan(&laterSaleID); err != nil {
		t.Fatalf("read Fio's Ticket Sale: %v", err)
	}
	laterTicketIDs := ticketIDsOfSale(t, env, laterSaleID)

	assignTicketOK(t, env, ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	holdClocksAt(fixedClock.Add(time.Minute))
	fio := customerSignIn(t, env, "fio@example.com")
	assignTicketOK(t, env, fio, laterSaleID, laterTicketIDs[0], "gabi@example.com")

	// One hour SHORT of the first Event's start: still assignable, still nobody's
	// to take.
	holdClocksAt(fixedClock.Add(30*24*time.Hour - time.Hour))
	if result := purgeHolderAddresses(t, env); result.AddressesPurged != 0 {
		t.Fatalf("purge took %d addresses an hour before the doors opened, want 0 — the window closes AT the doors", result.AddressesPurged)
	}

	// Two hours later the first Event has started and the second has not. The
	// same run takes one address and leaves the other.
	holdClocksAt(fixedClock.Add(30*24*time.Hour + time.Hour))
	result := purgeHolderAddresses(t, env)
	if result.AddressesPurged != 1 || result.EventsPurged != 1 {
		t.Fatalf("purge = %+v, want 1 address off 1 Event — the started Event's, and not the one a month away", result)
	}
	if result.AddressesHeld != 1 {
		t.Errorf("purge reports %d addresses held, want 1 — Gabi's is still waiting on an Event that has not happened", result.AddressesHeld)
	}

	if row := readTicketAssignment(t, env, f.anaTicketIDs[0]); row.holderEmail.Valid {
		t.Errorf("the started Event's Ticket still carries %q", row.holderEmail.String)
	}
	later := readTicketAssignment(t, env, laterTicketIDs[0])
	if !later.holderEmail.Valid || later.holderEmail.String != "gabi@example.com" {
		t.Fatalf("the Ticket for an Event a month away lost its address (%+v).\n"+
			"The rule is the EVENT's start, read per Event; a purge that keyed on anything else deletes the future.", later.holderEmail)
	}
	if marker := holderAddressPurgedAt(t, env, laterTicketIDs[0]); marker.Valid {
		t.Error("the untouched Ticket carries a purge marker")
	}

	// And when that Event's own doors open, its address goes too.
	holdClocksAt(fixedClock.Add(60*24*time.Hour + time.Hour))
	if result := purgeHolderAddresses(t, env); result.AddressesPurged != 1 {
		t.Fatalf("purge took %d addresses once the second Event started, want 1", result.AddressesPurged)
	}
	if row := readTicketAssignment(t, env, laterTicketIDs[0]); row.holderEmail.Valid {
		t.Errorf("the second Event's Ticket still carries %q after its own doors opened", row.holderEmail.String)
	}
}

// TestAnUnscheduledEventsAddressIsNotPurged pins the one place this job is
// deliberately conservative rather than deliberately thorough.
//
// An Event that has never said when it starts has not started, which is the
// reading catalog.AssignmentWindow already gives a nil start — so its Tickets
// stay assignable and their addresses stay. Reading a missing start as "started"
// would take the address off every Ticket of every unscheduled Event the moment
// this job first ran, and a deletion is the wrong thing to be wrong about in that
// direction.
//
// It is worth knowing what this costs: an Event that never acquires a start holds
// its unaccepted addresses indefinitely. The answer is that it acquires one — an
// Event has to be scheduled before it can happen — and the next tick sweeps it.
func TestAnUnscheduledEventsAddressIsNotPurged(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	enableTicketAssignment(t)

	// An Event with a Ticket Type, a sale and no start date at all.
	undatedID := createDraftEvent(t, env, f.staffSession, "Someday Fest", "someday-fest")
	undatedTypeID := createTicketTypeWithCapacity(t, env, f.staffSession, undatedID, "GA", 2000, 50)
	commitBatch(t, env, f.staffSession, undatedID, "someday-hugo", []map[string]any{{
		"customer_email": "hugo@example.com", "customer_first_name": "Hugo", "customer_last_name": "Paz",
		"ticket_type_id": undatedTypeID, "quantity": 1, "payment_method": "cash",
		"sold_at": "2026-07-04T10:00:00Z",
	}})
	var undatedSaleID string
	if err := env.db.QueryRow(
		`SELECT id FROM ticket_sales WHERE customer_email = 'hugo@example.com'`,
	).Scan(&undatedSaleID); err != nil {
		t.Fatalf("read Hugo's Ticket Sale: %v", err)
	}
	undatedTicketIDs := ticketIDsOfSale(t, env, undatedSaleID)

	hugo := customerSignIn(t, env, "hugo@example.com")
	assignTicketOK(t, env, hugo, undatedSaleID, undatedTicketIDs[0], "ines@example.com")

	// Years on, and the Event still has no start.
	holdClocksAt(fixedClock.Add(3 * 365 * 24 * time.Hour))
	if result := purgeHolderAddresses(t, env); result.AddressesPurged != 0 {
		t.Fatalf("purge took %d addresses from an Event that has never said when it starts, want 0.\n"+
			"A NULL start read as \"started\" empties every unscheduled Event on the platform.", result.AddressesPurged)
	}
	row := readTicketAssignment(t, env, undatedTicketIDs[0])
	if !row.holderEmail.Valid || row.holderEmail.String != "ines@example.com" {
		t.Fatalf("the unscheduled Event's Ticket lost its address (%+v)", row.holderEmail)
	}

	// The Organization finally schedules it, in the past. The next tick sweeps it
	// — nothing had to be caught up, because the predicate is state and not a
	// series of events the job could have missed.
	scheduleEvent(t, env, f.staffSession, undatedID, "Someday Fest", "someday-fest",
		env.fixedClock.Add(24*time.Hour))
	if result := purgeHolderAddresses(t, env); result.AddressesPurged != 1 {
		t.Fatalf("purge took %d addresses once the Event acquired a start in the past, want 1", result.AddressesPurged)
	}
	if row := readTicketAssignment(t, env, undatedTicketIDs[0]); row.holderEmail.Valid {
		t.Errorf("the newly scheduled Event's Ticket still carries %q", row.holderEmail.String)
	}
}
