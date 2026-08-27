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
// THE CHAIN IS SHOWN, IN THE BUYER'S WORDS (#485, ADR 0061). After a Sale
// Invoice Reissue the Sale has more than one factura, and the buyer must
// find every document they were delivered and know which one stands: each
// document carries a Role — `current`, `superseded`, `credit_note` — and
// the ids it points at, so the storefront can order and label the chain
// without knowing why it exists. The superseded factura stays authorized
// and downloadable: it is a legal document the buyer received.
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

// CustomerDocumentRole is what a document is to the Sale today.
type CustomerDocumentRole string

const (
	// CustomerDocumentCurrent: the Sale's current factura — the one that
	// stands, or the only one there ever was.
	CustomerDocumentCurrent CustomerDocumentRole = "current"
	// CustomerDocumentSuperseded: a factura a Sale Invoice Reissue
	// corrected; still authorized, still the buyer's, no longer current.
	CustomerDocumentSuperseded CustomerDocumentRole = "superseded"
	// CustomerDocumentCreditNote: a nota de crédito, whatever it was owed
	// for; the buyer is told why by mail, never here.
	CustomerDocumentCreditNote CustomerDocumentRole = "credit_note"
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
	// Role is `current`, `superseded` or `credit_note` (#485): the card
	// orders the chain by it and labels a superseded factura.
	Role CustomerDocumentRole `json:"role"`
	// SupersedesInvoiceID is, on a factura a reissue produced, the factura
	// it corrects; SupersededByInvoiceID, on a superseded factura, the one
	// that corrects it; CreditsInvoiceID, on a Credit Note, the factura it
	// credits. Each names another document of this same list, null when
	// there is none.
	SupersedesInvoiceID   *string `json:"supersedes_invoice_id"`
	SupersededByInvoiceID *string `json:"superseded_by_invoice_id"`
	CreditsInvoiceID      *string `json:"credits_invoice_id"`
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
	doc := CustomerDocument{
		ID:                    inv.ID,
		Kind:                  string(inv.Kind),
		Role:                  customerDocumentRole(inv),
		SupersedesInvoiceID:   optionalID(inv.SupersedesInvoiceID),
		SupersededByInvoiceID: optionalID(inv.SupersededByInvoiceID),
		CreditsInvoiceID:      optionalID(inv.CreditsInvoiceID),
	}
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

// customerDocumentRole reads a document's place in the Sale's chain: a
// Credit Note is one whatever it was owed for; a factura with a live
// successor is superseded; any other factura is the Sale's current one.
func customerDocumentRole(inv *invoicing.Invoice) CustomerDocumentRole {
	switch {
	case inv.Kind == invoicing.DocumentKindCreditNote:
		return CustomerDocumentCreditNote
	case inv.SupersededByInvoiceID != "":
		return CustomerDocumentSuperseded
	default:
		return CustomerDocumentCurrent
	}
}

func optionalID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

// CustomerDocumentXMLPath is the API path of a Sale document's XML for the
// buyer, spelled once for the view and the route.
func CustomerDocumentXMLPath(ticketSaleID, invoiceID string) string {
	return "/api/v1/customer/ticket-sales/" + ticketSaleID + "/tax-documents/" + invoiceID + "/xml"
}
