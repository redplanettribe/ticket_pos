package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// GetOrganizationPayouts returns the Organization's Payouts section: its two
// balances and its payout history.
//
// @Summary      Get the Organization's balances and payout history
// @Description  Returns the acting Member's Organization's Withdrawable Balance — the Net Proceeds of its active Online Sales (each line's unit price minus the Platform Fee and Fee IVA snapshotted on it) minus every recorded Payout — and its Payable Balance, the same arithmetic counting only the sales that have cleared: those recorded before today in America/Guayaquil with no Reversal Request still open on them (ADR 0026). Both are in the Organization currency and both are signed: a sale reversed after a settlement makes them negative, and that is shown as-is. Cleared sales are a subset, so payable_balance_cents never exceeds withdrawable_balance_cents; the gap is money the platform holds but has not yet cleared, and a fully settled Organization that sold today shows a negative Payable Balance beside a positive Withdrawable one. The payout history follows, newest first. Read-only from this side: a Payout is recorded by a Platform Operator on the operator surface (ADR 0015), and appears here unchanged. Org Admin only.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopePayoutsSummary
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/staff/organization/payouts [get]
func (h *Handler) GetOrganizationPayouts(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	summary, err := h.svc.OrganizationPayouts(r.Context(), actorFromRequest(r))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, summary)
}
