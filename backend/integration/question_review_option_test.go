package integration

import (
	"net/http"
	"strings"
	"testing"
)

// An Option added to an approved choice question waits as a draft and rides
// the next Question Review (#409, parent #404, ADR 0056): offered to nobody
// on any answer surface while the question keeps collecting in its approved
// shape, carried as an Option item under its question, and after the verdict
// either offered everywhere or retired with the Operator's reason.

// optionLabelsOf lists a question's Options by label, in order.
func optionLabelsOf(q ticketQuestion) []string {
	labels := make([]string, 0, len(q.Options))
	for _, o := range q.Options {
		labels = append(labels, o.Label)
	}
	return labels
}

// addOption adds one Option to a question and returns the Option as the
// authoring route reads it back.
func addOption(t *testing.T, env *testEnv, session, eventID, ticketTypeID, questionID, label string) ticketQuestionOption {
	t.Helper()
	resp, body := env.post(t, optionsPath(eventID, ticketTypeID, questionID), map[string]any{"label": label}, authHeader(session))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add option %q: status=%d error=%+v", label, resp.StatusCode, body.Error)
	}
	for _, option := range decodeTicketQuestion(t, body.Data).Options {
		if option.Label == label {
			return option
		}
	}
	t.Fatalf("the added Option %q is not on the question", label)
	return ticketQuestionOption{}
}

func TestAnOptionAddedToAnApprovedQuestionRidesTheNextReview(t *testing.T) {
	env := setupTest(t)
	f := newReviewFixture(t, env)
	operator := operatorSession(t, env, "operator@example.com")
	approveTicketQuestion(t, env, f.size.ID)
	approveTicketQuestion(t, env, f.meal.ID)

	// The buyer's second Ticket goes to Carla, whose Assignment Link is the
	// fourth answer surface.
	ana := buyerSession(t, env, "ana@example.com")
	assignedID := ticketIDsOfSale(t, env, f.saleID)[1]
	assignTicketOK(t, env, ana, f.saleID, assignedID, "carla@example.com")
	carlaToken := assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com"))

	// ADDED TO AN APPROVED QUESTION: a draft, on a question still approved.
	fish := addOption(t, env, f.staff, f.eventID, f.ticketTypeID, f.meal.ID, "Fish")
	if fish.ReviewStatus != "draft" || fish.ApprovedBy != nil {
		t.Fatalf("the added Option reads %q approved by %v, want a draft", fish.ReviewStatus, fish.ApprovedBy)
	}
	if got := reviewStatusOf(t, env, f.staff, f.eventID, f.ticketTypeID, f.meal.ID); got != "approved" {
		t.Fatalf("the parent question reads %q after an Option was added, want still approved", got)
	}

	approvedOnly := []string{"Chicken", "Vegetarian"}
	assertOffered := func(want []string, when string) {
		t.Helper()
		public := publicEventQuestions(t, env, "review-fest").TicketTypes[0].TicketQuestions
		var offered []string
		if len(public) == 2 {
			for _, o := range public[1].Options {
				offered = append(offered, o.Label)
			}
		}
		if !equalStrings(offered, want) {
			t.Fatalf("%s: checkout offers %v, want %v", when, offered, want)
		}
		staff := saleTickets(t, env, f.staff, f.eventID, f.saleID)[0].Questions
		if len(staff) != 2 || !equalStrings(optionLabelsOf(staff[1].Question), want) {
			t.Fatalf("%s: the staff view offers %v, want %v", when, optionLabelsOf(staff[1].Question), want)
		}
		held, _ := listHeldTickets(t, env, ana)
		if len(held) != 1 || len(held[0].Questions) != 2 || !equalStrings(optionLabelsOf(held[0].Questions[1].Question), want) {
			t.Fatalf("%s: the buyer's held Ticket offers %+v, want %v", when, held, want)
		}
		link := acceptAssignmentOK(t, env, carlaToken)
		if len(link.Questions) != 2 || !equalStrings(optionLabelsOf(link.Questions[1].Question), want) {
			t.Fatalf("%s: the Assignment Link offers %v, want %v", when, optionLabelsOf(link.Questions[1].Question), want)
		}
	}
	assertOffered(approvedOnly, "with the Option a draft")
	sheet := openSalesExportAnswers(t, downloadOK(t, env, f.staff, f.eventID, ""))
	if !contains(sheet.header, "Meal") || strings.Contains(strings.Join(sheet.header, "|"), "Fish") {
		t.Fatalf("export headers %v, want the Meal column and no trace of the draft Option", sheet.header)
	}
	resp, body := answerHeldTicket(t, env, ana, f.selfHeldID, f.meal.ID, map[string]any{"option_ids": []string{fish.ID}})
	if resp.StatusCode == http.StatusOK || body.Error == nil || body.Error.Code != "ANSWER_OPTION_NOT_OFFERED" {
		t.Fatalf("choosing the draft Option: status=%d error=%+v, want ANSWER_OPTION_NOT_OFFERED", resp.StatusCode, body.Error)
	}

	// SUBMITTED: the Review carries the Option alone, as an item under its
	// question, and the Operator's detail shows which question offers it.
	submitted := submitQuestionReview(t, env, f.staff, f.eventID)
	if len(submitted.Items) != 1 || submitted.Items[0].QuestionID != f.meal.ID || submitted.Items[0].OptionID == nil || *submitted.Items[0].OptionID != fish.ID {
		t.Fatalf("submitted items = %+v, want the one Option item under the Meal question", submitted.Items)
	}
	if got := reviewStatusOf(t, env, f.staff, f.eventID, f.ticketTypeID, f.meal.ID); got != "approved" {
		t.Fatalf("the parent question reads %q while its Option is under review, want approved", got)
	}
	assertOffered(approvedOnly, "with the Option under review")
	resp, body = env.get(t, operatorQuestionReviewsPath+"/"+submitted.ID, authHeader(operator))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail status=%d error=%+v", resp.StatusCode, body.Error)
	}
	detail := decodeOperatorQuestionReview(t, body.Data)
	if len(detail.Review.Items) != 1 || detail.Review.QuestionCount != 0 {
		t.Fatalf("detail = %+v, want one Option item and no question", detail.Review)
	}
	item := detail.Review.Items[0]
	if item.Option == nil || item.Option.Label != "Fish" || item.Question == nil || item.Question.Label != "Meal" || item.Question.Kind != "single_choice" {
		t.Fatalf("item = %+v, want the Fish Option under the Meal question", item)
	}

	// APPROVED: offered everywhere, and choosable.
	resp, body = env.post(t, operatorQuestionReviewsPath+"/"+submitted.ID+"/answer",
		map[string]any{"verdicts": []map[string]any{verdict(item.ID, "approved", "")}}, authHeader(operator))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve status=%d error=%+v", resp.StatusCode, body.Error)
	}
	withFish := []string{"Chicken", "Vegetarian", "Fish"}
	assertOffered(withFish, "once the Option is approved")
	answerHeldTicketOK(t, env, ana, f.selfHeldID, f.meal.ID, map[string]any{"option_ids": []string{fish.ID}})
	sheet = openSalesExportAnswers(t, downloadOK(t, env, f.staff, f.eventID, ""))
	if !strings.Contains(strings.Join(flatten(sheet.rows), "|"), "Fish") {
		t.Fatalf("export rows %v, want the chosen Option's words", sheet.rows)
	}

	// REFUSED: retired, with the reason on the item and on the Option, and the
	// question still approved and still asked with what it had.
	beef := addOption(t, env, f.staff, f.eventID, f.ticketTypeID, f.meal.ID, "Beef")
	again := submitQuestionReview(t, env, f.staff, f.eventID)
	beefItem := itemOf(t, again, f.meal.ID, &beef.ID)
	resp, body = env.post(t, operatorQuestionReviewsPath+"/"+again.ID+"/answer",
		map[string]any{"verdicts": []map[string]any{verdict(beefItem, "refused", "no beef")}}, authHeader(operator))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refuse status=%d error=%+v", resp.StatusCode, body.Error)
	}
	refused := decodeOperatorQuestionReview(t, body.Data).Review.Items[0]
	if refused.Verdict == nil || *refused.Verdict != "refused" || refused.Reason == nil || *refused.Reason != "no beef" {
		t.Fatalf("refused item = %+v, want refused with the reason", refused)
	}
	resp, body = env.get(t, questionsPath(f.eventID, f.ticketTypeID), authHeader(f.staff))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	listed := decodeTicketQuestions(t, body.Data)
	if listed[1].ReviewStatus != "approved" || len(listed[1].Options) != 4 {
		t.Fatalf("the Meal question reads %q with %d Options, want approved with all four listed", listed[1].ReviewStatus, len(listed[1].Options))
	}
	retired := listed[1].Options[3]
	if retired.Label != "Beef" || !retired.Retired || retired.ReviewStatus != "refused" || retired.RefusalReason == nil || *retired.RefusalReason != "no beef" {
		t.Fatalf("the refused Option reads %+v, want retired and refused with the reason", retired)
	}
	assertOffered(withFish, "once the Option is refused")
	resp, body = answerHeldTicket(t, env, ana, f.selfHeldID, f.meal.ID, map[string]any{"option_ids": []string{beef.ID}})
	if resp.StatusCode == http.StatusOK || body.Error == nil || body.Error.Code != "ANSWER_OPTION_NOT_OFFERED" {
		t.Fatalf("choosing the refused Option: status=%d error=%+v, want ANSWER_OPTION_NOT_OFFERED", resp.StatusCode, body.Error)
	}
	// A retired Option is nothing to resubmit.
	resp, body = env.post(t, questionReviewsPath(f.eventID), map[string]any{"acknowledged": true}, authHeader(f.staff))
	if resp.StatusCode == http.StatusCreated {
		t.Fatalf("a refused Option was carried into a fresh Review: %+v", decodeQuestionReview(t, body.Data).Items)
	}
}

func flatten(rows [][]string) []string {
	var out []string
	for _, row := range rows {
		out = append(out, row...)
	}
	return out
}
