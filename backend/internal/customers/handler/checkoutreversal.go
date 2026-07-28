package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// GetCheckoutReversal reports whether the Ticket Sale one online checkout
// produced can be undone right now, and by when.
//
// Unauthenticated by necessity, not by oversight: online checkout is guest-facing
// and the buyer reading the success page has no Customer Session at all (#121).
// What makes that safe is that the response holds nothing about anybody — a
// boolean and an instant — and the key is our own client transaction id, which
// names one checkout attempt and can be widened into no person, no email and no
// purchase history.
//
// Strictly read-only. There is no reversal here and no route by which one could
// be added to this handler without changing its method: undoing a purchase stays
// behind a Customer Session, because the alternative is an action a forwarded
// confirmation email could trigger (ADR 0018).
//
// @Summary      Reversal Window for a checkout
// @Description  Reports whether the Ticket Sale produced by one online checkout can be undone right now (`reversible`) and, when it can, the instant its Reversal Window closes (`reversal_window_closes_at`, RFC3339 UTC) — the earlier of 20:00 Ecuador time on the day of purchase or the Event's start (ADR 0018). Keyed by our own client transaction id, so a guest who has just checked out and holds no Customer Session can still be told the deadline; the response carries nothing about the buyer. The same rule the Customer Area applies is applied here, so both surfaces report the same instant for the same sale: an active Online Sale, inside its window, whose Payment this deployment could actually undo. Anything else — a checkout that does not exist, a Payment not approved, a sale already reversed, a closed window, a Payment Provider that cannot reverse — reports `reversible: false` with a null closing time. Read-only: no reversal can be performed here, which requires a Customer Session.
// @Tags         customer
// @Produce      json
// @Param        clientTransactionId  path  string  true  "Our client transaction id for the checkout"
// @Success      200  {object}  openapi.EnvelopeCheckoutReversal
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/public/checkout/{clientTransactionId}/reversal [get]
func (h *Handler) GetCheckoutReversal(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	offer, err := h.svc.GetCheckoutReversal(r.Context(), r.PathValue("clientTransactionId"))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, offer)
}
