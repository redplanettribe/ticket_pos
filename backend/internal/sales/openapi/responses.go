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

// EnvelopeReversalDrain documents POST /internal/reversals/drain success responses.
type EnvelopeReversalDrain struct {
	Data      service.ReversalDrainResult `json:"data"`
	Error     *platform.APIError          `json:"error"`
	RequestID string                      `json:"request_id"`
}

// EnvelopeSalesSummary documents GET /staff/events/{id}/sales/summary success responses.
type EnvelopeSalesSummary struct {
	Data      service.SalesSummary `json:"data"`
	Error     *platform.APIError   `json:"error"`
	RequestID string               `json:"request_id"`
}

// EnvelopePayoutProfile documents GET and PUT /staff/organization/payout-profile
// success responses. The data is nullable in practice — an Organization that has
// never recorded a Payout Profile reads back `null` — which the generated schema
// cannot say and the endpoint's description does.
type EnvelopePayoutProfile struct {
	Data      service.PayoutProfile `json:"data"`
	Error     *platform.APIError    `json:"error"`
	RequestID string                `json:"request_id"`
}

// EnvelopePayoutRequest documents POST /staff/organization/payout-requests and
// the cancel route's success responses. The 201/200 distinction on the
// submission — recorded, versus the outstanding request handed back — is carried
// by the status code rather than the body, which is the same shape either way.
type EnvelopePayoutRequest struct {
	Data      service.PayoutRequest `json:"data"`
	Error     *platform.APIError    `json:"error"`
	RequestID string                `json:"request_id"`
}

// EnvelopePayoutRequests documents GET /staff/organization/payout-requests
// success responses: the Organization's own request history, newest first.
type EnvelopePayoutRequests struct {
	Data      []service.PayoutRequest `json:"data"`
	Error     *platform.APIError      `json:"error"`
	RequestID string                  `json:"request_id"`
}

// EnvelopePayoutsSummary documents GET /staff/organization/payouts success responses.
type EnvelopePayoutsSummary struct {
	Data      service.PayoutsSummary `json:"data"`
	Error     *platform.APIError     `json:"error"`
	RequestID string                 `json:"request_id"`
}
