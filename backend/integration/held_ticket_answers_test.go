package integration

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"
)

// ONLY THE HOLDER ANSWERS (#343, parent #342, ADR 0049): a signed-in Customer
// lists the Tickets they hold — the Self-held Ticket of their own purchase, or a
// Ticket they accepted by Assignment Link — and answers one Ticket Question on
// one of them. "Holds" is the only authorisation concept: the holder customer
// id on the Ticket row. A Ticket on the caller's own Sale that somebody else
// holds, and a Ticket that does not exist, get one and the same reply.
//
// The tests run at the HTTP seam (docs/testing.md). Every property here is
// about the bytes that leave the process — which Tickets are in the list, what
// a Holder's row does and does not carry — so the disclosure assertions search
// the RAW payload rather than a decoded struct, which would silently drop a
// field somebody had started sending.

const heldTicketsPath = "/api/v1/customer/held-tickets"

func heldAnswerPath(ticketID, questionID string) string {
	return heldTicketsPath + "/" + ticketID + "/answers/" + questionID
}

// heldTicket decodes one row of the held-ticket list. Deliberately without a
// Sale, a price, an ordinal or a buyer: the shape is the same whether the
// reader bought the Ticket or accepted it, and carries nothing about the Sale.
type heldTicket struct {
	TicketID          string `json:"ticket_id"`
	EventName         string `json:"event_name"`
	EventSlug         string `json:"event_slug"`
	TicketTypeName    string `json:"ticket_type_name"`
	Answerable        bool   `json:"answerable"`
	AnswerableRefusal string `json:"answerable_refusal"`
	OutstandingCount  int    `json:"outstanding_count"`
	Questions         []struct {
		Question ticketQuestion `json:"question"`
		Answer   *holderAnswer  `json:"answer"`
	} `json:"questions"`
}

func decodeHeldTickets(t *testing.T, data json.RawMessage) []heldTicket {
	t.Helper()
	var tickets []heldTicket
	if err := json.Unmarshal(data, &tickets); err != nil {
		t.Fatalf("decode held tickets: %v", err)
	}
	return tickets
}

func listHeldTickets(t *testing.T, env *testEnv, session string) ([]heldTicket, []byte) {
	t.Helper()
	resp, body := env.get(t, heldTicketsPath, authHeader(session))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("held tickets status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("held tickets error=%+v, want none", body.Error)
	}
	return decodeHeldTickets(t, body.Data), body.Data
}

func answerHeldTicket(t *testing.T, env *testEnv, session, ticketID, questionID string, reply map[string]any) (*http.Response, envelope) {
	t.Helper()
	return env.put(t, heldAnswerPath(ticketID, questionID), reply, authHeader(session))
}

func answerHeldTicketOK(t *testing.T, env *testEnv, session, ticketID, questionID string, reply map[string]any) (heldTicket, []byte) {
	t.Helper()
	resp, body := answerHeldTicket(t, env, session, ticketID, questionID, reply)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("answer held ticket status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("answer held ticket error=%+v, want none", body.Error)
	}
	var ticket heldTicket
	if err := json.Unmarshal(body.Data, &ticket); err != nil {
		t.Fatalf("decode held ticket: %v", err)
	}
	return ticket, body.Data
}

func heldAnswerFor(t *testing.T, ticket heldTicket, questionID string) *holderAnswer {
	t.Helper()
	for _, pair := range ticket.Questions {
		if pair.Question.ID == questionID {
			return pair.Answer
		}
	}
	t.Fatalf("question %s is not on the held Ticket payload", questionID)
	return nil
}

// jsonKeys lists the top-level keys of one JSON object, sorted — the shape of a
// row, independent of its values.
func jsonKeys(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("decode object: %v", err)
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// heldTicketsFixture is one published Event sold ONLINE, so that the buyer
// holds one Ticket by paying (ADR 0048): Ana buys two GA Tickets, holds the
// first and assigns the second to Carla, who accepts. A required question and
// an optional one, so that the outstanding count has something to count.
type heldTicketsFixture struct {
	staffSession  string
	eventID       string
	ticketTypeID  string
	sizeQuestion  ticketQuestion
	extraQuestion ticketQuestion

	anaSaleID    string
	anaRef       string
	selfHeldID   string // Ana's own, accepted by purchase
	assignedID   string // the one Ana hands to Carla
	ana          string
	carla        string
	carlaAccepts bool
}

func newHeldTicketsFixture(t *testing.T, env *testEnv, carlaAccepts bool) heldTicketsFixture {
	t.Helper()
	enableTicketQuestions(t)
	enableTicketAssignment(t)

	f := heldTicketsFixture{carlaAccepts: carlaAccepts}
	f.staffSession = orgAdminSession(t, env)
	f.eventID, f.ticketTypeID = publishCheckoutEvent(t, env, f.staffSession, "Held Fest", "held-fest", 2000, 20)
	f.sizeQuestion = createTicketQuestion(t, env, f.staffSession, f.eventID, f.ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})
	f.extraQuestion = createTicketQuestion(t, env, f.staffSession, f.eventID, f.ticketTypeID, map[string]any{
		"label": "Anything else?", "kind": "long_text", "required": false,
	})

	begun := beginCheckoutOK(t, env, "test-org", "held-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(f.ticketTypeID, 2)))
	confirmed := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	f.anaRef = confirmed.ConfirmationRef
	f.anaSaleID = saleIDOfPayment(t, env, begun.ClientTransactionID)
	ids := ticketIDsOfSale(t, env, f.anaSaleID)
	if len(ids) != 2 {
		t.Fatalf("Ana's sale minted %d Tickets, want 2", len(ids))
	}
	f.selfHeldID, f.assignedID = ids[0], ids[1]

	f.ana = customerSignIn(t, env, "ana@example.com")
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.assignedID, "carla@example.com")
	if carlaAccepts {
		acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))
	}
	f.carla = customerSignIn(t, env, "carla@example.com")
	return f
}

// THE BUYER ANSWERS THEIR SELF-HELD TICKET, AND ONLY THAT ONE. The held list
// names the one Ticket the buyer holds by paying, with its questions and its
// Outstanding Answer count; writing an Answer returns the Ticket with the debt
// discharged; and the Sale's other Ticket — Ana's to assign, not to answer —
// is nowhere on it.
func TestTheBuyerListsAndAnswersTheirSelfHeldTicket(t *testing.T) {
	env := setupTest(t)
	f := newHeldTicketsFixture(t, env, false)

	held, raw := listHeldTickets(t, env, f.ana)
	if len(held) != 1 {
		t.Fatalf("Ana holds %d Tickets, want her one Self-held Ticket: %s", len(held), raw)
	}
	own := held[0]
	if own.TicketID != f.selfHeldID || own.EventName != "Held Fest" || own.EventSlug != "held-fest" || own.TicketTypeName != "GA" {
		t.Errorf("the held row reads %+v", own)
	}
	if !own.Answerable || own.AnswerableRefusal != "" {
		t.Errorf("answerable=%v refusal=%q before the doors open", own.Answerable, own.AnswerableRefusal)
	}
	if own.OutstandingCount != 1 || len(own.Questions) != 2 {
		t.Errorf("outstanding=%d questions=%d, want 1 owed of 2 asked", own.OutstandingCount, len(own.Questions))
	}
	if strings.Contains(string(raw), f.assignedID) {
		t.Error("the Ticket Ana assigned away is on her held list")
	}

	answered, _ := answerHeldTicketOK(t, env, f.ana, f.selfHeldID, f.sizeQuestion.ID, map[string]any{"text": "M"})
	if answered.TicketID != f.selfHeldID || answered.OutstandingCount != 0 {
		t.Errorf("after answering: ticket=%s outstanding=%d", answered.TicketID, answered.OutstandingCount)
	}
	if a := heldAnswerFor(t, answered, f.sizeQuestion.ID); a == nil || a.Text == nil || *a.Text != "M" {
		t.Errorf("the write did not come back on the row: %+v", a)
	}

	// A correction is the same request with a different body, and the list
	// reads what was last said.
	answerHeldTicketOK(t, env, f.ana, f.selfHeldID, f.sizeQuestion.ID, map[string]any{"text": "L"})
	held, _ = listHeldTickets(t, env, f.ana)
	if a := heldAnswerFor(t, held[0], f.sizeQuestion.ID); a == nil || a.Text == nil || *a.Text != "L" {
		t.Errorf("the list reads %+v, want the corrected Answer", a)
	}

	// Event Staff see the same Answer on the same Ticket: one write path.
	for _, ticket := range saleTickets(t, env, f.staffSession, f.eventID, f.anaSaleID) {
		if ticket.TicketID == f.selfHeldID {
			if got := answerTo(t, ticket, f.sizeQuestion.ID); got == nil || *got != "L" {
				t.Errorf("staff read %v on the buyer's own Ticket", got)
			}
		}
	}
}

// A CONFIRMATION LINK SESSION COUNTS AS THE BUYER for the Self-held Ticket — the
// forwarded receipt lands on the same page — and sees only that Sale's.
func TestAConfirmationLinkSessionHoldsTheSelfHeldTicketOfItsOwnSale(t *testing.T) {
	env := setupTest(t)
	f := newHeldTicketsFixture(t, env, true)

	// A second purchase by Ana, with its own Self-held Ticket.
	begun := beginCheckoutOK(t, env, "test-org", "held-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(f.ticketTypeID, 1)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	secondSaleID := saleIDOfPayment(t, env, begun.ClientTransactionID)
	secondHeld := ticketIDsOfSale(t, env, secondSaleID)[0]

	// The full session holds both.
	held, _ := listHeldTickets(t, env, f.ana)
	if len(held) != 2 {
		t.Fatalf("Ana's full session holds %d Tickets, want 2", len(held))
	}

	// The receipt's session holds the one on the Sale it names.
	_, linkSession := redeemConfirmationLinkOK(t, env, confirmationLinkTokenForRef(t, env, f.anaRef), "")
	held, _ = listHeldTickets(t, env, linkSession)
	if len(held) != 1 || held[0].TicketID != f.selfHeldID {
		t.Fatalf("the link session holds %+v, want only the Self-held Ticket of its own Sale", held)
	}
	answerHeldTicketOK(t, env, linkSession, f.selfHeldID, f.sizeQuestion.ID, map[string]any{"text": "S"})

	// And the other Sale's Self-held Ticket is not found from it.
	resp, body := answerHeldTicket(t, env, linkSession, secondHeld, f.sizeQuestion.ID, map[string]any{"text": "S"})
	assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_NOT_FOUND")
}

// A HOLDER ANSWERS THE TICKET THEY ACCEPTED THROUGH THE SAME ROUTE, and the row
// is the same shape as the buyer's: the two are indistinguishable to this
// surface, because "held" is the only thing it knows. And the Holder's row
// carries NOTHING about the purchase — no buyer, no price, no Tax ID, no Sale
// Confirmation reference, no other Ticket — which is the Assignment Link's
// disclosure rule holding in the Customer Area (CONTEXT.md, Holder).
func TestAHolderAnswersTheirAcceptedTicketThroughTheSameRouteAndSeesNothingOfTheSale(t *testing.T) {
	env := setupTest(t)
	f := newHeldTicketsFixture(t, env, true)

	carlasHeld, raw := listHeldTickets(t, env, f.carla)
	if len(carlasHeld) != 1 || carlasHeld[0].TicketID != f.assignedID {
		t.Fatalf("Carla holds %+v, want the one Ticket she accepted", carlasHeld)
	}
	if carlasHeld[0].EventName != "Held Fest" || carlasHeld[0].TicketTypeName != "GA" || carlasHeld[0].OutstandingCount != 1 {
		t.Errorf("Carla's row reads %+v", carlasHeld[0])
	}

	anasHeld, anasRaw := listHeldTickets(t, env, f.ana)
	if len(anasHeld) != 1 {
		t.Fatalf("Ana holds %d Tickets", len(anasHeld))
	}
	var anaRows, carlaRows []json.RawMessage
	if err := json.Unmarshal(anasRaw, &anaRows); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &carlaRows); err != nil {
		t.Fatal(err)
	}
	anaKeys, carlaKeys := jsonKeys(t, anaRows[0]), jsonKeys(t, carlaRows[0])
	if strings.Join(anaKeys, ",") != strings.Join(carlaKeys, ",") {
		t.Errorf("the buyer's held row has keys %v and the Holder's %v; a Self-held Ticket and an accepted one must be indistinguishable here",
			anaKeys, carlaKeys)
	}

	// Nothing about the Sale, in the list or in what the write returns.
	answered, answeredRaw := answerHeldTicketOK(t, env, f.carla, f.assignedID, f.sizeQuestion.ID, map[string]any{"text": "XS"})
	if answered.OutstandingCount != 0 {
		t.Errorf("outstanding=%d after Carla answered", answered.OutstandingCount)
	}
	for _, payload := range []string{string(raw), string(answeredRaw)} {
		lower := strings.ToLower(payload)
		for _, forbidden := range []string{
			f.anaRef, f.anaSaleID, f.selfHeldID, "ana@example.com", "lopez",
			"confirmation_ref", "ticket_sale_id", "amount", "price", "tax_id", "holder_email", "ordinal", "self_held", "customer_email",
		} {
			if strings.Contains(lower, strings.ToLower(forbidden)) {
				t.Errorf("the Holder's payload carries %q:\n%s", forbidden, payload)
			}
		}
	}

	// Both keys to distinguish a buyer's view must be absent from the buyer's
	// own held row too: the rows are one shape.
	for _, forbidden := range []string{"confirmation_ref", "ticket_sale_id", "answer_link", "holder_email"} {
		if strings.Contains(string(anasRaw), forbidden) {
			t.Errorf("the buyer's held row carries %q", forbidden)
		}
	}
}

// A TICKET THE CALLER DOES NOT HOLD IS NOT FOUND, exactly as one that does not
// exist: the buyer's own Sale's other Ticket — unassigned, assigned, or accepted
// by somebody else — a stranger's Ticket, an id belonging to nobody and a
// malformed id all get one reply. Trying ids here teaches nothing.
func TestANonHeldTicketIsNotFoundExactlyAsANonexistentOne(t *testing.T) {
	env := setupTest(t)
	f := newHeldTicketsFixture(t, env, false)
	reply := map[string]any{"text": "M"}

	resp, baseline := answerHeldTicket(t, env, f.ana, unownedSaleID, f.sizeQuestion.ID, reply)
	assertAPIError(t, resp, baseline, http.StatusNotFound, "TICKET_NOT_FOUND")

	probe := func(label, session, ticketID string) {
		t.Helper()
		resp, body := answerHeldTicket(t, env, session, ticketID, f.sizeQuestion.ID, reply)
		assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_NOT_FOUND")
		if body.Error.Message != baseline.Error.Message {
			t.Errorf("%s: message %q differs from a nonexistent Ticket's %q", label, body.Error.Message, baseline.Error.Message)
		}
	}

	// Assigned to Carla and not yet accepted: Ana owns the Sale, holds nothing.
	probe("assigned, unaccepted, on the buyer's own Sale", f.ana, f.assignedID)
	probe("malformed id", f.ana, "not-a-ticket")
	// Carla, who has accepted nothing, holds nothing either.
	probe("the named address before accepting", f.carla, f.assignedID)

	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))
	probe("accepted by somebody else, on the buyer's own Sale", f.ana, f.assignedID)
	probe("the buyer's Self-held Ticket, from the Holder", f.carla, f.selfHeldID)

	// And nothing was written by any of it.
	var count int
	if err := env.db.QueryRow(`SELECT count(*) FROM ticket_answers`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("%d Answers written by refused requests", count)
	}
}

// THE WRITE CLOSES AT THE DOORS AND ON A REVERSED SALE; THE READ NEVER DOES.
// After the Event starts the held list still reads, marked unanswerable, and a
// write is refused. The buyer keeps a reversed Sale's Self-held Ticket on their
// list — a Reversal voids a purchase, it does not erase what was answered —
// and may not write to it.
func TestHeldTicketWritesCloseAtTheDoorsAndOnAReversedSaleButReadsNever(t *testing.T) {
	env := setupTest(t)
	f := newHeldTicketsFixture(t, env, true)

	answerHeldTicketOK(t, env, f.ana, f.selfHeldID, f.sizeQuestion.ID, map[string]any{"text": "M"})
	answerHeldTicketOK(t, env, f.carla, f.assignedID, f.sizeQuestion.ID, map[string]any{"text": "S"})

	setEventStart(t, env, f.eventID, env.fixedClock.Add(-time.Hour))
	for _, who := range []struct{ session, ticketID string }{{f.ana, f.selfHeldID}, {f.carla, f.assignedID}} {
		resp, body := answerHeldTicket(t, env, who.session, who.ticketID, f.sizeQuestion.ID, map[string]any{"text": "L"})
		assertAPIError(t, resp, body, http.StatusConflict, "EVENT_STARTED_ANSWERS_CLOSED")
		held, _ := listHeldTickets(t, env, who.session)
		if len(held) != 1 || held[0].Answerable || held[0].AnswerableRefusal != "event_started" {
			t.Errorf("after the doors, the held list reads %+v", held)
		}
		if a := heldAnswerFor(t, held[0], f.sizeQuestion.ID); a == nil || a.Text == nil {
			t.Error("the Answer vanished when the doors opened")
		}
	}

	// Back in the future, and the Sale reversed instead. Done in SQL because
	// the customer's own reversal route is about money and its window, not
	// about this.
	setEventStart(t, env, f.eventID, env.fixedClock.Add(30*24*time.Hour))
	if _, err := env.db.Exec(`UPDATE ticket_sales SET status = 'reversed' WHERE id = $1`, f.anaSaleID); err != nil {
		t.Fatalf("reverse the Sale: %v", err)
	}
	resp, body := answerHeldTicket(t, env, f.ana, f.selfHeldID, f.sizeQuestion.ID, map[string]any{"text": "L"})
	assertAPIError(t, resp, body, http.StatusConflict, "TICKET_SALE_REVERSED")
	held, _ := listHeldTickets(t, env, f.ana)
	if len(held) != 1 || held[0].Answerable || held[0].AnswerableRefusal != "sale_reversed" {
		t.Errorf("after the reversal, the buyer's held list reads %+v", held)
	}
	if a := heldAnswerFor(t, held[0], f.sizeQuestion.ID); a == nil || a.Text == nil || *a.Text != "M" {
		t.Error("the reversal erased the buyer's Answer")
	}
}

// REASSIGNMENT AND SALE REVERSAL DROP THE TICKET FROM THE HOLDER'S LIST. A
// Holder may stop being one without warning (CONTEXT.md); the Ticket leaves
// their list and a write to it is not found — a reversed Sale is a fact about
// somebody else's money, and the refusal must not say so.
func TestReassignmentAndReversalDropTheTicketFromTheHoldersList(t *testing.T) {
	env := setupTest(t)
	f := newHeldTicketsFixture(t, env, true)
	answerHeldTicketOK(t, env, f.carla, f.assignedID, f.sizeQuestion.ID, map[string]any{"text": "S"})

	// Reassigned to Diego: gone from Carla, and Carla's Answer with it.
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.assignedID, "diego@example.com")
	held, _ := listHeldTickets(t, env, f.carla)
	if len(held) != 0 {
		t.Fatalf("Carla still holds %+v after the Ticket was reassigned", held)
	}
	resp, body := answerHeldTicket(t, env, f.carla, f.assignedID, f.sizeQuestion.ID, map[string]any{"text": "M"})
	assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_NOT_FOUND")

	// Diego accepts and holds it, with no Answer inherited from Carla.
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "diego@example.com")))
	diego := customerSignIn(t, env, "diego@example.com")
	held, _ = listHeldTickets(t, env, diego)
	if len(held) != 1 || held[0].TicketID != f.assignedID || held[0].OutstandingCount != 1 {
		t.Fatalf("Diego holds %+v, want the reassigned Ticket with its Answer cleared", held)
	}

	// The Sale is reversed: gone from Diego, and not found from him.
	if _, err := env.db.Exec(`UPDATE ticket_sales SET status = 'reversed' WHERE id = $1`, f.anaSaleID); err != nil {
		t.Fatalf("reverse the Sale: %v", err)
	}
	held, _ = listHeldTickets(t, env, diego)
	if len(held) != 0 {
		t.Fatalf("Diego still holds %+v after the Sale was reversed", held)
	}
	resp, body = answerHeldTicket(t, env, diego, f.assignedID, f.sizeQuestion.ID, map[string]any{"text": "M"})
	assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_NOT_FOUND")
	if strings.Contains(strings.ToLower(body.Error.Message), "revers") {
		t.Error("the refusal tells the Holder the Sale was reversed")
	}
}

// THE ROUTES ARE INVISIBLE WHILE THE TICKET QUESTIONS FLAG IS CLOSED (ADR 0045),
// and refuse a caller with no Customer Session.
func TestHeldTicketRoutesRespectTheFlagAndTheSession(t *testing.T) {
	env := setupTest(t)
	f := newHeldTicketsFixture(t, env, true)

	resp, body := env.get(t, heldTicketsPath, nil)
	assertAPIError(t, resp, body, http.StatusUnauthorized, "UNAUTHORIZED")
	resp, body = answerHeldTicket(t, env, "", f.selfHeldID, f.sizeQuestion.ID, map[string]any{"text": "M"})
	assertAPIError(t, resp, body, http.StatusUnauthorized, "UNAUTHORIZED")

	sharedApp.CatalogService.WithTicketQuestions(false)
	sharedApp.SalesService.WithTicketQuestions(false)
	resp, body = env.get(t, heldTicketsPath, authHeader(f.ana))
	assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_QUESTIONS_UNAVAILABLE")
	resp, body = answerHeldTicket(t, env, f.ana, f.selfHeldID, f.sizeQuestion.ID, map[string]any{"text": "M"})
	assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_QUESTIONS_UNAVAILABLE")
}

// AN ANSWER THAT DOES NOT FIT ITS QUESTION'S KIND IS REFUSED ON THIS ROUTE AS ON
// EVERY OTHER — one write path, one parser.
func TestAHeldTicketAnswerIsParsedByTheQuestionsKind(t *testing.T) {
	env := setupTest(t)
	f := newHeldTicketsFixture(t, env, true)

	resp, body := answerHeldTicket(t, env, f.carla, f.assignedID, f.sizeQuestion.ID, map[string]any{"number": "3"})
	assertAPIError(t, resp, body, http.StatusBadRequest, "INVALID_ANSWER")
	resp, body = answerHeldTicket(t, env, f.carla, f.assignedID, unownedSaleID, map[string]any{"text": "M"})
	assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_QUESTION_NOT_FOUND")
}
