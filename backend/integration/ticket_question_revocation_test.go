package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Operator revokes an approved Ticket Question (#410, parent #404,
// ADR 0056): a Revocation retires the question with a reason the Organization
// is told. It stops being asked from that moment, every Answer it was given
// stays, and the approval stays on the record because it was real.

const operatorEmail = "operator@example.com"

func operatorEventQuestionsPath(eventID string) string {
	return "/api/v1/operator/events/" + eventID + "/ticket-questions"
}

func revokeTicketQuestionPath(questionID string) string {
	return "/api/v1/operator/ticket-questions/" + questionID + "/revoke"
}

// operatorTicketQuestion is one question as the Operator's Event view lists it:
// the staff payload plus the Ticket Type it hangs off.
type operatorTicketQuestion struct {
	ticketQuestion
	TicketTypeID   string `json:"ticket_type_id"`
	TicketTypeName string `json:"ticket_type_name"`
}

// A second Org Admin of the test Organization, who reads Spanish. The
// Revocation notice goes to every Org Admin in their own Mail Locale.
func addSpanishOrgAdmin(t *testing.T, env *testEnv, email string) {
	t.Helper()
	if _, err := env.db.Exec(`
		INSERT INTO members (organization_id, email, role, created_at)
		SELECT id, $1, 'org_admin', $2 FROM organizations WHERE slug = 'test-org'
	`, email, env.fixedClock); err != nil {
		t.Fatalf("add org admin %s: %v", email, err)
	}
	setStaffLocale(t, env, staffSignIn(t, env, email, ""), "es")
}

func TestTheOperatorRevokesAnApprovedTicketQuestion(t *testing.T) {
	env := setupTest(t)
	f := newReviewFixture(t, env)
	approveTicketQuestion(t, env, f.size.ID)
	approveTicketQuestion(t, env, f.meal.ID)
	addSpanishOrgAdmin(t, env, "socia@example.com")
	operator := operatorSession(t, env, operatorEmail)

	// An Answer given under the approval, which the Revocation must keep.
	putAnswer(t, env, f.staff, f.eventID, f.selfHeldID, f.size.ID, map[string]any{"text": "M"})
	sharedEmail.Reset()

	// The Operator can see the Event's questions before deciding.
	var listed []operatorTicketQuestion
	operatorGetOK(t, env, operator, operatorEventQuestionsPath(f.eventID), &listed)
	if len(listed) != 2 || listed[0].ID != f.size.ID || listed[0].TicketTypeName != "GA" || listed[0].ReviewStatus != "approved" {
		t.Fatalf("the Operator's view lists %+v, want the two approved questions on the GA Ticket Type", listed)
	}

	// A question that is not approved cannot be revoked: there is no approval
	// to take back.
	draft := draftTicketQuestion(t, env, f.staff, f.eventID, f.ticketTypeID, map[string]any{
		"label": "Allergies", "kind": "long_text",
	})
	resp, body := env.post(t, revokeTicketQuestionPath(draft.ID), map[string]any{"reason": "Health data"}, authHeader(operator))
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "TICKET_QUESTION_NOT_APPROVED" {
		t.Fatalf("revoking a draft: status=%d error=%+v, want 409 TICKET_QUESTION_NOT_APPROVED", resp.StatusCode, body.Error)
	}

	// The reason is required: it is what the Organization is told.
	resp, body = env.post(t, revokeTicketQuestionPath(f.size.ID), map[string]any{"reason": "   "}, authHeader(operator))
	if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("revoking without a reason: status=%d error=%+v, want 400 VALIDATION_FAILED", resp.StatusCode, body.Error)
	}
	if n := len(sharedEmail.RevokedTicketQuestions); n != 0 {
		t.Fatalf("%d Revocation notices sent by refused requests, want 0", n)
	}

	// An Org Admin cannot: the namespace is the Operator's.
	resp, _ = env.post(t, revokeTicketQuestionPath(f.size.ID), map[string]any{"reason": "Asks for a body measurement"}, authHeader(f.staff))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("an Org Admin revoking: status=%d, want 403", resp.StatusCode)
	}

	// THE REVOCATION.
	resp, body = env.post(t, revokeTicketQuestionPath(f.size.ID), map[string]any{"reason": "  Asks for a body measurement  "}, authHeader(operator))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke status=%d error=%+v", resp.StatusCode, body.Error)
	}
	revoked := decodeTicketQuestion(t, body.Data)
	if !revoked.Retired || revoked.ReviewStatus != "approved" || revoked.RevocationReason == nil || *revoked.RevocationReason != "Asks for a body measurement" {
		t.Fatalf("revoked question reads retired=%v status=%q reason=%v, want retired, still approved, with the trimmed reason", revoked.Retired, revoked.ReviewStatus, revoked.RevocationReason)
	}
	var revokedBy *string
	if err := env.db.QueryRow(`SELECT revoked_by FROM ticket_questions WHERE id = $1`, f.size.ID).Scan(&revokedBy); err != nil {
		t.Fatalf("read revoked_by: %v", err)
	}
	if revokedBy == nil || *revokedBy != operatorEmail {
		t.Fatalf("revoked_by = %v, want the Operator's email", revokedBy)
	}

	// Twice is a conflict: the question is already retired.
	resp, body = env.post(t, revokeTicketQuestionPath(f.size.ID), map[string]any{"reason": "Again"}, authHeader(operator))
	if resp.StatusCode != http.StatusConflict || body.Error == nil || body.Error.Code != "TICKET_QUESTION_RETIRED" {
		t.Fatalf("revoking twice: status=%d error=%+v, want 409 TICKET_QUESTION_RETIRED", resp.StatusCode, body.Error)
	}

	// THE MAIL: every Org Admin of the Organization, each in their Mail Locale.
	notices := sharedEmail.RevokedTicketQuestions
	if len(notices) != 2 {
		t.Fatalf("%d Revocation notices, want one per Org Admin", len(notices))
	}
	byTo := map[string]platform.TicketQuestionRevoked{}
	for _, n := range notices {
		byTo[n.To] = n
	}
	en, ok := byTo["admin@example.com"]
	if !ok || en.Subject() != `A ticket question for "Review Fest" was revoked` {
		t.Fatalf("English notice = %+v, subject %q", en, en.Subject())
	}
	if text := en.Text(); !strings.Contains(text, "T-shirt size") || !strings.Contains(text, "Reason: Asks for a body measurement") || !strings.Contains(text, "Test Org") {
		t.Fatalf("English notice text:\n%s", text)
	}
	es, ok := byTo["socia@example.com"]
	if !ok || es.Subject() != `Se revocó una pregunta de entrada de "Review Fest"` {
		t.Fatalf("Spanish notice = %+v, subject %q", es, es.Subject())
	}
	if text := es.Text(); !strings.Contains(text, "Motivo: Asks for a body measurement") {
		t.Fatalf("Spanish notice text:\n%s", text)
	}

	// THE FIVE SURFACES.
	// 1. The checkout and the public Event page ask the meal alone.
	if ids := publicQuestionIDs(t, env, "review-fest"); len(ids) != 1 || ids[0] != f.meal.ID {
		t.Fatalf("the public Event page asks %v, want the meal alone", ids)
	}
	// 2. The staff answer view keeps the Answer given under the approval.
	tickets := saleTickets(t, env, f.staff, f.eventID, f.saleID)
	answer := answerFor(t, tickets[0], f.size.ID)
	if answer == nil || answer.Text == nil || *answer.Text != "M" {
		t.Fatalf("the staff view lost the Answer to the revoked question: %+v", tickets[0].Questions)
	}
	// 3. The Holder List owes nothing on a question nobody is asked.
	page := listOutstanding(t, env, f.staff, f.eventID)
	if !owesNothing(t, page, f.selfHeldID) || page.OutstandingCount != 0 {
		t.Fatalf("outstanding=%v count=%d, want nothing owed after the Revocation", labelsOwedBy(page, f.selfHeldID), page.OutstandingCount)
	}
	// 4. The Sales Export keeps the column and the Answer in it.
	sheet := openSalesExportAnswers(t, downloadOK(t, env, f.staff, f.eventID, ""))
	if !contains(sheet.header, "T-shirt size") || sheet.cell(t, 0, "T-shirt size") != "M" {
		t.Fatalf("export headers %v rows %v, want the revoked question's column with its Answer", sheet.header, sheet.rows)
	}
	// 5. The Answer Reminder chases nobody about it.
	if swept := sweepAnswerReminders(t, env); swept.Sent != 0 {
		t.Fatalf("sweep = %+v, want nobody chased about a revoked question", swept)
	}

	// The staff editor reads the reason on the retired question.
	resp, body = env.get(t, questionsPath(f.eventID, f.ticketTypeID), authHeader(f.staff))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var staffView []struct {
		ID               string  `json:"id"`
		Retired          bool    `json:"retired"`
		RevocationReason *string `json:"revocation_reason"`
	}
	if err := json.Unmarshal(body.Data, &staffView); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var found bool
	for _, q := range staffView {
		if q.ID == f.size.ID {
			found = true
			if !q.Retired || q.RevocationReason == nil || *q.RevocationReason != "Asks for a body measurement" {
				t.Fatalf("staff editor reads %+v, want retired with the Revocation's reason", q)
			}
		}
	}
	if !found {
		t.Fatalf("the revoked question vanished from the staff editor: %+v", staffView)
	}
}
