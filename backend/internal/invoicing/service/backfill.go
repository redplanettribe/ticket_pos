package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

// The Uninvoiced House Sales (#507, parent #506, ADR 0064): the backlog a
// Sale Invoice Backfill works from — every paid Online Sale that stands,
// owes no Sale Invoice of any status, and belongs to an Organization that
// is House now — listed oldest first with a total, and counted. Read-only;
// the act that owes the documents is #508.
//
// Behind SALE_INVOICING_ENABLED with the fact itself, exactly as the
// Recipient Warning count is: closed, both answer SALE_INVOICING_UNAVAILABLE
// and the page is not offered. The predicate itself is the repository's,
// written once, so what the list shows and what the act accepts can never
// drift apart.

// UninvoicedHouseSaleList is one page of the backlog, oldest sale first,
// with the total: the ADR-0006 nested envelope.
type UninvoicedHouseSaleList struct {
	Data              []UninvoicedHouseSale `json:"data"`
	InvoicePagination InvoicePagination     `json:"pagination"`
}

// UninvoicedHouseSale is one candidate sale as the page shows it.
type UninvoicedHouseSale struct {
	TicketSaleID    string `json:"ticket_sale_id"`
	ConfirmationRef string `json:"confirmation_ref"`
	// SoldAt is the sale's own instant — the date the page shows beside
	// every row, because a factura issued from here will be dated the day
	// of the act and never this one.
	SoldAt           time.Time `json:"sold_at"`
	OrganizationID   string    `json:"organization_id"`
	OrganizationName string    `json:"organization_name"`
	EventID          string    `json:"event_id"`
	EventName        string    `json:"event_name"`
	// BuyerName is the buyer as the Sale transacted them, "First Last".
	BuyerName string `json:"buyer_name"`
	// BuyerTaxIDType and BuyerTaxIDNumber are the Tax ID the checkout was
	// transacted under (cedula, ruc or passport), null when it carried none.
	BuyerTaxIDType   *string `json:"buyer_tax_id_type"`
	BuyerTaxIDNumber *string `json:"buyer_tax_id_number"`
	// TotalCents is what the buyer paid, fees included.
	TotalCents int64  `json:"total_cents"`
	Currency   string `json:"currency"`
}

// UninvoicedHouseSaleCount is the backlog's size as one number: the count
// beside the invoicing list's other counts. It counts exactly what the
// list shows.
type UninvoicedHouseSaleCount struct {
	UninvoicedHouseSaleCount int `json:"uninvoiced_house_sale_count"`
}

// ListUninvoicedHouseSales returns one page of the backlog, oldest sale
// first.
func (s *Service) ListUninvoicedHouseSales(ctx context.Context, page, pageSize int) (*UninvoicedHouseSaleList, error) {
	if !s.saleInvoicingEnabled {
		return nil, invoicing.ErrSaleInvoicingUnavailable()
	}
	rows, total, err := s.repo.ListUninvoicedHouseSales(ctx, page, pageSize)
	if err != nil {
		return nil, err
	}
	items := make([]UninvoicedHouseSale, 0, len(rows))
	for i := range rows {
		items = append(items, uninvoicedHouseSaleView(&rows[i]))
	}
	return &UninvoicedHouseSaleList{Data: items, InvoicePagination: pagination(page, pageSize, total)}, nil
}

// CountUninvoicedHouseSales returns how many sales the backlog holds.
func (s *Service) CountUninvoicedHouseSales(ctx context.Context) (*UninvoicedHouseSaleCount, error) {
	if !s.saleInvoicingEnabled {
		return nil, invoicing.ErrSaleInvoicingUnavailable()
	}
	n, err := s.repo.CountUninvoicedHouseSales(ctx)
	if err != nil {
		return nil, err
	}
	return &UninvoicedHouseSaleCount{UninvoicedHouseSaleCount: n}, nil
}

func uninvoicedHouseSaleView(row *repository.UninvoicedHouseSaleRow) UninvoicedHouseSale {
	v := UninvoicedHouseSale{
		TicketSaleID:     row.TicketSaleID,
		ConfirmationRef:  row.ConfirmationRef,
		SoldAt:           row.SoldAt.UTC(),
		OrganizationID:   row.OrganizationID,
		OrganizationName: row.OrganizationName,
		EventID:          row.EventID,
		EventName:        row.EventName,
		BuyerName:        strings.TrimSpace(row.BuyerFirstName + " " + row.BuyerLastName),
		TotalCents:       row.TotalCents,
		Currency:         row.Currency,
	}
	if row.BuyerTaxIDType.Valid {
		v.BuyerTaxIDType = &row.BuyerTaxIDType.String
	}
	if row.BuyerTaxIDNumber.Valid {
		v.BuyerTaxIDNumber = &row.BuyerTaxIDNumber.String
	}
	return v
}

// The Sale Invoice Backfill (#508, parent #506, ADR 0064): a Platform
// Operator's act of owing a Sale Invoice to one or many Uninvoiced House
// Sales, dated the day of the act and never the day of the sale. Each sale
// is worked IN ITS OWN TRANSACTION — the ticket_sales row locked, the
// candidate predicate asked again of the locked row, the sale described by
// the sales module exactly as the checkout describes one, the document owed
// through the checkout's own builder and stamped with who and when — so a
// refused sale blocks nothing, a repeated request finds the sale already
// invoiced, and two requests naming the same sale serialize on the lock.
// Then ONE kick of the Sale Invoice Drainer for everything now due, and
// nothing downstream knows the document was owed late.

// PaidOnlineSaleReader is what the invoicing module asks of the sales module
// to describe a Ticket Sale that already exists (#508): the same snapshot
// the checkout hands over at commit — the buyer as transacted, the lines as
// paid, the Payment's amount — read in the backfill's transaction. Sales
// states what it sold; whether the sale QUALIFIES is this module's
// predicate, asked before the reader is. Optional, like the SaleInvoicer
// seam it mirrors: unwired, the backfill answers that every sale is
// unsupported rather than owing a document it cannot describe.
type PaidOnlineSaleReader interface {
	PaidOnlineSaleInTx(ctx context.Context, tx *sql.Tx, ticketSaleID string) (sales.PaidOnlineSale, error)
}

// WithPaidOnlineSaleReader ties the sales module's reader on.
func (s *Service) WithPaidOnlineSaleReader(reader PaidOnlineSaleReader) *Service {
	s.paidOnlineSales = reader
	return s
}

// The refusal codes a Sale Invoice Backfill answers per sale.
const (
	// BackfillRefusalNotACandidate: the sale is not an Uninvoiced House
	// Sale — unknown id, already invoiced, reversed, not House, not online,
	// free — as the predicate finds it under the lock.
	BackfillRefusalNotACandidate = "not_a_candidate"
	// BackfillRefusalUnsupportedSale: the sale is a candidate but the
	// checkout's builder refuses it — its lines do not match its Payment —
	// so nothing was written and no sequence number consumed.
	BackfillRefusalUnsupportedSale = "unsupported_sale"
)

// MaxBackfillSelection bounds one request: the page shows at most 100 and
// the act is meant to be a page or two, not the whole backlog at once.
const MaxBackfillSelection = 200

// SaleInvoiceBackfillResult is the act's answer, in request order.
type SaleInvoiceBackfillResult struct {
	Owed    []BackfilledSale      `json:"owed"`
	Refused []RefusedBackfillSale `json:"refused"`
}

// BackfilledSale names a sale now owed a document, and the document.
type BackfilledSale struct {
	TicketSaleID string `json:"ticket_sale_id"`
	InvoiceID    string `json:"invoice_id"`
}

// RefusedBackfillSale names a sale the act did not owe a document, and why.
type RefusedBackfillSale struct {
	TicketSaleID string `json:"ticket_sale_id"`
	Code         string `json:"code"`
}

// BackfillSaleInvoices performs a Sale Invoice Backfill over the given
// sales, in order, each in its own transaction, stamped with operatorEmail
// and the clock's now; then kicks the Drainer once if anything was owed.
// Behind SALE_INVOICING_ENABLED with the list it works from.
func (s *Service) BackfillSaleInvoices(ctx context.Context, ticketSaleIDs []string, operatorEmail string) (*SaleInvoiceBackfillResult, error) {
	if !s.saleInvoicingEnabled {
		return nil, invoicing.ErrSaleInvoicingUnavailable()
	}
	out := &SaleInvoiceBackfillResult{Owed: []BackfilledSale{}, Refused: []RefusedBackfillSale{}}
	for _, id := range ticketSaleIDs {
		invoiceID, refusal, err := s.backfillOne(ctx, id, operatorEmail)
		if err != nil {
			return nil, err
		}
		if refusal != "" {
			out.Refused = append(out.Refused, RefusedBackfillSale{TicketSaleID: id, Code: refusal})
			continue
		}
		out.Owed = append(out.Owed, BackfilledSale{TicketSaleID: id, InvoiceID: invoiceID})
	}
	s.logger.Info("invoicing: sale invoice backfill", "requested", len(ticketSaleIDs), "owed", len(out.Owed), "refused", len(out.Refused))
	if len(out.Owed) > 0 {
		// ONE kick for the whole selection: with no Sale named, the round
		// claims the documents due — the ones just owed, all due at once —
		// within one round's batch and budget, exactly as the scheduled drain
		// would; a selection larger than a round leaves the tail to the next
		// tick. Fire-and-forget: the answer goes back now, and the Drainer's
		// outcome is the list's to show.
		s.KickSaleInvoiceDrainer(ctx, "")
	}
	return out, nil
}

// backfillOne owes one sale its document in its own transaction. It
// returns the document's id, or a refusal code and no id; an error is a
// failure of the act itself, never a refusal.
func (s *Service) backfillOne(ctx context.Context, ticketSaleID, operatorEmail string) (invoiceID, refusal string, err error) {
	tx, err := s.repo.BeginBackfillTx(ctx)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = tx.Rollback() }()

	exists, err := s.repo.LockTicketSale(ctx, tx, ticketSaleID)
	if err != nil {
		return "", "", err
	}
	if !exists {
		return "", BackfillRefusalNotACandidate, nil
	}
	candidate, err := s.repo.GetUninvoicedHouseSale(ctx, tx, ticketSaleID)
	if err != nil {
		return "", "", err
	}
	if candidate == nil {
		return "", BackfillRefusalNotACandidate, nil
	}
	if s.paidOnlineSales == nil {
		s.logger.Warn("invoicing: no paid online sale reader is wired; the backfill can describe no sale")
		return "", BackfillRefusalUnsupportedSale, nil
	}
	sale, err := s.paidOnlineSales.PaidOnlineSaleInTx(ctx, tx, ticketSaleID)
	if err != nil {
		return "", "", err
	}
	now := s.clock()
	invoiceID, err = s.owePaidOnlineSale(ctx, tx, sale, func(inv *invoicing.Invoice) {
		inv.BackfilledBy = operatorEmail
		inv.BackfilledAt = &now
	})
	if errors.Is(err, errUnsupportedSale) {
		// Rolled back by the defer: no row, no sequence number, nothing.
		s.logger.Warn("invoicing: backfill refused a sale the builder cannot invoice", "ticket_sale_id", ticketSaleID, "error", err)
		return "", BackfillRefusalUnsupportedSale, nil
	}
	if err != nil {
		return "", "", err
	}
	if err := tx.Commit(); err != nil {
		return "", "", fmt.Errorf("commit backfill: %w", err)
	}
	return invoiceID, "", nil
}
