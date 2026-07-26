package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// GetOrganizationPayouts returns the Organization's Payouts section: its
// Withdrawable Balance and its payout history.
//
// @Summary      Get the Organization's Withdrawable Balance and payout history
// @Description  Returns the acting Member's Organization's Withdrawable Balance — the Net Proceeds of its active Online Sales (each line's unit price minus the Platform Fee and Fee IVA snapshotted on it) minus every recorded Payout — in the Organization currency, plus the payout history newest first. The balance is signed: a sale reversed after a settlement makes it negative, and that is shown as-is. Read-only; Payouts are recorded by the platform operator directly in the database. Org Admin only.
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
