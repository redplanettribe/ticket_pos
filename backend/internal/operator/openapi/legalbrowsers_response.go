package openapi

import (
	"github.com/peter/ticket_pos/backend/internal/operator/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The two acceptance browsers' response envelopes (#565).
//
// TWO ENVELOPES AND NOT ONE, mirroring the two screens. The payloads differ in
// exactly the ways the populations do — a Customer row carries an opaque id and
// two status columns, a staff row carries a digest and one — and a single
// envelope with both halves nullable would document a response that is never
// sent.
//
// NEITHER IS THE ADR-0006 `{data, pagination}` envelope, and ADR 0067 records
// the departure: there is no `page`, no `page_size`, no `total_pages` and no
// `total`. The whole of "is there more?" is `next_cursor` being non-null.

// EnvelopeOperatorCustomerAcceptancePage documents the customer browser's page.
type EnvelopeOperatorCustomerAcceptancePage struct {
	Data      service.CustomerAcceptancePage `json:"data"`
	Error     *platform.APIError             `json:"error"`
	RequestID string                         `json:"request_id"`
}

// EnvelopeOperatorStaffAcceptancePage documents the staff browser's page.
type EnvelopeOperatorStaffAcceptancePage struct {
	Data      service.StaffAcceptancePage `json:"data"`
	Error     *platform.APIError          `json:"error"`
	RequestID string                      `json:"request_id"`
}
