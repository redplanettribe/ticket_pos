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
// @Description  Reverses one of the signed-in Customer's own Ticket Sales within its Reversal Window (ADR 0018), or records the ask as a Reversal Request when the Payment Provider does not answer in time (ADR 0024). Authorization is the Customer Session and nothing else — a Customer may only reverse a Ticket Sale they own, and a Confirmation Link session is not a credential for this. The Reversal Window is enforced here on the server whatever the client believed, and it is evaluated once, when the Reversal Request is created; a request already made stays authorised however long the answer takes. **200 OK — undone.** The Ticket Sale becomes reversed with status `reversed` and `reversed_at`, every line's quantity returns to its Ticket Type's capacity, and the void notice is emailed to the Customer once the reversal has committed. The sale is not deleted: it keeps its Sale Confirmation reference and stays visible in the Customer Area with a reversed status. **202 Accepted — pending.** The Payment Provider gave no definite answer (a timeout, a 5xx, a body this integration cannot place), so the money may or may not have moved: the body carries status `pending` and `requested_at`, and a Reversal Request is in flight. Nothing about the money or the tickets has changed — the Ticket Sale stays active, its capacity stays held, and the tickets are still valid — and the platform asks the provider again, including whenever the Customer loads their Customer Area. A client must never render a 202 as a completed refund. Pressing Undo again while a request is in flight returns 202 with the original request's `requested_at`: no second Reversal Request, and no second call to the Payment Provider. Refused, with nothing changed, when the sale is already reversed (SALE_ALREADY_REVERSED), is not an Online Sale or was settled by a Payment Provider this deployment cannot ask (SALE_NOT_REVERSIBLE), or its window has closed (REVERSAL_WINDOW_CLOSED). Refused with 502 SALE_REVERSAL_FAILED, again with nothing changed, when the provider was asked and definitely declined: nothing happened, the response carries the Sale Confirmation reference and makes no claim about the cause, because the provider publishes no code meaning the deadline passed, and the provider's own error code goes to the log only. Two simultaneous presses reverse once: attempts on one Ticket Sale are serialised for the whole attempt including the call to the Payment Provider, so the second is refused as already reversed (SALE_ALREADY_REVERSED), the provider is asked exactly once, and capacity is never restored twice. A free Online Sale has no provider to ask, creates no Reversal Request, and is always answered synchronously.
// @Tags         customer
// @Produce      json
// @Security     BearerAuth
// @Param        ticketSaleId  path      string  true  "Ticket Sale id"
// @Success      200           {object}  openapi.EnvelopeSaleReversal
// @Success      202           {object}  openapi.EnvelopeSaleReversal
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

	// 200 means undone; 202 means the platform is still finding out (ADR 0024).
	// The distinction is carried by the status code as well as by the body's own
	// word, because a client that checks only "did this succeed" must not read a
	// Reversal Request still in flight as a refund that happened — that is a lie
	// about somebody's money, and it is the one this endpoint could most easily
	// tell by accident.
	status := http.StatusOK
	if result.Pending() {
		status = http.StatusAccepted
	}
	_ = platform.WriteSuccess(w, reqID, status, result)
}
