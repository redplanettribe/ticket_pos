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
	Error     *platform.APIError             `json:"error"`
	RequestID string                         `json:"request_id"`
}

// EnvelopeEventDetail documents event detail success responses.
type EnvelopeEventDetail struct {
	Data      service.EventDetail `json:"data"`
	Error     *platform.APIError         `json:"error"`
	RequestID string                     `json:"request_id"`
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
