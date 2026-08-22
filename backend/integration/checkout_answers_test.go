package integration

import (
	"net/http"
	"testing"
)

// The buyer answers at checkout (#311, ADR 0044): a skippable answer section
// whose replies ride the Payment keyed by (payment line, index) and land on the
// minted Tickets in order when the sale commits.
//
// These run the WHOLE journey — begin, provider redirect, confirm — and then
// read the Answers back through the staff API rather than out of SQL, because
// what is worth asserting is that the Answer arrived on the right Ticket, and
// "the right Ticket" is a thing the staff surface names by ordinal.
//
// EVERY TEST HERE IS ULTIMATELY ABOUT ONE SENTENCE: nothing about a Ticket
// Question may refuse or delay a checkout. The negative cases are not edge
// cases; they are the feature.

// checkoutQuestionFixture publishes an Event whose one Ticket Type asks a
// required short_text question and an optional single_choice one, and returns
// everything a checkout and a read-back need.
//
// The question is REQUIRED deliberately. Required's only effect is producing an
// Outstanding Answer (migration 072), so a fixture whose question is optional
// would let every "the checkout was not refused" assertion below pass for the
// wrong reason.
func checkoutQuestionFixture(t *testing.T, env *testEnv) (
	sessionID, eventID, ticketTypeID string,
	sizeQuestion, mealQuestion ticketQuestion,
) {
	t.Helper()
	enableTicketQuestions(t)
	sessionID = orgAdminSession(t, env)
	eventID, ticketTypeID = publishCheckoutEvent(t, env, sessionID, "Answer Fest", "answer-fest", 2000, 20)

	sizeQuestion = createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label":    "T-shirt size",
		"kind":     "short_text",
		"required": true,
	})
	mealQuestion = createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label":         "Meal",
		"kind":          "single_choice",
		"option_labels": []string{"Chicken", "Vegetarian"},
	})
	return sessionID, eventID, ticketTypeID, sizeQuestion, mealQuestion
}

// checkoutAnswer builds one entry of the checkout body's answer section.
func checkoutAnswer(ticketTypeID string, index int, questionID string, reply map[string]any) map[string]any {
	answer := map[string]any{
		"ticket_type_id":     ticketTypeID,
		"ticket_index":       index,
		"ticket_question_id": questionID,
	}
	for key, value := range reply {
		answer[key] = value
	}
	return answer
}

// saleIDOfPayment returns the Ticket Sale a settled Payment produced.
func saleIDOfPayment(t *testing.T, env *testEnv, clientTransactionID string) string {
	t.Helper()
	status, saleID, _ := paymentRecord(t, env, clientTransactionID)
	if status != "approved" || saleID == nil {
		t.Fatalf("payment status=%q sale=%v, want an approved Payment with a Ticket Sale", status, saleID)
	}
	return *saleID
}

// saleTickets reads a Ticket Sale's Tickets with their questions and Answers,
// in the order the staff surface lists them (Ticket Type, then ordinal).
func saleTickets(t *testing.T, env *testEnv, sessionID, eventID, ticketSaleID string) []ticketAnswers {
	t.Helper()
	resp, body := env.get(t, ticketSaleTicketsPath(eventID, ticketSaleID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read sale tickets status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return decodeTicketAnswersList(t, body.Data)
}

// answerTo finds one Ticket's Answer to one question, or nil for an Outstanding
// Answer — which is the absence of a row and not a blank one.
func answerTo(t *testing.T, ticket ticketAnswers, questionID string) *string {
	t.Helper()
	for _, entry := range ticket.Questions {
		if entry.Question.ID != questionID {
			continue
		}
		if entry.Answer == nil {
			return nil
		}
		return entry.Answer.Text
	}
	t.Fatalf("ticket %d was never asked question %s", ticket.Ordinal, questionID)
	return nil
}

// TestCheckoutAnswersReachTheRightTickets is the acceptance criterion, whole: a
// cart of three of one Ticket Type presents three separate answer sets, and each
// one lands on its own Ticket, in form order.
//
// It is asserted by giving three DIFFERENT answers, because three identical ones
// would pass on an implementation that wrote the first onto all of them — which
// is precisely the bug `tickets.ordinal` exists to prevent.
func TestCheckoutAnswersReachTheRightTickets(t *testing.T) {
	env := setupTest(t)
	sessionID, eventID, ticketTypeID, size, meal := checkoutQuestionFixture(t, env)

	body := checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(ticketTypeID, 3))
	body["answers"] = []map[string]any{
		checkoutAnswer(ticketTypeID, 1, size.ID, map[string]any{"text": "S"}),
		checkoutAnswer(ticketTypeID, 2, size.ID, map[string]any{"text": "M"}),
		checkoutAnswer(ticketTypeID, 3, size.ID, map[string]any{"text": "L"}),
		checkoutAnswer(ticketTypeID, 2, meal.ID, map[string]any{"option_ids": []string{meal.Options[1].ID}}),
	}

	begun := beginCheckoutOK(t, env, "test-org", "answer-fest", body)
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	tickets := saleTickets(t, env, sessionID, eventID, saleIDOfPayment(t, env, begun.ClientTransactionID))

	if len(tickets) != 3 {
		t.Fatalf("sale has %d Tickets, want 3", len(tickets))
	}
	for i, want := range []string{"S", "M", "L"} {
		if tickets[i].Ordinal != i+1 {
			t.Fatalf("ticket %d has ordinal %d", i, tickets[i].Ordinal)
		}
		got := answerTo(t, tickets[i], size.ID)
		if got == nil || *got != want {
			t.Fatalf("ticket %d answered %v to the size question, want %q", tickets[i].Ordinal, got, want)
		}
	}

	// The choice Answer landed on the SECOND Ticket alone, with the identity of
	// the Option chosen and the words it read at the time.
	for _, ticket := range tickets {
		var chosen []string
		for _, entry := range ticket.Questions {
			if entry.Question.ID == meal.ID && entry.Answer != nil {
				for _, option := range entry.Answer.Options {
					chosen = append(chosen, option.Label)
				}
			}
		}
		if ticket.Ordinal == 2 {
			if len(chosen) != 1 || chosen[0] != "Vegetarian" {
				t.Fatalf("ticket 2 chose %v, want [Vegetarian]", chosen)
			}
			continue
		}
		if len(chosen) != 0 {
			t.Fatalf("ticket %d chose %v, want nothing — the meal question was answered for ticket 2 alone", ticket.Ordinal, chosen)
		}
	}
}

// Skipping every question completes the checkout exactly as it does today, and
// leaves every question outstanding. The required one is the point: `required`
// produces an Outstanding Answer and never a refusal (ADR 0044).
func TestCheckoutSkippingEveryQuestionCompletesUnchanged(t *testing.T) {
	env := setupTest(t)
	sessionID, eventID, ticketTypeID, size, meal := checkoutQuestionFixture(t, env)

	// No `answers` key at all, which is what a buyer who touched nothing sends.
	begun := beginCheckoutOK(t, env, "test-org", "answer-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(ticketTypeID, 2)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	tickets := saleTickets(t, env, sessionID, eventID, saleIDOfPayment(t, env, begun.ClientTransactionID))

	if len(tickets) != 2 {
		t.Fatalf("sale has %d Tickets, want 2", len(tickets))
	}
	for _, ticket := range tickets {
		for _, questionID := range []string{size.ID, meal.ID} {
			if got := answerTo(t, ticket, questionID); got != nil {
				t.Fatalf("ticket %d answered %q to a question nobody filled in", ticket.Ordinal, *got)
			}
		}
	}
}

// Nothing about an answer can refuse a checkout, from any angle. Every body here
// would be a 400 on the staff answering API and is a completed purchase here.
//
// This is the single most important test in the file: the day one of these
// starts returning anything but 201, ADR 0044 has been reversed by accident.
func TestCheckoutIsNeverRefusedOverAnAnswer(t *testing.T) {
	env := setupTest(t)
	_, _, ticketTypeID, size, meal := checkoutQuestionFixture(t, env)

	for _, tc := range []struct {
		name   string
		answer map[string]any
	}{
		{"a reply of the wrong shape for the kind", checkoutAnswer(ticketTypeID, 1, size.ID,
			map[string]any{"checked": true})},
		{"an empty reply", checkoutAnswer(ticketTypeID, 1, size.ID,
			map[string]any{"text": "   "})},
		{"an index past the quantity", checkoutAnswer(ticketTypeID, 9, size.ID,
			map[string]any{"text": "M"})},
		{"a zero index", checkoutAnswer(ticketTypeID, 0, size.ID,
			map[string]any{"text": "M"})},
		{"a question that does not exist", checkoutAnswer(ticketTypeID, 1,
			"11111111-1111-4111-8111-111111111111", map[string]any{"text": "M"})},
		{"a Ticket Type that is not in the cart", checkoutAnswer(
			"22222222-2222-4222-8222-222222222222", 1, size.ID, map[string]any{"text": "M"})},
		{"an Option belonging to nobody", checkoutAnswer(ticketTypeID, 1, meal.ID,
			map[string]any{"option_ids": []string{"33333333-3333-4333-8333-333333333333"}})},
		{"two Options on a single_choice question", checkoutAnswer(ticketTypeID, 1, meal.ID,
			map[string]any{"option_ids": []string{meal.Options[0].ID, meal.Options[1].ID}})},
		{"a reply naming no slot at all", checkoutAnswer(ticketTypeID, 1, size.ID,
			map[string]any{})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(ticketTypeID, 1))
			body["answers"] = []map[string]any{tc.answer}
			resp, envelope := beginCheckout(t, env, "test-org", "answer-fest", body)
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("begin checkout status=%d error=%+v, want 201 — no Answer may refuse a checkout", resp.StatusCode, envelope.Error)
			}
		})
	}
}

// One unusable answer must not take a usable one with it. The buyer who fills
// two fields and fat-fingers one keeps the other.
func TestCheckoutKeepsTheGoodAnswerBesideTheBadOne(t *testing.T) {
	env := setupTest(t)
	sessionID, eventID, ticketTypeID, size, meal := checkoutQuestionFixture(t, env)

	body := checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(ticketTypeID, 1))
	body["answers"] = []map[string]any{
		checkoutAnswer(ticketTypeID, 1, meal.ID, map[string]any{"option_ids": []string{"not-an-option"}}),
		checkoutAnswer(ticketTypeID, 1, size.ID, map[string]any{"text": "M"}),
	}

	begun := beginCheckoutOK(t, env, "test-org", "answer-fest", body)
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	tickets := saleTickets(t, env, sessionID, eventID, saleIDOfPayment(t, env, begun.ClientTransactionID))

	if got := answerTo(t, tickets[0], size.ID); got == nil || *got != "M" {
		t.Fatalf("the size answer = %v, want %q — a dropped answer must not take its neighbour", got, "M")
	}
	if got := answerTo(t, tickets[0], meal.ID); got != nil {
		t.Fatalf("the meal question was answered %q from an Option that does not exist", *got)
	}
}

// A Payment that fails produces no Tickets and therefore no Answers on any
// Ticket. The Answers held on the Payment are not the same thing as an Answer,
// which is a property of a Ticket that does not exist here.
func TestFailedPaymentLeavesNoAnsweredTickets(t *testing.T) {
	env := setupTest(t)
	_, _, ticketTypeID, size, _ := checkoutQuestionFixture(t, env)

	body := checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(ticketTypeID, 2))
	body["answers"] = []map[string]any{
		checkoutAnswer(ticketTypeID, 1, size.ID, map[string]any{"text": "S"}),
		checkoutAnswer(ticketTypeID, 2, size.ID, map[string]any{"text": "M"}),
	}
	begun := beginCheckoutOK(t, env, "test-org", "answer-fest", body)

	// The buyer's answers ARE on the Payment at this point: that is the whole
	// mechanism, and asserting it here is what makes the assertion below mean
	// "the commit wrote nothing" rather than "the capture never ran".
	if held := countHeldAnswers(t, env, begun.ClientTransactionID); held != 2 {
		t.Fatalf("the pending Payment holds %d Answers, want 2", held)
	}

	confirmCheckout(t, env, begun.ClientTransactionID, "declined")

	if status, saleID, _ := paymentRecord(t, env, begun.ClientTransactionID); status != "failed" || saleID != nil {
		t.Fatalf("payment status=%q sale=%v, want a failed Payment with no sale", status, saleID)
	}
	if tickets := countTickets(t, env); tickets != 0 {
		t.Fatalf("%d Tickets exist after a declined Payment, want 0", tickets)
	}
	if answers := countTicketAnswers(t, env); answers != 0 {
		t.Fatalf("%d Answers exist on Tickets after a declined Payment, want 0", answers)
	}
}

// A free checkout settles without a Payment Provider, inside the begin request
// (ADR 0017), and carries its answers through by exactly the same path.
//
// It is the case most easily broken by an implementation that captures answers
// after deciding what to do with the cart: there is no confirm leg here, so an
// answer held one statement too late is an answer held after the Tickets were
// already minted.
func TestFreeCheckoutCarriesItsAnswersThrough(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID := orgAdminSession(t, env)
	eventID, freeID := publishCheckoutEvent(t, env, sessionID, "Free Fest", "free-fest", 0, 10)
	size := createTicketQuestion(t, env, sessionID, eventID, freeID, map[string]any{
		"label":    "T-shirt size",
		"kind":     "short_text",
		"required": true,
	})

	body := checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(freeID, 2))
	body["answers"] = []map[string]any{
		checkoutAnswer(freeID, 1, size.ID, map[string]any{"text": "S"}),
		checkoutAnswer(freeID, 2, size.ID, map[string]any{"text": "XL"}),
	}
	result := beginCheckoutSettled(t, env, "test-org", "free-fest", "", body)
	approvedRef(t, result)

	tickets := saleTickets(t, env, sessionID, eventID, saleIDOfPayment(t, env, result.ClientTransactionID))
	if len(tickets) != 2 {
		t.Fatalf("free sale has %d Tickets, want 2", len(tickets))
	}
	for i, want := range []string{"S", "XL"} {
		if got := answerTo(t, tickets[i], size.ID); got == nil || *got != want {
			t.Fatalf("free ticket %d answered %v, want %q", tickets[i].Ordinal, got, want)
		}
	}
}

// With the flag closed — which is how the feature ships (ADR 0045) — a checkout
// carrying answers completes and captures NOTHING. No Answer may come into
// existence before the Privacy Policy describes the collection, and a body that
// arrives from a client which learned the shape elsewhere must not be a way
// round that.
func TestCheckoutCapturesNoAnswersWhileTheFlagIsOff(t *testing.T) {
	env := setupTest(t)
	// Authored with the flag open, then closed again: the questions exist, which
	// is the only way to reach the interesting half of this test. A deployment
	// where the flag has never been open has no questions to answer.
	sessionID, eventID, ticketTypeID, size, _ := checkoutQuestionFixture(t, env)
	sharedApp.CatalogService.WithTicketQuestions(false)
	sharedApp.SalesService.WithTicketQuestions(false)

	body := checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(ticketTypeID, 1))
	body["answers"] = []map[string]any{
		checkoutAnswer(ticketTypeID, 1, size.ID, map[string]any{"text": "M"}),
	}
	begun := beginCheckoutOK(t, env, "test-org", "answer-fest", body)
	if held := countHeldAnswers(t, env, begun.ClientTransactionID); held != 0 {
		t.Fatalf("the Payment holds %d Answers with the flag closed, want 0", held)
	}

	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	if answers := countTicketAnswers(t, env); answers != 0 {
		t.Fatalf("%d Answers were written with the flag closed, want 0", answers)
	}
	// And the sale itself is entirely ordinary: the Tickets are minted exactly as
	// they were before this ticket existed.
	saleID := saleIDOfPayment(t, env, begun.ClientTransactionID)
	enableTicketQuestions(t)
	if tickets := saleTickets(t, env, sessionID, eventID, saleID); len(tickets) != 1 {
		t.Fatalf("sale has %d Tickets, want 1", len(tickets))
	}
}

// countHeldAnswers counts the Answers a Payment is holding. SQL, because a
// Payment is not a sale and nothing exposes one — the same reason
// paymentRecord goes to SQL.
func countHeldAnswers(t *testing.T, env *testEnv, clientTransactionID string) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`
		SELECT COUNT(*)
		FROM payment_ticket_answers a
		JOIN payment_lines l ON l.id = a.payment_line_id
		JOIN payments p ON p.id = l.payment_id
		WHERE p.client_transaction_id = $1
	`, clientTransactionID).Scan(&n); err != nil {
		t.Fatalf("count held answers: %v", err)
	}
	return n
}

// countTicketAnswers counts every Answer on every Ticket in the database.
//
// Deliberately unscoped: the assertions it serves are "nothing was written
// anywhere", and scoping it to one sale would let an Answer written onto the
// wrong Ticket pass.
func countTicketAnswers(t *testing.T, env *testEnv) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM ticket_answers`).Scan(&n); err != nil {
		t.Fatalf("count ticket answers: %v", err)
	}
	return n
}

// countTickets counts every Ticket in the database, on the same terms.
func countTickets(t *testing.T, env *testEnv) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM tickets`).Scan(&n); err != nil {
		t.Fatalf("count tickets: %v", err)
	}
	return n
}
