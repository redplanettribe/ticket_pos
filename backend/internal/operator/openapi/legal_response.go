package openapi

import (
	consentsvc "github.com/peter/ticket_pos/backend/internal/consent/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// EnvelopeOperatorLegalWorkspace documents all three Legal Center responses
// (#561): the workspace read, the draft save and the draft discard.
//
// ONE ENVELOPE FOR ALL THREE, because they answer with the same payload — every
// one of them reports the document as it now stands, and a write that answered
// with less would make the editor read again to find out what it had done. The
// payload type is the consent module's own view rather than a copy: what an
// Operator is shown about a legal document is that module's answer, and a
// second struct here would be a second place for the wire contract to drift.
type EnvelopeOperatorLegalWorkspace struct {
	Data      consentsvc.OperatorLegalWorkspace `json:"data"`
	Error     *platform.APIError                `json:"error"`
	RequestID string                            `json:"request_id"`
}
