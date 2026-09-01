package openapi

import (
	"github.com/peter/ticket_pos/backend/internal/operator/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The per-subject consent record's response envelopes (#566).
//
// THREE ENVELOPES FOR THREE READS, and the split mirrors the routes rather than
// the data. The customer record and its history are separate envelopes because
// they are separate endpoints — which is #566's own ruling: the history has its
// own address so that the landing request and page 2 return the SAME shape, and
// a single envelope carrying both would put that back the way it was.
//
// None is the ADR 0006 `{data, pagination}` envelope, following the acceptance
// browsers and for ADR 0067's reason.

// EnvelopeOperatorCustomerLegalRecord documents one Customer's record: who they
// are, both gates' standing, and the cross-link to their staff record.
type EnvelopeOperatorCustomerLegalRecord struct {
	Data      service.CustomerLegalRecordView `json:"data"`
	Error     *platform.APIError              `json:"error"`
	RequestID string                          `json:"request_id"`
}

// EnvelopeOperatorConsentActPage documents one page of a Customer's history.
//
// `visible_count` on the payload is HOW MANY ACTS ARE ON THIS PAGE and NOT a
// total. It is documented as a count rather than as pagination metadata for
// exactly that reason: with `next_cursor` it says whether the page is the whole
// record, which is the only question worth answering about evidence.
type EnvelopeOperatorConsentActPage struct {
	Data      service.ConsentActPage `json:"data"`
	Error     *platform.APIError     `json:"error"`
	RequestID string                 `json:"request_id"`
}

// EnvelopeOperatorStaffLegalRecord documents one staff person's record, found
// by Staff Digest. Unpaged, because a staff acceptance history is a few rows
// and cannot grow without bound.
type EnvelopeOperatorStaffLegalRecord struct {
	Data      service.StaffLegalRecordView `json:"data"`
	Error     *platform.APIError           `json:"error"`
	RequestID string                       `json:"request_id"`
}
