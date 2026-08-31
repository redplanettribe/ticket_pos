// Package openapi holds the typed success-envelope shapes the consent handlers
// reference from their swagger annotations, so the generated spec documents the
// real `data` payload rather than an untyped envelope.
package openapi

import (
	"github.com/peter/ticket_pos/backend/internal/consent/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// EnvelopePrivacyPolicy documents GET /api/v1/public/privacy-policy/{locale}
// success responses.
type EnvelopePrivacyPolicy struct {
	Data      service.PolicyView `json:"data"`
	Error     *platform.APIError `json:"error"`
	RequestID string             `json:"request_id"`
}

// EnvelopeTerms documents GET /api/v1/public/terms/{locale} success responses.
type EnvelopeTerms struct {
	Data      service.TermsView  `json:"data"`
	Error     *platform.APIError `json:"error"`
	RequestID string             `json:"request_id"`
}
