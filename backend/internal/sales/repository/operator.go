package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// orgMoneySQL is every Organization's money in one row each: what the platform
// has withheld from it (Platform Fee and Fee IVA, accumulated from the amounts
// each active Online Sale line snapshotted) and its Withdrawable Balance (Net
// Proceeds minus recorded Payouts).
//
// Net Proceeds is lineNetProceedsSQL, the single definition the Event summary
// and the Organization's own payouts surface already read, so the operator's
// figures cannot drift from the ones the Organization is shown (ADR 0014). The
// channel and status filters are load-bearing in the same way they are there:
// only money the platform actually held and still holds counts.
//
// The balance is signed — a sale reversed after it was paid out leaves the
// Organization owing the platform — and callers decide what to do with that.
//
// Beside those, and touching none of them, the kept-fee term: the Platform Fee
// and Fee IVA of sales an Operator Reversal voided while stating the platform
// kept its commission (#127). It is the one exception to the active-only rule,
// it belongs to the platform's revenue alone, and it is carried in its own
// columns so that no Organization-facing figure can pick it up by accident.
const orgMoneySQL = `
	SELECT
		org.id AS organization_id,
		org.currency AS currency,
		COALESCE(lines.fee_cents, 0) AS fee_cents,
		COALESCE(lines.fee_iva_cents, 0) AS fee_iva_cents,
		COALESCE(kept.fee_cents, 0) AS kept_fee_cents,
		COALESCE(kept.fee_iva_cents, 0) AS kept_fee_iva_cents,
		COALESCE(lines.net_proceeds_cents, 0) - COALESCE(paid.paid_cents, 0) AS balance_cents
	FROM organizations org
	LEFT JOIN LATERAL (
		SELECT
			SUM(` + lineNetProceedsSQL + `) AS net_proceeds_cents,
			SUM(tsl.quantity * tsl.fee_cents) AS fee_cents,
			SUM(tsl.quantity * tsl.fee_iva_cents) AS fee_iva_cents
		FROM ticket_sale_lines tsl
		JOIN ticket_sales ts ON ts.id = tsl.ticket_sale_id
		WHERE ts.organization_id = org.id
		  AND ts.channel = 'online'
		  AND ts.status = 'active'
	) lines ON TRUE
	LEFT JOIN LATERAL (
		SELECT
			SUM(tsl.quantity * tsl.fee_cents) AS fee_cents,
			SUM(tsl.quantity * tsl.fee_iva_cents) AS fee_iva_cents
		FROM ticket_sale_lines tsl
		JOIN ticket_sales ts ON ts.id = tsl.ticket_sale_id
		WHERE ts.organization_id = org.id
		  AND ts.channel = 'online'
		  AND ts.status = 'reversed'
		  AND ts.platform_fee_kept
	) kept ON TRUE
	LEFT JOIN LATERAL (
		SELECT SUM(p.amount_cents) AS paid_cents
		FROM payouts p
		WHERE p.organization_id = org.id
	) paid ON TRUE
`

// CurrencyTotalsRow is the platform's own money in one currency: what it has
// earned in Platform Fees and Fee IVA, and what it currently owes.
//
// KeptFee* is the part of the two fee figures that stands on reversed sales,
// already counted inside them, reported separately only so the Operator
// Dashboard can say why the revenue figure survived a reversal.
type CurrencyTotalsRow struct {
	Currency         string
	PlatformFeeCents int
	FeeIVACents      int
	TotalOwedCents   int
	KeptFeeCents     int
	KeptFeeIVACents  int
}

// OperatorPayoutRow is one recorded Payout with its audit trail: who entered it
// and when the row was written, alongside the bare fact it records. RecordedBy
// is null for Payouts entered directly in the database before the Operator
// Dashboard existed (ADR 0015).
type OperatorPayoutRow struct {
	ID          string
	AmountCents int
	PaidAt      time.Time
	Note        *string
	RecordedBy  *string
	CreatedAt   time.Time
}

// BalancesByOrganizationIDs returns each given Organization's Withdrawable
// Balance, signed. Organizations with neither sales nor Payouts come back as 0
// rather than missing, so the caller never has to tell "no data" from "zero".
func (r *Repository) BalancesByOrganizationIDs(ctx context.Context, orgIDs []string) (map[string]int, error) {
	balances := make(map[string]int, len(orgIDs))
	if len(orgIDs) == 0 {
		return balances, nil
	}

	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT organization_id, balance_cents
		FROM (`+orgMoneySQL+`) money
		WHERE organization_id = ANY($1)
	`, orgIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var orgID string
		var balance int
		if err := rows.Scan(&orgID, &balance); err != nil {
			return nil, err
		}
		balances[orgID] = balance
	}
	return balances, rows.Err()
}

// PlatformCurrencyTotals aggregates the platform's money one row per currency,
// with no FX conversion anywhere: currencies are never added together, so the
// numbers stay honest the day a second one appears (ADR 0015).
//
// Total owed sums only POSITIVE balances. A negative balance is an Organization
// owing the platform after a post-settlement reversal, and netting it off would
// understate the cash the platform must keep on hand to settle everyone else.
//
// Revenue is the one figure on the platform that is not active-only: the fees
// of a reversed sale whose Operator Reversal said the platform kept its
// commission are money the platform still holds, so they stay in the total and
// are also reported on their own for the dashboard's disclosure (#127).
func (r *Repository) PlatformCurrencyTotals(ctx context.Context) ([]CurrencyTotalsRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT
			currency,
			SUM(fee_cents + kept_fee_cents),
			SUM(fee_iva_cents + kept_fee_iva_cents),
			SUM(GREATEST(balance_cents, 0)),
			SUM(kept_fee_cents),
			SUM(kept_fee_iva_cents)
		FROM (`+orgMoneySQL+`) money
		GROUP BY currency
		ORDER BY currency ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CurrencyTotalsRow
	for rows.Next() {
		var t CurrencyTotalsRow
		if err := rows.Scan(
			&t.Currency, &t.PlatformFeeCents, &t.FeeIVACents, &t.TotalOwedCents,
			&t.KeptFeeCents, &t.KeptFeeIVACents,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListPayoutsWithRecorder returns an Organization's recorded Payouts newest
// first, carrying the recorder for the operator's reconciliation view.
func (r *Repository) ListPayoutsWithRecorder(ctx context.Context, orgID string) ([]OperatorPayoutRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT id, amount_cents, paid_at, note, recorded_by, created_at
		FROM payouts
		WHERE organization_id = $1
		ORDER BY paid_at DESC, id DESC
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []OperatorPayoutRow
	for rows.Next() {
		p, err := scanOperatorPayout(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// InsertPayout records one Payout against an Organization and returns it.
//
// There is no balance check here or anywhere above it: by the time an operator
// types the amount the money has already left the bank, and refusing to record
// reality would corrupt the ledger. An over-balance Payout simply drives the
// Withdrawable Balance negative, which is what it means (ADR 0015).
func (r *Repository) InsertPayout(
	ctx context.Context,
	orgID string,
	amountCents int,
	paidAt time.Time,
	note *string,
	recordedBy string,
) (OperatorPayoutRow, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		INSERT INTO payouts (organization_id, amount_cents, paid_at, note, recorded_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, amount_cents, paid_at, note, recorded_by, created_at
	`, orgID, amountCents, paidAt, note, recordedBy)
	return scanOperatorPayout(row)
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanOperatorPayout(row rowScanner) (OperatorPayoutRow, error) {
	var p OperatorPayoutRow
	var note, recordedBy sql.NullString
	if err := row.Scan(&p.ID, &p.AmountCents, &p.PaidAt, &note, &recordedBy, &p.CreatedAt); err != nil {
		return OperatorPayoutRow{}, err
	}
	if note.Valid {
		p.Note = &note.String
	}
	if recordedBy.Valid {
		p.RecordedBy = &recordedBy.String
	}
	return p, nil
}

// OperatorSaleRow is one Ticket Sale as the Platform Operator's lookup by Sale
// Confirmation reference sees it (#124): the sale, its money broken into the
// three figures the platform recorded, the buyer as it snapshotted them, and
// the Event that places it in time.
//
// It is scoped by nothing. Every other read of a Ticket Sale in this module is
// narrowed by an Organization or by the owning Customer, because that scope IS
// the authorization; here the authorization is the operator allowlist on the
// namespace, and the point of the read is that it spans every Organization
// (ADR 0015).
//
// It is not the Sales list's row. That one exists to be listed and filtered by
// an Organization's own staff; this one exists to identify a sale a support
// thread named, so it carries the Event and the Organization the reference
// alone does not reveal, and the fee split the operator needs to see where the
// money went.
type OperatorSaleRow struct {
	ID              string
	OrganizationID  string
	ConfirmationRef string
	Status          string
	Channel         string
	Source          sql.NullString
	PaymentMethod   sql.NullString
	SoldAt          time.Time
	RecordedAt      time.Time
	ReversedAt      sql.NullTime
	ReversedBy      sql.NullString

	// The Operator Reversal's money memo (#125), all null unless ReversedBy is
	// 'operator': the acting operator, what they said the buyer got back, whether
	// the platform kept its fee, and their note. Operator-facing only — nothing
	// an Organization reads carries these.
	ReversedByOperator  sql.NullString
	ReversalNote        sql.NullString
	RefundedAmountCents sql.NullInt64
	PlatformFeeKept     sql.NullBool

	CustomerEmail     string
	CustomerFirstName string
	CustomerLastName  string

	// TicketTypes is the same roll-up the Sales list carries, built by the same
	// aggregate, so both surfaces name a sale's contents identically.
	TicketTypes []SaleLineRollup
	// TicketCount is how many tickets the sale is for — the quantity a reversal
	// would hand back to the Ticket Types.
	TicketCount int

	Currency         string
	AmountCents      int
	PlatformFeeCents int
	FeeIVACents      int
	NetProceedsCents int

	EventID       string
	EventName     string
	EventSlug     string
	EventStartsAt sql.NullTime
	EventTimezone sql.NullString
}

// GetSaleByConfirmationRef returns the one Ticket Sale carrying a Sale
// Confirmation reference, or nil when none does.
//
// The match is case-insensitive because the reference reaches an operator by
// being quoted — pasted out of an email, retyped in a support thread — and case
// is the first thing quoting loses. It cannot become ambiguous: references are
// generated from an uppercase alphabet and are unique, so folding case can find
// at most the one sale it would have found exactly.
func (r *Repository) GetSaleByConfirmationRef(ctx context.Context, confirmationRef string) (*OperatorSaleRow, error) {
	var out OperatorSaleRow
	var typesJSON []byte
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT
			ts.id,
			ts.organization_id,
			ts.confirmation_ref,
			ts.status,
			ts.channel,
			ts.source,
			ts.payment_method,
			ts.sold_at,
			ts.created_at,
			ts.reversed_at,
			ts.reversed_by,
			ts.reversed_by_operator,
			ts.reversal_note,
			ts.refunded_amount_cents,
			ts.platform_fee_kept,
			ts.customer_email,
			ts.customer_first_name,
			ts.customer_last_name,
			lines.ticket_types,
			lines.ticket_count,
			org.currency,
			lines.amount_cents,
			lines.fee_cents,
			lines.fee_iva_cents,
			lines.net_proceeds_cents,
			e.id,
			e.name,
			e.slug,
			e.starts_at,
			e.timezone
		FROM ticket_sales ts
		JOIN organizations org ON org.id = ts.organization_id
		JOIN events e ON e.id = ts.event_id
		JOIN LATERAL (
			SELECT
				COALESCE(SUM(tsl.quantity * tsl.unit_price_cents), 0) AS amount_cents,
				COALESCE(SUM(tsl.quantity * tsl.fee_cents), 0) AS fee_cents,
				COALESCE(SUM(tsl.quantity * tsl.fee_iva_cents), 0) AS fee_iva_cents,
				COALESCE(SUM(`+lineNetProceedsSQL+`), 0) AS net_proceeds_cents,
				COALESCE(SUM(tsl.quantity), 0) AS ticket_count,
				COALESCE(
					json_agg(
						json_build_object('ticket_type_name', tt.name, 'quantity', tsl.quantity)
						ORDER BY tt.sort_order, tt.name
					),
					'[]'::json
				) AS ticket_types
			FROM ticket_sale_lines tsl
			JOIN ticket_types tt ON tt.id = tsl.ticket_type_id
			WHERE tsl.ticket_sale_id = ts.id
		) lines ON TRUE
		WHERE UPPER(ts.confirmation_ref) = UPPER($1)
	`, confirmationRef).Scan(
		&out.ID,
		&out.OrganizationID,
		&out.ConfirmationRef,
		&out.Status,
		&out.Channel,
		&out.Source,
		&out.PaymentMethod,
		&out.SoldAt,
		&out.RecordedAt,
		&out.ReversedAt,
		&out.ReversedBy,
		&out.ReversedByOperator,
		&out.ReversalNote,
		&out.RefundedAmountCents,
		&out.PlatformFeeKept,
		&out.CustomerEmail,
		&out.CustomerFirstName,
		&out.CustomerLastName,
		&typesJSON,
		&out.TicketCount,
		&out.Currency,
		&out.AmountCents,
		&out.PlatformFeeCents,
		&out.FeeIVACents,
		&out.NetProceedsCents,
		&out.EventID,
		&out.EventName,
		&out.EventSlug,
		&out.EventStartsAt,
		&out.EventTimezone,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(typesJSON, &out.TicketTypes); err != nil {
		return nil, err
	}
	return &out, nil
}
