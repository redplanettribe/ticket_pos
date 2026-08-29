package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// The Uninvoiced House Sales (#507, parent #506, ADR 0064): the paid Online
// Sales that stand, owe no Sale Invoice of any status, and belong to an
// Organization that is House NOW. The operator's backlog list and its
// count, and the same predicate asked of one sale inside the backfill's
// transaction (#508), so the act can never owe a document to a sale the
// list would not have shown.
//
// THE PREDICATE IS WRITTEN ONCE. uninvoicedHouseSaleFrom is the whole of
// what makes a sale a candidate; the list, the count and the single-sale
// read differ only in their tail. A sale is one when:
//   - it was transacted online (channel = online), so its money passed
//     through the platform — imported and Manually Recorded sales ride
//     the import channel and are not;
//   - a Payment approved for it carries an amount above zero — a free
//     sale's approved Payment is for nothing and owes nothing;
//   - it is active with no Reversal Request in flight or parked
//     needs_attention — a refused request leaves the sale standing;
//   - no Sale Invoice row names it, in ANY status — annulled and withdrawn
//     documents had one (#480's dead end, not a missing factura);
//   - its Organization is House now. The designation instant is never
//     compared with sold_at: designation is bookkeeping, not the tax fact.

// UninvoicedHouseSaleRow is one candidate sale as the list shows it: the
// sale, where it was made, who bought it and for how much.
type UninvoicedHouseSaleRow struct {
	TicketSaleID     string
	ConfirmationRef  string
	SoldAt           time.Time
	OrganizationID   string
	OrganizationName string
	EventID          string
	EventName        string
	BuyerFirstName   string
	BuyerLastName    string
	BuyerTaxIDType   sql.NullString
	BuyerTaxIDNumber sql.NullString
	// TotalCents is what the approved Payment was for: the price the buyer
	// paid, fees included, in the Organization's currency.
	TotalCents int64
	Currency   string
}

const uninvoicedHouseSaleColumns = `
	ts.id, ts.confirmation_ref, ts.sold_at,
	o.id, o.name, e.id, e.name,
	ts.customer_first_name, ts.customer_last_name, ts.customer_tax_id_type, ts.customer_tax_id_number,
	p.amount_cents, o.currency`

// uninvoicedHouseSaleFrom is the candidate predicate. The Payment is joined
// LATERAL and bounded to one row so a sale can never be listed twice, and
// the Organization's designation is read as it stands at query time.
const uninvoicedHouseSaleFrom = `
	FROM ticket_sales ts
	JOIN organizations o ON o.id = ts.organization_id
	JOIN events e ON e.id = ts.event_id
	JOIN LATERAL (
		SELECT p.amount_cents
		FROM payments p
		WHERE p.ticket_sale_id = ts.id AND p.status = 'approved' AND p.amount_cents > 0
		ORDER BY p.created_at ASC, p.id ASC
		LIMIT 1
	) p ON TRUE
	WHERE ts.channel = 'online'
	  AND ts.status = 'active'
	  AND o.house_designated_at IS NOT NULL
	  AND NOT EXISTS (
		SELECT 1 FROM sale_reversals r
		WHERE r.ticket_sale_id = ts.id AND r.status IN ('in_flight', 'needs_attention'))
	  AND NOT EXISTS (
		SELECT 1 FROM invoicing_invoices i
		WHERE i.ticket_sale_id = ts.id AND i.kind = 'sale')`

// rowQuerier is what the single-sale read needs of a connection: the
// backfill's transaction, which must see the sale row it has locked.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ListUninvoicedHouseSales reads a page of the candidates, OLDEST SALE
// FIRST by sold_at then id, and how many there are in all.
func (r *Repository) ListUninvoicedHouseSales(ctx context.Context, page, pageSize int) ([]UninvoicedHouseSaleRow, int, error) {
	total, err := r.CountUninvoicedHouseSales(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+uninvoicedHouseSaleColumns+uninvoicedHouseSaleFrom+`
		ORDER BY ts.sold_at ASC, ts.id ASC
		LIMIT $1 OFFSET $2
	`, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list uninvoiced house sales: %w", err)
	}
	defer rows.Close()
	out := []UninvoicedHouseSaleRow{}
	for rows.Next() {
		row, err := scanUninvoicedHouseSale(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("list uninvoiced house sales: %w", err)
		}
		out = append(out, *row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list uninvoiced house sales: %w", err)
	}
	return out, total, nil
}

// CountUninvoicedHouseSales counts the candidates: the count beside the
// invoicing list's other counts, and exactly what the list shows.
func (r *Repository) CountUninvoicedHouseSales(ctx context.Context) (int, error) {
	var n int
	if err := r.db.Pool.QueryRowContext(ctx, `SELECT COUNT(*)`+uninvoicedHouseSaleFrom).Scan(&n); err != nil {
		return 0, fmt.Errorf("count uninvoiced house sales: %w", err)
	}
	return n, nil
}

// GetUninvoicedHouseSale reads one sale IF it is a candidate, or nil when
// it is not one — whether because no sale has the id or because the
// predicate refuses it; the two are the same answer to a backfill. Takes
// the connection so the backfill (#508) can ask inside its own
// transaction, after locking the ticket_sales row, and see the sale as
// locked.
func (r *Repository) GetUninvoicedHouseSale(ctx context.Context, q rowQuerier, ticketSaleID string) (*UninvoicedHouseSaleRow, error) {
	row, err := scanUninvoicedHouseSale(q.QueryRowContext(ctx, `
		SELECT `+uninvoicedHouseSaleColumns+uninvoicedHouseSaleFrom+`
		  AND ts.id = $1
	`, ticketSaleID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get uninvoiced house sale: %w", err)
	}
	return row, nil
}

func scanUninvoicedHouseSale(s interface{ Scan(dest ...any) error }) (*UninvoicedHouseSaleRow, error) {
	var row UninvoicedHouseSaleRow
	if err := s.Scan(
		&row.TicketSaleID, &row.ConfirmationRef, &row.SoldAt,
		&row.OrganizationID, &row.OrganizationName, &row.EventID, &row.EventName,
		&row.BuyerFirstName, &row.BuyerLastName, &row.BuyerTaxIDType, &row.BuyerTaxIDNumber,
		&row.TotalCents, &row.Currency,
	); err != nil {
		return nil, err
	}
	return &row, nil
}

// BeginBackfillTx opens the transaction a Sale Invoice Backfill owes ONE
// document in (#508): one per sale, so a refused sale blocks nothing and a
// repeated request is harmless.
func (r *Repository) BeginBackfillTx(ctx context.Context) (*sql.Tx, error) {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin backfill transaction: %w", err)
	}
	return tx, nil
}

// LockTicketSale takes a row lock on the Ticket Sale for the rest of the
// backfill's transaction (#508), so two requests naming the same sale are
// serialized: the second waits, then asks the predicate of a sale that
// already has its document, and is refused. It reports whether the row
// exists at all — an unknown id is the same answer as a sale that is not
// a candidate.
func (r *Repository) LockTicketSale(ctx context.Context, tx *sql.Tx, ticketSaleID string) (bool, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM ticket_sales WHERE id = $1 FOR UPDATE`, ticketSaleID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock ticket sale: %w", err)
	}
	return true, nil
}
