// Package openapi holds typed response envelope wrappers for Home's endpoints,
// so the generated OpenAPI spec (and the api-client built from it) carries
// real `data` types instead of a bare envelope.
package openapi

import (
	"github.com/peter/ticket_pos/backend/internal/organizationtrends/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// EnvelopeHomeNow documents GET /staff/organization/home/now success
// responses.
type EnvelopeHomeNow struct {
	Data      service.HomeNow    `json:"data"`
	Error     *platform.APIError `json:"error"`
	RequestID string             `json:"request_id"`
}

// EnvelopeOrganizationTrends documents GET /staff/organization/trends
// success responses.
type EnvelopeOrganizationTrends struct {
	Data      service.OrganizationTrends `json:"data"`
	Error     *platform.APIError         `json:"error"`
	RequestID string                     `json:"request_id"`
}

// EnvelopeOrganizationTrendBreakdown documents GET
// /staff/organization/trends/breakdown success responses.
type EnvelopeOrganizationTrendBreakdown struct {
	Data      service.OrganizationTrendBreakdown `json:"data"`
	Error     *platform.APIError                 `json:"error"`
	RequestID string                             `json:"request_id"`
}
