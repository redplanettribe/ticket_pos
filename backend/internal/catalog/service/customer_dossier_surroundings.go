package service

import (
	"context"
)

// What surrounds each Sale on the Customer Dossier (#639, spec #635): the phone
// given on its checkout (ADR 0073), the Affiliate Link that attributed it, its
// Sale Invoices, and whether and when it was re-addressed (ADR 0058).
//
// The phone, the link's name and the re-addressing instant are plain reads of
// this Event's own rows. The Sale Invoices are the invoicing module's answer —
// the same per-Sale documents read the operator Sale lookup lists — so no
// invoicing rule (chain order, roles, what a closed SALE_INVOICING_ENABLED
// hides) is restated here.
//
// NEVER AN ADDRESS OR A TOKEN. A re-addressing is stated as an instant only;
// the address the Sale moved from, the corrected one and the link's token are
// not read. A Sale Invoice's recipient email and address are dropped too: the
// Recipient's email is the Sale's snapshot at issue, which for a re-addressed
// Sale is exactly the address it moved from.

// DossierSaleDocuments is what the Dossier needs from invoicing: the Tax
// Invoices about one Ticket Sale, in chain order, empty for a Sale that owes
// nothing. The invoicing service satisfies it.
type DossierSaleDocuments interface {
	SaleDocuments(ctx context.Context, ticketSaleID string) ([]InvoicingSaleDocument, error)
}

// WithDossierSaleDocuments ties the invoicing read the Dossier lists each Sale's
// Sale Invoices from (#639). Tied after construction because invoicing is built
// after catalog; untied, every Sale lists none.
func (s *Service) WithDossierSaleDocuments(documents DossierSaleDocuments) *Service {
	s.saleDocuments = documents
	return s
}

// DossierTaxInvoiceView is one Tax Invoice about a Dossier Sale: what it is,
// where it stands, and who was invoiced.
type DossierTaxInvoiceView struct {
	// Kind is `sale` or `credit_note`.
	Kind string `json:"kind" enums:"sale,credit_note"`
	// Role is the document's place in the Sale's chain: `current`,
	// `superseded`, `credit_note` or `not_current`.
	Role string `json:"role" enums:"current,superseded,credit_note,not_current"`
	// Number is the document number as printed; null until signed.
	Number *string `json:"number"`
	Status string  `json:"status" enums:"owed,pending,authorized,not_authorized,rejected,needs_attention,withdrawn,annulled,abandoned"`
	// RecipientLegalName, RecipientTaxIDType and RecipientTaxID are who the
	// document invoices — a company where one was invoiced rather than the
	// person.
	RecipientLegalName string `json:"recipient_legal_name"`
	RecipientTaxIDType string `json:"recipient_tax_id_type"`
	RecipientTaxID     string `json:"recipient_tax_id"`
}

// fillDossierSaleSurroundings sets each Sale's phone, Affiliate Link name,
// re-addressing instant and Sale Invoices.
func (s *Service) fillDossierSaleSurroundings(ctx context.Context, sales []DossierSaleView) error {
	ids := make([]string, 0, len(sales))
	for _, sale := range sales {
		ids = append(ids, sale.ID)
	}
	surroundings, err := s.repo.ListDossierSaleSurroundings(ctx, ids)
	if err != nil {
		return err
	}
	for i := range sales {
		sale := &sales[i]
		around := surroundings[sale.ID]
		sale.Phone = around.Phone
		sale.AffiliateLinkName = around.AffiliateLinkName
		sale.ReAddressedAt = around.ReAddressedAt
		sale.TaxInvoices = []DossierTaxInvoiceView{}
		if s.saleDocuments == nil {
			continue
		}
		documents, err := s.saleDocuments.SaleDocuments(ctx, sale.ID)
		if err != nil {
			return err
		}
		for _, doc := range documents {
			sale.TaxInvoices = append(sale.TaxInvoices, dossierTaxInvoiceView(doc))
		}
	}
	return nil
}

// dossierTaxInvoiceView narrows invoicing's document to what the Dossier
// shows.
func dossierTaxInvoiceView(doc InvoicingSaleDocument) DossierTaxInvoiceView {
	return DossierTaxInvoiceView{
		Kind:               doc.Kind,
		Role:               string(doc.Role),
		Number:             doc.Number,
		Status:             doc.Status,
		RecipientLegalName: doc.Recipient.LegalName,
		RecipientTaxIDType: doc.Recipient.TaxIDType,
		RecipientTaxID:     doc.Recipient.TaxID,
	}
}
