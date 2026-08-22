package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// The Outstanding Answers surface (#313): which of an Event's Tickets still owe
// required Answers, and which questions they owe.
//
// THESE TESTS ARE WHERE THE DEFINITION IS HELD TOGETHER. The rule lives twice —
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
		Outstanding       []struct {
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

func owesNothing(page outstandingAnswers, ticketID string) bool {
	for _, row := range page.Data {
		if row.TicketID == ticketID {
			return false
		}
	}
	return true
}

// Nothing about the Outstanding Answers surface is reachable while the flag is
// off — the same 404 a build without the feature gives, on the same terms as
// every other Answer route (ADR 0045).
func TestOutstandingAnswersAreInvisibleWhileTheFlagIsOff(t *testing.T) {
	env := setupTest(t)
	// Deliberately NOT calling enableTicketQuestions: this is the shipped state.
	sessionID, eventID, _, _, _, _ := answeredFixture(t, env)

	resp, body := env.get(t, outstandingPath(eventID), authHeader(sessionID))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 while the feature is dark; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "TICKET_QUESTIONS_UNAVAILABLE" {
		t.Fatalf("error=%+v, want TICKET_QUESTIONS_UNAVAILABLE", body.Error)
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
	if !owesNothing(page, ticketIDs[0]) {
		t.Fatalf("a fully answered Ticket is still on the list: %v", labelsOwedBy(page, ticketIDs[0]))
	}
	if page.Pagination.Total != 1 || page.OutstandingCount != 2 {
		t.Fatalf("tickets=%d outstanding=%d, want the second Ticket's two debts alone",
			page.Pagination.Total, page.OutstandingCount)
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
	if !owesNothing(listOutstanding(t, env, sessionID, eventID), row.TicketID) {
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

	page := listOutstanding(t, env, sessionID, eventID)
	if page.Pagination.Total != 1 || page.OutstandingCount != 1 {
		t.Fatalf("tickets=%d outstanding=%d, want only the VIP Ticket owing",
			page.Pagination.Total, page.OutstandingCount)
	}
	if page.Data[0].TicketTypeName != "VIP" {
		t.Fatalf("owing Ticket Type = %q, want VIP — a General Ticket owes nothing the VIP type asks",
			page.Data[0].TicketTypeName)
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
