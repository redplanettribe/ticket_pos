package integration

import (
	"net/http"
	"testing"
)

// setTicketQuestionUnderReview stands in for a Question Review carrying the
// question (#406 builds the submit route on its own branch): the row is put
// under review by SQL, exactly as the submission will leave it.
func setTicketQuestionUnderReview(t *testing.T, env *testEnv, questionID string) {
	t.Helper()
	if _, err := env.db.Exec(`UPDATE ticket_questions SET review_status = 'under_review' WHERE id = $1`, questionID); err != nil {
		t.Fatalf("put question %s under review: %v", questionID, err)
	}
}

// patchQuestion sends the whole PATCH body the editor sends, with one field
// varied, so each refusal below is about one change and nothing else.
func patchQuestion(t *testing.T, env *testEnv, sessionID, eventID, ticketTypeID string, q ticketQuestion, changes map[string]any) (*http.Response, envelope) {
	t.Helper()
	body := map[string]any{
		"label":    q.Label,
		"kind":     q.Kind,
		"required": q.Required,
		"timing":   q.Timing,
	}
	for k, v := range changes {
		body[k] = v
	}
	return env.patch(t, questionPath(eventID, ticketTypeID, q.ID), body, authHeader(sessionID))
}

// TestAnApprovedTicketQuestionIsImmutableExceptForNarrowing is ADR 0056's
// immutability rule at the API (#408): wording, kind and required-ness are
// what the Operator read, so the only edits an approved question takes
// without a review are the ones that collect less.
func TestAnApprovedTicketQuestionIsImmutableExceptForNarrowing(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID := ticketQuestionFixture(t, env)

	size := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label":         "T-shirt size",
		"kind":          "single_choice",
		"required":      true,
		"option_labels": []string{"S", "M", "L"},
	})
	size.ReviewStatus = "approved"

	// Every change the Operator did not read is refused, with the code that
	// names why.
	for name, change := range map[string]map[string]any{
		"reword":      {"label": "Shirt size"},
		"change kind": {"kind": "short_text"},
		"retime":      {"timing": "after_purchase"},
	} {
		resp, body := patchQuestion(t, env, sessionID, eventID, ticketTypeID, size, change)
		if resp.StatusCode != http.StatusConflict || body.Error.Code != "TICKET_QUESTION_APPROVED_IMMUTABLE" {
			t.Fatalf("%s on an approved question: status=%d error=%+v, want 409 TICKET_QUESTION_APPROVED_IMMUTABLE", name, resp.StatusCode, body.Error)
		}
	}

	// Renaming an approved Option is a change the Operator did not read; the
	// correction is retire-and-add.
	resp, body := env.patch(t, optionPath(eventID, ticketTypeID, size.ID, size.Options[1].ID), map[string]any{
		"label": "Medium",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict || body.Error.Code != "TICKET_QUESTION_OPTION_APPROVED_IMMUTABLE" {
		t.Fatalf("rename approved option: status=%d error=%+v, want 409 TICKET_QUESTION_OPTION_APPROVED_IMMUTABLE", resp.StatusCode, body.Error)
	}

	// Narrowing collects less and needs nobody: optional, an Option retired,
	// and the running order is nobody's concern but the Organization's.
	resp, body = patchQuestion(t, env, sessionID, eventID, ticketTypeID, size, map[string]any{"required": false})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("making an approved question optional: status=%d error=%+v", resp.StatusCode, body.Error)
	}
	optional := decodeTicketQuestion(t, body.Data)
	if optional.Required || optional.ReviewStatus != "approved" {
		t.Fatalf("after narrowing: required=%v review_status=%q, want optional and still approved", optional.Required, optional.ReviewStatus)
	}

	// And back to required is widening: refused.
	resp, body = patchQuestion(t, env, sessionID, eventID, ticketTypeID, size, map[string]any{"required": true})
	if resp.StatusCode != http.StatusConflict || body.Error.Code != "TICKET_QUESTION_APPROVED_IMMUTABLE" {
		t.Fatalf("re-requiring an approved question: status=%d error=%+v, want 409 TICKET_QUESTION_APPROVED_IMMUTABLE", resp.StatusCode, body.Error)
	}

	resp, body = env.deleteJSON(t, optionPath(eventID, ticketTypeID, size.ID, size.Options[2].ID), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retiring an approved Option: status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// A draft Option added to an approved question is not yet what anybody
	// read, so it can still be renamed.
	resp, body = env.post(t, optionsPath(eventID, ticketTypeID, size.ID), map[string]any{"label": "XL"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("adding an Option to an approved question: status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var draftOptionID string
	for _, option := range decodeTicketQuestion(t, body.Data).Options {
		if option.Label == "XL" {
			draftOptionID = option.ID
		}
	}
	resp, body = env.patch(t, optionPath(eventID, ticketTypeID, size.ID, draftOptionID), map[string]any{
		"label": "Extra large",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("renaming a draft Option on an approved question: status=%d error=%+v", resp.StatusCode, body.Error)
	}

	other := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "Dietary needs",
		"kind":  "short_text",
	})
	resp, body = env.put(t, questionsPath(eventID, ticketTypeID)+"/order", map[string]any{
		"question_ids": []string{other.ID, size.ID},
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reordering approved questions: status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.deleteJSON(t, questionPath(eventID, ticketTypeID, size.ID), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retiring an approved question: status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if got := decodeTicketQuestion(t, body.Data); !got.Retired || got.ReviewStatus != "approved" {
		t.Fatalf("retired=%v review_status=%q, want retired and still approved — the approval was real", got.Retired, got.ReviewStatus)
	}
}

// TestATicketQuestionUnderReviewCannotBeEditedUntilWithdrawn: while a Question
// Review carries a question, what the Operator is reading must hold still.
// Narrowing, reorder and retirement stay open in every state.
func TestATicketQuestionUnderReviewCannotBeEditedUntilWithdrawn(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID := ticketQuestionFixture(t, env)

	meal := draftTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label":         "Meal",
		"kind":          "single_choice",
		"required":      true,
		"option_labels": []string{"Veg", "Fish"},
	})
	setTicketQuestionUnderReview(t, env, meal.ID)

	for name, call := range map[string]func() (*http.Response, envelope){
		"reword": func() (*http.Response, envelope) {
			return patchQuestion(t, env, sessionID, eventID, ticketTypeID, meal, map[string]any{"label": "Menu"})
		},
		"rename option": func() (*http.Response, envelope) {
			return env.patch(t, optionPath(eventID, ticketTypeID, meal.ID, meal.Options[0].ID), map[string]any{"label": "Vegetarian"}, authHeader(sessionID))
		},
		"add option": func() (*http.Response, envelope) {
			return env.post(t, optionsPath(eventID, ticketTypeID, meal.ID), map[string]any{"label": "Meat"}, authHeader(sessionID))
		},
	} {
		resp, body := call()
		if resp.StatusCode != http.StatusConflict || body.Error.Code != "TICKET_QUESTION_UNDER_REVIEW" {
			t.Fatalf("%s under review: status=%d error=%+v, want 409 TICKET_QUESTION_UNDER_REVIEW", name, resp.StatusCode, body.Error)
		}
	}

	resp, body := patchQuestion(t, env, sessionID, eventID, ticketTypeID, meal, map[string]any{"required": false})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("making a question under review optional: status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if got := decodeTicketQuestion(t, body.Data); got.Required || got.ReviewStatus != "under_review" {
		t.Fatalf("required=%v review_status=%q, want optional and still under review", got.Required, got.ReviewStatus)
	}

	resp, body = env.deleteJSON(t, optionPath(eventID, ticketTypeID, meal.ID, meal.Options[1].ID), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retiring an Option under review: status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.put(t, questionsPath(eventID, ticketTypeID)+"/order", map[string]any{
		"question_ids": []string{meal.ID},
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reordering under review: status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.deleteJSON(t, questionPath(eventID, ticketTypeID, meal.ID), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retiring a question under review: status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// TestADraftOrRefusedTicketQuestionIsFreelyEdited: nobody has read a draft,
// and a refusal is an invitation to change it.
func TestADraftOrRefusedTicketQuestionIsFreelyEdited(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID := ticketQuestionFixture(t, env)

	for _, status := range []string{"draft", "refused"} {
		q := draftTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
			"label":         "Allergies (" + status + ")",
			"kind":          "single_choice",
			"option_labels": []string{"None", "Nuts"},
		})
		if status == "refused" {
			if _, err := env.db.Exec(`UPDATE ticket_questions SET review_status = 'refused', refused_at = NOW(), refused_by = 'test:operator', refusal_reason = 'Too broad' WHERE id = $1`, q.ID); err != nil {
				t.Fatalf("refuse question: %v", err)
			}
		}
		resp, body := patchQuestion(t, env, sessionID, eventID, ticketTypeID, q, map[string]any{"label": "Any allergies we should know about?", "required": true})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("editing a %s question: status=%d error=%+v", status, resp.StatusCode, body.Error)
		}
		resp, body = env.patch(t, optionPath(eventID, ticketTypeID, q.ID, q.Options[1].ID), map[string]any{"label": "Tree nuts"}, authHeader(sessionID))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("renaming an Option of a %s question: status=%d error=%+v", status, resp.StatusCode, body.Error)
		}
	}
}
