package openapi

// The record-payout envelope lives in its own file so it can import the sales
// service package unaliased: swag (v2.0.0-rc5) cannot resolve a type behind an
// aliased import, and responses.go already imports the operator service package
// under the name `service`.

import (
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales/service"
)

// EnvelopeOperatorPayout documents POST /operator/organizations/{orgID}/payouts
// success responses.
type EnvelopeOperatorPayout struct {
	Data      service.OperatorPayout `json:"data"`
	Error     *platform.APIError     `json:"error"`
	RequestID string                 `json:"request_id"`
}
