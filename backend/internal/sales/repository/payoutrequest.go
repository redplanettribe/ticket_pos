package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/sales"
)

// PayoutRequestRow is one Payout Request as stored: the ask, who made it, the
// frozen Payout Profile it was made against, and whatever answer it has since
// received (ADR 0026).
//
// The profile is a sales.PayoutProfile rather than six loose strings so the
// snapshot and the live profile are the same shape everywhere they are handled —
// which is what lets one renderer serve both and one validator judge both.
type PayoutRequestRow struct {
	ID string
	// OrganizationID is whose ask it is. The Organization-scoped reads all know
	// it already; the operator's queue and its detail view do not, because they
	// arrive from a cross-Organization list and the Organization is one of the
	// answers (#176).
	OrganizationID string
	AmountCents    int
	Note           *string
	Status         string
	// RequestedBy is an email, exactly as PayoutRow.RecordedBy is: the record
	// outlives the Member who made it (ADR 0026).
	RequestedBy string
	RequestedAt time.Time
	// PayableBalanceCents is the Payable Balance at the instant of asking,
	// signed and unclamped like the live figure it was copied from.
	PayableBalanceCents int
	Profile             sales.PayoutProfile
	DeclineReason       *string
	ResolvedBy          *string
	ResolvedAt          *time.Time
	PayoutID            *string
}

// payoutRequestColumns is the one column list every read of this table uses, in
// the one order payoutRequestScan expects. Stated once because a request read
// back differently by two paths is a bug that only shows up in whichever surface
// nobody looked at.
const payoutRequestColumns = `
	id, organization_id, amount_cents, note, status, requested_by, created_at, payable_balance_cents,
	bank_name, account_type, account_number, account_holder_name, tax_id_type, tax_id_number,
	decline_reason, resolved_by, resolved_at, payout_id
`

// CreatePayoutRequestInput is one ask, already validated: the amount is within
// the Payable Balance and the profile is complete and normalised. The repository
// decides nothing about either — it writes the row and reports what the unique
// index made of it.
type CreatePayoutRequestInput struct {
	OrganizationID      string
	AmountCents         int
	Note                *string
	RequestedBy         string
	PayableBalanceCents int
	Profile             sales.PayoutProfile
}

// CreatePayoutRequest records the ask, or — when the Organization already has an
// outstanding one — returns that instead, reporting created=false.
//
// The one-outstanding rule is the DATABASE's, not this function's. The INSERT
// aims straight at the partial unique index from migration 043 and lets it
// decide: ON CONFLICT names the index by its columns AND its predicate, which is
// how Postgres infers a partial index, so a second ask writes nothing and comes
// back empty. That is the whole of the race protection — a SELECT-then-INSERT
// could have two callers both find nothing and both try to write, and only one
// of them would learn about it, as a unique violation rather than as an answer.
//
// The read that follows is deliberately unconditional on error handling: reaching
// it means the index refused the write, so a pending row exists. It is fetched
// rather than reconstructed because the courtesy ADR 0024 established is to show
// the asker THEIR EARLIER ASK — its amount, its note, its snapshot — and not a
// restatement of what they just typed.
func (r *Repository) CreatePayoutRequest(ctx context.Context, input CreatePayoutRequestInput) (*PayoutRequestRow, bool, error) {
	row, err := payoutRequestScan(r.db.Pool.QueryRowContext(ctx, `
		INSERT INTO payout_requests
			(organization_id, amount_cents, note, status, requested_by, payable_balance_cents,
			 bank_name, account_type, account_number, account_holder_name, tax_id_type, tax_id_number)
		VALUES ($1, $2, $3, 'pending', $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (organization_id) WHERE status = 'pending' DO NOTHING
		RETURNING `+payoutRequestColumns,
		input.OrganizationID,
		input.AmountCents,
		input.Note,
		input.RequestedBy,
		input.PayableBalanceCents,
		input.Profile.BankName,
		input.Profile.AccountType,
		input.Profile.AccountNumber,
		input.Profile.AccountHolderName,
		input.Profile.TaxIDType,
		input.Profile.TaxIDNumber,
	))
	if err == nil {
		return row, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}

	existing, err := r.GetPendingPayoutRequest(ctx, input.OrganizationID)
	if err != nil {
		return nil, false, err
	}
	if existing == nil {
		// The pending row that refused the INSERT was resolved between the two
		// statements — a cancellation landing in the gap. Nothing was written and
		// nothing outstanding exists, so the honest answer is that this call did
		// not record the ask; the caller retries or the organizer presses again.
		return nil, false, nil
	}
	return existing, false, nil
}

// GetPendingPayoutRequest returns the Organization's outstanding Payout Request,
// or nil when it has none. Nil is an ordinary answer: having nothing outstanding
// is the state every Organization spends most of its life in.
func (r *Repository) GetPendingPayoutRequest(ctx context.Context, orgID string) (*PayoutRequestRow, error) {
	row, err := payoutRequestScan(r.db.Pool.QueryRowContext(ctx, `
		SELECT `+payoutRequestColumns+`
		FROM payout_requests
		WHERE organization_id = $1 AND status = 'pending'
	`, orgID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return row, err
}

// ListPayoutRequests returns the Organization's Payout Requests, newest first —
// the history the organizer reads beside their payout history.
//
// Newest first is the house convention for a history. The operator's queue
// inverts it (#177), and that inversion is a property of a WORK QUEUE, not of
// this table: the oldest unanswered request is the one about to become a
// complaint, while the newest ask is the one an organizer is looking for.
func (r *Repository) ListPayoutRequests(ctx context.Context, orgID string) ([]PayoutRequestRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+payoutRequestColumns+`
		FROM payout_requests
		WHERE organization_id = $1
		ORDER BY created_at DESC, id DESC
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]PayoutRequestRow, 0)
	for rows.Next() {
		row, err := payoutRequestScan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *row)
	}
	return out, rows.Err()
}

// CancelPayoutRequest withdraws the Organization's outstanding ask.
//
// It is a compare-and-swap, in the same shape fulfilment uses (#177, ADR 0026):
// the UPDATE carries `status = 'pending'` in its WHERE, so it is the row's own
// state that decides, not a read taken a moment earlier. Zero rows means
// somebody got there first — an operator paying or declining it in the gap — and
// the caller is told what the request actually became rather than being handed a
// generic conflict. Nothing is ever locked, so no cancellation can go stale.
//
// The organization_id in the WHERE is what keeps one Organization from
// cancelling another's ask; a request that is not this Organization's reads back
// as absent, which is the answer the caller turns into a not-found.
func (r *Repository) CancelPayoutRequest(ctx context.Context, orgID, requestID, resolvedBy string, now time.Time) (*PayoutRequestRow, error) {
	row, err := payoutRequestScan(r.db.Pool.QueryRowContext(ctx, `
		UPDATE payout_requests
		SET status = 'cancelled', resolved_by = $3, resolved_at = $4, updated_at = $4
		WHERE id = $2 AND organization_id = $1 AND status = 'pending'
		RETURNING `+payoutRequestColumns,
		orgID, requestID, resolvedBy, now,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return row, err
}

// GetPayoutRequest returns one of the Organization's Payout Requests by id, or
// nil when there is no such request under this Organization. It exists to tell
// "never yours" from "already resolved" after a compare-and-swap writes nothing,
// which are two different sentences for the person who pressed the button.
func (r *Repository) GetPayoutRequest(ctx context.Context, orgID, requestID string) (*PayoutRequestRow, error) {
	row, err := payoutRequestScan(r.db.Pool.QueryRowContext(ctx, `
		SELECT `+payoutRequestColumns+`
		FROM payout_requests
		WHERE id = $2 AND organization_id = $1
	`, orgID, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return row, err
}

// ListPendingPayoutRequests returns one page of every OUTSTANDING Payout Request
// on the platform, OLDEST FIRST, plus the unpaginated total (ADR 0006, #176).
//
// Oldest first is a deliberate break with the newest-first convention every
// other list in this file follows, and the break is a property of what this list
// IS. A history answers "what happened to my ask", so the newest row is the one
// its reader came for; this is a WORK QUEUE, and the oldest unanswered request
// is the one about to become a complaint (ADR 0026). The id tiebreaker is the
// house rule unchanged: two requests made in the same instant must not swap
// places between pages.
//
// It crosses every Organization on purpose and is scoped by nothing. There is no
// Organization to scope it by — the operator is a Member of none, and whose ask
// this is is one of the answers — and the authorization is the operator
// allowlist on the namespace it is reached through (ADR 0015).
//
// The WHERE is exactly the predicate of payout_requests_pending_queue_idx from
// migration 043, which was created for this query: the index stays the size of
// the backlog rather than the size of the history, so the queue does not slow
// down as answered requests accumulate behind it.
func (r *Repository) ListPendingPayoutRequests(ctx context.Context, limit, offset int) ([]PayoutRequestRow, int, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+payoutRequestColumns+`, COUNT(*) OVER() AS total
		FROM payout_requests
		WHERE status = 'pending'
		ORDER BY created_at ASC, id ASC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var (
		out   = make([]PayoutRequestRow, 0)
		total int
	)
	for rows.Next() {
		row, err := payoutRequestScan(trailingScanner{inner: rows, trailing: []any{&total}})
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *row)
	}
	return out, total, rows.Err()
}

// CountPendingPayoutRequests is the backlog as one number: what the operator
// navigation wears so a queue is noticed by somebody who had not already decided
// to look (ADR 0026).
//
// It is its own read rather than a by-product of listing, because the badge is
// rendered on every page of the staff app and the list is rendered on one. Both
// count the same partial index, so they cannot disagree.
func (r *Repository) CountPendingPayoutRequests(ctx context.Context) (int, error) {
	var count int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM payout_requests WHERE status = 'pending'
	`).Scan(&count)
	return count, err
}

// GetPayoutRequestByID returns one Payout Request whatever Organization it
// belongs to, or nil when there is none — the operator's read, where scoping by
// Organization would be scoping by a Membership the operator does not have.
//
// The caller checks the id is a UUID first; this reads it as one.
func (r *Repository) GetPayoutRequestByID(ctx context.Context, requestID string) (*PayoutRequestRow, error) {
	row, err := payoutRequestScan(r.db.Pool.QueryRowContext(ctx, `
		SELECT `+payoutRequestColumns+`
		FROM payout_requests
		WHERE id = $1
	`, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return row, err
}

// trailingScanner lets payoutRequestScan read a row that carries extra columns
// after the shared column list — COUNT(*) OVER() on the paginated queue.
//
// It exists so the queue reads a request through the SAME scan every other path
// does. The alternative is a second scan listing all seventeen columns beside
// one more, which is exactly the drift payoutRequestColumns was written to
// prevent: a column added there would be read by every path but one.
type trailingScanner struct {
	inner    rowScanner
	trailing []any
}

func (s trailingScanner) Scan(dest ...any) error {
	return s.inner.Scan(append(dest, s.trailing...)...)
}

// payoutRequestScan reads one row in payoutRequestColumns order, turning the
// nullable columns into pointers. It takes the rowScanner *sql.Row and *sql.Rows
// share (operator.go), so the single-row reads and the list read one row exactly
// alike and can never drift apart. A malformed request id is a legitimate way to
// arrive here with no row, which is why every caller maps sql.ErrNoRows itself
// rather than this function inventing a sentinel.
func payoutRequestScan(scanner rowScanner) (*PayoutRequestRow, error) {
	var row PayoutRequestRow
	var note, declineReason, resolvedBy, payoutID sql.NullString
	var resolvedAt sql.NullTime

	if err := scanner.Scan(
		&row.ID,
		&row.OrganizationID,
		&row.AmountCents,
		&note,
		&row.Status,
		&row.RequestedBy,
		&row.RequestedAt,
		&row.PayableBalanceCents,
		&row.Profile.BankName,
		&row.Profile.AccountType,
		&row.Profile.AccountNumber,
		&row.Profile.AccountHolderName,
		&row.Profile.TaxIDType,
		&row.Profile.TaxIDNumber,
		&declineReason,
		&resolvedBy,
		&resolvedAt,
		&payoutID,
	); err != nil {
		return nil, err
	}

	if note.Valid {
		row.Note = &note.String
	}
	if declineReason.Valid {
		row.DeclineReason = &declineReason.String
	}
	if resolvedBy.Valid {
		row.ResolvedBy = &resolvedBy.String
	}
	if resolvedAt.Valid {
		at := resolvedAt.Time
		row.ResolvedAt = &at
	}
	if payoutID.Valid {
		row.PayoutID = &payoutID.String
	}
	return &row, nil
}
