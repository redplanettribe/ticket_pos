package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/catalog/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// answerBody is one Answer as a form states it (#310).
//
// FIVE OPTIONAL FIELDS AND EXACTLY ONE OF THEM SENT, chosen by the Ticket
// Question's kind. They are pointers so that ABSENCE IS DISTINGUISHABLE FROM A
// ZERO VALUE, which matters most on `checked`: `false` is an Answer — somebody
// read the question and said no — while an absent `checked` is no Answer at all,
// and a plain bool could not tell those apart.
//
// The body does NOT carry the kind. The kind is the question's property, stored
// and frozen once anything has answered, and letting a caller state it would
// mean either trusting them about what the question is or refusing them for
// disagreeing — neither of which the caller can do anything about.
type answerBody struct {
	// Text answers short_text and long_text.
	Text *string `json:"text"`
	// Number answers number, as a decimal STRING rather than a JSON number.
	// JSON numbers are doubles in most parsers, and a value that survives a
	// NUMERIC column only to be rounded on the way through the wire would defeat
	// the column. See catalog.SubmittedAnswer.
	Number *string `json:"number"`
	// Date answers date, as a calendar date YYYY-MM-DD. Never an instant: a
	// date carries no time and no zone, so nothing can shift it by a day.
	Date *string `json:"date"`
	// Checked answers checkbox.
	Checked *bool `json:"checked"`
	// OptionIDs answers single_choice (one) and multi_choice (any number). They
	// are OPTION IDENTITIES and never labels, because a label could not survive
	// a rename — which is the whole reason an Option has an id.
	//
	// An empty array is somebody clearing their choices, which is refused as an
	// empty Answer; the way to say "not said" is to DELETE the Answer.
	OptionIDs []string `json:"option_ids"`
}

// ticketAnswerRoute pulls the Event and Ticket every Answer route hangs off.
func ticketAnswerRoute(w http.ResponseWriter, r *http.Request, reqID string) (eventID, ticketID string, ok bool) {
	eventID = strings.TrimSpace(r.PathValue("id"))
	ticketID = strings.TrimSpace(r.PathValue("ticketId"))

	var fields []platform.FieldError
	if eventID == "" {
		fields = append(fields, platform.FieldError{Field: "id", Code: platform.CodeRequired, Message: "is required"})
	}
	if ticketID == "" {
		fields = append(fields, platform.FieldError{Field: "ticketId", Code: platform.CodeRequired, Message: "is required"})
	}
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return "", "", false
	}
	return eventID, ticketID, true
}

// ListTicketSaleAnswers returns a Ticket Sale's Tickets with their questions and
// Answers.
//
// @Summary      List a ticket sale's tickets and answers
// @Description  Lists every Ticket of one Ticket Sale with its Ticket Questions and this Ticket's Answers. Reversed sales are listed and readable; only writing is refused. Org Admin and Event Owner only; Event Staff are refused. Answers 404 while the Ticket Question feature flag is off.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id             path      string  true  "Event ID"
// @Param        ticketSaleId   path      string  true  "Ticket sale ID"
// @Success      200  {object}  openapi.EnvelopeTicketAnswersList
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/ticket-sales/{ticketSaleId}/tickets [get]
func (h *Handler) ListTicketSaleAnswers(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ok := pathValueRequired(w, r, reqID, "id")
	if !ok {
		return
	}
	ticketSaleID, ok := pathValueRequired(w, r, reqID, "ticketSaleId")
	if !ok {
		return
	}

	tickets, err := h.svc.ListTicketSaleAnswers(r.Context(), actorFromRequest(r), eventID, ticketSaleID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, tickets)
}

// GetTicketAnswers returns one Ticket with its questions and Answers.
//
// @Summary      Get a ticket's questions and answers
// @Description  One Ticket, its Ticket Type's Ticket Questions (retired ones included) and what this Ticket has answered. Org Admin and Event Owner only; Event Staff are refused. Answers 404 while the Ticket Question feature flag is off.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id         path      string  true  "Event ID"
// @Param        ticketId   path      string  true  "Ticket ID"
// @Success      200  {object}  openapi.EnvelopeTicketAnswersDetail
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/tickets/{ticketId} [get]
func (h *Handler) GetTicketAnswers(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ticketID, ok := ticketAnswerRoute(w, r, reqID)
	if !ok {
		return
	}

	ticket, err := h.svc.GetTicketAnswers(r.Context(), actorFromRequest(r), eventID, ticketID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, ticket)
}

// AnswerTicketQuestion writes one Ticket's Answer to one Ticket Question.
//
// PUT and not POST or PATCH, because it is the whole Answer every time: an
// Answer is what one Ticket says in reply to one question, there is exactly one
// per pair, and the address names it. A correction is the same request with a
// different body, which is why re-answering needs no second verb.
//
// The response is the whole Ticket rather than the one Answer, so a form redraws
// from one payload instead of reassembling the Ticket from a fragment — the same
// arrangement the Option endpoints have with their question.
//
// @Summary      Answer a ticket question
// @Description  Writes one Ticket's Answer to one Ticket Question, creating it or correcting it. Refused once the Event has started and on a reversed Ticket Sale. Org Admin and Event Owner only; Event Staff are refused. Answers 404 while the Ticket Question feature flag is off.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id           path      string      true  "Event ID"
// @Param        ticketId     path      string      true  "Ticket ID"
// @Param        questionId   path      string      true  "Ticket question ID"
// @Param        body         body      answerBody  true  "The answer, in the shape its question's kind takes"
// @Success      200  {object}  openapi.EnvelopeTicketAnswersDetail
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/tickets/{ticketId}/answers/{questionId} [put]
func (h *Handler) AnswerTicketQuestion(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ticketID, ok := ticketAnswerRoute(w, r, reqID)
	if !ok {
		return
	}
	questionID, ok := pathValueRequired(w, r, reqID, "questionId")
	if !ok {
		return
	}

	var body answerBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	// NOTHING IS VALIDATED HERE, and that is the decision worth naming. Every
	// rule about an Answer needs the question's KIND, which this layer does not
	// have and must not guess: text is valid for two kinds and refused by five,
	// and a handler that decided which it was looking at would be a second copy
	// of catalog.ParseAnswer that could disagree with the first. What comes back
	// is a domain error carrying the kind and a problem token, which the form
	// turns into a sentence.
	ticket, err := h.svc.AnswerTicketQuestion(r.Context(), actorFromRequest(r), eventID, ticketID, questionID, service.AnswerInput{
		Text:      body.Text,
		Number:    body.Number,
		Date:      body.Date,
		Checked:   body.Checked,
		OptionIDs: body.OptionIDs,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, ticket)
}

// RemoveTicketAnswer takes one Ticket's Answer to one Ticket Question away.
//
// A REAL DELETE, and the only one in this feature. It is not an exception to
// "retired, never deleted": a Ticket Question and an Option are the
// Organization's own words that OTHER Tickets' Answers still point at, whereas
// an Answer points at nothing, and removing one restores the state the Ticket
// was in before anybody answered — an Outstanding Answer where the question is
// required. Somebody who ticked the wrong box needs a way back to "not said",
// and a blank Answer stored to mean that would be a row no later reader could
// tell from a real reply.
//
// @Summary      Remove a ticket's answer
// @Description  Removes one Ticket's Answer to one Ticket Question, restoring the Outstanding Answer where the question is required. Refused once the Event has started and on a reversed Ticket Sale. Org Admin and Event Owner only; Event Staff are refused. Answers 404 while the Ticket Question feature flag is off.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id           path      string  true  "Event ID"
// @Param        ticketId     path      string  true  "Ticket ID"
// @Param        questionId   path      string  true  "Ticket question ID"
// @Success      200  {object}  openapi.EnvelopeTicketAnswersDetail
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/tickets/{ticketId}/answers/{questionId} [delete]
func (h *Handler) RemoveTicketAnswer(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ticketID, ok := ticketAnswerRoute(w, r, reqID)
	if !ok {
		return
	}
	questionID, ok := pathValueRequired(w, r, reqID, "questionId")
	if !ok {
		return
	}

	ticket, err := h.svc.RemoveTicketAnswer(r.Context(), actorFromRequest(r), eventID, ticketID, questionID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, ticket)
}
