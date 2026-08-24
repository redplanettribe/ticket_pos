package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/operator/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Question Review queue's three reads and the one write that answers a
// Review (#407, ADR 0056), on the Payout Request queue's terms: gated by the
// operator allowlist and nothing else, since every row belongs to an
// Organization the Operator is not a Member of.

// answerQuestionReviewBody is the Operator's whole answer: one verdict per
// item the Review carries.
type answerQuestionReviewBody struct {
	Verdicts []questionReviewVerdictBody `json:"verdicts"`
}

// questionReviewVerdictBody is one ruling: `approved`, or `refused` with a
// reason.
type questionReviewVerdictBody struct {
	ItemID  string `json:"item_id"`
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

// ListQuestionReviews returns the cross-Organization queue of outstanding
// Reviews.
//
// @Summary      List every outstanding Question Review, oldest first
// @Description  Returns a page of every OUTSTANDING Question Review across every Organization on the platform — the Operator's second work queue, beside the Payout Requests (ADR 0056). Ordered OLDEST FIRST, on the Payout Request queue's reasoning: the Review that has waited longest is the one whose Event is nearest. Each row carries the Review (status, note, acknowledgement instant, who submitted it and when, how many questions it carries — Options not counted — and its items without their shapes) with the Organization and the Event, including the Event's start, which is the instant the Review lapses. THE LAPSE IS DECIDED ON THIS READ: a Review whose Event has started is marked `lapsed` and its questions returned to draft before the page is built, so nothing here is ever past answering. Answered, withdrawn and lapsed Reviews are not in the queue; they stay readable by id. Response is the ADR-0006 nested envelope { data, pagination }; page_size defaults to 50 (max 100) and page floors at 1. 404 TICKET_QUESTIONS_UNAVAILABLE while the feature is dark (ADR 0045). Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        page       query  int  false  "Page number (1-based; floors at 1)"
// @Param        page_size  query  int  false  "Page size (default 50, max 100)"
// @Success      200  {object}  openapi.EnvelopeOperatorQuestionReviewQueue
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/question-reviews [get]
func (h *Handler) ListQuestionReviews(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	query := r.URL.Query()
	queue, err := h.svc.QuestionReviewQueue(r.Context(), pageParam(query.Get("page")), pageSizeParam(query.Get("page_size")))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, queue)
}

// CountOutstandingQuestionReviews returns the backlog as one number.
//
// @Summary      Count the outstanding Question Reviews
// @Description  Returns outstanding_count: how many Question Reviews are waiting on a Platform Operator across every Organization, the badge the Operator Dashboard wears beside the Payout Requests' (ADR 0056). Reviews whose Event has started are lapsed before counting, so it counts exactly what the queue lists. Zero is an ordinary answer. 404 TICKET_QUESTIONS_UNAVAILABLE while the feature is dark. Read-only. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeOperatorOutstandingQuestionReviewCount
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/question-reviews/count [get]
func (h *Handler) CountOutstandingQuestionReviews(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	count, err := h.svc.OutstandingQuestionReviewCount(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, count)
}

// GetQuestionReview returns one Review in full.
//
// @Summary      Get one Question Review with every item's question and Option
// @Description  Returns one Question Review whole, in any state: the Review with its Organization and Event (name, start instant, timezone), and each item carrying the WHOLE question it names — label, kind, required-ness, timing, every Option with its own review columns, and the Ticket Type it hangs off — plus, for an Option item, the Option itself, so a ruling on "Chicken" is read under the question that offers it. Verdicts and reasons are on the items once answered and absent until then. The lapse is decided on this read: a Review whose Event has started reads `lapsed`, with answered_by naming the lapse and its questions back in draft. An unknown or malformed id is 404 QUESTION_REVIEW_NOT_FOUND; 404 TICKET_QUESTIONS_UNAVAILABLE while the feature is dark. Read-only. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        reviewID  path  string  true  "Question Review ID"
// @Success      200  {object}  openapi.EnvelopeOperatorQuestionReview
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/question-reviews/{reviewID} [get]
func (h *Handler) GetQuestionReview(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	review, err := h.svc.GetQuestionReview(r.Context(), strings.TrimSpace(r.PathValue("reviewID")))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, review)
}

// AnswerQuestionReview records the Operator's verdicts as one act.
//
// @Summary      Answer a Question Review: a verdict per item, refusals with a reason
// @Description  Records the Operator's answer to one outstanding Question Review as ONE ACT with a verdict PER ITEM (ADR 0056): every question and Option the Review carries is `approved`, or `refused` with a reason the Organization reads. Who answered is taken from the Staff Session, never from the body. THE BODY IS CHECKED WHOLE BEFORE ANYTHING IS WRITTEN and each refusal names the item in its details: an item without a verdict is 400 QUESTION_REVIEW_VERDICT_REQUIRED, a refusal without a reason is 400 QUESTION_REVIEW_REASON_REQUIRED, and a verdict naming an item the Review does not carry is 400 QUESTION_REVIEW_UNKNOWN_ITEM. Reasons are trimmed and bounded at 500 characters. On success, in one transaction: the Review becomes `answered` with answered_by/answered_at, each item takes its verdict and reason, each approved question and Option becomes `approved` with the Operator's authorship and starts collecting on the spot — asked at checkout and in the Customer Area, chased by the Answer Reminder, counted on the Holder List, given a column in the Sales Export — and each refused one becomes `refused` carrying the reason, shown in the Organization's editor, asked of nobody, and editable: its first edit returns it to `draft`. The submitter is mailed the verdicts, item by item, in their Mail Locale. A Review that is no longer outstanding — answered by a colleague, withdrawn by the Organization, or lapsed because its Event started (decided on this call, no scheduler) — is 409 QUESTION_REVIEW_NOT_OUTSTANDING with the state reached in the details. 404 QUESTION_REVIEW_NOT_FOUND for an unknown or malformed id. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        reviewID  path  string                    true  "Question Review ID"
// @Param        body      body  answerQuestionReviewBody  true  "One verdict per item"
// @Success      200  {object}  openapi.EnvelopeOperatorQuestionReview
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/operator/question-reviews/{reviewID}/answer [post]
func (h *Handler) AnswerQuestionReview(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body answerQuestionReviewBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	verdicts := make([]service.QuestionReviewVerdict, 0, len(body.Verdicts))
	for i, v := range body.Verdicts {
		// A refusal's reason is held to the Payout Request decline's bound;
		// whether one is REQUIRED is the service's ruling, which names the item.
		reason := strings.TrimSpace(v.Reason)
		if len([]rune(reason)) > resolutionReasonMaxLength {
			_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{
				Field:   "verdicts[" + strconv.Itoa(i) + "].reason",
				Message: "Reason must be at most 500 characters",
			}})
			return
		}
		verdicts = append(verdicts, service.QuestionReviewVerdict{
			ItemID:  strings.TrimSpace(v.ItemID),
			Verdict: strings.TrimSpace(v.Verdict),
			Reason:  reason,
		})
	}

	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	review, err := h.svc.AnswerQuestionReview(r.Context(), strings.TrimSpace(r.PathValue("reviewID")), service.AnswerQuestionReviewInput{
		Verdicts: verdicts,
		Operator: session.Email,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, review)
}
