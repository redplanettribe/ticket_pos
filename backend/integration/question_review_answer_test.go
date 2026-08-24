package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The Operator answers a Question Review from the Dashboard, and a Review
// lapses at Event start (#407, parent #404, ADR 0056): one act with a verdict
// per item, approve or refuse with a reason; approved questions collect on the
// spot, refused ones carry the reason and go back to draft on their first
// edit, and a Review whose Event has started reads lapsed on read.

const operatorQuestionReviewsPath = "/api/v1/operator/question-reviews"

// operatorQuestionReview decodes the Operator's queue row and detail: the
// Review with its Organization and Event beside it.
type operatorQuestionReview struct {
	Review struct {
		questionReview
		QuestionCount int `json:"question_count"`
		Items         []struct {
			ID         string  `json:"id"`
			QuestionID string  `json:"ticket_question_id"`
			OptionID   *string `json:"ticket_question_option_id"`
			Verdict    *string `json:"verdict"`
			Reason     *string `json:"reason"`
			Question   *struct {
				ID             string `json:"id"`
				Label          string `json:"label"`
				Kind           string `json:"kind"`
				Required       bool   `json:"required"`
				TicketTypeName string `json:"ticket_type_name"`
				Options        []struct {
					ID    string `json:"id"`
					Label string `json:"label"`
				} `json:"options"`
			} `json:"question"`
			Option *struct {
				ID    string `json:"id"`
				Label string `json:"label"`
			} `json:"option"`
		} `json:"items"`
	} `json:"review"`
	Organization struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"organization"`
	Event struct {
		ID       string     `json:"id"`
		Name     string     `json:"name"`
		StartsAt *time.Time `json:"starts_at"`
		Timezone string     `json:"timezone"`
	} `json:"event"`
}

func decodeOperatorQuestionReview(t *testing.T, data json.RawMessage) operatorQuestionReview {
	t.Helper()
	var out operatorQuestionReview
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode operator question review: %v", err)
	}
	return out
}

func operatorQuestionReviewQueue(t *testing.T, env *testEnv, operator string) []operatorQuestionReview {
	t.Helper()
	resp, body := env.get(t, operatorQuestionReviewsPath, authHeader(operator))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("queue status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var page struct {
		Data []operatorQuestionReview `json:"data"`
	}
	if err := json.Unmarshal(body.Data, &page); err != nil {
		t.Fatalf("decode queue: %v", err)
	}
	return page.Data
}

func outstandingQuestionReviewCount(t *testing.T, env *testEnv, operator string) int {
	t.Helper()
	resp, body := env.get(t, operatorQuestionReviewsPath+"/count", authHeader(operator))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("count status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var count struct {
		OutstandingCount int `json:"outstanding_count"`
	}
	if err := json.Unmarshal(body.Data, &count); err != nil {
		t.Fatalf("decode count: %v", err)
	}
	return count.OutstandingCount
}

func submitQuestionReview(t *testing.T, env *testEnv, sessionID, eventID string) questionReview {
	t.Helper()
	resp, body := env.post(t, questionReviewsPath(eventID),
		map[string]any{"acknowledged": true}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("submit status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return decodeQuestionReview(t, body.Data)
}

// itemOf finds the Review item carrying one question or Option.
func itemOf(t *testing.T, review questionReview, questionID string, optionID *string) string {
	t.Helper()
	for _, item := range review.Items {
		if item.QuestionID != questionID {
			continue
		}
		if optionID == nil && item.OptionID == nil {
			return item.ID
		}
		if optionID != nil && item.OptionID != nil && *item.OptionID == *optionID {
			return item.ID
		}
	}
	t.Fatalf("no item for question %s option %v", questionID, optionID)
	return ""
}

// errorDetail reads one key off a domain error's details.
func errorDetail(body envelope, key string) any {
	details, _ := body.Error.Details.(map[string]any)
	return details[key]
}

func verdict(itemID, verdict, reason string) map[string]any {
	v := map[string]any{"item_id": itemID, "verdict": verdict}
	if reason != "" {
		v["reason"] = reason
	}
	return v
}

func TestTheOperatorAnswersAQuestionReviewPerItem(t *testing.T) {
	env := setupTest(t)
	f := newReviewFixture(t, env)
	operator := operatorSession(t, env, "operator@example.com")

	// Before anything is submitted the queue is empty.
	if got := outstandingQuestionReviewCount(t, env, operator); got != 0 {
		t.Fatalf("outstanding count = %d before any submission, want 0", got)
	}

	submitted := submitQuestionReview(t, env, f.staff, f.eventID)
	sharedEmail.Reset()
	sizeItem := itemOf(t, submitted, f.size.ID, nil)
	mealItem := itemOf(t, submitted, f.meal.ID, nil)
	chickenItem := itemOf(t, submitted, f.meal.ID, &f.meal.Options[0].ID)
	vegItem := itemOf(t, submitted, f.meal.ID, &f.meal.Options[1].ID)

	// THE QUEUE: whose it is, which Event, when it starts, how much it carries.
	if got := outstandingQuestionReviewCount(t, env, operator); got != 1 {
		t.Fatalf("outstanding count = %d, want 1", got)
	}
	queue := operatorQuestionReviewQueue(t, env, operator)
	if len(queue) != 1 {
		t.Fatalf("queue has %d rows, want 1", len(queue))
	}
	row := queue[0]
	if row.Review.ID != submitted.ID || row.Review.Status != "outstanding" || row.Review.QuestionCount != 2 ||
		row.Organization.Name != "Test Org" || row.Organization.Slug != "test-org" ||
		row.Event.ID != f.eventID || row.Event.Name != "Review Fest" || row.Event.StartsAt == nil ||
		!row.Event.StartsAt.Equal(env.fixedClock.Add(30*24*time.Hour)) {
		t.Fatalf("queue row = %+v", row)
	}

	// THE DETAIL: each item with its question's whole shape, or its Option's.
	resp, body := env.get(t, operatorQuestionReviewsPath+"/"+submitted.ID, authHeader(operator))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail status=%d error=%+v", resp.StatusCode, body.Error)
	}
	detail := decodeOperatorQuestionReview(t, body.Data)
	if detail.Review.SubmittedBy != "admin@example.com" || detail.Review.AcknowledgedAt.IsZero() || len(detail.Review.Items) != 4 {
		t.Fatalf("detail = %+v", detail.Review)
	}
	first, third := detail.Review.Items[0], detail.Review.Items[2]
	if first.Question == nil || first.Question.Label != "T-shirt size" || first.Question.Kind != "short_text" || !first.Question.Required || first.Option != nil {
		t.Fatalf("first item = %+v, want the required short_text question", first)
	}
	if third.Option == nil || third.Option.Label != "Chicken" || third.Question == nil || third.Question.Label != "Meal" || len(third.Question.Options) != 2 {
		t.Fatalf("third item = %+v, want the Chicken Option of the Meal question", third)
	}

	// REFUSED ANSWERS, each naming the item.
	answerPath := operatorQuestionReviewsPath + "/" + submitted.ID + "/answer"
	resp, body = env.post(t, answerPath, map[string]any{"verdicts": []map[string]any{
		verdict(sizeItem, "approved", ""),
		verdict(mealItem, "refused", "say why"),
		verdict(chickenItem, "refused", "say why"),
	}}, authHeader(operator))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "QUESTION_REVIEW_VERDICT_REQUIRED" {
		t.Fatalf("answer with a missing verdict: status=%d error=%+v, want 400 QUESTION_REVIEW_VERDICT_REQUIRED", resp.StatusCode, body.Error)
	}
	if named := errorDetail(body, "item_id"); named != vegItem {
		t.Fatalf("missing verdict names %v, want %s", named, vegItem)
	}
	resp, body = env.post(t, answerPath, map[string]any{"verdicts": []map[string]any{
		verdict(sizeItem, "approved", ""),
		verdict(mealItem, "refused", "  "),
		verdict(chickenItem, "refused", "say why"),
		verdict(vegItem, "refused", "say why"),
	}}, authHeader(operator))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "QUESTION_REVIEW_REASON_REQUIRED" {
		t.Fatalf("refusal without a reason: status=%d error=%+v, want 400 QUESTION_REVIEW_REASON_REQUIRED", resp.StatusCode, body.Error)
	}
	if named := errorDetail(body, "item_id"); named != mealItem {
		t.Fatalf("missing reason names %v, want %s", named, mealItem)
	}
	resp, body = env.post(t, answerPath, map[string]any{"verdicts": []map[string]any{
		verdict("00000000-0000-0000-0000-000000000000", "approved", ""),
	}}, authHeader(operator))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "QUESTION_REVIEW_UNKNOWN_ITEM" {
		t.Fatalf("unknown item: status=%d error=%+v, want 400 QUESTION_REVIEW_UNKNOWN_ITEM", resp.StatusCode, body.Error)
	}
	if got := reviewStatusOf(t, env, f.staff, f.eventID, f.ticketTypeID, f.size.ID); got != "under_review" {
		t.Fatalf("a refused answer moved the question to %q", got)
	}
	if n := len(sharedEmail.QuestionReviewsAnswered()); n != 0 {
		t.Fatalf("%d answered notices for refused answers, want 0", n)
	}

	// THE ANSWER: size approved, meal and its Options refused.
	resp, body = env.post(t, answerPath, map[string]any{"verdicts": []map[string]any{
		verdict(sizeItem, "approved", ""),
		verdict(mealItem, "refused", "say why"),
		verdict(chickenItem, "refused", "say why"),
		verdict(vegItem, "refused", "say why"),
	}}, authHeader(operator))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("answer status=%d error=%+v", resp.StatusCode, body.Error)
	}
	answered := decodeOperatorQuestionReview(t, body.Data)
	if answered.Review.Status != "answered" || answered.Review.AnsweredBy == nil || *answered.Review.AnsweredBy != "operator@example.com" || answered.Review.AnsweredAt == nil {
		t.Fatalf("answered review = %+v", answered.Review)
	}
	for _, item := range answered.Review.Items {
		switch item.ID {
		case sizeItem:
			if item.Verdict == nil || *item.Verdict != "approved" || item.Reason != nil {
				t.Fatalf("size item = %+v, want approved with no reason", item)
			}
		default:
			if item.Verdict == nil || *item.Verdict != "refused" || item.Reason == nil || *item.Reason != "say why" {
				t.Fatalf("item %s = %+v, want refused with the reason", item.ID, item)
			}
		}
	}
	if got := outstandingQuestionReviewCount(t, env, operator); got != 0 {
		t.Fatalf("outstanding count = %d after the answer, want 0", got)
	}
	if queue := operatorQuestionReviewQueue(t, env, operator); len(queue) != 0 {
		t.Fatalf("queue still lists %d rows after the answer", len(queue))
	}
	resp, body = env.post(t, answerPath, map[string]any{"verdicts": []map[string]any{
		verdict(sizeItem, "approved", ""),
	}}, authHeader(operator))
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "QUESTION_REVIEW_NOT_OUTSTANDING" {
		t.Fatalf("second answer: status=%d error=%+v, want 409 QUESTION_REVIEW_NOT_OUTSTANDING", resp.StatusCode, body.Error)
	}

	// The staff editor reads the verdicts, with authorship and the reason.
	resp, body = env.get(t, questionsPath(f.eventID, f.ticketTypeID), authHeader(f.staff))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	listed := decodeTicketQuestions(t, body.Data)
	if listed[0].ReviewStatus != "approved" || listed[0].ApprovedBy == nil || *listed[0].ApprovedBy != "operator@example.com" {
		t.Fatalf("approved question reads %q by %v", listed[0].ReviewStatus, listed[0].ApprovedBy)
	}
	if listed[1].ReviewStatus != "refused" || listed[1].RefusalReason == nil || *listed[1].RefusalReason != "say why" {
		t.Fatalf("refused question reads %q with reason %v", listed[1].ReviewStatus, listed[1].RefusalReason)
	}
	if listed[1].Options[0].ReviewStatus != "refused" || listed[1].Options[1].ReviewStatus != "refused" {
		t.Fatalf("refused Options read %q/%q", listed[1].Options[0].ReviewStatus, listed[1].Options[1].ReviewStatus)
	}

	// THE FIVE SURFACES: the approved question is asked, the refused one is
	// not.
	if ids := publicQuestionIDs(t, env, "review-fest"); len(ids) != 1 || ids[0] != f.size.ID {
		t.Fatalf("the public Event page asks %v, want the approved question alone", ids)
	}
	tickets := saleTickets(t, env, f.staff, f.eventID, f.saleID)
	if len(tickets[0].Questions) != 1 || tickets[0].Questions[0].Question.ID != f.size.ID {
		t.Fatalf("the staff view shows %+v, want the approved question alone", tickets[0].Questions)
	}
	page := listOutstanding(t, env, f.staff, f.eventID)
	if owed := labelsOwedBy(page, f.selfHeldID); len(owed) != 1 || owed[0] != "T-shirt size" || page.OutstandingCount != 2 {
		t.Fatalf("outstanding=%v count=%d, want the approved required question owed on both Tickets", owed, page.OutstandingCount)
	}
	headers := openSalesExportAnswers(t, downloadOK(t, env, f.staff, f.eventID, "")).header
	if !contains(headers, "T-shirt size") || contains(headers, "Meal") {
		t.Fatalf("export headers %v, want the approved question's column and not the refused one's", headers)
	}
	if swept := sweepAnswerReminders(t, env); swept.Sent != 1 {
		t.Fatalf("sweep = %+v, want the buyer chased about the approved question", swept)
	}

	// THE MAIL to the submitter, in English, listing each question's verdict.
	notices := sharedEmail.QuestionReviewsAnswered()
	if len(notices) != 1 || notices[0].To != "admin@example.com" {
		t.Fatalf("answered notices = %+v, want one to the submitter", notices)
	}
	en := notices[0].Subject() + "\n" + notices[0].Text()
	for _, want := range []string{"Review Fest", "operator@example.com", "T-shirt size: approved", "Meal: refused", "say why", "Chicken", "Vegetarian"} {
		if !strings.Contains(en, want) {
			t.Fatalf("English notice lacks %q:\n%s", want, en)
		}
	}

	// A REFUSED QUESTION IS EDITABLE, and its first edit returns it to draft.
	resp, body = patchQuestion(t, env, f.staff, f.eventID, f.ticketTypeID, f.meal, map[string]any{"label": "Meal (no allergens listed)"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("editing the refused question: status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if edited := decodeTicketQuestion(t, body.Data); edited.ReviewStatus != "draft" {
		t.Fatalf("an edited refused question reads %q, want draft", edited.ReviewStatus)
	}

	// RESUBMITTED and approved whole, with the submitter reading Spanish.
	setStaffLocale(t, env, f.staff, "es")
	again := submitQuestionReview(t, env, f.staff, f.eventID)
	sharedEmail.Reset()
	verdicts := make([]map[string]any, 0, len(again.Items))
	for _, item := range again.Items {
		verdicts = append(verdicts, verdict(item.ID, "approved", ""))
	}
	resp, body = env.post(t, operatorQuestionReviewsPath+"/"+again.ID+"/answer",
		map[string]any{"verdicts": verdicts}, authHeader(operator))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("answer again status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if got := reviewStatusOf(t, env, f.staff, f.eventID, f.ticketTypeID, f.meal.ID); got != "approved" {
		t.Fatalf("resubmitted question reads %q, want approved", got)
	}
	if ids := publicQuestionIDs(t, env, "review-fest"); len(ids) != 2 {
		t.Fatalf("the public Event page asks %v, want both questions once both are approved", ids)
	}
	notices = sharedEmail.QuestionReviewsAnswered()
	if len(notices) != 1 {
		t.Fatalf("captured %d answered notices, want 1", len(notices))
	}
	es := notices[0].Subject() + "\n" + notices[0].Text()
	for _, want := range []string{"Review Fest", "Meal (no allergens listed): aprobada", "Chicken: aprobada"} {
		if !strings.Contains(es, want) {
			t.Fatalf("Spanish notice lacks %q:\n%s", want, es)
		}
	}
}

func TestAQuestionReviewLapsesWhenTheEventStarts(t *testing.T) {
	env := setupTest(t)
	f := newQuestionReviewFixture(t, env)
	operator := operatorSession(t, env, "operator@example.com")

	review := submitQuestionReview(t, env, f.admin, f.eventID)
	sharedEmail.Reset()
	if got := outstandingQuestionReviewCount(t, env, operator); got != 1 {
		t.Fatalf("outstanding count = %d, want 1", got)
	}

	// The Event starts, a month on: the Review lapses on read, with no
	// scheduler having run.
	moveClockTo(t, env.fixedClock.Add(30*24*time.Hour))

	if got := outstandingQuestionReviewCount(t, env, operator); got != 0 {
		t.Fatalf("outstanding count = %d once the Event has started, want 0", got)
	}
	if queue := operatorQuestionReviewQueue(t, env, operator); len(queue) != 0 {
		t.Fatalf("queue lists %d rows once the Event has started", len(queue))
	}
	resp, body := env.get(t, operatorQuestionReviewsPath+"/"+review.ID, authHeader(operator))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if lapsed := decodeOperatorQuestionReview(t, body.Data); lapsed.Review.Status != "lapsed" || lapsed.Review.AnsweredAt == nil {
		t.Fatalf("review reads %+v, want lapsed with an end instant", lapsed.Review)
	}
	resp, body = env.post(t, operatorQuestionReviewsPath+"/"+review.ID+"/answer",
		map[string]any{"verdicts": []map[string]any{verdict(review.Items[0].ID, "approved", "")}}, authHeader(operator))
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "QUESTION_REVIEW_NOT_OUTSTANDING" || errorDetail(body, "status") != "lapsed" {
		t.Fatalf("answering a lapsed review: status=%d error=%+v, want 409 QUESTION_REVIEW_NOT_OUTSTANDING lapsed", resp.StatusCode, body.Error)
	}

	// The editor reads the same, and its items are drafts again.
	resp, body = env.get(t, questionReviewsPath(f.eventID)+"/current", authHeader(f.admin))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get current status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if current := decodeQuestionReview(t, body.Data); current.Status != "lapsed" {
		t.Fatalf("current review reads %q, want lapsed", current.Status)
	}
	for _, id := range []string{f.size.ID, f.meal.ID, f.refused.ID} {
		if got := reviewStatusOf(t, env, f.admin, f.eventID, f.ticketTypeID, id); got != "draft" {
			t.Fatalf("lapsed question reads %q, want draft", got)
		}
	}
	resp, body = env.get(t, questionsPath(f.eventID, f.ticketTypeID), authHeader(f.admin))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	for _, q := range decodeTicketQuestions(t, body.Data) {
		if q.ID == f.meal.ID && (q.Options[0].ReviewStatus != "draft" || q.Options[1].ReviewStatus != "draft") {
			t.Fatalf("lapsed Options read %q/%q, want draft", q.Options[0].ReviewStatus, q.Options[1].ReviewStatus)
		}
	}
	if n := len(sharedEmail.QuestionReviewsAnswered()); n != 0 {
		t.Fatalf("%d answered notices for a lapse, want 0", n)
	}
}
