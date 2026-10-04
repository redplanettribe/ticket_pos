package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// THE BUYER'S PROVISIONAL ANSWERS (#672, spec #665, ADR 0076).
//
// On an Event that requires Named Tickets the buyer answered every Ticket at
// checkout, somebody else's included. Those Answers are the buyer's to read and
// correct while the Ticket is `assigned`, from the same sale page they assign
// from, and become the Holder's the moment the Holder accepts: from then the
// buyer sees the assignment state alone and their writes are refused, exactly
// as ADR 0049 has it. On an Event without Named Tickets nothing changes.
//
// The seam is the buyer's sale page (a Customer Session and a Confirmation Link
// session), the Holder's accept page, the staff Answers dialog and the Holder
// List, as the people who read them see them.

// provisionalAnswers decodes the `provisional_answers` block of a buyer's row.
type provisionalAnswers struct {
	Answerable        bool   `json:"answerable"`
	AnswerableRefusal string `json:"answerable_refusal"`
	OutstandingCount  int    `json:"outstanding_count"`
	Questions         []struct {
		Question ticketQuestion `json:"question"`
		Answer   *holderAnswer  `json:"answer"`
	} `json:"questions"`
}

// provisionalAnswerPath is the buyer's write on one question of one Ticket of
// their own Sale. NOT the sale-scoped answer write ADR 0049 retired
// (retiredBuyerAnswerPath), which stays gone: this one is named for the only
// Answers it reaches.
func provisionalAnswerPath(ticketSaleID, ticketID, questionID string) string {
	return buyerTicketsPath(ticketSaleID) + "/" + ticketID + "/provisional-answers/" + questionID
}

func answerProvisionally(
	t *testing.T, env *testEnv, session, ticketSaleID, ticketID, questionID string, reply map[string]any,
) (*http.Response, envelope) {
	t.Helper()
	return env.put(t, provisionalAnswerPath(ticketSaleID, ticketID, questionID), reply, authHeader(session))
}

// answerProvisionallyOK writes and returns the whole Sale's rows as the write
// handed them back, checking their bytes as every buyer read is checked.
func answerProvisionallyOK(
	t *testing.T, env *testEnv, session, ticketSaleID, ticketID, questionID string, reply map[string]any,
) []buyerTicket {
	t.Helper()
	resp, body := answerProvisionally(t, env, session, ticketSaleID, ticketID, questionID, reply)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("provisional answer status=%d error=%+v", resp.StatusCode, body.Error)
	}
	assertNoAnswerOnTheBuyersRows(t, body.Data)
	return decodeBuyerTickets(t, body.Data)
}

// provisionalText is the text the buyer's row shows for one question, failing
// when the row carries no provisional Answers at all.
func provisionalText(t *testing.T, row buyerTicket, questionID string) *string {
	t.Helper()
	if row.ProvisionalAnswers == nil {
		t.Fatalf("Ticket %d (%s) carries no provisional Answers", row.Ordinal, row.AssignmentState)
	}
	for _, pair := range row.ProvisionalAnswers.Questions {
		if pair.Question.ID == questionID {
			if pair.Answer == nil {
				return nil
			}
			return pair.Answer.Text
		}
	}
	t.Fatalf("question %s is not on Ticket %d's provisional Answers", questionID, row.Ordinal)
	return nil
}

// rawBuyerRow is one row of the buyer's list as bytes, for the assertions about
// what is ABSENT, which a decoded struct cannot make.
func rawBuyerRow(t *testing.T, env *testEnv, session, ticketSaleID, ticketID string) string {
	t.Helper()
	resp, body := env.get(t, buyerTicketsPath(ticketSaleID), authHeader(session))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("buyer tickets status=%d error=%+v", resp.StatusCode, body.Error)
	}
	assertNoAnswerOnTheBuyersRows(t, body.Data)
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(body.Data, &rows); err != nil {
		t.Fatalf("decode buyer rows: %v", err)
	}
	for _, row := range rows {
		if string(row["ticket_id"]) == `"`+ticketID+`"` {
			raw, _ := json.Marshal(row)
			return string(raw)
		}
	}
	t.Fatalf("Ticket %s is not on the buyer's list", ticketID)
	return ""
}

// namedAssignmentFixture is newAssignmentFixture on an Event requiring Named
// Tickets, with Ana's first Ticket assigned to Carla and sized as the buyer
// sizes it, with the address (#673): an `assigned`, unaccepted Ticket carrying
// an Answer, and Carla's Assignment Link in the captured inbox.
type namedAssignmentFixture struct {
	assignmentFixture
	carlaTicketID string
}

func newNamedAssignmentFixture(t *testing.T, env *testEnv) namedAssignmentFixture {
	t.Helper()
	f := namedAssignmentFixture{assignmentFixture: newAssignmentFixture(t, env)}
	setBuyerFestNamedTickets(t, env, f.assignmentFixture, true)
	f.carlaTicketID = f.anaTicketIDs[0]
	assignWithAnswersOK(t, env, f.ana, f.anaSaleID, f.carlaTicketID, "carla@example.com",
		givenAnswer(f.sizeQuestion.ID, map[string]any{"text": "M"}))
	return f
}

// THE CENTRE OF THE TICKET, through a real Named Tickets checkout: Ana reads
// what she answered for Ben and corrects it, the Organization sees the
// correction, and Ben's owed Assignment mail is still owed for the assignment
// his Ticket carries - a correction is not a reassignment.
func TestTheBuyerReadsAndCorrectsTheAnswersOnATicketNamedForSomebodyElse(t *testing.T) {
	env := setupTest(t)
	f := newNamedCommitFixture(t, env, 2000)
	begun := beginCheckoutOK(t, env, testOrgSlug, "named-fest", f.familyBasket())
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	saleID := saleIDOfPayment(t, env, begun.ClientTransactionID)
	ana := customerSignIn(t, env, "ana@example.com")

	tickets := listBuyerTickets(t, env, ana, saleID)
	for _, tc := range []struct {
		ordinal int
		want    string
	}{{2, "M"}, {4, "XL"}} {
		row := buyerTicketAt(t, tickets, tc.ordinal)
		if got := provisionalText(t, row, f.size.ID); got == nil || *got != tc.want {
			t.Errorf("Ben's Ticket %d shows the buyer %v, want %q", tc.ordinal, got, tc.want)
		}
		if !row.ProvisionalAnswers.Answerable || row.ProvisionalAnswers.OutstandingCount != 0 {
			t.Errorf("Ben's Ticket %d provisional Answers = %+v, want answerable and owing nothing",
				tc.ordinal, row.ProvisionalAnswers)
		}
	}
	// The buyer's own Tickets are answered where every Holder answers: the
	// held-ticket routes. Their sale-page rows carry no provisional block.
	for _, ordinal := range []int{1, 3} {
		if row := buyerTicketAt(t, tickets, ordinal); row.ProvisionalAnswers != nil {
			t.Errorf("Ana's own Ticket %d carries provisional Answers on the sale page", ordinal)
		}
	}

	ben := buyerTicketAt(t, tickets, 2)
	before := readTicketAssignment(t, env, ben.TicketID)
	mailsBefore := assignmentMailCount(env)
	holdClocksAt(fixedClock.Add(time.Hour))

	returned := answerProvisionallyOK(t, env, ana, saleID, ben.TicketID, f.size.ID, map[string]any{"text": "L"})
	if got := provisionalText(t, buyerTicketAt(t, returned, 2), f.size.ID); got == nil || *got != "L" {
		t.Fatalf("the write handed back %v, want the corrected L", got)
	}
	if got := provisionalText(t, buyerTicketAt(t, listBuyerTickets(t, env, ana, saleID), 2), f.size.ID); got == nil || *got != "L" {
		t.Fatalf("the buyer's re-read shows %v, want the corrected L", got)
	}

	// THE ORGANIZATION READS THE CORRECTION, on the staff dialog that shows
	// every Answer, and the Holder List still owes nothing.
	for _, ticket := range saleTickets(t, env, f.sessionID, f.eventID, saleID) {
		if ticket.TicketID != ben.TicketID {
			continue
		}
		if got := answerTo(t, ticket, f.size.ID); got == nil || *got != "L" {
			t.Errorf("Event Staff read %v on Ben's Ticket, want the buyer's correction L", got)
		}
	}
	if list := listOutstanding(t, env, f.sessionID, f.eventID); list.OutstandingCount != 0 {
		t.Errorf("Outstanding Answers = %d after a correction, want 0", list.OutstandingCount)
	}

	// A CORRECTION IS NOT A REASSIGNMENT. assigned_at is what the owed mail and
	// every Assignment Link are signed over, so it must not move.
	after := readTicketAssignment(t, env, ben.TicketID)
	if !after.assignedAt.Time.Equal(before.assignedAt.Time) || after.holderEmail != before.holderEmail {
		t.Errorf("correcting an Answer moved the assignment: %+v then %+v", before, after)
	}
	if mail, ok := owedMails(t, env, saleID)[ben.TicketID]; !ok || !mail.current {
		t.Errorf("Ben's owed Assignment mail is gone or stale after a correction (owed=%v current=%v)", ok, mail.current)
	}
	if got := assignmentMailCount(env); got != mailsBefore {
		t.Errorf("correcting an Answer sent %d Assignment mail(s), want none", got-mailsBefore)
	}
}

// THE HOLDER LIST REFLECTS A PROVISIONAL ANSWER: a required question approved
// after the sale is owed on the assigned Ticket until the buyer answers it,
// and from a Confirmation Link session as much as from a Customer Session.
func TestAConfirmationLinkSessionAnswersAnAssignedTicketAndTheHolderListPaysTheDebt(t *testing.T) {
	env := setupTest(t)
	f := newNamedAssignmentFixture(t, env)
	meal := createTicketQuestion(t, env, f.staffSession, f.eventID, f.ticketTypeID, map[string]any{
		"label": "Meal", "kind": "short_text", "required": true,
	})
	if owed := labelsOwedBy(listOutstanding(t, env, f.staffSession, f.eventID), f.carlaTicketID); len(owed) != 1 || owed[0] != "Meal" {
		t.Fatalf("Carla's Ticket owes %v, want the late Meal question alone", owed)
	}

	_, linkSession := redeemConfirmationLinkOK(t, env, confirmationLinkTokenForRef(t, env, f.anaRef), "")
	row := findBuyerRow(t, listBuyerTickets(t, env, linkSession, f.anaSaleID), f.carlaTicketID)
	if got := provisionalText(t, row, f.sizeQuestion.ID); got == nil || *got != "M" {
		t.Fatalf("the link session reads %v on Carla's Ticket, want M", got)
	}
	if row.ProvisionalAnswers.OutstandingCount != 1 {
		t.Fatalf("outstanding_count = %d, want the one late Meal question", row.ProvisionalAnswers.OutstandingCount)
	}

	returned := answerProvisionallyOK(t, env, linkSession, f.anaSaleID, f.carlaTicketID, meal.ID,
		map[string]any{"text": "Vegetarian"})
	if got := findBuyerRow(t, returned, f.carlaTicketID).ProvisionalAnswers.OutstandingCount; got != 0 {
		t.Errorf("outstanding_count = %d after answering, want 0", got)
	}
	if !owesNothing(t, listOutstanding(t, env, f.staffSession, f.eventID), f.carlaTicketID) {
		t.Errorf("the Holder List still owes on Carla's Ticket after the buyer answered")
	}
	if got := staffAnswerText(t, env, f.staffSession, f.eventID, f.carlaTicketID, meal.ID); got == nil || *got != "Vegetarian" {
		t.Errorf("Event Staff read %v, want Vegetarian", got)
	}
}

// ONCE THE HOLDER ACCEPTS THE ANSWERS ARE THEIRS: the accept page shows what
// the buyer gave, already filled in, and the Holder may change it; the buyer's
// row goes back to the assignment state alone and the buyer's write is refused
// as a Ticket they cannot reach.
func TestOnceTheHolderAcceptsTheAnswersAreTheirsAndTheBuyerLosesThem(t *testing.T) {
	env := setupTest(t)
	f := newNamedAssignmentFixture(t, env)
	answerProvisionallyOK(t, env, f.ana, f.anaSaleID, f.carlaTicketID, f.extraQuestion.ID,
		map[string]any{"text": "Arriving late"})

	token := assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com"))
	accepted := acceptAssignmentOK(t, env, token)
	if got := holderAnswerFor(t, accepted, f.sizeQuestion.ID); got == nil || got.Text == nil || *got.Text != "M" {
		t.Errorf("the accept page shows %+v for the size, want the buyer's M pre-filled", got)
	}
	if got := holderAnswerFor(t, accepted, f.extraQuestion.ID); got == nil || got.Text == nil || *got.Text != "Arriving late" {
		t.Errorf("the accept page shows %+v for the note, want the buyer's provisional Answer", got)
	}
	changed := answerByAssignmentLinkOK(t, env, token, f.sizeQuestion.ID, map[string]any{"text": "S"})
	if got := holderAnswerFor(t, changed, f.sizeQuestion.ID); got == nil || got.Text == nil || *got.Text != "S" {
		t.Fatalf("the Holder's change reads %+v, want S", got)
	}

	raw := rawBuyerRow(t, env, f.ana, f.anaSaleID, f.carlaTicketID)
	if !strings.Contains(raw, `"assignment_state":"accepted"`) {
		t.Fatalf("Carla's Ticket reads %s, want accepted", raw)
	}
	// `"questions"` as a key: the row still lists reassignment_questions, the
	// questions a reassignment would have to answer (#673), and no Answer.
	for _, leaked := range []string{"provisional_answers", `"questions"`, `"S"`, "Arriving late"} {
		if strings.Contains(raw, leaked) {
			t.Errorf("the buyer's row for an accepted Ticket carries %s: %s", leaked, raw)
		}
	}

	resp, body := answerProvisionally(t, env, f.ana, f.anaSaleID, f.carlaTicketID, f.sizeQuestion.ID,
		map[string]any{"text": "XXL"})
	assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_NOT_FOUND")
	if got := staffAnswerText(t, env, f.staffSession, f.eventID, f.carlaTicketID, f.sizeQuestion.ID); got == nil || *got != "S" {
		t.Errorf("the size reads %v after the buyer's refused write, want the Holder's S", got)
	}
}

// WITHOUT NAMED TICKETS NOTHING CHANGES: the buyer reads no question on a
// Ticket they assigned and cannot write one. Nor can they on a Named Tickets
// Event's unassigned Ticket, nor on anybody else's Sale; every refusal is the
// not-found a Ticket that does not exist gets.
func TestTheBuyerCannotReachAnswersTheyDoNotProvisionallyHold(t *testing.T) {
	env := setupTest(t)
	f := newNamedAssignmentFixture(t, env)
	bruno := customerSignIn(t, env, "bruno@example.com")

	for _, tc := range []struct {
		name                  string
		requires              bool
		session, sale, ticket string
	}{
		{"an assigned Ticket on an Event without Named Tickets", false, f.ana, f.anaSaleID, f.carlaTicketID},
		{"an unassigned Ticket on a Named Tickets Event", true, f.ana, f.anaSaleID, f.anaTicketIDs[1]},
		{"another Customer, on the assigned Ticket", true, bruno, f.anaSaleID, f.carlaTicketID},
		{"another Customer, naming their own Sale", true, bruno, f.brunoSaleID, f.carlaTicketID},
		{"a Ticket id belonging to nobody", true, f.ana, f.anaSaleID, "22222222-2222-4222-8222-222222222222"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setBuyerFestNamedTickets(t, env, f.assignmentFixture, tc.requires)
			resp, body := answerProvisionally(t, env, tc.session, tc.sale, tc.ticket, f.sizeQuestion.ID,
				map[string]any{"text": "XXL"})
			assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_NOT_FOUND")
		})
	}

	setBuyerFestNamedTickets(t, env, f.assignmentFixture, false)
	if raw := rawBuyerRow(t, env, f.ana, f.anaSaleID, f.carlaTicketID); strings.Contains(raw, "provisional_answers") {
		t.Errorf("an assigned Ticket on an Event without Named Tickets carries provisional Answers: %s", raw)
	}
	if got := staffAnswerText(t, env, f.staffSession, f.eventID, f.carlaTicketID, f.sizeQuestion.ID); got == nil || *got != "M" {
		t.Errorf("the size reads %v after every refused write, want M untouched", got)
	}
	var written int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM ticket_answers WHERE ticket_id = $1`, f.anaTicketIDs[1]).Scan(&written); err != nil {
		t.Fatalf("count Answers: %v", err)
	}
	if written != 0 {
		t.Errorf("%d Answers were written on the unassigned Ticket", written)
	}
}

// THE WINDOW IS THE ANSWER WINDOW: writes are refused once the Event has
// started and on a reversed Sale, while what was said stays readable to the
// buyer with the reason beside it.
func TestProvisionalAnswerWritesCloseAtTheDoorsAndOnAReversedSale(t *testing.T) {
	t.Run("the Event has started", func(t *testing.T) {
		env := setupTest(t)
		f := newNamedAssignmentFixture(t, env)
		holdClocksAt(fixedClock.Add(31 * 24 * time.Hour))

		resp, body := answerProvisionally(t, env, f.ana, f.anaSaleID, f.carlaTicketID, f.sizeQuestion.ID,
			map[string]any{"text": "XXL"})
		assertAPIError(t, resp, body, http.StatusConflict, "EVENT_STARTED_ANSWERS_CLOSED")
		row := findBuyerRow(t, listBuyerTickets(t, env, f.ana, f.anaSaleID), f.carlaTicketID)
		if got := provisionalText(t, row, f.sizeQuestion.ID); got == nil || *got != "M" {
			t.Errorf("the buyer reads %v after the doors, want M still readable", got)
		}
		if row.ProvisionalAnswers.Answerable || row.ProvisionalAnswers.AnswerableRefusal != "event_started" {
			t.Errorf("provisional Answers read %+v, want closed with event_started", row.ProvisionalAnswers)
		}
	})
	t.Run("the Sale was reversed", func(t *testing.T) {
		env := setupTest(t)
		f := newNamedAssignmentFixture(t, env)
		undoBatch(t, env, f.staffSession, f.eventID, f.anaBatchID)

		resp, body := answerProvisionally(t, env, f.ana, f.anaSaleID, f.carlaTicketID, f.sizeQuestion.ID,
			map[string]any{"text": "XXL"})
		assertAPIError(t, resp, body, http.StatusConflict, "TICKET_SALE_REVERSED")
		if got := staffAnswerText(t, env, f.staffSession, f.eventID, f.carlaTicketID, f.sizeQuestion.ID); got == nil || *got != "M" {
			t.Errorf("the size reads %v on a reversed Sale, want M untouched", got)
		}
	})
}

// A PROVISIONAL ANSWER IS PARSED BY THE QUESTION'S KIND, through the one write
// every Answer route shares: a value that does not fit is refused and writes
// nothing.
func TestAProvisionalAnswerIsParsedByTheQuestionsKind(t *testing.T) {
	env := setupTest(t)
	f := newNamedAssignmentFixture(t, env)
	resp, body := answerProvisionally(t, env, f.ana, f.anaSaleID, f.carlaTicketID, f.sizeQuestion.ID,
		map[string]any{"checked": true})
	assertAPIError(t, resp, body, http.StatusBadRequest, "INVALID_ANSWER")
	if got := staffAnswerText(t, env, f.staffSession, f.eventID, f.carlaTicketID, f.sizeQuestion.ID); got == nil || *got != "M" {
		t.Errorf("the size reads %v after a refused write, want M", got)
	}
}
