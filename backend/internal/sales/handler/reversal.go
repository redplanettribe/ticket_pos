package handler

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/customers"
	customersmiddleware "github.com/peter/ticket_pos/backend/internal/customers/middleware"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

// ReverseTicketSale is a Customer undoing their own Online Sale from the
// Customer Area, within the Reversal Window (ADR 0018).
//
// It lives on the sales handler although it is served under the customer
// namespace, for the same reason the Organization's payout surface does: this is
// an operation on a Ticket Sale, and Ticket Sales — with the reversal primitive
// that restores capacity, the void notice, and the Payment Provider — belong to
// this module. The customers module owns who is asking; it does not own what is
// being asked for.
//
// The body is empty and there is nothing to validate but the id in the path.
// That id names WHICH of the caller's own sales to reverse and can do nothing
// else: the service loads it scoped to the Customer on the session, so an id
// belonging to somebody else resolves to nothing at all.
//
// @Summary      Undo a Ticket Sale
// @Description  Reverses one of the signed-in Customer's own Ticket Sales within its Reversal Window (ADR 0018). The Ticket Sale becomes reversed, every line's quantity returns to its Ticket Type's capacity, and the void notice is emailed to the Customer once the reversal has committed. The sale is not deleted: it keeps its Sale Confirmation reference and stays visible in the Customer Area with a reversed status. Authorization is the Customer Session and nothing else — a Customer may only reverse a Ticket Sale they own, and a Confirmation Link session is not a credential for this. The Reversal Window is enforced here on the server whatever the client believed. Refused, with nothing changed, when the sale is already reversed (SALE_ALREADY_REVERSED), is not an Online Sale or was settled by a Payment Provider that cannot reverse it (SALE_NOT_REVERSIBLE), or its window has closed (REVERSAL_WINDOW_CLOSED). Pressing twice reverses once: the second request is refused as already reversed and capacity is never restored twice.
// @Tags         customer
// @Produce      json
// @Security     BearerAuth
// @Param        ticketSaleId  path      string  true  "Ticket Sale id"
// @Success      200           {object}  openapi.EnvelopeSaleReversal
// @Failure      400           {object}  platform.Envelope
// @Failure      401           {object}  platform.Envelope
// @Failure      404           {object}  platform.Envelope
// @Failure      409           {object}  platform.Envelope
// @Failure      502           {object}  platform.Envelope
// @Router       /api/v1/customer/ticket-sales/{ticketSaleId}/reverse [post]
func (h *Handler) ReverseTicketSale(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	// The Customer comes from the session the middleware validated, and from
	// nowhere else. Its absence here would mean the route was wired without that
	// middleware, which must fail closed rather than act on an unauthenticated
	// request.
	session, ok := customersmiddleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	// A Confirmation Link session may READ the one sale it names and may not
	// undo it. The link is a read credential that travels by email and can be
	// forwarded, quoted, or shared; only Proof of Email Ownership authorizes the
	// first mutating, money-moving action a Customer can take (ADR 0018).
	if session.TicketSaleID != "" {
		_ = platform.WriteDomainError(w, reqID, customers.ErrReversalRequiresFullSession())
		return
	}

	// A malformed id is answered exactly as an id nobody owns. The alternative
	// tells a caller probing ids which of their guesses were at least well
	// formed, and this endpoint owes them no such help.
	saleID := strings.TrimSpace(r.PathValue("ticketSaleId"))
	if _, err := uuid.Parse(saleID); err != nil {
		_ = platform.WriteDomainError(w, reqID, sales.ErrTicketSaleNotFound())
		return
	}

	result, err := h.svc.ReverseOwnSale(r.Context(), session.CustomerID, saleID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}
