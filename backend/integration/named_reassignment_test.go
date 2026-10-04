package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	catalogrepo "github.com/peter/ticket_pos/backend/internal/catalog/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/apperror"
)

// REASSIGNMENT KEEPS THE NAMED TICKETS RULE (#673, spec #665, ADR 0076).
//
// A reassignment clears the old Holder's Answers (ADR 0046), so on an Event that
// requires Named Tickets the buyer gives the new Holder's required Answers with
// the address, or a change of plans would undo the roster the checkout was
// refused until it had. Missing ones are refused in begin-checkout's shape
// (#669), so the Storefront reads one refusal from either surface. The rule
// follows the Event and not the channel: an imported Sale's buyer is asked the
// same. Off such an Event the route is ADR 0046's, an address alone.
//
// The fixtures are newAssignmentFixture's `import` Sale of two Tickets to Ana on
// Buyer Fest (a required T-shirt size, an optional note), and the named one
// built on it, whose first Ticket is `assigned` to Carla, sized M.

// reassignmentQuestion is one entry of a buyer's row's reassignment_questions:
// the question as the checkout form asks it, and nothing anybody said.
type reassignmentQuestion struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	Required bool   `json:"required"`
}

// givenAnswer is one entry of the assign body's `answers`: the question, and
// the reply in the shape its kind takes.
func givenAnswer(questionID string, reply map[string]any) map[string]any {
	answer := map[string]any{"ticket_question_id": questionID}
	for key, value := range reply {
		answer[key] = value
	}
	return answer
}

// assignWithAnswers names an address for one Ticket with Answers beside it and
// returns the whole response, refusal and all.
func assignWithAnswers(
	t *testing.T, env *testEnv, session, ticketSaleID, ticketID, holderEmail string, answers ...map[string]any,
) (*http.Response, envelope) {
	t.Helper()
	if answers == nil {
		answers = []map[string]any{}
	}
	return env.put(t, buyerAssignmentPath(ticketSaleID, ticketID),
		map[string]any{"holder_email": holderEmail, "answers": answers}, authHeader(session))
}

// assignWithAnswersOK assigns with Answers and returns the whole Sale's rows,
// checking their bytes as every buyer read is checked.
func assignWithAnswersOK(
	t *testing.T, env *testEnv, session, ticketSaleID, ticketID, holderEmail string, answers ...map[string]any,
) []buyerTicket {
	t.Helper()
	resp, body := assignWithAnswers(t, env, session, ticketSaleID, ticketID, holderEmail, answers...)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("assign %s to %q with answers status=%d error=%+v", ticketID, holderEmail, resp.StatusCode, body.Error)
	}
	assertNoAnswerOnTheBuyersRows(t, body.Data)
	return decodeBuyerTickets(t, body.Data)
}

// refusedAsUnanswered asserts an assignment was refused for Named Tickets and
// returns what the refusal says is owed, read exactly as the checkout's is.
func refusedAsUnanswered(t *testing.T, resp *http.Response, body envelope) []owedTicket {
	t.Helper()
	assertAPIError(t, resp, body, http.StatusBadRequest, "NAMED_TICKETS_INCOMPLETE")
	raw, err := json.Marshal(body.Error.Details)
	if err != nil {
		t.Fatalf("encode details: %v", err)
	}
	var details namedRefusal
	if err := json.Unmarshal(raw, &details); err != nil {
		t.Fatalf("decode details %s: %v", raw, err)
	}
	for _, ticket := range details.Tickets {
		if ticket.MissingQuestionIDs == nil {
			t.Fatalf("details = %s: missing_question_ids must be an array, never null", raw)
		}
	}
	return details.Tickets
}

// namedTicketsAssignmentFixture is newAssignmentFixture with the Event set to
// require Named Tickets, and nothing assigned yet.
func namedTicketsAssignmentFixture(t *testing.T, env *testEnv) assignmentFixture {
	t.Helper()
	f := newAssignmentFixture(t, env)
	setBuyerFestNamedTickets(t, env, f, true)
	return f
}

// answersOnTicket is how many Answers one Ticket carries, by SQL because the
// buyer is shown none of them on a Ticket they do not hold.
func answersOnTicket(t *testing.T, env *testEnv, ticketID string) int {
	t.Helper()
	var count int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM ticket_answers WHERE ticket_id = $1`, ticketID).Scan(&count); err != nil {
		t.Fatalf("count Answers on %s: %v", ticketID, err)
	}
	return count
}

// assignmentMailsTo is every Assignment mail captured for one address, for the
// assertions that none went; assignmentMailFor insists on exactly one.
func assignmentMailsTo(env *testEnv, address string) []platform.TicketAssignment {
	var found []platform.TicketAssignment
	for _, mail := range env.email.TicketAssignmentsSent() {
		if mail.To == address {
			found = append(found, mail)
		}
	}
	return found
}

// THE REFUSAL: an address without the required Answers is refused in the shape
// begin-checkout refuses with, naming the Ticket by its Ticket Type and its
// ordinal and the question it owes - and nothing is written, nobody is mailed.
// An Answer the question cannot take is missing, and an optional one alone
// pays nothing.
func TestReassigningOnANamedTicketsEventWithoutTheRequiredAnswersIsRefused(t *testing.T) {
	env := setupTest(t)
	f := namedTicketsAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[1]
	mailBefore := assignmentMailCount(env)

	for _, tc := range []struct {
		name    string
		answers []map[string]any
	}{
		{"no answers at all", nil},
		{"only the optional one", []map[string]any{
			givenAnswer(f.extraQuestion.ID, map[string]any{"text": "Arriving late"}),
		}},
		{"a blank size", []map[string]any{givenAnswer(f.sizeQuestion.ID, map[string]any{"text": "   "})}},
		{"a size in the wrong shape", []map[string]any{givenAnswer(f.sizeQuestion.ID, map[string]any{"checked": true})}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := assignWithAnswers(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com", tc.answers...)
			assertOwed(t, refusedAsUnanswered(t, resp, body), owedTicket{
				TicketTypeID:       f.ticketTypeID,
				TicketIndex:        2,
				MissingQuestionIDs: []string{f.sizeQuestion.ID},
			})
		})
	}

	// The body without an `answers` key at all reads the same.
	resp, body := assignTicket(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")
	refusedAsUnanswered(t, resp, body)

	if row := readTicketAssignment(t, env, ticketID); row.holderEmail.Valid || row.assignedAt.Valid {
		t.Errorf("a refused reassignment wrote holder=%v assigned_at=%v", row.holderEmail, row.assignedAt)
	}
	if got := answersOnTicket(t, env, ticketID); got != 0 {
		t.Errorf("a refused reassignment wrote %d Answer(s)", got)
	}
	if got := assignmentMailCount(env); got != mailBefore {
		t.Errorf("a refused reassignment sent %d Assignment mail(s)", got-mailBefore)
	}
}

// THE CENTRE OF THE TICKET: the buyer hands Carla's Ticket to Diego, giving
// Diego's size and a note with the address. Carla's Answers are gone, Diego's are
// on the Ticket, Diego is mailed, and the Organization reads Diego's roster line
// complete.
func TestReassigningWithTheAnswersReplacesTheOldHoldersAnswers(t *testing.T) {
	env := setupTest(t)
	f := newNamedAssignmentFixture(t, env)
	answerProvisionallyOK(t, env, f.ana, f.anaSaleID, f.carlaTicketID, f.extraQuestion.ID,
		map[string]any{"text": "Carla arrives late"})

	holdClocksAt(fixedClock.Add(time.Hour))
	returned := assignWithAnswersOK(t, env, f.ana, f.anaSaleID, f.carlaTicketID, "diego@example.com",
		givenAnswer(f.sizeQuestion.ID, map[string]any{"text": "L"}))

	row := findBuyerRow(t, returned, f.carlaTicketID)
	if row.AssignmentState != "assigned" || row.HolderEmail != "diego@example.com" {
		t.Fatalf("the reassigned Ticket reads state=%q holder=%q, want assigned to diego", row.AssignmentState, row.HolderEmail)
	}
	// Diego's Answers are the buyer's to correct until he accepts (#672).
	if got := provisionalText(t, row, f.sizeQuestion.ID); got == nil || *got != "L" {
		t.Errorf("the buyer reads %v for Diego's size, want L", got)
	}
	if got := staffAnswerText(t, env, f.staffSession, f.eventID, f.carlaTicketID, f.sizeQuestion.ID); got == nil || *got != "L" {
		t.Errorf("Event Staff read %v for the size, want Diego's L", got)
	}
	// CARLA'S NOTE IS NOT DIEGO'S. The reassignment cleared it, and nothing
	// given with the address restated it.
	if got := staffAnswerText(t, env, f.staffSession, f.eventID, f.carlaTicketID, f.extraQuestion.ID); got != nil {
		t.Errorf("Diego inherited Carla's note %q", *got)
	}
	if !owesNothing(t, listOutstanding(t, env, f.staffSession, f.eventID), f.carlaTicketID) {
		t.Error("the Holder List owes on Diego's Ticket after a reassignment that gave his size")
	}
	assignmentMailFor(t, env, "diego@example.com")

	// AN OPTIONAL ANSWER MAY TRAVEL TOO, and is written with the required one.
	holdClocksAt(fixedClock.Add(2 * time.Hour))
	assignWithAnswersOK(t, env, f.ana, f.anaSaleID, f.carlaTicketID, "elena@example.com",
		givenAnswer(f.sizeQuestion.ID, map[string]any{"text": "S"}),
		givenAnswer(f.extraQuestion.ID, map[string]any{"text": "Vegetarian"}))
	if got := staffAnswerText(t, env, f.staffSession, f.eventID, f.carlaTicketID, f.sizeQuestion.ID); got == nil || *got != "S" {
		t.Errorf("Event Staff read %v for the size, want Elena's S", got)
	}
	if got := staffAnswerText(t, env, f.staffSession, f.eventID, f.carlaTicketID, f.extraQuestion.ID); got == nil || *got != "Vegetarian" {
		t.Errorf("Event Staff read %v for the note, want Elena's optional Answer", got)
	}
}

// ONE TRANSACTION. When writing the new Holder's Answers fails, the address,
// the clearing of the old Holder's Answers and every timestamp go with it: the
// Ticket is still Carla's, sized M, and her Assignment Link still opens. The
// failure is forced with a trigger, because nothing a buyer can send makes a
// validated Answer fail to write.
func TestAReassignmentWhoseAnswersFailToWriteLeavesTheTicketAsItWas(t *testing.T) {
	env := setupTest(t)
	f := newNamedAssignmentFixture(t, env)
	before := readTicketAssignment(t, env, f.carlaTicketID)

	if _, err := env.db.Exec(`
		CREATE FUNCTION refuse_boom_answer_673() RETURNS trigger AS $$
		BEGIN
			IF NEW.text_value = 'BOOM' THEN RAISE EXCEPTION 'refused for the test'; END IF;
			RETURN NEW;
		END $$ LANGUAGE plpgsql;
		CREATE TRIGGER refuse_boom_answer_673 BEFORE INSERT OR UPDATE ON ticket_answers
			FOR EACH ROW EXECUTE FUNCTION refuse_boom_answer_673();
	`); err != nil {
		t.Fatalf("install the refusing trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = env.db.Exec(`
			DROP TRIGGER IF EXISTS refuse_boom_answer_673 ON ticket_answers;
			DROP FUNCTION IF EXISTS refuse_boom_answer_673();
		`)
	})

	holdClocksAt(fixedClock.Add(time.Hour))
	resp, body := assignWithAnswers(t, env, f.ana, f.anaSaleID, f.carlaTicketID, "diego@example.com",
		givenAnswer(f.sizeQuestion.ID, map[string]any{"text": "BOOM"}))
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d error=%+v, want the forced 500", resp.StatusCode, body.Error)
	}

	after := readTicketAssignment(t, env, f.carlaTicketID)
	if after.holderEmail != before.holderEmail || !after.assignedAt.Time.Equal(before.assignedAt.Time) {
		t.Errorf("a failed reassignment moved the assignment: %+v then %+v", before, after)
	}
	if got := staffAnswerText(t, env, f.staffSession, f.eventID, f.carlaTicketID, f.sizeQuestion.ID); got == nil || *got != "M" {
		t.Errorf("a failed reassignment left the size at %v, want Carla's M back", got)
	}
	if mails := assignmentMailsTo(env, "diego@example.com"); len(mails) != 0 {
		t.Errorf("a failed reassignment mailed Diego %d time(s)", len(mails))
	}
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))
}

// THE BUYER'S OWN ADDRESS STILL OWES THE ANSWERS on a Named Tickets Event - the
// buyer's own Ticket owes them at checkout too - and given them, it is accepted
// at once with nothing mailed (#668).
func TestOwnAddressReassignmentOnANamedTicketsEventNeedsTheAnswersAndMailsNobody(t *testing.T) {
	env := setupTest(t)
	f := newNamedAssignmentFixture(t, env)
	mailBefore := assignmentMailCount(env)

	resp, body := assignTicket(t, env, f.ana, f.anaSaleID, f.carlaTicketID, "ana@example.com")
	assertOwed(t, refusedAsUnanswered(t, resp, body), owedTicket{
		TicketTypeID:       f.ticketTypeID,
		TicketIndex:        1,
		MissingQuestionIDs: []string{f.sizeQuestion.ID},
	})
	if row := readTicketAssignment(t, env, f.carlaTicketID); row.holderEmail.String != "carla@example.com" {
		t.Fatalf("a refused own-address reassignment moved the Ticket to %v", row.holderEmail)
	}

	holdClocksAt(fixedClock.Add(time.Hour))
	returned := assignWithAnswersOK(t, env, f.ana, f.anaSaleID, f.carlaTicketID, " Ana@Example.com ",
		givenAnswer(f.sizeQuestion.ID, map[string]any{"text": "XS"}))
	assertOwnAddressAccepted(t, findBuyerRow(t, returned, f.carlaTicketID), "ana@example.com")
	if got := staffAnswerText(t, env, f.staffSession, f.eventID, f.carlaTicketID, f.sizeQuestion.ID); got == nil || *got != "XS" {
		t.Errorf("Event Staff read %v for the size, want the buyer's XS", got)
	}
	if got := assignmentMailCount(env); got != mailBefore {
		t.Errorf("an own-address reassignment sent %d Assignment mail(s)", got-mailBefore)
	}
	// Carla never accepted, so she was never told she had anything to lose.
	if mails := noLongerHoldingMailsTo(env, "carla@example.com"); len(mails) != 0 {
		t.Errorf("Carla, who never accepted, was sent %d No Longer Holding mail(s)", len(mails))
	}
}

// THE RULE FOLLOWS THE EVENT, NOT THE CHANNEL. An imported Sale was never
// refused by Named Tickets, and an online one was; their buyers reassigning on
// the same kind of Event are asked the same.
func TestImportedAndOnlineSalesOnANamedTicketsEventAreAskedTheSame(t *testing.T) {
	t.Run("an imported Sale", func(t *testing.T) {
		env := setupTest(t)
		f := namedTicketsAssignmentFixture(t, env)
		var channel string
		if err := env.db.QueryRow(`SELECT channel FROM ticket_sales WHERE id = $1`, f.anaSaleID).Scan(&channel); err != nil {
			t.Fatalf("read the Sale's channel: %v", err)
		}
		if channel != "import" {
			t.Fatalf("the fixture Sale is %q, want an import", channel)
		}

		resp, body := assignTicket(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[1], "carla@example.com")
		refusedAsUnanswered(t, resp, body)
		assignWithAnswersOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[1], "carla@example.com",
			givenAnswer(f.sizeQuestion.ID, map[string]any{"text": "M"}))
		if got := staffAnswerText(t, env, f.staffSession, f.eventID, f.anaTicketIDs[1], f.sizeQuestion.ID); got == nil || *got != "M" {
			t.Errorf("Event Staff read %v, want the M given with the address", got)
		}
	})

	t.Run("an online Sale", func(t *testing.T) {
		env := setupTest(t)
		f := newNamedCommitFixture(t, env, 2000)
		begun := beginCheckoutOK(t, env, testOrgSlug, "named-fest", f.familyBasket())
		confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
		saleID := saleIDOfPayment(t, env, begun.ClientTransactionID)
		ana := customerSignIn(t, env, "ana@example.com")
		ben := buyerTicketAt(t, listBuyerTickets(t, env, ana, saleID), 2)

		resp, body := assignTicket(t, env, ana, saleID, ben.TicketID, "dora@example.com")
		assertOwed(t, refusedAsUnanswered(t, resp, body), owedTicket{
			TicketTypeID:       f.gaID,
			TicketIndex:        2,
			MissingQuestionIDs: []string{f.size.ID},
		})
		holdClocksAt(fixedClock.Add(time.Hour))
		returned := assignWithAnswersOK(t, env, ana, saleID, ben.TicketID, "dora@example.com",
			givenAnswer(f.size.ID, map[string]any{"text": "XL"}))
		if got := provisionalText(t, buyerTicketAt(t, returned, 2), f.size.ID); got == nil || *got != "XL" {
			t.Errorf("the buyer reads %v for Dora's size, want XL", got)
		}
	})
}

// WITHOUT NAMED TICKETS THE ROUTE IS ADR 0046's: an address alone is enough,
// the row lists no questions, and Answers sent anyway are not written - only the
// Holder answers on such an Event (ADR 0049).
func TestWithoutNamedTicketsTheRouteAsksForNoAnswers(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)

	returned := assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[1], "carla@example.com")
	for _, row := range returned {
		if row.ReassignmentQuestions != nil {
			t.Errorf("Ticket %d lists reassignment questions on an Event without Named Tickets", row.Ordinal)
		}
	}

	holdClocksAt(fixedClock.Add(time.Hour))
	assignWithAnswersOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[1], "diego@example.com",
		givenAnswer(f.sizeQuestion.ID, map[string]any{"text": "XXL"}))
	if got := answersOnTicket(t, env, f.anaTicketIDs[1]); got != 0 {
		t.Errorf("the buyer's Answers were written on an Event without Named Tickets (%d)", got)
	}
}

// AFTER THE DOORS NOTHING CHANGES: a reassignment is refused as today, Answers
// or not, and the rows stop listing questions nobody can answer any more.
func TestReassigningOnANamedTicketsEventAfterItHasStartedIsStillRefused(t *testing.T) {
	env := setupTest(t)
	f := newNamedAssignmentFixture(t, env)

	holdClocksAt(fixedClock.Add(31 * 24 * time.Hour))
	resp, body := assignWithAnswers(t, env, f.ana, f.anaSaleID, f.carlaTicketID, "diego@example.com",
		givenAnswer(f.sizeQuestion.ID, map[string]any{"text": "L"}))
	assertAPIError(t, resp, body, http.StatusConflict, "ASSIGNMENT_EVENT_STARTED")
	if row := readTicketAssignment(t, env, f.carlaTicketID); row.holderEmail.String != "carla@example.com" {
		t.Errorf("a refused reassignment after the start moved the Ticket to %v", row.holderEmail)
	}
	if got := staffAnswerText(t, env, f.staffSession, f.eventID, f.carlaTicketID, f.sizeQuestion.ID); got == nil || *got != "M" {
		t.Errorf("the size reads %v, want Carla's M untouched", got)
	}
	for _, row := range listBuyerTickets(t, env, f.ana, f.anaSaleID) {
		if row.ReassignmentQuestions != nil {
			t.Errorf("Ticket %d lists reassignment questions after the Event started", row.Ordinal)
		}
	}
}

// THE BUYER'S ROWS SAY WHAT A REASSIGNMENT WILL ASK, so the Storefront can draw
// the fields beside the address: the Ticket Type's questions as checkout asks
// them, the required one marked, on every row that may be reassigned - and no
// Answer on any of them, the accepted Holder's included.
func TestTheBuyersRowsListTheQuestionsAReassignmentMustAnswer(t *testing.T) {
	env := setupTest(t)
	f := newNamedAssignmentFixture(t, env)
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))

	for _, row := range listBuyerTickets(t, env, f.ana, f.anaSaleID) {
		if len(row.ReassignmentQuestions) != 2 {
			t.Fatalf("Ticket %d (%s) lists %+v, want the size and the note", row.Ordinal, row.AssignmentState,
				row.ReassignmentQuestions)
		}
		size, note := row.ReassignmentQuestions[0], row.ReassignmentQuestions[1]
		if size.ID != f.sizeQuestion.ID || !size.Required || size.Kind != "short_text" || size.Label != "T-shirt size" {
			t.Errorf("Ticket %d's first question reads %+v, want the required size", row.Ordinal, size)
		}
		if note.ID != f.extraQuestion.ID || note.Required {
			t.Errorf("Ticket %d's second question reads %+v, want the optional note", row.Ordinal, note)
		}
	}
	// listBuyerTickets checked the bytes: no Answer travels under the field.
}

// THE RATIONING IS UNTOUCHED: a reassignment refused for its Answers is refused
// before anything is sent, so it spends no allowance, and the one the buyer
// then completes is mailed as any reassignment is.
func TestARefusedReassignmentSpendsNoAssignmentMailAllowance(t *testing.T) {
	env := setupTest(t)
	f := namedTicketsAssignmentFixture(t, env)
	withAssignmentMailLimits(t, catalog.AssignmentMailLimits{PerTicket: 1, PerBuyer: 1})

	for range 3 {
		resp, body := assignTicket(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[1], "carla@example.com")
		refusedAsUnanswered(t, resp, body)
	}
	assignWithAnswersOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[1], "carla@example.com",
		givenAnswer(f.sizeQuestion.ID, map[string]any{"text": "M"}))
	if got := assignmentMailCount(env); got != 1 {
		t.Errorf("sent %d Assignment mail(s), want the one completed reassignment's", got)
	}
}

// THE SAME ADDRESS AGAIN IS STILL A NO-OP: it names nobody new, so the Answers
// beside it are not written - the Ticket's Answers may already be a Holder's.
func TestResubmittingTheSameAddressWithAnswersWritesNothing(t *testing.T) {
	env := setupTest(t)
	f := newNamedAssignmentFixture(t, env)
	before := readTicketAssignment(t, env, f.carlaTicketID)
	mailBefore := assignmentMailCount(env)

	holdClocksAt(fixedClock.Add(time.Hour))
	assignWithAnswersOK(t, env, f.ana, f.anaSaleID, f.carlaTicketID, "carla@example.com",
		givenAnswer(f.sizeQuestion.ID, map[string]any{"text": "XXL"}))

	if after := readTicketAssignment(t, env, f.carlaTicketID); !after.assignedAt.Time.Equal(before.assignedAt.Time) {
		t.Errorf("a same-address resubmission moved assigned_at: %v then %v", before.assignedAt, after.assignedAt)
	}
	if got := staffAnswerText(t, env, f.staffSession, f.eventID, f.carlaTicketID, f.sizeQuestion.ID); got == nil || *got != "M" {
		t.Errorf("a same-address resubmission rewrote the size to %v, want M", got)
	}
	if got := assignmentMailCount(env); got != mailBefore {
		t.Errorf("a same-address resubmission sent %d Assignment mail(s)", got-mailBefore)
	}
}

// THE VERDICT IS APPLIED UNDER THE LOCK. The service judges the Answers on its
// own earlier read of the Ticket; if a concurrent save moved the address in
// between, that read is stale, and only the locked row says whether this call
// is a change. Driven at the repository, because no request can be made to
// interleave there: the same verdict refuses a call that turns out to change
// the address, and is ignored by one that turns out not to.
func TestTheNamedTicketsVerdictIsDecidedUnderTheRowLock(t *testing.T) {
	env := setupTest(t)
	f := newNamedAssignmentFixture(t, env)
	repo := catalogrepo.New(&platform.DB{Pool: env.db})
	before := readTicketAssignment(t, env, f.carlaTicketID)
	var anaCustomerID string
	if err := env.db.QueryRow(`SELECT customer_id FROM ticket_sales WHERE id = $1`, f.anaSaleID).Scan(&anaCustomerID); err != nil {
		t.Fatalf("read the Sale's Customer: %v", err)
	}
	owing := &catalogrepo.NamedTicketsVerdict{Owed: []catalog.OwedTicket{{
		TicketTypeID:       f.ticketTypeID,
		TicketIndex:        1,
		MissingQuestionIDs: []string{f.sizeQuestion.ID},
	}}}
	assign := func(address string) (catalogrepo.AssignTicketResult, error) {
		return repo.AssignTicketToHolder(context.Background(), catalogrepo.AssignTicketInput{
			TicketID:        f.carlaTicketID,
			HolderEmail:     address,
			BuyerCustomerID: anaCustomerID,
			NamedTickets:    owing,
			Now:             fixedClock.Add(time.Hour),
		})
	}

	// The service read the Ticket as already Diego's; it is Carla's.
	_, err := assign("diego@example.com")
	var refusal apperror.DomainError
	if !errors.As(err, &refusal) || refusal.Code() != "NAMED_TICKETS_INCOMPLETE" {
		t.Fatalf("a change of address owing its Answers returned %v, want NAMED_TICKETS_INCOMPLETE", err)
	}
	if after := readTicketAssignment(t, env, f.carlaTicketID); after.holderEmail != before.holderEmail ||
		!after.assignedAt.Time.Equal(before.assignedAt.Time) {
		t.Errorf("a refused change moved the assignment: %+v then %+v", before, after)
	}
	if got := answersOnTicket(t, env, f.carlaTicketID); got != 1 {
		t.Errorf("a refused change left %d Answer(s), want Carla's 1", got)
	}

	// The service read the Ticket as somebody else's; it is already Carla's.
	result, err := assign("carla@example.com")
	if err != nil || result.Changed {
		t.Fatalf("a same-address call owing Answers returned %+v, %v, want a no-op", result, err)
	}
}

// THE SAME ADDRESS ASKS FOR NO ANSWERS. Whether a call names somebody new is
// decided under the row lock, and a call that does not is a no-op success
// whether or not it carries Answers - a buyer pressing save twice, or saving an
// accepted Holder's address back unchanged, owes nothing and is shown nothing
// of that Holder's Answers (ADR 0049).
func TestResubmittingTheSameAddressWithoutAnswersIsANoOp(t *testing.T) {
	for _, tc := range []struct {
		name   string
		accept bool
	}{
		{"while Carla has not accepted", false},
		{"after Carla accepted", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := setupTest(t)
			f := newNamedAssignmentFixture(t, env)
			if tc.accept {
				acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))
			}
			before := readTicketAssignment(t, env, f.carlaTicketID)
			mailBefore := assignmentMailCount(env)

			holdClocksAt(fixedClock.Add(time.Hour))
			assignTicketOK(t, env, f.ana, f.anaSaleID, f.carlaTicketID, " Carla@Example.com ")

			after := readTicketAssignment(t, env, f.carlaTicketID)
			if !after.assignedAt.Time.Equal(before.assignedAt.Time) || after.acceptedAt.Valid != before.acceptedAt.Valid {
				t.Errorf("a same-address resubmission moved the assignment: %+v then %+v", before, after)
			}
			if got := staffAnswerText(t, env, f.staffSession, f.eventID, f.carlaTicketID, f.sizeQuestion.ID); got == nil || *got != "M" {
				t.Errorf("a same-address resubmission left the size at %v, want M", got)
			}
			if got := assignmentMailCount(env); got != mailBefore {
				t.Errorf("a same-address resubmission sent %d Assignment mail(s)", got-mailBefore)
			}
		})
	}
}
