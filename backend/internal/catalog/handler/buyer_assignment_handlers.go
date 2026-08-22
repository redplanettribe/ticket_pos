package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The buyer's one route into a Ticket Assignment (#324, parent #322).
//
// IT SITS BESIDE ListBuyerTicketAnswers AND SHARES ITS SCOPING EXACTLY: the
// credential is the Customer Session, the path names only which of the caller's
// OWN sales and which Ticket of it, and nothing in the body is believed about
// who is asking. See buyer_answer_handlers.go, which states that arrangement at
// length; this file does not repeat it.
//
// WHAT IS DIFFERENT HERE is what the body carries: somebody ELSE'S email
// address, typed by the buyer, about a person who has never visited this
// platform and has not agreed to anything. That is the whole reason the feature
// is behind its own flag, the reason #322 gives the address a purge, and the
// reason the Storefront must tell the buyer — before they submit — that the
// address will be mailed and shown to the Organization. The API cannot enforce
// that notice; it can and does refuse to accept anything at all while
// TICKET_ASSIGNMENT_ENABLED is closed.

// buyerAssignmentBody is the whole of what a buyer sends: one address.
//
// ONE FIELD AND NOT TWO. There is no name here, and no Ticket Question answer:
// what the platform knows about a Holder is what the HOLDER told it when they
// accepted (#325), never what the buyer guessed on their behalf. A `holder_name`
// the buyer typed would be a fact about a person recorded from somebody else's
// memory, and it would go straight into the Organization's guest list.
//
// NO TOKEN, for the reason its Answer neighbour has none: this route has a
// session, so the body carries no credential.
type buyerAssignmentBody struct {
	// HolderEmail is the address as the buyer typed it. Normalised and shape
	// checked by catalog.ParseHolderEmail in the service, and NOT here — the
	// address is a domain value with one definition, and a second check in the
	// handler is a second place for it to disagree.
	HolderEmail string `json:"holder_email"`
}

// AssignOwnTicket assigns or reassigns one Ticket of the buyer's own Sale to an
// email address.
//
// PUT, for the reason its Answer neighbour is one: it states the whole current
// fact every time, there is exactly one Holder address per Ticket, and
// correcting a typo is the same request with a different body. Sending the same
// address twice changes nothing at all — see repository.AssignTicketToHolder.
//
// @Summary      Assign one of your own tickets to an email address
// @Description  Names the email address that holds one Ticket of the signed-in Customer's own Ticket Sale, creating the Ticket Assignment or replacing the one that was there — assign, reassign and correcting a typo are all this one call. **A change of address mails the new address an Assignment Link**, which is how a Ticket becomes `accepted`; re-sending the address already there mails nobody. **Sending is rationed**: one Ticket may send at most a small fixed number of Assignment mails in its whole life (a first send plus a resend allowance for a mistyped address), and one buyer may send only so many inside a rolling window across all their Tickets. A Ticket out of allowance is refused with 409 ASSIGNMENT_MAIL_CAP_REACHED and a buyer over their window with 429 ASSIGNMENT_RATE_LIMITED. Both refuse the ASSIGNMENT outright and send no mail — the address is not written and no timestamp moves, because a write without a send would kill every Assignment Link already outstanding for that Ticket and replace it with nothing. The buyer's fallback is the Ticket's Answer Link, which keeps working. **Reassigning to a DIFFERENT address clears that Ticket's Answers back to Outstanding**: an Answer is a fact about a person and is never inherited by a new Holder. A first assignment clears nothing, and re-sending the address the Ticket already carries is a no-op that moves no timestamp. The buyer may assign any Ticket of their sale, including to their own address, and may assign only some of them. Authorization is the Customer Session; a Confirmation Link session may assign the one sale it names. A Ticket that is not on one of the caller's own sales is refused with 404 TICKET_NOT_FOUND, indistinguishably from one that does not exist. Available on `online` and `import` Ticket Sales only — an `in_person` door sale has no buyer surface and is refused with 409 ASSIGNMENT_CHANNEL_UNSUPPORTED. Also refused with 409 once the Event has started (ASSIGNMENT_EVENT_STARTED) or the Ticket Sale has been reversed (ASSIGNMENT_SALE_REVERSED), and with 400 INVALID_HOLDER_EMAIL when the value is not an email address. The whole sale's tickets come back, not just the one that changed. Answers 404 while TICKET_ASSIGNMENT_ENABLED is off, which is how it ships — that flag is separate from the Ticket Question one, so closing it leaves Ticket Questions working. The Storefront must tell the buyer, before they submit, that the address will be mailed and shown to the Organization.
// @Tags         customer
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        ticketSaleId  path      string                true  "Ticket Sale id"
// @Param        ticketId      path      string                true  "Ticket id"
// @Param        body          body      buyerAssignmentBody   true  "The email address to assign this ticket to"
// @Success      200  {object}  openapi.EnvelopeBuyerTicketAnswers
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Failure      429  {object}  platform.Envelope
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/customer/ticket-sales/{ticketSaleId}/tickets/{ticketId}/assignment [put]
func (h *Handler) AssignOwnTicket(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	session, saleID, ok := buyerSaleRoute(w, r, reqID)
	if !ok {
		return
	}
	// A malformed sale id is not found, exactly as a well-formed one belonging
	// to somebody else is.
	if saleID == "" {
		_ = platform.WriteDomainError(w, reqID, catalog.ErrTicketNotFound())
		return
	}

	ticketID := strings.TrimSpace(r.PathValue("ticketId"))
	if _, err := uuid.Parse(ticketID); err != nil {
		_ = platform.WriteDomainError(w, reqID, catalog.ErrTicketNotFound())
		return
	}

	var body buyerAssignmentBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	// The address is neither trimmed nor validated here. It goes to the service
	// verbatim, which reads the FLAG first and resolves the Ticket before it
	// looks at the address at all — so a caller naming somebody else's Ticket
	// cannot learn from a validation error that their id was reachable.
	views, err := h.svc.AssignOwnTicket(
		r.Context(), session.CustomerID, session.TicketSaleID, saleID, ticketID, body.HolderEmail,
	)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, views)
}
