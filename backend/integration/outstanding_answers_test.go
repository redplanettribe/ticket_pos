package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The Holder List (#333; the Outstanding Answers surface of #313, widened to
// the roster): every Ticket of an Event, who is coming on each, and — where the
// Event asks Ticket Questions — which questions each still owes. Outstanding
// Answers is the `outstanding=true` FILTER on this list, never its definition.
//
// THESE TESTS ARE WHERE THE DEBT'S DEFINITION IS HELD TOGETHER. The rule lives twice —
// in Go as catalog.IsOutstandingAnswer and in SQL as
// repository.outstandingAnswerWhere — because an Event's whole ticket roll
// cannot be filtered in Go. The unit tests beside the Go one prove the four
// clauses; these prove the SQL agrees with them against real rows. Neither
// statement of the rule may be changed without the other, and this file is what
// notices.

// outstandingAnswers decodes the Event's Outstanding Answers page.
type outstandingAnswers struct {
	Data []struct {
		TicketID          string    `json:"ticket_id"`
		Ordinal           int       `json:"ordinal"`
		TicketTypeID      string    `json:"ticket_type_id"`
		TicketTypeName    string    `json:"ticket_type_name"`
		TicketSaleID      string    `json:"ticket_sale_id"`
		ConfirmationRef   string    `json:"confirmation_ref"`
		Channel           string    `json:"channel"`
		CustomerFirstName string    `json:"customer_first_name"`
		CustomerLastName  string    `json:"customer_last_name"`
		CustomerEmail     string    `json:"customer_email"`
		SoldAt            time.Time `json:"sold_at"`
		// The Holder (#329). Every one of these is ABSENT while
		// TICKET_ASSIGNMENT_ENABLED is closed, which is one of the two flags
		// that open this surface — see the flag tests below.
		AssignmentState string `json:"assignment_state"`
		// NeverAccepted marks a Ticket whose unaccepted address the retention
		// purge took: assigned, never claimed, address gone (#334).
		NeverAccepted   bool   `json:"never_accepted"`
		HolderFirstName string `json:"holder_first_name"`
		HolderLastName  string `json:"holder_last_name"`
		HolderEmail     string `json:"holder_email"`
		Outstanding     []struct {
			QuestionID string `json:"question_id"`
			Label      string `json:"label"`
			Kind       string `json:"kind"`
			SortOrder  int    `json:"sort_order"`
		} `json:"outstanding"`
	} `json:"data"`
	Pagination struct {
		Page       int `json:"page"`
		PageSize   int `json:"page_size"`
		Total      int `json:"total"`
		TotalPages int `json:"total_pages"`
	} `json:"pagination"`
	OutstandingCount int `json:"outstanding_count"`
}

func outstandingPath(eventID string) string {
	return "/api/v1/staff/events/" + eventID + "/outstanding-answers"
}

func decodeOutstanding(t *testing.T, data json.RawMessage) outstandingAnswers {
	t.Helper()
	var page outstandingAnswers
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatalf("decode outstanding answers: %v", err)
	}
	return page
}

// listOutstanding reads the surface, failing on any refusal — for tests whose
// subject is what is ON the list rather than who may see it.
func listOutstanding(t *testing.T, env *testEnv, sessionID, eventID string) outstandingAnswers {
	t.Helper()
	resp, body := env.get(t, outstandingPath(eventID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("outstanding status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return decodeOutstanding(t, body.Data)
}

// labelsOwedBy collects what one Ticket owes, so a test can say what it means
// without indexing into the payload.
func labelsOwedBy(page outstandingAnswers, ticketID string) []string {
	for _, row := range page.Data {
		if row.TicketID != ticketID {
			continue
		}
		labels := make([]string, 0, len(row.Outstanding))
		for _, question := range row.Outstanding {
			labels = append(labels, question.Label)
		}
		return labels
	}
	return nil
}

// owesNothing reports that a Ticket is ON the roster and owes nothing — since
// #333 a fully answered Ticket STAYS on the Holder List with an empty
// `outstanding`, so "answered" and "absent" are opposite facts and this helper
// refuses to conflate them.
func owesNothing(t *testing.T, page outstandingAnswers, ticketID string) bool {
	t.Helper()
	for _, row := range page.Data {
		if row.TicketID == ticketID {
			return len(row.Outstanding) == 0
		}
	}
	t.Fatalf("Ticket %s is not on the Holder List at all — the roster lost a Ticket", ticketID)
	return false
}

// Nothing about the Holder List is reachable while BOTH flags are off — the
// same 404 a build without either feature gives, on the same terms as every
// other Answer route (ADR 0045). One flag suffices to open it (#333); that is
// the next test's subject.
func TestTheHolderListIsInvisibleWhileBothFlagsAreOff(t *testing.T) {
	env := setupTest(t)
	// Deliberately calling neither enableTicketQuestions nor
	// enableTicketAssignment: this is the shipped state.
	sessionID, eventID, _, _, _, _ := answeredFixture(t, env)

	resp, body := env.get(t, outstandingPath(eventID), authHeader(sessionID))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 while both features are dark; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "TICKET_QUESTIONS_UNAVAILABLE" {
		t.Fatalf("error=%+v, want TICKET_QUESTIONS_UNAVAILABLE", body.Error)
	}
}

// THE HOLDER LIST OPENS ON ASSIGNMENT ALONE (#333). An Organization that
// assigns 80 tickets and asks nothing came here for "who is coming", and that
// is the headline value of Ticket Assignment — so the read is gated on EITHER
// flag, and an Event with no Ticket Questions still has a roster.
//
// AND IT SAYS NOTHING ABOUT DEBTS, because with TICKET_QUESTIONS_ENABLED
// closed there is no such thing as one: `outstanding` and `outstanding_count`
// are ABSENT — not empty, absent — so this payload admits nothing about a
// feature that is not shipping (ADR 0045). Asserted on the raw body, because a
// decoded struct cannot tell absent from empty.
func TestTheHolderListOpensOnAssignmentAlone(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	// Questions stay CLOSED, and no question exists: the fixture's Event asks
	// nothing at all.
	sessionID, eventID, _, _, _, ticketIDs := answeredFixture(t, env)

	resp, body := env.get(t, outstandingPath(eventID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d error=%+v — an Organization that asks nothing still has a Holder List",
			resp.StatusCode, body.Error)
	}
	page := decodeOutstanding(t, body.Data)
	if page.Pagination.Total != 2 || len(page.Data) != 2 {
		t.Fatalf("tickets = %d (total %d), want the Event's two — the roster is every Ticket",
			len(page.Data), page.Pagination.Total)
	}
	for _, ticketID := range ticketIDs {
		if got := guestRow(t, page, ticketID); got.state != "unassigned" {
			t.Errorf("Ticket %s reads state=%q, want `unassigned` with the roster visible", ticketID, got.state)
		}
	}
	if strings.Contains(string(body.Data), "outstanding") {
		t.Error("the response speaks of `outstanding` while TICKET_QUESTIONS_ENABLED is closed.\n" +
			"A build with questions dark must send the bytes a build without the feature sends (ADR 0045).")
	}

	// The Outstanding Answers filter belongs to the questions feature and is
	// IGNORED while it is dark, rather than becoming a side channel that
	// filters by a debt the platform says does not exist.
	resp, body = env.get(t, outstandingPath(eventID)+"?outstanding=true", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("filtered status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if page := decodeOutstanding(t, body.Data); page.Pagination.Total != 2 {
		t.Fatalf("filtered total = %d, want the whole roster — the filter is dark with the questions feature",
			page.Pagination.Total)
	}
}

// THE WHOLE MEANING OF `required`. Only a required question produces a debt; an
// unanswered OPTIONAL question is not one, because nobody promised to answer it
// and there is nothing to chase.
func TestOnlyRequiredTicketQuestionsProduceOutstandingAnswers(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, ticketIDs := answeredFixture(t, env)

	createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})
	createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "Anything else we should know?", "kind": "long_text", "required": false,
	})

	page := listOutstanding(t, env, sessionID, eventID)

	// Both Tickets of the sale owe, and each owes exactly the ONE required
	// question. The optional one is answered by nobody and is on nobody's list.
	if page.Pagination.Total != 2 {
		t.Fatalf("tickets owing = %d, want both of the sale's two", page.Pagination.Total)
	}
	if page.OutstandingCount != 2 {
		t.Fatalf("outstanding answers = %d, want 2 — one required question on each of two Tickets", page.OutstandingCount)
	}
	for _, ticketID := range ticketIDs {
		labels := labelsOwedBy(page, ticketID)
		if len(labels) != 1 || labels[0] != "T-shirt size" {
			t.Fatalf("ticket %s owes %v, want only the required question", ticketID, labels)
		}
	}
}

// The list empties as Answers arrive, and refills when one is taken away. It is
// DERIVED on every read: nothing sweeps it and nothing invalidates it, which is
// why an Answer from any route removes its row without anything being told.
func TestOutstandingAnswersEmptyAsAnswersArrive(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, ticketIDs := answeredFixture(t, env)

	size := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})
	dinner := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "Coming to the dinner?", "kind": "checkbox", "required": true,
	})

	if page := listOutstanding(t, env, sessionID, eventID); page.OutstandingCount != 4 {
		t.Fatalf("outstanding = %d, want 4 — two required questions on each of two Tickets", page.OutstandingCount)
	}

	putAnswer(t, env, sessionID, eventID, ticketIDs[0], size.ID, map[string]any{"text": "M"})

	page := listOutstanding(t, env, sessionID, eventID)
	if page.OutstandingCount != 3 {
		t.Fatalf("outstanding = %d after one Answer, want 3", page.OutstandingCount)
	}
	if labels := labelsOwedBy(page, ticketIDs[0]); len(labels) != 1 || labels[0] != "Coming to the dinner?" {
		t.Fatalf("ticket owes %v, want only the question it has not answered", labels)
	}

	// FALSE IS AN ANSWER. Somebody who read "Coming to the dinner?" and left it
	// unticked has said no, which is a different fact from never having been
	// asked — and it discharges the debt exactly as any other reply does. The
	// rule reads the Answer's EXISTENCE and never its content.
	putAnswer(t, env, sessionID, eventID, ticketIDs[0], dinner.ID, map[string]any{"checked": false})

	page = listOutstanding(t, env, sessionID, eventID)
	if !owesNothing(t, page, ticketIDs[0]) {
		t.Fatalf("a fully answered Ticket still owes: %v", labelsOwedBy(page, ticketIDs[0]))
	}
	// THE ROSTER KEEPS THE ANSWERED TICKET (#333). Both Tickets stay listed —
	// the list is every Ticket of the Event, and answering leaves it with an
	// empty debt rather than off the sheet. Only the debt count moves.
	if page.Pagination.Total != 2 || page.OutstandingCount != 2 {
		t.Fatalf("tickets=%d outstanding=%d, want both Tickets on the roster with the second's two debts alone",
			page.Pagination.Total, page.OutstandingCount)
	}

	// And the Outstanding Answers FILTER is where the old list went: only the
	// Ticket that still owes.
	resp2, body2 := env.get(t, outstandingPath(eventID)+"?outstanding=true", authHeader(sessionID))
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("filtered status=%d error=%+v", resp2.StatusCode, body2.Error)
	}
	filtered := decodeOutstanding(t, body2.Data)
	if filtered.Pagination.Total != 1 || len(filtered.Data) != 1 || filtered.Data[0].TicketID != ticketIDs[1] {
		t.Fatalf("filtered rows=%d total=%d, want only the owing Ticket — Outstanding Answers is a filter of the roster",
			len(filtered.Data), filtered.Pagination.Total)
	}
	// The Event's debt count is the Event's, unmoved by the filter.
	if filtered.OutstandingCount != 2 {
		t.Fatalf("filtered outstanding_count=%d, want the Event's 2", filtered.OutstandingCount)
	}

	// Removing an Answer is the way back to "not said", and it restores the
	// debt. That is what makes the removal honest rather than a blank row.
	resp, body := env.deleteJSON(t, answerPath(eventID, ticketIDs[0], size.ID), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove answer status=%d error=%+v", resp.StatusCode, body.Error)
	}
	page = listOutstanding(t, env, sessionID, eventID)
	if labels := labelsOwedBy(page, ticketIDs[0]); len(labels) != 1 || labels[0] != "T-shirt size" {
		t.Fatalf("ticket owes %v after the Answer was removed, want the debt restored", labels)
	}
}

// THE RETIRED-QUESTION RULING, at the level that can prove it matters.
//
// A retired question owes nothing, because the write path refuses one on every
// route into an Answer — as this test drives, immediately after. A retired
// required question left on the list would be a row with no working button
// behind it, and an Answer Reminder chasing a buyer about a question that has
// left the form they would be sent to.
//
// It erases nothing: the Answers already given under the retired question are
// untouched and still read on the Ticket.
func TestARetiredTicketQuestionOwesNothing(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, ticketIDs := answeredFixture(t, env)

	doomed := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "Bus from the station?", "kind": "checkbox", "required": true,
	})
	kept := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})

	// One Ticket answers the question that is about to be retired, so this test
	// also witnesses that retiring takes the DEBT and never the RECORD.
	putAnswer(t, env, sessionID, eventID, ticketIDs[0], doomed.ID, map[string]any{"checked": true})

	if page := listOutstanding(t, env, sessionID, eventID); page.OutstandingCount != 3 {
		t.Fatalf("outstanding = %d before retiring, want 3", page.OutstandingCount)
	}

	resp, body := env.deleteJSON(t, questionPath(eventID, ticketTypeID, doomed.ID), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retire question status=%d error=%+v", resp.StatusCode, body.Error)
	}

	page := listOutstanding(t, env, sessionID, eventID)
	if page.OutstandingCount != 2 {
		t.Fatalf("outstanding = %d after retiring, want 2 — a retired question owes nothing", page.OutstandingCount)
	}
	for _, ticketID := range ticketIDs {
		labels := labelsOwedBy(page, ticketID)
		if len(labels) != 1 || labels[0] != "T-shirt size" {
			t.Fatalf("ticket %s owes %v, want only the live question", ticketID, labels)
		}
	}

	// The debt could not have been discharged even by somebody who wanted to,
	// which is the whole reason it is not a debt. This is the refusal that makes
	// the ruling necessary rather than merely tidy.
	resp, body = env.put(t, answerPath(eventID, ticketIDs[1], doomed.ID),
		map[string]any{"checked": true}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409 answering a retired question; error=%+v", resp.StatusCode, body.Error)
	}

	// And what was already answered still reads. "Retired, never deleted" is
	// untouched by any of this.
	resp, body = env.get(t, ticketPath(eventID, ticketIDs[0]), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get ticket status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if answer := answerFor(t, decodeTicketAnswers(t, body.Data), doomed.ID); answer == nil || answer.Checked == nil || !*answer.Checked {
		t.Fatalf("answer=%+v — retiring the question erased what a Ticket had said", answer)
	}
	_ = kept
}

// Tickets from `in_person` and `import` sales stand beside the `online` ones and
// start out owing EVERYTHING, because nobody ever put the questions to those
// buyers — there is no checkout form on a door sale or a spreadsheet import.
// That is the honest state of the debt, not a defect, and the surface must not
// hide it.
//
// The three sales are minted through the Sale Import — the shortest route to a
// recorded Ticket Sale — and then set to the channel each is standing for. The
// channel is one column on ticket_sales and the property under test is that the
// derivation does not FILTER on it, so moving that one column is exactly the
// dimension being isolated; driving three whole sale-creation paths would test
// those paths instead.
func TestOutstandingAnswersSpanEverySalesChannel(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Channel Fest", "channel-fest")
	ticketTypeID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2000, 50)

	commitBatch(t, env, sessionID, eventID, "batch-channels", []map[string]any{
		{"customer_email": "online@example.com", "customer_first_name": "On", "customer_last_name": "Line",
			"ticket_type_id": ticketTypeID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "door@example.com", "customer_first_name": "At", "customer_last_name": "Door",
			"ticket_type_id": ticketTypeID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
		{"customer_email": "imported@example.com", "customer_first_name": "Im", "customer_last_name": "Ported",
			"ticket_type_id": ticketTypeID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-03T10:00:00Z"},
	})

	for email, channel := range map[string]string{
		"online@example.com": "online",
		"door@example.com":   "in_person",
	} {
		if _, err := env.db.Exec(
			`UPDATE ticket_sales SET channel = $1 WHERE event_id = $2 AND customer_email = $3`,
			channel, eventID, email,
		); err != nil {
			t.Fatalf("set channel %s: %v", channel, err)
		}
	}

	createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})

	page := listOutstanding(t, env, sessionID, eventID)
	if page.Pagination.Total != 3 {
		t.Fatalf("tickets owing = %d, want all three channels", page.Pagination.Total)
	}

	seen := make(map[string]string, 3)
	for _, row := range page.Data {
		seen[row.Channel] = row.CustomerEmail
		if len(row.Outstanding) != 1 {
			t.Fatalf("%s ticket owes %d questions, want the one required question", row.Channel, len(row.Outstanding))
		}
	}
	for _, channel := range []string{"online", "in_person", "import"} {
		if seen[channel] == "" {
			t.Fatalf("no %s Ticket on the list — the surface is hiding a Sales Channel", channel)
		}
	}

	// The channel travels on the row, because it is what explains it: a door
	// sale owing everything is a buyer who was never asked, and a surface that
	// could not say so would read as lost data.
	if seen["in_person"] != "door@example.com" {
		t.Fatalf("in_person row = %q, want the door sale's buyer", seen["in_person"])
	}

	// Oldest sale first: the buyer who has been silent longest is the one whose
	// shirt is least likely to arrive, so they lead a chase list.
	if page.Data[0].CustomerEmail != "online@example.com" {
		t.Fatalf("first row = %q, want the oldest sale first", page.Data[0].CustomerEmail)
	}
}

// Tickets of a REVERSED Ticket Sale never appear. Its tickets have ceased to
// exist and its money has gone back, so there is nobody left to chase — and a
// whole sale leaves the list at once, without anything having to sweep it.
func TestReversedTicketSalesNeverOweOutstandingAnswers(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, ticketSaleID, batchID, _ := answeredFixture(t, env)

	createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})

	if page := listOutstanding(t, env, sessionID, eventID); page.Pagination.Total != 2 {
		t.Fatalf("tickets owing = %d before the Reversal, want 2", page.Pagination.Total)
	}

	undoBatch(t, env, sessionID, eventID, batchID)

	page := listOutstanding(t, env, sessionID, eventID)
	if page.Pagination.Total != 0 || page.OutstandingCount != 0 {
		t.Fatalf("tickets=%d outstanding=%d after the Reversal, want nothing owed",
			page.Pagination.Total, page.OutstandingCount)
	}
	if page.Pagination.TotalPages != 0 {
		t.Fatalf("total_pages = %d over an empty list, want 0", page.Pagination.TotalPages)
	}

	// The Tickets themselves are NOT gone, and neither is anything they said. A
	// Sale Reversal voids a sale; it does not unmint Tickets or erase Answers.
	// Only the debt goes with it.
	resp, body := env.get(t, ticketSaleTicketsPath(eventID, ticketSaleID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list sale tickets status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if tickets := decodeTicketAnswersList(t, body.Data); len(tickets) != 2 {
		t.Fatalf("tickets = %d — leaving the Outstanding list unminted Tickets", len(tickets))
	}
}

// The list is something staff can ACT on: every row carries the Ticket Sale and
// the buyer's own reference, which is what the Answers dialog is keyed on and
// names itself after. A row without them would be a complaint nobody could
// answer.
func TestOutstandingAnswersCarryTheJumpToTheTicket(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, ticketSaleID, _, ticketIDs := answeredFixture(t, env)

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})

	page := listOutstanding(t, env, sessionID, eventID)
	row := page.Data[0]
	if row.TicketSaleID != ticketSaleID || row.ConfirmationRef == "" {
		t.Fatalf("row sale=%q ref=%q, want the Ticket Sale that opens it", row.TicketSaleID, row.ConfirmationRef)
	}
	if row.Ordinal < 1 {
		t.Fatalf("ordinal = %d, want the 1..quantity number that tells two Tickets of one line apart", row.Ordinal)
	}
	if row.TicketTypeName != "GA" || row.CustomerFirstName == "" || row.CustomerEmail == "" {
		t.Fatalf("row = %+v, want the buyer and the Ticket Type a chase needs", row)
	}
	if row.Outstanding[0].QuestionID != question.ID || row.Outstanding[0].Kind != "short_text" {
		t.Fatalf("owed question = %+v, want the required question named with its kind", row.Outstanding[0])
	}

	// Following the jump discharges the debt, which is the round trip the
	// surface exists to make: read the list, open the Ticket, answer it, and the
	// row is gone.
	putAnswer(t, env, sessionID, eventID, row.TicketID, question.ID, map[string]any{"text": "L"})
	if !owesNothing(t, listOutstanding(t, env, sessionID, eventID), row.TicketID) {
		t.Fatal("the Ticket is still owing after being answered through the jump")
	}
	_ = ticketIDs
}

// Paging is the Sales list's, because this is the same kind of screen read by
// the same people: 50 by default, clamped to 100, and a page past the last still
// reports the true total rather than appearing to have emptied.
func TestOutstandingAnswersArePaged(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, _ := answeredFixture(t, env)

	createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})

	resp, body := env.get(t, outstandingPath(eventID)+"?page=1&page_size=1", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d error=%+v", resp.StatusCode, body.Error)
	}
	page := decodeOutstanding(t, body.Data)
	if len(page.Data) != 1 || page.Pagination.Total != 2 || page.Pagination.TotalPages != 2 {
		t.Fatalf("page = %d rows, total=%d pages=%d; want 1 of 2 across 2 pages",
			len(page.Data), page.Pagination.Total, page.Pagination.TotalPages)
	}

	// A page past the last: empty rows, TRUE total. The count is what the screen
	// says out loud, and it must not depend on which page is being looked at.
	resp, body = env.get(t, outstandingPath(eventID)+"?page=9&page_size=1", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d error=%+v", resp.StatusCode, body.Error)
	}
	page = decodeOutstanding(t, body.Data)
	if len(page.Data) != 0 || page.Pagination.Total != 2 {
		t.Fatalf("page 9 = %d rows total=%d, want none with the true total", len(page.Data), page.Pagination.Total)
	}

	// A nonsense page size falls back to the default rather than refusing: there
	// is nothing a caller could do about the refusal except send a sane number.
	resp, body = env.get(t, outstandingPath(eventID)+"?page=nonsense&page_size=-4", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d error=%+v", resp.StatusCode, body.Error)
	}
	page = decodeOutstanding(t, body.Data)
	if page.Pagination.Page != 1 || page.Pagination.PageSize != 50 {
		t.Fatalf("page=%d size=%d, want the defaults", page.Pagination.Page, page.Pagination.PageSize)
	}
}

// A Ticket Type's questions reach only ITS OWN Tickets. Ticket Questions belong
// to the Ticket Type — never to the Event and never to the Organization — so a
// Ticket of the General type owes nothing the VIP type asks.
func TestOutstandingAnswersFollowTheTicketType(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Two Types", "two-types")
	generalID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "General", 2000, 50)
	vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 9000, 10)

	commitBatch(t, env, sessionID, eventID, "batch-types", []map[string]any{
		{"customer_email": "gen@example.com", "customer_first_name": "Gen", "customer_last_name": "Eral",
			"ticket_type_id": generalID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "vip@example.com", "customer_first_name": "Vi", "customer_last_name": "Pea",
			"ticket_type_id": vipID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
	})

	createTicketQuestion(t, env, sessionID, eventID, vipID, map[string]any{
		"label": "Dietary requirements", "kind": "short_text", "required": true,
	})

	// The ROSTER holds both Tickets — the list is the Event's Tickets, not its
	// debtors — and only the VIP one owes anything.
	page := listOutstanding(t, env, sessionID, eventID)
	if page.Pagination.Total != 2 || page.OutstandingCount != 1 {
		t.Fatalf("tickets=%d outstanding=%d, want both Tickets on the roster and one debt",
			page.Pagination.Total, page.OutstandingCount)
	}
	for _, row := range page.Data {
		switch row.TicketTypeName {
		case "VIP":
			if len(row.Outstanding) != 1 || row.Outstanding[0].Label != "Dietary requirements" {
				t.Fatalf("the VIP Ticket owes %v, want its own one question", labelsOwedBy(page, row.TicketID))
			}
		case "General":
			if len(row.Outstanding) != 0 {
				t.Fatalf("the General Ticket owes %v — a General Ticket owes nothing the VIP type asks",
					labelsOwedBy(page, row.TicketID))
			}
		}
	}

	// The Outstanding Answers filter is where "only the owing" lives now.
	resp, body := env.get(t, outstandingPath(eventID)+"?outstanding=true", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("filtered status=%d error=%+v", resp.StatusCode, body.Error)
	}
	filtered := decodeOutstanding(t, body.Data)
	if filtered.Pagination.Total != 1 || filtered.Data[0].TicketTypeName != "VIP" {
		t.Fatalf("filtered total=%d type=%q, want the VIP Ticket alone",
			filtered.Pagination.Total, filtered.Data[0].TicketTypeName)
	}
}

// The surface is scoped to the Organization and gated exactly as its siblings
// are: another Organization cannot see it, and `event_staff` is refused every
// catalog verb on this platform today.
func TestOutstandingAnswersAreScopedAndGated(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, _ := answeredFixture(t, env)
	createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})

	otherSession := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSession, "Other Org", "other-org")
	resp, body := env.get(t, outstandingPath(eventID), authHeader(otherSession))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 across Organizations; error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.post(t, "/api/v1/staff/members", map[string]string{
		"email": "doorstaff@example.com", "role": "event_staff",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}
	staffSessionID := verifyOTP(t, env, "doorstaff@example.com")

	resp, body = env.get(t, outstandingPath(eventID), authHeader(staffSessionID))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d, want 403 for event_staff; error=%+v", resp.StatusCode, body.Error)
	}
}

// THE HOLDER LIST'S PEOPLE (#329, parent #322, ADR 0047; roster per #333).
//
// The same read, widened rather than duplicated. An Organization asked "who is
// coming to my Event" could previously answer only with the buyer's name
// repeated once per Ticket; these tests are what makes it answer with people.
//
// THE ROWS ARE THE EVENT'S TICKETS. Since #333 the list is the roster — every
// Ticket of every live sale — and assignment neither adds a row nor takes one
// off; what a Ticket owes is a column on it. That is the whole reason there is
// no second staff endpoint.

// guestEntry is what the Organization is shown about the person on one row.
type guestEntry struct {
	state, firstName, lastName, email string
	// neverAccepted is the purge's presentation-level marker (#334): somebody
	// was named and never claimed the Ticket, and the address is gone.
	neverAccepted bool
}

// guestRow picks one Ticket's row off the Organization's list, failing if the
// Ticket is not on it — so a test that means "Carla's row says X" cannot quietly
// pass because there was no row at all.
func guestRow(t *testing.T, page outstandingAnswers, ticketID string) guestEntry {
	t.Helper()
	for _, row := range page.Data {
		if row.TicketID == ticketID {
			return guestEntry{
				row.AssignmentState, row.HolderFirstName, row.HolderLastName, row.HolderEmail,
				row.NeverAccepted,
			}
		}
	}
	t.Fatalf("Ticket %s is not on the Organization's list", ticketID)
	return guestEntry{}
}

// THE CENTRE OF THIS TICKET: three Tickets of one Event in the three states, on
// one list, and what the Organization is shown about each.
//
//   - `accepted` — a Holder proved the address and gave their own name. The
//     Organization sees BOTH, name and address, which is the disclosure ADR 0047
//     records with its cost stated: an Organizer needs a way to reach the people
//     attending its Event, and a name it cannot write to leaves it routing
//     through buyers by hand.
//   - `assigned` — an address was typed and nobody clicked it. The Organization
//     is told the state and NOT the person. That address has no consent moment
//     behind it at all; ADR 0047 rejects disclosing it outright and calls it the
//     line the whole design is drawn around.
//   - `unassigned` — nobody was named. Neither.
//
// AND THE BUYER STAYS ON EVERY ROW. A Holder is the named person a Ticket was
// handed to and never its owner: the Sale, the money and the Reversal Window are
// still the buyer's, and a Holder List that replaced them would be describing a
// transfer that never happened.
func TestTheGuestListNamesAcceptedHoldersAndNobodyElse(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	accepted, assigned := f.anaTicketIDs[0], f.anaTicketIDs[1]

	// Carla is handed a Ticket and accepts it, which mints her Customer record,
	// and then gives the name the Organization will read.
	assignTicketOK(t, env, f.ana, f.anaSaleID, accepted, "carla@example.com")
	token := assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com"))
	acceptAssignmentOK(t, env, token)
	resp, body, _ := answerLinkRequest(t, env, http.MethodPut, assignmentLinkNamePath, map[string]any{
		"token": token, "first_name": "Carla", "last_name": "Ruiz",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holder name status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// Diego is handed the other one and never clicks. A minute later, so the two
	// assignments are separable facts on this suite's fixed clock.
	holdClocksAt(fixedClock.Add(time.Minute))
	assignTicketOK(t, env, f.ana, f.anaSaleID, assigned, "diego@example.com")

	page := listOutstanding(t, env, f.staffSession, f.eventID)
	if page.Pagination.Total != 3 {
		t.Fatalf("tickets on the list = %d, want the Event's three — assignment adds and removes no rows",
			page.Pagination.Total)
	}

	if got := guestRow(t, page, accepted); got.state != "accepted" ||
		got.firstName != "Carla" || got.lastName != "Ruiz" || got.email != "carla@example.com" {
		t.Errorf("the accepted row reads %+v.\n"+
			"An Organization sees an accepted Holder's name AND email address (ADR 0047), "+
			"and the two name parts stay apart (ADR 0005).", got)
	}

	// THE ASSIGNED ROW IS THE ONE TO GET RIGHT. It says `assigned` and says
	// nothing else: no name, because nobody has given one, and no address,
	// because the person at it has agreed to nothing and may not know a ticket
	// was bought for them.
	got := guestRow(t, page, assigned)
	if got.state != "assigned" {
		t.Errorf("the assigned row reads state=%q, want `assigned` — without the state a Ticket "+
			"nobody accepted is indistinguishable from one nobody was named for", got.state)
	}
	if got.firstName != "" || got.lastName != "" {
		t.Errorf("the assigned row names %q %q; a name arrives only with acceptance", got.firstName, got.lastName)
	}
	if got.email != "" {
		t.Errorf("the assigned row discloses %q.\n"+
			"ADR 0047: an address that was typed by a buyer and never accepted is NEVER shown to the "+
			"Organization. That is the line the whole design is drawn around.", got.email)
	}

	// And Bruno's Ticket, which nobody was ever named for.
	if got := guestRow(t, page, f.brunoTicketIDs[0]); got.state != "unassigned" ||
		got.firstName != "" || got.email != "" {
		t.Errorf("the unassigned row reads %+v, want the state alone", got)
	}

	// The buyer is still on every row, Holder or no Holder.
	for _, row := range page.Data {
		if row.CustomerEmail == "" || row.CustomerFirstName == "" {
			t.Errorf("row %s lost its buyer: %+v — assignment is never transfer", row.TicketID, row)
		}
	}
	// Nothing else about a stranger's address leaked onto the list either.
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("re-encode page: %v", err)
	}
	if strings.Contains(string(raw), "diego@example.com") {
		t.Error("diego@example.com appears somewhere on the Organization's list; nothing is disclosed before acceptance")
	}
}

// A PURGED TICKET READS `assigned, never accepted` ON THE HOLDER LIST — AND
// THERE IS STILL NO FOURTH STATE (#334, ruling of 2026-08-22).
//
// An address nobody accepted is taken when the Event starts (#331, migration
// 081), which takes `assigned_at` with it and leaves only the platform's own
// marker that this Ticket once carried one. After the Event starts every
// unaccepted assignment would otherwise read `unassigned`, and the
// morning-after sheet could not distinguish "nobody was named" from "named and
// never claimed" — opposite facts to the person reading it. So the Holder List
// derives `assigned` with `never_accepted` beside it AT READ TIME from the
// marker, and catalog.AssignmentState keeps its three values: the buyer's page
// and the export still read such a Ticket as `unassigned`, which #331's
// rejection of a fourth state protects. Nothing personal is disclosed — the
// address is gone by definition.
func TestAPurgedTicketReadsAssignedNeverAcceptedOnTheHolderList(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[0]

	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "diego@example.com")
	got := guestRow(t, listOutstanding(t, env, f.staffSession, f.eventID), ticketID)
	if got.state != "assigned" || got.neverAccepted {
		t.Fatalf("row before the purge = %+v, want plain `assigned` — the marker is the purge's alone", got)
	}

	// The doors open — the fixture's Event starts 30 days out — and the purge
	// takes the address it was never allowed to keep.
	holdClocksAt(fixedClock.Add(30*24*time.Hour + time.Hour))
	if result := purgeHolderAddresses(t, env); result.AddressesPurged != 1 {
		t.Fatalf("purge = %+v, want the one unaccepted address taken", result)
	}

	// The Event has started, and this list still reports what happened —
	// "somebody was named and never claimed it" is what a reader after the fact
	// came to find out, and `unassigned` would rewrite it as "nobody was named".
	got = guestRow(t, listOutstanding(t, env, f.staffSession, f.eventID), ticketID)
	if got.state != "assigned" || !got.neverAccepted {
		t.Errorf("a purged Ticket reads %+v to the Organization, want `assigned` with never_accepted — "+
			"the morning-after sheet must not rewrite what happened (#334)", got)
	}
	if got.email != "" || got.firstName != "" || got.lastName != "" {
		t.Errorf("a purged Ticket still discloses %+v — the address is gone by definition", got)
	}

	// And the Ticket nobody was ever named for stays a plain `unassigned`
	// beside it, which is the distinction the marker exists to draw.
	if other := guestRow(t, listOutstanding(t, env, f.staffSession, f.eventID), f.anaTicketIDs[1]); other.state != "unassigned" || other.neverAccepted {
		t.Errorf("the never-assigned Ticket reads %+v, want plain `unassigned`", other)
	}
}

// EVENT STAFF CAN STILL CORRECT ANY ANSWER ON ANY TICKET OF THEIR EVENT,
// INCLUDING AN ACCEPTED ONE.
//
// Acceptance closes the ANSWER LINK — a stranger still holding a forwarded URL
// must not overwrite the Holder's own reply (ADR 0046) — and it closes nothing
// else. An Organization that could not fix the size of a Holder who has stopped
// replying would find the Holder List turning every unreachable person into a
// dead end, which is the opposite of what this surface is for.
func TestStaffStillAnswerForAnAcceptedHolder(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[0]

	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))

	page := listOutstanding(t, env, f.staffSession, f.eventID)
	if got := guestRow(t, page, ticketID); got.state != "accepted" {
		t.Fatalf("state = %q, want `accepted`", got.state)
	}
	if labels := labelsOwedBy(page, ticketID); len(labels) != 1 {
		t.Fatalf("an accepted Ticket owes %v; accepting answers nothing by itself", labels)
	}

	// The row is acted on exactly as any other is: through the jump it carries.
	putAnswer(t, env, f.staffSession, f.eventID, ticketID, f.sizeQuestion.ID, map[string]any{"text": "S"})
	if !owesNothing(t, listOutstanding(t, env, f.staffSession, f.eventID), ticketID) {
		t.Error("the Ticket still owes after Event Staff answered it — an unreachable Holder is a dead end")
	}
}

// WITH TICKET_ASSIGNMENT_ENABLED CLOSED, THIS SURFACE IS BYTE-IDENTICAL TO WHAT A
// BUILD WITHOUT THE FEATURE SENDS.
//
// The two flags are separate on purpose: killing assignment must not take Ticket
// Questions down with it, so the Outstanding Answers list has to keep working
// with the Holder List absent — not empty, ABSENT. Asserted on the RAW BODY,
// because a decoded struct reports an empty string for a field sent as `""` and
// for one never sent at all, and those are the two cases this distinguishes
// (ADR 0045).
func TestTheGuestListIsAbsentWhileTheAssignmentFlagIsClosed(t *testing.T) {
	env := setupTest(t)
	// Questions open, assignment left CLOSED — deliberately not calling
	// enableTicketAssignment, which is the shipped state.
	f := newBuyerAnswersFixture(t, env)

	resp, body := env.get(t, outstandingPath(f.eventID), authHeader(f.staffSession))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d error=%+v — closing assignment must not close Ticket Questions",
			resp.StatusCode, body.Error)
	}
	page := decodeOutstanding(t, body.Data)
	if page.Pagination.Total != 3 {
		t.Fatalf("tickets = %d, want the Event's three still listed", page.Pagination.Total)
	}
	for _, forbidden := range []string{"assignment_state", "never_accepted", "holder_first_name", "holder_last_name", "holder_email"} {
		if strings.Contains(string(body.Data), forbidden) {
			t.Errorf("the response carries %q while TICKET_ASSIGNMENT_ENABLED is closed.\n"+
				"A closed build must send the bytes a build without the feature sends (ADR 0045).", forbidden)
		}
	}
}

// THE HOLDER LIST REACHES EVERY TICKET, AND A FULLY ANSWERED ONE STAYS ON IT
// WITH ITS HOLDER'S NAME.
//
// This test is the INVERSION TestTheGuestListOnlyReachesTicketsThatOweAnAnswer
// predicted (#333, ruling of 2026-08-22): that test recorded, without blessing
// it, that a Ticket dropped off the Holder List the moment its Answers were all
// in — Carla answered her one question and left the list with her name. The
// ruling made the list the roster: answering discharges the DEBT and touches
// nothing else, because who is coming and what they still owe are different
// columns of one list, and the Outstanding Answers filter is where the old
// behaviour lives.
func TestTheHolderListKeepsAFullyAnsweredTicket(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[0]

	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))
	if got := guestRow(t, listOutstanding(t, env, f.staffSession, f.eventID), ticketID); got.email == "" {
		t.Fatal("the accepted Holder is not on the list to begin with")
	}

	// Carla answers the one required question — and STAYS, owing nothing.
	putAnswer(t, env, f.staffSession, f.eventID, ticketID, f.sizeQuestion.ID, map[string]any{"text": "S"})
	page := listOutstanding(t, env, f.staffSession, f.eventID)
	if page.Pagination.Total != 3 {
		t.Fatalf("tickets = %d after answering, want the Event's three — answering discharges a debt, not a person",
			page.Pagination.Total)
	}
	if !owesNothing(t, page, ticketID) {
		t.Fatalf("the answered Ticket still owes %v", labelsOwedBy(page, ticketID))
	}
	if got := guestRow(t, page, ticketID); got.state != "accepted" || got.email != "carla@example.com" {
		t.Errorf("the answered Ticket reads %+v — a fully answered Holder must not vanish from \"who is coming\"", got)
	}

	// The Outstanding Answers filter is the view that narrows: Carla's Ticket
	// leaves IT, and only it.
	resp, body := env.get(t, outstandingPath(f.eventID)+"?outstanding=true", authHeader(f.staffSession))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("filtered status=%d error=%+v", resp.StatusCode, body.Error)
	}
	filtered := decodeOutstanding(t, body.Data)
	if filtered.Pagination.Total != 2 {
		t.Fatalf("filtered total = %d, want the two Tickets still owing", filtered.Pagination.Total)
	}
	for _, row := range filtered.Data {
		if row.TicketID == ticketID {
			t.Error("the fully answered Ticket is still on the Outstanding Answers filter")
		}
	}
}
