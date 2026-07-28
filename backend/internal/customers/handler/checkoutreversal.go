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
// boolean, an instant, and the opaque id of the sale the offer concerns — and the
// key is our own client transaction id, which names one checkout attempt and can
// be widened into no person, no email and no purchase history. The sale id is an
// address for the Customer Area and not a credential for it; without a Customer
// Session belonging to the buyer it opens nothing.
//
// Strictly read-only. There is no reversal here and no route by which one could
// be added to this handler without changing its method: undoing a purchase stays
// behind a Customer Session, because the alternative is an action a forwarded
// confirmation email could trigger (ADR 0018).
//
// @Summary      Reversal Window for a checkout
// @Description  Reports whether the Ticket Sale produced by one online checkout can be undone right now (`reversible`), until when (`reversible_until`, RFC3339 UTC), and which sale it is (`ticket_sale_id`). The deadline is the offer's, not a publication of the Reversal Window: while an undo is on offer it is the earlier of 20:00 Ecuador time on the day of purchase or the Event's start (ADR 0018), and it is null whenever no undo is on offer even if that Window is still open. Keyed by our own client transaction id, so a guest who has just checked out and holds no Customer Session can still be told the deadline; the response carries nothing about the buyer, and `ticket_sale_id` is an address for the Customer Area rather than a credential for it. The same rule the Customer Area applies is applied here, so both surfaces report the same instant for the same sale: an active Online Sale, inside its window, whose Payment this deployment could actually undo. Anything else — a checkout that does not exist, a Payment not approved, a sale already reversed, a closed window, a Payment Provider that cannot reverse — reports `reversible: false` with a null deadline and a null sale id. Read-only: no reversal can be performed here, which requires a Customer Session.
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
