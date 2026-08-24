package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/service"
	identitymiddleware "github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Question Review's staff routes (#406, parent #404, ADR 0056).

// questionReviewBody is what a submission states: the Organization's note, and
// its acknowledgement of what it is choosing to collect.
type questionReviewBody struct {
	Note         string `json:"note"`
	Acknowledged bool   `json:"acknowledged"`
}

// SubmitQuestionReview submits an Event's draft questions as one Question Review.
//
// @Summary      Submit a Question Review
// @Description  Submits the Event's draft and refused Ticket Questions — and their draft Options — as one Question Review for a Platform Operator to answer (ADR 0056). Moves every carried row to `under_review`; approved questions keep collecting meanwhile. Requires `acknowledged: true`, the Organization's recorded affirmation of what it is choosing to collect (400 QUESTION_REVIEW_ACKNOWLEDGEMENT_REQUIRED without it). Refused with 409 QUESTION_REVIEW_OUTSTANDING while the Event already has one waiting, 409 QUESTION_REVIEW_EVENT_STARTED once the Event has started, and 409 QUESTION_REVIEW_NOTHING_TO_REVIEW when no draft exists. Every allowlisted Platform Operator is mailed in their Staff Locale. Org Admin or Event Owner; answers 404 while the Ticket Question feature flag is off.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      string              true  "Event ID"
// @Param        body  body      questionReviewBody  true  "Note and acknowledgement"
// @Success      201  {object}  openapi.EnvelopeQuestionReview
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/question-reviews [post]
func (h *Handler) SubmitQuestionReview(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ok := pathValueRequired(w, r, reqID, "id")
	if !ok {
		return
	}

	var body questionReviewBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	member, _ := identitymiddleware.ActiveMemberFromContext(r.Context())
	review, err := h.svc.SubmitQuestionReview(r.Context(), actorFromRequest(r), eventID, service.SubmitQuestionReviewInput{
		Note:         body.Note,
		Acknowledged: body.Acknowledged,
		SubmittedBy:  member.Email,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusCreated, review)
}

// WithdrawQuestionReview takes an outstanding Question Review back.
//
// A POST to a verb and not a DELETE, because nothing is deleted: the Review
// stays in the history saying it was asked and withdrawn.
//
// @Summary      Withdraw a Question Review
// @Description  Withdraws the Event's outstanding Question Review (ADR 0056): the Review moves to `withdrawn`, stamped with the withdrawing Member's email and the instant, and every question and Option it carried returns to `draft`. A compare-and-swap on the outstanding state: a Review already answered, withdrawn or lapsed is refused with 409 QUESTION_REVIEW_NOT_OUTSTANDING naming the state it reached, and one that is not this Event's is 404 QUESTION_REVIEW_NOT_FOUND. Org Admin or Event Owner.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id        path  string  true  "Event ID"
// @Param        reviewId  path  string  true  "Question Review ID"
// @Success      200  {object}  openapi.EnvelopeQuestionReview
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/question-reviews/{reviewId}/withdraw [post]
func (h *Handler) WithdrawQuestionReview(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ok := pathValueRequired(w, r, reqID, "id")
	if !ok {
		return
	}
	reviewID := strings.TrimSpace(r.PathValue("reviewId"))
	// The column is UUID, so a malformed id would otherwise surface as a
	// database error rather than the not-found it is.
	if _, err := uuid.Parse(reviewID); err != nil {
		_ = platform.WriteDomainError(w, reqID, catalog.ErrQuestionReviewNotFound())
		return
	}

	member, _ := identitymiddleware.ActiveMemberFromContext(r.Context())
	review, err := h.svc.WithdrawQuestionReview(r.Context(), actorFromRequest(r), eventID, reviewID, member.Email)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, review)
}

// GetCurrentQuestionReview returns the Event's outstanding Question Review, or
// the last one submitted.
//
// @Summary      Get the Event's current Question Review
// @Description  Returns the Event's outstanding Question Review with its items, or — when none is outstanding — the last one submitted, for the staff editor's banner (ADR 0056). 404 QUESTION_REVIEW_NOT_FOUND when the Event has never had one. Org Admin or Event Owner.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  string  true  "Event ID"
// @Success      200  {object}  openapi.EnvelopeQuestionReview
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/question-reviews/current [get]
func (h *Handler) GetCurrentQuestionReview(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ok := pathValueRequired(w, r, reqID, "id")
	if !ok {
		return
	}
	review, err := h.svc.GetCurrentQuestionReview(r.Context(), actorFromRequest(r), eventID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, review)
}
