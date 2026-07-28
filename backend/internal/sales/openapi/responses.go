// Package openapi holds typed response envelope wrappers for the sales
// endpoints, so the generated OpenAPI spec (and the api-client built from it)
// carries real `data` types instead of a bare envelope.
package openapi

import (
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales/service"
)

// EnvelopeBeginCheckout documents POST .../events/{eventSlug}/checkout success responses.
type EnvelopeBeginCheckout struct {
	Data      service.BeginCheckoutResult `json:"data"`
	Error     *platform.APIError          `json:"error"`
	RequestID string                      `json:"request_id"`
}

// EnvelopeConfirmCheckout documents POST /checkout/{clientTransactionId}/confirm success responses.
type EnvelopeConfirmCheckout struct {
	Data      service.ConfirmCheckoutResult `json:"data"`
	Error     *platform.APIError            `json:"error"`
	RequestID string                        `json:"request_id"`
}

// EnvelopeSaleReversal documents POST /customer/ticket-sales/{ticketSaleId}/reverse success responses.
type EnvelopeSaleReversal struct {
	Data      service.SaleReversalResult `json:"data"`
	Error     *platform.APIError         `json:"error"`
	RequestID string                     `json:"request_id"`
}

// EnvelopeSalesSummary documents GET /staff/events/{id}/sales/summary success responses.
type EnvelopeSalesSummary struct {
	Data      service.SalesSummary `json:"data"`
	Error     *platform.APIError   `json:"error"`
	RequestID string               `json:"request_id"`
}

// EnvelopePayoutsSummary documents GET /staff/organization/payouts success responses.
type EnvelopePayoutsSummary struct {
	Data      service.PayoutsSummary `json:"data"`
	Error     *platform.APIError     `json:"error"`
	RequestID string                 `json:"request_id"`
}
