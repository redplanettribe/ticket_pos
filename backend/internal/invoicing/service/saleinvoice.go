package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/sri"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

// The Sale Invoice's birth (#473, parent #471, ADR 0060): the sales module
// has just recorded a paid online sale of a House Organization and, still
// inside that transaction, tells this module so. What is written here is a
// Tax Invoice of kind `sale` in state `owed` — the whole document minus
// everything that needs an Issuer: no number, no clave, no signature, no
// environment, no call to the authority. The Sale Invoice Drainer (a later
// ticket) turns owed into pending and onward; its next_attempt_at is set so
// the document is due at once.
//
// EVERYTHING IS COPIED AT OWING TIME. The Recipient is the Sale's buyer as
// transacted, the lines are the Ticket Sale Lines as paid, the rate is the
// platform's one rate, and none of it is re-read from the Customer or the
// catalog afterwards (CONTEXT.md, Recipient; ADR 0060). What the Drainer
// signs is what is on this row.
//
// NOTHING HERE CAN REFUSE FOR THE ISSUER'S SAKE. No Issuer, a test Issuer,
// an expired certificate: all of those are the Drainer's to find and the
// operator's to fix, never the buyer's to wait for (#471 story 5). The only
// failures are the row's own constraints, and those fail the sale — which
// is the invariant, not a bug.

// OwePaidOnlineSale implements the sales module's SaleInvoicer seam.
func (s *Service) OwePaidOnlineSale(ctx context.Context, tx *sql.Tx, sale sales.PaidOnlineSale) error {
	inv, err := saleInvoiceOf(sale, s.clock())
	if err != nil {
		return err
	}
	id, err := s.repo.OweInvoice(ctx, tx, inv)
	if err != nil {
		return err
	}
	// Counts and states only: no buyer, no address, no Sale (#471 story 35).
	s.logger.Info("invoicing: sale invoice owed", "invoice_id", id, "lines", len(inv.Lines), "total_cents", inv.TotalCents)
	return nil
}

// saleInvoiceOf builds the owed document from the sale as the commit
// described it. Pure, so the arithmetic is testable without a database.
func saleInvoiceOf(sale sales.PaidOnlineSale, now time.Time) (invoicing.Invoice, error) {
	if len(sale.Lines) == 0 {
		return invoicing.Invoice{}, fmt.Errorf("invoicing: sale %s has no lines to invoice", sale.TicketSaleID)
	}
	code, err := sri.IVACodeFor(invoicing.SaleInvoiceIVARate)
	if err != nil {
		return invoicing.Invoice{}, err
	}
	inv := invoicing.Invoice{
		Kind:         invoicing.DocumentKindSale,
		Country:      invoicing.CountryEcuador,
		Status:       invoicing.InvoiceStatusOwed,
		TicketSaleID: sale.TicketSaleID,
		IVARate:      invoicing.SaleInvoiceIVARate,
		Recipient: invoicing.Recipient{
			TaxIDType: sale.Buyer.TaxID.Type,
			TaxID:     sale.Buyer.TaxID.Number,
			LegalName: recipientLegalName(sale.Buyer.FirstName, sale.Buyer.LastName),
			Email:     sale.Buyer.Email,
		},
		Currency:      "USD",
		PaymentMethod: string(sri.PaymentMethodForProvider(sale.PaymentProvider)),
		// Due at once: the Drainer's first attempt is the immediate one
		// right after checkout (ADR 0060).
		NextAttemptAt: &now,
	}
	var total int64
	for i, l := range sale.Lines {
		paid := int64(l.Quantity) * int64(l.UnitPriceCents)
		base, iva, err := sri.BackOutIVA(paid, code)
		if err != nil {
			return invoicing.Invoice{}, err
		}
		inv.Lines = append(inv.Lines, invoicing.InvoiceLine{
			Position:           i + 1,
			Description:        lineDescription(l.TicketTypeName, sale.EventName),
			QuantityMillionths: int64(sri.QuantityFromInt(int64(l.Quantity))),
			UnitPriceCents:     int64(l.UnitPriceCents),
			DiscountCents:      0,
			IVARate:            invoicing.SaleInvoiceIVARate,
			BaseCents:          base,
			IVACents:           iva,
		})
		inv.SubtotalCents += base
		inv.IVACents += iva
		total += paid
	}
	inv.TotalCents = total
	if total != int64(sale.AmountCents) {
		// The lines and the Payment disagree about what was charged, which
		// no document may paper over.
		return invoicing.Invoice{}, fmt.Errorf("invoicing: sale %s lines sum to %d cents but the payment was %d", sale.TicketSaleID, total, sale.AmountCents)
	}
	return inv, nil
}

// recipientLegalName is "First Last" as the checkout collected the two
// halves, trimmed and joined by one space (CONTEXT.md, Recipient).
func recipientLegalName(first, last string) string {
	return strings.TrimSpace(strings.TrimSpace(first) + " " + strings.TrimSpace(last))
}

// lineDescription names the Ticket Type and the Event on the line — what
// the buyer bought, in the words they bought it under.
func lineDescription(ticketType, event string) string {
	return ticketType + " — " + event
}

// The Sale's paperwork on reversal (#476, parent #471, ADR 0060): the sales
// module has just marked a Ticket Sale reversed and, still inside that
// transaction, tells this module so. What happens is decided by the state
// of each Sale Invoice, read under a lock, and by nothing outside the
// database:
//
//   - NEVER SENT (owed, or parked needs_attention while unsignable — no
//     signature, no number): WITHDRAWN. The Tax Authority is told nothing
//     about a sale that no longer stands (#471 story 28).
//   - AUTHORIZED: a CREDIT NOTE is owed, due at once — the whole amount,
//     the same Recipient and lines, the reversal route as its reason.
//   - SIGNED AND UNDECIDED (pending, or parked needs_attention after a
//     refusal or 24 h of silence — a number consumed, the authority
//     possibly holding it): a Credit Note is owed and WAITS. The Drainer
//     claims it only once the factura is terminal (repository.
//     ClaimDueInvoice): authorized → issued; withdrawn or annulled → the
//     Credit Note is withdrawn with it (#471 story 29).
//   - ANNULLED, or already credited: nothing. The factura is already
//     undone at the authority, or a Credit Note already stands.
//
// A REVERSAL DURING A REISSUE (#484, ADR 0061) is neither refused nor
// delayed, and ONE Credit Note ends up crediting the old factura at the
// authority. The reissue's corrected factura, unsent, is withdrawn by the
// first rule. The reissue's Credit Note against the old factura decides
// the rest, by ITS state:
//
//   - UNSENT (owed, unsigned): withdrawn with the corrected factura — the
//     authority hears nothing of a correction to a sale that no longer
//     stands — and the reversal's own Credit Note credits the old factura
//     with the reversal's reason, worked at once.
//   - SIGNED AND UNDECIDED (pending, needs_attention): the reversal's
//     Credit Note is owed and WAITS for the reissue Credit Note's answer
//     (repository.ClaimDueInvoice, the sibling wait): authorized → the
//     reversal's is withdrawn, the factura being credited already
//     (drainer.go, withdrawRedundantCreditNote); annulled → the reversal's
//     is issued.
//   - AUTHORIZED: nothing owed; the old factura is credited already.
//
// A reversal AFTER a settled reissue credits the corrected factura alone:
// the superseded one is credited by the reissue's Credit Note and left as
// it is.
//
// Never refused for the Issuer's sake, never delayed by the authority: the
// only failures are the rows' own constraints, which fail the reversal's
// transaction exactly as they would fail any write in it.

// SettleReversedSale implements the sales module's SaleInvoicer seam for a
// Sale Reversal. It reports whether the Drainer now has work for the Sale.
func (s *Service) SettleReversedSale(ctx context.Context, tx *sql.Tx, reversal sales.SaleReversal) (bool, error) {
	docs, err := s.repo.LockSaleDocumentsForReversal(ctx, tx, reversal.TicketSaleID)
	if err != nil {
		return false, err
	}
	var facturas []*invoicing.Invoice
	notesOf := map[string][]*invoicing.Invoice{}
	for i := range docs {
		inv := &docs[i].Invoice
		switch inv.Kind {
		case invoicing.DocumentKindSale:
			facturas = append(facturas, inv)
		case invoicing.DocumentKindCreditNote:
			notesOf[inv.CreditsInvoiceID] = append(notesOf[inv.CreditsInvoiceID], inv)
		}
	}
	due := false
	for _, inv := range facturas {
		switch {
		case !inv.Signed():
			// owed, or needs_attention with nothing signed: never sent.
			if err := s.withdrawNeverSent(ctx, tx, inv, reversal); err != nil {
				return false, err
			}
		case inv.Status == invoicing.InvoiceStatusAnnulled:
			s.logger.Info("invoicing: reversed sale's invoice needs no credit note", "invoice_id", inv.ID, "status", inv.Status)
		default:
			// authorized, pending, or needs_attention with a number consumed.
			// What already stands against it decides whether one more is owed.
			credited, undecided := false, false
			for _, note := range notesOf[inv.ID] {
				switch {
				case !note.Signed() && note.Status != invoicing.InvoiceStatusWithdrawn:
					// A reissue's Credit Note nothing was sent for: withdrawn
					// with the corrected factura it was owed for.
					if err := s.withdrawNeverSent(ctx, tx, note, reversal); err != nil {
						return false, err
					}
				case note.Status == invoicing.InvoiceStatusAuthorized:
					credited = true
				case note.Status == invoicing.InvoiceStatusPending, note.Status == invoicing.InvoiceStatusNeedsAttention:
					undecided = true
				}
			}
			if credited {
				s.logger.Info("invoicing: reversed sale's invoice needs no credit note", "invoice_id", inv.ID, "status", inv.Status, "already_credited", true)
				continue
			}
			note := creditNoteOf(inv, reversal.Route, s.clock())
			id, err := s.repo.OweInvoice(ctx, tx, note)
			if err != nil {
				return false, err
			}
			waits := inv.Status != invoicing.InvoiceStatusAuthorized || undecided
			s.logger.Info("invoicing: credit note owed", "invoice_id", id, "credits_invoice_id", inv.ID, "route", reversal.Route, "waits", waits, "total_cents", note.TotalCents)
			due = true
		}
	}
	return due, nil
}

// withdrawNeverSent withdraws a document of the reversed Sale that was
// never sent, in the reversal's transaction, refusing rather than papering
// over a row that was signed between the lock and the write — impossible
// under FOR UPDATE.
func (s *Service) withdrawNeverSent(ctx context.Context, tx *sql.Tx, inv *invoicing.Invoice, reversal sales.SaleReversal) error {
	withdrawn, err := s.repo.WithdrawUnsignedInvoice(ctx, tx, inv.ID, reversal.At)
	if err != nil {
		return err
	}
	if !withdrawn {
		return fmt.Errorf("invoicing: %s %s could not be withdrawn", inv.Kind, inv.ID)
	}
	s.logger.Info("invoicing: document withdrawn; its sale was reversed before it was sent", "invoice_id", inv.ID, "kind", inv.Kind, "route", reversal.Route)
	return nil
}

// creditNoteOf is the Credit Note a Sale Invoice owes — on its Sale's
// reversal, or on its reissue (#483): the same document minus everything
// that needs an Issuer, of kind credit_note, naming what it credits and
// why (a reversal route, or CreditNoteReasonReissue). Copied from the Sale
// Invoice ROW, never from the Sale — the Recipient is fixed once issued
// (CONTEXT.md, Recipient), and what is credited is what was invoiced.
// Pure, so the copy is testable without a database.
func creditNoteOf(factura *invoicing.Invoice, reason string, now time.Time) invoicing.Invoice {
	note := invoicing.Invoice{
		Kind:             invoicing.DocumentKindCreditNote,
		Country:          factura.Country,
		Status:           invoicing.InvoiceStatusOwed,
		TicketSaleID:     factura.TicketSaleID,
		CreditsInvoiceID: factura.ID,
		CreditNoteReason: reason,
		IVARate:          factura.IVARate,
		Recipient:        factura.Recipient,
		Currency:         factura.Currency,
		SubtotalCents:    factura.SubtotalCents,
		DiscountCents:    factura.DiscountCents,
		IVACents:         factura.IVACents,
		TotalCents:       factura.TotalCents,
		PaymentMethod:    factura.PaymentMethod,
		// Due at once. Whether it may be WORKED at once is the claim's
		// question, answered by the factura's state (ClaimDueInvoice).
		NextAttemptAt: &now,
	}
	note.Lines = append(note.Lines, factura.Lines...)
	return note
}
