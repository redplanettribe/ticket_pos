package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// NAMED TICKETS AT COMMIT (#670, parent #665, ADR 0076).
//
// When a Storefront Ticket Sale is recorded, the addresses and Answers the buyer
// gave at a Named Tickets checkout land on its Tickets in the transaction that
// records it. An address that is the buyer's own makes the Ticket `accepted` at
// once, held by the buyer; any other makes it `assigned` and owes an Assignment
// mail. Nothing is mailed by the commit: a swept sender sends what is owed
// (#671), and the per-buyer mail rationing never refuses or consumes any of it.
//
// THE SEAM IS THE BUYER'S SALE PAGE, THE STAFF ANSWERS DIALOG, THE HOLDER LIST
// AND THE HOLDER EXPORT, as the people who read them see them. SQL is read only
// for the owed marks, which no surface exposes - the sweep is their only reader.

// namedCommitFixture is an Event requiring Named Tickets whose one Ticket Type,
// at `priceCents`, asks a required size question.
type namedCommitFixture struct {
	sessionID, eventID, gaID string
	size                     ticketQuestion
}

func newNamedCommitFixture(t *testing.T, env *testEnv, priceCents int) namedCommitFixture {
	t.Helper()
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	f := namedCommitFixture{sessionID: orgAdminSession(t, env)}
	f.eventID, f.gaID = publishNamedEvent(t, env, f.sessionID, "Named Fest", "named-fest", priceCents, 20)
	f.size = createTicketQuestion(t, env, f.sessionID, f.eventID, f.gaID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})
	return f
}

// familyBasket is Ana buying four: her own seat, Ben, herself again (typed the
// way an address book gives it), and Ben again - every Ticket sized.
func (f namedCommitFixture) familyBasket() map[string]any {
	body := checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(f.gaID, 4))
	body["holders"] = []map[string]any{
		holder(f.gaID, 2, "ben@example.com"),
		holder(f.gaID, 3, " Ana@Example.com"),
		holder(f.gaID, 4, "ben@example.com"),
	}
	body["answers"] = []map[string]any{
		checkoutAnswer(f.gaID, 1, f.size.ID, map[string]any{"text": "S"}),
		checkoutAnswer(f.gaID, 2, f.size.ID, map[string]any{"text": "M"}),
		checkoutAnswer(f.gaID, 3, f.size.ID, map[string]any{"text": "L"}),
		checkoutAnswer(f.gaID, 4, f.size.ID, map[string]any{"text": "XL"}),
	}
	return body
}

// beginSettlingCheckout begins a checkout that may settle in the begin request,
// as a free one does: beginCheckoutOK insists on a provider redirect, which a
// free cart never has.
func beginSettlingCheckout(t *testing.T, env *testEnv, body map[string]any) freeCheckoutResult {
	t.Helper()
	resp, envBody := beginCheckout(t, env, testOrgSlug, "named-fest", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("begin checkout status=%d error=%+v, want 201", resp.StatusCode, envBody.Error)
	}
	var result freeCheckoutResult
	if err := json.Unmarshal(envBody.Data, &result); err != nil || result.ClientTransactionID == "" {
		t.Fatalf("decode begin checkout result %s: %v", envBody.Data, err)
	}
	return result
}

// owedMail is one owed Assignment mail, as the swept sender will find it.
type owedMail struct {
	assignedAt time.Time
	// current is whether the mail is owed for the assignment the Ticket carries
	// now - the stale-row test the sweep makes.
	current bool
}

// owedMails reads the Assignment mails a Sale's Tickets are owed, by Ticket id.
// SQL because the swept sender is their only reader.
func owedMails(t *testing.T, env *testEnv, saleID string) map[string]owedMail {
	t.Helper()
	rows, err := env.db.Query(`
		SELECT o.ticket_id, o.assigned_at, o.assigned_at = tk.assigned_at
		FROM owed_assignment_mails o
		JOIN tickets tk ON tk.id = o.ticket_id
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		WHERE l.ticket_sale_id = $1
	`, saleID)
	if err != nil {
		t.Fatalf("read owed assignment mails: %v", err)
	}
	defer rows.Close()
	owed := map[string]owedMail{}
	for rows.Next() {
		var ticketID string
		var mail owedMail
		if err := rows.Scan(&ticketID, &mail.assignedAt, &mail.current); err != nil {
			t.Fatalf("scan owed assignment mail: %v", err)
		}
		owed[ticketID] = mail
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read owed assignment mails: %v", err)
	}
	return owed
}

// buyerTicketAt picks the buyer's row for one ordinal.
func buyerTicketAt(t *testing.T, tickets []buyerTicket, ordinal int) buyerTicket {
	t.Helper()
	for _, ticket := range tickets {
		if ticket.Ordinal == ordinal {
			return ticket
		}
	}
	t.Fatalf("no Ticket %d on the buyer's list %+v", ordinal, tickets)
	return buyerTicket{}
}

// assertNamedAsTyped is the centre of the ticket, read from the buyer's Sale
// page: the seat and the own-address Ticket held by Ana, the two Ben Tickets
// assigned to Ben and each owing one Assignment mail, and nothing mailed.
func assertNamedAsTyped(t *testing.T, env *testEnv, f namedCommitFixture, saleID string) {
	t.Helper()
	ana := customerSignIn(t, env, "ana@example.com")
	tickets := listBuyerTickets(t, env, ana, saleID)
	if len(tickets) != 4 {
		t.Fatalf("the Sale has %d Tickets, want 4", len(tickets))
	}
	for _, ordinal := range []int{1, 3} {
		assertOwnAddressAccepted(t, buyerTicketAt(t, tickets, ordinal), "ana@example.com")
	}
	owed := owedMails(t, env, saleID)
	for _, ordinal := range []int{2, 4} {
		ticket := buyerTicketAt(t, tickets, ordinal)
		if ticket.AssignmentState != "assigned" || ticket.HolderEmail != "ben@example.com" {
			t.Errorf("Ticket %d reads %q to %q, want assigned to ben@example.com",
				ordinal, ticket.AssignmentState, ticket.HolderEmail)
		}
		if ticket.AcceptedAt != nil || ticket.SelfHeld {
			t.Errorf("Ticket %d named for Ben is accepted=%v self_held=%v, want neither",
				ordinal, ticket.AcceptedAt, ticket.SelfHeld)
		}
		mail, ok := owed[ticket.TicketID]
		if !ok {
			t.Errorf("Ticket %d named for Ben owes no Assignment mail", ordinal)
			continue
		}
		if !mail.current {
			t.Errorf("Ticket %d's owed mail is for an assignment it does not carry", ordinal)
		}
	}
	if len(owed) != 2 {
		t.Errorf("%d Assignment mails owed, want 2 - the buyer's own Tickets owe none", len(owed))
	}
	if got := len(env.email.TicketAssignmentsSent()); got != 0 {
		t.Errorf("the commit sent %d Assignment mails, want 0 - the swept sender sends them", got)
	}
}

// assertEveryTicketAnswered reads the staff Answers dialog: every Ticket, the
// buyer's own and Ben's unaccepted ones alike, carries the size given for it.
func assertEveryTicketAnswered(t *testing.T, env *testEnv, f namedCommitFixture, saleID string) {
	t.Helper()
	tickets := saleTickets(t, env, f.sessionID, f.eventID, saleID)
	if len(tickets) != 4 {
		t.Fatalf("the Sale has %d Tickets, want 4", len(tickets))
	}
	for i, want := range []string{"S", "M", "L", "XL"} {
		if got := answerTo(t, tickets[i], f.size.ID); got == nil || *got != want {
			t.Errorf("Ticket %d answered %v, want %q", tickets[i].Ordinal, got, want)
		}
	}
}

// TestANamedCheckoutWritesItsAssignmentsOnEveryCommit: a paid commit, a free
// one, and a late approval of an `expired` Payment all land the names and the
// Answers exactly as typed.
func TestANamedCheckoutWritesItsAssignmentsOnEveryCommit(t *testing.T) {
	for _, tc := range []struct {
		name       string
		priceCents int
		commit     func(t *testing.T, env *testEnv, f namedCommitFixture, clientTransactionID string)
	}{
		{"a paid commit", 2000, func(t *testing.T, env *testEnv, _ namedCommitFixture, id string) {
			confirmCheckoutOK(t, env, id, "approved")
		}},
		{"a free commit", 0, func(*testing.T, *testEnv, namedCommitFixture, string) {
			// Settled inside the begin request; there is no confirm leg.
		}},
		{"a late approval of an expired Payment", 2000, func(t *testing.T, env *testEnv, f namedCommitFixture, id string) {
			holdClocksAt(afterHoldWindow())
			beginCheckoutOK(t, env, testOrgSlug, "named-fest", func() map[string]any {
				body := checkoutBody("noa@example.com", "Noa", "New", cartLine(f.gaID, 1))
				body["answers"] = []map[string]any{
					checkoutAnswer(f.gaID, 1, f.size.ID, map[string]any{"text": "M"}),
				}
				return body
			}())
			if got := paymentStatus(t, env, id); got != "expired" {
				t.Fatalf("payment status = %q, want expired before the late approval", got)
			}
			confirmCheckoutOK(t, env, id, "approved")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := setupTest(t)
			f := newNamedCommitFixture(t, env, tc.priceCents)
			begun := beginSettlingCheckout(t, env, f.familyBasket())
			if got := len(env.email.TicketAssignmentsSent()); got != 0 {
				t.Fatalf("begin-checkout sent %d Assignment mails, want 0", got)
			}
			tc.commit(t, env, f, begun.ClientTransactionID)

			saleID := saleIDOfPayment(t, env, begun.ClientTransactionID)
			assertNamedAsTyped(t, env, f, saleID)
			assertEveryTicketAnswered(t, env, f, saleID)
		})
	}
}

// TestTheOrganizationReadsTheCheckoutAnswersBeforeAnybodyAccepts: the Holder
// List owes nothing on any Ticket the checkout answered, names the buyer on
// the Tickets she holds and nobody on Ben's, and the Holder Export carries
// every size without ever carrying Ben's unaccepted address.
func TestTheOrganizationReadsTheCheckoutAnswersBeforeAnybodyAccepts(t *testing.T) {
	env := setupTest(t)
	f := newNamedCommitFixture(t, env, 2000)
	begun := beginCheckoutOK(t, env, testOrgSlug, "named-fest", f.familyBasket())
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	list := listOutstanding(t, env, f.sessionID, f.eventID)
	if len(list.Data) != 4 {
		t.Fatalf("the Holder List has %d rows, want 4", len(list.Data))
	}
	if list.OutstandingCount != 0 {
		t.Errorf("Outstanding Answers = %d, want 0 - every required Answer was given at checkout", list.OutstandingCount)
	}
	for _, row := range list.Data {
		if !owesNothing(t, list, row.TicketID) {
			t.Errorf("Ticket %d owes %v, want nothing", row.Ordinal, labelsOwedBy(list, row.TicketID))
		}
		switch row.Ordinal {
		case 1, 3:
			if row.AssignmentState != "accepted" || row.HolderEmail != "ana@example.com" ||
				row.HolderFirstName != "Ana" || row.HolderLastName != "Lopez" {
				t.Errorf("Ticket %d reads %q held by %q %q <%q>, want accepted by Ana Lopez",
					row.Ordinal, row.AssignmentState, row.HolderFirstName, row.HolderLastName, row.HolderEmail)
			}
		case 2, 4:
			if row.AssignmentState != "assigned" || row.HolderEmail != "" ||
				row.HolderFirstName != "" || row.HolderLastName != "" {
				t.Errorf("Ticket %d reads %q held by %q %q <%q>, want assigned and nobody named",
					row.Ordinal, row.AssignmentState, row.HolderFirstName, row.HolderLastName, row.HolderEmail)
			}
		}
	}

	resp, body := env.get(t, holderListPath(f.eventID)+"?outstanding=true", authHeader(f.sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("outstanding filter status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if owing := decodeOutstanding(t, body.Data); len(owing.Data) != 0 {
		t.Errorf("the Outstanding Answers filter lists %d Tickets, want none", len(owing.Data))
	}

	resp, data := downloadHolderExport(t, env, f.sessionID, f.eventID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holder export status=%d", resp.StatusCode)
	}
	assertAbsentFromExport(t, data, "ben@example.com")
	file := openHolderExport(t, env, f.sessionID, f.eventID, "")
	sizes := strings.Join(file.column(t, "T-shirt size"), ",")
	for _, want := range []string{"S", "M", "L", "XL"} {
		if !strings.Contains(","+sizes+",", ","+want+",") {
			t.Errorf("the Holder Export's sizes are %q, want %q among them", sizes, want)
		}
	}
}

// TestANineTicketCheckoutIsNeverRationed: a buyer whose Assignment mail window
// is already spent still buys nine Tickets naming eight people, and all eight
// are assigned and owed their mail. The rationing is about after-sale
// assignment; at checkout the money has already moved.
func TestANineTicketCheckoutIsNeverRationed(t *testing.T) {
	env := setupTest(t)
	f := newNamedCommitFixture(t, env, 2000)

	// Ana's first purchase gives her a Customer and a Ticket to spend her
	// window against.
	first := checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(f.gaID, 1))
	first["answers"] = []map[string]any{checkoutAnswer(f.gaID, 1, f.size.ID, map[string]any{"text": "S"})}
	firstBegun := beginCheckoutOK(t, env, testOrgSlug, "named-fest", first)
	confirmCheckoutOK(t, env, firstBegun.ClientTransactionID, "approved")
	spendBuyersAssignmentWindow(t, env, saleIDOfPayment(t, env, firstBegun.ClientTransactionID))

	body := checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(f.gaID, 9))
	var holders, answers []map[string]any
	for index := 1; index <= 9; index++ {
		answers = append(answers, checkoutAnswer(f.gaID, index, f.size.ID, map[string]any{"text": "M"}))
		if index > 1 {
			holders = append(holders, holder(f.gaID, index, "friend"+string(rune('0'+index))+"@example.com"))
		}
	}
	body["holders"], body["answers"] = holders, answers
	begun := beginCheckoutOK(t, env, testOrgSlug, "named-fest", body)
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	saleID := saleIDOfPayment(t, env, begun.ClientTransactionID)
	tickets := listBuyerTickets(t, env, customerSignIn(t, env, "ana@example.com"), saleID)
	if len(tickets) != 9 {
		t.Fatalf("the Sale has %d Tickets, want 9", len(tickets))
	}
	owed := owedMails(t, env, saleID)
	for _, ticket := range tickets {
		if ticket.Ordinal == 1 {
			continue
		}
		want := "friend" + string(rune('0'+ticket.Ordinal)) + "@example.com"
		if ticket.AssignmentState != "assigned" || ticket.HolderEmail != want {
			t.Errorf("Ticket %d reads %q to %q, want assigned to %s",
				ticket.Ordinal, ticket.AssignmentState, ticket.HolderEmail, want)
		}
		if _, ok := owed[ticket.TicketID]; !ok {
			t.Errorf("Ticket %d owes no Assignment mail", ticket.Ordinal)
		}
	}
	if len(owed) != 8 {
		t.Errorf("%d Assignment mails owed, want 8", len(owed))
	}
	if got := len(env.email.TicketAssignmentsSent()); got != 0 {
		t.Errorf("the commit sent %d Assignment mails, want 0", got)
	}
}

// spendBuyersAssignmentWindow fills the buyer of a Sale's rolling Assignment
// mail window, as twenty sends in the last hour would have. Arranged in SQL
// because spending it through the API would mean twenty real assignments.
func spendBuyersAssignmentWindow(t *testing.T, env *testEnv, saleID string) {
	t.Helper()
	if _, err := env.db.Exec(`
		INSERT INTO ticket_assignment_mails (ticket_id, buyer_customer_id, sent_at)
		SELECT tk.id, ts.customer_id, $2
		FROM ticket_sales ts
		JOIN ticket_sale_lines l ON l.ticket_sale_id = ts.id
		JOIN tickets tk ON tk.ticket_sale_line_id = l.id
		CROSS JOIN generate_series(1, 20)
		WHERE ts.id = $1
	`, saleID, env.fixedClock.Add(-time.Hour)); err != nil {
		t.Fatalf("spend the buyer's Assignment mail window: %v", err)
	}
}

// TestNamedTicketsNeverRefusesARecordedSale: on an Event requiring Named
// Tickets, a Sale Import, a Manually Recorded Sale and a Sale Correction's
// replacement are recorded as before, naming nobody beyond the buyer's seat and
// owing no mail.
func TestNamedTicketsNeverRefusesARecordedSale(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Recorded Fest", "recorded-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	enableTicketQuestions(t)
	createTicketQuestion(t, env, sessionID, eventID, gaID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})
	if !getEventNamedTickets(t, env, sessionID, eventID) {
		t.Fatal("a new Event does not require Named Tickets; this test needs one that does")
	}

	commitBatch(t, env, sessionID, eventID, "recorded-1", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
			"ticket_type_id": gaID, "quantity": 3, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	imported := activeSaleIDByEmail(t, env, eventID, "ana@example.com")
	manual := recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("bob@example.com", "Bob", "Ng", gaID, 2, "cash", "2026-07-01T10:00:00Z"))
	corrected := correctImportedSaleOK(t, env, sessionID, eventID, imported,
		correctionBody("anna@example.com", "Anna", "Lopez", gaID, 3, "cash", "2026-07-01T10:00:00Z"))

	for _, sale := range []struct {
		id, email string
		quantity  int
	}{
		{manual.SaleID, "bob@example.com", 2},
		{corrected.ReplacementSaleID, "anna@example.com", 3},
	} {
		assertBuyerHoldsTicketOneAlone(t, env, sale.id, sale.email, sale.quantity)
		if owed := owedMails(t, env, sale.id); len(owed) != 0 {
			t.Errorf("recorded sale to %s owes %d Assignment mails, want 0", sale.email, len(owed))
		}
	}
}
