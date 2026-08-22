package handler

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/catalog/service"
	customersmiddleware "github.com/peter/ticket_pos/backend/internal/customers/middleware"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The buyer's read of their own Tickets (#315, ADR 0044; narrowed by #344,
// ADR 0049): the page behind the Confirmation Link, and the Customer Area.
//
// IT LIVES ON THE CATALOG HANDLER AND IS SERVED UNDER THE CUSTOMER NAMESPACE,
// which is the arrangement the Customer's undo already has in reverse: the
// customers module owns WHO IS ASKING and does not own WHAT IS BEING ASKED FOR.
// A Ticket and whose it is are the catalog's, so the handler is here; the
// credential is a Customer Session, so the route is registered there and behind
// that middleware.
//
// ONE ROUTE AND NOT TWO, SINCE ADR 0049. The sale-scoped answer write that sat
// beside this read is gone: an Answer is given only by a Ticket's Holder —
// the buyer for their Self-held Ticket, through held_ticket_handlers.go — or by
// Event Staff. The buyer's assignment write, which shares this read's scoping,
// is in buyer_assignment_handlers.go.
//
// THE CREDENTIAL IS THE SESSION AND THE PATH NAMES ONLY WHICH OF THE CALLER'S
// OWN SALES. The route reads no Organization or Event from anywhere — a buyer
// does not know which Organization sold them a ticket — and the sale id in the
// path is narrowed twice before it reaches a row: once against the Confirmation
// Link session's own sale, and once by the customer_id clause in the query
// itself. An id nobody owns is answered exactly as an id that does not exist.

// buyerSaleRoute pulls the Customer Session and the Ticket Sale id every route
// in this file hangs off.
//
// ok is false ONLY when the refusal has already been written — that is, when
// there is no session, which means the route was wired without its middleware
// and must fail closed rather than act on an unauthenticated request for a
// payload full of credentials.
//
// An empty saleID with ok true means the id was MALFORMED, and each caller
// answers that exactly as it answers an id nobody owns: an empty list on the
// read, a not-found on the write. The two must be indistinguishable, because
// telling them apart would tell a caller probing ids which of their guesses were
// at least well formed — and on a route that hands back Answer Links, that is a
// hint worth denying.
func buyerSaleRoute(w http.ResponseWriter, r *http.Request, reqID string) (*customersmiddleware.CustomerSession, string, bool) {
	session, ok := customersmiddleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return nil, "", false
	}

	saleID := strings.TrimSpace(r.PathValue("ticketSaleId"))
	if _, err := uuid.Parse(saleID); err != nil {
		return session, "", true
	}
	return session, saleID, true
}

// ListBuyerTicketAnswers returns the buyer's own Tickets with their assignment
// state.
//
// @Summary      List a Ticket Sale's tickets and whose they are
// @Description  Returns every Ticket of one of the signed-in Customer's own Ticket Sales — its position, its Ticket Type and its Ticket Assignment state (unassigned, assigned, accepted) with the address the buyer gave it. It carries NO Ticket Questions, Answers, outstanding counts or Answer Links for any Ticket (ADR 0049): an Answer is given only by a Ticket's Holder, and the buyer reads and answers the one Ticket they hold through `/api/v1/customer/held-tickets`. This is the page behind the Confirmation Link and the Customer Area, which are one surface: a Confirmation Link session is narrowed to the single Ticket Sale it names and sees only that one. Authorization is the Customer Session and nothing else — the Ticket Sale id in the path names which of the caller's OWN sales, and a sale belonging to somebody else returns an empty list rather than a refusal, so that ids cannot be probed. A reversed sale's Tickets are still listed. Answers 404 while the Ticket Question feature flag is off.
// @Tags         customer
// @Produce      json
// @Security     BearerAuth
// @Param        ticketSaleId  path      string  true  "Ticket Sale id"
// @Success      200  {object}  openapi.EnvelopeBuyerTicketAnswers
// @Failure      401  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/customer/ticket-sales/{ticketSaleId}/tickets [get]
func (h *Handler) ListBuyerTicketAnswers(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	session, saleID, ok := buyerSaleRoute(w, r, reqID)
	if !ok {
		return
	}
	// A malformed id owns nothing, which is the same answer as an id owned by
	// somebody else: the empty list, and never a validation error that would
	// confirm the shape of a guess.
	if saleID == "" {
		_ = platform.WriteSuccess(w, reqID, http.StatusOK, []service.BuyerTicketAnswersView{})
		return
	}

	views, err := h.svc.ListBuyerTicketAnswers(r.Context(), session.CustomerID, session.TicketSaleID, saleID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, views)
}
