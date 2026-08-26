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
