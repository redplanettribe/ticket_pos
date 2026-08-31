package openapi

import (
	"github.com/peter/ticket_pos/backend/internal/operator/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The consent access log reader's documented payload (#569, spec #556, ADR
// 0067).
//
// ONE ENVELOPE, because there is one route: the log has no purge, no retention
// setting and no export, so there is nothing else about it to document.
//
// It is the acceptance browsers' shape and not the ADR 0006 envelope — no
// `total`, no offset, `next_cursor` null on the last page — for ADR 0067's
// reason and one of its own: this table is append-only and never purged, so a
// total over it is the most expensive number the screen could compute and the
// least useful thing it could say.

// EnvelopeOperatorLegalAccessLogPage documents one page of the access log.
type EnvelopeOperatorLegalAccessLogPage struct {
	Data      service.LegalAccessLogPage `json:"data"`
	Error     *platform.APIError         `json:"error"`
	RequestID string                     `json:"request_id"`
}
