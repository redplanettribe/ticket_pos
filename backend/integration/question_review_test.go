package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The Organization submits and withdraws a Question Review (#406, parent #404,
// ADR 0056): an Event's drafts and refused questions, carried as one ask to
// the Platform Operator, one outstanding per Event, with a note and a recorded
// acknowledgement of what the Organization is choosing to collect.

// questionReview decodes the staff Question Review payload.
type questionReview struct {
	ID             string     `json:"id"`
	EventID        string     `json:"event_id"`
	Status         string     `json:"status"`
	Note           *string    `json:"note"`
	AcknowledgedAt time.Time  `json:"acknowledged_at"`
	SubmittedBy    string     `json:"submitted_by"`
	SubmittedAt    time.Time  `json:"submitted_at"`
	AnsweredBy     *string    `json:"answered_by"`
	AnsweredAt     *time.Time `json:"answered_at"`
	Items          []struct {
		ID         string  `json:"id"`
		QuestionID string  `json:"ticket_question_id"`
		OptionID   *string `json:"ticket_question_option_id"`
		Verdict    *string `json:"verdict"`
		Reason     *string `json:"reason"`
	} `json:"items"`
}

func questionReviewsPath(eventID string) string {
	return "/api/v1/staff/events/" + eventID + "/question-reviews"
}

func decodeQuestionReview(t *testing.T, data json.RawMessage) questionReview {
	t.Helper()
	var review questionReview
	if err := json.Unmarshal(data, &review); err != nil {
		t.Fatalf("decode question review: %v", err)
	}
	return review
}

// questionItemIDs is the questions a Review carries, as ids, Option items
// excluded.
func questionItemIDs(review questionReview) []string {
	var ids []string
	for _, item := range review.Items {
		if item.OptionID == nil {
			ids = append(ids, item.QuestionID)
		}
	}
	return ids
}

func optionItemIDs(review questionReview) []string {
	var ids []string
	for _, item := range review.Items {
		if item.OptionID != nil {
			ids = append(ids, *item.OptionID)
		}
	}
	return ids
}

// reviewStatusOf reads a question's review_status back through the editor's
// own list, never the database.
func reviewStatusOf(t *testing.T, env *testEnv, sessionID, eventID, ticketTypeID, questionID string) string {
	t.Helper()
	resp, body := env.get(t, questionsPath(eventID, ticketTypeID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	for _, q := range decodeTicketQuestions(t, body.Data) {
		if q.ID == questionID {
			return q.ReviewStatus
		}
	}
	t.Fatalf("question %s is not listed", questionID)
	return ""
}

// refuseTicketQuestion stands in for the Operator's refusal (#407) so this
// file can prove a refused question rides the next Review.
func refuseTicketQuestion(t *testing.T, env *testEnv, questionID string) {
	t.Helper()
	if _, err := env.db.Exec(`
		UPDATE ticket_questions
		SET review_status = 'refused', refused_at = NOW(), refused_by = $2, refusal_reason = 'say why'
		WHERE id = $1
	`, questionID, testApprovedBy); err != nil {
		t.Fatalf("refuse question %s: %v", questionID, err)
	}
}

func addMemberSession(t *testing.T, env *testEnv, adminSessionID, email, role string) string {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": email,
		"role":  role,
	}, authHeader(adminSessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add %s member status=%d error=%+v", role, resp.StatusCode, body.Error)
	}
	return verifyOTP(t, env, email)
}

// questionReviewFixture is one draft Event a month away, with one Ticket Type
// carrying a required draft, a draft choice question with two draft Options,
// an approved question that must NOT be carried, and a refused one that must.
type questionReviewFixture struct {
	admin, eventID, ticketTypeID  string
	size, meal, approved, refused ticketQuestion
}

func newQuestionReviewFixture(t *testing.T, env *testEnv) questionReviewFixture {
	t.Helper()
	enableTicketQuestions(t)
	f := questionReviewFixture{}
	f.admin, f.eventID, f.ticketTypeID = ticketQuestionFixture(t, env)
	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2, timezone = 'America/Guayaquil' WHERE id = $1`,
		f.eventID, env.fixedClock.Add(30*24*time.Hour)); err != nil {
		t.Fatalf("set the Event's start: %v", err)
	}
	f.size = draftTicketQuestion(t, env, f.admin, f.eventID, f.ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})
	f.meal = draftTicketQuestion(t, env, f.admin, f.eventID, f.ticketTypeID, map[string]any{
		"label": "Meal", "kind": "single_choice", "option_labels": []string{"Chicken", "Vegetarian"},
	})
	f.approved = createTicketQuestion(t, env, f.admin, f.eventID, f.ticketTypeID, map[string]any{
		"label": "Company", "kind": "short_text",
	})
	f.refused = draftTicketQuestion(t, env, f.admin, f.eventID, f.ticketTypeID, map[string]any{
		"label": "Any allergies?", "kind": "long_text",
	})
	refuseTicketQuestion(t, env, f.refused.ID)
	sharedEmail.Reset()
	return f
}

func TestTheOrganizationSubmitsAndWithdrawsAQuestionReview(t *testing.T) {
	env := setupTest(t)
	f := newQuestionReviewFixture(t, env)

	// Two Operators, and the second reads Spanish: one submission fans out to
	// the whole allowlist, each in their own Mail Locale.
	seedPlatformOperator(t, env, "first.operator@example.com")
	spanish := operatorSession(t, env, "segunda.operadora@example.com")
	setStaffLocale(t, env, spanish, "es")
	sharedEmail.Reset()

	// Refused without the acknowledgement: the record ADR 0056 says this is.
	resp, body := env.post(t, questionReviewsPath(f.eventID),
		map[string]any{"note": "for the gala", "acknowledged": false}, authHeader(f.admin))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "QUESTION_REVIEW_ACKNOWLEDGEMENT_REQUIRED" {
		t.Fatalf("submit unacknowledged: status=%d error=%+v, want 400 QUESTION_REVIEW_ACKNOWLEDGEMENT_REQUIRED", resp.StatusCode, body.Error)
	}

	// Refused to Event Staff; an Event Owner may.
	staff := addMemberSession(t, env, f.admin, "staff@example.com", "event_staff")
	resp, body = env.post(t, questionReviewsPath(f.eventID),
		map[string]any{"acknowledged": true}, authHeader(staff))
	if resp.StatusCode != http.StatusForbidden || body.Error == nil || body.Error.Code != "FORBIDDEN" {
		t.Fatalf("submit as Event Staff: status=%d error=%+v, want 403 FORBIDDEN", resp.StatusCode, body.Error)
	}
	if n := len(sharedEmail.QuestionReviewsSubmitted()); n != 0 {
		t.Fatalf("%d notices sent for refused submissions, want 0", n)
	}

	// THE SUBMISSION.
	resp, body = env.post(t, questionReviewsPath(f.eventID),
		map[string]any{"note": "for the gala", "acknowledged": true}, authHeader(f.admin))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("submit status=%d error=%+v", resp.StatusCode, body.Error)
	}
	review := decodeQuestionReview(t, body.Data)
	if review.Status != "outstanding" || review.EventID != f.eventID || review.SubmittedBy != "admin@example.com" {
		t.Fatalf("submitted review = %+v", review)
	}
	if review.Note == nil || *review.Note != "for the gala" || review.AcknowledgedAt.IsZero() || review.AnsweredBy != nil {
		t.Fatalf("submitted review = %+v, want the note, an acknowledgement instant and no answer", review)
	}
	// The drafts and the refused question ride; the approved one does not.
	if got := questionItemIDs(review); len(got) != 3 || got[0] != f.size.ID || got[1] != f.meal.ID || got[2] != f.refused.ID {
		t.Fatalf("question items %v, want size, meal and the refused question in order", got)
	}
	if got := optionItemIDs(review); len(got) != 2 || got[0] != f.meal.Options[0].ID || got[1] != f.meal.Options[1].ID {
		t.Fatalf("option items %v, want the meal's two draft Options", got)
	}
	for _, item := range review.Items {
		if item.Verdict != nil || item.Reason != nil {
			t.Fatalf("a fresh item carries a verdict: %+v", item)
		}
	}
	for _, id := range []string{f.size.ID, f.meal.ID, f.refused.ID} {
		if got := reviewStatusOf(t, env, f.admin, f.eventID, f.ticketTypeID, id); got != "under_review" {
			t.Fatalf("carried question reads %q, want under_review", got)
		}
	}
	if got := reviewStatusOf(t, env, f.admin, f.eventID, f.ticketTypeID, f.approved.ID); got != "approved" {
		t.Fatalf("the approved question reads %q after a submission, want approved", got)
	}

	// Every Operator is told, in their own language.
	notices := sharedEmail.QuestionReviewsSubmitted()
	if len(notices) != 2 {
		t.Fatalf("captured %d submission notices, want one per allowlisted Operator", len(notices))
	}
	if notices[0].To != "first.operator@example.com" || notices[1].To != "segunda.operadora@example.com" {
		t.Fatalf("notices addressed to %q and %q", notices[0].To, notices[1].To)
	}
	en := notices[0].Subject() + "\n" + notices[0].Text()
	for _, want := range []string{"Test Org", "Workshop", "admin@example.com", "for the gala", "3 questions", "Review the questions on the Operator Dashboard"} {
		if !strings.Contains(en, want) {
			t.Fatalf("English notice lacks %q:\n%s", want, en)
		}
	}
	es := notices[1].Subject() + "\n" + notices[1].Text()
	for _, want := range []string{"Test Org", "Workshop", "admin@example.com", "for the gala", "3 preguntas", "Revise las preguntas en el Panel de Operador"} {
		if !strings.Contains(es, want) {
			t.Fatalf("Spanish notice lacks %q:\n%s", want, es)
		}
	}

	// One outstanding per Event.
	resp, body = env.post(t, questionReviewsPath(f.eventID),
		map[string]any{"acknowledged": true}, authHeader(f.admin))
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "QUESTION_REVIEW_OUTSTANDING" {
		t.Fatalf("second submit: status=%d error=%+v, want 409 QUESTION_REVIEW_OUTSTANDING", resp.StatusCode, body.Error)
	}

	// Drafting while a Review is outstanding is allowed, and the new draft is
	// not in it.
	late := draftTicketQuestion(t, env, f.admin, f.eventID, f.ticketTypeID, map[string]any{
		"label": "Seat preference", "kind": "short_text",
	})
	if late.ReviewStatus != "draft" {
		t.Fatalf("a question drafted during a Review reads %q, want draft", late.ReviewStatus)
	}
	resp, body = env.get(t, questionReviewsPath(f.eventID)+"/current", authHeader(f.admin))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get current status=%d error=%+v", resp.StatusCode, body.Error)
	}
	current := decodeQuestionReview(t, body.Data)
	if current.ID != review.ID || current.Status != "outstanding" || len(current.Items) != 5 {
		t.Fatalf("current review = %+v, want the outstanding one with its 5 items", current)
	}

	// THE WITHDRAWAL, by an Event Owner: the items go back to draft.
	owner := addMemberSession(t, env, f.admin, "owner@example.com", "event_owner")
	resp, body = env.post(t, questionReviewsPath(f.eventID)+"/"+review.ID+"/withdraw", nil, authHeader(owner))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("withdraw status=%d error=%+v", resp.StatusCode, body.Error)
	}
	withdrawn := decodeQuestionReview(t, body.Data)
	if withdrawn.Status != "withdrawn" || withdrawn.AnsweredBy == nil || *withdrawn.AnsweredBy != "owner@example.com" || withdrawn.AnsweredAt == nil {
		t.Fatalf("withdrawn review = %+v", withdrawn)
	}
	for _, id := range []string{f.size.ID, f.meal.ID, f.refused.ID} {
		if got := reviewStatusOf(t, env, f.admin, f.eventID, f.ticketTypeID, id); got != "draft" {
			t.Fatalf("withdrawn question reads %q, want draft", got)
		}
	}
	resp, body = env.get(t, questionsPath(f.eventID, f.ticketTypeID), authHeader(f.admin))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	for _, q := range decodeTicketQuestions(t, body.Data) {
		if q.ID == f.meal.ID && (q.Options[0].ReviewStatus != "draft" || q.Options[1].ReviewStatus != "draft") {
			t.Fatalf("withdrawn Options read %q/%q, want draft", q.Options[0].ReviewStatus, q.Options[1].ReviewStatus)
		}
	}

	resp, body = env.post(t, questionReviewsPath(f.eventID)+"/"+review.ID+"/withdraw", nil, authHeader(f.admin))
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "QUESTION_REVIEW_NOT_OUTSTANDING" {
		t.Fatalf("second withdraw: status=%d error=%+v, want 409 QUESTION_REVIEW_NOT_OUTSTANDING", resp.StatusCode, body.Error)
	}

	// With nothing outstanding, the editor reads the LAST Review.
	resp, body = env.get(t, questionReviewsPath(f.eventID)+"/current", authHeader(owner))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get last status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if last := decodeQuestionReview(t, body.Data); last.ID != review.ID || last.Status != "withdrawn" {
		t.Fatalf("last review = %+v, want the withdrawn one", last)
	}

	// And a fresh submission carries the late draft along with the rest.
	resp, body = env.post(t, questionReviewsPath(f.eventID),
		map[string]any{"acknowledged": true}, authHeader(f.admin))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("resubmit status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if again := decodeQuestionReview(t, body.Data); len(questionItemIDs(again)) != 4 || again.Note != nil {
		t.Fatalf("resubmitted review = %+v, want 4 question items and no note", again)
	}
}

func TestAQuestionReviewIsRefusedOnceTheEventHasStartedAndWhenNothingIsDraft(t *testing.T) {
	env := setupTest(t)
	f := newQuestionReviewFixture(t, env)

	// Nothing outstanding and no Review ever: the editor is told so.
	resp, body := env.get(t, questionReviewsPath(f.eventID)+"/current", authHeader(f.admin))
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "QUESTION_REVIEW_NOT_FOUND" {
		t.Fatalf("get current with none: status=%d error=%+v, want 404 QUESTION_REVIEW_NOT_FOUND", resp.StatusCode, body.Error)
	}

	// Started an hour ago, in the Event's own timezone.
	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		f.eventID, env.fixedClock.Add(-time.Hour)); err != nil {
		t.Fatalf("start the Event: %v", err)
	}
	resp, body = env.post(t, questionReviewsPath(f.eventID),
		map[string]any{"acknowledged": true}, authHeader(f.admin))
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "QUESTION_REVIEW_EVENT_STARTED" {
		t.Fatalf("submit after start: status=%d error=%+v, want 409 QUESTION_REVIEW_EVENT_STARTED", resp.StatusCode, body.Error)
	}
	if got := reviewStatusOf(t, env, f.admin, f.eventID, f.ticketTypeID, f.size.ID); got != "draft" {
		t.Fatalf("a refused submission moved the draft to %q", got)
	}

	// Nothing to carry: every question already approved.
	other := createDraftEvent(t, env, f.admin, "Quiet", "quiet")
	otherType := createTicketTypePriced(t, env, f.admin, other, 1000)
	createTicketQuestion(t, env, f.admin, other, otherType, map[string]any{"label": "Company", "kind": "short_text"})
	resp, body = env.post(t, questionReviewsPath(other),
		map[string]any{"acknowledged": true}, authHeader(f.admin))
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "QUESTION_REVIEW_NOTHING_TO_REVIEW" {
		t.Fatalf("submit with nothing to review: status=%d error=%+v, want 409 QUESTION_REVIEW_NOTHING_TO_REVIEW", resp.StatusCode, body.Error)
	}
	if n := len(sharedEmail.QuestionReviewsSubmitted()); n != 0 {
		t.Fatalf("%d notices sent for refused submissions, want 0", n)
	}

	// Dark with the flag, like every question route (ADR 0045).
	sharedApp.CatalogService.WithTicketQuestions(false)
	resp, body = env.post(t, questionReviewsPath(f.eventID),
		map[string]any{"acknowledged": true}, authHeader(f.admin))
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "TICKET_QUESTIONS_UNAVAILABLE" {
		t.Fatalf("submit with the flag off: status=%d error=%+v, want 404", resp.StatusCode, body.Error)
	}
}
