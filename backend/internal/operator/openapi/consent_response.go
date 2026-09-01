package openapi

import (
	"github.com/peter/ticket_pos/backend/internal/customers/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// EnvelopeOperatorCustomerConsent documents the Consent Withdrawal's response:
// POST /operator/legal/customers/{customerID}/withdrawal.
//
// IT USED TO DOCUMENT TWO ROUTES. The by-address lookup beside it — GET
// /operator/customers/{email}/consent — is deleted in #566, along with the
// address-keyed withdrawal, and the withdrawal now hangs off the person's
// consent record inside the Legal Center. `withdrew` is therefore never null on
// this route today: it was null on the lookup, which took nothing away because
// it performed nothing, and the field stays nullable because that is a property
// of the value rather than of this shape.
//
// The payload type is the customers module's own view rather than a copy: what
// an Operator is shown about a Customer's consents is that module's answer, and
// a second struct here would be a second place for the wire contract to drift.
type EnvelopeOperatorCustomerConsent struct {
	Data      service.OperatorCustomerConsentView `json:"data"`
	Error     *platform.APIError                  `json:"error"`
	RequestID string                              `json:"request_id"`
}
