// Package openapi documents the Affiliate Links response envelopes for swaggo.
package openapi

import (
	"github.com/peter/ticket_pos/backend/internal/affiliates/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// EnvelopeAffiliateLink documents Affiliate Link create success responses.
type EnvelopeAffiliateLink struct {
	Data      service.AffiliateLinkView `json:"data"`
	Error     *platform.APIError        `json:"error"`
	RequestID string                    `json:"request_id"`
}

// EnvelopeAffiliateTrends documents GET /staff/events/{id}/affiliate-links/trends
// success responses: the affiliate tab's whole trends payload in one bounded
// read (ADR 0057).
type EnvelopeAffiliateTrends struct {
	Data      service.AffiliateTrends `json:"data"`
	Error     *platform.APIError      `json:"error"`
	RequestID string                  `json:"request_id"`
}

// EnvelopeAffiliateLinkList documents Affiliate Link list success responses.
type EnvelopeAffiliateLinkList struct {
	Data      []service.AffiliateLinkView `json:"data"`
	Error     *platform.APIError          `json:"error"`
	RequestID string                      `json:"request_id"`
}
