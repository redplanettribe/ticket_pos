package service

import (
	"context"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
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
