package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
)

// The Customer Area's view of a Sale's documents (#475, ADR 0060): what the
// buyer sees on their Sale's card, and the download behind it.
//
// THE BUYER IS TOLD TWO THINGS AND NEVER A THIRD. A document is either
// authorized — here it is, download it — or on its way; owed, pending and
// needs_attention are all "on its way", because whatever the authority said
// or the platform could not do is the Operator's to read and remedy, never
// the buyer's problem to be handed (#471 story 15). No message, no attempt,
// no number travels here. A document that was withdrawn was never sent and
// is never shown: to the buyer it does not exist, exactly as the sale it
// belonged to no longer stands.
//
// SCOPED TWICE. The repository returns a document only to the Customer whose
// Sale it is; and a Confirmation Link session is narrowed to the one Sale it
// names before the repository is asked, as every other buyer read is
// (catalog/service ListBuyerTicketAnswers). Another Sale — even the same
// buyer's — answers as if it had no documents, and another Customer's
// download answers as if there were no such document, because "not yours"
// and "not there" must be one answer.

// CustomerDocumentStatus is where a document stands, in the buyer's words.
type CustomerDocumentStatus string

const (
	// CustomerDocumentAuthorized: the legal artifact exists; download it.
	CustomerDocumentAuthorized CustomerDocumentStatus = "authorized"
	// CustomerDocumentOnItsWay: owed, pending or parked for an operator —
	// the buyer is told the same thing in every case.
	CustomerDocumentOnItsWay CustomerDocumentStatus = "on_its_way"
)

// CustomerDocument is one document as the Sale's card lists it.
type CustomerDocument struct {
	ID string `json:"id"`
	// Kind is `sale` (a factura) or `credit_note`; the card names each by
	// its own word.
	Kind string `json:"kind"`
	// Status is `authorized` or `on_its_way`, and nothing else.
	Status CustomerDocumentStatus `json:"status"`
	// DownloadURL is the API path of the signed XML once authorized, null
	// before: the card draws the download exactly where this is set.
	DownloadURL *string `json:"download_url"`
}

// CustomerSaleDocuments lists the documents of one of the Customer's Ticket
// Sales for the Customer Area: an empty list for a Sale that owes none, for
// one that is not theirs, and for one a Confirmation Link session does not
// name.
func (s *Service) CustomerSaleDocuments(ctx context.Context, customerID, sessionTicketSaleID, ticketSaleID string) ([]CustomerDocument, error) {
	if sessionTicketSaleID != "" && sessionTicketSaleID != ticketSaleID {
		return []CustomerDocument{}, nil
	}
	rows, err := s.repo.ListSaleDocumentsForCustomer(ctx, customerID, ticketSaleID)
	if err != nil {
		return nil, err
	}
	out := make([]CustomerDocument, 0, len(rows))
	for i := range rows {
		if doc, ok := customerDocumentView(&rows[i]); ok {
			out = append(out, doc)
		}
	}
	return out, nil
}

// CustomerSaleDocumentXML hands the buyer the signed XML of one of their
// Sale's authorized documents, and answers INVOICE_NOT_FOUND for everything
// else — another Sale, another Customer, a document not yet authorized —
// with one refusal for all of them.
func (s *Service) CustomerSaleDocumentXML(ctx context.Context, customerID, sessionTicketSaleID, ticketSaleID, invoiceID string) (*invoicing.Document, error) {
	if sessionTicketSaleID != "" && sessionTicketSaleID != ticketSaleID {
		return nil, invoicing.ErrInvoiceNotFound()
	}
	row, err := s.repo.GetSaleDocumentForCustomer(ctx, customerID, ticketSaleID, invoiceID)
	if err != nil {
		return nil, err
	}
	if row == nil || row.Invoice.Status != invoicing.InvoiceStatusAuthorized || row.Ecuador == nil || !row.Invoice.Signed() {
		return nil, invoicing.ErrInvoiceNotFound()
	}
	return &invoicing.Document{
		Filename:    row.Ecuador.AccessKey + ".xml",
		ContentType: invoicing.ContentTypeXML,
		Body:        row.Invoice.SignedXML,
	}, nil
}

// customerDocumentView is one row in the buyer's words, or nothing for a
// document the buyer is never shown.
func customerDocumentView(row *repository.InvoiceRow) (CustomerDocument, bool) {
	inv := &row.Invoice
	doc := CustomerDocument{ID: inv.ID, Kind: string(inv.Kind)}
	switch inv.Status {
	case invoicing.InvoiceStatusAuthorized:
		doc.Status = CustomerDocumentAuthorized
		url := CustomerDocumentXMLPath(inv.TicketSaleID, inv.ID)
		doc.DownloadURL = &url
	case invoicing.InvoiceStatusOwed, invoicing.InvoiceStatusPending, invoicing.InvoiceStatusNeedsAttention:
		doc.Status = CustomerDocumentOnItsWay
	default:
		// withdrawn, annulled, and the manual-only refusals a Sale document
		// never carries: nothing the buyer is shown.
		return CustomerDocument{}, false
	}
	return doc, true
}

// CustomerDocumentXMLPath is the API path of a Sale document's XML for the
// buyer, spelled once for the view and the route.
func CustomerDocumentXMLPath(ticketSaleID, invoiceID string) string {
	return "/api/v1/customer/ticket-sales/" + ticketSaleID + "/tax-documents/" + invoiceID + "/xml"
}
