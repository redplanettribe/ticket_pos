package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The buyer's write on their provisional Answers (#672, ADR 0076).
//
// IT SHARES THE BUYER'S SALE-SCOPED ROUTES' SCOPING EXACTLY: the credential is
// the Customer Session, the path names which of the caller's OWN sales, which
// Ticket of it and which question, and the body carries only the Answer. See
// buyer_answer_handlers.go, which states that arrangement at length.

// buyerProvisionalAnswerBody is answerBody plus nothing - the same five value
// fields every Answer route posts, against the same parser.
type buyerProvisionalAnswerBody struct {
	answerBody
}

// AnswerBuyerProvisionalTicketQuestion writes one provisional Answer on one
// Ticket of the buyer's own Sale.
//
// PUT for the reason its neighbours are: it is the whole Answer every time,
// there is exactly one per (Ticket, question), and a correction is the same
// request with a different body.
//
// @Summary      Answer a ticket question on a ticket you named for somebody else
// @Description  Writes the Answer to one Ticket Question on one Ticket of the signed-in Customer's own Ticket Sale whose Answers are provisionally the buyer's (ADR 0076): the Event requires Named Tickets and the Ticket is `assigned` to somebody who has not yet accepted. Creates the Answer or corrects what was there, and returns the whole Sale's tickets, as the assignment write does. Once the Holder accepts, the Answers are theirs and this route refuses. It never moves the assignment: the Holder's Assignment Link and any owed Assignment mail stay valid. Authorization is the Customer Session; a Confirmation Link session may answer on the one sale it names. Any Ticket whose Answers are not provisionally the buyer's - accepted, unassigned, held by the buyer, on an Event without Named Tickets, or not on one of the caller's own sales - is refused with 404 TICKET_NOT_FOUND, indistinguishably from one that does not exist. Refused with 400 INVALID_ANSWER when the value does not fit the question's kind, and with 409 once the Event has started (EVENT_STARTED_ANSWERS_CLOSED) or the Ticket Sale has been reversed (TICKET_SALE_REVERSED). Answers 404 while the Ticket Question feature flag is off.
// @Tags         customer
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        ticketSaleId  path      string                      true  "Ticket Sale id"
// @Param        ticketId      path      string                      true  "Ticket id"
// @Param        questionId    path      string                      true  "Ticket question ID"
// @Param        body          body      buyerProvisionalAnswerBody  true  "The answer, in the shape its question's kind takes"
// @Success      200  {object}  openapi.EnvelopeBuyerTicketAnswers
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/customer/ticket-sales/{ticketSaleId}/tickets/{ticketId}/provisional-answers/{questionId} [put]
func (h *Handler) AnswerBuyerProvisionalTicketQuestion(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	session, saleID, ok := buyerSaleRoute(w, r, reqID)
	if !ok {
		return
	}
	// A malformed sale or Ticket id is not found, exactly as a well-formed one
	// belonging to somebody else is.
	if saleID == "" {
		_ = platform.WriteDomainError(w, reqID, catalog.ErrTicketNotFound())
		return
	}
	ticketID := strings.TrimSpace(r.PathValue("ticketId"))
	if _, err := uuid.Parse(ticketID); err != nil {
		_ = platform.WriteDomainError(w, reqID, catalog.ErrTicketNotFound())
		return
	}
	questionID, ok := pathValueRequired(w, r, reqID, "questionId")
	if !ok {
		return
	}

	var body buyerProvisionalAnswerBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	// Nothing about the Answer is validated here, exactly as on every other
	// answer route: every rule needs the question's KIND, which this layer does
	// not have and must not guess.
	views, err := h.svc.AnswerBuyerProvisionalTicketQuestion(
		r.Context(), session.CustomerID, session.TicketSaleID, saleID, ticketID, questionID,
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
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, views)
}
