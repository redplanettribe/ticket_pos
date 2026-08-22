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

// EnvelopeHolderList documents the Event's Holder List response (#333; the
// Outstanding Answers response of #313, widened to the roster): a page of
// EVERY Ticket of the Event, each with its assignment and — where the Event
// asks Ticket Questions — the questions it still owes.
//
// The payload is NESTED rather than a bare array, because a list of rows alone
// could not carry the two counts that make it readable — how many Tickets the
// current view holds, and how many things are unknown in all.
type EnvelopeHolderList struct {
	Data      service.HolderListPage `json:"data"`
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

// EnvelopeBuyerTicketAnswers documents the buyer's own responses — the Tickets
// of their Ticket Sale with their assignment state, and the same list after an
// address has been given (#315, ADR 0044; narrowed by #344, ADR 0049).
//
// ITS DATA IS service.BuyerTicketAnswersView, distinct from TicketAnswersView
// and from HeldTicketAnswersView because each has a different reader and each
// may see a different amount. Despite its name it carries NO ANSWER: since
// ADR 0049 a Ticket's questions travel only on the held-ticket routes, to the
// Holder, and this payload says whose each Ticket is and nothing more.
//
// A BARE ARRAY and not a nested page. There is no count to carry beside it — a
// Ticket Sale's Tickets are all of them, never a page.
type EnvelopeBuyerTicketAnswers struct {
	Data      []service.BuyerTicketAnswersView `json:"data"`
	Error     *platform.APIError               `json:"error"`
	RequestID string                           `json:"request_id"`
}

// EnvelopeHeldTickets documents the list of Tickets the signed-in Customer
// HOLDS (#343, ADR 0049), and EnvelopeHeldTicket the one Ticket an Answer
// write on it returns.
//
// ITS DATA IS service.HeldTicketAnswersView, a fourth shape distinct from the three
// above and narrower than the buyer's: no Answer Link, no Holder address, no
// assignment state, no ordinal, no Sale. The reader holds the Ticket and may
// not be the party of record, so the generated client must not offer the
// Storefront anything about the purchase to render.
type EnvelopeHeldTickets struct {
	Data      []service.HeldTicketAnswersView `json:"data"`
	Error     *platform.APIError       `json:"error"`
	RequestID string                   `json:"request_id"`
}

type EnvelopeHeldTicket struct {
	Data      service.HeldTicketAnswersView `json:"data"`
	Error     *platform.APIError     `json:"error"`
	RequestID string                 `json:"request_id"`
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

// EnvelopeHolderAddressPurge documents POST
// /api/v1/internal/holder-addresses/purge success responses (#331).
//
// The payload is counts and one timestamp, and it names no address, Ticket,
// buyer or Event — see catalog/handler.PurgeUnacceptedHolderAddresses for why a
// response listing what had just been deleted would publish precisely the thing
// the deletion exists to remove.
type EnvelopeHolderAddressPurge struct {
	Data      service.HolderAddressPurgeResult `json:"data"`
	Error     *platform.APIError               `json:"error"`
	RequestID string                           `json:"request_id"`
}
