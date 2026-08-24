package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/operator/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// revokeTicketQuestionBody is the Revocation's one field.
type revokeTicketQuestionBody struct {
	Reason string `json:"reason"`
}

// ListEventTicketQuestions returns every Ticket Question of one Event.
//
// @Summary      List an Event's Ticket Questions for the Platform Operator
// @Description  Returns every Ticket Question of the Event across its Ticket Types, in every review state and retired ones included, each with its Options and its review columns (`review_status`, `approved_by`, `refusal_reason`, `revocation_reason`) and the Ticket Type it hangs off. This is where the Operator sees what an Organization is asking and takes an approval back (ADR 0056). Not scoped to an Organization: the operator allowlist is the whole of the gate. 404 EVENT_NOT_FOUND for an unknown or malformed id; 404 TICKET_QUESTIONS_UNAVAILABLE while the feature is dark (ADR 0045). Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        eventID  path  string  true  "Event ID"
// @Success      200  {object}  openapi.EnvelopeOperatorTicketQuestions
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/events/{eventID}/ticket-questions [get]
func (h *Handler) ListEventTicketQuestions(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	questions, err := h.svc.ListEventTicketQuestions(r.Context(), strings.TrimSpace(r.PathValue("eventID")))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, questions)
}

// RevokeTicketQuestion takes an approval back, with a reason the Organization
// is told.
//
// @Summary      Revoke an approved Ticket Question, with a reason the Organization reads
// @Description  A Revocation (ADR 0056): retires the question and records who revoked it (taken from the Staff Session, never from the body), when, and why. `review_status` stays `approved` because the approval was real; `retired` becomes true and `revocation_reason` carries the reason. From that moment the question is asked of nobody — gone from the checkout, the public Event page, the Customer Area, the Holder List's Outstanding Answers and the Answer Reminder — while every Answer already given stays on its Ticket, on the staff answer view and in the Sales Export. Every Org Admin of the Organization is mailed the reason in their own language. THE REASON IS REQUIRED — blank or whitespace-only is refused with a field error — and bounded at 500 characters. Only an approved, live question can be revoked: a draft, under-review or refused one is 409 TICKET_QUESTION_NOT_APPROVED, and one already retired is 409 TICKET_QUESTION_RETIRED. 404 TICKET_QUESTION_NOT_FOUND for an unknown or malformed id. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        questionID  path  string                    true  "Ticket Question ID"
// @Param        body        body  revokeTicketQuestionBody  true  "Why the approval is taken back"
// @Success      200  {object}  openapi.EnvelopeOperatorRevokedTicketQuestion
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/operator/ticket-questions/{questionID}/revoke [post]
func (h *Handler) RevokeTicketQuestion(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body revokeTicketQuestionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	// The same rule the Payout Request decline holds its reason to: required,
	// trimmed, 500 characters, shown to the organizer.
	reason, fields := validateResolutionReason(body.Reason)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	question, err := h.svc.RevokeTicketQuestion(r.Context(), strings.TrimSpace(r.PathValue("questionID")), service.RevokeTicketQuestionInput{
		Reason:   reason,
		Operator: session.Email,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, question)
}
