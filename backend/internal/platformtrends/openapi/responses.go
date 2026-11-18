// Package openapi holds typed response envelope wrappers for the Platform
// Trends endpoints, so the generated OpenAPI spec (and the api-client built
// from it) carries real `data` types instead of a bare envelope.
package openapi

import (
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platformtrends/service"
)

// EnvelopePlatformTrends documents GET /operator/platform-trends success
// responses.
type EnvelopePlatformTrends struct {
	Data      service.PlatformTrends `json:"data"`
	Error     *platform.APIError     `json:"error"`
	RequestID string                 `json:"request_id"`
}

// EnvelopeDailyClose documents POST /internal/platform-trends/close success
// responses.
type EnvelopeDailyClose struct {
	Data      service.DailyCloseResult `json:"data"`
	Error     *platform.APIError       `json:"error"`
	RequestID string                   `json:"request_id"`
}
