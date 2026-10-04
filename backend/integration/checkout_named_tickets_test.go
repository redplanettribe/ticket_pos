package integration

import (
	"encoding/json"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/sales"
)

// NAMED TICKETS AT BEGIN-CHECKOUT (#669, parent #665, ADR 0076).
//
// On an Event that requires Named Tickets, a Storefront checkout is refused
// until every Ticket beyond the buyer's own names a Holder by email, and every
// Ticket - the buyer's own included - answers every required Ticket Question of
// its Ticket Type. The addresses the buyer typed then wait on the Payment beside
// its Answers, for the commit to pick up.
//
// The rule is judged once, here, and only while all three hold: the Event
// requires Named Tickets, it has not started, and Ticket Assignment is open. So
// every test below opens Ticket Assignment unless the closed flag is its
// subject, and switches the setting on through the Event form, since the
// package's shared fixture publishes Events with it off (publishCheckoutEvent).
//
// THE SEAM IS THE BEGIN-CHECKOUT ROUTE AS THE STOREFRONT CALLS IT, and the
// refusal's details are asserted in full because they are what the Storefront
// points the buyer at. SQL is read only for the held addresses, which no surface
// exposes until the commit writes them onto Tickets (#670).

// publishNamedEvent is publishCheckoutEvent with Named Tickets switched on, as
// an Org Admin would leave a new Event.
func publishNamedEvent(t *testing.T, env *testEnv, sessionID, name, slug string, priceCents, capacity int) (eventID, ticketTypeID string) {
	t.Helper()
	eventID, ticketTypeID = publishCheckoutEvent(t, env, sessionID, name, slug, priceCents, capacity)
	patchCheckoutEvent(t, env, sessionID, eventID, name, slug, true)
	return eventID, ticketTypeID
}

// namedQuestionFixture is checkoutQuestionFixture - a required size question
// and an optional meal question - on an Event that requires Named Tickets.
func namedQuestionFixture(t *testing.T, env *testEnv) (
	sessionID, eventID, ticketTypeID string,
	sizeQuestion, mealQuestion ticketQuestion,
) {
	t.Helper()
	sessionID, eventID, ticketTypeID, sizeQuestion, mealQuestion = checkoutQuestionFixture(t, env)
	patchCheckoutEvent(t, env, sessionID, eventID, "Answer Fest", "answer-fest", true)
	return sessionID, eventID, ticketTypeID, sizeQuestion, mealQuestion
}

// publishNamedUpgradeEvent is publishUpgradeEvent - a free Ticket Type and a
// paid one - on an Event that requires Named Tickets.
func publishNamedUpgradeEvent(t *testing.T, env *testEnv, sessionID string) (eventID, freeID, paidID string) {
	t.Helper()
	eventID, freeID, paidID = publishUpgradeEvent(t, env, sessionID, "Upgrade Fest", "upgrade-fest")
	patchCheckoutEvent(t, env, sessionID, eventID, "Upgrade Fest", "upgrade-fest", true)
	return eventID, freeID, paidID
}

// namedRefusal is the refusal's details as the Storefront reads them.
type namedRefusal struct {
	Tickets []owedTicket `json:"tickets"`
}

type owedTicket struct {
	TicketTypeID       string   `json:"ticket_type_id"`
	TicketIndex        int      `json:"ticket_index"`
	HolderEmailMissing bool     `json:"holder_email_missing"`
	MissingQuestionIDs []string `json:"missing_question_ids"`
}

// holder builds one entry of the begin-checkout body's Holder section.
func holder(ticketTypeID string, index int, email string) map[string]any {
	return map[string]any{
		"ticket_type_id": ticketTypeID,
		"ticket_index":   index,
		"holder_email":   email,
	}
}

// refusedAsUnnamed asserts a begin-checkout was refused for Named Tickets, that
// nothing was written, and returns what the refusal says is owed.
func refusedAsUnnamed(t *testing.T, env *testEnv, eventSlug string, body map[string]any) []owedTicket {
	t.Helper()
	before := countPayments(t, env)
	resp, envBody := beginCheckout(t, env, testOrgSlug, eventSlug, body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("begin checkout status=%d error=%+v, want 400", resp.StatusCode, envBody.Error)
	}
	if envBody.Error == nil || envBody.Error.Code != "NAMED_TICKETS_INCOMPLETE" {
		t.Fatalf("error=%+v, want NAMED_TICKETS_INCOMPLETE", envBody.Error)
	}
	if envBody.Error.Message == "" {
		t.Fatal("NAMED_TICKETS_INCOMPLETE carries no message")
	}
	if after := countPayments(t, env); after != before {
		t.Fatalf("a refused checkout wrote %d Payments", after-before)
	}
	raw, err := json.Marshal(envBody.Error.Details)
	if err != nil {
		t.Fatalf("encode details: %v", err)
	}
	var details namedRefusal
	if err := json.Unmarshal(raw, &details); err != nil {
		t.Fatalf("decode details %s: %v", raw, err)
	}
	if len(details.Tickets) == 0 {
		t.Fatalf("details = %s, want the Tickets that still owe something", raw)
	}
	for _, ticket := range details.Tickets {
		if ticket.MissingQuestionIDs == nil {
			t.Fatalf("details = %s: missing_question_ids must be an array, never null", raw)
		}
	}
	return details.Tickets
}

// assertOwed compares a refusal's Tickets against what each one should owe, in
// the order the refusal lists them.
func assertOwed(t *testing.T, got []owedTicket, want ...owedTicket) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("refusal names %+v, want %+v", got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		sort.Strings(g.MissingQuestionIDs)
		sort.Strings(w.MissingQuestionIDs)
		if g.TicketTypeID != w.TicketTypeID || g.TicketIndex != w.TicketIndex ||
			g.HolderEmailMissing != w.HolderEmailMissing ||
			len(g.MissingQuestionIDs) != len(w.MissingQuestionIDs) {
			t.Fatalf("refusal names %+v, want %+v", got, want)
		}
		for j := range w.MissingQuestionIDs {
			if g.MissingQuestionIDs[j] != w.MissingQuestionIDs[j] {
				t.Fatalf("refusal names %+v, want %+v", got, want)
			}
		}
	}
}

// TestNamedTicketsRefusesATicketWithNoHolder is the tracer bullet: two Tickets,
// the first the buyer's own, the second naming nobody.
func TestNamedTicketsRefusesATicketWithNoHolder(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishNamedEvent(t, env, sessionID, "Named Fest", "named-fest", 2000, 20)

	owed := refusedAsUnnamed(t, env, "named-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 2)))

	assertOwed(t, owed, owedTicket{TicketTypeID: gaID, TicketIndex: 2, HolderEmailMissing: true})
}

// heldHolders reads the Holder addresses a Payment holds, by Ticket Type and
// index. SQL because no surface exposes them before the commit writes them onto
// Tickets (#670).
func heldHolders(t *testing.T, env *testEnv, clientTransactionID string) map[string]map[int]string {
	t.Helper()
	rows, err := env.db.Query(`
		SELECT l.ticket_type_id, h.ticket_index, h.holder_email
		FROM payment_ticket_holders h
		JOIN payment_lines l ON l.id = h.payment_line_id
		JOIN payments p ON p.id = l.payment_id
		WHERE p.client_transaction_id = $1
	`, clientTransactionID)
	if err != nil {
		t.Fatalf("read held holders: %v", err)
	}
	defer rows.Close()
	held := map[string]map[int]string{}
	for rows.Next() {
		var ticketTypeID, email string
		var index int
		if err := rows.Scan(&ticketTypeID, &index, &email); err != nil {
			t.Fatalf("scan held holder: %v", err)
		}
		if held[ticketTypeID] == nil {
			held[ticketTypeID] = map[int]string{}
		}
		held[ticketTypeID][index] = email
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read held holders: %v", err)
	}
	return held
}

// assertHeld compares a Payment's held addresses with what should be there.
func assertHeld(t *testing.T, env *testEnv, clientTransactionID string, want map[string]map[int]string) {
	t.Helper()
	held := heldHolders(t, env, clientTransactionID)
	if len(held) != len(want) {
		t.Fatalf("held = %v, want %v", held, want)
	}
	for ticketTypeID, byIndex := range want {
		if len(held[ticketTypeID]) != len(byIndex) {
			t.Fatalf("held = %v, want %v", held, want)
		}
		for index, email := range byIndex {
			if held[ticketTypeID][index] != email {
				t.Fatalf("held = %v, want %v", held, want)
			}
		}
	}
}

// TestNamedTicketsAcceptsTheBuyersOwnAddressAndARepeat: a parent buying for two
// small children names themself twice. Any well-formed address will do, and
// the addresses wait on the Payment normalised, keyed by the Ticket they name.
func TestNamedTicketsAcceptsTheBuyersOwnAddressAndARepeat(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishNamedEvent(t, env, sessionID, "Named Fest", "named-fest", 2000, 20)

	body := checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 3))
	body["holders"] = []map[string]any{
		holder(gaID, 2, "  Ana@Example.com "),
		holder(gaID, 3, "ana@example.com"),
	}
	begun := beginCheckoutOK(t, env, testOrgSlug, "named-fest", body)

	assertHeld(t, env, begun.ClientTransactionID, map[string]map[int]string{
		gaID: {2: "ana@example.com", 3: "ana@example.com"},
	})
}

// TestNamedTicketsRefusesAMissingRequiredAnswerOnEveryTicket: every Ticket owes
// its required Answers, the buyer's own included, and the refusal names each
// Ticket with exactly what it still owes.
//
// The fixture's optional meal question is left unanswered everywhere, and a
// required question still awaiting Operator approval sits beside them: neither
// is owed, because an optional question never refuses and an unapproved one is
// not asked.
func TestNamedTicketsRefusesAMissingRequiredAnswerOnEveryTicket(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID, eventID, gaID, size, _ := namedQuestionFixture(t, env)
	draftTicketQuestion(t, env, sessionID, eventID, gaID, map[string]any{
		"label": "Allergies", "kind": "short_text", "required": true,
	})

	body := checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 3))
	body["holders"] = []map[string]any{
		holder(gaID, 2, "ben@example.com"),
	}
	body["answers"] = []map[string]any{
		checkoutAnswer(gaID, 2, size.ID, map[string]any{"text": "M"}),
		// The wrong shape for a short_text question: dropped, so still owed.
		checkoutAnswer(gaID, 3, size.ID, map[string]any{"checked": true}),
	}

	owed := refusedAsUnnamed(t, env, "answer-fest", body)

	assertOwed(t, owed,
		owedTicket{TicketTypeID: gaID, TicketIndex: 1, MissingQuestionIDs: []string{size.ID}},
		owedTicket{TicketTypeID: gaID, TicketIndex: 3, HolderEmailMissing: true, MissingQuestionIDs: []string{size.ID}},
	)
}

// TestNamedTicketsAsksNothingWhereItDoesNotBind: the same unnamed, unanswered
// basket is bought exactly as before when the setting is off, once the Event
// has started, and while Ticket Assignment is dark - and holds no address even
// where the body named one, since nobody asked for it.
func TestNamedTicketsAsksNothingWhereItDoesNotBind(t *testing.T) {
	for _, tc := range []struct {
		name    string
		arrange func(t *testing.T, env *testEnv, sessionID, eventID string)
	}{
		{"the setting is off", func(t *testing.T, env *testEnv, sessionID, eventID string) {
			enableTicketAssignment(t)
			patchCheckoutEvent(t, env, sessionID, eventID, "Answer Fest", "answer-fest", false)
		}},
		{"the Event has started", func(t *testing.T, env *testEnv, _, _ string) {
			enableTicketAssignment(t)
			// patchCheckoutEvent opens the doors 72 hours after the fixed
			// clock; this is one minute after.
			holdClocksAt(env.fixedClock.Add(72*time.Hour + time.Minute))
		}},
		{"Ticket Assignment is dark", func(*testing.T, *testEnv, string, string) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := setupTest(t)
			sessionID, eventID, gaID, _, _ := namedQuestionFixture(t, env)
			tc.arrange(t, env, sessionID, eventID)

			body := checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 3))
			body["holders"] = []map[string]any{holder(gaID, 2, "ben@example.com")}
			begun := beginCheckoutOK(t, env, testOrgSlug, "answer-fest", body)

			assertHeld(t, env, begun.ClientTransactionID, map[string]map[int]string{})
		})
	}
}

// TestNamedTicketsRefusesAMalformedAddressAsAFieldError: an address that is not
// one is a form the buyer must fix, refused on the field before anything is
// judged - on every Event, since it is a shape check.
func TestNamedTicketsRefusesAMalformedAddressAsAFieldError(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishNamedEvent(t, env, sessionID, "Named Fest", "named-fest", 2000, 20)

	for _, malformed := range []string{"ben-at-example.com", "Ben <ben@example.com>"} {
		t.Run(malformed, func(t *testing.T) {
			body := checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 2))
			body["holders"] = []map[string]any{holder(gaID, 2, malformed)}
			resp, envBody := beginCheckout(t, env, testOrgSlug, "named-fest", body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("begin checkout status=%d error=%+v, want 400", resp.StatusCode, envBody.Error)
			}
			fields := fieldErrorsByName(t, envBody)
			if got := fields["holders[0].holder_email"].Code; got != "INVALID_EMAIL" {
				t.Fatalf("field errors = %+v, want holders[0].holder_email INVALID_EMAIL", fields)
			}
		})
	}
}

// TestNamedTicketsExemptsTheTicketTheCommitSeats: the Ticket excused from naming
// a Holder is the one the commit then seats the buyer on - the first Ticket of
// the line sold dearest, which is neither the first in the cart nor the first
// in the catalog, and which a Promotion can move.
func TestNamedTicketsExemptsTheTicketTheCommitSeats(t *testing.T) {
	for _, tc := range []struct {
		name string
		// promoteVIP sells the dearer Ticket Type below GA's List Price, so
		// the seat moves to GA.
		promoteVIP bool
		seatType   string
	}{
		{"the dearest line", false, "VIP"},
		{"the dearest line as sold, under a Promotion", true, "GA"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := setupTest(t)
			enableTicketQuestions(t)
			enableTicketAssignment(t)
			sessionID := orgAdminSession(t, env)
			eventID, gaID := publishNamedEvent(t, env, sessionID, "Seat Fest", "seat-fest", 3000, 20)
			// Created second, so it sorts after GA in the catalog, and put
			// second in the cart too.
			vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 9000, 5)
			if tc.promoteVIP {
				setPromotion(t, env, sessionID, eventID, vipID, 1000, nil, env.fixedClock.Add(48*time.Hour))
			}
			seatID, otherID := vipID, gaID
			if tc.seatType == "GA" {
				seatID, otherID = gaID, vipID
			}
			lines := []map[string]any{cartLine(gaID, 2), cartLine(vipID, 2)}

			// Naming nobody owes an address on every Ticket but the seat.
			owed := refusedAsUnnamed(t, env, "seat-fest", checkoutBody("ana@example.com", "Ana", "Lopez", lines...))
			want := []owedTicket{
				{TicketTypeID: gaID, TicketIndex: 1, HolderEmailMissing: true},
				{TicketTypeID: gaID, TicketIndex: 2, HolderEmailMissing: true},
				{TicketTypeID: vipID, TicketIndex: 1, HolderEmailMissing: true},
				{TicketTypeID: vipID, TicketIndex: 2, HolderEmailMissing: true},
			}
			var exceptSeat []owedTicket
			for _, ticket := range want {
				if ticket.TicketTypeID == seatID && ticket.TicketIndex == 1 {
					continue
				}
				exceptSeat = append(exceptSeat, ticket)
			}
			assertOwed(t, owed, exceptSeat...)

			// Naming the other three is enough; an address given for the seat
			// is not held, because the seat is the buyer's.
			body := checkoutBody("ana@example.com", "Ana", "Lopez", lines...)
			body["holders"] = []map[string]any{
				holder(seatID, 1, "nobody@example.com"),
				holder(seatID, 2, "ben@example.com"),
				holder(otherID, 1, "cai@example.com"),
				holder(otherID, 2, "dee@example.com"),
			}
			begun := beginCheckoutOK(t, env, testOrgSlug, "seat-fest", body)
			assertHeld(t, env, begun.ClientTransactionID, map[string]map[int]string{
				seatID:  {2: "ben@example.com"},
				otherID: {1: "cai@example.com", 2: "dee@example.com"},
			})

			// And the commit seats the buyer on exactly that Ticket.
			confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
			ana := customerSignIn(t, env, "ana@example.com")
			if got := selfHeldTypeName(t, env, ana, saleIDOfPayment(t, env, begun.ClientTransactionID)); got != tc.seatType {
				t.Fatalf("the commit seated the buyer on %q, but begin-checkout exempted %q", got, tc.seatType)
			}
		})
	}
}

// TestNamedTicketsAsksNothingOfASurrenderedFreeLine: a buyer who puts a giveaway
// beside a VIP Ticket and elects the Upgrade never buys the giveaway, so it is
// asked neither an address nor its Answers. Without the election it is an
// ordinary Ticket and owes both.
func TestNamedTicketsAsksNothingOfASurrenderedFreeLine(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID, freeID, paidID := publishNamedUpgradeEvent(t, env, sessionID)
	size := createTicketQuestion(t, env, sessionID, eventID, freeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})

	owed := refusedAsUnnamed(t, env, "upgrade-fest", mixedBasket("ana@example.com", freeID, paidID))
	assertOwed(t, owed, owedTicket{
		TicketTypeID: freeID, TicketIndex: 1, HolderEmailMissing: true, MissingQuestionIDs: []string{size.ID},
	})

	begun := beginCheckoutOK(t, env, testOrgSlug, "upgrade-fest",
		electing(mixedBasket("ana@example.com", freeID, paidID)))
	assertHeld(t, env, begun.ClientTransactionID, map[string]map[int]string{})

	// The commit agrees: the free line is not bought, and the buyer holds VIP.
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	assertLines(t, saleOf(t, env, sessionID, eventID, "ana@example.com"), map[string]int{"VIP": 1}, "upgraded")
}

// TestNamedTicketsIsJudgedOnceAtBegin: the setting is read at begin-checkout
// only, so a Payment under way settles on the terms it started on whichever way
// an Org Admin flips it while the buyer is at the Payment Provider.
func TestNamedTicketsIsJudgedOnceAtBegin(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishNamedEvent(t, env, sessionID, "Named Fest", "named-fest", 2000, 20)
	flip := func(requires bool) {
		t.Helper()
		patchCheckoutEvent(t, env, sessionID, eventID, "Named Fest", "named-fest", requires)
	}

	named := checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 2))
	named["holders"] = []map[string]any{holder(gaID, 2, "ben@example.com")}
	begunNamed := beginCheckoutOK(t, env, testOrgSlug, "named-fest", named)
	flip(false)
	begunUnnamed := beginCheckoutOK(t, env, testOrgSlug, "named-fest",
		checkoutBody("cai@example.com", "Cai", "Unnamed", cartLine(gaID, 2)))
	flip(true)
	assertHeld(t, env, begunNamed.ClientTransactionID, map[string]map[int]string{gaID: {2: "ben@example.com"}})
	assertHeld(t, env, begunUnnamed.ClientTransactionID, map[string]map[int]string{})

	confirmCheckoutOK(t, env, begunNamed.ClientTransactionID, "approved")
	confirmCheckoutOK(t, env, begunUnnamed.ClientTransactionID, "approved")
	if got := ticketHolderEmails(t, env, begunNamed.ClientTransactionID); got[2] != "ben@example.com" {
		t.Fatalf("named sale's holders = %v, want Ben on ticket 2", got)
	}
	if got := ticketHolderEmails(t, env, begunUnnamed.ClientTransactionID); got[2] != "" {
		t.Fatalf("unnamed sale's holders = %v, want nobody named on ticket 2", got)
	}
}

// ticketHolderEmails reads the holder_email each Ticket of a Payment's sale
// carries, by ordinal, with "" for a Ticket nobody holds.
func ticketHolderEmails(t *testing.T, env *testEnv, clientTransactionID string) map[int]string {
	t.Helper()
	rows, err := env.db.Query(`
		SELECT tk.ordinal, COALESCE(tk.holder_email, '')
		FROM tickets tk
		JOIN ticket_sale_lines sl ON sl.id = tk.ticket_sale_line_id
		WHERE sl.ticket_sale_id = $1
	`, saleIDOfPayment(t, env, clientTransactionID))
	if err != nil {
		t.Fatalf("read ticket holders: %v", err)
	}
	defer rows.Close()
	byOrdinal := map[int]string{}
	for rows.Next() {
		var ordinal int
		var email string
		if err := rows.Scan(&ordinal, &email); err != nil {
			t.Fatalf("scan ticket holder: %v", err)
		}
		byOrdinal[ordinal] = email
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read ticket holders: %v", err)
	}
	return byOrdinal
}

// namedPurgeResult is the purge's tally, Holder addresses included.
type namedPurgeResult struct {
	AnswersPurged  int `json:"answers_purged"`
	HoldersPurged  int `json:"holders_purged"`
	PaymentsPurged int `json:"payments_purged"`
	HoldersHeld    int `json:"holders_held"`
}

func purgeAbandonedCheckoutData(t *testing.T, env *testEnv) namedPurgeResult {
	t.Helper()
	resp, body := env.post(t, "/api/v1/internal/checkout-answers/purge", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("purge status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	var out namedPurgeResult
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode purge result: %v", err)
	}
	return out
}

// TestHeldAddressesArePurgedWithHeldAnswers: the abandoned-checkout purge takes
// the addresses on exactly the held Answers' terms (ADR 0076) - 30 days after a
// Payment that is not approved, never on `expired` alone - in the same run.
//
// The patient buyer's Payment is lazily expired inside the window and then
// confirmed late, which is the case a purge keyed on `expired` would get wrong:
// its addresses must still be there for the commit. The other buyer never
// comes back, and a month on their Payment keeps its row and loses the
// addresses of the people they named.
func TestHeldAddressesArePurgedWithHeldAnswers(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishNamedEvent(t, env, sessionID, "Named Fest", "named-fest", 2000, 20)

	namedPair := func(buyer, friend string) beginCheckoutResult {
		body := checkoutBody(buyer, "Buyer", "Named", cartLine(gaID, 2))
		body["holders"] = []map[string]any{holder(gaID, 2, friend)}
		return beginCheckoutOK(t, env, testOrgSlug, "named-fest", body)
	}
	gone := namedPair("gone@example.com", "ben@example.com")
	patient := namedPair("pat@example.com", "cai@example.com")

	// Past the hold window a fresh begin lazily expires both.
	holdClocksAt(afterHoldWindow())
	beginCheckoutOK(t, env, testOrgSlug, "named-fest",
		checkoutBody("noa@example.com", "Noa", "New", cartLine(gaID, 1)))
	if got := paymentStatus(t, env, patient.ClientTransactionID); got != "expired" {
		t.Fatalf("stale payment status = %q, want expired", got)
	}
	if result := purgeAbandonedCheckoutData(t, env); result.HoldersPurged != 0 || result.HoldersHeld != 2 {
		t.Fatalf("purge inside the window = %+v, want nothing purged and 2 addresses held", result)
	}
	confirmCheckoutOK(t, env, patient.ClientTransactionID, "approved")
	// The late commit wrote Cai onto the Ticket and took the Payment's copy.
	assertHeld(t, env, patient.ClientTransactionID, map[string]map[int]string{})

	holdClocksAt(fixedClock.Add(sales.AbandonedAnswerRetention + time.Hour))
	result := purgeAbandonedCheckoutData(t, env)
	if result.HoldersPurged != 1 || result.PaymentsPurged != 1 || result.HoldersHeld != 0 {
		t.Fatalf("purge a month on = %+v, want 1 address off 1 Payment and none still held", result)
	}
	assertHeld(t, env, gone.ClientTransactionID, map[string]map[int]string{})
	if got := countPaymentLines(t, env, gone.ClientTransactionID); got != 1 {
		t.Fatalf("payment lines = %d after the purge, want 1 - the Payment is kept", got)
	}

	if again := purgeAbandonedCheckoutData(t, env); again.HoldersPurged != 0 || again.PaymentsPurged != 0 {
		t.Fatalf("second purge = %+v, want zeros", again)
	}
}

// TestAnAddressLeftOnACommittedPaymentIsPurgedOnTheNextRun: the commit takes
// the held addresses with it, but a Payment committed before it did (or by any
// future path that forgets to) would otherwise keep a third party's address
// forever, because an approved Payment is never abandoned. The purge takes any
// address an approved Payment still carries on its next run, whatever its age:
// the Ticket already has it, so nothing waits on the held copy.
func TestAnAddressLeftOnACommittedPaymentIsPurgedOnTheNextRun(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishNamedEvent(t, env, sessionID, "Named Fest", "named-fest", 2000, 20)

	body := checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 2))
	body["holders"] = []map[string]any{holder(gaID, 2, "ben@example.com")}
	sold := beginCheckoutOK(t, env, testOrgSlug, "named-fest", body)
	confirmCheckoutOK(t, env, sold.ClientTransactionID, "approved")

	// The row a commit from before the fix left behind.
	if _, err := env.db.Exec(`
		INSERT INTO payment_ticket_holders (payment_line_id, ticket_index, holder_email, created_at)
		SELECT l.id, 2, 'ben@example.com', $2
		FROM payment_lines l JOIN payments p ON p.id = l.payment_id
		WHERE p.client_transaction_id = $1
	`, sold.ClientTransactionID, fixedClock); err != nil {
		t.Fatalf("plant a leftover address: %v", err)
	}

	// Minutes after the sale, long inside the abandoned-checkout window.
	result := purgeAbandonedCheckoutData(t, env)
	if result.HoldersPurged != 1 || result.PaymentsPurged != 1 || result.HoldersHeld != 0 {
		t.Fatalf("purge = %+v, want the committed Payment's 1 address gone", result)
	}
	assertHeld(t, env, sold.ClientTransactionID, map[string]map[int]string{})
}

// TestNamedTicketsSucceedsWhenEveryTicketIsNamedAndAnswered is the other half:
// the same basket with nothing owed is bought, its addresses and Answers held
// on the Payment for the commit, and the optional question may stay blank.
func TestNamedTicketsSucceedsWhenEveryTicketIsNamedAndAnswered(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID, eventID, gaID, size, meal := namedQuestionFixture(t, env)

	body := checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 2))
	body["holders"] = []map[string]any{holder(gaID, 2, "ben@example.com")}
	body["answers"] = []map[string]any{
		checkoutAnswer(gaID, 1, size.ID, map[string]any{"text": "S"}),
		checkoutAnswer(gaID, 2, size.ID, map[string]any{"text": "L"}),
		checkoutAnswer(gaID, 2, meal.ID, map[string]any{"option_ids": []string{meal.Options[1].ID}}),
	}
	begun := beginCheckoutOK(t, env, testOrgSlug, "answer-fest", body)
	assertHeld(t, env, begun.ClientTransactionID, map[string]map[int]string{gaID: {2: "ben@example.com"}})

	// The Answers ride the Payment to the commit, which already copies them
	// onto the minted Tickets.
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	tickets := saleTickets(t, env, sessionID, eventID, saleIDOfPayment(t, env, begun.ClientTransactionID))
	if len(tickets) != 2 {
		t.Fatalf("sale has %d Tickets, want 2", len(tickets))
	}
	for i, want := range []string{"S", "L"} {
		if got := answerTo(t, tickets[i], size.ID); got == nil || *got != want {
			t.Fatalf("ticket %d answered %v to the size question, want %q", tickets[i].Ordinal, got, want)
		}
	}
}
