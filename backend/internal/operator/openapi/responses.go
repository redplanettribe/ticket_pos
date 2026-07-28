// Package openapi holds typed response envelope wrappers for the operator
// endpoints, so the generated OpenAPI spec (and the api-client built from it)
// carries real `data` types instead of a bare envelope.
package openapi

import (
	"github.com/peter/ticket_pos/backend/internal/operator/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// EnvelopePlatformSummary documents GET /operator/summary success responses.
type EnvelopePlatformSummary struct {
	Data      service.PlatformSummary `json:"data"`
	Error     *platform.APIError      `json:"error"`
	RequestID string                  `json:"request_id"`
}

// EnvelopeOperatorOrganizationList documents GET /operator/organizations success
// responses: the ADR-0006 nested envelope inside the standard one.
type EnvelopeOperatorOrganizationList struct {
	Data      service.OrganizationList `json:"data"`
	Error     *platform.APIError       `json:"error"`
	RequestID string                   `json:"request_id"`
}

// EnvelopeOperatorOrganizationDetail documents GET /operator/organizations/{orgID}
// success responses.
type EnvelopeOperatorOrganizationDetail struct {
	Data      service.OrganizationDetail `json:"data"`
	Error     *platform.APIError         `json:"error"`
	RequestID string                     `json:"request_id"`
}

// EnvelopeOperatorSaleLookup documents GET /operator/sales/{confirmationRef}
// success responses.
type EnvelopeOperatorSaleLookup struct {
	Data      service.SaleLookup `json:"data"`
	Error     *platform.APIError `json:"error"`
	RequestID string             `json:"request_id"`
}

// EnvelopeOperatorSaleReversal documents POST
// /operator/sales/{confirmationRef}/reverse success responses.
type EnvelopeOperatorSaleReversal struct {
	Data      service.SaleReversal `json:"data"`
	Error     *platform.APIError   `json:"error"`
	RequestID string               `json:"request_id"`
}
