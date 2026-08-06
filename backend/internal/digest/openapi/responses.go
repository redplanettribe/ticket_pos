// Package openapi holds typed response envelope wrappers for the Follow Digest
// pipeline's internal endpoints, so the generated OpenAPI spec (and the
// api-client built from it) carries real `data` types instead of a bare
// envelope.
package openapi

import (
	"github.com/peter/ticket_pos/backend/internal/digest/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// EnvelopeFollowDigestEnqueue documents POST /internal/follow-digests/enqueue
// success responses.
type EnvelopeFollowDigestEnqueue struct {
	Data      service.EnqueueResult `json:"data"`
	Error     *platform.APIError    `json:"error"`
	RequestID string                `json:"request_id"`
}

// EnvelopeFollowDigestDrain documents POST /internal/follow-digests/drain
// success responses.
type EnvelopeFollowDigestDrain struct {
	Data      service.DrainResult `json:"data"`
	Error     *platform.APIError  `json:"error"`
	RequestID string              `json:"request_id"`
}
