package openapi

import (
	"github.com/peter/ticket_pos/backend/internal/customers/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// EnvelopeOperatorCustomerConsent documents both operator consent responses:
// GET /operator/customers/{email}/consent and the withdrawal POST beside it.
//
// ONE ENVELOPE FOR BOTH, because they answer with the same payload — the
// withdrawal reports the Customer's state AFTER the act, which is the same
// question the lookup asks before it. The one field that differs is `withdrew`,
// null on the lookup because a read takes nothing away, and that is a property
// of the value rather than of the shape.
//
// The payload type is the customers module's own view rather than a copy: what
// an Operator is shown about a Customer's consents is that module's answer, and
// a second struct here would be a second place for the wire contract to drift.
type EnvelopeOperatorCustomerConsent struct {
	Data      service.OperatorCustomerConsentView `json:"data"`
	Error     *platform.APIError                  `json:"error"`
	RequestID string                              `json:"request_id"`
}
