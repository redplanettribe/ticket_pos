// Package openapi holds typed response envelope wrappers for the invoicing
// endpoints, so the generated OpenAPI spec (and the api-client built from it)
// carries real `data` types instead of a bare envelope.
package openapi

import (
	"github.com/peter/ticket_pos/backend/internal/invoicing/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// EnvelopeEcuadorIssuer documents GET and PUT /operator/invoicing/issuers/ec
// success responses. `data` is null when no Issuer has been recorded.
type EnvelopeEcuadorIssuer struct {
	Data      *service.EcuadorIssuer `json:"data"`
	Error     *platform.APIError     `json:"error"`
	RequestID string                 `json:"request_id"`
}

// EnvelopeInvoiceDetail documents POST /operator/invoicing/invoices and
// GET /operator/invoicing/invoices/{id} success responses.
type EnvelopeInvoiceDetail struct {
	Data      service.InvoiceDetail `json:"data"`
	Error     *platform.APIError    `json:"error"`
	RequestID string                `json:"request_id"`
}

// EnvelopeInvoiceList documents GET /operator/invoicing/invoices success
// responses: the ADR-0006 nested envelope inside the standard one.
type EnvelopeInvoiceList struct {
	Data      service.InvoiceList `json:"data"`
	Error     *platform.APIError  `json:"error"`
	RequestID string              `json:"request_id"`
}

// EnvelopeInvoiceTotals documents POST /operator/invoicing/invoices/totals
// success responses.
type EnvelopeInvoiceTotals struct {
	Data      service.TotalsView `json:"data"`
	Error     *platform.APIError `json:"error"`
	RequestID string             `json:"request_id"`
}

// EnvelopeSaleInvoiceDrain documents POST /internal/sale-invoices/drain
// success responses.
type EnvelopeSaleInvoiceDrain struct {
	Data      service.SaleInvoiceDrainResult `json:"data"`
	Error     *platform.APIError             `json:"error"`
	RequestID string                         `json:"request_id"`
}

// EnvelopeNeedsAttentionQueue documents GET /operator/invoicing/needs-attention
// success responses: the ADR-0006 nested envelope inside the standard one
// (#477).
type EnvelopeNeedsAttentionQueue struct {
	Data      service.NeedsAttentionQueue `json:"data"`
	Error     *platform.APIError          `json:"error"`
	RequestID string                      `json:"request_id"`
}

// EnvelopeNeedsAttentionCount documents GET
// /operator/invoicing/needs-attention/count success responses (#477).
type EnvelopeNeedsAttentionCount struct {
	Data      service.NeedsAttentionCount `json:"data"`
	Error     *platform.APIError          `json:"error"`
	RequestID string                      `json:"request_id"`
}

// EnvelopeRecipientWarningCount documents GET
// /operator/invoicing/recipient-warnings/count success responses (#482).
type EnvelopeRecipientWarningCount struct {
	Data      service.RecipientWarningCount `json:"data"`
	Error     *platform.APIError            `json:"error"`
	RequestID string                        `json:"request_id"`
}

// EnvelopeUninvoicedHouseSaleList documents GET
// /operator/invoicing/uninvoiced-sales success responses: the ADR-0006
// nested envelope inside the standard one (#507).
type EnvelopeUninvoicedHouseSaleList struct {
	Data      service.UninvoicedHouseSaleList `json:"data"`
	Error     *platform.APIError              `json:"error"`
	RequestID string                          `json:"request_id"`
}

// EnvelopeUninvoicedHouseSaleCount documents GET
// /operator/invoicing/uninvoiced-sales/count success responses (#507).
type EnvelopeUninvoicedHouseSaleCount struct {
	Data      service.UninvoicedHouseSaleCount `json:"data"`
	Error     *platform.APIError               `json:"error"`
	RequestID string                           `json:"request_id"`
}

// EnvelopeCustomerDocuments documents GET
// /customer/ticket-sales/{ticketSaleId}/tax-documents success responses: the
// Sale's documents in the buyer's words, an empty list when it owes none.
type EnvelopeCustomerDocuments struct {
	Data      []service.CustomerDocument `json:"data"`
	Error     *platform.APIError         `json:"error"`
	RequestID string                     `json:"request_id"`
}
