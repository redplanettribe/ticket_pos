package openapi

import (
	"github.com/peter/ticket_pos/backend/internal/catalog/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/storage"
)

// MessageData is a simple message payload in the success envelope.
type MessageData struct {
	Message string `json:"message"`
}

// EnvelopeEventList documents GET /api/v1/staff/events success responses.
type EnvelopeEventList struct {
	Data      []service.EventListItem `json:"data"`
	Error     *platform.APIError      `json:"error"`
	RequestID string                  `json:"request_id"`
}

// EnvelopeEventDetail documents event detail success responses.
type EnvelopeEventDetail struct {
	Data      service.EventDetail `json:"data"`
	Error     *platform.APIError  `json:"error"`
	RequestID string              `json:"request_id"`
}

// EnvelopeMessage documents simple message success responses.
type EnvelopeMessage struct {
	Data      MessageData        `json:"data"`
	Error     *platform.APIError `json:"error"`
	RequestID string             `json:"request_id"`
}

// EnvelopeCoverUploadURL documents cover upload URL success responses.
type EnvelopeCoverUploadURL struct {
	Data      storage.CoverUploadResult `json:"data"`
	Error     *platform.APIError        `json:"error"`
	RequestID string                    `json:"request_id"`
}

// EnvelopeTicketTypeList documents ticket type list success responses.
type EnvelopeTicketTypeList struct {
	Data      []service.TicketTypeDetail `json:"data"`
	Error     *platform.APIError         `json:"error"`
	RequestID string                     `json:"request_id"`
}

// EnvelopeTicketTypeDetail documents ticket type detail success responses.
type EnvelopeTicketTypeDetail struct {
	Data      service.TicketTypeDetail `json:"data"`
	Error     *platform.APIError       `json:"error"`
	RequestID string                   `json:"request_id"`
}

// EnvelopeTicketQuestionList documents Ticket Question list success responses.
type EnvelopeTicketQuestionList struct {
	Data      []service.TicketQuestionView `json:"data"`
	Error     *platform.APIError           `json:"error"`
	RequestID string                       `json:"request_id"`
}

// EnvelopeTicketQuestionDetail documents the single-Ticket-Question responses.
// Every Option write answers with the whole question rather than the Option, so
// an editor never has to reassemble one from a fragment.
type EnvelopeTicketQuestionDetail struct {
	Data      service.TicketQuestionView `json:"data"`
	Error     *platform.APIError         `json:"error"`
	RequestID string                     `json:"request_id"`
}

// EnvelopeTicketAnswersList documents the Tickets-of-a-Ticket-Sale response:
// one entry per Ticket, each carrying its Ticket Type's Ticket Questions and
// what that Ticket has answered (#310).
type EnvelopeTicketAnswersList struct {
	Data      []service.TicketAnswersView `json:"data"`
	Error     *platform.APIError          `json:"error"`
	RequestID string                      `json:"request_id"`
}

// EnvelopeTicketAnswersDetail documents the single-Ticket responses. Every
// Answer write answers with the WHOLE Ticket rather than the one Answer, so a
// form redraws from one payload instead of reassembling the Ticket from a
// fragment — the same arrangement the Option endpoints have with their question.
type EnvelopeTicketAnswersDetail struct {
	Data      service.TicketAnswersView `json:"data"`
	Error     *platform.APIError        `json:"error"`
	RequestID string                    `json:"request_id"`
}

// EnvelopeOutstandingAnswers documents the Event's Outstanding Answers response
// (#313): a page of the Tickets that still owe required Answers, each naming the
// questions it owes, with the Event's total debt count beside the page.
//
// The payload is NESTED rather than a bare array, because a list of rows alone
// could not carry the two counts that make it readable — how many Tickets are
// waiting on the Organization, and how many things are unknown in all.
type EnvelopeOutstandingAnswers struct {
	Data      service.OutstandingAnswersPage `json:"data"`
	Error     *platform.APIError             `json:"error"`
	RequestID string                         `json:"request_id"`
}

// EnvelopeAnswerLink documents both Answer Link responses — opening one and
// answering through one (#312, ADR 0044).
//
// ITS DATA IS service.AnswerLinkView AND NOT TicketAnswersView, and the two must
// never be conflated. This one is what an UNAUTHENTICATED holder sees, and it
// carries the Event name, the Ticket Type name and the questions — never the
// buyer, the price, the Tax ID, the Sale Confirmation reference, the Ticket
// Sale's id, or anything about the Sale's other Tickets. Documenting it as the
// staff shape would put every one of those into the generated client's types and
// invite somebody to render them.
type EnvelopeAnswerLink struct {
	Data      service.AnswerLinkView `json:"data"`
	Error     *platform.APIError     `json:"error"`
	RequestID string                 `json:"request_id"`
}

// EnvelopeAssignmentLink documents all three Assignment Link responses —
// accepting one, naming the Holder, and answering through it (#325, ADR 0046).
//
// ITS DATA IS service.AssignmentLinkView, WHICH IS A FOURTH SHAPE AND NOT ANY OF
// THE OTHER THREE. It is what a Holder sees: the Event name, the Ticket Type
// name, their OWN name for the form to prefill, and this Ticket's questions.
// Never the buyer, the price, the Tax ID, the Sale Confirmation reference or the
// Sale's other Tickets — being a Customer of this platform buys nobody a fact
// about somebody else's purchase.
//
// Documenting it as any of the others would put every one of those into the
// generated client's types and invite somebody to render them.
type EnvelopeAssignmentLink struct {
	Data      service.AssignmentLinkView `json:"data"`
	Error     *platform.APIError         `json:"error"`
	RequestID string                     `json:"request_id"`
}

// EnvelopeBuyerTicketAnswers documents both of the buyer's own responses — the
// Tickets of their Ticket Sale, and the same list after one has been answered
// (#315, ADR 0044).
//
// ITS DATA IS service.BuyerTicketAnswersView AND IS THE THIRD SHAPE THIS FEATURE
// HAS, distinct from TicketAnswersView and from AnswerLinkView. The three exist
// because each has a different reader and each may see a different amount, and
// the generated client is where conflating them would do the damage: this is the
// only one of the three carrying an ANSWER LINK, and the only one whose reader
// has proved they own the purchase.
//
// A BARE ARRAY and not a nested page. There is no count to carry beside it — a
// Ticket Sale's Tickets are all of them, never a page — and the per-Ticket
// outstanding count travels on each row, where it belongs.
type EnvelopeBuyerTicketAnswers struct {
	Data      []service.BuyerTicketAnswersView `json:"data"`
	Error     *platform.APIError               `json:"error"`
	RequestID string                           `json:"request_id"`
}

// EnvelopeTagList documents tag list success responses (search and event tags).
type EnvelopeTagList struct {
	Data      []service.TagView  `json:"data"`
	Error     *platform.APIError `json:"error"`
	RequestID string             `json:"request_id"`
}

// EnvelopePublicEventPage documents GET /api/v1/public/events success responses.
type EnvelopePublicEventPage struct {
	Data      service.PublicEventPage `json:"data"`
	Error     *platform.APIError      `json:"error"`
	RequestID string                  `json:"request_id"`
}

// EnvelopePublicOrganizationEvents documents public organization events responses.
type EnvelopePublicOrganizationEvents struct {
	Data      service.PublicOrganizationEvents `json:"data"`
	Error     *platform.APIError               `json:"error"`
	RequestID string                           `json:"request_id"`
}

// EnvelopePublicEventDetail documents public event detail responses.
type EnvelopePublicEventDetail struct {
	Data      service.PublicEventDetail `json:"data"`
	Error     *platform.APIError        `json:"error"`
	RequestID string                    `json:"request_id"`
}
