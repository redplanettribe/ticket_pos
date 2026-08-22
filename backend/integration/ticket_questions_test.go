package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// ticketQuestion decodes the staff Ticket Question payload.
type ticketQuestion struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Kind      string `json:"kind"`
	Required  bool   `json:"required"`
	Timing    string `json:"timing"`
	SortOrder int    `json:"sort_order"`
	Retired   bool   `json:"retired"`
	Options   []struct {
		ID        string `json:"id"`
		Label     string `json:"label"`
		SortOrder int    `json:"sort_order"`
		Retired   bool   `json:"retired"`
	} `json:"options"`
}

func questionsPath(eventID, ticketTypeID string) string {
	return "/api/v1/staff/events/" + eventID + "/ticket-types/" + ticketTypeID + "/questions"
}

func questionPath(eventID, ticketTypeID, questionID string) string {
	return questionsPath(eventID, ticketTypeID) + "/" + questionID
}

func optionsPath(eventID, ticketTypeID, questionID string) string {
	return questionPath(eventID, ticketTypeID, questionID) + "/options"
}

// enableTicketQuestions opens the authoring surface for one test.
//
// The flag is a property of the running service rather than of the request, so
// this is what a deployment that has set TICKET_QUESTIONS_ENABLED looks like
// from inside the suite. setupTest closes it again before the next test, so the
// dark default — which is how the feature ships — stays the state every other
// test in this package sees.
func enableTicketQuestions(t *testing.T) {
	t.Helper()
	sharedApp.CatalogService.WithTicketQuestions(true)
}

func decodeTicketQuestion(t *testing.T, data json.RawMessage) ticketQuestion {
	t.Helper()
	var q ticketQuestion
	if err := json.Unmarshal(data, &q); err != nil {
		t.Fatalf("decode ticket question: %v", err)
	}
	return q
}

func decodeTicketQuestions(t *testing.T, data json.RawMessage) []ticketQuestion {
	t.Helper()
	var questions []ticketQuestion
	if err := json.Unmarshal(data, &questions); err != nil {
		t.Fatalf("decode ticket questions: %v", err)
	}
	return questions
}

// createTicketQuestion adds a question and returns it, failing the test on any
// refusal — for the tests whose subject is something further along.
func createTicketQuestion(t *testing.T, env *testEnv, sessionID, eventID, ticketTypeID string, body map[string]any) ticketQuestion {
	t.Helper()
	resp, envelope := env.post(t, questionsPath(eventID, ticketTypeID), body, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create ticket question status=%d error=%+v", resp.StatusCode, envelope.Error)
	}
	return decodeTicketQuestion(t, envelope.Data)
}

// ticketQuestionFixture is the Ticket Type every test here hangs its questions
// off, plus the Org Admin session that may author them.
func ticketQuestionFixture(t *testing.T, env *testEnv) (sessionID, eventID, ticketTypeID string) {
	t.Helper()
	sessionID = orgAdminSession(t, env)
	eventID = createDraftEvent(t, env, sessionID, "Workshop", "workshop")
	ticketTypeID = createTicketTypePriced(t, env, sessionID, eventID, 5000)
	return sessionID, eventID, ticketTypeID
}

// TestTicketQuestionsAreInvisibleWhileTheFlagIsOff is the acceptance criterion
// ADR 0045 exists for, asserted at the only place it can be: the API.
//
// Every verb answers 404 — the same answer a build without the feature gives —
// so nothing an Organization can reach admits that the surface is there, and no
// Answer can be brought into existence before the Privacy Policy describes the
// collection.
func TestTicketQuestionsAreInvisibleWhileTheFlagIsOff(t *testing.T) {
	env := setupTest(t)
	sessionID, eventID, ticketTypeID := ticketQuestionFixture(t, env)

	// Deliberately NOT calling enableTicketQuestions: this is the shipped state.
	for _, tc := range []struct {
		name string
		call func() (*http.Response, envelope)
	}{
		{"list", func() (*http.Response, envelope) {
			return env.get(t, questionsPath(eventID, ticketTypeID), authHeader(sessionID))
		}},
		{"create", func() (*http.Response, envelope) {
			return env.post(t, questionsPath(eventID, ticketTypeID), map[string]any{
				"label": "T-shirt size",
				"kind":  "short_text",
			}, authHeader(sessionID))
		}},
		{"update", func() (*http.Response, envelope) {
			return env.patch(t, questionPath(eventID, ticketTypeID, "11111111-1111-4111-8111-111111111111"), map[string]any{
				"label": "T-shirt size",
				"kind":  "short_text",
			}, authHeader(sessionID))
		}},
		{"retire", func() (*http.Response, envelope) {
			return env.deleteJSON(t, questionPath(eventID, ticketTypeID, "11111111-1111-4111-8111-111111111111"), nil, authHeader(sessionID))
		}},
		{"reorder", func() (*http.Response, envelope) {
			return env.put(t, questionsPath(eventID, ticketTypeID)+"/order", map[string]any{
				"question_ids": []string{"11111111-1111-4111-8111-111111111111"},
			}, authHeader(sessionID))
		}},
		{"add option", func() (*http.Response, envelope) {
			return env.post(t, optionsPath(eventID, ticketTypeID, "11111111-1111-4111-8111-111111111111"), map[string]any{
				"label": "S",
			}, authHeader(sessionID))
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
			if body.Data != nil && string(body.Data) != "null" {
				t.Fatalf("expected null data on a refusal, got %s", string(body.Data))
			}
		})
	}
}

// The Event payload carries the flag so the staff app can hide the surface
// rather than offer one whose every request would 404. Off is the shipped state
// and the one every other surface is written against.
func TestEventPayloadReportsTheTicketQuestionFlag(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Flagged", "flagged")

	readFlag := func() bool {
		t.Helper()
		resp, body := env.get(t, "/api/v1/staff/events/"+eventID, authHeader(sessionID))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get event status=%d error=%+v", resp.StatusCode, body.Error)
		}
		var event struct {
			TicketQuestionsEnabled bool `json:"ticket_questions_enabled"`
		}
		if err := json.Unmarshal(body.Data, &event); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		return event.TicketQuestionsEnabled
	}

	if readFlag() {
		t.Fatal("ticket_questions_enabled is true on a build that has not opened the flag")
	}
	enableTicketQuestions(t)
	if !readFlag() {
		t.Fatal("ticket_questions_enabled stayed false after the flag was opened")
	}
}

// All seven kinds can be created, and the five that are not answered by
// choosing come back with no Options at all.
func TestAllSevenTicketQuestionKindsCanBeCreated(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID := ticketQuestionFixture(t, env)

	for _, tc := range []struct {
		kind        string
		optionLabel []string
	}{
		{kind: "short_text"},
		{kind: "long_text"},
		{kind: "single_choice", optionLabel: []string{"S", "M", "L"}},
		{kind: "multi_choice", optionLabel: []string{"Vegetarian", "Vegan"}},
		{kind: "number"},
		{kind: "date"},
		{kind: "checkbox"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			body := map[string]any{"label": "Question " + tc.kind, "kind": tc.kind}
			if tc.optionLabel != nil {
				body["option_labels"] = tc.optionLabel
			}
			question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, body)

			if question.Kind != tc.kind {
				t.Fatalf("kind=%q, want %q", question.Kind, tc.kind)
			}
			// v1 writes at_checkout for every question; the field exists so the
			// choice can be honoured later without migrating Answers.
			if question.Timing != "at_checkout" {
				t.Fatalf("timing=%q, want at_checkout", question.Timing)
			}
			if len(question.Options) != len(tc.optionLabel) {
				t.Fatalf("options=%d, want %d for a %s question", len(question.Options), len(tc.optionLabel), tc.kind)
			}
			for i, option := range question.Options {
				if option.Label != tc.optionLabel[i] {
					t.Fatalf("option %d label=%q, want %q", i, option.Label, tc.optionLabel[i])
				}
				if option.ID == "" {
					t.Fatal("an Option arrived without an identity of its own")
				}
			}
		})
	}
}

// The five non-choice kinds refuse Options rather than silently dropping them: a
// caller that sent them believed something about the question that is not true.
func TestNonChoiceTicketQuestionRefusesOptions(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID := ticketQuestionFixture(t, env)

	resp, body := env.post(t, questionsPath(eventID, ticketTypeID), map[string]any{
		"label":         "Any allergies?",
		"kind":          "long_text",
		"option_labels": []string{"Nuts"},
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "TICKET_QUESTION_KIND_TAKES_NO_OPTIONS" {
		t.Fatalf("error=%+v, want TICKET_QUESTION_KIND_TAKES_NO_OPTIONS", body.Error)
	}
}

// A choice question must arrive with something to choose from.
func TestChoiceTicketQuestionRequiresAnOption(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID := ticketQuestionFixture(t, env)

	resp, body := env.post(t, questionsPath(eventID, ticketTypeID), map[string]any{
		"label": "T-shirt size",
		"kind":  "single_choice",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "TICKET_QUESTION_OPTIONS_REQUIRED" {
		t.Fatalf("error=%+v, want TICKET_QUESTION_OPTIONS_REQUIRED", body.Error)
	}
}

// Twenty Options are allowed and a twenty-first is refused, whether the
// twenty-first arrives with the question or afterwards.
func TestTicketQuestionRefusesATwentyFirstOption(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID := ticketQuestionFixture(t, env)

	twenty := make([]string, 0, 20)
	for i := range 20 {
		twenty = append(twenty, fmt.Sprintf("Option %d", i))
	}

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label":         "Pick one",
		"kind":          "single_choice",
		"option_labels": twenty,
	})
	if len(question.Options) != 20 {
		t.Fatalf("options=%d, want the full twenty", len(question.Options))
	}

	resp, body := env.post(t, optionsPath(eventID, ticketTypeID, question.ID), map[string]any{
		"label": "One too many",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "TOO_MANY_TICKET_QUESTION_OPTIONS" {
		t.Fatalf("error=%+v, want TOO_MANY_TICKET_QUESTION_OPTIONS", body.Error)
	}

	// The same refusal when the twenty-first arrives with the question.
	twentyOne := append(append([]string{}, twenty...), "One too many")
	resp, body = env.post(t, questionsPath(eventID, ticketTypeID), map[string]any{
		"label":         "Pick one",
		"kind":          "single_choice",
		"option_labels": twentyOne,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 on a body that states twenty-one; error=%+v", resp.StatusCode, body.Error)
	}
}

// Options are added and renamed freely, and retired but never deleted. The
// rename keeps the Option's identity, which is what will keep the Answers given
// under the old wording attached to it.
func TestTicketQuestionOptionsAreAddedRenamedAndRetiredButNeverDeleted(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID := ticketQuestionFixture(t, env)

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label":         "T-shirt size",
		"kind":          "single_choice",
		"option_labels": []string{"S", "Mediun"},
	})
	typoOptionID := question.Options[1].ID

	// Added at any time.
	resp, body := env.post(t, optionsPath(eventID, ticketTypeID, question.ID), map[string]any{
		"label": "L",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add option status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// Renamed at any time, and the identity does not move.
	resp, body = env.patch(t, optionsPath(eventID, ticketTypeID, question.ID)+"/"+typoOptionID, map[string]any{
		"label": "Medium",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rename option status=%d error=%+v", resp.StatusCode, body.Error)
	}
	renamed := decodeTicketQuestion(t, body.Data)
	var found bool
	for _, option := range renamed.Options {
		if option.ID != typoOptionID {
			continue
		}
		found = true
		if option.Label != "Medium" {
			t.Fatalf("label=%q, want the correction", option.Label)
		}
	}
	if !found {
		t.Fatal("the renamed Option lost its identity; every Answer under it would have been forked")
	}

	// Retired, never deleted: still present, and marked.
	resp, body = env.deleteJSON(t, optionsPath(eventID, ticketTypeID, question.ID)+"/"+typoOptionID, nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retire option status=%d error=%+v", resp.StatusCode, body.Error)
	}
	retired := decodeTicketQuestion(t, body.Data)
	if len(retired.Options) != 3 {
		t.Fatalf("options=%d, want all three still present — retiring is not deleting", len(retired.Options))
	}
	var sawRetired bool
	for _, option := range retired.Options {
		if option.ID == typoOptionID {
			sawRetired = option.Retired
			if option.Label != "Medium" {
				t.Fatalf("a retired Option lost its label: %q", option.Label)
			}
		}
	}
	if !sawRetired {
		t.Fatal("the Option was not marked retired")
	}
}

// The last live Option cannot be retired: a choice question with nothing to
// choose from is not a question. Retiring the question is the way out.
func TestRetiringTheLastTicketQuestionOptionIsRefused(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID := ticketQuestionFixture(t, env)

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label":         "T-shirt size",
		"kind":          "single_choice",
		"option_labels": []string{"One size"},
	})

	resp, body := env.deleteJSON(t, optionsPath(eventID, ticketTypeID, question.ID)+"/"+question.Options[0].ID, nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "TICKET_QUESTION_OPTIONS_REQUIRED" {
		t.Fatalf("error=%+v, want TICKET_QUESTION_OPTIONS_REQUIRED", body.Error)
	}
}

// A question is renamed and its flags restated, and it is retired rather than
// deleted.
func TestTicketQuestionIsRenamedAndRetiredNeverDeleted(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID := ticketQuestionFixture(t, env)

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "Shirt size",
		"kind":  "short_text",
	})

	resp, body := env.patch(t, questionPath(eventID, ticketTypeID, question.ID), map[string]any{
		"label":    "T-shirt size",
		"kind":     "short_text",
		"required": true,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update status=%d error=%+v", resp.StatusCode, body.Error)
	}
	updated := decodeTicketQuestion(t, body.Data)
	if updated.Label != "T-shirt size" {
		t.Fatalf("label=%q, want the rename", updated.Label)
	}
	if !updated.Required {
		t.Fatal("required was not set")
	}

	resp, body = env.deleteJSON(t, questionPath(eventID, ticketTypeID, question.ID), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retire status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if retired := decodeTicketQuestion(t, body.Data); !retired.Retired {
		t.Fatal("the question was not marked retired")
	}

	// Still readable, so the authoring surface can show what was retired rather
	// than appearing to have lost it.
	resp, body = env.get(t, questionsPath(eventID, ticketTypeID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	questions := decodeTicketQuestions(t, body.Data)
	if len(questions) != 1 || !questions[0].Retired {
		t.Fatalf("expected one retired question in the list, got %+v", questions)
	}

	// And a retired question is not an authoring surface.
	resp, body = env.patch(t, questionPath(eventID, ticketTypeID, question.ID), map[string]any{
		"label": "Anything",
		"kind":  "short_text",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want 409 editing a retired question; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "TICKET_QUESTION_RETIRED" {
		t.Fatalf("error=%+v, want TICKET_QUESTION_RETIRED", body.Error)
	}
}

// The kind can still change while nothing has answered — which is every question
// on the platform today, because no Answer can exist yet (ADR 0045). This is the
// reachable half of the freeze; the unreachable half is asserted directly
// against catalog.TicketQuestionKindFrozen, which is the level the guard can be
// tested at until Answers land.
func TestTicketQuestionKindChangesWhileNothingHasAnswered(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID := ticketQuestionFixture(t, env)

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "How many guests?",
		"kind":  "short_text",
	})

	resp, body := env.patch(t, questionPath(eventID, ticketTypeID, question.ID), map[string]any{
		"label": "How many guests?",
		"kind":  "number",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want the kind to be free to change before any Answer; error=%+v", resp.StatusCode, body.Error)
	}
	if updated := decodeTicketQuestion(t, body.Data); updated.Kind != "number" {
		t.Fatalf("kind=%q, want number", updated.Kind)
	}
}

// Reordering states the whole running order rather than a move, so two questions
// can never end up claiming the same place.
func TestTicketQuestionsAreReordered(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID := ticketQuestionFixture(t, env)

	first := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "First", "kind": "short_text",
	})
	second := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "Second", "kind": "short_text",
	})

	resp, body := env.put(t, questionsPath(eventID, ticketTypeID)+"/order", map[string]any{
		"question_ids": []string{second.ID, first.ID},
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reorder status=%d error=%+v", resp.StatusCode, body.Error)
	}
	questions := decodeTicketQuestions(t, body.Data)
	if len(questions) != 2 || questions[0].ID != second.ID || questions[1].ID != first.ID {
		t.Fatalf("order not applied: %+v", questions)
	}

	// A partial order is refused rather than partially honoured.
	resp, body = env.put(t, questionsPath(eventID, ticketTypeID)+"/order", map[string]any{
		"question_ids": []string{first.ID},
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 on an order that names only some of the questions; error=%+v", resp.StatusCode, body.Error)
	}
}

// A label is the Organization's own words and is kept AS COINED: the accents,
// the casing and the punctuation come back exactly as typed, in every Locale,
// because there is only one of them. Only the outer whitespace goes.
func TestTicketQuestionLabelsAreKeptAsCoined(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID := ticketQuestionFixture(t, env)

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label":         "  ¿Talla de Camiseta?  ",
		"kind":          "single_choice",
		"option_labels": []string{"  Pequeña ", "Mediana"},
	})

	if question.Label != "¿Talla de Camiseta?" {
		t.Fatalf("label=%q, want the words as coined with only the outer whitespace gone", question.Label)
	}
	if question.Options[0].Label != "Pequeña" {
		t.Fatalf("option label=%q, want the words as coined", question.Options[0].Label)
	}

	// The list read gives back the same one string. There is no per-Locale
	// authoring and no per-Locale storage, so there is nothing here that could
	// read differently on an `es` page than on an `en` one — only the chrome
	// around it follows the Locale (ADR 0027).
	resp, body := env.get(t, questionsPath(eventID, ticketTypeID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	questions := decodeTicketQuestions(t, body.Data)
	if len(questions) != 1 || questions[0].Label != "¿Talla de Camiseta?" {
		t.Fatalf("list label=%+v, want the coined label", questions)
	}
}

// A blank or overlong label is refused as a field error, so an editor can point
// at the offending field.
func TestTicketQuestionLabelValidation(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID := ticketQuestionFixture(t, env)

	for _, tc := range []struct {
		name  string
		label string
	}{
		{"blank", "   "},
		{"overlong", strings.Repeat("a", 201)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := env.post(t, questionsPath(eventID, ticketTypeID), map[string]any{
				"label": tc.label,
				"kind":  "short_text",
			}, authHeader(sessionID))
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status=%d, want 400; error=%+v", resp.StatusCode, body.Error)
			}
			if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
				t.Fatalf("error=%+v, want VALIDATION_FAILED", body.Error)
			}
		})
	}
}

// An unknown kind is refused by name rather than defaulted to something.
func TestTicketQuestionRefusesAnUnknownKind(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID := ticketQuestionFixture(t, env)

	resp, body := env.post(t, questionsPath(eventID, ticketTypeID), map[string]any{
		"label": "Upload your ID",
		"kind":  "file",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("error=%+v, want VALIDATION_FAILED", body.Error)
	}
}

// Ticket Question authoring is gated exactly as editing the Ticket Type it
// belongs to is: Event Staff hired for the door are refused every verb.
func TestTicketQuestionsForbiddenForMemberWithoutCatalogAuthority(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID := ticketQuestionFixture(t, env)

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text",
	})

	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": "doorstaff@example.com",
		"role":  "event_staff",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add member status=%d error=%+v", resp.StatusCode, body.Error)
	}
	staffSessionID := verifyOTP(t, env, "doorstaff@example.com")

	for _, tc := range []struct {
		name string
		call func() (*http.Response, envelope)
	}{
		{"list", func() (*http.Response, envelope) {
			return env.get(t, questionsPath(eventID, ticketTypeID), authHeader(staffSessionID))
		}},
		{"create", func() (*http.Response, envelope) {
			return env.post(t, questionsPath(eventID, ticketTypeID), map[string]any{
				"label": "Sneaked in", "kind": "short_text",
			}, authHeader(staffSessionID))
		}},
		{"update", func() (*http.Response, envelope) {
			return env.patch(t, questionPath(eventID, ticketTypeID, question.ID), map[string]any{
				"label": "Sneaked in", "kind": "short_text",
			}, authHeader(staffSessionID))
		}},
		{"retire", func() (*http.Response, envelope) {
			return env.deleteJSON(t, questionPath(eventID, ticketTypeID, question.ID), nil, authHeader(staffSessionID))
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

// One Organization's Ticket Type id must not resolve under another's Event.
func TestTicketQuestionsAreScopedToTheirOrganization(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	_, eventID, ticketTypeID := ticketQuestionFixture(t, env)

	otherSession := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSession, "Other Org", "other-org")

	resp, body := env.get(t, questionsPath(eventID, ticketTypeID), authHeader(otherSession))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 across Organizations; error=%+v", resp.StatusCode, body.Error)
	}
}
