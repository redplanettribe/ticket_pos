package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The buyer distributes the Answer Links (#315, ADR 0044): the Customer's own
// view of the Tickets they bought, each with its Ticket Questions, whatever has
// been answered so far, and the per-Ticket Answer Link to forward to whoever
// will actually be using that ticket.
//
// THIS IS THE ONLY SURFACE ON THE PLATFORM THAT HANDS OUT ANSWER LINKS, which is
// what makes this file different in kind from its neighbours. Everywhere else, a
// scoping bug discloses a row: somebody learns a name, a reference, a price.
// Here a scoping bug hands over a durable, unauthenticated WRITE CREDENTIAL over
// a stranger's ticket, good until that Event's doors open. So the tests below
// are weighted accordingly — the isolation test is the centre of the file and
// everything else is arranged around it.
//
// They run end to end through the HTTP API because every property here is about
// the bytes that leave the process: which tickets are in the list, and whether
// an `answer_link` is in the row. Neither can be asserted anywhere further in.

// buyerTicket decodes one row of the buyer's own Tickets list.
//
// A NARROW STRUCT, AND THE ISOLATION TEST DELIBERATELY DOES NOT RELY ON IT.
// Decoding drops any field the API started sending that nobody declared here,
// which is precisely the shape of the regression that would leak a link, so the
// isolation assertions search the RAW payload for the forbidden strings and use
// this only for the positive half.
type buyerTicket struct {
	TicketID       string `json:"ticket_id"`
	Ordinal        int    `json:"ordinal"`
	TicketTypeName string `json:"ticket_type_name"`
	// AnswerLink is the field this whole feature exists to produce, and the one
	// that must be EMPTY the moment the Ticket can no longer be answered.
	AnswerLink        string `json:"answer_link"`
	Answerable        bool   `json:"answerable"`
	AnswerableRefusal string `json:"answerable_refusal"`
	OutstandingCount  int    `json:"outstanding_count"`
	// The Ticket Assignment fields (#324), decoded here so that the buyer's list
	// is read by ONE struct across both files. They are absent from the payload
	// entirely while TICKET_ASSIGNMENT_ENABLED is closed, which is what makes
	// their zero values meaningful — see ticket_assignment_test.go, where the
	// flag test asserts their absence over the RAW bytes rather than through
	// this decode.
	AssignmentState   string  `json:"assignment_state"`
	HolderEmail       string  `json:"holder_email"`
	AssignedAt        *string `json:"assigned_at"`
	AcceptedAt        *string `json:"accepted_at"`
	Assignable        bool    `json:"assignable"`
	AssignableRefusal string  `json:"assignable_refusal"`
	Questions         []struct {
		Question ticketQuestion `json:"question"`
		Answer   *struct {
			Text    *string `json:"text"`
			Number  *string `json:"number"`
			Date    *string `json:"date"`
			Checked *bool   `json:"checked"`
		} `json:"answer"`
	} `json:"questions"`
}

func buyerTicketsPath(ticketSaleID string) string {
	return "/api/v1/customer/ticket-sales/" + ticketSaleID + "/tickets"
}

func buyerAnswerPath(ticketSaleID, ticketID, questionID string) string {
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
func listBuyerTickets(t *testing.T, env *testEnv, customerSession, ticketSaleID string) []buyerTicket {
	t.Helper()
	resp, body := env.get(t, buyerTicketsPath(ticketSaleID), authHeader(customerSession))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("buyer tickets status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return decodeBuyerTickets(t, body.Data)
}

// buyerAnswerFor picks one question's Answer out of a buyer row.
func buyerAnswerFor(t *testing.T, ticket buyerTicket, questionID string) *struct {
	Text    *string `json:"text"`
	Number  *string `json:"number"`
	Date    *string `json:"date"`
	Checked *bool   `json:"checked"`
} {
	t.Helper()
	for _, pair := range ticket.Questions {
		if pair.Question.ID == questionID {
			return pair.Answer
		}
	}
	t.Fatalf("question %s is not on the buyer's Ticket payload", questionID)
	return nil
}

// tokenFromAnswerLink pulls the token out of a link the BUYER'S PAGE handed out,
// so a test can go and use it exactly as the person it was forwarded to would.
//
// This is the difference between asserting that the field is non-empty and
// asserting that the feature works: a link that is a well-formed string and
// opens nothing would satisfy the first and fail the second, and the buyer who
// pasted it into a group chat would never find out.
func tokenFromAnswerLink(t *testing.T, link string) string {
	t.Helper()
	_, token, found := strings.Cut(link, "?token=")
	if !found || token == "" {
		t.Fatalf("answer_link %q carries no token", link)
	}
	return token
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

// THE BUYER GETS EVERY TICKET OF THEIR OWN SALE, EACH WITH A WORKING LINK. This
// is the first acceptance criterion of #315 and the reason the feature exists:
// a buyer who bought four tickets knows one t-shirt size and cannot be written
// to about the other three, because the platform holds no address for the people
// holding them and asks for none (ADR 0044). What it can do is give the buyer
// something to forward.
//
// The link is not merely asserted to be non-empty. A well-formed string that
// opens nothing would satisfy that and betray the buyer, who pastes it into a
// group chat and considers the job done — so each link is USED here, through the
// unauthenticated public route a forwarded recipient would hit, and must land on
// exactly the Ticket whose row carried it.
func TestBuyerListsEveryTicketOfTheirOwnSaleWithAWorkingAnswerLink(t *testing.T) {
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
		// The ordinal is the buyer's ONLY handle on which of the identical
		// tickets they are copying a link for: "ticket 2 of 4" is the whole of
		// the page's ability to distinguish them.
		if ticket.Ordinal != i+1 {
			t.Errorf("row %d ordinal = %d, want %d", i, ticket.Ordinal, i+1)
		}
		if ticket.TicketTypeName != "GA" {
			t.Errorf("row %d ticket_type_name = %q, want GA", i, ticket.TicketTypeName)
		}
		if !ticket.Answerable || ticket.AnswerableRefusal != "" {
			t.Errorf("row %d answerable=%v refusal=%q, want an open window before the doors",
				i, ticket.Answerable, ticket.AnswerableRefusal)
		}
		// BOTH questions, the optional one included: the buyer is being asked to
		// distribute the form, not a filtered version of it.
		if len(ticket.Questions) != 2 {
			t.Fatalf("row %d carries %d questions, want the Ticket Type's two", i, len(ticket.Questions))
		}
		if ticket.Questions[0].Question.ID != f.sizeQuestion.ID ||
			ticket.Questions[1].Question.ID != f.extraQuestion.ID {
			t.Errorf("row %d questions are in the wrong order: %+v", i, ticket.Questions)
		}
		if ticket.AnswerLink == "" {
			t.Fatalf("row %d carries no answer_link — the page has nothing to hand out", i)
		}
	}

	// TWO TICKETS, TWO DIFFERENT LINKS. One link for the whole Sale would mean
	// the four people in the group chat all answering for the same ticket.
	if tickets[0].AnswerLink == tickets[1].AnswerLink {
		t.Fatal("both Tickets of the Sale carry the same Answer Link")
	}

	// And now the half that a non-empty string cannot prove: the link under the
	// FIRST row answers the FIRST row's Ticket. Sent through the public,
	// unauthenticated route, with no session anywhere, exactly as the person it
	// was forwarded to would send it.
	resp, body, _ := answerLinkRequest(t, env, http.MethodPut,
		answerLinkQuestionPath+f.sizeQuestion.ID,
		map[string]any{"token": tokenFromAnswerLink(t, tickets[0].AnswerLink), "text": "XL"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("answering through the forwarded link status=%d error=%+v", resp.StatusCode, body.Error)
	}

	reread := listBuyerTickets(t, env, ana, f.anaSaleID)
	first := buyerAnswerFor(t, reread[0], f.sizeQuestion.ID)
	if first == nil || first.Text == nil || *first.Text != "XL" {
		t.Fatalf("first Ticket's answer = %+v, want the XL given through the link on its own row", first)
	}
	if buyerAnswerFor(t, reread[1], f.sizeQuestion.ID) != nil {
		t.Fatal("the link handed out on row 1 answered row 2's Ticket — the rows and the links disagree")
	}
}

// THIS IS THE MOST IMPORTANT TEST IN THE FILE.
//
// One Customer naming another Customer's Ticket Sale id gets an EMPTY LIST. Not
// their tickets, not their questions, and above all not their Answer Links —
// which are unauthenticated credentials that write Answers on somebody else's
// ticket until that Event's doors open. Every other leak on the customer
// namespace discloses a purchase; this one would hand over a durable key.
//
// EMPTY RATHER THAN REFUSED, deliberately, and that is asserted too: "you do not
// own this" and "this does not exist" must be one answer, or the endpoint
// becomes an oracle for which Ticket Sale ids are real — and the thing behind
// the real ones is worth guessing at.
//
// The assertions run over the RAW payload rather than over the decoded struct,
// for the reason answer_link_test.go gives: a decode silently drops a field
// somebody added, and a field somebody added is exactly how a link would escape.
func TestOneCustomerNeverSeesAnotherCustomersTicketsOrTheirAnswerLinks(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)

	// Ana's own read first, so the sentinels below are strings that genuinely
	// exist on this surface for somebody. An isolation test whose forbidden
	// values were never produced by anything would pass on a broken build.
	ana := customerSignIn(t, env, "ana@example.com")
	anaTickets := listBuyerTickets(t, env, ana, f.anaSaleID)
	if len(anaTickets) != 2 || anaTickets[0].AnswerLink == "" {
		t.Fatalf("Ana's own read = %+v, want two Tickets carrying links", anaTickets)
	}

	bruno := customerSignIn(t, env, "bruno@example.com")

	resp, body := env.get(t, buyerTicketsPath(f.anaSaleID), authHeader(bruno))
	// 200 AND EMPTY, never 403 and never 404: the answer for a sale somebody
	// else owns is the answer for a sale that does not exist.
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
	forbidden := map[string]string{
		"Ana's first Ticket id":  f.anaTicketIDs[0],
		"Ana's second Ticket id": f.anaTicketIDs[1],
		"Ana's Ticket Sale id":   f.anaSaleID,
		"Ana's confirmation ref": f.anaRef,
		// The two that would be catastrophic rather than merely wrong.
		"Ana's first Answer Link":  anaTickets[0].AnswerLink,
		"Ana's second Answer Link": anaTickets[1].AnswerLink,
	}
	for what, secret := range forbidden {
		if strings.Contains(raw, secret) {
			t.Errorf("another Customer's read discloses %s (%q).\n"+
				"An Answer Link is an unauthenticated write credential over that Ticket.\n"+
				"See repository.ListAnswerableTicketsForBuyer and its customer_id clause.\nbody: %s",
				what, secret, raw)
		}
	}

	// The empty list is not the route being broken for everyone: Bruno's OWN
	// sale reads perfectly through the same session and the same call.
	own := listBuyerTickets(t, env, bruno, f.brunoSaleID)
	if len(own) != 1 || own[0].TicketID != f.brunoTicketIDs[0] || own[0].AnswerLink == "" {
		t.Fatalf("Bruno's own read = %+v, want his one Ticket with its link", own)
	}
	// And Ana is untouched by any of it.
	if len(listBuyerTickets(t, env, ana, f.anaSaleID)) != 2 {
		t.Fatal("Ana's own Tickets went missing while Bruno was probing")
	}
}

// A CONFIRMATION LINK SESSION SEES THE ONE SALE IT NAMES AND NOTHING ELSE —
// including another sale belonging to the SAME Customer, which is the case that
// makes this a separate rule rather than a restatement of the one above.
//
// Sale Confirmations get forwarded: to the friend coming along, to whoever is
// paying. The bargain in ADR 0010 is that possession of a forwarded receipt
// opens exactly the purchase it was a receipt for, and here that bargain is
// carrying Answer Links — so a widened link session would hand the person who
// was forwarded one receipt the write credentials for a different purchase
// entirely.
//
// The full sign-in at the end is what makes the empty list above mean
// "narrowed" rather than "broken".
func TestAConfirmationLinkSessionReachesOnlyTheSaleItNames(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)

	// A SECOND purchase by the same person, on the same Event and the same
	// Ticket Type. Belonging to Ana is exactly what must not be enough.
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

	// The sale the receipt was for: everything, links included. A link session
	// is not a second-class reader — forwarding the receipt to the person who
	// will use the ticket is the journey this feature is FOR.
	linked := listBuyerTickets(t, env, linkSession, f.anaSaleID)
	if len(linked) != 2 {
		t.Fatalf("link session sees %d Tickets of the sale it names, want 2", len(linked))
	}
	if linked[0].AnswerLink == "" || linked[1].AnswerLink == "" {
		t.Fatalf("link session got rows with no answer_link: %+v", linked)
	}

	// Ana's OTHER sale: empty, on the same terms a stranger's is. The session's
	// scope may only ever narrow, never widen back to the Customer behind it.
	if others := listBuyerTickets(t, env, linkSession, secondSaleID); len(others) != 0 {
		t.Fatalf("a Confirmation Link session reached %d Tickets of the Customer's OTHER sale: %+v",
			len(others), others)
	}
	// And a stranger's, for completeness: the narrowing is checked before the
	// customer_id clause, and both must hold.
	if others := listBuyerTickets(t, env, linkSession, f.brunoSaleID); len(others) != 0 {
		t.Fatalf("a Confirmation Link session reached %d Tickets of another Customer's sale", len(others))
	}

	// The write is narrowed identically, and refuses as NOT FOUND rather than as
	// a scope error — the same rule as the read: naming a sale this session may
	// not see must teach the caller nothing about whether it exists.
	secondTicketIDs := ticketIDsOfSale(t, env, secondSaleID)
	resp, body := env.put(t, buyerAnswerPath(secondSaleID, secondTicketIDs[0], f.sizeQuestion.ID),
		map[string]any{"text": "M"}, authHeader(linkSession))
	assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_NOT_FOUND")

	// A full sign-in by the same person reaches both sales, which is what makes
	// the empty lists above the LINK's narrowing rather than the second sale
	// being invisible generally.
	full := customerSignIn(t, env, "ana@example.com")
	if tickets := listBuyerTickets(t, env, full, secondSaleID); len(tickets) != 1 {
		t.Fatalf("a full session sees %d Tickets of Ana's second sale, want 1", len(tickets))
	}
	if tickets := listBuyerTickets(t, env, full, f.anaSaleID); len(tickets) != 2 {
		t.Fatalf("a full session sees %d Tickets of Ana's first sale, want 2", len(tickets))
	}
}

// THE BUYER MAY ANSWER ANY TICKET OF THEIR SALE, and not merely some notional
// "own" one. An Answer belongs to the TICKET and three parties may supply it
// (ADR 0044): the holder through an Answer Link, the buyer, and Event Staff. A
// mother buying for her three children answers all four herself, and must not
// have to mail herself four links to do it.
//
// THE WHOLE SALE COMES BACK FROM THE WRITE, which is the other half of this
// test. The page is a list of Tickets whose outstanding counts move together, so
// answering one and being handed back only that row would leave the surface
// showing a fresh row beside a stale total — the kind of bug nobody notices
// until an Organization asks why a buyer insists they answered.
func TestTheBuyerMayAnswerAnyTicketOfTheirSaleAndGetsTheWholeSaleBack(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	ana := customerSignIn(t, env, "ana@example.com")

	// The SECOND Ticket, deliberately: the one whose link the buyer would
	// otherwise have had to forward to themselves.
	resp, body := env.put(t, buyerAnswerPath(f.anaSaleID, f.anaTicketIDs[1], f.sizeQuestion.ID),
		map[string]any{"text": "S"}, authHeader(ana))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("buyer's answer status=%d error=%+v", resp.StatusCode, body.Error)
	}

	returned := decodeBuyerTickets(t, body.Data)
	if len(returned) != 2 {
		t.Fatalf("the write returned %d Tickets, want the whole Sale's 2", len(returned))
	}
	if answer := buyerAnswerFor(t, returned[1], f.sizeQuestion.ID); answer == nil || *answer.Text != "S" {
		t.Fatalf("the answered row came back as %+v, want S", answer)
	}
	// The counts in the same payload have already moved, which is the point of
	// returning all of it.
	if returned[1].OutstandingCount != 0 || returned[0].OutstandingCount != 1 {
		t.Fatalf("outstanding counts = %d and %d, want the answered Ticket at 0 and the other still at 1",
			returned[0].OutstandingCount, returned[1].OutstandingCount)
	}

	// Read back on a fresh request, because a write that answers correctly and
	// stores nothing is a whole class of bug.
	reread := listBuyerTickets(t, env, ana, f.anaSaleID)
	if answer := buyerAnswerFor(t, reread[1], f.sizeQuestion.ID); answer == nil || *answer.Text != "S" {
		t.Fatalf("re-read answer = %+v, want the S the buyer gave", answer)
	}
	if buyerAnswerFor(t, reread[0], f.sizeQuestion.ID) != nil {
		t.Fatal("answering one Ticket wrote an Answer on its sibling too")
	}

	// ONE ANSWER, ONE ROW, SHARED WITH EVERY OTHER SURFACE. What the buyer wrote
	// is what Event Staff read — there is no second copy keyed on who wrote it,
	// which is what "an Answer belongs to the Ticket" means.
	staffView := decodeTicketAnswers(t, mustGetTicket(t, env, f.staffSession, f.eventID, f.anaTicketIDs[1]))
	if answer := answerFor(t, staffView, f.sizeQuestion.ID); answer == nil || *answer.Text != "S" {
		t.Fatalf("staff read the buyer's Answer as %+v, want S", answer)
	}

	// A correction is the same request with a different body, and it replaces
	// rather than accumulates.
	resp, body = env.put(t, buyerAnswerPath(f.anaSaleID, f.anaTicketIDs[1], f.sizeQuestion.ID),
		map[string]any{"text": "M"}, authHeader(ana))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("buyer's correction status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var rowCount int
	if err := env.db.QueryRow(`
		SELECT COUNT(*) FROM ticket_answers WHERE ticket_id = $1 AND ticket_question_id = $2
	`, f.anaTicketIDs[1], f.sizeQuestion.ID).Scan(&rowCount); err != nil {
		t.Fatalf("count Answers: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("ticket_answers rows = %d, want exactly 1 — an Answer replaces, it does not accumulate", rowCount)
	}
}

// A TICKET ON SOMEBODY ELSE'S SALE IS NOT FOUND, however it is named.
//
// The write resolves its Ticket through the same customer-scoped read the
// listing uses, so this is the isolation property restated on the path that
// MUTATES — and a caller who has somehow learned a Ticket id must not be able to
// use it to confirm the id is real, let alone to write on it. 404 and not 403,
// for the same reason the read returns an empty list rather than a refusal.
//
// Every shape of the mistake is driven, because they take different routes
// through the handler and the service: another Customer's Ticket named under
// their own sale (the sale is not the caller's), named under the caller's own
// sale (the sale is the caller's, the Ticket is not), and a Ticket id that
// belongs to nobody at all.
func TestATicketOnSomebodyElsesSaleIsNotFoundFromTheBuyersWrite(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	bruno := customerSignIn(t, env, "bruno@example.com")

	for name, path := range map[string]string{
		"another Customer's Ticket, under their own sale": buyerAnswerPath(
			f.anaSaleID, f.anaTicketIDs[0], f.sizeQuestion.ID),
		"another Customer's Ticket, smuggled under the caller's own sale": buyerAnswerPath(
			f.brunoSaleID, f.anaTicketIDs[0], f.sizeQuestion.ID),
		"a Ticket id belonging to nobody": buyerAnswerPath(
			f.brunoSaleID, "22222222-2222-4222-8222-222222222222", f.sizeQuestion.ID),
	} {
		t.Run(name, func(t *testing.T) {
			resp, body := env.put(t, path, map[string]any{"text": "XL"}, authHeader(bruno))
			assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_NOT_FOUND")
		})
	}

	// Nothing was written on the way past. The refusal above being correct in
	// its status code would be worth little if the row had already been upserted.
	var written int
	if err := env.db.QueryRow(`
		SELECT COUNT(*) FROM ticket_answers WHERE ticket_id = ANY($1)
	`, f.anaTicketIDs).Scan(&written); err != nil {
		t.Fatalf("count Answers on Ana's Tickets: %v", err)
	}
	if written != 0 {
		t.Fatalf("%d Answers were written on another Customer's Tickets by refused requests", written)
	}

	// And Bruno's own Ticket takes the same write, so the loop above was not
	// passing against a route that refuses everybody.
	resp, body := env.put(t, buyerAnswerPath(f.brunoSaleID, f.brunoTicketIDs[0], f.sizeQuestion.ID),
		map[string]any{"text": "XL"}, authHeader(bruno))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Bruno's own Ticket status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// A REVERSED TICKET SALE HANDS OUT NO LINKS AND STILL SHOWS EVERYTHING.
//
// The two halves are a single decision. `answer_link` goes EMPTY rather than
// present-but-dead, because the copy button is a promise: a buyer who forwards a
// link has finished the task as far as they know and will not discover for weeks
// that what they sent opened nothing. Better no button than a button that
// forwards a dead end.
//
// And the sale keeps its place in the Customer Area with everything it said
// intact. A Sale Reversal voids a purchase; it does not unmint Tickets and it
// does not erase Answers. Tickets that vanished from the list at that moment
// would read to the buyer as data destroyed — and this is the moment somebody is
// most likely to be looking, because their money has just gone back.
func TestAReversedSaleKeepsItsTicketsAndAnswersButHandsOutNoLinks(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	ana := customerSignIn(t, env, "ana@example.com")

	// Something said BEFORE the reversal, so "still readable" is a claim about
	// real content rather than about empty rows.
	putAnswer(t, env, f.staffSession, f.eventID, f.anaTicketIDs[0], f.sizeQuestion.ID,
		map[string]any{"text": "XL"})

	undoBatch(t, env, f.staffSession, f.eventID, f.anaBatchID)

	tickets := listBuyerTickets(t, env, ana, f.anaSaleID)
	if len(tickets) != 2 {
		t.Fatalf("a reversed Sale lists %d Tickets, want both still there", len(tickets))
	}
	for i, ticket := range tickets {
		if ticket.AnswerLink != "" {
			t.Errorf("row %d of a reversed Sale still hands out an Answer Link (%q) — "+
				"a copy button that forwards a dead link is worse than no button",
				i, ticket.AnswerLink)
		}
		if ticket.Answerable {
			t.Errorf("row %d of a reversed Sale reports itself answerable", i)
		}
		// The page must be able to SAY what happened rather than leave somebody
		// pressing a form that will not take.
		if ticket.AnswerableRefusal != "sale_reversed" {
			t.Errorf("row %d refusal = %q, want sale_reversed", i, ticket.AnswerableRefusal)
		}
		// Nothing below the link is hidden: the questions are all still listed.
		if len(ticket.Questions) != 2 {
			t.Errorf("row %d carries %d questions after the Reversal, want 2", i, len(ticket.Questions))
		}
		// There is nobody left to chase, so the debt is nil (#313).
		if ticket.OutstandingCount != 0 {
			t.Errorf("row %d outstanding_count = %d on a reversed Sale, want 0", i, ticket.OutstandingCount)
		}
	}

	if answer := buyerAnswerFor(t, tickets[0], f.sizeQuestion.ID); answer == nil || *answer.Text != "XL" {
		t.Fatalf("the Answer given before the Reversal reads as %+v — reversing a sale erased what a Ticket said", answer)
	}

	// The WRITE is what a reversed Sale refuses, and it says which of the two
	// closed windows this is. The buyer is entitled to know: it is their own
	// purchase and their own money that went back.
	resp, body := env.put(t, buyerAnswerPath(f.anaSaleID, f.anaTicketIDs[0], f.sizeQuestion.ID),
		map[string]any{"text": "M"}, authHeader(ana))
	assertAPIError(t, resp, body, http.StatusConflict, "TICKET_SALE_REVERSED")
}

// THE BUYER'S OUTSTANDING COUNT IS THE ORGANIZATION'S DEFINITION AND NOT A
// SECOND ONE.
//
// catalog.IsOutstandingAnswer is the single rule (#313), already stated twice on
// purpose — once in Go and once in the SQL behind the Organization's chase list —
// and that duplication is affordable only because it is exactly two. A third
// statement of "required, not retired, active Sale, nothing said" on the buyer's
// page would be the one that quietly disagreed, and it would disagree in the
// worst possible place: the buyer told they owe nothing while the Organization's
// list still names them and an Answer Reminder is still chasing them.
//
// So every clause is walked on the buyer's page AND cross-checked against the
// staff chase list in the same breath. The two numbers must move together.
func TestBuyerOutstandingCountsAgreeWithTheOrganizationsChaseList(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	ana := customerSignIn(t, env, "ana@example.com")

	// A second REQUIRED question, so the count has somewhere to move to besides
	// zero, and the retirement below leaves something behind.
	dinner := createTicketQuestion(t, env, f.staffSession, f.eventID, f.ticketTypeID, map[string]any{
		"label": "Coming to the dinner?", "kind": "checkbox", "required": true,
	})

	// REQUIRED COUNTS, OPTIONAL DOES NOT. Three questions on the Ticket Type, two
	// of them required, nothing answered: the debt is two.
	tickets := listBuyerTickets(t, env, ana, f.anaSaleID)
	for i, ticket := range tickets {
		if len(ticket.Questions) != 3 {
			t.Fatalf("row %d carries %d questions, want 3", i, len(ticket.Questions))
		}
		if ticket.OutstandingCount != 2 {
			t.Fatalf("row %d outstanding_count = %d, want 2 — the optional question is nobody's debt",
				i, ticket.OutstandingCount)
		}
	}
	// The Organization's list is EVENT-WIDE, so it also holds Bruno's Ticket and
	// its two debts: six across three Tickets. The comparison that matters is
	// per-Ticket — the same two questions named on Ana's rows as counted on her
	// page — because the buyer is told a number and the Organization is told a
	// list of labels, and it is those two that must never disagree.
	page := listOutstanding(t, env, f.staffSession, f.eventID)
	if page.OutstandingCount != 6 {
		t.Fatalf("the Organization's list says %d outstanding, want 6 — two questions on each of the Event's three Tickets",
			page.OutstandingCount)
	}
	for _, ticketID := range f.anaTicketIDs {
		if labels := labelsOwedBy(page, ticketID); len(labels) != 2 {
			t.Fatalf("the Organization's list says Ticket %s owes %v while the buyer's page counts 2",
				ticketID, labels)
		}
	}

	// FALSE IS AN ANSWER. Somebody who read "Coming to the dinner?" and left it
	// unticked has said no, which is a different fact from never having been
	// asked, and it discharges the debt exactly as any other reply does. The rule
	// reads the Answer's EXISTENCE and never its content — which is the clause a
	// re-implementation on this page would be most likely to get wrong.
	resp, body := env.put(t, buyerAnswerPath(f.anaSaleID, f.anaTicketIDs[0], dinner.ID),
		map[string]any{"checked": false}, authHeader(ana))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("answering false status=%d error=%+v", resp.StatusCode, body.Error)
	}
	after := decodeBuyerTickets(t, body.Data)
	if after[0].OutstandingCount != 1 {
		t.Fatalf("outstanding_count = %d after a `false` Answer, want 1 — false is an Answer",
			after[0].OutstandingCount)
	}
	if after[1].OutstandingCount != 2 {
		t.Fatalf("the sibling's outstanding_count = %d, want 2 — one Ticket's Answer is not another's",
			after[1].OutstandingCount)
	}
	if labels := labelsOwedBy(listOutstanding(t, env, f.staffSession, f.eventID), f.anaTicketIDs[0]); len(labels) != 1 ||
		labels[0] != "T-shirt size" {
		t.Fatalf("the Organization's list says the Ticket owes %v while the buyer's page says one question", labels)
	}

	// A RETIRED QUESTION OWES NOTHING, because no route into an Answer will take
	// one — so a debt against it would be a row on the buyer's page with no
	// working form behind it.
	resp, body = env.deleteJSON(t, questionPath(f.eventID, f.ticketTypeID, f.sizeQuestion.ID), nil,
		authHeader(f.staffSession))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retire question status=%d error=%+v", resp.StatusCode, body.Error)
	}

	retired := listBuyerTickets(t, env, ana, f.anaSaleID)
	if retired[0].OutstandingCount != 0 {
		t.Fatalf("outstanding_count = %d after the last live question was retired, want 0",
			retired[0].OutstandingCount)
	}
	if retired[1].OutstandingCount != 1 {
		t.Fatalf("the sibling owes %d after the retirement, want only the dinner question",
			retired[1].OutstandingCount)
	}
	if !owesNothing(listOutstanding(t, env, f.staffSession, f.eventID), f.anaTicketIDs[0]) {
		t.Fatal("the Organization's chase list still names a Ticket the buyer's page says owes nothing")
	}
	// The retired question is still LISTED and still readable — retiring takes
	// the debt, never the record.
	if len(retired[0].Questions) != 3 {
		t.Fatalf("row carries %d questions after a retirement, want the retired one kept: %+v",
			len(retired[0].Questions), retired[0].Questions)
	}
}

// WHILE THE FLAG IS OFF, NEITHER ROUTE IS THERE. The same 404 and the same code
// every other Answer surface gives, so a dark build is indistinguishable from a
// build that never had the feature (ADR 0045) — and no Answer can come into
// existence before the Privacy Policy describes the collection.
//
// The fixture runs with the flag ON so that the questions, the Tickets and the
// Sale all genuinely exist, and is then closed again: this is a deployment
// turning the flag back off, which is the state in which a route that leaked its
// existence would do so most embarrassingly.
func TestBuyerTicketRoutesAreInvisibleWhileTheFlagIsOff(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)
	ana := customerSignIn(t, env, "ana@example.com")

	sharedApp.CatalogService.WithTicketQuestions(false)
	sharedApp.SalesService.WithTicketQuestions(false)

	for name, call := range map[string]func() (*http.Response, envelope){
		"listing the buyer's own Tickets": func() (*http.Response, envelope) {
			return env.get(t, buyerTicketsPath(f.anaSaleID), authHeader(ana))
		},
		"answering one of them": func() (*http.Response, envelope) {
			return env.put(t, buyerAnswerPath(f.anaSaleID, f.anaTicketIDs[0], f.sizeQuestion.ID),
				map[string]any{"text": "L"}, authHeader(ana))
		},
	} {
		t.Run(name, func(t *testing.T) {
			resp, body := call()
			assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_QUESTIONS_UNAVAILABLE")
		})
	}
}

// NO SESSION, NO ANSWER — on a payload made of Answer Links, which is why this
// is asserted rather than assumed. The routes sit behind RequireCustomerSession
// and a wiring change that dropped the middleware would leave a public endpoint
// handing out write credentials to anyone who could guess a Ticket Sale id.
//
// The handler ALSO fails closed on its own, independently of the middleware
// (buyerSaleRoute refuses when no session is on the context), and that belt is
// what this test would still be standing on if the braces went.
func TestBuyerTicketRoutesRefuseACallerWithNoCustomerSession(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env)

	for name, tc := range map[string]struct {
		headers map[string]string
		code    string
	}{
		// No credential at all: the request never names anybody.
		"no Authorization header": {nil, "UNAUTHORIZED"},
		// A credential that is not a Customer Session. Reported as the session
		// not existing rather than as a malformed anything, because a token
		// nobody minted and a token that has been signed out are the same fact.
		"an invented bearer token": {authHeader("not-a-session"), "CUSTOMER_SESSION_NOT_FOUND"},
	} {
		t.Run(name, func(t *testing.T) {
			resp, body := env.get(t, buyerTicketsPath(f.anaSaleID), tc.headers)
			assertAPIError(t, resp, body, http.StatusUnauthorized, tc.code)

			resp, body = env.put(t, buyerAnswerPath(f.anaSaleID, f.anaTicketIDs[0], f.sizeQuestion.ID),
				map[string]any{"text": "L"}, tc.headers)
			assertAPIError(t, resp, body, http.StatusUnauthorized, tc.code)
		})
	}

	// A STAFF SESSION IS NOT A CUSTOMER SESSION. The Org Admin who can read
	// every one of these Tickets through their own route is nobody at all here,
	// because the two are unrelated records on unrelated surfaces.
	resp, body := env.get(t, buyerTicketsPath(f.anaSaleID), authHeader(f.staffSession))
	assertAPIError(t, resp, body, http.StatusUnauthorized, "CUSTOMER_SESSION_NOT_FOUND")
}

// A MALFORMED TICKET SALE ID IS ANSWERED EXACTLY AS ONE NOBODY OWNS.
//
// Never a 400. A validation error would tell somebody probing this route which
// of their guesses were at least the right SHAPE, which narrows the search for a
// real id — and what sits behind a real id here is a set of Answer Links. So
// "that is not a uuid", "nobody owns that", and "that does not exist" are one
// answer: an empty list on the read, TICKET_NOT_FOUND on the write.
//
// The read's two responses are compared BYTE FOR BYTE, because "both were empty"
// is not the property; the property is that the caller cannot tell them apart.
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
		// A confirmation reference: the one identifier of a sale a buyer
		// genuinely holds, and therefore the likeliest thing to be sent here by
		// mistake or by hand.
		f.anaRef,
		// A uuid with a character too many, which is the shape a truncated or
		// re-typed id takes.
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

			resp, body = env.put(t,
				buyerAnswerPath(malformed, f.anaTicketIDs[0], f.sizeQuestion.ID),
				map[string]any{"text": "L"}, authHeader(ana))
			assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_NOT_FOUND")
		})
	}

	// The write refuses a well-formed unowned sale id with the very same code,
	// which is the other end of the comparison above.
	resp, body := env.put(t, buyerAnswerPath(unownedSaleID, f.anaTicketIDs[0], f.sizeQuestion.ID),
		map[string]any{"text": "L"}, authHeader(ana))
	assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_NOT_FOUND")

	// A MALFORMED TICKET id is the same answer again, on the other path segment.
	resp, body = env.put(t, buyerAnswerPath(f.anaSaleID, "not-a-uuid", f.sizeQuestion.ID),
		map[string]any{"text": "L"}, authHeader(ana))
	assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_NOT_FOUND")

	// And the real ids still work, so none of the above is passing against a
	// route that refuses everything.
	if tickets := listBuyerTickets(t, env, ana, f.anaSaleID); len(tickets) != 2 {
		t.Fatalf("the real sale id returned %d Tickets, want 2", len(tickets))
	}
}
