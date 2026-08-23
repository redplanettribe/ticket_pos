package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// THE BUYER HOLDS ONE TICKET BY PAYING (ADR 0048). One Ticket of every Online
// Sale is the buyer's own from the moment the Sale is made: assigned to the
// buyer's address and accepted, so the Organization's roster names the buyer
// and the storefront can say "your ticket" truthfully. It is the first Ticket
// of the line whose Ticket Type comes FIRST IN THE CATALOG, not first in the
// cart, and every other Ticket starts `unassigned` exactly as before.
func TestOnlineCheckoutMakesTheFirstCatalogTicketTheBuyersOwn(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Held Fest", "held-fest", 2000, 20)
	// Created second, so it sorts AFTER GA in the catalog — and goes first in
	// the cart, so the test tells the two orders apart.
	vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 5000, 5)

	begun := beginCheckoutOK(t, env, "test-org", "held-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(vipID, 1), cartLine(gaID, 2)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	saleID := saleIDOfPayment(t, env, begun.ClientTransactionID)

	ana := customerSignIn(t, env, "ana@example.com")
	tickets := listBuyerTickets(t, env, ana, saleID)
	if len(tickets) != 3 {
		t.Fatalf("sale has %d Tickets, want 3", len(tickets))
	}
	var own, others int
	for _, ticket := range tickets {
		switch {
		case ticket.TicketTypeName == "GA" && ticket.Ordinal == 1:
			own++
			if ticket.AssignmentState != "accepted" || ticket.HolderEmail != "ana@example.com" || ticket.AcceptedAt == nil {
				t.Errorf("the buyer's own Ticket reads state=%q holder=%q accepted=%v; want accepted by the buyer",
					ticket.AssignmentState, ticket.HolderEmail, ticket.AcceptedAt)
			}
			if !ticket.SelfHeld {
				t.Error("the buyer's own Ticket is not reported self_held")
			}
		default:
			others++
			if ticket.SelfHeld {
				t.Errorf("%s #%d is reported self_held", ticket.TicketTypeName, ticket.Ordinal)
			}
			if ticket.AssignmentState != "unassigned" {
				t.Errorf("%s #%d reads %q; every Ticket but the buyer's own starts unassigned",
					ticket.TicketTypeName, ticket.Ordinal, ticket.AssignmentState)
			}
		}
	}
	if own != 1 || others != 2 {
		t.Fatalf("found %d own and %d other Tickets", own, others)
	}

	// And it is NOT among "tickets someone gave you": that list is other
	// people's purchases, and the buyer's own sits on their Sale.
	resp, body := env.get(t, "/api/v1/customer/ticket-sales", authHeader(ana))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("customer area status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var area struct {
		Holding []json.RawMessage `json:"holding"`
	}
	if err := json.Unmarshal(body.Data, &area); err != nil {
		t.Fatalf("decode customer area: %v", err)
	}
	if len(area.Holding) != 0 {
		t.Errorf("the buyer's own Ticket is listed as given to them: %s", area.Holding)
	}

	// The Organization sees an ordinary accepted Ticket under the buyer's
	// checkout name — no fourth state, nothing to tell it apart by.
	resp, body = env.get(t, outstandingPath(eventID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holder list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var seen bool
	for _, row := range decodeOutstanding(t, body.Data).Data {
		if row.TicketTypeName == "GA" && row.Ordinal == 1 {
			seen = true
			if row.AssignmentState != "accepted" || row.HolderEmail != "ana@example.com" ||
				row.HolderFirstName != "Ana" || row.HolderLastName != "Lopez" {
				t.Errorf("holder list shows state=%q holder=%q %q %q", row.AssignmentState,
					row.HolderEmail, row.HolderFirstName, row.HolderLastName)
			}
		}
	}
	if !seen {
		t.Fatal("the buyer's own Ticket is not on the Holder List")
	}
}

// ACCEPTED BY PURCHASE IS NOT PROOF OF EMAIL OWNERSHIP. A buyer holds their
// Ticket, but the customers row is not marked Verified BY THE SALE: verification
// stays the sign-in module's authority (ADR 0035), and a self-held Ticket
// asserts nothing about who controls the inbox.
//
// Since ADR 0054 every online buyer is verified anyway — by the sign-in that let
// them buy — so the property is read here as it is now readable: the sale is
// settled by a Payment whose buyer never proved anything (the shape of every
// checkout begun before #386, and of the rows this rule was written for), and
// the commit that mints the self-held Ticket verifies nobody.
func TestASelfHeldTicketMakesNobodyVerified(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Guest Fest", "guest-fest", 1000, 10)

	begun := beginLegacyGuestCheckout(t, env, "guest-fest", "guest@example.com", "Gus", "Perez",
		boolPtr(true), nil, nil, cartLine(gaID, 1))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	var verified *string
	var accepted int
	if err := env.db.QueryRow(`
		SELECT c.verified_at::text,
		       (SELECT count(*) FROM tickets tk WHERE tk.holder_customer_id = c.id AND tk.accepted_at IS NOT NULL)
		FROM customers c WHERE c.email = 'guest@example.com'
	`).Scan(&verified, &accepted); err != nil {
		t.Fatalf("read customer: %v", err)
	}
	if accepted != 1 {
		t.Fatalf("guest holds %d accepted Tickets, want 1", accepted)
	}
	if verified != nil {
		t.Errorf("a guest checkout marked the buyer Verified (%s); paying is not Proof of Email Ownership", *verified)
	}
}

// OFF BY DEFAULT, WITH THE REST OF ASSIGNMENT. While TICKET_ASSIGNMENT_ENABLED
// is closed an Online Sale writes no Holder at all, so a deployment without the
// feature is byte-for-byte the one it was.
func TestNoSelfHeldTicketWhileAssignmentIsClosed(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, sessionID, "Closed Fest", "closed-fest", 1000, 10)

	begun := beginCheckoutOK(t, env, "test-org", "closed-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 2)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	var held int
	if err := env.db.QueryRow(`
		SELECT count(*) FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		WHERE l.ticket_sale_id = $1 AND tk.holder_email IS NOT NULL
	`, saleIDOfPayment(t, env, begun.ClientTransactionID)).Scan(&held); err != nil {
		t.Fatalf("count held: %v", err)
	}
	if held != 0 {
		t.Fatalf("%d Tickets carry a Holder with assignment closed", held)
	}
}

// THE IMPORTED SALE'S BUYER HOLDS ONE TICKET TOO (ADR 0055, #393). ADR 0048
// excluded `import` on the premise that an imported buyer "is a name somebody
// else typed"; ADR 0055 says that premise is false of a transcription, which
// records a transaction the buyer made themself somewhere else. So one Ticket of
// every imported Ticket Sale is a Self-held Ticket, on all three routes onto the
// channel — a file Sale Import, a Manually Recorded Sale, and a Sale Correction's
// replacement — chosen by ADR 0048's rule unchanged and written in the same
// transaction that mints it.
//
// THE WARRANT IS THE TRANSCRIPTION, NOT A PROOF: the buyer is PRESUMED to attend,
// which is weaker than paying and far weaker than a click. So these tests insist
// on what the presumption does NOT buy — nobody is made Verified, no Assignment
// mail is written, and nothing at all happens while TICKET_ASSIGNMENT_ENABLED is
// closed.

// importedTicketRow is the assignment state of one Ticket of an imported Sale,
// read from the columns migration 080 added. SQL, because ordinal-by-ordinal is
// the whole subject and no surface hands a test a Ticket by ordinal.
type importedTicketRow struct {
	ordinal    int
	holder     *string
	assignedAt *string
	acceptedAt *string
}

// importedTicketRows reads a Ticket Sale's Tickets in ordinal order.
func importedTicketRows(t *testing.T, env *testEnv, saleID string) []importedTicketRow {
	t.Helper()
	rows, err := env.db.Query(`
		SELECT tk.ordinal, tk.holder_email, tk.assigned_at::text, tk.accepted_at::text
		FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		WHERE l.ticket_sale_id = $1
		ORDER BY tk.ordinal ASC
	`, saleID)
	if err != nil {
		t.Fatalf("read the Tickets of sale %s: %v", saleID, err)
	}
	defer rows.Close()
	var out []importedTicketRow
	for rows.Next() {
		var row importedTicketRow
		if err := rows.Scan(&row.ordinal, &row.holder, &row.assignedAt, &row.acceptedAt); err != nil {
			t.Fatalf("scan Ticket: %v", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the Tickets of sale %s: %v", saleID, err)
	}
	return out
}

// activeSaleIDByEmail names the one active Ticket Sale a buyer has on an Event.
// The commit routes that record several at once hand back no per-row id.
func activeSaleIDByEmail(t *testing.T, env *testEnv, eventID, email string) string {
	t.Helper()
	var id string
	if err := env.db.QueryRow(`
		SELECT id FROM ticket_sales
		WHERE event_id = $1 AND customer_email = $2 AND status = 'active'
	`, eventID, email).Scan(&id); err != nil {
		t.Fatalf("read the active Ticket Sale of %s: %v", email, err)
	}
	return id
}

// assertBuyerHoldsTicketOneAlone is ADR 0055's forward rule over one Sale: the
// lowest-ordinal Ticket is accepted under the buyer's own address in the act
// that recorded the Sale, and every other Ticket is untouched.
func assertBuyerHoldsTicketOneAlone(t *testing.T, env *testEnv, saleID, buyerEmail string, quantity int) {
	t.Helper()
	tickets := importedTicketRows(t, env, saleID)
	if len(tickets) != quantity {
		t.Fatalf("sale %s minted %d Tickets, want %d", saleID, len(tickets), quantity)
	}
	for _, ticket := range tickets {
		if ticket.ordinal == 1 {
			if ticket.holder == nil || *ticket.holder != buyerEmail {
				t.Errorf("Ticket 1 of %s is held by %v, want the buyer %s", saleID, ticket.holder, buyerEmail)
				continue
			}
			if ticket.assignedAt == nil || ticket.acceptedAt == nil || *ticket.assignedAt != *ticket.acceptedAt {
				t.Errorf("Ticket 1 of %s reads assigned=%v accepted=%v; a Self-held Ticket is assigned and accepted in one act",
					saleID, ticket.assignedAt, ticket.acceptedAt)
			}
			continue
		}
		if ticket.holder != nil || ticket.assignedAt != nil || ticket.acceptedAt != nil {
			t.Errorf("Ticket %d of %s reads holder=%v assigned=%v accepted=%v; every Ticket but the buyer's own starts unassigned",
				ticket.ordinal, saleID, ticket.holder, ticket.assignedAt, ticket.acceptedAt)
		}
	}
}

// assertPresumingIsNotProving insists the presumption granted no Proof of Email
// Ownership: the buyer's Customer row is not Verified.
func assertPresumingIsNotProving(t *testing.T, env *testEnv, email string) {
	t.Helper()
	var verified *string
	if err := env.db.QueryRow(`SELECT verified_at::text FROM customers WHERE email = $1`, email).Scan(&verified); err != nil {
		t.Fatalf("read the Customer %s: %v", email, err)
	}
	if verified != nil {
		t.Errorf("recording an imported Sale marked %s Verified (%s); a transcription is not Proof of Email Ownership",
			email, *verified)
	}
}

// A FILE SALE IMPORT SEATS EVERY ROW'S BUYER ON ITS OWN TICKET 1, and the
// Organization's roster names them. A quantity of one is fully held; a
// multi-Ticket row holds Ticket 1 and leaves the rest for the buyer to assign.
func TestASaleImportSeatsEachRowsBuyerOnTicketOne(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Roster Fest", "roster-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	env.email.Reset()

	commitBatch(t, env, sessionID, eventID, "roster-1", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
			"ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng",
			"ticket_type_id": gaID, "quantity": 3, "payment_method": "transfer", "sold_at": "2026-07-02T10:00:00Z"},
	})

	anaSaleID := activeSaleIDByEmail(t, env, eventID, "ana@example.com")
	bobSaleID := activeSaleIDByEmail(t, env, eventID, "bob@example.com")
	assertBuyerHoldsTicketOneAlone(t, env, anaSaleID, "ana@example.com", 1)
	assertBuyerHoldsTicketOneAlone(t, env, bobSaleID, "bob@example.com", 3)
	assertPresumingIsNotProving(t, env, "ana@example.com")
	assertPresumingIsNotProving(t, env, "bob@example.com")

	// THE ROSTER NAMES THEM. The Holder List is the Organization's answer to
	// "who is coming", and the hole ADR 0055 was written to close is the row it
	// used to render as nobody named.
	named := map[string]bool{}
	for _, row := range listOutstanding(t, env, sessionID, eventID).Data {
		if row.Ordinal != 1 {
			if row.AssignmentState != "unassigned" {
				t.Errorf("Ticket %d of %s reads %q on the Holder List", row.Ordinal, row.TicketSaleID, row.AssignmentState)
			}
			continue
		}
		if row.AssignmentState != "accepted" {
			t.Errorf("Ticket 1 of %s reads %q on the Holder List, want accepted", row.TicketSaleID, row.AssignmentState)
		}
		named[row.HolderEmail+"|"+row.HolderFirstName+" "+row.HolderLastName] = true
	}
	for _, want := range []string{"ana@example.com|Ana Lopez", "bob@example.com|Bob Ng"} {
		if !named[want] {
			t.Errorf("the Holder List does not name %q; it names %v", want, named)
		}
	}

	// NO MAIL BEYOND THE SALE CONFIRMATION THE IMPORT ALREADY SENDS. A Self-held
	// Ticket is accepted without a link, so nothing is written to anybody about
	// it and no Assignment mail ledger row is spent.
	if got := len(env.email.Confirmations()); got != 2 {
		t.Errorf("Sale Confirmations = %d, want one per row", got)
	}
	if got := len(env.email.TicketAssignmentsSent()); got != 0 {
		t.Errorf("Assignment mails = %d, want 0 — a Self-held Ticket is accepted by transcription, not by link", got)
	}
	if got := len(env.email.NoLongerHoldingsSent()); got != 0 {
		t.Errorf("No Longer Holding mails = %d, want 0", got)
	}

	// AND NOTHING ABOUT A FIGURE MOVES: a Ticket Assignment has never changed one.
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 4 {
		t.Errorf("sold_count = %d, want 4", got)
	}
	summary := salesSummaryOK(t, env, sessionID, eventID)
	if summary.TicketsSold != 4 || summary.SalesCount != 2 {
		t.Errorf("summary = %+v, want 2 sales of 4 tickets", summary)
	}
	if got := takingsCents(t, env, sessionID, eventID); got != 4000 {
		t.Errorf("Takings = %d, want 4000", got)
	}
}

// A MANUALLY RECORDED SALE DOES THE SAME. It shares the batchless import write
// with a Sale Correction's replacement, so the two are asserted apart.
func TestAManuallyRecordedSaleSeatsTheBuyerOnTicketOne(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Hand Roster Fest", "hand-roster-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	env.email.Reset()

	result := recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z"))

	assertBuyerHoldsTicketOneAlone(t, env, result.SaleID, "ana@example.com", 2)
	assertPresumingIsNotProving(t, env, "ana@example.com")

	if got := len(env.email.Confirmations()); got != 1 {
		t.Errorf("Sale Confirmations = %d, want exactly 1", got)
	}
	if got := len(env.email.TicketAssignmentsSent()); got != 0 {
		t.Errorf("Assignment mails = %d, want 0", got)
	}
	if got := soldCount(t, env, sessionID, eventID, gaID); got != 2 {
		t.Errorf("sold_count = %d, want 2", got)
	}
}

// A SALE CORRECTION RE-SEATS THE BUYER RATHER THAN DROPPING THEM OFF THE ROSTER.
// This amends ADR 0050's "the replacement's Tickets all start `unassigned`",
// which was less a decision about Self-held Tickets than a description of a
// channel that had none.
func TestASaleCorrectionReseatsTheBuyerOnTheReplacement(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Corrected Roster Fest", "corrected-roster-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	commitBatch(t, env, sessionID, eventID, "corrected-1", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
			"ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	saleID := activeSaleIDByEmail(t, env, eventID, "ana@example.com")
	assertBuyerHoldsTicketOneAlone(t, env, saleID, "ana@example.com", 2)
	env.email.Reset()

	// The typo being corrected is the address itself, which is the case that
	// makes re-seating matter: the roster must end up naming the person who is
	// actually coming rather than the misspelling.
	result := correctImportedSaleOK(t, env, sessionID, eventID, saleID,
		correctionBody("anna@example.com", "Anna", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z"))

	assertBuyerHoldsTicketOneAlone(t, env, result.ReplacementSaleID, "anna@example.com", 2)
	assertPresumingIsNotProving(t, env, "anna@example.com")
	if got := len(env.email.TicketAssignmentsSent()); got != 0 {
		t.Errorf("Assignment mails = %d, want 0", got)
	}
}

// THE TICKET IS THE BUYER'S OWN IN EVERY RESPECT: it is on their Sale page as
// self-held, its Ticket Questions are theirs to answer, and they may reassign it
// like any Ticket — after which it stops being self-held and stops being theirs.
//
// The remedy for a wrong presumption is exactly the one an online buyer has, and
// this is the test that says so.
func TestTheImportedBuyerAnswersAndReassignsTheirSelfHeldTicket(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Presumed Fest", "presumed-fest")
	scheduleEvent(t, env, sessionID, eventID, "Presumed Fest", "presumed-fest",
		env.fixedClock.Add(30*24*time.Hour))
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	size := createTicketQuestion(t, env, sessionID, eventID, gaID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})
	commitBatch(t, env, sessionID, eventID, "presumed-1", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
			"ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	saleID := activeSaleIDByEmail(t, env, eventID, "ana@example.com")
	ticketIDs := ticketIDsOfSale(t, env, saleID)

	ana := customerSignIn(t, env, "ana@example.com")
	for _, ticket := range listBuyerTickets(t, env, ana, saleID) {
		if ticket.Ordinal == 1 {
			if !ticket.SelfHeld || ticket.AssignmentState != "accepted" || ticket.HolderEmail != "ana@example.com" {
				t.Errorf("the buyer's own Ticket reads self_held=%v state=%q holder=%q",
					ticket.SelfHeld, ticket.AssignmentState, ticket.HolderEmail)
			}
			continue
		}
		if ticket.SelfHeld || ticket.AssignmentState != "unassigned" {
			t.Errorf("Ticket %d reads self_held=%v state=%q", ticket.Ordinal, ticket.SelfHeld, ticket.AssignmentState)
		}
	}

	// SHE MAY ANSWER ITS TICKET QUESTIONS, through the held-ticket route that is
	// the only one an Answer travels on (ADR 0049).
	held, raw := listHeldTickets(t, env, ana)
	if len(held) != 1 || held[0].TicketID != ticketIDs[0] {
		t.Fatalf("Ana holds %d Tickets, want her one Self-held Ticket: %s", len(held), raw)
	}
	answered, _ := answerHeldTicketOK(t, env, ana, ticketIDs[0], size.ID, map[string]any{"text": "M"})
	if answered.OutstandingCount != 0 {
		t.Errorf("outstanding = %d after answering, want 0", answered.OutstandingCount)
	}

	// AND SHE MAY GIVE IT AWAY. Reassignment is the remedy for a presumption
	// that was wrong, and it takes the Ticket off her.
	assignTicketOK(t, env, ana, saleID, ticketIDs[0], "carla@example.com")
	for _, ticket := range listBuyerTickets(t, env, ana, saleID) {
		if ticket.Ordinal == 1 && (ticket.SelfHeld || ticket.HolderEmail != "carla@example.com" ||
			ticket.AssignmentState != "assigned") {
			t.Errorf("after reassignment Ticket 1 reads self_held=%v holder=%q state=%q",
				ticket.SelfHeld, ticket.HolderEmail, ticket.AssignmentState)
		}
	}
	if stillHeld, _ := listHeldTickets(t, env, ana); len(stillHeld) != 0 {
		t.Errorf("Ana still holds %d Tickets after giving hers away", len(stillHeld))
	}
}

// NOTHING IS WRITTEN WHILE THE FLAG IS CLOSED, on any of the three routes, so
// TICKET_ASSIGNMENT_ENABLED keeps meaning one thing on every channel.
func TestNoSelfHeldTicketOnAnImportedSaleWhileAssignmentIsClosed(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Dark Fest", "dark-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	commitBatch(t, env, sessionID, eventID, "dark-1", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
			"ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	importedSaleID := activeSaleIDByEmail(t, env, eventID, "ana@example.com")
	manual := recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("bob@example.com", "Bob", "Ng", gaID, 2, "cash", "2026-07-01T10:00:00Z"))
	corrected := correctImportedSaleOK(t, env, sessionID, eventID, importedSaleID,
		correctionBody("ana@example.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z"))

	for name, saleID := range map[string]string{
		"the Sale Import":              importedSaleID,
		"the Manually Recorded Sale":   manual.SaleID,
		"the correction's replacement": corrected.ReplacementSaleID,
	} {
		for _, ticket := range importedTicketRows(t, env, saleID) {
			if ticket.holder != nil || ticket.assignedAt != nil || ticket.acceptedAt != nil {
				t.Errorf("%s wrote a Holder onto Ticket %d with TICKET_ASSIGNMENT_ENABLED closed",
					name, ticket.ordinal)
			}
		}
	}
}
