// Package openapi holds typed response envelope wrappers for the invoicing
// endpoints, so the generated OpenAPI spec (and the api-client built from it)
// carries real `data` types instead of a bare envelope.
package openapi

import (
	"github.com/peter/ticket_pos/backend/internal/invoicing/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// EnvelopeEcuadorIssuer documents GET and PUT /operator/invoicing/issuers/ec
// success responses. `data` is null when no Issuer has been recorded.
type EnvelopeEcuadorIssuer struct {
	Data      *service.EcuadorIssuer `json:"data"`
	Error     *platform.APIError     `json:"error"`
	RequestID string                 `json:"request_id"`
}
