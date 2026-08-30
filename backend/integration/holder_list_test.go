package integration

import (
	"encoding/json"
	"net/http"
	"net/url"
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

// holderListPath is the Holder List's own address (#519, ADR 0065): the list is
// named after itself and not after `outstanding`, which is one filter of it.
func holderListPath(eventID string) string {
	return "/api/v1/staff/events/" + eventID + "/holder-list"
}

// legacyHolderListPath is the address the list was built at (#313) and kept
// through #333. Aliased to the same handler for one release against deploy skew
// and deleted by #531 — at which point this helper and the test that uses it go
// with it. Nothing else in this file may reach for it: every other test states
// what the Holder List DOES, and that is the new path's subject.
func legacyHolderListPath(eventID string) string {
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
	resp, body := env.get(t, holderListPath(eventID), authHeader(sessionID))
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

	resp, body := env.get(t, holderListPath(eventID), authHeader(sessionID))
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

	resp, body := env.get(t, holderListPath(eventID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d error=%+v — an Organization that asks nothing still has a Holder List",
			resp.StatusCode, body.Error)
	}
	page := decodeOutstanding(t, body.Data)
	if page.Pagination.Total != 2 || len(page.Data) != 2 {
		t.Fatalf("tickets = %d (total %d), want the Event's two — the roster is every Ticket",
			len(page.Data), page.Pagination.Total)
	}
	// The fixture's Sale is an import recorded with assignment already open, so
	// its buyer holds Ticket 1 by presumption (ADR 0055) and Ticket 2 is nobody's
	// yet. Both are on the roster, which is what this test is about.
	if got := guestRow(t, page, ticketIDs[0]); got.state != "accepted" || got.email != "ana@example.com" {
		t.Errorf("Ticket 1 reads state=%q holder=%q, want the imported buyer holding their own",
			got.state, got.email)
	}
	if got := guestRow(t, page, ticketIDs[1]); got.state != "unassigned" {
		t.Errorf("Ticket 2 reads state=%q, want `unassigned` with the roster visible", got.state)
	}
	if strings.Contains(string(body.Data), "outstanding") {
		t.Error("the response speaks of `outstanding` while TICKET_QUESTIONS_ENABLED is closed.\n" +
			"A build with questions dark must send the bytes a build without the feature sends (ADR 0045).")
	}

	// The Outstanding Answers filter belongs to the questions feature and is
	// IGNORED while it is dark, rather than becoming a side channel that
	// filters by a debt the platform says does not exist.
	resp, body = env.get(t, holderListPath(eventID)+"?outstanding=true", authHeader(sessionID))
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
	resp2, body2 := env.get(t, holderListPath(eventID)+"?outstanding=true", authHeader(sessionID))
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

	resp, body := env.get(t, holderListPath(eventID)+"?page=1&page_size=1", authHeader(sessionID))
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
	resp, body = env.get(t, holderListPath(eventID)+"?page=9&page_size=1", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d error=%+v", resp.StatusCode, body.Error)
	}
	page = decodeOutstanding(t, body.Data)
	if len(page.Data) != 0 || page.Pagination.Total != 2 {
		t.Fatalf("page 9 = %d rows total=%d, want none with the true total", len(page.Data), page.Pagination.Total)
	}

	// A nonsense page size falls back to the default rather than refusing: there
	// is nothing a caller could do about the refusal except send a sane number.
	resp, body = env.get(t, holderListPath(eventID)+"?page=nonsense&page_size=-4", authHeader(sessionID))
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
	resp, body := env.get(t, holderListPath(eventID)+"?outstanding=true", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("filtered status=%d error=%+v", resp.StatusCode, body.Error)
	}
	filtered := decodeOutstanding(t, body.Data)
	if filtered.Pagination.Total != 1 || filtered.Data[0].TicketTypeName != "VIP" {
		t.Fatalf("filtered total=%d type=%q, want the VIP Ticket alone",
			filtered.Pagination.Total, filtered.Data[0].TicketTypeName)
	}
}

// The surface is scoped to the Organization: another Organization cannot see
// it at all, and asking for one it does not own is a 404 rather than a 403 —
// the Event is not hidden behind a refusal, it does not exist for that reader.
//
// The `event_staff` refusal is asserted here too, and again beside the Event
// Owner in TestTheHolderListReadIsForTheOrgAdminAndTheEventOwner below. That
// repetition is deliberate: this test's subject is the Organization boundary
// and that one's is the role gate, and the door staff refusal is the clause
// #521 was most likely to take away by accident.
func TestOutstandingAnswersAreScopedAndGated(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, _ := answeredFixture(t, env)
	createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})

	otherSession := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSession, "Other Org", "other-org")
	resp, body := env.get(t, holderListPath(eventID), authHeader(otherSession))
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

	resp, body = env.get(t, holderListPath(eventID), authHeader(staffSessionID))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d, want 403 for event_staff; error=%+v", resp.StatusCode, body.Error)
	}
}

// THE READ IS FOR THE ORG ADMIN AND THE EVENT OWNER, AND FOR NOBODY ELSE
// (#521, ADR 0065).
//
// All three roles in one test, because the decision is the SHAPE of the gate
// and not any one of its three answers: an Org Admin still reads it, an Event
// Owner now reads it, and Event Staff are still refused. Split across three
// tests, deleting the middle one would look like removing a feature's test
// rather than reverting a permissions decision.
//
// WHY IT WIDENED. The Sales Export next door already emits this Event's
// assignment states and its accepted Holders' names and addresses to an Event
// Owner, so the `orgAdmin` gate that stood here held nothing in — it was
// inherited from the Ticket Question routes the list grew out of, not chosen
// for roster data. This is a deliberate widening of access to personal data.
//
// WHY IT STOPS THERE. Event Staff are Members of the Event and read the Sales
// list, so their refusal is not a side effect of the auth stack — it is the
// line ADR 0065 draws, and this is what holds it. If a future change makes this
// route `member`, this test fails, which is the point.
//
// The Event Owner's read is asserted on the BODY and not the status alone: a
// gate that admits somebody to an empty list has widened the door and not the
// disclosure, and it is the roster this ticket promised them.
func TestTheHolderListReadIsForTheOrgAdminAndTheEventOwner(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	adminSession, eventID, ticketTypeID, _, _, _ := answeredFixture(t, env)
	createTicketQuestion(t, env, adminSession, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})

	// addMember mints a colleague of this Organization at the given role and
	// signs them in, exactly as the Sales Export's access test does.
	addMember := func(email, role string) string {
		t.Helper()
		resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
			"email": email,
			"role":  role,
		}, authHeader(adminSession))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("add %s status=%d error=%+v", role, resp.StatusCode, body.Error)
		}
		return verifyOTP(t, env, email)
	}

	// THE ORG ADMIN, UNCHANGED. Read first, so the rows the Event Owner is
	// compared against are a fact of this Event and not of this test.
	adminPage := listOutstanding(t, env, adminSession, eventID)
	if adminPage.Pagination.Total == 0 {
		t.Fatal("the Org Admin's Holder List is empty; this test cannot tell a widened gate from an empty roster")
	}

	// THE EVENT OWNER, NEWLY ADMITTED.
	ownerSession := addMember("owner@example.com", "event_owner")
	resp, body := env.get(t, holderListPath(eventID), authHeader(ownerSession))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("event owner status=%d, want 200; error=%+v", resp.StatusCode, body.Error)
	}
	ownerPage := decodeOutstanding(t, body.Data)
	if ownerPage.Pagination.Total != adminPage.Pagination.Total {
		t.Errorf("event owner sees %d Tickets where the Org Admin sees %d — the same roster, or the gate widened onto a different list",
			ownerPage.Pagination.Total, adminPage.Pagination.Total)
	}
	if ownerPage.OutstandingCount != adminPage.OutstandingCount {
		t.Errorf("event owner is told of %d debts where the Org Admin is told of %d",
			ownerPage.OutstandingCount, adminPage.OutstandingCount)
	}

	// EVENT STAFF, STILL REFUSED — and refused a 403 rather than a 404, because
	// this Event is theirs to see; the roster on it is not.
	doorSession := addMember("doorstaff@example.com", "event_staff")
	resp, body = env.get(t, holderListPath(eventID), authHeader(doorSession))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("event staff status=%d, want 403; error=%+v", resp.StatusCode, body.Error)
	}
	// And the refusal is of the roster alone: the Sales list they work from is
	// untouched by it, the same pair of facts the Sales Export's access test
	// asserts one tab over.
	if resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales", authHeader(doorSession)); resp.StatusCode != http.StatusOK {
		t.Fatalf("event staff sales list status=%d error=%+v", resp.StatusCode, body.Error)
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
	resp, body, _ := publicLinkRequest(t, env, http.MethodPut, assignmentLinkNamePath, map[string]any{
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

	resp, body := env.get(t, holderListPath(f.eventID), authHeader(f.staffSession))
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
	resp, body := env.get(t, holderListPath(f.eventID)+"?outstanding=true", authHeader(f.staffSession))
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

// THE OLD PATH ANSWERS IDENTICALLY, FOR ONE RELEASE (#519, ADR 0065).
//
// The Holder List moved to an address named after itself rather than after
// `outstanding`, which ADR 0065 turns into one filter of seven. The staff app
// and the API deploy separately, so a new API will serve an old frontend for as
// long as that gap lasts — and with CI and Deploy refused for billing, nothing
// in the pipeline would catch a bad ordering. So the old path is registered to
// the SAME handler value and this test is what says so.
//
// IT COMPARES THE WHOLE PAYLOAD and not a status code, because the failure this
// guards against is not a 404 — a 404 anybody would notice on the first click.
// It is somebody later giving the alias its own registration, its own gate or
// its own defaults, at which point the two addresses quietly disagree and only
// the deployment that happens to be skewed finds out. The envelope's request id
// is the one field allowed to differ; it differs on every request.
//
// THIS TEST IS DELETED WITH THE ALIAS (#531). It is not an assertion that the
// old path should exist — it is the width of the deploy window written down.
func TestTheOldOutstandingAnswersPathIsAnAliasOfTheHolderList(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)

	// A filter on the query string too: the alias forwards a request, it does
	// not re-read one, so whatever the new path honours the old path honours.
	const query = "?outstanding=true&page=1&page_size=100"

	resp, body := env.get(t, holderListPath(f.eventID)+query, authHeader(f.staffSession))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holder-list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	legacyResp, legacyBody := env.get(t, legacyHolderListPath(f.eventID)+query, authHeader(f.staffSession))
	if legacyResp.StatusCode != resp.StatusCode {
		t.Fatalf("the old path answers %d where the new one answers %d; error=%+v — the alias is gone or gated differently",
			legacyResp.StatusCode, resp.StatusCode, legacyBody.Error)
	}
	if string(legacyBody.Data) != string(body.Data) {
		t.Errorf("the old path answers a different body:\n old: %s\n new: %s", legacyBody.Data, body.Data)
	}

	// The gate is one gate. Event Staff are refused at the old address exactly
	// as they are at the new one, so the alias cannot become the way around an
	// Org Admin check somebody forgot to repeat — which is the whole reason it
	// shares a handler VALUE and not a handler function.
	addResp, addBody := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": "aliasdoor@example.com", "role": "event_staff",
	}, authHeader(f.staffSession))
	if addResp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", addResp.StatusCode, addBody.Error)
	}
	doorSession := verifyOTP(t, env, "aliasdoor@example.com")
	doorResp, doorBody := env.get(t, legacyHolderListPath(f.eventID), authHeader(doorSession))
	if doorResp.StatusCode != http.StatusForbidden {
		t.Errorf("Event Staff read the old path with status=%d, want 403; error=%+v", doorResp.StatusCode, doorBody.Error)
	}
}

// THE STRUCTURAL FILTERS (#523, ADR 0065): Ticket Type, Sales Channel and the
// sale's date. They narrow the roster, they compose with each other and with
// `outstanding`, and the pagination total follows them.
//
// THESE TESTS ARE THE ACCEPTANCE CRITERION and are deliberately written against
// REAL ROWS rather than against the query builder. Every one of these filters is
// a string reaching a comparison in SQL: a transposed argument, a copied EXISTS,
// an off-by-one on a day boundary and a count query that forgot a filter all
// compile, all run, and all differ from the truth only in which people are on
// the list. Nothing but a database can notice that.

// filterFixture stands up an Event with a roster worth filtering: two Ticket
// Types, three Sales Channels and four sale days, one Ticket each so a count of
// rows is a count of the thing being tested.
//
// The Event is put in America/New_York (UTC-4 in July) and the sale times are
// chosen around ITS midnights, so a bound read in UTC and a bound read in the
// Event's zone select different rows — see the date test, which is the only way
// that mistake ever gets caught.
type holderFilterFixture struct {
	sessionID string
	eventID   string
	gaID      string
	vipID     string
}

func newHolderFilterFixture(t *testing.T, env *testEnv) holderFilterFixture {
	t.Helper()
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Filter Fest", "filter-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2000, 100)
	vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 9000, 100)
	setEventTimezone(t, env, eventID, "America/New_York")

	commitBatch(t, env, sessionID, eventID, "filter-batch", []map[string]any{
		// 07-01 00:00 EDT — the inclusive lower boundary of the 1st.
		{"customer_email": "ga-online@example.com", "customer_first_name": "Ga", "customer_last_name": "Online",
			"ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T04:00:00Z"},
		// 07-01 23:59 EDT — the last moment of the 1st, which a bound read in UTC
		// would push into the 2nd.
		{"customer_email": "vip-door@example.com", "customer_first_name": "Vip", "customer_last_name": "Door",
			"ticket_type_id": vipID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T03:59:00Z"},
		// 07-02 00:00 EDT — the exclusive upper boundary of the 1st.
		{"customer_email": "ga-import@example.com", "customer_first_name": "Ga", "customer_last_name": "Import",
			"ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T04:00:00Z"},
		// 06-30 23:59 EDT — before the 1st, and a UTC reader would call it the 1st.
		{"customer_email": "vip-online@example.com", "customer_first_name": "Vip", "customer_last_name": "Online",
			"ticket_type_id": vipID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T03:59:00Z"},
	})

	// Every sale a Sale Import commits is on the `import` channel; two are moved
	// onto the others in SQL, which is the harness's existing way of staging a
	// channel there is no recording endpoint for (see moveSaleToTheDoor).
	moveHolderSaleToChannel(t, env, eventID, "ga-online@example.com", "online")
	moveHolderSaleToChannel(t, env, eventID, "vip-online@example.com", "online")
	moveHolderSaleToChannel(t, env, eventID, "vip-door@example.com", "in_person")

	return holderFilterFixture{sessionID: sessionID, eventID: eventID, gaID: gaID, vipID: vipID}
}

// moveHolderSaleToChannel stages one sale on a Sales Channel, by the buyer's
// email so the fixture reads as the sentence it is setting up.
func moveHolderSaleToChannel(t *testing.T, env *testEnv, eventID, email, channel string) {
	t.Helper()
	res, err := env.db.Exec(`
		UPDATE ticket_sales SET channel = $1 WHERE event_id = $2 AND customer_email = $3
	`, channel, eventID, email)
	if err != nil {
		t.Fatalf("move %s onto %s: %v", email, channel, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("moving %s onto %s affected %d rows, want 1", email, channel, n)
	}
}

// holderRoster reads the Holder List under a raw query string, failing on any
// refusal — every test below is about WHICH ROWS come back, never about who may
// ask.
func holderRoster(t *testing.T, env *testEnv, sessionID, eventID, query string) outstandingAnswers {
	t.Helper()
	resp, body := env.get(t, holderListPath(eventID)+query, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holder list %q status=%d error=%+v", query, resp.StatusCode, body.Error)
	}
	return decodeOutstanding(t, body.Data)
}

// holderBuyers names the buyers on the page, in the order the list returned
// them, so an assertion can say who is on the roster rather than how many.
func holderBuyers(page outstandingAnswers) []string {
	emails := make([]string, 0, len(page.Data))
	for _, row := range page.Data {
		emails = append(emails, row.CustomerEmail)
	}
	return emails
}

// assertRoster holds both halves of one view at once: WHO is on the page, and
// what the pagination says the view holds. They are asserted together on
// purpose — a total that counted the unfiltered roster under a filtered page is
// the exact bug that puts "1 of 12 pages" over four rows, and it is invisible to
// a test that checks only the rows.
func assertRoster(t *testing.T, page outstandingAnswers, want []string) {
	t.Helper()
	got := holderBuyers(page)
	if len(got) != len(want) {
		t.Fatalf("roster = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("roster = %v, want %v", got, want)
		}
	}
	if page.Pagination.Total != len(want) {
		t.Fatalf("pagination total = %d over %d rows — the count query and the page query disagree about the view",
			page.Pagination.Total, len(want))
	}
}

// THE TICKET TYPE FILTER: the VIP roster apart from general admission.
func TestTheHolderListFiltersByTicketType(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	f := newHolderFilterFixture(t, env)

	// The whole roster first, so the filters below are narrowings of something
	// known — oldest sale first, which is this list's order.
	assertRoster(t, holderRoster(t, env, f.sessionID, f.eventID, ""), []string{
		"vip-online@example.com", "ga-online@example.com", "vip-door@example.com", "ga-import@example.com",
	})

	assertRoster(t, holderRoster(t, env, f.sessionID, f.eventID, "?ticket_type_id="+f.vipID), []string{
		"vip-online@example.com", "vip-door@example.com",
	})
	assertRoster(t, holderRoster(t, env, f.sessionID, f.eventID, "?ticket_type_id="+f.gaID), []string{
		"ga-online@example.com", "ga-import@example.com",
	})
}

// A MIXED SALE'S OTHER TICKETS STAY OFF THE FILTERED ROSTER, and this is the
// test that would fail if somebody copied the Sales list's filter across.
//
// The Sales list matches its Ticket Type with `EXISTS (SELECT 1 FROM
// ticket_sale_lines ...)`, because a Ticket SALE can span several types and must
// appear once with its rollup intact. A TICKET cannot: it is minted on exactly
// one Ticket Sale Line and belongs to exactly one type. Under the EXISTS, asking
// for the VIP roster of a sale that bought two GA and one VIP would hand back
// all three Tickets — two people who are not VIPs, on a list an Organizer is
// about to use to seat a room.
func TestTheHolderListTicketTypeFilterKeepsAMixedSalesOtherTicketsOff(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Mixed Fest", "mixed-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2000, 100)
	vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 9000, 100)

	commitBatch(t, env, sessionID, eventID, "mixed-batch", []map[string]any{
		{"customer_email": "mixed@example.com", "customer_first_name": "Mix", "customer_last_name": "Ed",
			"ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	// A second line on the same sale, and the Ticket it mints: one Ticket Sale
	// holding two GA Tickets and one VIP Ticket. Staged in SQL because no import
	// row carries two types — the same seeding the Sales list's own multi-line
	// test does.
	if _, err := env.db.Exec(`
		WITH line AS (
			INSERT INTO ticket_sale_lines (ticket_sale_id, ticket_type_id, quantity, unit_price_cents, created_at)
			SELECT ts.id, $1, 1, 9000, NOW()
			FROM ticket_sales ts
			WHERE ts.event_id = $2 AND ts.customer_email = 'mixed@example.com'
			RETURNING id
		)
		INSERT INTO tickets (ticket_sale_line_id, ordinal, created_at)
		SELECT line.id, 1, NOW() FROM line
	`, vipID, eventID); err != nil {
		t.Fatalf("seed the mixed sale's VIP line: %v", err)
	}

	// Three Tickets on one sale, and the roster is the Tickets.
	assertRoster(t, holderRoster(t, env, sessionID, eventID, ""), []string{
		"mixed@example.com", "mixed@example.com", "mixed@example.com",
	})

	// One VIP Ticket, and exactly one.
	vip := holderRoster(t, env, sessionID, eventID, "?ticket_type_id="+vipID)
	assertRoster(t, vip, []string{"mixed@example.com"})
	if vip.Data[0].TicketTypeID != vipID {
		t.Fatalf("the VIP roster holds a %s Ticket — the filter matched the SALE and not the Ticket",
			vip.Data[0].TicketTypeName)
	}
	if got := holderRoster(t, env, sessionID, eventID, "?ticket_type_id="+gaID); got.Pagination.Total != 2 {
		t.Fatalf("the GA roster holds %d Tickets, want the sale's 2", got.Pagination.Total)
	}
}

// THE SALES CHANNEL FILTER: the buyers who came through the door, or through an
// import and were therefore never asked anything.
func TestTheHolderListFiltersBySalesChannel(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	f := newHolderFilterFixture(t, env)

	assertRoster(t, holderRoster(t, env, f.sessionID, f.eventID, "?channel=online"), []string{
		"vip-online@example.com", "ga-online@example.com",
	})
	assertRoster(t, holderRoster(t, env, f.sessionID, f.eventID, "?channel=in_person"), []string{
		"vip-door@example.com",
	})
	assertRoster(t, holderRoster(t, env, f.sessionID, f.eventID, "?channel=import"), []string{
		"ga-import@example.com",
	})
}

// THE DATE BOUNDS ARE READ IN THE EVENT'S TIMEZONE, and the end day is included
// whole.
//
// This is the test the fixture's awkward sale times exist for. The Event is in
// America/New_York, UTC-4 in July, so local midnight on the 1st is 04:00Z: a
// bound resolved in UTC instead would select vip-online (06-30 23:59 EDT, which
// is 07-01 03:59Z) and drop vip-door (07-01 23:59 EDT, which is 07-02 03:59Z).
// Both mistakes are one row wide and neither is visible on an Event that
// happens to be in UTC.
func TestTheHolderListDateBoundsAreReadInTheEventsTimezone(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	f := newHolderFilterFixture(t, env)

	// Both bounds on the 1st: the two Tickets sold on the 1st LOCALLY, including
	// the one at 23:59 — the end day is inclusive, which is what makes
	// `sold_from=X&sold_to=X` mean "that day" rather than "the instant of
	// midnight".
	assertRoster(t, holderRoster(t, env, f.sessionID, f.eventID,
		"?sold_from=2026-07-01&sold_to=2026-07-01"), []string{
		"ga-online@example.com", "vip-door@example.com",
	})

	// Open upper bound: the 1st onwards, so the 06-30 sale drops out and the
	// 07-02 one stays.
	assertRoster(t, holderRoster(t, env, f.sessionID, f.eventID, "?sold_from=2026-07-01"), []string{
		"ga-online@example.com", "vip-door@example.com", "ga-import@example.com",
	})

	// Open lower bound: through the 1st, so the 07-02 sale drops out and the
	// 06-30 one stays.
	assertRoster(t, holderRoster(t, env, f.sessionID, f.eventID, "?sold_to=2026-07-01"), []string{
		"vip-online@example.com", "ga-online@example.com", "vip-door@example.com",
	})

	// And the same Event in UTC selects a DIFFERENT pair from the same rows,
	// which is the whole point stated as an experiment: the zone is not
	// decoration, it decides who is on the list.
	setEventTimezone(t, env, f.eventID, "UTC")
	assertRoster(t, holderRoster(t, env, f.sessionID, f.eventID,
		"?sold_from=2026-07-01&sold_to=2026-07-01"), []string{
		"vip-online@example.com", "ga-online@example.com",
	})
}

// THE FILTERS COMPOSE, with each other and with `outstanding`. "Which VIP door
// sales are still unclaimed" is one query, which is the whole argument for
// having filters rather than a set of separate views.
func TestTheHolderListFiltersCompose(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	enableTicketQuestions(t)
	f := newHolderFilterFixture(t, env)

	// VIP and in_person: one Ticket, and it is neither the other VIP nor the
	// other door sale.
	assertRoster(t, holderRoster(t, env, f.sessionID, f.eventID,
		"?ticket_type_id="+f.vipID+"&channel=in_person"), []string{"vip-door@example.com"})

	// VIP, online, and sold on 06-30 locally: the intersection is one row, and
	// asking for the same three filters over the 1st is empty rather than wrong.
	assertRoster(t, holderRoster(t, env, f.sessionID, f.eventID,
		"?ticket_type_id="+f.vipID+"&channel=online&sold_from=2026-06-30&sold_to=2026-06-30"),
		[]string{"vip-online@example.com"})
	assertRoster(t, holderRoster(t, env, f.sessionID, f.eventID,
		"?ticket_type_id="+f.vipID+"&channel=online&sold_from=2026-07-01&sold_to=2026-07-01"),
		[]string{})

	// AND WITH `outstanding`. Only the VIP type asks anything, so only the two
	// VIP Tickets owe — and the door one is the answer to "which VIP door sales
	// are still unclaimed".
	createTicketQuestion(t, env, f.sessionID, f.eventID, f.vipID, map[string]any{
		"label": "Dietary requirements", "kind": "short_text", "required": true,
	})
	assertRoster(t, holderRoster(t, env, f.sessionID, f.eventID, "?outstanding=true"), []string{
		"vip-online@example.com", "vip-door@example.com",
	})
	assertRoster(t, holderRoster(t, env, f.sessionID, f.eventID,
		"?outstanding=true&channel=in_person"), []string{"vip-door@example.com"})
	// A GA Ticket owes nothing, so GA plus outstanding is empty — the two
	// filters intersect rather than one of them winning.
	assertRoster(t, holderRoster(t, env, f.sessionID, f.eventID,
		"?outstanding=true&ticket_type_id="+f.gaID), []string{})
}

// PAGINATION DESCRIBES THE FILTERED VIEW, not the roster behind it.
//
// The count query and the page query take their WHERE from one place, and this
// is what says so. A total left counting the whole roster is invisible on page
// one — the rows are right — and shows up only as a page count promising pages
// that answer with nothing.
func TestTheHolderListPaginationFollowsTheFilters(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	f := newHolderFilterFixture(t, env)

	whole := holderRoster(t, env, f.sessionID, f.eventID, "?page_size=1")
	if whole.Pagination.Total != 4 || whole.Pagination.TotalPages != 4 {
		t.Fatalf("unfiltered total=%d pages=%d, want 4 across 4 pages",
			whole.Pagination.Total, whole.Pagination.TotalPages)
	}

	filtered := holderRoster(t, env, f.sessionID, f.eventID, "?channel=online&page_size=1")
	if filtered.Pagination.Total != 2 || filtered.Pagination.TotalPages != 2 {
		t.Fatalf("filtered total=%d pages=%d, want 2 across 2 pages — the count ignored the filter",
			filtered.Pagination.Total, filtered.Pagination.TotalPages)
	}
	if got := holderBuyers(filtered); len(got) != 1 || got[0] != "vip-online@example.com" {
		t.Fatalf("filtered page 1 = %v, want the earlier online sale alone", got)
	}
	// The second page of the filtered view is the OTHER online sale, and not the
	// second row of the unfiltered roster.
	second := holderRoster(t, env, f.sessionID, f.eventID, "?channel=online&page=2&page_size=1")
	if got := holderBuyers(second); len(got) != 1 || got[0] != "ga-online@example.com" {
		t.Fatalf("filtered page 2 = %v, want the later online sale — the offset is being applied to the wrong view", got)
	}
}

// AN UNUSABLE FILTER IS IGNORED, NEVER REFUSED (#523, and #524 generalises it).
//
// This surface is lenient throughout — a bad `page`, a bad `page_size` and a
// mangled `outstanding` all fall back rather than erroring — and a malformed
// date, an unknown channel or a malformed Ticket Type id join them. A stale
// bookmark stays a working roster instead of becoming an error page, and the
// filter bar showing that field empty is how the reader sees the view is wide.
//
// The Ticket Type case is the one with teeth: `ticket_type_id` is compared to a
// `uuid NOT NULL` column, so a non-uuid reaching the query is a 500 rather than
// a 400 — a hand-edited URL turning into a server fault.
func TestTheHolderListIgnoresAnUnusableFilter(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	f := newHolderFilterFixture(t, env)

	for _, query := range []string{
		"?sold_from=01-07-2026",
		"?sold_to=nonsense",
		"?channel=doorway",
		"?ticket_type_id=not-a-uuid",
		// An unlisted assignment state joins them (#524). `purged` is the word
		// somebody would guess for the fourth value, and guessing it must widen
		// the roster rather than empty it.
		"?assignment_state=purged",
	} {
		page := holderRoster(t, env, f.sessionID, f.eventID, query)
		if page.Pagination.Total != 4 {
			t.Errorf("%s = %d rows, want the whole roster of 4 — an unusable filter is ignored, not honoured as an empty one",
				query, page.Pagination.Total)
		}
	}

	// A WELL-FORMED id that names nothing is a different case and IS honoured:
	// it is a filter the caller could have meant, it matches nothing because
	// nothing matches, and the query is already scoped to this Event, so it can
	// neither reach nor reveal another Organization's Ticket Type.
	assertRoster(t, holderRoster(t, env, f.sessionID, f.eventID,
		"?ticket_type_id=00000000-0000-0000-0000-000000000000"), []string{})
}

// THE ASSIGNMENT-STATE FILTER (#524, ADR 0065): where each Ticket stands with
// its Holder, in four selectable values — and `never_accepted` is one of them
// WITHOUT A FOURTH ASSIGNMENT STATE EXISTING ANYWHERE.
//
// catalog.AssignmentState still knows three states; #331 rejected a fourth and
// nothing here reopens it. The fourth VALUE selects the presentation
// service.fillHolderListEntry already derives at read time from the retention
// purge's marker: somebody was named, nobody claimed the Ticket, and the
// address is gone by definition. "Who did I name who never claimed their
// ticket" is the morning-after question, and after the Event starts it is
// otherwise indistinguishable from "nobody was named".
//
// THESE TESTS ARE WHERE THE FILTER'S SQL AND fillHolderListEntry ARE HELD
// TOGETHER, exactly as the tests at the top of this file hold the debt's two
// statements together. repository.holderRosterAssignmentStates is the same four
// tests over the same four columns, in SQL because a roster of thousands cannot
// be filtered in Go — and only real rows can notice the two drifting apart.

// holderStateFixture is one Event holding ONE TICKET IN EACH of the filter's
// four values, so a count of rows is a count of the thing being tested and the
// four counts can be summed against the roster.
//
// ONE SALE OF FOUR TICKETS, COMMITTED BEFORE THE FLAG IS OPENED. With Ticket
// Assignment open at commit time a Sale Import hands the buyer its first Ticket
// by presumption (ADR 0055), which would start the roster with an `accepted`
// row nobody chose — so the batch goes in dark and every Ticket starts
// `unassigned`.
//
// THREE OF THE FOUR ARE REACHED THROUGH THE API: the buyer signs in and assigns,
// and one of the two named people clicks their Assignment Link and accepts. The
// PURGED one is staged in SQL, and that is not laziness — see
// stageHolderAddressPurge for why the real purge cannot produce this roster.
type holderStateFixture struct {
	sessionID string
	eventID   string
	saleID    string

	unassigned    string
	assigned      string
	accepted      string
	neverAccepted string
}

func newHolderStateFixture(t *testing.T, env *testEnv) holderStateFixture {
	t.Helper()
	f := holderStateFixture{}
	f.sessionID = orgAdminSession(t, env)
	f.eventID = createDraftEvent(t, env, f.sessionID, "State Fest", "state-fest")
	// SCHEDULED AND COMFORTABLY IN THE FUTURE: the assignment window closes at
	// the doors, and two of these states are reached by assigning.
	scheduleEvent(t, env, f.sessionID, f.eventID, "State Fest", "state-fest",
		env.fixedClock.Add(30*24*time.Hour))
	ticketTypeID := createTicketTypeWithCapacity(t, env, f.sessionID, f.eventID, "GA", 2000, 50)

	commitBatch(t, env, f.sessionID, f.eventID, "state-batch", []map[string]any{{
		"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
		"ticket_type_id": ticketTypeID, "quantity": 4, "payment_method": "cash",
		"sold_at": "2026-07-01T10:00:00Z",
	}})
	if err := env.db.QueryRow(`
		SELECT id FROM ticket_sales WHERE event_id = $1 AND customer_email = 'ana@example.com'
	`, f.eventID).Scan(&f.saleID); err != nil {
		t.Fatalf("read Ana's Ticket Sale: %v", err)
	}
	tickets := ticketIDsOfSale(t, env, f.saleID)
	if len(tickets) != 4 {
		t.Fatalf("Tickets minted = %d, want the sale's 4", len(tickets))
	}

	enableTicketAssignment(t)
	ana := customerSignIn(t, env, "ana@example.com")

	// `accepted`: the whole walk — named, mailed, clicked.
	assignTicketOK(t, env, ana, f.saleID, tickets[1], "carla@example.com")
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))
	// `assigned`: named and waiting, which is where most of a roster sits.
	assignTicketOK(t, env, ana, f.saleID, tickets[2], "diego@example.com")
	// `never_accepted`: named, never claimed, address taken.
	stageHolderAddressPurge(t, env, tickets[3])

	f.unassigned, f.accepted, f.assigned, f.neverAccepted = tickets[0], tickets[1], tickets[2], tickets[3]
	return f
}

// stageHolderAddressPurge leaves one Ticket exactly as the retention purge
// leaves it: the address gone, `assigned_at` gone with it, and migration 081's
// marker in their place (#334).
//
// SQL, AND FOR A REASON THE NEXT READER MUST NOT "FIX" BY CALLING THE PURGE.
// The purge takes EVERY unaccepted address of a started Event at once, and the
// window for assigning closes at the same doors — so a roster holding both a
// plain `assigned` Ticket and a purged one is unreachable by running it. The
// purge itself is somebody else's subject: #334's test drives the real thing
// end to end and asserts the row it produces, which is the row staged here.
// What these tests are about is which of four values selects it.
func stageHolderAddressPurge(t *testing.T, env *testEnv, ticketID string) {
	t.Helper()
	res, err := env.db.Exec(`
		UPDATE tickets
		SET holder_email = NULL, assigned_at = NULL, holder_address_purged_at = NOW()
		WHERE id = $1
	`, ticketID)
	if err != nil {
		t.Fatalf("stage the purge marker on Ticket %s: %v", ticketID, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("staging the purge marker on Ticket %s affected %d rows, want 1", ticketID, n)
	}
}

// holderTicketIDs names the Tickets on the page, so an assertion about a roster
// of one buyer's four Tickets can say WHICH row it means.
func holderTicketIDs(page outstandingAnswers) []string {
	ids := make([]string, 0, len(page.Data))
	for _, row := range page.Data {
		ids = append(ids, row.TicketID)
	}
	return ids
}

// assertOneTicket holds both halves of a one-row view: that it is the right
// Ticket, and that the pagination agrees the view holds one. Same pairing as
// assertRoster, and for the same reason — a count query that forgot the filter
// is invisible to a test that checks only the rows.
func assertOneTicket(t *testing.T, page outstandingAnswers, want, label string) {
	t.Helper()
	got := holderTicketIDs(page)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("%s = %v, want the one Ticket %s", label, got, want)
	}
	if page.Pagination.Total != 1 {
		t.Fatalf("%s pagination total = %d over 1 row — the count query and the page query disagree",
			label, page.Pagination.Total)
	}
}

// EACH OF THE FOUR VALUES NARROWS THE ROSTER, and each selects the Ticket whose
// row already reads that way.
//
// The `never_accepted` case is the one with teeth. Its Ticket reports
// `assignment_state: assigned` with `never_accepted` beside it — that is
// fillHolderListEntry's presentation and it does not change — so a filter
// written off the wire FIELD instead of off the same columns would return it
// under `assigned` and nothing under `never_accepted`.
func TestTheHolderListFiltersByAssignmentState(t *testing.T) {
	env := setupTest(t)
	f := newHolderStateFixture(t, env)

	// The whole roster first, so the filters below are narrowings of something
	// known: one buyer's four Tickets, in ordinal order.
	whole := holderRoster(t, env, f.sessionID, f.eventID, "")
	if whole.Pagination.Total != 4 {
		t.Fatalf("roster = %d Tickets, want the sale's 4", whole.Pagination.Total)
	}

	assertOneTicket(t, holderRoster(t, env, f.sessionID, f.eventID, "?assignment_state=unassigned"),
		f.unassigned, "unassigned")
	assertOneTicket(t, holderRoster(t, env, f.sessionID, f.eventID, "?assignment_state=assigned"),
		f.assigned, "assigned")
	assertOneTicket(t, holderRoster(t, env, f.sessionID, f.eventID, "?assignment_state=accepted"),
		f.accepted, "accepted")

	purged := holderRoster(t, env, f.sessionID, f.eventID, "?assignment_state=never_accepted")
	assertOneTicket(t, purged, f.neverAccepted, "never_accepted")
	// And the row it returned is the one the screen draws as "never accepted":
	// `assigned` on the wire, with the marker beside it. The filter and the
	// presentation are one rule stated twice, and this is where they meet.
	if row := purged.Data[0]; row.AssignmentState != "assigned" || !row.NeverAccepted {
		t.Errorf("the never_accepted row reads state=%q never_accepted=%v, want `assigned` with the marker — "+
			"the filter selects fillHolderListEntry's presentation and does not invent a fourth state",
			row.AssignmentState, row.NeverAccepted)
	}
}

// THE FOUR VALUES PARTITION THE ROSTER: every Ticket matches exactly one, so the
// four filtered counts sum to the unfiltered total.
//
// THIS IS THE TEST THAT NOTICES A CLAUSE ADDED TO ONE SIDE AND NOT THE OTHER.
// The four predicates are derived from the same four columns
// fillHolderListEntry reads, and the whole risk of writing them twice is that
// they come to overlap or to leak: a purged Ticket counted under BOTH
// `never_accepted` and `assigned` makes the counts sum to more than the roster,
// and one counted under NEITHER makes them sum to less — which is worse,
// because a Ticket no value selects is a Ticket a filtered export silently
// drops.
//
// A PURGED TICKET IS ON `never_accepted` AND NOWHERE ELSE. It is deliberately
// NOT on `assigned`, though its row says `assigned`, because the badge the
// reader is looking at says "never accepted" — the filter agrees with the word
// on the screen. And it is deliberately not on `unassigned` either, though
// catalog.AssignmentState reads it as unassigned everywhere else, because the
// purge took the address and "nobody was named" would rewrite what happened.
func TestTheHolderListAssignmentStatesPartitionTheRoster(t *testing.T) {
	env := setupTest(t)
	f := newHolderStateFixture(t, env)

	total := holderRoster(t, env, f.sessionID, f.eventID, "").Pagination.Total
	sum := 0
	for _, state := range []string{"unassigned", "assigned", "accepted", "never_accepted"} {
		sum += holderRoster(t, env, f.sessionID, f.eventID, "?assignment_state="+state).Pagination.Total
	}
	if sum != total {
		t.Errorf("the four states hold %d Tickets between them over a roster of %d.\n"+
			"The four values must PARTITION the roster — every Ticket in exactly one — "+
			"or the counts do not add up and a filtered export loses or duplicates rows.", sum, total)
	}

	// Said again as the two facts it is made of, so a failure names which half
	// broke rather than only that the arithmetic did.
	for _, state := range []string{"assigned", "unassigned"} {
		page := holderRoster(t, env, f.sessionID, f.eventID, "?assignment_state="+state)
		for _, row := range page.Data {
			if row.TicketID == f.neverAccepted {
				t.Errorf("the purged Ticket also answers to `%s`.\n"+
					"It belongs to `never_accepted` alone: on `assigned` the two values overlap, "+
					"and on `unassigned` the list would say nobody was ever named.", state)
			}
		}
	}
}

// THE FILTERS COMPOSE WITH THE STRUCTURAL ONES, which is what makes "which VIP
// door sales were never claimed" one query rather than a page somebody reads
// down.
func TestTheHolderListAssignmentStateComposesWithTheOtherFilters(t *testing.T) {
	env := setupTest(t)
	f := newHolderStateFixture(t, env)

	// Every Ticket of this fixture is on the same import sale of the same day,
	// so a matching structural filter leaves the state's answer alone…
	assertOneTicket(t, holderRoster(t, env, f.sessionID, f.eventID,
		"?assignment_state=accepted&channel=import"), f.accepted, "accepted+import")
	// …and a non-matching one empties it, rather than one of the two winning.
	if page := holderRoster(t, env, f.sessionID, f.eventID,
		"?assignment_state=accepted&channel=online"); page.Pagination.Total != 0 {
		t.Errorf("accepted+online = %d Tickets, want none — the filters intersect", page.Pagination.Total)
	}
}

// WITH TICKET ASSIGNMENT DARK, `assignment_state` IS IGNORED AND THE WHOLE
// ROSTER COMES BACK — 200, not 400, and not an empty list.
//
// THIS IS THE RULE #525 AND EVERYTHING AFTER IT FOLLOW (ADR 0065, "a dark
// feature's filter: ignored, not refused"). A refusal would turn a stale
// bookmark into an error page — the view lives in the URL since #522 — and it
// would force the staff app to hold a copy of a deployment flag that ADR 0045
// exists to keep it from knowing: the app decides which controls exist from
// what the payload CONTAINS, and a control that 400s is not a control.
//
// A 400 would also be a TELL. A build with assignment dark must answer as a
// build without the feature would, and an error naming `assignment_state` would
// admit the parameter exists.
//
// The list is read through the QUESTIONS flag here, because with both dark the
// route is a 404 (#333) and there would be no roster to be wide.
func TestTheHolderListIgnoresTheAssignmentStateFilterWhileAssignmentIsDark(t *testing.T) {
	env := setupTest(t)
	f := newHolderStateFixture(t, env)
	enableTicketQuestions(t)
	closeTicketAssignment(t)

	resp, body := env.get(t, holderListPath(f.eventID)+"?assignment_state=accepted", authHeader(f.sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d error=%+v — a filter belonging to a dark feature is IGNORED, never refused: "+
			"a URL carrying it must still return the roster", resp.StatusCode, body.Error)
	}
	page := decodeOutstanding(t, body.Data)
	if page.Pagination.Total != 4 || len(page.Data) != 4 {
		t.Fatalf("rows=%d total=%d, want the WHOLE roster of 4 — the dark filter must not narrow anything, "+
			"and must not empty the list either", len(page.Data), page.Pagination.Total)
	}
	// And the payload still admits nothing about the dark feature: no state
	// travels, so nothing on this response could have been filtered by one.
	if strings.Contains(string(body.Data), "assignment_state") {
		t.Error("the response speaks of `assignment_state` while TICKET_ASSIGNMENT_ENABLED is closed (ADR 0045)")
	}

	// Every one of the four values, so no single one of them is a side channel:
	// `never_accepted` in particular must not narrow a dark build's roster to
	// the purged Tickets.
	for _, state := range []string{"unassigned", "assigned", "accepted", "never_accepted"} {
		if got := holderRoster(t, env, f.sessionID, f.eventID,
			"?assignment_state="+state).Pagination.Total; got != 4 {
			t.Errorf("`%s` on a dark build returns %d Tickets, want the whole roster of 4", state, got)
		}
	}
}

// THE NAMED-QUESTION FILTER (#525, ADR 0065): the Tickets owing ONE named
// Ticket Question, rather than owing anything at all.
//
// "Who still hasn't told me their shirt size" is a different chase from "who
// owes me something", and on an Event asking several questions the boolean is
// too blunt to work from — the Organizer ordering shirts has to read past the
// people who only owe a dietary note.
//
// NOTHING IN THIS FILTER DECIDES WHAT IS OUTSTANDING, and these tests are what
// says so. repository.holderRosterOwingQuestion is holderRosterOwingOnly with
// `AND q.id = $n` added to the same sub-query — the debt's own
// outstandingAnswerFrom and outstandingAnswerWhere, reused and not restated —
// so the retired question, the optional one, the reversed sale and the
// approval gate follow it for free. TestTheNamedQuestionFilterAnswersAs
// OutstandingDoes below is the proof: wherever `outstanding` says a Ticket owes
// nothing, `question_id` must find nothing either. A second predicate spelling
// the debt out again would pass every other test here and fail that one, on the
// retired case, which is the one nobody thinks about.

// holderQuestionFixture is an Event whose Ticket Type asks TWO required
// questions, over three Tickets on two sales — one online, one at the door —
// with one question answered on one Ticket.
//
// THE PARTLY-ANSWERED TICKET IS THE POINT. With every Ticket owing everything,
// a filter that ignored `question_id` entirely would pass; Ana's first Ticket
// owes the diet question and NOT the size one, so the two questions select
// different rows and a filter that did nothing is visible immediately.
type holderQuestionFixture struct {
	sessionID string
	eventID   string
	gaID      string

	// size and diet are the two required questions the Ticket Type asks.
	sizeID string
	dietID string

	// anaAnswered has answered `size` and owes `diet` alone; anaOwing owes
	// both. Both are on the same online sale.
	anaAnswered string
	anaOwing    string
	// betoDoor is the door sale's single Ticket, owing both — a buyer nobody
	// ever put the questions to, because there is no checkout form on a door
	// sale.
	betoDoor string
}

func newHolderQuestionFixture(t *testing.T, env *testEnv) holderQuestionFixture {
	t.Helper()
	f := holderQuestionFixture{}
	f.sessionID = orgAdminSession(t, env)
	f.eventID = createDraftEvent(t, env, f.sessionID, "Question Fest", "question-fest")
	f.gaID = createTicketTypeWithCapacity(t, env, f.sessionID, f.eventID, "GA", 2000, 50)

	commitBatch(t, env, f.sessionID, f.eventID, "question-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
			"ticket_type_id": f.gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		// A day later, so the roster's order (oldest sale first) is settled and
		// an assertion can name the rows it expects in order.
		{"customer_email": "beto@example.com", "customer_first_name": "Beto", "customer_last_name": "Diaz",
			"ticket_type_id": f.gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
	})
	// Both sales are staged onto a channel of their own, the way
	// newHolderFilterFixture stages its three: a Sale Import commits everything
	// on `import`, and this fixture needs the two apart so the composition test
	// can ask for the door sale's debtors alone.
	moveHolderSaleToChannel(t, env, f.eventID, "ana@example.com", "online")
	moveHolderSaleToChannel(t, env, f.eventID, "beto@example.com", "in_person")

	anaTickets := ticketIDsOfSale(t, env, saleIDOfBuyer(t, env, f.eventID, "ana@example.com"))
	if len(anaTickets) != 2 {
		t.Fatalf("Ana's Tickets = %d, want 2", len(anaTickets))
	}
	betoTickets := ticketIDsOfSale(t, env, saleIDOfBuyer(t, env, f.eventID, "beto@example.com"))
	if len(betoTickets) != 1 {
		t.Fatalf("Beto's Tickets = %d, want 1", len(betoTickets))
	}
	f.anaAnswered, f.anaOwing, f.betoDoor = anaTickets[0], anaTickets[1], betoTickets[0]

	f.sizeID = createTicketQuestion(t, env, f.sessionID, f.eventID, f.gaID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	}).ID
	f.dietID = createTicketQuestion(t, env, f.sessionID, f.eventID, f.gaID, map[string]any{
		"label": "Any dietary requirements?", "kind": "short_text", "required": true,
	}).ID

	// One Answer, on one Ticket, to one of the two questions.
	putAnswer(t, env, f.sessionID, f.eventID, f.anaAnswered, f.sizeID, map[string]any{"text": "M"})
	return f
}

// saleIDOfBuyer names one Event's sale by its buyer, so a fixture can say whose
// sale it means rather than counting rows.
func saleIDOfBuyer(t *testing.T, env *testEnv, eventID, email string) string {
	t.Helper()
	var saleID string
	if err := env.db.QueryRow(`
		SELECT id FROM ticket_sales WHERE event_id = $1 AND customer_email = $2
	`, eventID, email).Scan(&saleID); err != nil {
		t.Fatalf("read %s's Ticket Sale: %v", email, err)
	}
	return saleID
}

// assertTickets holds both halves of a filtered view at once: WHICH Tickets are
// on the page, in order, and that the pagination agrees the view holds that
// many. Same pairing as assertRoster and assertOneTicket, for their reason — a
// count query that forgot a filter is invisible to a test that checks only the
// rows.
func assertTickets(t *testing.T, page outstandingAnswers, want []string, label string) {
	t.Helper()
	got := holderTicketIDs(page)
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", label, got, want)
		}
	}
	if page.Pagination.Total != len(want) {
		t.Fatalf("%s pagination total = %d over %d rows — the count query and the page query disagree about the view",
			label, page.Pagination.Total, len(want))
	}
}

// FILTERING BY ONE QUESTION RETURNS THE TICKETS OWING THAT QUESTION, AND NO
// OTHERS.
//
// The two questions select different rows because one Ticket has answered one
// of them, which is the whole distinction the boolean cannot draw: three
// Tickets owe something, two of them owe a shirt size.
func TestTheHolderListFiltersByOneNamedTicketQuestion(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	f := newHolderQuestionFixture(t, env)

	// The whole roster first, so the filters below are narrowings of something
	// known: three Tickets, oldest sale first.
	assertTickets(t, holderRoster(t, env, f.sessionID, f.eventID, ""),
		[]string{f.anaAnswered, f.anaOwing, f.betoDoor}, "the whole roster")

	assertTickets(t, holderRoster(t, env, f.sessionID, f.eventID, "?question_id="+f.sizeID),
		[]string{f.anaOwing, f.betoDoor}, "owing the shirt size")
	assertTickets(t, holderRoster(t, env, f.sessionID, f.eventID, "?question_id="+f.dietID),
		[]string{f.anaAnswered, f.anaOwing, f.betoDoor}, "owing the dietary note")

	// The Event's debt count is the EVENT's and is unmoved by any filter — five
	// debts over three Tickets, whichever question is being chased. It answers
	// "how much don't I know yet", which is not a question about the page.
	if page := holderRoster(t, env, f.sessionID, f.eventID, "?question_id="+f.sizeID); page.OutstandingCount != 5 {
		t.Errorf("outstanding_count = %d under the question filter, want the Event's 5", page.OutstandingCount)
	}

	// And a well-formed id nobody owes — a question of another Event, or one
	// that never existed — is an empty roster and not a refusal. The query is
	// scoped to this Event of this Organization, so such a filter cannot even
	// distinguish an id that exists elsewhere from one that exists nowhere.
	if page := holderRoster(t, env, f.sessionID, f.eventID,
		"?question_id=11111111-1111-4111-8111-111111111111"); page.Pagination.Total != 0 {
		t.Errorf("a question nobody owes returned %d Tickets, want none", page.Pagination.Total)
	}
}

// IT COMPOSES WITH `outstanding`, WHICH IS UNTOUCHED — and the two together
// mean what the question means alone, because owing this question implies owing
// something.
//
// That identity is the evidence the named question NARROWS the existing answer
// instead of replacing it. If the two filters disagreed, one of them would be
// deciding what is outstanding for itself.
func TestTheNamedQuestionFilterComposesWithOutstanding(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	f := newHolderQuestionFixture(t, env)

	alone := holderRoster(t, env, f.sessionID, f.eventID, "?question_id="+f.sizeID)
	both := holderRoster(t, env, f.sessionID, f.eventID, "?outstanding=true&question_id="+f.sizeID)
	assertTickets(t, alone, []string{f.anaOwing, f.betoDoor}, "the question alone")
	assertTickets(t, both, holderTicketIDs(alone), "the question with outstanding=true")

	// And `outstanding` on its own still means what it always meant: every
	// Ticket owing anything, which here is all three. The named question is a
	// narrowing beside it and never a redefinition of it.
	assertTickets(t, holderRoster(t, env, f.sessionID, f.eventID, "?outstanding=true"),
		[]string{f.anaAnswered, f.anaOwing, f.betoDoor}, "outstanding alone")
}

// IT COMPOSES WITH THE STRUCTURAL FILTERS AND WITH THE STATE FILTER, which is
// what makes "which door sales still owe me a shirt size" one query rather than
// a page somebody reads down.
func TestTheNamedQuestionFilterComposesWithTheOtherFilters(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	f := newHolderQuestionFixture(t, env)
	// ASSIGNMENT IS OPENED AFTER THE SALES ARE COMMITTED, as
	// newHolderStateFixture opens it: with the flag on at commit time a Sale
	// Import hands the buyer its first Ticket by presumption (ADR 0055), which
	// would start this roster with `accepted` rows nobody chose and make the
	// state assertions below about the import rather than about the filter.
	enableTicketAssignment(t)

	// The door sale alone, of the two Tickets owing a shirt size.
	assertTickets(t, holderRoster(t, env, f.sessionID, f.eventID,
		"?question_id="+f.sizeID+"&channel=in_person"), []string{f.betoDoor}, "size at the door")
	// And the online sale's one remaining debtor.
	assertTickets(t, holderRoster(t, env, f.sessionID, f.eventID,
		"?question_id="+f.sizeID+"&channel=online"), []string{f.anaOwing}, "size online")

	// A non-matching structural filter EMPTIES the view rather than one of the
	// two filters winning: they intersect.
	if page := holderRoster(t, env, f.sessionID, f.eventID,
		"?question_id="+f.sizeID+"&channel=import"); page.Pagination.Total != 0 {
		t.Errorf("size on the import channel = %d Tickets, want none — the filters intersect",
			page.Pagination.Total)
	}
	// Nobody on this roster was ever named, so the state filter narrows it to
	// the same rows and `accepted` empties it — the two dimensions are
	// independent, as they must be.
	assertTickets(t, holderRoster(t, env, f.sessionID, f.eventID,
		"?question_id="+f.sizeID+"&assignment_state=unassigned"),
		[]string{f.anaOwing, f.betoDoor}, "size, nobody named")
	if page := holderRoster(t, env, f.sessionID, f.eventID,
		"?question_id="+f.sizeID+"&assignment_state=accepted"); page.Pagination.Total != 0 {
		t.Errorf("size among accepted Tickets = %d, want none on a roster nobody has accepted",
			page.Pagination.Total)
	}

	// The sale date, and the Ticket Type the questions hang off — both narrow
	// this filter exactly as they narrow the plain roster.
	assertTickets(t, holderRoster(t, env, f.sessionID, f.eventID,
		"?question_id="+f.sizeID+"&sold_from=2026-07-02"), []string{f.betoDoor}, "size sold on the 2nd")
	assertTickets(t, holderRoster(t, env, f.sessionID, f.eventID,
		"?question_id="+f.sizeID+"&ticket_type_id="+f.gaID),
		[]string{f.anaOwing, f.betoDoor}, "size on the GA roster")
}

// A RETIRED QUESTION AND AN OPTIONAL ONE ANSWER TO THIS FILTER EXACTLY AS THEY
// ANSWER TO `outstanding` — WHICH IS THE PROOF THAT NO SECOND STATEMENT OF THE
// RULE WAS INTRODUCED.
//
// Both are debts nobody has: an OPTIONAL question was never promised an Answer,
// and a RETIRED one has been un-asked — every write path refuses an Answer to
// it, so a debt under one could never be discharged by anybody. The rule that
// says so lives in catalog.IsOutstandingAnswer and in outstandingAnswerWhere
// beside it, and this filter reuses those clauses rather than restating them.
//
// SO THE TEST IS AN IMPLICATION AND NOT A CONSTANT: wherever the roster shows
// no Ticket owing a question, `question_id` on that question must return
// nothing. A hand-written predicate would almost certainly get the optional
// case right — `required` is the obvious clause — and get the retired one
// wrong, handing an Organizer a chase list of a question they stopped asking.
func TestTheNamedQuestionFilterAnswersAsOutstandingDoes(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	f := newHolderQuestionFixture(t, env)

	optional := createTicketQuestion(t, env, f.sessionID, f.eventID, f.gaID, map[string]any{
		"label": "Anything else we should know?", "kind": "long_text", "required": false,
	})
	retired := createTicketQuestion(t, env, f.sessionID, f.eventID, f.gaID, map[string]any{
		"label": "Bus from the station?", "kind": "checkbox", "required": true,
	})
	resp, body := env.deleteJSON(t, questionPath(f.eventID, f.gaID, retired.ID), nil, authHeader(f.sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retire question status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// What `outstanding` says: neither question is owed by anybody, on any
	// Ticket of the roster.
	roster := holderRoster(t, env, f.sessionID, f.eventID, "")
	for _, question := range []struct {
		id, label string
	}{{optional.ID, "the optional question"}, {retired.ID, "the retired question"}} {
		for _, row := range roster.Data {
			for _, owed := range row.Outstanding {
				if owed.QuestionID == question.id {
					t.Fatalf("%s is owed by Ticket %s on the roster — this test's premise is gone",
						question.label, row.TicketID)
				}
			}
		}
		if page := holderRoster(t, env, f.sessionID, f.eventID, "?question_id="+question.id); page.Pagination.Total != 0 {
			t.Errorf("%s selects %d Tickets while the roster says nobody owes it.\n"+
				"The named-question filter must NARROW the debt's own definition "+
				"(catalog.IsOutstandingAnswer and repository.outstandingAnswerWhere), never restate it — "+
				"a second predicate drifts here first.", question.label, page.Pagination.Total)
		}
	}

	// The retired question's own ANSWERS are untouched by any of this: what
	// ended is the debt, not the record. Asserted through the filter's own
	// vocabulary — a question that is not owed is not on this list, and that is
	// all this filter ever claimed.
	if page := holderRoster(t, env, f.sessionID, f.eventID, "?question_id="+f.sizeID); page.Pagination.Total != 2 {
		t.Errorf("the live question now selects %d Tickets, want 2 — retiring another question moved it",
			page.Pagination.Total)
	}
}

// WITH TICKET QUESTIONS DARK, `question_id` IS IGNORED AND THE WHOLE ROSTER
// COMES BACK — 200, not 400, and not an empty list.
//
// This is #524's rule followed and not re-decided (ADR 0065, "a dark feature's
// filter: ignored, not refused"): a refusal would turn a stale bookmark into an
// error page now that the view lives in the URL (#522), would force the staff
// app to hold a copy of a deployment flag ADR 0045 exists to keep it from
// holding, and would itself be a tell that an unshipped parameter exists.
//
// The list is read through the ASSIGNMENT flag here, because with both dark the
// route is a 404 (#333) and there would be no roster to be wide.
func TestTheHolderListIgnoresTheNamedQuestionFilterWhileQuestionsAreDark(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	f := newHolderQuestionFixture(t, env)
	enableTicketAssignment(t)
	// Closed the way every other test in this package closes it: both services,
	// because one deployment flag reaches both and half of it is a state no
	// deployment can be in.
	sharedApp.CatalogService.WithTicketQuestions(false)
	sharedApp.SalesService.WithTicketQuestions(false)

	resp, body := env.get(t, holderListPath(f.eventID)+"?question_id="+f.sizeID, authHeader(f.sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d error=%+v — a filter belonging to a dark feature is IGNORED, never refused: "+
			"a URL carrying it must still return the roster", resp.StatusCode, body.Error)
	}
	page := decodeOutstanding(t, body.Data)
	if page.Pagination.Total != 3 || len(page.Data) != 3 {
		t.Fatalf("rows=%d total=%d, want the WHOLE roster of 3 — the dark filter must not narrow anything, "+
			"and must not empty the list either", len(page.Data), page.Pagination.Total)
	}
	// And the payload still admits nothing about the dark feature: no debt
	// travels, so nothing here could have been filtered by one (ADR 0045).
	if strings.Contains(string(body.Data), "outstanding") {
		t.Error("the response speaks of `outstanding` while TICKET_QUESTIONS_ENABLED is closed (ADR 0045)")
	}
	// `outstanding` is dropped on the same line for the same reason, and the
	// pair is asserted together so that closing one and not the other cannot
	// pass.
	if got := holderRoster(t, env, f.sessionID, f.eventID,
		"?outstanding=true&question_id="+f.sizeID).Pagination.Total; got != 3 {
		t.Errorf("both question filters on a dark build return %d Tickets, want the whole roster of 3", got)
	}
}

// THE SEARCH BOX (#526, ADR 0065), AND THE SECURITY RULE IT IS BOUNDED BY:
// SEARCHABLE IF AND ONLY IF DISPLAYABLE.
//
// `q` is a case-insensitive substring over the buyer's name and address, the
// Sale Confirmation reference, and — FOR AN ACCEPTED HOLDER ONLY — that
// Holder's name and address. An address a buyer typed and its owner never
// accepted matches NOTHING, and neither does a purged one.
//
// THESE TESTS ARE THE ACCEPTANCE CRITERIA and one of them is load-bearing; see
// TestSearchingAnUnacceptedHoldersAddressReturnsZeroRows below. They are
// written against REAL ROWS and not against a query builder for the reason the
// structural filters' tests give: every part of this is a string reaching a
// predicate in SQL, and a clause moved from one branch of an OR to the outside
// of it compiles, runs, and differs from the truth only in whose address can be
// confirmed to be on an Organization's roster. Nothing but a database notices.

// holderSearchFixture is one Event whose roster holds every case the search has
// an opinion about: two buyers, and one buyer's four Tickets standing in the
// four assignment states — so a Holder address that IS disclosed, one that is
// NOT, and one the purge has taken can each be typed into the box and the
// answers compared.
//
// The Ticket Types are two so a search can be composed with a filter that
// contradicts it, and the whole roster is one Sale Import, so the channel and
// the day are constant and cannot be what a search test is accidentally
// measuring.
type holderSearchFixture struct {
	sessionID string
	eventID   string
	gaID      string
	vipID     string
	anaSaleID string

	unassigned    string
	accepted      string
	assigned      string
	neverAccepted string
}

// The addresses the fixture stands up, named here because three of the four are
// assertions in themselves: Carla's is disclosed and therefore searchable,
// Diego's is stored and must never match, Elena's has been purged and is gone.
const (
	holderSearchAcceptedEmail  = "carla@example.com"
	holderSearchUnacceptedMail = "diego@example.com"
	holderSearchPurgedEmail    = "elena@example.com"
)

func newHolderSearchFixture(t *testing.T, env *testEnv) holderSearchFixture {
	t.Helper()
	f := holderSearchFixture{}
	f.sessionID = orgAdminSession(t, env)
	f.eventID = createDraftEvent(t, env, f.sessionID, "Search Fest", "search-fest")
	// SCHEDULED AND COMFORTABLY IN THE FUTURE: the assignment window closes at
	// the doors, and two of these states are reached by assigning.
	scheduleEvent(t, env, f.sessionID, f.eventID, "Search Fest", "search-fest",
		env.fixedClock.Add(30*24*time.Hour))
	f.gaID = createTicketTypeWithCapacity(t, env, f.sessionID, f.eventID, "GA", 2000, 50)
	f.vipID = createTicketTypeWithCapacity(t, env, f.sessionID, f.eventID, "VIP", 9000, 50)

	// COMMITTED BEFORE THE FLAG IS OPENED, exactly as newHolderStateFixture is:
	// with Ticket Assignment open at commit time a Sale Import hands the buyer
	// its first Ticket by presumption (ADR 0055), which would start the roster
	// with an `accepted` row nobody chose.
	commitBatch(t, env, f.sessionID, f.eventID, "search-batch", []map[string]any{{
		"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
		"ticket_type_id": f.gaID, "quantity": 4, "payment_method": "cash",
		"sold_at": "2026-07-01T10:00:00Z",
	}, {
		"customer_email": "bruno@example.com", "customer_first_name": "Bruno", "customer_last_name": "Diaz",
		"ticket_type_id": f.vipID, "quantity": 1, "payment_method": "cash",
		"sold_at": "2026-07-02T10:00:00Z",
	}})
	f.anaSaleID = saleIDOfBuyer(t, env, f.eventID, "ana@example.com")
	tickets := ticketIDsOfSale(t, env, f.anaSaleID)
	if len(tickets) != 4 {
		t.Fatalf("Tickets minted = %d, want Ana's 4", len(tickets))
	}

	enableTicketAssignment(t)
	ana := customerSignIn(t, env, "ana@example.com")

	// `accepted`: named, mailed, clicked — AND NAMED HERSELF, which is the only
	// way a Holder's own name reaches this roster and therefore the only way it
	// can be searched for.
	assignTicketOK(t, env, ana, f.anaSaleID, tickets[1], holderSearchAcceptedEmail)
	token := assignmentTokenFrom(t, assignmentMailFor(t, env, holderSearchAcceptedEmail))
	acceptAssignmentOK(t, env, token)
	resp, body, _ := publicLinkRequest(t, env, http.MethodPut, assignmentLinkNamePath, map[string]any{
		"token": token, "first_name": "Carla", "last_name": "Ruiz",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holder name status=%d error=%+v", resp.StatusCode, body.Error)
	}
	// `assigned`: an address typed by the buyer that its owner never clicked.
	// THE SUBJECT OF THE LOAD-BEARING TEST BELOW.
	assignTicketOK(t, env, ana, f.anaSaleID, tickets[2], holderSearchUnacceptedMail)
	// `never_accepted`: named, never claimed, address taken by the purge. It is
	// assigned FIRST so the address really was stored before being nulled —
	// staging the marker on a Ticket nobody was named for would prove nothing.
	assignTicketOK(t, env, ana, f.anaSaleID, tickets[3], holderSearchPurgedEmail)
	stageHolderAddressPurge(t, env, tickets[3])

	f.unassigned, f.accepted, f.assigned, f.neverAccepted = tickets[0], tickets[1], tickets[2], tickets[3]
	return f
}

// holderSearch reads the roster under one search term, escaped as a browser
// would escape it — the terms here carry spaces, `@` and, in the wildcard test,
// the LIKE metacharacters themselves. `extra` carries any further filters, so a
// composition test reads as the one sentence it is asserting.
func holderSearch(
	t *testing.T, env *testEnv, sessionID, eventID, term, extra string,
) outstandingAnswers {
	t.Helper()
	return holderRoster(t, env, sessionID, eventID, "?q="+url.QueryEscape(term)+extra)
}

// assertNoRows is the shape three of these tests end in: NOTHING matched, and
// the pagination agrees that nothing did. Both halves, because a count query
// that missed the search predicate would leave a total over an empty page —
// and on this list that total would itself be the disclosure, since it would
// confirm the address is on the roster without drawing a row.
func assertNoRows(t *testing.T, page outstandingAnswers, term, why string) {
	t.Helper()
	if len(page.Data) != 0 || page.Pagination.Total != 0 {
		t.Fatalf("searching %q returned %d rows with a total of %d, want NONE.\n%s",
			term, len(page.Data), page.Pagination.Total, why)
	}
}

// assertSearchFinds names exactly which Tickets a search returned, so a failure
// says who was found rather than how many. Totals are checked with the rows for
// assertRoster's reason: the count and the page must describe one view.
func assertSearchFinds(t *testing.T, page outstandingAnswers, want []string, term string) {
	t.Helper()
	got := holderTicketIDs(page)
	if len(got) != len(want) {
		t.Fatalf("searching %q found %d Tickets %v, want %d %v", term, len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("searching %q found %v, want %v", term, got, want)
		}
	}
	if page.Pagination.Total != len(want) {
		t.Fatalf("searching %q put a total of %d over %d rows — the count query and the page query "+
			"disagree about the view, which since #526 is also a disagreement about which join they carry",
			term, page.Pagination.Total, len(want))
	}
}

// THE BUYER AND THE REFERENCE: the two things always on a row, whatever the
// feature flags say and whoever has or has not accepted anything.
func TestTheHolderListSearchesTheBuyerAndTheReference(t *testing.T) {
	env := setupTest(t)
	f := newHolderSearchFixture(t, env)

	// The whole roster first, so every search below is a narrowing of something
	// known: Ana's four Tickets, oldest sale first, then Bruno's one.
	whole := holderRoster(t, env, f.sessionID, f.eventID, "")
	if whole.Pagination.Total != 5 {
		t.Fatalf("roster = %d Tickets, want the Event's 5", whole.Pagination.Total)
	}
	anaTickets := []string{f.unassigned, f.accepted, f.assigned, f.neverAccepted}
	brunoTicket := holderTicketIDs(whole)[4]

	// The buyer's ADDRESS, in part.
	assertSearchFinds(t, holderSearch(t, env, f.sessionID, f.eventID, "bruno@example", ""),
		[]string{brunoTicket}, "bruno@example")
	// The buyer's NAME, joined as `first || ' ' || last` — the Sales list's
	// spelling, so a reader typing a full name is not defeated by the column
	// split.
	assertSearchFinds(t, holderSearch(t, env, f.sessionID, f.eventID, "Ana Lopez", ""),
		anaTickets, "Ana Lopez")
	// Half a surname, which is what a search box is actually used for.
	assertSearchFinds(t, holderSearch(t, env, f.sessionID, f.eventID, "iaz", ""),
		[]string{brunoTicket}, "iaz")

	// THE SALE CONFIRMATION REFERENCE, which is how a roster is joined back to
	// the receipt in somebody's inbox.
	var ref string
	for _, row := range whole.Data {
		if row.TicketID == brunoTicket {
			ref = row.ConfirmationRef
		}
	}
	if ref == "" {
		t.Fatal("Bruno's row carries no confirmation_ref — there is nothing to search by")
	}
	assertSearchFinds(t, holderSearch(t, env, f.sessionID, f.eventID, ref, ""),
		[]string{brunoTicket}, ref)
}

// AN ACCEPTED HOLDER IS SEARCHABLE BY NAME AND BY ADDRESS, because an accepted
// Holder is DISPLAYED by name and by address (ADR 0047): the Organization is
// already looking at both, and a roster that draws a person it cannot find is a
// roster nobody can work.
//
// THE ADDRESS MATCHED IS THE ONE THE ROW DISCLOSES — `tickets.holder_email`,
// which service.fillHolderListEntry puts on the wire — and not the joined
// Customer's own column. Matching a different column would make the searchable
// set and the displayed set the same set only by coincidence.
func TestTheHolderListSearchesAnAcceptedHolder(t *testing.T) {
	env := setupTest(t)
	f := newHolderSearchFixture(t, env)

	// The row really does disclose both, which is what makes searching them
	// legitimate — this test's premise, asserted rather than assumed.
	row := guestRow(t, holderRoster(t, env, f.sessionID, f.eventID, ""), f.accepted)
	if row.state != "accepted" || row.email != holderSearchAcceptedEmail || row.firstName != "Carla" {
		t.Fatalf("the accepted row reads %+v, want Carla Ruiz at %s — the fixture is not set up",
			row, holderSearchAcceptedEmail)
	}

	assertSearchFinds(t, holderSearch(t, env, f.sessionID, f.eventID, holderSearchAcceptedEmail, ""),
		[]string{f.accepted}, holderSearchAcceptedEmail)
	assertSearchFinds(t, holderSearch(t, env, f.sessionID, f.eventID, "Carla Ruiz", ""),
		[]string{f.accepted}, "Carla Ruiz")
	// The Holder's surname alone, to prove the name is matched as a substring of
	// the joined pair and not by equality on either half.
	assertSearchFinds(t, holderSearch(t, env, f.sessionID, f.eventID, "Rui", ""),
		[]string{f.accepted}, "Rui")
}

// THE LOAD-BEARING TEST OF THIS FEATURE. Read the whole comment before changing
// anything it protects.
//
// An address a buyer typed into a Ticket Assignment and whose owner never
// accepted is named NOWHERE on this platform (ADR 0047): it has no consent
// moment behind it, and the person may not know a ticket was bought for them.
// The address is nonetheless sitting in `tickets.holder_email`, one column away
// from the search predicate.
//
// SO A SEARCH FOR IT MUST RETURN ZERO ROWS, AND ZERO IN THE TOTAL. If it
// returned a row, the search box would answer — one address at a time — exactly
// the question the non-disclosure exists to refuse: type an address, get a row,
// and the empty Holder cell now means *yes, they are on this list*. The display
// rule would survive in the markup and die in the query.
//
// ADDING `holder_email` TO THE PREDICATE UNCONDITIONALLY IS A ONE-LINE CHANGE
// THAT PASSES REVIEW, BREAKS NOTHING VISIBLE, AND REOPENS ADR 0047. It is the
// single most plausible regression in this file. So is lifting the
// `tk.accepted_at IS NOT NULL` clause out of the Holder branch of
// repository.holderRosterSearch and into a condition beside it — which looks
// like a simplification and is a disclosure. THIS TEST IS WHAT NOTICES EITHER.
// If it fails, the answer is never to relax it.
//
// It asserts the Ticket IS on the unfiltered roster first, so it cannot pass by
// the fixture being empty or by the search finding nothing for some unrelated
// reason.
func TestSearchingAnUnacceptedHoldersAddressReturnsZeroRows(t *testing.T) {
	env := setupTest(t)
	f := newHolderSearchFixture(t, env)

	// The Ticket exists, is on the roster, and stands exactly where the rule is
	// about: an address was typed and nobody clicked it.
	whole := holderRoster(t, env, f.sessionID, f.eventID, "")
	row := guestRow(t, whole, f.assigned)
	if row.state != "assigned" {
		t.Fatalf("the Ticket under test reads state=%q, want `assigned` — the fixture is not set up", row.state)
	}
	// And the address is genuinely stored, or this test would be asserting that
	// a search finds nothing where there was nothing to find.
	if stored := readTicketAssignment(t, env, f.assigned); stored.holderEmail.String != holderSearchUnacceptedMail {
		t.Fatalf("the Ticket holds holder_email=%q, want %q stored and undisclosed",
			stored.holderEmail.String, holderSearchUnacceptedMail)
	}
	// The roster does not name it, which is the display rule this query rule is
	// the twin of.
	if row.email != "" || row.firstName != "" {
		t.Fatalf("the unaccepted row already discloses %q %q — ADR 0047 is broken before the search is",
			row.firstName, row.email)
	}

	assertNoRows(t, holderSearch(t, env, f.sessionID, f.eventID, holderSearchUnacceptedMail, ""),
		holderSearchUnacceptedMail,
		"An address a buyer typed and its owner NEVER ACCEPTED must match nothing (ADR 0047, ADR 0065).\n"+
			"A row here means the search box confirms, one address at a time, that a person is on this "+
			"Organization's roster — which is precisely the question the non-disclosure exists to refuse.\n"+
			"The `tk.accepted_at IS NOT NULL` clause belongs INSIDE the Holder branch of "+
			"repository.holderRosterSearch. Do not relax this test.")

	// Part of the address, too: a substring match must not be a way round the
	// rule that whole-address equality would have blocked.
	assertNoRows(t, holderSearch(t, env, f.sessionID, f.eventID, "diego", ""), "diego",
		"A FRAGMENT of an unaccepted address must not match either — the disclosure is the same one.")

	// And the row is still reachable the way ADR 0065 says it must be: by its
	// BUYER. This is the stated, accepted cost of the rule — the Organizer who
	// typed the address finds the Ticket through Ana, never through Diego — and
	// it is asserted here so the cost is visible beside the refusal rather than
	// being rediscovered as a bug.
	byBuyer := holderSearch(t, env, f.sessionID, f.eventID, "Ana Lopez", "")
	found := false
	for _, id := range holderTicketIDs(byBuyer) {
		if id == f.assigned {
			found = true
		}
	}
	if !found {
		t.Error("the unaccepted Ticket cannot be found by its BUYER either.\n" +
			"That is the route ADR 0065 leaves open in exchange for refusing the Holder's address; " +
			"without it the row is unreachable and the trade is not the one that was made.")
	}
}

// A PURGED ADDRESS RETURNS ZERO ROWS. It should follow from the rule above —
// the column is NULL once the purge has taken it, and a NULL never satisfies
// ILIKE — but a property that "should follow" is exactly the one to assert,
// because it would also follow from a predicate that had quietly stopped
// checking anything at all.
//
// This Ticket is on `never_accepted`: somebody was named, nobody claimed it, and
// the address is gone by definition (#334, migration 081).
func TestSearchingAPurgedHoldersAddressReturnsZeroRows(t *testing.T) {
	env := setupTest(t)
	f := newHolderSearchFixture(t, env)

	row := guestRow(t, holderRoster(t, env, f.sessionID, f.eventID, ""), f.neverAccepted)
	if !row.neverAccepted || row.email != "" {
		t.Fatalf("the purged row reads %+v, want the marker and no address — the fixture is not set up", row)
	}

	assertNoRows(t, holderSearch(t, env, f.sessionID, f.eventID, holderSearchPurgedEmail, ""),
		holderSearchPurgedEmail,
		"A purged address must match NOTHING: the retention purge took it, and a roster that could "+
			"still find it by typing it would be keeping the thing the purge exists to delete.")

	// The Ticket itself is still on the roster, and still selectable by the
	// filter that describes it — so this is a search that found nothing, not a
	// row that has fallen off the list.
	assertOneTicket(t, holderRoster(t, env, f.sessionID, f.eventID, "?assignment_state=never_accepted"),
		f.neverAccepted, "never_accepted")
}

// CASE-INSENSITIVE, AND THE LIKE METACHARACTERS ARE LITERALS — the Sales list's
// two properties, restated here because the two screens must not mean different
// things by "search".
//
// The wildcard half is the one with teeth: without likeEscape, `%` is "match
// everything" and `_` is "match any one character", so a reader who typed
// either would be handed the WHOLE ROSTER under a filter bar claiming to be
// narrowed — a screen that says it is showing a search result and is showing
// everybody.
func TestTheHolderListSearchIsCaseInsensitiveAndTreatsWildcardsAsLiterals(t *testing.T) {
	env := setupTest(t)
	f := newHolderSearchFixture(t, env)
	anaTickets := []string{f.unassigned, f.accepted, f.assigned, f.neverAccepted}

	assertSearchFinds(t, holderSearch(t, env, f.sessionID, f.eventID, "ANA LOPEZ", ""),
		anaTickets, "ANA LOPEZ")
	assertSearchFinds(t, holderSearch(t, env, f.sessionID, f.eventID, "cArLa@ExAmPlE.com", ""),
		[]string{f.accepted}, "cArLa@ExAmPlE.com")

	// Nothing in this fixture contains either character, so a literal match is
	// empty and an UNESCAPED one would return the whole roster of five.
	for _, wildcard := range []string{"%", "_", "%%", "a%z"} {
		assertNoRows(t, holderSearch(t, env, f.sessionID, f.eventID, wildcard, ""), wildcard,
			"A LIKE metacharacter typed into the box is a LITERAL, never a pattern (likeEscape).\n"+
				"Unescaped, this term matches the whole roster and the screen shows everybody under a "+
				"filter bar claiming to be narrowed.")
	}
}

// SEARCH COMPOSES WITH EVERY OTHER FILTER, and intersects with them rather than
// one of the two winning — which is what makes "the VIP door sales for somebody
// called Lopez" one query.
func TestTheHolderListSearchComposesWithTheOtherFilters(t *testing.T) {
	env := setupTest(t)
	f := newHolderSearchFixture(t, env)

	// With the assignment state, which is the pairing that matters most here:
	// the search and the state are the two halves of "where is this person's
	// ticket".
	assertSearchFinds(t, holderSearch(t, env, f.sessionID, f.eventID, "Ana Lopez", "&assignment_state=accepted"),
		[]string{f.accepted}, "Ana Lopez + accepted")
	assertSearchFinds(t, holderSearch(t, env, f.sessionID, f.eventID, "Ana Lopez", "&assignment_state=never_accepted"),
		[]string{f.neverAccepted}, "Ana Lopez + never_accepted")

	// With the Ticket Type: Ana bought GA, so the VIP roster holds none of her
	// Tickets and the intersection is empty rather than one filter winning.
	assertSearchFinds(t, holderSearch(t, env, f.sessionID, f.eventID, "Ana Lopez", "&ticket_type_id="+f.gaID),
		[]string{f.unassigned, f.accepted, f.assigned, f.neverAccepted}, "Ana Lopez + GA")
	assertNoRows(t, holderSearch(t, env, f.sessionID, f.eventID, "Ana Lopez", "&ticket_type_id="+f.vipID),
		"Ana Lopez + VIP", "The filters INTERSECT; neither wins.")

	// With the channel and with the date bounds. Every Ticket here came from one
	// Sale Import on a known day, so a matching bound leaves the search's answer
	// alone and a non-matching one empties it.
	assertSearchFinds(t, holderSearch(t, env, f.sessionID, f.eventID, "bruno@example.com", "&channel=import"),
		holderTicketIDs(holderSearch(t, env, f.sessionID, f.eventID, "bruno@example.com", "")),
		"bruno + import")
	assertNoRows(t, holderSearch(t, env, f.sessionID, f.eventID, "bruno@example.com", "&channel=online"),
		"bruno + online", "The filters INTERSECT; neither wins.")
	assertNoRows(t, holderSearch(t, env, f.sessionID, f.eventID, "Ana Lopez", "&sold_from=2026-07-02"),
		"Ana Lopez + sold on or after the 2nd",
		"Ana's sale is on the 1st; the date bound and the search intersect.")

	// AND COMPOSITION DOES NOT SMUGGLE THE UNACCEPTED ADDRESS BACK IN. A
	// narrower view is not a licence to disclose: `assignment_state=assigned`
	// selects exactly the Ticket whose address is withheld, and searching that
	// address still finds nothing.
	assertNoRows(t, holderSearch(t, env, f.sessionID, f.eventID, holderSearchUnacceptedMail,
		"&assignment_state=assigned"), holderSearchUnacceptedMail+" + assigned",
		"The disclosure rule is in the PREDICATE, so no combination of filters can widen it.")
}

// PAGING A SEARCHED VIEW IS STABLE: page 1 and page 2 of one search share no
// Ticket and lose none between them.
//
// The risk this covers is specific and would otherwise be invisible. The count
// and the page are two statements, and since #526 they must agree about the
// JOIN they carry as well as the WHERE — a COUNT without the Holder join would
// error on the search predicate's name columns, and a page whose ORDER BY had
// no deterministic tiebreak would duplicate and drop rows across a page
// boundary. A roster that loses a person is worse than one that is badly
// ordered.
func TestPagingASearchedHolderListIsStable(t *testing.T) {
	env := setupTest(t)
	f := newHolderSearchFixture(t, env)

	first := holderSearch(t, env, f.sessionID, f.eventID, "Ana Lopez", "&page_size=2")
	second := holderSearch(t, env, f.sessionID, f.eventID, "Ana Lopez", "&page_size=2&page=2")

	if first.Pagination.Total != 4 || second.Pagination.Total != 4 {
		t.Fatalf("the searched view totals %d then %d, want 4 on both — the total describes the VIEW "+
			"and does not move as the reader pages through it",
			first.Pagination.Total, second.Pagination.Total)
	}
	if len(first.Data) != 2 || len(second.Data) != 2 {
		t.Fatalf("pages hold %d and %d rows, want 2 and 2", len(first.Data), len(second.Data))
	}

	seen := map[string]bool{}
	for _, id := range append(holderTicketIDs(first), holderTicketIDs(second)...) {
		if seen[id] {
			t.Errorf("Ticket %s appears on both pages of one searched view — a paginated list that "+
				"duplicates a row is one that has also dropped another", id)
		}
		seen[id] = true
	}
	for _, id := range []string{f.unassigned, f.accepted, f.assigned, f.neverAccepted} {
		if !seen[id] {
			t.Errorf("Ticket %s is on neither page of a search that matched it — the roster lost a person", id)
		}
	}
}

// SEARCH BELONGS TO NO FEATURE FLAG, and is the only filter on this list that
// does not: a buyer's name, a buyer's address and a Sale Confirmation reference
// are on every roster of every build.
//
// So on a PLAIN ROSTER with Ticket Assignment dark the box still works — read
// through the questions flag, because with both dark the route is a 404 (#333).
// And the Holder branch of the predicate simply never matches there, because on
// such a build nobody has ever accepted anything: there is no second rule for
// the dark case, and no flag is read anywhere on the search path.
func TestTheHolderListSearchNeedsNoFeatureFlag(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	f := newHolderFilterFixture(t, env)
	enableTicketQuestions(t)
	closeTicketAssignment(t)

	// The buyer is still found, by address and by name.
	page := holderSearch(t, env, f.sessionID, f.eventID, "ga-import@example.com", "")
	if page.Pagination.Total != 1 || len(page.Data) != 1 ||
		page.Data[0].CustomerEmail != "ga-import@example.com" {
		t.Fatalf("searching a buyer on a build with Ticket Assignment dark found %d rows (total %d), "+
			"want the one Ticket — `q` belongs to no flag", len(page.Data), page.Pagination.Total)
	}
	if got := holderSearch(t, env, f.sessionID, f.eventID, "Vip Online", "").Pagination.Total; got != 1 {
		t.Errorf("searching a buyer's joined name on a dark build found %d Tickets, want 1", got)
	}
	// And nothing about a Holder is reachable, because there are no Holders:
	// the roster carries no assignment at all on this fixture.
	assertNoRows(t, holderSearch(t, env, f.sessionID, f.eventID, "carla@example.com", ""),
		"carla@example.com", "Nobody has accepted anything on this roster.")
}
