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

// The buyer's two routes onto their own Tickets' Answers (#315, ADR 0044): the
// page behind the Confirmation Link, and the Customer Area.
//
// THEY LIVE ON THE CATALOG HANDLER AND ARE SERVED UNDER THE CUSTOMER NAMESPACE,
// which is the arrangement the Customer's undo already has in reverse: the
// customers module owns WHO IS ASKING and does not own WHAT IS BEING ASKED FOR.
// A Ticket, its Ticket Questions and its Answers are the catalog's, so the
// handler is here; the credential is a Customer Session, so the route is
// registered there and behind that middleware.
//
// THE CREDENTIAL IS THE SESSION AND THE PATH NAMES ONLY WHICH OF THE CALLER'S
// OWN SALES. Neither route reads an Organization or an Event from anywhere — a
// buyer does not know which Organization sold them a ticket — and the sale id in
// the path is narrowed twice before it reaches a row: once against the
// Confirmation Link session's own sale, and once by the customer_id clause in
// the query itself.
//
// WHY THAT NARROWING IS SHARPER HERE THAN ON THE OTHER CUSTOMER ROUTES. What
// comes back is not a view of a row; it is a per-Ticket ANSWER LINK, an
// unauthenticated credential that answers for that Ticket until the Event
// starts. Every other leak on this namespace would disclose a purchase. A leak
// here would hand somebody a durable write credential over a stranger's ticket,
// which is why the sale id is treated as untrusted input the whole way down and
// why an id nobody owns is answered exactly as an id that does not exist.

// buyerAnswerBody is answerBody plus nothing.
//
// It embeds rather than restates the five value fields so that the shape a buyer
// posts, the shape a holder posts through an Answer Link and the shape Event
// Staff post cannot drift apart — all three are answering the same Ticket
// Question against the same catalog.ParseAnswer.
//
// THERE IS NO TOKEN IN IT, which is the difference from answerLinkAnswerBody and
// the whole of this route's security posture. The Answer Link's write carries
// its own authority in the body because it has no session; this one has a
// session, so the body carries no credential at all and nothing in it is
// believed about who is asking.
type buyerAnswerBody struct {
	answerBody
}

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

// ListBuyerTicketAnswers returns the buyer's own Tickets with their questions,
// their Answers and their Answer Links.
//
// @Summary      List a Ticket Sale's tickets and their answers
// @Description  Returns every Ticket of one of the signed-in Customer's own Ticket Sales, with the Ticket Questions its Ticket Type asks, whatever has been answered so far, and a per-Ticket **Answer Link** to pass to whoever will be using that ticket (ADR 0044). This is the page behind the Confirmation Link and the Customer Area, which are one surface: a Confirmation Link session is narrowed to the single Ticket Sale it names and sees only that one. Authorization is the Customer Session and nothing else — the Ticket Sale id in the path names which of the caller's OWN sales, and a sale belonging to somebody else returns an empty list rather than a refusal, so that ids cannot be probed. An Answer Link is minted only while the Ticket can still be answered: it is absent once the Event has started and on a reversed Ticket Sale, because a copy button that forwards a dead link is worse than no button. A reversed sale's Tickets are still listed and still readable — a Sale Reversal voids a purchase, it does not erase what its Tickets answered. Ticket Question labels and Option labels read as the Organization coined them in every Locale (ADR 0027). Answers 404 while the Ticket Question feature flag is off.
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

// AnswerOwnTicketQuestion writes one Answer on a Ticket of the buyer's own Sale.
//
// PUT for the reason its two neighbours are: it is the whole Answer every time,
// there is exactly one per (Ticket, question), and a correction is the same
// request with a different body.
//
// THE BUYER MAY ANSWER ANY TICKET OF THEIR SALE and not merely "their own" one.
// An Answer belongs to the TICKET and three parties may supply it (ADR 0044); a
// mother buying for her children answers all four herself and must not have to
// mail herself four links to do it.
//
// @Summary      Answer a ticket question on your own Ticket Sale
// @Description  Writes the Answer to one Ticket Question on one Ticket of the signed-in Customer's own Ticket Sale, creating it or correcting what was there. The buyer may answer ANY Ticket of their sale, not only one of them: an Answer belongs to the Ticket and the buyer, the holder of its Answer Link and Event Staff may all supply it (ADR 0044). An Answer written here replaces one given at checkout, and may itself be replaced later by whoever holds the Ticket's Answer Link — nothing records which of them wrote it. Authorization is the Customer Session; a Ticket that is not on one of the caller's own sales is refused with 404 TICKET_NOT_FOUND, indistinguishably from one that does not exist. The whole sale's tickets come back, not just the one that changed, so the page's outstanding counts cannot go stale against the row beside them. Refused with 400 INVALID_ANSWER when the value does not fit the question's kind, with 409 once the Event has started (EVENT_STARTED_ANSWERS_CLOSED) or the Ticket Sale has been reversed (TICKET_SALE_REVERSED). Answers 404 while the Ticket Question feature flag is off.
// @Tags         customer
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        ticketSaleId  path      string           true  "Ticket Sale id"
// @Param        ticketId      path      string           true  "Ticket id"
// @Param        questionId    path      string           true  "Ticket question ID"
// @Param        body          body      buyerAnswerBody  true  "The answer, in the shape its question's kind takes"
// @Success      200  {object}  openapi.EnvelopeBuyerTicketAnswers
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/customer/ticket-sales/{ticketSaleId}/tickets/{ticketId}/answers/{questionId} [put]
func (h *Handler) AnswerOwnTicketQuestion(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	session, saleID, ok := buyerSaleRoute(w, r, reqID)
	if !ok {
		return
	}
	// A malformed sale id is not found, exactly as a well-formed one belonging to
	// somebody else is.
	if saleID == "" {
		_ = platform.WriteDomainError(w, reqID, catalog.ErrTicketNotFound())
		return
	}

	ticketID := strings.TrimSpace(r.PathValue("ticketId"))
	questionID, ok := pathValueRequired(w, r, reqID, "questionId")
	if !ok {
		return
	}
	// A malformed Ticket id is not found, exactly as a well-formed one belonging
	// to somebody else is. The service would reach the same answer by failing to
	// find it among the buyer's own Tickets; refusing it here keeps a value of
	// the wrong shape away from the query.
	if _, err := uuid.Parse(ticketID); err != nil {
		_ = platform.WriteDomainError(w, reqID, catalog.ErrTicketNotFound())
		return
	}

	var body buyerAnswerBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	// Nothing about the Answer is validated here, exactly as on the staff and
	// Answer Link routes: every rule needs the question's KIND, which this layer
	// does not have and must not guess. See AnswerTicketQuestion.
	views, err := h.svc.AnswerOwnTicketQuestion(
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
