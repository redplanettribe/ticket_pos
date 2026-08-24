package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The Answer: what one Ticket says in reply to one Ticket Question (#310).
//
// These run through the staff API rather than through SQL, because unlike #308's
// Tickets — which nothing exposed — an Answer is reachable, and the things worth
// asserting are what a caller is allowed to say and when.

// ticketAnswers decodes one Ticket with its questions and Answers.
type ticketAnswers struct {
	TicketID          string `json:"ticket_id"`
	Ordinal           int    `json:"ordinal"`
	TicketTypeID      string `json:"ticket_type_id"`
	TicketTypeName    string `json:"ticket_type_name"`
	TicketSaleID      string `json:"ticket_sale_id"`
	ConfirmationRef   string `json:"confirmation_ref"`
	Answerable        bool   `json:"answerable"`
	AnswerableRefusal string `json:"answerable_refusal"`
	Questions         []struct {
		Question ticketQuestion `json:"question"`
		Answer   *struct {
			Text    *string `json:"text"`
			Number  *string `json:"number"`
			Date    *string `json:"date"`
			Checked *bool   `json:"checked"`
			Options []struct {
				OptionID     string `json:"option_id"`
				Label        string `json:"label"`
				CurrentLabel string `json:"current_label"`
				Retired      bool   `json:"retired"`
			} `json:"options"`
			UpdatedAt time.Time `json:"updated_at"`
		} `json:"answer"`
	} `json:"questions"`
}

func ticketSaleTicketsPath(eventID, ticketSaleID string) string {
	return "/api/v1/staff/events/" + eventID + "/ticket-sales/" + ticketSaleID + "/tickets"
}

func ticketPath(eventID, ticketID string) string {
	return "/api/v1/staff/events/" + eventID + "/tickets/" + ticketID
}

func answerPath(eventID, ticketID, questionID string) string {
	return ticketPath(eventID, ticketID) + "/answers/" + questionID
}

func decodeTicketAnswers(t *testing.T, data json.RawMessage) ticketAnswers {
	t.Helper()
	var ticket ticketAnswers
	if err := json.Unmarshal(data, &ticket); err != nil {
		t.Fatalf("decode ticket answers: %v", err)
	}
	return ticket
}

func decodeTicketAnswersList(t *testing.T, data json.RawMessage) []ticketAnswers {
	t.Helper()
	var tickets []ticketAnswers
	if err := json.Unmarshal(data, &tickets); err != nil {
		t.Fatalf("decode ticket answers list: %v", err)
	}
	return tickets
}

// answerDetail reads one field out of a refusal's details. The details are how
// an INVALID_ANSWER says WHICH question and WHAT about it, so a form can point
// at the offending field rather than at the form.
func answerDetail(body envelope, key string) string {
	details, _ := body.Error.Details.(map[string]any)
	value, _ := details[key].(string)
	return value
}

// answeredFixture is a sold Ticket Type with Tickets on it: an Event, a Ticket
// Type, a two-ticket Sale Import, and the Org Admin session that may reach them.
//
// The `import` channel because it is the shortest route to a recorded Ticket
// Sale — a Sale Import commits through the same spine an Online Sale does and
// mints Tickets in the same transaction (ADR 0043), so what these tests are
// about is unaffected by which channel put the rows there.
func answeredFixture(t *testing.T, env *testEnv) (sessionID, eventID, ticketTypeID, ticketSaleID, batchID string, ticketIDs []string) {
	t.Helper()
	sessionID = orgAdminSession(t, env)
	eventID = createDraftEvent(t, env, sessionID, "Answer Fest", "answer-fest")
	ticketTypeID = createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2000, 50)
	batchID = commitBatch(t, env, sessionID, eventID, "batch-answers", []map[string]any{{
		"customer_email":      "ana@example.com",
		"customer_first_name": "Ana",
		"customer_last_name":  "Lopez",
		"ticket_type_id":      ticketTypeID,
		"quantity":            2,
		"payment_method":      "cash",
		"sold_at":             "2026-07-01T10:00:00Z",
	}})

	if err := env.db.QueryRow(`
		SELECT s.id FROM ticket_sales s WHERE s.event_id = $1
	`, eventID).Scan(&ticketSaleID); err != nil {
		t.Fatalf("read Ticket Sale: %v", err)
	}
	rows, err := env.db.Query(`
		SELECT tk.id
		FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		WHERE l.ticket_sale_id = $1
		ORDER BY tk.ordinal ASC
	`, ticketSaleID)
	if err != nil {
		t.Fatalf("read Tickets: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan Ticket: %v", err)
		}
		ticketIDs = append(ticketIDs, id)
	}
	if len(ticketIDs) != 2 {
		t.Fatalf("Tickets minted = %d, want 2", len(ticketIDs))
	}
	return sessionID, eventID, ticketTypeID, ticketSaleID, batchID, ticketIDs
}

// putAnswer writes an Answer and returns the Ticket, failing on any refusal —
// for tests whose subject is something further along.
func putAnswer(t *testing.T, env *testEnv, sessionID, eventID, ticketID, questionID string, body map[string]any) ticketAnswers {
	t.Helper()
	resp, envelope := env.put(t, answerPath(eventID, ticketID, questionID), body, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("answer status=%d error=%+v", resp.StatusCode, envelope.Error)
	}
	return decodeTicketAnswers(t, envelope.Data)
}

// answerFor picks one question's Answer out of a Ticket payload.
func answerFor(t *testing.T, ticket ticketAnswers, questionID string) *struct {
	Text    *string `json:"text"`
	Number  *string `json:"number"`
	Date    *string `json:"date"`
	Checked *bool   `json:"checked"`
	Options []struct {
		OptionID     string `json:"option_id"`
		Label        string `json:"label"`
		CurrentLabel string `json:"current_label"`
		Retired      bool   `json:"retired"`
	} `json:"options"`
	UpdatedAt time.Time `json:"updated_at"`
} {
	t.Helper()
	for _, pair := range ticket.Questions {
		if pair.Question.ID == questionID {
			return pair.Answer
		}
	}
	t.Fatalf("question %s is not on the Ticket payload", questionID)
	return nil
}

// Nothing about the Answer is reachable while the flag is off — the same
// property ADR 0045 asserts for question authoring, at the only place it can be
// asserted. Every verb answers exactly as a build without the feature would.
func TestTicketAnswersAreInvisibleWhileTheFlagIsOff(t *testing.T) {
	env := setupTest(t)
	// The fixture runs with the flag off throughout: Tickets are minted for
	// every sale whether or not any Ticket Question exists (ADR 0043), so the
	// rows are there to be asked about and the API still must not admit it.
	sessionID, eventID, _, ticketSaleID, _, ticketIDs := answeredFixture(t, env)
	someQuestion := "11111111-1111-4111-8111-111111111111"

	for _, tc := range []struct {
		name string
		call func() (*http.Response, envelope)
	}{
		{"list a sale's tickets", func() (*http.Response, envelope) {
			return env.get(t, ticketSaleTicketsPath(eventID, ticketSaleID), authHeader(sessionID))
		}},
		{"get a ticket", func() (*http.Response, envelope) {
			return env.get(t, ticketPath(eventID, ticketIDs[0]), authHeader(sessionID))
		}},
		{"answer", func() (*http.Response, envelope) {
			return env.put(t, answerPath(eventID, ticketIDs[0], someQuestion),
				map[string]any{"text": "M"}, authHeader(sessionID))
		}},
		{"remove an answer", func() (*http.Response, envelope) {
			return env.deleteJSON(t, answerPath(eventID, ticketIDs[0], someQuestion), nil, authHeader(sessionID))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := tc.call()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status=%d, want 404 while the flag is off; error=%+v", resp.StatusCode, body.Error)
			}
			if body.Error == nil || body.Error.Code != "TICKET_QUESTIONS_UNAVAILABLE" {
				t.Fatalf("error=%+v, want TICKET_QUESTIONS_UNAVAILABLE", body.Error)
			}
		})
	}
}

// Every one of the seven kinds is answered, re-answered, and read back in the
// shape its kind takes — a number as a number and a date as a date.
func TestEventStaffAnswerEverySevenKindsAndCorrectThem(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, ticketIDs := answeredFixture(t, env)
	ticketID := ticketIDs[0]

	for _, tc := range []struct {
		kind    string
		options []string
		// first and second are the Answer and the correction. The Option cases
		// fill option_ids in the body from the created question's Options.
		first, second map[string]any
		firstOptions  []int
		secondOptions []int
		read          func(t *testing.T, answer map[string]any)
	}{
		{kind: "short_text",
			first:  map[string]any{"text": "  Medium  "},
			second: map[string]any{"text": "Large"}},
		{kind: "long_text",
			first:  map[string]any{"text": "Coeliac"},
			second: map[string]any{"text": "Coeliac, and no shellfish"}},
		{kind: "number",
			first:  map[string]any{"number": "3"},
			second: map[string]any{"number": "4.50"}},
		{kind: "date",
			first:  map[string]any{"date": "2026-09-01"},
			second: map[string]any{"date": "2026-09-02"}},
		{kind: "checkbox",
			first:  map[string]any{"checked": true},
			second: map[string]any{"checked": false}},
		{kind: "single_choice", options: []string{"S", "M", "L"},
			firstOptions: []int{0}, secondOptions: []int{2}},
		{kind: "multi_choice", options: []string{"Vegetarian", "Vegan", "Nuts"},
			firstOptions: []int{0}, secondOptions: []int{1, 2}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			body := map[string]any{"label": "Question " + tc.kind, "kind": tc.kind}
			if tc.options != nil {
				body["option_labels"] = tc.options
			}
			question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, body)

			first := tc.first
			if tc.firstOptions != nil {
				first = map[string]any{"option_ids": optionIDsAt(question, tc.firstOptions)}
			}
			ticket := putAnswer(t, env, sessionID, eventID, ticketID, question.ID, first)
			answer := answerFor(t, ticket, question.ID)
			if answer == nil {
				t.Fatal("the Answer did not come back on the Ticket")
			}

			switch tc.kind {
			case "short_text":
				// Trimmed and otherwise kept as written: this is somebody's own
				// words about themselves.
				if answer.Text == nil || *answer.Text != "Medium" {
					t.Fatalf("text=%v, want Medium", answer.Text)
				}
			case "number":
				// Read back as the digits that were typed. A NUMERIC column and
				// no float in the path, so nothing rounds.
				if answer.Number == nil || *answer.Number != "3" {
					t.Fatalf("number=%v, want 3", answer.Number)
				}
			case "date":
				if answer.Date == nil || *answer.Date != "2026-09-01" {
					t.Fatalf("date=%v, want 2026-09-01", answer.Date)
				}
			case "checkbox":
				if answer.Checked == nil || !*answer.Checked {
					t.Fatalf("checked=%v, want true", answer.Checked)
				}
			case "single_choice", "multi_choice":
				if len(answer.Options) != len(tc.firstOptions) {
					t.Fatalf("options=%d, want %d", len(answer.Options), len(tc.firstOptions))
				}
			}

			// The correction: the same request with a different body. Nothing
			// needs a second verb, and no version of the old Answer is kept.
			second := tc.second
			if tc.secondOptions != nil {
				second = map[string]any{"option_ids": optionIDsAt(question, tc.secondOptions)}
			}
			ticket = putAnswer(t, env, sessionID, eventID, ticketID, question.ID, second)
			corrected := answerFor(t, ticket, question.ID)

			switch tc.kind {
			case "short_text":
				if corrected.Text == nil || *corrected.Text != "Large" {
					t.Fatalf("corrected text=%v, want Large", corrected.Text)
				}
			case "number":
				// The trailing zero survives: a number question asking a price
				// or a measurement means it.
				if corrected.Number == nil || *corrected.Number != "4.50" {
					t.Fatalf("corrected number=%v, want 4.50", corrected.Number)
				}
			case "checkbox":
				// FALSE IS AN ANSWER. Somebody who read the question and said no
				// has said something, and it is not the same as never having
				// been asked.
				if corrected.Checked == nil || *corrected.Checked {
					t.Fatalf("corrected checked=%v, want a present false", corrected.Checked)
				}
			case "multi_choice":
				// SEVERAL OPTIONS IN ONE ANSWER — the property this kind exists
				// for.
				if len(corrected.Options) != 2 {
					t.Fatalf("corrected options=%d, want 2", len(corrected.Options))
				}
			case "single_choice":
				if len(corrected.Options) != 1 || corrected.Options[0].Label != "L" {
					t.Fatalf("corrected options=%+v, want the one L", corrected.Options)
				}
			}
		})
	}
}

// optionIDsAt reads the ids of a created question's Options by position.
func optionIDsAt(question ticketQuestion, positions []int) []string {
	ids := make([]string, 0, len(positions))
	for _, position := range positions {
		ids = append(ids, question.Options[position].ID)
	}
	return ids
}

// An Answer belongs to ONE TICKET. Two Tickets on one Ticket Sale Line answer
// the same question differently, which is the entire reason ADR 0043 made a
// Ticket a row: a buyer of four is not assumed to know four people's sizes.
func TestAnswersBelongToTheTicketAndNotTheSale(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, ticketSaleID, _, ticketIDs := answeredFixture(t, env)

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text",
	})

	putAnswer(t, env, sessionID, eventID, ticketIDs[0], question.ID, map[string]any{"text": "S"})
	putAnswer(t, env, sessionID, eventID, ticketIDs[1], question.ID, map[string]any{"text": "XL"})

	resp, body := env.get(t, ticketSaleTicketsPath(eventID, ticketSaleID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	tickets := decodeTicketAnswersList(t, body.Data)
	if len(tickets) != 2 {
		t.Fatalf("tickets=%d, want the sale's two", len(tickets))
	}
	if tickets[0].Ordinal != 1 || tickets[1].Ordinal != 2 {
		t.Fatalf("ordinals=%d,%d — the order two Tickets of one line are told apart by", tickets[0].Ordinal, tickets[1].Ordinal)
	}

	first := answerFor(t, tickets[0], question.ID)
	second := answerFor(t, tickets[1], question.ID)
	if first == nil || second == nil {
		t.Fatal("one of the two Tickets lost its Answer")
	}
	if *first.Text != "S" || *second.Text != "XL" {
		t.Fatalf("answers=%q and %q, want S and XL — one Answer per Ticket", *first.Text, *second.Text)
	}
}

// A choice Answer records WHICH Option was picked and THE WORDS IT SHOWED AT THE
// TIME. Renaming the Option afterwards moves the current label and leaves the
// snapshot exactly where it was.
func TestRenamingAnOptionLeavesTheAnswersSnapshotIntact(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, ticketIDs := answeredFixture(t, env)

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "Main course", "kind": "single_choice",
		"option_labels": []string{"Chicken", "Fish"},
	})
	chickenID := question.Options[0].ID

	ticket := putAnswer(t, env, sessionID, eventID, ticketIDs[0], question.ID,
		map[string]any{"option_ids": []string{chickenID}})
	if answer := answerFor(t, ticket, question.ID); answer.Options[0].Label != "Chicken" {
		t.Fatalf("snapshot=%q, want the words that were shown", answer.Options[0].Label)
	}

	// The wording is corrected months later (by SQL: no route renames an
	// approved Option since #408, and the subject here is the snapshot).
	correctOptionLabel(t, env, chickenID, "Chicken (halal)")

	resp, body := env.get(t, ticketPath(eventID, ticketIDs[0]), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get ticket status=%d error=%+v", resp.StatusCode, body.Error)
	}
	answer := answerFor(t, decodeTicketAnswers(t, body.Data), question.ID)
	if len(answer.Options) != 1 {
		t.Fatalf("options=%d, want the one still attached", len(answer.Options))
	}
	// The IDENTITY did not move, which is what kept the Answer attached at all.
	if answer.Options[0].OptionID != chickenID {
		t.Fatal("the rename forked the Answer off its Option")
	}
	// The snapshot is what the person READ; the current label is what every list
	// and the export header show now. Both are true, about different moments.
	if answer.Options[0].Label != "Chicken" {
		t.Fatalf("snapshot=%q — a rename rewrote what somebody read", answer.Options[0].Label)
	}
	if answer.Options[0].CurrentLabel != "Chicken (halal)" {
		t.Fatalf("current label=%q, want the correction", answer.Options[0].CurrentLabel)
	}
}

// An Answer against a RETIRED Option persists and stays readable. What a retired
// Option may not do is be chosen afresh — it has left every new list — while one
// this Answer had already chosen may be kept.
func TestAnswersAgainstARetiredOptionPersistAndStayReadable(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, ticketIDs := answeredFixture(t, env)
	ticketID := ticketIDs[0]

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "Dietary", "kind": "multi_choice",
		"option_labels": []string{"Vegetarian", "Vegan", "Nuts"},
	})
	vegetarianID := question.Options[0].ID
	veganID := question.Options[1].ID
	nutsID := question.Options[2].ID

	putAnswer(t, env, sessionID, eventID, ticketID, question.ID,
		map[string]any{"option_ids": []string{vegetarianID}})

	resp, body := env.deleteJSON(t, optionPath(eventID, ticketTypeID, question.ID, vegetarianID), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retire option status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// Still there, still readable, and marked as retired so a reader knows the
	// Option has left the list rather than wondering why it is not offered.
	resp, body = env.get(t, ticketPath(eventID, ticketID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get ticket status=%d error=%+v", resp.StatusCode, body.Error)
	}
	answer := answerFor(t, decodeTicketAnswers(t, body.Data), question.ID)
	if len(answer.Options) != 1 || answer.Options[0].OptionID != vegetarianID {
		t.Fatalf("options=%+v — retiring an Option took the Answer with it", answer.Options)
	}
	if answer.Options[0].Label != "Vegetarian" || !answer.Options[0].Retired {
		t.Fatalf("option=%+v, want the snapshot kept and the Option marked retired", answer.Options[0])
	}

	// Ticking one more box does not lose the retired one that was already
	// chosen: "kept on the Tickets that chose it" survives a correction.
	ticket := putAnswer(t, env, sessionID, eventID, ticketID, question.ID,
		map[string]any{"option_ids": []string{vegetarianID, nutsID}})
	if kept := answerFor(t, ticket, question.ID); len(kept.Options) != 2 {
		t.Fatalf("options=%+v, want the retired one kept alongside the new one", kept.Options)
	}

	// But a retired Option that this Answer had NOT already chosen is not on
	// offer, so choosing one afresh is refused.
	resp, body = env.deleteJSON(t, optionPath(eventID, ticketTypeID, question.ID, veganID), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retire second option status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body = env.put(t, answerPath(eventID, ticketID, question.ID),
		map[string]any{"option_ids": []string{veganID}}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 choosing a retired Option afresh; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "ANSWER_OPTION_NOT_OFFERED" {
		t.Fatalf("error=%+v, want ANSWER_OPTION_NOT_OFFERED", body.Error)
	}
}

// Each kind refuses what does not fit it, and refuses rather than coercing.
func TestAnswerValidationPerKind(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, ticketIDs := answeredFixture(t, env)
	ticketID := ticketIDs[0]

	kinds := map[string]ticketQuestion{}
	for _, spec := range []struct {
		kind    string
		options []string
	}{
		{kind: "short_text"},
		{kind: "long_text"},
		{kind: "number"},
		{kind: "date"},
		{kind: "checkbox"},
		{kind: "single_choice", options: []string{"S", "M"}},
		{kind: "multi_choice", options: []string{"Vegetarian", "Vegan"}},
	} {
		body := map[string]any{"label": "Q " + spec.kind, "kind": spec.kind}
		if spec.options != nil {
			body["option_labels"] = spec.options
		}
		kinds[spec.kind] = createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, body)
	}

	for _, tc := range []struct {
		name    string
		kind    string
		body    map[string]any
		problem string
	}{
		{"text to a number question", "number", map[string]any{"text": "three"}, "wrong_shape"},
		{"a number that is not one", "number", map[string]any{"number": "three"}, "not_a_number"},
		{"a date that never happened", "date", map[string]any{"date": "2026-02-30"}, "not_a_date"},
		{"an instant to a date question", "date", map[string]any{"date": "2026-09-01T18:00:00Z"}, "not_a_date"},
		{"text to a checkbox", "checkbox", map[string]any{"text": "yes"}, "wrong_shape"},
		{"text to a choice question", "single_choice", map[string]any{"text": "M"}, "wrong_shape"},
		{"options to a text question", "short_text", map[string]any{"option_ids": []string{"x"}}, "wrong_shape"},
		{"nothing at all", "short_text", map[string]any{}, "missing"},
		{"a blank answer", "short_text", map[string]any{"text": "   "}, "missing"},
		{"no option chosen", "single_choice", map[string]any{"option_ids": []string{}}, "missing"},
		{"an overlong short text", "short_text", map[string]any{"text": strings.Repeat("a", 201)}, "too_long"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := env.put(t, answerPath(eventID, ticketID, kinds[tc.kind].ID), tc.body, authHeader(sessionID))
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status=%d, want 400; error=%+v", resp.StatusCode, body.Error)
			}
			if body.Error == nil || body.Error.Code != "INVALID_ANSWER" {
				t.Fatalf("error=%+v, want INVALID_ANSWER", body.Error)
			}
			// The refusal names the kind and the problem so a form can point at
			// the field rather than at the form.
			if got := answerDetail(body, "problem"); got != tc.problem {
				t.Fatalf("problem=%q, want %q (details=%+v)", got, tc.problem, body.Error.Details)
			}
			if got := answerDetail(body, "kind"); got != tc.kind {
				t.Fatalf("kind=%q, want %q", got, tc.kind)
			}
		})
	}

	// A single_choice question takes ONE Option; two is a different question.
	single := kinds["single_choice"]
	resp, body := env.put(t, answerPath(eventID, ticketID, single.ID),
		map[string]any{"option_ids": []string{single.Options[0].ID, single.Options[1].ID}}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 on two Options for a single choice; error=%+v", resp.StatusCode, body.Error)
	}
	if got := answerDetail(body, "problem"); got != "one_option_only" {
		t.Fatalf("problem=%q, want one_option_only", got)
	}

	// An Option belonging to another question is not one this question offers.
	multi := kinds["multi_choice"]
	resp, body = env.put(t, answerPath(eventID, ticketID, single.ID),
		map[string]any{"option_ids": []string{multi.Options[0].ID}}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "ANSWER_OPTION_NOT_OFFERED" {
		t.Fatalf("status=%d error=%+v, want 400 ANSWER_OPTION_NOT_OFFERED", resp.StatusCode, body.Error)
	}
}

// Answering is refused once the Event has started, and everything already
// answered stays readable — a closed window freezes writing, it does not hide.
func TestAnsweringIsRefusedOnceTheEventHasStarted(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, ticketIDs := answeredFixture(t, env)
	ticketID := ticketIDs[0]

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text",
	})
	putAnswer(t, env, sessionID, eventID, ticketID, question.ID, map[string]any{"text": "M"})

	// The doors open. `starts_at` is an instant with the Event's timezone
	// already baked into it, which is why this is a comparison and not a
	// conversion — see catalog.AnswerWindow.
	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		eventID, env.fixedClock.Add(-time.Hour)); err != nil {
		t.Fatalf("set the Event's start: %v", err)
	}

	resp, body := env.put(t, answerPath(eventID, ticketID, question.ID),
		map[string]any{"text": "L"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409 after the doors open; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_STARTED_ANSWERS_CLOSED" {
		t.Fatalf("error=%+v, want EVENT_STARTED_ANSWERS_CLOSED", body.Error)
	}

	// Removing one is refused on the same terms: what is recorded stays.
	resp, body = env.deleteJSON(t, answerPath(eventID, ticketID, question.ID), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict || body.Error.Code != "EVENT_STARTED_ANSWERS_CLOSED" {
		t.Fatalf("delete status=%d error=%+v, want 409 EVENT_STARTED_ANSWERS_CLOSED", resp.StatusCode, body.Error)
	}

	// And the Answer is still readable, with the payload saying why it is frozen
	// rather than leaving somebody to wonder why the form will not take.
	resp, body = env.get(t, ticketPath(eventID, ticketID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d error=%+v", resp.StatusCode, body.Error)
	}
	ticket := decodeTicketAnswers(t, body.Data)
	if ticket.Answerable || ticket.AnswerableRefusal != "event_started" {
		t.Fatalf("answerable=%v refusal=%q, want a frozen Ticket naming the doors", ticket.Answerable, ticket.AnswerableRefusal)
	}
	if answer := answerFor(t, ticket, question.ID); answer == nil || *answer.Text != "M" {
		t.Fatalf("answer=%+v — a started Event lost what was answered before it", answer)
	}
}

// Answering is refused on a Ticket whose Ticket Sale is reversed, and everything
// already answered stays readable: a Sale Reversal voids a sale, it does not
// unmint its Tickets or erase what they said.
func TestAnsweringIsRefusedOnAReversedTicketSale(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, ticketSaleID, batchID, ticketIDs := answeredFixture(t, env)
	ticketID := ticketIDs[0]

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text",
	})
	putAnswer(t, env, sessionID, eventID, ticketID, question.ID, map[string]any{"text": "M"})

	undoBatch(t, env, sessionID, eventID, batchID)

	resp, body := env.put(t, answerPath(eventID, ticketID, question.ID),
		map[string]any{"text": "L"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409 on a reversed Sale; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "TICKET_SALE_REVERSED" {
		t.Fatalf("error=%+v, want TICKET_SALE_REVERSED", body.Error)
	}

	// The sale's Tickets are still listed and still carry their Answers. A
	// reversed sale that vanished from this surface would look as though the
	// Answers had been destroyed, and they have not been.
	resp, body = env.get(t, ticketSaleTicketsPath(eventID, ticketSaleID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	tickets := decodeTicketAnswersList(t, body.Data)
	if len(tickets) != 2 {
		t.Fatalf("tickets=%d — a Sale Reversal unminted Tickets", len(tickets))
	}
	if tickets[0].Answerable || tickets[0].AnswerableRefusal != "sale_reversed" {
		t.Fatalf("answerable=%v refusal=%q, want a frozen Ticket naming the Reversal", tickets[0].Answerable, tickets[0].AnswerableRefusal)
	}
	if answer := answerFor(t, tickets[0], question.ID); answer == nil || *answer.Text != "M" {
		t.Fatalf("answer=%+v — a Sale Reversal erased an Answer", answer)
	}
}

// An Answer records WHEN it last changed and nothing else: no version history,
// and a re-submission of the same value is not a change.
func TestAnswerRecordsWhenItLastChangedAndKeepsNoHistory(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, ticketIDs := answeredFixture(t, env)
	ticketID := ticketIDs[0]

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text",
	})

	first := answerFor(t, putAnswer(t, env, sessionID, eventID, ticketID, question.ID,
		map[string]any{"text": "M"}), question.ID)

	// The clock moves, and the same Answer is sent again. updated_at means "when
	// this Answer last CHANGED", so a form saved twice must not move it.
	later := env.fixedClock.Add(time.Hour)
	sharedApp.CatalogService.WithClock(func() time.Time { return later })

	unchanged := answerFor(t, putAnswer(t, env, sessionID, eventID, ticketID, question.ID,
		map[string]any{"text": "M"}), question.ID)
	if !unchanged.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatalf("updated_at moved on a re-submission of the same Answer: %v then %v", first.UpdatedAt, unchanged.UpdatedAt)
	}

	changed := answerFor(t, putAnswer(t, env, sessionID, eventID, ticketID, question.ID,
		map[string]any{"text": "L"}), question.ID)
	if !changed.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("updated_at did not move on a correction: %v then %v", first.UpdatedAt, changed.UpdatedAt)
	}

	// NO VERSION HISTORY. One row per (Ticket, question), whatever was said
	// before, and no sibling table holding the old value or its author.
	var rows int
	if err := env.db.QueryRow(`
		SELECT COUNT(*) FROM ticket_answers WHERE ticket_id = $1 AND ticket_question_id = $2
	`, ticketID, question.ID).Scan(&rows); err != nil {
		t.Fatalf("count Answers: %v", err)
	}
	if rows != 1 {
		t.Fatalf("rows=%d, want exactly one Answer per (Ticket, Ticket Question)", rows)
	}
}

// The Answer can be taken away, which is the only way to say "not said" — a
// blank Answer would be a row no later reader could tell from a real reply.
func TestAnAnswerCanBeRemoved(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, ticketIDs := answeredFixture(t, env)
	ticketID := ticketIDs[0]

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "Attending the dinner?", "kind": "checkbox", "required": true,
	})
	putAnswer(t, env, sessionID, eventID, ticketID, question.ID, map[string]any{"checked": true})

	resp, body := env.deleteJSON(t, answerPath(eventID, ticketID, question.ID), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if answer := answerFor(t, decodeTicketAnswers(t, body.Data), question.ID); answer != nil {
		t.Fatalf("answer=%+v, want null — the question is an Outstanding Answer again", answer)
	}

	// And a second removal is a 404 rather than a silent success.
	resp, body = env.deleteJSON(t, answerPath(eventID, ticketID, question.ID), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusNotFound || body.Error.Code != "ANSWER_NOT_FOUND" {
		t.Fatalf("status=%d error=%+v, want 404 ANSWER_NOT_FOUND", resp.StatusCode, body.Error)
	}
}

// THE FROZEN BRANCH OF THE KIND FREEZE, reachable for the first time now that
// something can answer.
//
// A `single_choice` whose Answers are Option identities does not become a `date`
// by relabelling: the stored Answers ARE the kind. The permissive branch — the
// kind changing freely while nothing has answered — is covered by
// TestTicketQuestionKindChangesWhileNothingHasAnswered, and until this ticket
// that was the only branch any test could reach.
//
// Since #408 (ADR 0056) the approval that lets a question be answered is what
// the PATCH refuses on first — TICKET_QUESTION_APPROVED_IMMUTABLE — so over
// HTTP the kind freeze stands behind the immutability rule. What this test
// keeps is the fact underneath: an answered question's kind does not change.
func TestTicketQuestionKindIsFrozenOnceATicketHasAnswered(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, ticketIDs := answeredFixture(t, env)

	question := draftTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "How many guests?", "kind": "short_text",
	})

	// Free to change while nothing has answered.
	resp, body := env.patch(t, questionPath(eventID, ticketTypeID, question.ID),
		map[string]any{"label": "How many guests?", "kind": "number"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want the kind free before any Answer; error=%+v", resp.StatusCode, body.Error)
	}
	approveTicketQuestion(t, env, question.ID)

	putAnswer(t, env, sessionID, eventID, ticketIDs[0], question.ID, map[string]any{"number": "3"})

	resp, body = env.patch(t, questionPath(eventID, ticketTypeID, question.ID),
		map[string]any{"label": "How many guests?", "kind": "date"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409 once a Ticket has answered; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "TICKET_QUESTION_APPROVED_IMMUTABLE" {
		t.Fatalf("error=%+v, want TICKET_QUESTION_APPROVED_IMMUTABLE", body.Error)
	}

	// Restating the same kind is not a change, and narrowing is always open:
	// the same body with `required` unchanged is accepted.
	resp, body = env.patch(t, questionPath(eventID, ticketTypeID, question.ID),
		map[string]any{"label": "How many guests?", "kind": "number", "required": false}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want a no-op restatement accepted; error=%+v", resp.StatusCode, body.Error)
	}

	// The kind freeze reads Answers on REVERSED Sales too: those Tickets keep
	// their Answers and those Answers are still stored in this kind.
	// (Asserted here rather than in its own test because the fixture is the same
	// one and the fact is about this guard.)
	var answered bool
	if err := env.db.QueryRow(`
		SELECT EXISTS (SELECT 1 FROM ticket_answers WHERE ticket_question_id = $1)
	`, question.ID).Scan(&answered); err != nil {
		t.Fatalf("read Answers: %v", err)
	}
	if !answered {
		t.Fatal("the Answer that froze the kind is not in the table")
	}
}

// A retired Ticket Question is kept so that what has already been answered still
// reads; it is not something new can be said about.
func TestARetiredTicketQuestionCannotBeAnswered(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, ticketIDs := answeredFixture(t, env)
	ticketID := ticketIDs[0]

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "Old question", "kind": "short_text",
	})
	putAnswer(t, env, sessionID, eventID, ticketID, question.ID, map[string]any{"text": "M"})

	resp, body := env.deleteJSON(t, questionPath(eventID, ticketTypeID, question.ID), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retire question status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.put(t, answerPath(eventID, ticketID, question.ID),
		map[string]any{"text": "L"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict || body.Error.Code != "TICKET_QUESTION_RETIRED" {
		t.Fatalf("status=%d error=%+v, want 409 TICKET_QUESTION_RETIRED", resp.StatusCode, body.Error)
	}

	// And what was answered under it still reads, which is why it was retired
	// rather than deleted.
	resp, body = env.get(t, ticketPath(eventID, ticketID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if answer := answerFor(t, decodeTicketAnswers(t, body.Data), question.ID); answer == nil || *answer.Text != "M" {
		t.Fatalf("answer=%+v — retiring a question took its Answers", answer)
	}
}

// One Organization's Ticket id must not resolve under another's Event, and a
// Member hired for the door is refused every verb — the same gate the Ticket
// Question authoring routes use.
func TestTicketAnswersAreScopedAndGated(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, ticketSaleID, _, ticketIDs := answeredFixture(t, env)
	ticketID := ticketIDs[0]

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text",
	})

	otherSession := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSession, "Other Org", "other-org")
	resp, body := env.get(t, ticketPath(eventID, ticketID), authHeader(otherSession))
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

	for _, tc := range []struct {
		name string
		call func() (*http.Response, envelope)
	}{
		{"list a sale's tickets", func() (*http.Response, envelope) {
			return env.get(t, ticketSaleTicketsPath(eventID, ticketSaleID), authHeader(staffSessionID))
		}},
		{"get a ticket", func() (*http.Response, envelope) {
			return env.get(t, ticketPath(eventID, ticketID), authHeader(staffSessionID))
		}},
		{"answer", func() (*http.Response, envelope) {
			return env.put(t, answerPath(eventID, ticketID, question.ID),
				map[string]any{"text": "Sneaked in"}, authHeader(staffSessionID))
		}},
		{"remove an answer", func() (*http.Response, envelope) {
			return env.deleteJSON(t, answerPath(eventID, ticketID, question.ID), nil, authHeader(staffSessionID))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := tc.call()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status=%d, want 403; error=%+v", resp.StatusCode, body.Error)
			}
		})
	}
}
