package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// PurgeAbandonedAnswers runs the Abandoned Answer Purge: the Answers a buyer
// typed into a checkout that never became a sale, deleted 30 days on (#316,
// ADR 0044).
//
// INTERNAL SERVICE-TO-SERVICE, on exactly the terms DrainReversalRequests sets
// out: the API is deployed --no-allow-unauthenticated, so Cloud Run IAM rejects
// any caller without a Google-signed OIDC ID token — which Cloud Scheduler
// presents in `Authorization` — before the container is reached (ADR 0008). The
// route carries no application middleware because there is no application
// credential that grants this, and none is read here.
//
// There is nothing to validate and nothing the caller may aim. The body is
// empty, the path carries nothing, and — the point of this particular endpoint —
// THE WINDOW IS NOT THE CALLER'S TO NAME. A cutoff parameter would turn a
// retention job into a delete-every-Answer-on-the-platform button, reachable by
// anything that ever obtained the token.
//
// WHAT IT PUTS IN THE 200 BODY IS COUNTS AND A TIMESTAMP, and unlike the
// Reversal Reconciler's Unresolved queue it names no Payment, no buyer and no
// Answer. That is not an accident of it having nothing useful to say: an Answer
// is the thing this whole feature treats as potentially health data, so a
// response that listed what it had just deleted would publish precisely what the
// deletion exists to remove. Nobody may widen this to "which Payments", and the
// per-run log line is bounded by the same rule.
//
// Deliberately safe to call by hand, at any time and repeatedly. It is one
// DELETE against a predicate: a second call inside the same minute deletes
// nothing and reports zeros, two ticks overlapping delete disjoint sets, and a
// run killed halfway leaves the remainder for the next one.
//
// @Summary      Purge the Answers on abandoned Payments
// @Description  Deletes the Ticket Question Answers held on Payments that never reached `approved` and were begun more than 30 days ago (ADR 0044). The Payment row and its lines are untouched and kept forever — this is a purge of Answers, not of Payments — and the chosen Options of a choice Answer go with it. An approved Payment's Answers are never purged at any age. The predicate is non-approval plus age, and never the `expired` status alone: expiry in this platform is lazy, opportunistic bookkeeping and an expired Payment can still flip to approved when the Payment Provider confirms late, so purging on it would delete the Answers of a sale that then commits. The 30-day window is what makes non-approval safe to act on. Internal service-to-service only: Cloud Run IAM authenticates the caller by Google-signed OIDC ID token before the request reaches the API (ADR 0008), and no Customer Session or staff token reaches it. The window cannot be named by the caller; it is taken from the clock, and the cutoff actually used is echoed back. Safe to call by hand at any time and idempotent — a second run deletes nothing and reports zeros. The response reports how many Answers went, how many Payments they came off, the cutoff used, and how many Answers are still riding Payments across the platform, so two runs a day apart say whether the checkout is capturing at all. It names no Payment, no buyer and no Answer, because an Answer is the data this feature exists to protect.
// @Tags         internal
// @Produce      json
// @Success      200  {object}  openapi.EnvelopeAnswerPurge
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/internal/checkout-answers/purge [post]
func (h *Handler) PurgeAbandonedAnswers(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	result, err := h.svc.PurgeAbandonedAnswers(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}
