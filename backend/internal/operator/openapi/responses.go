// Package openapi holds typed response envelope wrappers for the operator
// endpoints, so the generated OpenAPI spec (and the api-client built from it)
// carries real `data` types instead of a bare envelope.
package openapi

import (
	"github.com/peter/ticket_pos/backend/internal/operator/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// EnvelopePlatformSummary documents GET /operator/summary success responses.
type EnvelopePlatformSummary struct {
	Data      service.PlatformSummary `json:"data"`
	Error     *platform.APIError      `json:"error"`
	RequestID string                  `json:"request_id"`
}

// EnvelopeOperatorOrganizationList documents GET /operator/organizations success
// responses: the ADR-0006 nested envelope inside the standard one.
type EnvelopeOperatorOrganizationList struct {
	Data      service.OrganizationList `json:"data"`
	Error     *platform.APIError       `json:"error"`
	RequestID string                   `json:"request_id"`
}

// EnvelopeOperatorOrganizationDetail documents GET /operator/organizations/{orgID}
// success responses.
type EnvelopeOperatorOrganizationDetail struct {
	Data      service.OrganizationDetail `json:"data"`
	Error     *platform.APIError         `json:"error"`
	RequestID string                     `json:"request_id"`
}

// EnvelopeOperatorSaleLookup documents GET /operator/sales/{confirmationRef}
// success responses.
type EnvelopeOperatorSaleLookup struct {
	Data      service.SaleLookup `json:"data"`
	Error     *platform.APIError `json:"error"`
	RequestID string             `json:"request_id"`
}

// EnvelopeOperatorSaleReAddressing documents POST
// /operator/sales/{confirmationRef}/re-address success responses (#420).
type EnvelopeOperatorSaleReAddressing struct {
	Data      service.ReAddressing `json:"data"`
	Error     *platform.APIError   `json:"error"`
	RequestID string               `json:"request_id"`
}

// EnvelopeOperatorPayoutRequestQueue documents GET /operator/payout-requests
// success responses: the ADR-0006 nested envelope inside the standard one. The
// account numbers on these rows are masked (#176).
type EnvelopeOperatorPayoutRequestQueue struct {
	Data      service.PayoutRequestQueue `json:"data"`
	Error     *platform.APIError         `json:"error"`
	RequestID string                     `json:"request_id"`
}

// EnvelopeOperatorPendingPayoutRequestCount documents GET
// /operator/payout-requests/count success responses.
type EnvelopeOperatorPendingPayoutRequestCount struct {
	Data      service.PendingPayoutRequestCount `json:"data"`
	Error     *platform.APIError                `json:"error"`
	RequestID string                            `json:"request_id"`
}

// EnvelopeOperatorPayoutRequestDetail documents GET
// /operator/payout-requests/{requestID} success responses — the one payload on
// this surface that carries a whole account number.
type EnvelopeOperatorPayoutRequestDetail struct {
	Data      service.PayoutRequestDetail `json:"data"`
	Error     *platform.APIError          `json:"error"`
	RequestID string                      `json:"request_id"`
}

// EnvelopeOperatorPayoutFulfilment documents POST
// /operator/payout-requests/{requestID}/fulfil success responses: the Payout
// that is now in the ledger, and the request it answered (#177).
type EnvelopeOperatorPayoutFulfilment struct {
	Data      service.PayoutFulfilment `json:"data"`
	Error     *platform.APIError       `json:"error"`
	RequestID string                   `json:"request_id"`
}

// EnvelopeOperatorPayoutRequest documents POST
// /operator/payout-requests/{requestID}/decline success responses: the request
// as it now stands, carrying the reason the Organization will read.
type EnvelopeOperatorPayoutRequest struct {
	Data      service.PayoutRequestWhole `json:"data"`
	Error     *platform.APIError         `json:"error"`
	RequestID string                     `json:"request_id"`
}

// EnvelopeOperatorSaleReversal documents POST
// /operator/sales/{confirmationRef}/reverse success responses.
type EnvelopeOperatorSaleReversal struct {
	Data      service.SaleReversal `json:"data"`
	Error     *platform.APIError   `json:"error"`
	RequestID string               `json:"request_id"`
}

// EnvelopeOperatorTicketQuestions documents GET
// /operator/events/{eventID}/ticket-questions success responses (#410).
type EnvelopeOperatorTicketQuestions struct {
	Data      []service.TicketQuestion `json:"data"`
	Error     *platform.APIError       `json:"error"`
	RequestID string                   `json:"request_id"`
}

// EnvelopeOperatorRevokedTicketQuestion documents POST
// /operator/ticket-questions/{questionID}/revoke success responses: the
// question as it now stands, retired and carrying the reason (#410).
type EnvelopeOperatorRevokedTicketQuestion struct {
	Data      service.RevokedTicketQuestion `json:"data"`
	Error     *platform.APIError            `json:"error"`
	RequestID string                        `json:"request_id"`
}

// EnvelopeOperatorQuestionReviewQueue documents GET /operator/question-reviews
// success responses (#407).
type EnvelopeOperatorQuestionReviewQueue struct {
	Data      service.QuestionReviewQueue `json:"data"`
	Error     *platform.APIError          `json:"error"`
	RequestID string                      `json:"request_id"`
}

// EnvelopeOperatorOutstandingQuestionReviewCount documents GET
// /operator/question-reviews/count success responses (#407).
type EnvelopeOperatorOutstandingQuestionReviewCount struct {
	Data      service.OutstandingQuestionReviewCount `json:"data"`
	Error     *platform.APIError                     `json:"error"`
	RequestID string                                 `json:"request_id"`
}

// EnvelopeOperatorQuestionReview documents GET /operator/question-reviews/{reviewID}
// and POST /operator/question-reviews/{reviewID}/answer success responses:
// one Review whole, with each item's question and Option (#407).
type EnvelopeOperatorQuestionReview struct {
	Data      service.QuestionReview `json:"data"`
	Error     *platform.APIError     `json:"error"`
	RequestID string                 `json:"request_id"`
}
