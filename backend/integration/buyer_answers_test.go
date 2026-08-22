package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The buyer's own view of the Tickets of their Sale (#315, ADR 0044; narrowed by
// #344, ADR 0049): position, Ticket Type and assignment state for each, and
// NOTHING about what any of them has been asked or has answered.
//
// WHAT CHANGED AND WHY THE FILE IS STILL WEIGHTED THE WAY IT IS. This route used
// to carry every Ticket's questions, its Answers and a per-Ticket Answer Link —
// an unauthenticated write credential over somebody else's Ticket. ADR 0049
// retires all of that from the buyer: an Answer is given only by a Ticket's
// Holder (the buyer for their Self-held Ticket, through the held-ticket routes
// in held_ticket_answers_test.go) or by Event Staff. So the centre of this file
// is now a NEGATIVE assertion over the raw bytes — that no Answer, no
// outstanding count and no link leaves this route for any row — beside the
// isolation test that was always here.
//
// They run end to end through the HTTP API because every property here is about
// the bytes that leave the process.

// buyerTicket decodes one row of the buyer's own Tickets list.
//
// A NARROW STRUCT, AND THE NEGATIVE TESTS DELIBERATELY DO NOT RELY ON IT.
// Decoding drops any field the API started sending that nobody declared here,
// which is precisely the shape of the regression that would leak an Answer, so
// those assertions search the RAW payload for the forbidden keys and use this
// only for the positive half.
type buyerTicket struct {
	TicketID       string `json:"ticket_id"`
	Ordinal        int    `json:"ordinal"`
	TicketTypeName string `json:"ticket_type_name"`
	// The Ticket Assignment fields (#324), decoded here so that the buyer's list
	// is read by ONE struct across every file. They are absent from the payload
	// entirely while TICKET_ASSIGNMENT_ENABLED is closed, which is what makes
	// their zero values meaningful — see ticket_assignment_test.go, where the
	// flag test asserts their absence over the RAW bytes rather than through
	// this decode.
	AssignmentState   string  `json:"assignment_state"`
	HolderEmail       string  `json:"holder_email"`
	AssignedAt        *string `json:"assigned_at"`
	AcceptedAt        *string `json:"accepted_at"`
	SelfHeld          bool    `json:"self_held"`
	Assignable        bool    `json:"assignable"`
	AssignableRefusal string  `json:"assignable_refusal"`
}

// answerKeysNeverOnTheBuyersRow are the JSON keys ADR 0049 took off this
// payload. Asserted over the raw bytes, key by key, in every test that reads
// the list — a field that came back under any of these names would be the
// regression this file exists to catch.
var answerKeysNeverOnTheBuyersRow = []string{
	`"answer_link"`, `"questions"`, `"outstanding_count"`, `"answerable"`, `"answerable_refusal"`,
}

func buyerTicketsPath(ticketSaleID string) string {
	return "/api/v1/customer/ticket-sales/" + ticketSaleID + "/tickets"
}

// retiredBuyerAnswerPath is the sale-scoped answer write ADR 0049 removed. It is
// kept as a PATH and nothing else so that the tests can prove it is gone.
func retiredBuyerAnswerPath(ticketSaleID, ticketID, questionID string) string {
	return buyerTicketsPath(ticketSaleID) + "/" + ticketID + "/answers/" + questionID
}

func decodeBuyerTickets(t *testing.T, data json.RawMessage) []buyerTicket {
	t.Helper()
	var tickets []buyerTicket
	if err := json.Unmarshal(data, &tickets); err != nil {
		t.Fatalf("decode buyer tickets: %v", err)
	}
	return tickets
}

// listBuyerTickets reads the buyer's own Tickets, failing on any refusal — for
// the tests whose subject is what is IN the list rather than who may ask.
//
// EVERY READ THROUGH HERE CHECKS THE RAW BYTES FOR AN ANSWER, so a test in any
// file that lists the buyer's Tickets after answering one is also the test that
// the Answer did not come back with them.
func listBuyerTickets(t *testing.T, env *testEnv, customerSession, ticketSaleID string) []buyerTicket {
	t.Helper()
	resp, body := env.get(t, buyerTicketsPath(ticketSaleID), authHeader(customerSession))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("buyer tickets status=%d error=%+v", resp.StatusCode, body.Error)
	}
	assertNoAnswerOnTheBuyersRows(t, body.Data)
	return decodeBuyerTickets(t, body.Data)
}

// assertNoAnswerOnTheBuyersRows is ADR 0049 over the bytes: the sale-scoped
// list names no question, no Answer, no debt and no link, on any row.
func assertNoAnswerOnTheBuyersRows(t *testing.T, raw json.RawMessage) {
	t.Helper()
	for _, key := range answerKeysNeverOnTheBuyersRow {
		if strings.Contains(string(raw), key) {
			t.Errorf("the buyer's sale-scoped list carries %s.\n"+
				"Only the Holder answers (ADR 0049): questions and Answers travel on the held-ticket routes alone.\n"+
				"See service.BuyerTicketAnswersView.\nbody: %s", key, raw)
		}
	}
}

// staffAnswerText reads what one Ticket says in reply to one question, through
// the staff read — the one surface that still sees every Ticket's Answers.
// Nil when there is no Answer.
func staffAnswerText(t *testing.T, env *testEnv, staffSession, eventID, ticketID, questionID string) *string {
	t.Helper()
	view := decodeTicketAnswers(t, mustGetTicket(t, env, staffSession, eventID, ticketID))
	answer := answerFor(t, view, questionID)
	if answer == nil {
		return nil
	}
	return answer.Text
}

// ticketIDsOfSale reads a Ticket Sale's Ticket ids in ordinal order. SQL,
// because a Ticket id is not something any surface hands a test for free — and
// half of what is asserted below is about ids the caller was NOT given.
func ticketIDsOfSale(t *testing.T, env *testEnv, ticketSaleID string) []string {
	t.Helper()
	rows, err := env.db.Query(`
		SELECT tk.id
		FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		WHERE l.ticket_sale_id = $1
		ORDER BY tk.ordinal ASC
	`, ticketSaleID)
	if err != nil {
		t.Fatalf("read Tickets of sale %s: %v", ticketSaleID, err)
	}
	defer rows.Close()

	ids := make([]string, 0, 2)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan Ticket: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read Tickets of sale %s: %v", ticketSaleID, err)
	}
	return ids
}

// confirmationLinkTokenForRef finds the Confirmation Link of ONE named sale
// among everything the fixture posted.
//
// By reference rather than by "the last one captured", because these tests seed
// several sales for several people and the last confirmation to arrive is
// usually somebody else's — which would make a test about isolation pass by
// looking at the wrong email.
func confirmationLinkTokenForRef(t *testing.T, env *testEnv, ref string) string {
	t.Helper()
	for _, conf := range env.email.Confirmations() {
		if conf.Reference == ref {
			return confirmationLinkTokenFrom(t, conf.ConfirmationLink)
		}
	}
	t.Fatalf("no Sale Confirmation captured for reference %q", ref)
	return ""
}

// buyerAnswersFixture is one scheduled Event whose Ticket Type asks a required
// question and an optional one, sold TWICE: two Tickets to Ana, and one to
// Bruno, on separate Sale Import batches.
//
// TWO BUYERS IS THE WHOLE POINT OF THE FIXTURE. Almost every property in this
// file is "…and not the other person's", and a fixture with one Customer in it
// would let each of those assertions pass against an empty database.
//
// SEPARATE BATCHES because reversing is done by undoing a batch, and a single
// batch holding both sales would reverse Bruno's purchase as a side effect of a
// test about Ana's. ANA'S IS COMMITTED SECOND, because a Sale Import can only be
// undone while it is the most recent one — so the buyer whose Sale gets reversed
// has to be the last one through the door.
//
// The questions are one REQUIRED and one OPTIONAL because `required` is the only
// thing that produces an Outstanding Answer: a fixture asking two optional
// questions would make every outstanding-count assertion below pass for the
// wrong reason.
type buyerAnswersFixture struct {
	staffSession string
	eventID      string
	ticketTypeID string
	// sizeQuestion is required; extraQuestion is not.
	sizeQuestion  ticketQuestion
	extraQuestion ticketQuestion

	// Ana bought two Tickets on one Sale — the buyer this feature is written
	// for, who knows one t-shirt size and must forward the other.
	anaSaleID    string
	anaBatchID   string
	anaRef       string
	anaTicketIDs []string

	// Bruno is a different Customer entirely, on the same Event.
	brunoSaleID    string
	brunoRef       string
	brunoTicketIDs []string
}

func newBuyerAnswersFixture(t *testing.T, env *testEnv) buyerAnswersFixture {
	t.Helper()
	enableTicketQuestions(t)

	f := buyerAnswersFixture{}
	f.staffSession = orgAdminSession(t, env)
	f.eventID = createDraftEvent(t, env, f.staffSession, "Buyer Fest", "buyer-fest")
	// SCHEDULED, and comfortably in the future. The Answer Link's window closes
	// at the doors, so an Event with no start would be answerable for reasons
	// that have nothing to do with what is under test here.
	scheduleEvent(t, env, f.staffSession, f.eventID, "Buyer Fest", "buyer-fest",
		env.fixedClock.Add(30*24*time.Hour))
	f.ticketTypeID = createTicketTypeWithCapacity(t, env, f.staffSession, f.eventID, "GA", 2000, 50)

	f.sizeQuestion = createTicketQuestion(t, env, f.staffSession, f.eventID, f.ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})
	f.extraQuestion = createTicketQuestion(t, env, f.staffSession, f.eventID, f.ticketTypeID, map[string]any{
		"label": "Anything else we should know?", "kind": "long_text", "required": false,
	})

	commitBatch(t, env, f.staffSession, f.eventID, "buyer-bruno", []map[string]any{{
		"customer_email": "bruno@example.com", "customer_first_name": "Bruno", "customer_last_name": "Diaz",
		"ticket_type_id": f.ticketTypeID, "quantity": 1, "payment_method": "cash",
		"sold_at": "2026-07-01T10:00:00Z",
	}})
	f.anaBatchID = commitBatch(t, env, f.staffSession, f.eventID, "buyer-ana", []map[string]any{{
		"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
		"ticket_type_id": f.ticketTypeID, "quantity": 2, "payment_method": "cash",
		"sold_at": "2026-07-02T10:00:00Z",
	}})

	if err := env.db.QueryRow(`
		SELECT id, confirmation_ref FROM ticket_sales WHERE customer_email = 'ana@example.com'
	`).Scan(&f.anaSaleID, &f.anaRef); err != nil {
		t.Fatalf("read Ana's Ticket Sale: %v", err)
	}
	if err := env.db.QueryRow(`
		SELECT id, confirmation_ref FROM ticket_sales WHERE customer_email = 'bruno@example.com'
	`).Scan(&f.brunoSaleID, &f.brunoRef); err != nil {
		t.Fatalf("read Bruno's Ticket Sale: %v", err)
	}

	f.anaTicketIDs = ticketIDsOfSale(t, env, f.anaSaleID)
	f.brunoTicketIDs = ticketIDsOfSale(t, env, f.brunoSaleID)
	if len(f.anaTicketIDs) != 2 || len(f.brunoTicketIDs) != 1 {
		t.Fatalf("Tickets minted = %d for Ana / %d for Bruno, want 2 and 1",
			len(f.anaTicketIDs), len(f.brunoTicketIDs))
	}
	return f
}

// unownedSaleID is a well-formed Ticket Sale id belonging to nobody. It is the
// baseline the malformed-id test compares against, and the id a caller probing
// this route would be sending.
const unownedSaleID = "11111111-1111-4111-8111-111111111111"

// THE BUYER GETS EVERY TICKET OF THEIR OWN SALE, EACH NAMED BY POSITION AND
// TICKET TYPE, AND NOT ONE OF THEM SAYS WHAT IT WAS ASKED. The ordinal is the
// buyer's only handle on which of two identical Tickets is which until an
// address is given; the questions are the Holder's business, and the buyer
// holds none of these — a Sale Import hands the buyer no Ticket (ADR 0048).
//
// Answered both BEFORE and AFTER Event Staff write an Answer on one of them,
// because "no questions on an unanswered Ticket" could be a payload that is
// empty for the wrong reason.
func TestBuyerListsEveryTicketOfTheirOwnSaleAndSeesNoAnswersOnAny(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	ana := customerSignIn(t, env, "ana@example.com")

	tickets := listBuyerTickets(t, env, ana, f.anaSaleID)
	if len(tickets) != 2 {
		t.Fatalf("buyer sees %d Tickets, want both of their own Sale's", len(tickets))
	}
	for i, ticket := range tickets {
		if ticket.TicketID != f.anaTicketIDs[i] {
			t.Fatalf("row %d is Ticket %s, want %s in ordinal order", i, ticket.TicketID, f.anaTicketIDs[i])
		}
		if ticket.Ordinal != i+1 {
			t.Errorf("row %d ordinal = %d, want %d", i, ticket.Ordinal, i+1)
		}
		if ticket.TicketTypeName != "GA" {
			t.Errorf("row %d ticket_type_name = %q, want GA", i, ticket.TicketTypeName)
		}
	}

	// Staff answer the first Ticket. The buyer's list must not know.
	putAnswer(t, env, f.staffSession, f.eventID, f.anaTicketIDs[0], f.sizeQuestion.ID,
		map[string]any{"text": "XL"})
	resp, body := env.get(t, buyerTicketsPath(f.anaSaleID), authHeader(ana))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("re-read status=%d error=%+v", resp.StatusCode, body.Error)
	}
	assertNoAnswerOnTheBuyersRows(t, body.Data)
	if strings.Contains(string(body.Data), "XL") || strings.Contains(string(body.Data), "T-shirt") {
		t.Fatalf("the buyer's list carries the Answer or the question Event Staff wrote on a Ticket they do not hold: %s", body.Data)
	}
}

// THE SALE-SCOPED ANSWER WRITE IS GONE, and its absence does not depend on who
// is asking or on what they name.
//
// A route that still existed but refused would be a route somebody could
// re-open with one line; the test is that the path answers as any unknown path
// does, for the Sale's owner, for a stranger and for a Ticket nobody owns alike,
// and that nothing was written by any of the attempts.
func TestTheSaleScopedAnswerWriteNoLongerExists(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	ana := customerSignIn(t, env, "ana@example.com")
	bruno := customerSignIn(t, env, "bruno@example.com")

	for name, tc := range map[string]struct {
		path    string
		session string
	}{
		"the buyer, on a Ticket of their own Sale": {
			retiredBuyerAnswerPath(f.anaSaleID, f.anaTicketIDs[0], f.sizeQuestion.ID), ana},
		"another Customer, on that same Ticket": {
			retiredBuyerAnswerPath(f.anaSaleID, f.anaTicketIDs[0], f.sizeQuestion.ID), bruno},
		"a Ticket id belonging to nobody": {
			retiredBuyerAnswerPath(f.brunoSaleID, "22222222-2222-4222-8222-222222222222", f.sizeQuestion.ID), bruno},
	} {
		t.Run(name, func(t *testing.T) {
			// A RAW REQUEST, not env.put: a path that is no longer routed is
			// answered by the mux itself, outside the envelope, and decoding
			// it as one would fail for the wrong reason.
			req, err := http.NewRequest(http.MethodPut, env.server.URL+tc.path, strings.NewReader(`{"text":"XL"}`))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+tc.session)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do request: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status=%d, want 404 — the sale-scoped answer write was retired by ADR 0049", resp.StatusCode)
			}
		})
	}

	var written int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM ticket_answers`).Scan(&written); err != nil {
		t.Fatalf("count Answers: %v", err)
	}
	if written != 0 {
		t.Fatalf("%d Answers were written through a route that no longer exists", written)
	}
}

// A TICKET THE BUYER DOES NOT HOLD CANNOT BE ANSWERED BY THEM ANYWHERE. The
// one remaining customer write is the held-ticket route, and a Ticket of the
// buyer's own Sale that they do not hold is NOT FOUND there — the same answer a
// stranger's Ticket and an invented id get, so that the buyer's own Sale gives
// them no purchase on its other rows.
//
// A Sale Import hands the buyer no Ticket (ADR 0048), so every Ticket of Ana's
// Sale is one she does not hold, which is exactly the case under test.
func TestABuyerCannotAnswerATicketOfTheirOwnSaleThatTheyDoNotHold(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	enableTicketAssignment(t)
	ana := customerSignIn(t, env, "ana@example.com")

	for name, ticketID := range map[string]string{
		"the first Ticket of the buyer's own Sale":  f.anaTicketIDs[0],
		"the second Ticket of the buyer's own Sale": f.anaTicketIDs[1],
		"another Customer's Ticket":                 f.brunoTicketIDs[0],
		"a Ticket id belonging to nobody":           "22222222-2222-4222-8222-222222222222",
	} {
		t.Run(name, func(t *testing.T) {
			resp, body := answerHeldTicket(t, env, ana, ticketID, f.sizeQuestion.ID, map[string]any{"text": "XL"})
			assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_NOT_FOUND")
		})
	}
	if held, _ := listHeldTickets(t, env, ana); len(held) != 0 {
		t.Fatalf("the buyer of a Sale Import holds %d Tickets, want none: %+v", len(held), held)
	}

	var written int
	if err := env.db.QueryRow(`
		SELECT COUNT(*) FROM ticket_answers WHERE ticket_id = ANY($1)
	`, f.anaTicketIDs).Scan(&written); err != nil {
		t.Fatalf("count Answers on Ana's Tickets: %v", err)
	}
	if written != 0 {
		t.Fatalf("%d Answers were written on Tickets the buyer does not hold", written)
	}
}

// ONE CUSTOMER NEVER SEES ANOTHER'S TICKETS. A stranger's sale id is answered
// with an empty list rather than a refusal, so that which ids are real cannot be
// learned by watching which of them refuse differently — and the raw payload is
// searched for every id that would have told them.
func TestOneCustomerNeverSeesAnotherCustomersTickets(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)

	ana := customerSignIn(t, env, "ana@example.com")
	if anaTickets := listBuyerTickets(t, env, ana, f.anaSaleID); len(anaTickets) != 2 {
		t.Fatalf("Ana's own read = %+v, want two Tickets", anaTickets)
	}

	bruno := customerSignIn(t, env, "bruno@example.com")
	resp, body := env.get(t, buyerTicketsPath(f.anaSaleID), authHeader(bruno))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200 with an empty list; a refusal here confirms the id is real (error=%+v)",
			resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("error=%+v, want none — a stranger's sale id is answered, not refused", body.Error)
	}
	if tickets := decodeBuyerTickets(t, body.Data); len(tickets) != 0 {
		t.Fatalf("Bruno sees %d of Ana's Tickets, want none: %+v", len(tickets), tickets)
	}

	raw := string(body.Data)
	for what, secret := range map[string]string{
		"Ana's first Ticket id":  f.anaTicketIDs[0],
		"Ana's second Ticket id": f.anaTicketIDs[1],
		"Ana's Ticket Sale id":   f.anaSaleID,
		"Ana's confirmation ref": f.anaRef,
	} {
		if strings.Contains(raw, secret) {
			t.Errorf("another Customer's read discloses %s (%q).\n"+
				"See repository.ListAnswerableTicketsForBuyer and its customer_id clause.\nbody: %s",
				what, secret, raw)
		}
	}

	own := listBuyerTickets(t, env, bruno, f.brunoSaleID)
	if len(own) != 1 || own[0].TicketID != f.brunoTicketIDs[0] {
		t.Fatalf("Bruno's own read = %+v, want his one Ticket", own)
	}
	if len(listBuyerTickets(t, env, ana, f.anaSaleID)) != 2 {
		t.Fatal("Ana's own Tickets went missing while Bruno was probing")
	}
}

// A CONFIRMATION LINK SESSION REACHES ONLY THE SALE IT NAMES — not the same
// Customer's other purchases, and not anybody else's. The narrowing can only
// ever narrow: a full session still sees everything.
func TestAConfirmationLinkSessionReachesOnlyTheSaleItNames(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)

	commitBatch(t, env, f.staffSession, f.eventID, "buyer-ana-second", []map[string]any{{
		"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
		"ticket_type_id": f.ticketTypeID, "quantity": 1, "payment_method": "cash",
		"sold_at": "2026-07-03T10:00:00Z",
	}})
	var secondSaleID string
	if err := env.db.QueryRow(`
		SELECT id FROM ticket_sales WHERE customer_email = 'ana@example.com' AND id <> $1
	`, f.anaSaleID).Scan(&secondSaleID); err != nil {
		t.Fatalf("read Ana's second Ticket Sale: %v", err)
	}

	_, linkSession := redeemConfirmationLinkOK(t, env, confirmationLinkTokenForRef(t, env, f.anaRef), "")

	if linked := listBuyerTickets(t, env, linkSession, f.anaSaleID); len(linked) != 2 {
		t.Fatalf("link session sees %d Tickets of the sale it names, want 2", len(linked))
	}
	if others := listBuyerTickets(t, env, linkSession, secondSaleID); len(others) != 0 {
		t.Fatalf("a Confirmation Link session reached %d Tickets of the Customer's OTHER sale: %+v",
			len(others), others)
	}
	if others := listBuyerTickets(t, env, linkSession, f.brunoSaleID); len(others) != 0 {
		t.Fatalf("a Confirmation Link session reached %d Tickets of another Customer's sale", len(others))
	}

	full := customerSignIn(t, env, "ana@example.com")
	if tickets := listBuyerTickets(t, env, full, secondSaleID); len(tickets) != 1 {
		t.Fatalf("a full session sees %d Tickets of Ana's second sale, want 1", len(tickets))
	}
	if tickets := listBuyerTickets(t, env, full, f.anaSaleID); len(tickets) != 2 {
		t.Fatalf("a full session sees %d Tickets of Ana's first sale, want 2", len(tickets))
	}
}

// A REVERSED SALE KEEPS ITS PLACE IN THE CUSTOMER AREA: its Tickets are still
// listed, and the Answers that were given before the Reversal are still on the
// record — visible to Event Staff, who are the record's keepers, and still not
// to the buyer, for whom a Reversal changes nothing about what they may see.
func TestAReversedSaleKeepsItsTicketsOnTheBuyersList(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	ana := customerSignIn(t, env, "ana@example.com")

	putAnswer(t, env, f.staffSession, f.eventID, f.anaTicketIDs[0], f.sizeQuestion.ID,
		map[string]any{"text": "XL"})
	undoBatch(t, env, f.staffSession, f.eventID, f.anaBatchID)

	tickets := listBuyerTickets(t, env, ana, f.anaSaleID)
	if len(tickets) != 2 {
		t.Fatalf("a reversed Sale lists %d Tickets, want both still there", len(tickets))
	}
	if got := staffAnswerText(t, env, f.staffSession, f.eventID, f.anaTicketIDs[0], f.sizeQuestion.ID); got == nil || *got != "XL" {
		t.Fatalf("the Answer given before the Reversal reads as %v — reversing a sale erased what a Ticket said", got)
	}
}

// THE ROUTE IS INVISIBLE WHILE THE FLAG IS OFF, answering exactly as a build
// that never had the feature (ADR 0045).
func TestBuyerTicketRoutesAreInvisibleWhileTheFlagIsOff(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	ana := customerSignIn(t, env, "ana@example.com")

	sharedApp.CatalogService.WithTicketQuestions(false)
	sharedApp.SalesService.WithTicketQuestions(false)

	resp, body := env.get(t, buyerTicketsPath(f.anaSaleID), authHeader(ana))
	assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_QUESTIONS_UNAVAILABLE")
}

// NO CUSTOMER SESSION, NO LIST — and a staff session is not a Customer Session.
func TestBuyerTicketRoutesRefuseACallerWithNoCustomerSession(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)

	for name, tc := range map[string]struct {
		headers map[string]string
		code    string
	}{
		"no Authorization header":  {nil, "UNAUTHORIZED"},
		"an invented bearer token": {authHeader("not-a-session"), "CUSTOMER_SESSION_NOT_FOUND"},
	} {
		t.Run(name, func(t *testing.T) {
			resp, body := env.get(t, buyerTicketsPath(f.anaSaleID), tc.headers)
			assertAPIError(t, resp, body, http.StatusUnauthorized, tc.code)
		})
	}

	resp, body := env.get(t, buyerTicketsPath(f.anaSaleID), authHeader(f.staffSession))
	assertAPIError(t, resp, body, http.StatusUnauthorized, "CUSTOMER_SESSION_NOT_FOUND")
}

// A MALFORMED SALE ID IS ANSWERED EXACTLY AS ONE NOBODY OWNS — the same status
// and the same bytes — so that the shape of an id cannot be learned by watching
// which ones are refused differently.
func TestAMalformedTicketSaleIdIsAnsweredExactlyAsOneNobodyOwns(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	ana := customerSignIn(t, env, "ana@example.com")

	unownedResp, unowned := env.get(t, buyerTicketsPath(unownedSaleID), authHeader(ana))
	if unownedResp.StatusCode != http.StatusOK {
		t.Fatalf("a well-formed unowned id status=%d error=%+v", unownedResp.StatusCode, unowned.Error)
	}
	if tickets := decodeBuyerTickets(t, unowned.Data); len(tickets) != 0 {
		t.Fatalf("a well-formed unowned id returned %d Tickets", len(tickets))
	}

	for _, malformed := range []string{
		"not-a-uuid",
		"0",
		f.anaRef,
		unownedSaleID + "0",
	} {
		t.Run(malformed, func(t *testing.T) {
			resp, body := env.get(t, buyerTicketsPath(malformed), authHeader(ana))
			if resp.StatusCode != unownedResp.StatusCode {
				t.Fatalf("status=%d for a malformed id, want %d — the same answer an unowned id gets (error=%+v)",
					resp.StatusCode, unownedResp.StatusCode, body.Error)
			}
			if string(body.Data) != string(unowned.Data) {
				t.Fatalf("malformed data=%s unowned data=%s — the two must be indistinguishable",
					body.Data, unowned.Data)
			}
		})
	}

	if tickets := listBuyerTickets(t, env, ana, f.anaSaleID); len(tickets) != 2 {
		t.Fatalf("the real sale id returned %d Tickets, want 2", len(tickets))
	}
}
