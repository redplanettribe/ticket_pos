package integration

import (
	"net/http"
	"testing"
	"time"
)

// A Ticket Question is born a draft and is asked of nobody until a Platform
// Operator has approved it (#405, parent #404, ADR 0056). Five surfaces ask,
// offer, remind, count or export questions, and ADR 0056 made their agreement
// an invariant to assert: this file walks all five for a fresh question, then
// again after the row is approved.
//
// APPROVAL HERE IS AN UPDATE, signed testApprovedBy. The Question Review that
// approves for real is #406-#410; until it exists the only honest stand-in is
// SQL, because approval is never a column default and never a route an
// Organization can reach (migration 089).

// reviewFixture is one published Event with a two-Ticket online Sale, whose
// buyer holds Ticket 1 (ADR 0048) and skipped every question — the one buyer
// an Answer Reminder can reach and the one Ticket a Holder List can chase.
type reviewFixture struct {
	staff        string
	eventID      string
	ticketTypeID string
	saleID       string
	selfHeldID   string
	// size is a required short_text draft; meal a single_choice draft with two
	// draft Options. Both were sent to the checkout below, and both were
	// dropped, because a draft is asked of nobody.
	size, meal ticketQuestion
}

func newReviewFixture(t *testing.T, env *testEnv) reviewFixture {
	t.Helper()
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	f := reviewFixture{}
	f.staff = orgAdminSession(t, env)
	f.eventID, f.ticketTypeID = publishCheckoutEvent(t, env, f.staff, "Review Fest", "review-fest", 2000, 50)
	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2 WHERE id = $1`,
		f.eventID, env.fixedClock.Add(30*24*time.Hour)); err != nil {
		t.Fatalf("set the Event's start: %v", err)
	}

	f.size = draftTicketQuestion(t, env, f.staff, f.eventID, f.ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})
	f.meal = draftTicketQuestion(t, env, f.staff, f.eventID, f.ticketTypeID, map[string]any{
		"label": "Meal", "kind": "single_choice", "option_labels": []string{"Chicken", "Vegetarian"},
	})

	// The buyer answers both drafts anyway — a stale form, or a crafted body.
	body := checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(f.ticketTypeID, 2))
	body["answers"] = []map[string]any{
		checkoutAnswer(f.ticketTypeID, 1, f.size.ID, map[string]any{"text": "M"}),
		checkoutAnswer(f.ticketTypeID, 1, f.meal.ID, map[string]any{"option_ids": []string{f.meal.Options[0].ID}}),
	}
	begun := beginCheckoutOK(t, env, "test-org", "review-fest", body)
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	f.saleID = saleIDOfPayment(t, env, begun.ClientTransactionID)
	f.selfHeldID = ticketIDsOfSale(t, env, f.saleID)[0]
	sharedEmail.Reset()
	return f
}

// publicQuestionIDs is what the public Event page asks, as ids.
func publicQuestionIDs(t *testing.T, env *testEnv, slug string) []string {
	t.Helper()
	page := publicEventQuestions(t, env, slug)
	var ids []string
	for _, tt := range page.TicketTypes {
		for _, q := range tt.TicketQuestions {
			ids = append(ids, q.ID)
		}
	}
	return ids
}

func TestATicketQuestionIsBornADraftAndAskedOfNobodyUntilApproved(t *testing.T) {
	env := setupTest(t)
	f := newReviewFixture(t, env)

	// Born a draft, with no author on an approval nobody has given.
	if f.size.ReviewStatus != "draft" || f.size.ApprovedBy != nil {
		t.Fatalf("a new question is %q approved by %v, want a draft with no approver", f.size.ReviewStatus, f.size.ApprovedBy)
	}
	if f.meal.Options[0].ReviewStatus != "draft" || f.meal.Options[0].ApprovedBy != nil {
		t.Fatalf("a new Option is %q approved by %v, want a draft with no approver", f.meal.Options[0].ReviewStatus, f.meal.Options[0].ApprovedBy)
	}

	// 1. The checkout and the public Event page: asked of nobody, and what the
	//    buyer sent anyway was dropped.
	if ids := publicQuestionIDs(t, env, "review-fest"); len(ids) != 0 {
		t.Fatalf("the public Event page asks %v, want nothing while every question is a draft", ids)
	}
	if n := countTicketAnswers(t, env); n != 0 {
		t.Fatalf("%d Answers stored against draft questions, want 0", n)
	}

	// 2. The staff answer view (and through the same body, the Customer Area's):
	//    no question to pair an Answer with, and nothing can be said in reply.
	tickets := saleTickets(t, env, f.staff, f.eventID, f.saleID)
	if len(tickets) != 2 || len(tickets[0].Questions) != 0 {
		t.Fatalf("the staff view shows %d questions on a Ticket, want 0 while every question is a draft", len(tickets[0].Questions))
	}
	resp, body := env.put(t, answerPath(f.eventID, f.selfHeldID, f.size.ID),
		map[string]any{"text": "L"}, authHeader(f.staff))
	if resp.StatusCode != http.StatusNotFound || body.Error.Code != "TICKET_QUESTION_NOT_FOUND" {
		t.Fatalf("answering a draft: status=%d error=%+v, want 404 TICKET_QUESTION_NOT_FOUND", resp.StatusCode, body.Error)
	}

	// 3. The Holder List and its Outstanding Answer count: a required draft is
	//    owed by nobody.
	page := listOutstanding(t, env, f.staff, f.eventID)
	if !owesNothing(t, page, f.selfHeldID) || page.OutstandingCount != 0 {
		t.Fatalf("outstanding=%v count=%d, want nothing owed on a draft", labelsOwedBy(page, f.selfHeldID), page.OutstandingCount)
	}

	// 4. The Sales Export: a draft earns no column, so the Event "asks nothing"
	//    and the answers sheet is not there. (Ticket Assignment is open, so the
	//    sheet exists for its Holder columns; what it must not carry is a
	//    question column.) Nothing asked, no question heading.
	if got := sheetNames(t, downloadOK(t, env, f.staff, f.eventID, "")); len(got) != 3 {
		t.Fatalf("sheets = %v, want the Holder sheet alone beside the two", got)
	}
	if headers := openSalesExportAnswers(t, downloadOK(t, env, f.staff, f.eventID, "")).header; contains(headers, "T-shirt size") {
		t.Fatalf("export headers %v carry a draft question's column", headers)
	}

	// 5. The Answer Reminder: nobody is chased about a question nobody asked.
	if swept := sweepAnswerReminders(t, env); swept.Sent != 0 || swept.Due != 0 {
		t.Fatalf("sweep = %+v, want nothing due while every question is a draft", swept)
	}
	assertNoReminders(t, env, f.saleID, "every question is a draft")

	// THE VERDICT, and the same five surfaces again.
	approveTicketQuestion(t, env, f.size.ID)
	approveTicketQuestion(t, env, f.meal.ID)

	resp, body = env.get(t, questionsPath(f.eventID, f.ticketTypeID), authHeader(f.staff))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	listed := decodeTicketQuestions(t, body.Data)
	if listed[0].ReviewStatus != "approved" || listed[0].ApprovedBy == nil || *listed[0].ApprovedBy != testApprovedBy {
		t.Fatalf("approved question reads %q by %v", listed[0].ReviewStatus, listed[0].ApprovedBy)
	}
	if listed[1].Options[1].ReviewStatus != "approved" {
		t.Fatalf("approved Option reads %q", listed[1].Options[1].ReviewStatus)
	}

	if ids := publicQuestionIDs(t, env, "review-fest"); len(ids) != 2 || ids[0] != f.size.ID || ids[1] != f.meal.ID {
		t.Fatalf("the public Event page asks %v, want the two approved questions", ids)
	}
	tickets = saleTickets(t, env, f.staff, f.eventID, f.saleID)
	if len(tickets[0].Questions) != 2 || len(tickets[0].Questions[1].Question.Options) != 2 {
		t.Fatalf("the staff view shows %+v, want both approved questions with the meal's two Options", tickets[0].Questions)
	}
	page = listOutstanding(t, env, f.staff, f.eventID)
	if owed := labelsOwedBy(page, f.selfHeldID); len(owed) != 1 || owed[0] != "T-shirt size" || page.OutstandingCount != 2 {
		t.Fatalf("outstanding=%v count=%d, want the required question owed on both Tickets", owed, page.OutstandingCount)
	}
	if headers := openSalesExportAnswers(t, downloadOK(t, env, f.staff, f.eventID, "")).header; !contains(headers, "T-shirt size") || !contains(headers, "Meal") {
		t.Fatalf("export headers %v lack the approved questions' columns", headers)
	}
	if swept := sweepAnswerReminders(t, env); swept.Sent != 1 {
		t.Fatalf("sweep = %+v, want the buyer chased once their question is approved", swept)
	}
	reminderFor(t, "ana@example.com")

	// An Option added to an approved question is a draft: the question keeps
	// collecting in its approved shape, and the new Option is offered to nobody.
	resp, body = env.post(t, optionsPath(f.eventID, f.ticketTypeID, f.meal.ID),
		map[string]any{"label": "Fish"}, authHeader(f.staff))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add option status=%d error=%+v", resp.StatusCode, body.Error)
	}
	fish := decodeTicketQuestion(t, body.Data)
	if fish.Options[2].ReviewStatus != "draft" {
		t.Fatalf("an Option added to an approved question is %q, want draft", fish.Options[2].ReviewStatus)
	}
	if offered := publicEventQuestions(t, env, "review-fest").TicketTypes[0].TicketQuestions[1].Options; len(offered) != 2 {
		t.Fatalf("the public Event page offers %d Options, want the two approved ones", len(offered))
	}
	resp, body = env.put(t, answerPath(f.eventID, f.selfHeldID, f.meal.ID),
		map[string]any{"option_ids": []string{fish.Options[2].ID}}, authHeader(f.staff))
	if resp.StatusCode == http.StatusOK || body.Error.Code != "ANSWER_OPTION_NOT_OFFERED" {
		t.Fatalf("choosing a draft Option: status=%d error=%+v, want ANSWER_OPTION_NOT_OFFERED", resp.StatusCode, body.Error)
	}
}

// The six questions in production are grandfathered by migration 090, which
// records an explicit approval attributed to itself — the one way a question is
// ever born approved (ADR 0056). A question authored after it is a draft.
func TestTheGrandfatheringMigrationApprovesWhatExistsAndNothingAfter(t *testing.T) {
	env := setupTest(t)
	sessionID, eventID, ticketTypeID := ticketQuestionFixture(t, env)
	enableTicketQuestions(t)

	before := draftTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "Meal", "kind": "single_choice", "option_labels": []string{"Chicken", "Vegetarian"},
	})

	executeMigration(t, env, "090_backfill_ticket_question_approval.sql")

	after := draftTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text",
	})

	resp, body := env.get(t, questionsPath(eventID, ticketTypeID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	listed := decodeTicketQuestions(t, body.Data)
	if len(listed) != 2 || listed[0].ID != before.ID || listed[1].ID != after.ID {
		t.Fatalf("listed %+v", listed)
	}
	const migration = "migration:090_backfill_ticket_question_approval"
	if listed[0].ReviewStatus != "approved" || listed[0].ApprovedBy == nil || *listed[0].ApprovedBy != migration {
		t.Fatalf("the pre-existing question reads %q approved by %v, want approved by %q", listed[0].ReviewStatus, listed[0].ApprovedBy, migration)
	}
	for _, option := range listed[0].Options {
		if option.ReviewStatus != "approved" || option.ApprovedBy == nil || *option.ApprovedBy != migration {
			t.Fatalf("the pre-existing Option %q reads %q approved by %v", option.Label, option.ReviewStatus, option.ApprovedBy)
		}
	}
	if listed[1].ReviewStatus != "draft" || listed[1].ApprovedBy != nil {
		t.Fatalf("the question authored after the migration reads %q approved by %v, want a draft", listed[1].ReviewStatus, listed[1].ApprovedBy)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
