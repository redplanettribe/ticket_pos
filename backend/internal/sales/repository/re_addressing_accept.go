package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

// The click on a Re-addressing Link (#421, ADR 0058): the one transaction in
// which an Online Sale changes hands.

// ReAddressingLinkRecord is one Sale Re-addressing record read beside the Sale
// and Event it is about — everything the link's open and accept need in order
// to judge the record's state and to name the purchase to its reader.
type ReAddressingLinkRecord struct {
	SaleReAddressingRow
	TicketSaleID    string
	OrganizationID  string
	EventID         string
	ConfirmationRef string
	SaleStatus      string
	// SaleCustomerID and SaleCustomerEmail are the Sale's addressee AS IT
	// STANDS: the ghost before acceptance, the corrected Customer after.
	SaleCustomerID    string
	SaleCustomerEmail string
	EventName         string
	EventStartsAt     sql.NullTime
}

// GetReAddressingLinkRecord reads the record a verified token names, with its
// Sale and Event, or nil when no such record exists.
func (r *Repository) GetReAddressingLinkRecord(ctx context.Context, recordID string) (*ReAddressingLinkRecord, error) {
	return scanReAddressingLinkRecord(r.db.Pool.QueryRowContext(ctx, reAddressingLinkRecordSQL, recordID))
}

const reAddressingLinkRecordSQL = `
	SELECT r.id, r.ticket_sale_id, r.operator_email, r.previous_email, r.corrected_email, r.note,
	       r.requested_at, r.accepted_at, r.withdrawn_at,
	       ts.id, ts.organization_id, ts.event_id, ts.confirmation_ref, ts.status,
	       ts.customer_id, ts.customer_email,
	       e.name, e.starts_at
	FROM sale_re_addressings r
	JOIN ticket_sales ts ON ts.id = r.ticket_sale_id
	JOIN events e ON e.id = ts.event_id
	WHERE r.id = $1
`

func scanReAddressingLinkRecord(row rowScanner) (*ReAddressingLinkRecord, error) {
	var out ReAddressingLinkRecord
	err := row.Scan(
		&out.ID, &out.SaleReAddressingRow.TicketSaleID, &out.OperatorEmail, &out.PreviousEmail, &out.CorrectedEmail, &out.Note,
		&out.RequestedAt, &out.AcceptedAt, &out.WithdrawnAt,
		&out.TicketSaleID, &out.OrganizationID, &out.EventID, &out.ConfirmationRef, &out.SaleStatus,
		&out.SaleCustomerID, &out.SaleCustomerEmail,
		&out.EventName, &out.EventStartsAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// AcceptedCustomerMinter is the customers module's half of the click, called
// INSIDE the accept transaction: given the proven address and the ghost the
// Sale belonged to, mint or match the Verified Customer the Sale moves to and
// report its id. The seam is a function value rather than an interface so
// this package stays ignorant of which module satisfies it — exactly as
// CommitSales is handed its Customer upsert.
type AcceptedCustomerMinter func(ctx context.Context, tx *sql.Tx, correctedEmail, ghostCustomerID string, now time.Time) (customerID string, err error)

// AcceptSaleReAddressingInput is one click as the service has already verified
// it: the record the signature named, the instant, and the Customer writer.
type AcceptSaleReAddressingInput struct {
	RecordID     string
	Now          time.Time
	MintCustomer AcceptedCustomerMinter
}

// AcceptSaleReAddressingResult is what came of the click. State is the
// record's derived state AS RE-JUDGED UNDER THE SALE'S LOCK; Written is true
// only when this call moved the Sale, and false both when the record was
// already accepted (idempotent: the earlier acceptance stands) and when it was
// no longer pending (nothing was written).
type AcceptSaleReAddressingResult struct {
	State   sales.ReAddressingState
	Written bool
	Record  *ReAddressingLinkRecord
	// CustomerID is the Customer the Sale now belongs to: the one minted or
	// matched here when Written, the Sale's own when already accepted.
	CustomerID string
}

// AcceptSaleReAddressing is THE ONE TRANSACTION in which a Sale changes hands
// (#421, ADR 0058): the Verified Customer is minted or matched, the Sale's
// Customer and snapshot email move, the Self-held Ticket follows if its Holder
// is still the wrong address, and the record is stamped accepted. All or
// nothing — a crash between any two of these would leave a Sale whose buyer
// holds nothing, or a Customer with no Sale.
//
// THE LOCK IS THE SALE'S ROW, taken FOR UPDATE, which serialises this against
// every reversal path (all of which lock the same row), against #420's
// recording and #423's withdraw, and against a second click on the same link.
// Under it the record and the Event's start are re-read and the state
// re-derived, so a Sale reversed or an Event moved between the service's read
// and this lock is seen: a click that raced a reversal writes nothing.
//
// IDEMPOTENT BY READING, NOT BY REWRITING. A record already accepted is
// reported as such and nothing is touched — not accepted_at, not the Sale,
// not the Ticket — so a second click keeps the first click's instant.
//
// THE SELF-HELD TICKET MOVES ONLY IF ITS HOLDER IS STILL THE WRONG ADDRESS,
// judged on the Ticket's own row: held by the ghost Customer, at the address
// the Sale carried. A Ticket the ghost's holder was reassigned away from — or
// any other Ticket of the Sale, whose Holder is somebody else — keeps its
// Holder and its Answers.
//
// EVERYTHING ELSE ON THE SALE IS UNTOUCHED: the snapshot name and Tax ID, the
// reference, sold_at, the lines, the Payment and its snapshot, the Platform
// Fee, the Reversal Window. Only customer_id and customer_email are written,
// because only the addressee was ever unproven.
func (r *Repository) AcceptSaleReAddressing(ctx context.Context, in AcceptSaleReAddressingInput) (*AcceptSaleReAddressingResult, error) {
	tx, err := r.db.Pool.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// Lock the Sale first, then read the record beside it. FOR UPDATE OF ts
	// locks the Sale's row alone — the record and Event rows are read.
	var saleID string
	if err := tx.QueryRowContext(ctx, `
		SELECT ts.id
		FROM sale_re_addressings r
		JOIN ticket_sales ts ON ts.id = r.ticket_sale_id
		WHERE r.id = $1
		FOR UPDATE OF ts
	`, in.RecordID).Scan(&saleID); err != nil {
		if err == sql.ErrNoRows {
			return &AcceptSaleReAddressingResult{State: sales.ReAddressingWithdrawn}, nil
		}
		return nil, err
	}
	record, err := scanReAddressingLinkRecord(tx.QueryRowContext(ctx, reAddressingLinkRecordSQL, in.RecordID))
	if err != nil {
		return nil, err
	}
	if record == nil {
		return &AcceptSaleReAddressingResult{State: sales.ReAddressingWithdrawn}, nil
	}

	state := sales.DeriveReAddressingState(
		nullTimeOrNil(record.AcceptedAt), nullTimeOrNil(record.WithdrawnAt),
		record.SaleStatus, nullTimeOrNil(record.EventStartsAt), in.Now,
	)
	if state == sales.ReAddressingAccepted {
		return &AcceptSaleReAddressingResult{State: state, Record: record, CustomerID: record.SaleCustomerID}, nil
	}
	if state != sales.ReAddressingPending || !record.CorrectedEmail.Valid {
		// Not pending — or pending in name only, its address purged (#424):
		// nothing to accept and nothing written.
		if !record.CorrectedEmail.Valid {
			state = sales.ReAddressingExpired
		}
		return &AcceptSaleReAddressingResult{State: state, Record: record}, nil
	}

	// THE CLICK IS PROOF OF EMAIL OWNERSHIP: this is where the corrected
	// address becomes a person, by the customers module's own hand.
	corrected := platform.NormalizeEmail(record.CorrectedEmail.String)
	customerID, err := in.MintCustomer(ctx, tx, corrected, record.SaleCustomerID, in.Now)
	if err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE ticket_sales
		SET customer_id = $2, customer_email = $3
		WHERE id = $1
	`, saleID, customerID, corrected); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tickets tk
		SET holder_email = $3, holder_customer_id = $2
		FROM ticket_sale_lines l
		WHERE tk.ticket_sale_line_id = l.id
		  AND l.ticket_sale_id = $1
		  AND tk.holder_email = $4
		  AND tk.holder_customer_id = $5
	`, saleID, customerID, corrected, record.SaleCustomerEmail, record.SaleCustomerID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE sale_re_addressings SET accepted_at = $2 WHERE id = $1
	`, in.RecordID, in.Now); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	record.AcceptedAt = sql.NullTime{Time: in.Now, Valid: true}
	record.SaleCustomerID = customerID
	record.SaleCustomerEmail = corrected
	return &AcceptSaleReAddressingResult{State: sales.ReAddressingAccepted, Written: true, Record: record, CustomerID: customerID}, nil
}

// GetRecordedSale reads one Ticket Sale in the shape the Sale Confirmation is
// written from — the shape every channel's commit hands the mail — so the
// fresh Confirmation an acceptance sends is the receipt the Sale always had,
// addressed to where it now goes.
func (r *Repository) GetRecordedSale(ctx context.Context, ticketSaleID string) (*RecordedSale, error) {
	var out RecordedSale
	var taxType, taxNumber, locale sql.NullString
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT ts.id, ts.customer_id, ts.confirmation_ref, ts.customer_email,
		       ts.customer_first_name, ts.customer_last_name,
		       ts.customer_tax_id_type, ts.customer_tax_id_number, ts.locale,
		       COALESCE((SELECT SUM(l.quantity * l.unit_price_cents) FROM ticket_sale_lines l WHERE l.ticket_sale_id = ts.id), 0)
		FROM ticket_sales ts
		WHERE ts.id = $1
	`, ticketSaleID).Scan(
		&out.ID, &out.CustomerID, &out.ConfirmationRef, &out.CustomerEmail,
		&out.CustomerFirstName, &out.CustomerLastName,
		&taxType, &taxNumber, &locale, &out.AmountCents,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if taxType.Valid && taxNumber.Valid {
		out.CustomerTaxID = platform.SaleTaxID{Type: taxType.String, Number: taxNumber.String}
	}
	out.Locale = locale.String
	return &out, nil
}

func nullTimeOrNil(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	return &v.Time
}
