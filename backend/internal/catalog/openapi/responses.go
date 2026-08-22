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
