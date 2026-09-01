package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
)

// The Sale Invoice Reissue (#483, parent #478, ADR 0061): a Platform
// Operator's correction of an authorized Sale Invoice whose Recipient is
// wrong. In ONE transaction the platform owes a Credit Note for the full
// amount against the factura — the same Recipient, reason `reissue` — and a
// corrected Sale Invoice to the Recipient as entered, with the email the
// Sale carries at that moment, the original lines and the stored rate,
// naming the factura it supersedes. Never an edit: the Sale, its money, its
// Tickets, its stored buyer and the Customer's stored Tax ID are read (one
// column, under a lock) and never written, and the superseded factura stays
// on file exactly as it was. What follows is the Drainer's (drainer.go):
// the Credit Note first, the corrected factura once it is authorized.
//
// READ-THEN-GUARDED-WRITE, as Mark annulled is. The refusals are decided
// on the row as read, and decided AGAIN inside the transaction on the same
// facts under the Sale's lock (repository.ReissueSaleInvoice), so a reissue
// that races a reversal — or a Drainer write, or a second operator — finds
// what then stands and refuses rather than double-writing. The pre-read
// exists so that an ordinary refusal costs no lock.

// ReissueInput is the corrected Recipient as the operator entered it,
// validated for shape by the handler, and the reissue's trail. There is no
// email: the Sale's is taken at the moment of the act.
type ReissueInput struct {
	ReissuedBy string
	Recipient  invoicing.Recipient
	Note       string
}

// ReissueSaleInvoice owes the Credit Note and the corrected Sale Invoice,
// kicks the Drainer as a checkout does, and returns the corrected document.
func (s *Service) ReissueSaleInvoice(ctx context.Context, id string, in ReissueInput) (*InvoiceDetail, error) {
	if !s.saleInvoicingEnabled {
		return nil, invoicing.ErrSaleInvoicingUnavailable()
	}
	row, err := s.repo.GetInvoice(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, invoicing.ErrInvoiceNotFound()
	}
	if err := reissuableDocument(&row.Invoice); err != nil {
		return nil, err
	}

	now := s.clock()
	correctedID, err := s.repo.ReissueSaleInvoice(ctx, id, func(factura *repository.InvoiceRow, facts repository.ReissueSaleFacts) (invoicing.Invoice, invoicing.Invoice, error) {
		if err := reissueRefusal(&factura.Invoice, facts); err != nil {
			return invoicing.Invoice{}, invoicing.Invoice{}, err
		}
		note := creditNoteOf(&factura.Invoice, invoicing.CreditNoteReasonReissue, now)
		corrected := correctedSaleInvoiceOf(&factura.Invoice, in, facts.SaleEmail, now)
		return note, corrected, nil
	})
	if err != nil {
		return nil, err
	}
	if correctedID == "" {
		return nil, invoicing.ErrInvoiceNotFound()
	}
	// Counts and ids only: no Recipient, no buyer (#471 story 35).
	s.logger.Info("invoicing: sale invoice reissued",
		"superseded_invoice_id", id, "corrected_invoice_id", correctedID, "reissued_by", in.ReissuedBy, "has_note", in.Note != "")
	s.KickSaleInvoiceDrainer(ctx, row.Invoice.TicketSaleID)
	return s.GetInvoice(ctx, correctedID)
}

// reissuableDocument is what can be refused from the document's own row:
// its kind and its state. A manual Tax Invoice is issued again by hand; a
// Credit Note is never credited; only an authorized factura is reissued —
// one owed, pending or parked has Resend and Mark annulled as its path, and
// one withdrawn or annulled no longer stands.
func reissuableDocument(inv *invoicing.Invoice) error {
	switch inv.Kind {
	case invoicing.DocumentKindManual:
		return invoicing.ErrInvoiceManualNotReissuable()
	case invoicing.DocumentKindCreditNote:
		return invoicing.ErrCreditNoteNotReissuable()
	}
	if inv.Status != invoicing.InvoiceStatusAuthorized {
		return invoicing.ErrInvoiceNotAuthorized(inv.Status)
	}
	return nil
}

// reissueRefusal is the whole decision, on the factura as locked and the
// Sale's facts: the document's own refusals, then the Sale's. A reversed
// Sale's income is never re-declared; a reissue still in flight on the
// Sale is answered before "superseded", because during the flight the
// factura is both and the flight is what the operator can wait out; a
// superseded factura points the operator at the current one — and only a
// LIVE successor makes it superseded, since #579 taught the successor read
// that withdrawn, annulled and abandoned are all deaths (ADR 0068), so a
// factura whose correction died at the authority falls through to whichever
// refusal is actually true of it; and a
// factura an authorized Credit Note already stands against, with no live
// successor, is the Sale-without-a-current-factura gap (#480), never
// credited twice — the gap #580's Issue again closes by owing a fresh
// factura rather than a second Credit Note.
func reissueRefusal(inv *invoicing.Invoice, facts repository.ReissueSaleFacts) error {
	if err := reissuableDocument(inv); err != nil {
		return err
	}
	switch {
	case facts.SaleStatus == "reversed":
		return invoicing.ErrInvoiceSaleReversed()
	case facts.ReissueInFlight:
		return invoicing.ErrReissueInFlight()
	case inv.SupersededByInvoiceID != "":
		return invoicing.ErrInvoiceSuperseded()
	case facts.CreditedByAuthorized:
		return invoicing.ErrInvoiceAlreadyCredited()
	}
	return nil
}

// correctedSaleInvoiceOf is the fresh Sale Invoice a reissue owes: the
// factura's own document minus everything that needs an Issuer, of kind
// sale, to the corrected Recipient — Tax ID Type, number, legal name and
// address as the operator entered them, the email the Sale carries now —
// with the original lines, totals and rate, naming the factura it
// supersedes and carrying the reissue's trail. Copied from the factura ROW
// and never from the Sale or the catalog: what is corrected is who the
// document names, and nothing else (CONTEXT.md, Sale Invoice Reissue).
// Pure, so the copy is testable without a database.
func correctedSaleInvoiceOf(factura *invoicing.Invoice, in ReissueInput, saleEmail string, now time.Time) invoicing.Invoice {
	corrected := invoicing.Invoice{
		Kind:         invoicing.DocumentKindSale,
		Country:      factura.Country,
		Status:       invoicing.InvoiceStatusOwed,
		TicketSaleID: factura.TicketSaleID,
		IVARate:      factura.IVARate,
		Recipient: invoicing.Recipient{
			TaxIDType: in.Recipient.TaxIDType,
			TaxID:     in.Recipient.TaxID,
			LegalName: in.Recipient.LegalName,
			Address:   in.Recipient.Address,
			Email:     saleEmail,
		},
		Currency:      factura.Currency,
		SubtotalCents: factura.SubtotalCents,
		DiscountCents: factura.DiscountCents,
		IVACents:      factura.IVACents,
		TotalCents:    factura.TotalCents,
		PaymentMethod: factura.PaymentMethod,
		// Due at once. Whether it may be WORKED at once is the claim's
		// question, answered by the Credit Note's state (ClaimDueInvoice).
		NextAttemptAt:       &now,
		SupersedesInvoiceID: factura.ID,
		ReissuedBy:          in.ReissuedBy,
		ReissuedAt:          &now,
		ReissueNote:         in.Note,
	}
	corrected.Lines = append(corrected.Lines, factura.Lines...)
	return corrected
}
