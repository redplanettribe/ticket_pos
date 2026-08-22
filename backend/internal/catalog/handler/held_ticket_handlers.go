package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/service"
	customersmiddleware "github.com/peter/ticket_pos/backend/internal/customers/middleware"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The held-ticket routes (#343, ADR 0049): a Customer lists the Tickets they
// hold and answers one Ticket Question on one of them.
//
// THEY LIVE ON THE CATALOG HANDLER AND ARE SERVED UNDER THE CUSTOMER NAMESPACE,
// for the reason the buyer's sale-scoped routes are: the customers module owns
// WHO IS ASKING and the catalog owns WHAT IS BEING ASKED FOR.
//
// THE CREDENTIAL IS THE SESSION AND THE PATH NAMES ONLY A TICKET. No Sale, no
// Event, no Organization: a Holder does not know which Sale their Ticket is on
// and has no business naming one. The Ticket id is narrowed against the
// Customer on the session inside the query itself, and an id the caller does
// not hold is answered exactly as an id that does not exist.

// heldAnswerBody is answerBody plus nothing — the same five value fields the
// buyer, the Assignment Link and Event Staff post, against the same parser.
type heldAnswerBody struct {
	answerBody
}

// ListHeldTickets returns the Tickets the signed-in Customer holds.
//
// @Summary      List the tickets you hold
// @Description  Returns every Ticket the signed-in Customer holds — the Self-held Ticket of their own purchase and every Ticket they accepted by Assignment Link, indistinguishably (ADR 0049) — each with the Ticket Questions its Ticket Type asks, whatever has been answered so far and its Outstanding Answer count. Authorization is the Customer Session and nothing else: a Ticket is listed because this Customer holds it, never because they bought the Sale it is on. A Confirmation Link session is narrowed to the Sale it names and sees only that Sale's Self-held Ticket. A Holder's row shows the Event, the Ticket Type and their own questions — never the buyer, the price, the Tax ID, the Sale Confirmation reference or the Sale's other Tickets. A reversed Sale's Ticket stays listed only for its buyer, read-only; a Holder who is not the buyer stops holding it, as they do when the buyer reassigns it. Answers 404 while the Ticket Question feature flag is off.
// @Tags         customer
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeHeldTickets
// @Failure      401  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/customer/held-tickets [get]
func (h *Handler) ListHeldTickets(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	session, ok := customersmiddleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}
	views, err := h.svc.ListHeldTickets(r.Context(), session.CustomerID, session.TicketSaleID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, views)
}

// AnswerHeldTicketQuestion writes one Answer on one Ticket the signed-in
// Customer holds.
//
// PUT for the reason its neighbours are: it is the whole Answer every time,
// there is exactly one per (Ticket, question), and a correction is the same
// request with a different body.
//
// @Summary      Answer a ticket question on a ticket you hold
// @Description  Writes the Answer to one Ticket Question on one Ticket the signed-in Customer holds, creating it or correcting what was there, and returns that Ticket with its questions, Answers and Outstanding Answer count. Only the Holder answers (ADR 0049): the buyer for their Self-held Ticket, a Holder for the Ticket they accepted, both through this route. A Ticket the caller does not hold — one on their own Sale that somebody else holds included — is refused with 404 TICKET_NOT_FOUND, indistinguishably from one that does not exist. Refused with 400 INVALID_ANSWER when the value does not fit the question's kind, with 409 once the Event has started (EVENT_STARTED_ANSWERS_CLOSED) or the Ticket Sale has been reversed (TICKET_SALE_REVERSED). Answers 404 while the Ticket Question feature flag is off.
// @Tags         customer
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        ticketId    path      string          true  "Ticket id"
// @Param        questionId  path      string          true  "Ticket question ID"
// @Param        body        body      heldAnswerBody  true  "The answer, in the shape its question's kind takes"
// @Success      200  {object}  openapi.EnvelopeHeldTicket
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/customer/held-tickets/{ticketId}/answers/{questionId} [put]
func (h *Handler) AnswerHeldTicketQuestion(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	session, ok := customersmiddleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	ticketID := strings.TrimSpace(r.PathValue("ticketId"))
	questionID, ok := pathValueRequired(w, r, reqID, "questionId")
	if !ok {
		return
	}
	// A malformed Ticket id is not found, exactly as a well-formed one the
	// caller does not hold is: telling them apart would tell a caller probing
	// ids which of their guesses were at least well formed.
	if _, err := uuid.Parse(ticketID); err != nil {
		_ = platform.WriteDomainError(w, reqID, catalog.ErrTicketNotFound())
		return
	}

	var body heldAnswerBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	// Nothing about the Answer is validated here, exactly as on every other
	// answer route: every rule needs the question's KIND, which this layer does
	// not have and must not guess. See AnswerTicketQuestion.
	view, err := h.svc.AnswerHeldTicketQuestion(
		r.Context(), session.CustomerID, session.TicketSaleID, ticketID, questionID,
		service.AnswerInput{
			Text:      body.Text,
			Number:    body.Number,
			Date:      body.Date,
			Checked:   body.Checked,
			OptionIDs: body.OptionIDs,
		},
	)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, view)
}
