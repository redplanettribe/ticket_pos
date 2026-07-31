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
	ResolutionReason    *string
	ResolvedBy          *string
	ResolvedAt          *time.Time
	PayoutID            *string
	// The transfer an operator submitted and the bank has not confirmed (#184).
	// All three are null on a request that never went through `processing` —
	// including one that went `pending → paid` directly, which is an instant
	// transfer and legal.
	//
	// TransferSubmittedBy is a DIFFERENT actor from ResolvedBy and must not be
	// folded into it: the operator who submits and the operator who confirms may
	// be different people days apart, and ResolvedBy keeps meaning who ENDED the
	// request (migration 045).
	//
	// They are read on every path because payoutRequestColumns is shared, and
	// deliberately not yet on any wire shape: exposing them, with the 72-hour
	// stale flag computed from the injected clock, is #186's contract change.
	TransferSubmittedBy *string
	TransferSubmittedAt *time.Time
	TransferReference   *string
}

// payoutRequestColumns is the one column list every read of this table uses, in
// the one order payoutRequestScan expects. Stated once because a request read
// back differently by two paths is a bug that only shows up in whichever surface
// nobody looked at.
const payoutRequestColumns = `
	id, organization_id, amount_cents, note, status, requested_by, created_at, payable_balance_cents,
	bank_name, account_type, account_number, account_holder_name, tax_id_type, tax_id_number,
	resolution_reason, resolved_by, resolved_at, payout_id,
	transfer_submitted_by, transfer_submitted_at, transfer_reference
`

// outstandingPayoutRequest is THE definition of an OUTSTANDING Payout Request —
// one still awaiting an answer — as a SQL predicate over payout_requests.
//
// It is stated ONCE, here, and every read that means *outstanding* is written
// against it. `outstanding` and `pending` are NOT synonyms: `pending` means
// nobody has touched the request yet, and `outstanding` means the Organization's
// single slot is still occupied. They described the same rows until `processing`
// arrived — an operator has submitted the transfer and the bank has not
// confirmed it, which is emphatically not untouched, and just as emphatically
// still occupies the slot. Without `processing` in this list an Organization
// whose transfer is in flight could ask again for money already on its way to
// them, because no balance has moved to stop them (#184, ADR 0026 amendment).
//
// IT IS ALSO THE PREDICATE OF THE PARTIAL UNIQUE INDEX
// payout_requests_one_outstanding_per_organization_key, and of the queue index
// payout_requests_pending_queue_idx beside it (migrations 043, 045). The three
// must stay in step:
//
//   - CreatePayoutRequest's ON CONFLICT names that unique index by its columns
//     AND its predicate, which is how Postgres infers a partial index. Change
//     this constant without changing the index and the INSERT stops finding an
//     arbiter — loudly, at runtime, on the first ask.
//   - The queue read below matches the queue index's predicate so the index stays
//     the size of the backlog rather than the size of the history. Change one
//     without the other and the queue quietly stops using its index.
//
// The two guards that are NOT written against it stay that way, and both say so
// where they live: cancelling and declining mean UNTOUCHED BY AN OPERATOR, and
// fulfilment means ANSWERABLE WITH MONEY. Each widens — or refuses to — on its
// own terms, because a state added to this list must not silently become a state
// an ask can be withdrawn from or a Payout written against.
const outstandingPayoutRequest = `status IN ('pending', 'processing')`

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
// aims straight at the partial unique index (migrations 043, 045) and lets it
// decide: ON CONFLICT names the index by its columns AND its predicate, which is
// how Postgres infers a partial index, so a second ask writes nothing and comes
// back empty. That is the whole of the race protection — a SELECT-then-INSERT
// could have two callers both find nothing and both try to write, and only one
// of them would learn about it, as a unique violation rather than as an answer.
//
// The read that follows is deliberately unconditional on error handling: reaching
// it means the index refused the write, so an outstanding row exists. It is fetched
// rather than reconstructed because the courtesy ADR 0024 established is to show
// the asker THEIR EARLIER ASK — its amount, its note, its snapshot — and not a
// restatement of what they just typed.
//
// The literal 'pending' in the VALUES list is NOT the outstanding predicate and
// must not become it. It is the state a new ask is BORN in — nobody has looked
// at it yet — and it stays a single named state however many states later come
// to count as outstanding. The ON CONFLICT predicate beside it is the other
// concept, and is the shared one.
func (r *Repository) CreatePayoutRequest(ctx context.Context, input CreatePayoutRequestInput) (*PayoutRequestRow, bool, error) {
	row, err := payoutRequestScan(r.db.Pool.QueryRowContext(ctx, `
		INSERT INTO payout_requests
			(organization_id, amount_cents, note, status, requested_by, payable_balance_cents,
			 bank_name, account_type, account_number, account_holder_name, tax_id_type, tax_id_number)
		VALUES ($1, $2, $3, 'pending', $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (organization_id) WHERE `+outstandingPayoutRequest+` DO NOTHING
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

	existing, err := r.GetOutstandingPayoutRequest(ctx, input.OrganizationID)
	if err != nil {
		return nil, false, err
	}
	if existing == nil {
		// The outstanding row that refused the INSERT was resolved between the two
		// statements — a cancellation landing in the gap. Nothing was written and
		// nothing outstanding exists, so the honest answer is that this call did
		// not record the ask; the caller retries or the organizer presses again.
		return nil, false, nil
	}
	return existing, false, nil
}

// GetOutstandingPayoutRequest returns the Organization's outstanding Payout
// Request, or nil when it has none. Nil is an ordinary answer: having nothing
// outstanding is the state every Organization spends most of its life in.
//
// It reads the row occupying the Organization's single slot — the one the
// partial unique index refused a second ask on — so it is written against
// outstandingPayoutRequest and must never be narrowed back to `pending`. It was
// called GetPendingPayoutRequest while the two words meant the same thing (#183).
func (r *Repository) GetOutstandingPayoutRequest(ctx context.Context, orgID string) (*PayoutRequestRow, error) {
	row, err := payoutRequestScan(r.db.Pool.QueryRowContext(ctx, `
		SELECT `+payoutRequestColumns+`
		FROM payout_requests
		WHERE organization_id = $1 AND `+outstandingPayoutRequest+`
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
//
// THE GUARD SAYS `status = 'pending'` AND IS NOT THE OUTSTANDING PREDICATE. What
// it means is UNTOUCHED BY AN OPERATOR: an Organization may withdraw an ask
// nobody has acted on, and nothing else. It happens to match the same rows as
// outstandingPayoutRequest today, and it must NOT widen with it — once an
// operator has submitted the transfer the bank is already acting on the ask, and
// letting the asker take it back would withdraw money that is on its way to them
// (ADR 0026, amendment).
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

// MarkPayoutRequestProcessingInput is an operator saying they submitted the
// transfer and cannot yet confirm it: who submitted it, when, and whatever the
// bank handed back.
//
// Operator comes from the Staff Session and never from a request body, exactly
// as the fulfilment's does (ADR 0015). Now is the service's injected clock and
// never NOW() in SQL — the same rule the Payable Balance's day boundary follows,
// and the reason a test can move a request to 73 hours old (ADR 0026).
//
// Reference is a pointer because it is genuinely optional: the provider does not
// always give one synchronously, and a required reference an operator cannot
// fill becomes a column full of dashes. The caller has already trimmed it, and
// an empty one arrives here as nil rather than as "".
type MarkPayoutRequestProcessingInput struct {
	RequestID string
	Operator  string
	Reference *string
	Now       time.Time
}

// MarkPayoutRequestProcessing records that the transfer has been submitted and
// the bank has not confirmed it, or returns nil when the request was no longer
// pending.
//
// NOTHING IS WRITTEN TO `payouts` HERE, AND NOTHING MAY BE. A Payout has meant
// money that MOVED since ADR 0014, and this state exists precisely because the
// operator cannot yet say that it did. Writing the ledger row on submission
// would mean deleting or negating it when the bank sends the money back, and
// inventing a voided-Payout concept every balance in the system would then have
// to understand. That is why this is a bare UPDATE where fulfilment is a
// transaction: there is no second statement to keep it honest with.
//
// The compare-and-swap is the house shape (see FulfilPayoutRequest): the guard
// lives in the WHERE, so it is the ROW's own state that decides rather than a
// read taken a moment earlier, and nothing is ever held. Zero rows means
// somebody got there first — another operator submitting a transfer for the same
// ask, the Organization cancelling it, an operator declining it — and the caller
// reads the request back to say which.
//
// THE GUARD SAYS `status = 'pending'` AND IS NOT THE OUTSTANDING PREDICATE.
// What it means is UNTOUCHED BY AN OPERATOR, the same thing cancel and decline
// mean: a transfer is submitted against an ask nobody has answered. It must NOT
// widen to `processing`, or a second operator could overwrite the first's stamp
// and the record of who to ask about a transfer in flight would be lost.
//
// resolved_by and resolved_at stay NULL, and the schema requires it
// (payout_requests_resolution_matches_status, migration 045). A `processing`
// request has not been resolved, it has been ACTED ON; who resolves it is a
// later, possibly different, operator.
func (r *Repository) MarkPayoutRequestProcessing(ctx context.Context, input MarkPayoutRequestProcessingInput) (*PayoutRequestRow, error) {
	row, err := payoutRequestScan(r.db.Pool.QueryRowContext(ctx, `
		UPDATE payout_requests
		SET status = 'processing',
		    transfer_submitted_by = $2,
		    transfer_submitted_at = $3,
		    transfer_reference = $4,
		    updated_at = $3
		WHERE id = $1 AND status = 'pending'
		RETURNING `+payoutRequestColumns,
		input.RequestID, input.Operator, input.Now, input.Reference,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return row, err
}

// FulfilPayoutRequestInput is an operator answering a request with money: what
// actually left the bank, the day it did, an optional note, and the operator's
// own email — which stamps BOTH the Payout's recorded_by and the request's
// resolved_by, and which the handler takes from the Staff Session and never from
// a request body (ADR 0015, ADR 0019, ADR 0026).
//
// AmountCents is what MOVED, which need not be what was asked. An operator who
// transfers less records the smaller figure; the request keeps the larger one,
// and the divergence between them stays visible forever. Partial fulfilment
// needs no model of its own (ADR 0026).
type FulfilPayoutRequestInput struct {
	RequestID      string
	OrganizationID string
	AmountCents    int
	PaidAt         time.Time
	Note           *string
	Operator       string
	Now            time.Time
}

// FulfilPayoutRequest records the Payout and marks the request paid, in ONE
// transaction, and returns nils when the request was no longer pending.
//
// THE TWO STATEMENTS BELOW MUST NOT BE SEPARATED, AND THE GUARD ON THE SECOND
// MUST NOT BE DROPPED. This is the compare-and-swap ADR 0026 chose over a claim
// lock, and every part of its shape is load-bearing:
//
//   - The UPDATE carries an explicit status guard in its WHERE, so it is the
//     ROW's own state that decides, not a read taken a moment earlier. Two operators
//     who both open the request and both press the button cannot both win,
//     however close together they land, because the loser's UPDATE matches
//     nothing.
//
//   - ZERO ROWS AFFECTED ROLLS THE INSERT BACK. This is the part a future reader
//     will be tempted to "simplify" into two independent statements, or into an
//     insert followed by a best-effort update. It must not be: the INSERT always
//     succeeds — nothing in the ledger knows requests exist, and nothing stops a
//     second Payout being written — so a fulfilment that failed the CAS but kept
//     its INSERT would record the Organization as PAID TWICE while telling the
//     operator their fulfilment failed. The rollback is the only thing standing
//     between a lost race and a duplicated settlement, and no request-shaped read
//     can detect the difference afterwards.
//
//   - NOTHING IS EVER HELD. No row lock, no claim, no advisory lock. An operator
//     who opens a request and goes to lunch leaves nothing stale behind, which is
//     precisely what a claim lock could not promise.
//
// THE GUARD SAYS `status IN ('pending', 'processing')` AND IS DELIBERATELY NOT
// WRITTEN AGAINST outstandingPayoutRequest, even though the two match the same
// rows. What this one means is ANSWERABLE WITH MONEY, and it now names both
// states money can land against: an instant transfer recorded in one sitting,
// and one submitted days ago that the bank has finally confirmed (#184). It
// widened ON ITS OWN TERMS — somebody decided a Payout may be recorded against a
// submitted transfer — and the next state added to the outstanding list must be
// decided about here again rather than arriving through a shared constant.
//
// It stops a double RECORD, not a double TRANSFER: if two operators both wired
// the money at the bank, the loser must record their Payout directly or the
// books understate what left the account. The refusal message the service builds
// says so, and that message is the whole of the mitigation (ADR 0026).
func (r *Repository) FulfilPayoutRequest(ctx context.Context, input FulfilPayoutRequestInput) (*OperatorPayoutRow, *PayoutRequestRow, error) {
	tx, err := r.db.Pool.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, nil, err
	}
	// Rolls back everything, including the Payout, unless Commit lands first.
	defer func() { _ = tx.Rollback() }()

	// The ledger write, through the same statement the direct path uses, so the
	// two rows cannot differ by a column (see insertPayoutSQL).
	payout, err := scanOperatorPayout(tx.QueryRowContext(ctx, insertPayoutSQL,
		input.OrganizationID, input.AmountCents, input.PaidAt, input.Note, input.Operator))
	if err != nil {
		return nil, nil, err
	}

	// The compare-and-swap. payout_id is the ONLY link between this queue and the
	// ledger, and the schema permits it on a `paid` row alone (migration 043).
	request, err := payoutRequestScan(tx.QueryRowContext(ctx, `
		UPDATE payout_requests
		SET status = 'paid', payout_id = $2, resolved_by = $3, resolved_at = $4, updated_at = $4
		WHERE id = $1 AND status IN ('pending', 'processing')
		RETURNING `+payoutRequestColumns,
		input.RequestID, payout.ID, input.Operator, input.Now,
	))
	if errors.Is(err, sql.ErrNoRows) {
		// Somebody got there first. The deferred Rollback takes the Payout with
		// it, which is the entire point of the transaction; the caller reads the
		// request back afterwards to say what it actually became and who ended it.
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return &payout, request, nil
}

// DeclinePayoutRequest refuses the ask, with the reason its asker is shown.
//
// The same compare-and-swap, without the ledger write: a decline moves no money,
// so there is nothing to roll back and nothing to hold. Zero rows means the
// request had already ended, and the caller reads it back to say how.
//
// The reason is not optional here or anywhere. A decline that swallowed the
// request silently would generate the support thread the queue was built to
// prevent, and the schema's payout_requests_resolution_has_reason CHECK is the
// backstop under this argument — the same one that requires a failure to carry
// one (migrations 043, 046, ADR 0026).
//
// THE GUARD SAYS `status = 'pending'` AND IS NOT THE OUTSTANDING PREDICATE. What
// it means is UNTOUCHED BY AN OPERATOR: a decline is a judgement made before
// anything happened, and it must NOT widen with the definition of outstanding.
// The platform cannot refuse an ask its own operator has already submitted a
// transfer for — the money is out there, and the answer to a transfer that
// bounces is `failed`, not `declined` (ADR 0026, amendment).
func (r *Repository) DeclinePayoutRequest(ctx context.Context, requestID, reason, resolvedBy string, now time.Time) (*PayoutRequestRow, error) {
	row, err := payoutRequestScan(r.db.Pool.QueryRowContext(ctx, `
		UPDATE payout_requests
		SET status = 'declined', resolution_reason = $2, resolved_by = $3, resolved_at = $4, updated_at = $4
		WHERE id = $1 AND status = 'pending'
		RETURNING `+payoutRequestColumns,
		requestID, reason, resolvedBy, now,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return row, err
}

// MarkPayoutRequestFailed records that the bank sent the transfer back, with the
// reason its asker reads, or returns nil when the request was not one a transfer
// had been submitted for.
//
// THE GUARD SAYS `status = 'processing'` AND NOT `pending`, AND THAT IS THE
// WHOLE DIFFERENCE BETWEEN THIS AND EVERY OTHER CAS IN THIS FILE. A request
// nobody submitted a transfer for cannot have had one bounce: there is no
// transfer to have failed, and a `pending` request marked `failed` would be an
// operator declining somebody in a word that says the bank did it. The refusal
// is deliberate and the ADR says so (0026, amendment) — an operator who means to
// refuse an untouched ask declines it, which is a judgement with a name on it.
//
// It is also what makes the resolution stamp below honest: the schema requires
// transfer_submitted_by on a `processing` row (payout_requests_transfer_submission_is_whole,
// migration 045), so every `failed` row carries the record of the transfer that
// failed. Widening this guard to `pending` would produce failures with no
// transfer behind them.
//
// NOTHING IS WRITTEN TO `payouts` AND NOTHING IS DELETED FROM IT. There is
// nothing to unwind precisely because marking a request `processing` wrote no
// ledger row: the money never moved and the books never claimed it did. That is
// why this is a bare UPDATE where fulfilment is a transaction — a future reader
// tempted to negate or delete a Payout here should find there is none.
//
// `failed` IS TERMINAL. It stamps resolved_by and resolved_at exactly as a
// decline does, because a failure IS a resolution — the request has ended, and
// the schema's payout_requests_resolution_matches_status requires the pair on
// every state outside ('pending', 'processing'). The Organization is freed to
// ask again by the partial unique index, which counts only those two states, so
// there is deliberately no reopening path.
//
// The reason is not optional here or anywhere, and
// payout_requests_resolution_has_reason (migration 046) is the backstop under
// this argument: "failed" alone tells an organizer nothing, while "the account
// number was rejected" is also the instruction.
//
// Zero rows means the request was `pending`, or had already ended, or another
// operator got there first. The caller reads it back to say which.
func (r *Repository) MarkPayoutRequestFailed(ctx context.Context, requestID, reason, resolvedBy string, now time.Time) (*PayoutRequestRow, error) {
	row, err := payoutRequestScan(r.db.Pool.QueryRowContext(ctx, `
		UPDATE payout_requests
		SET status = 'failed', resolution_reason = $2, resolved_by = $3, resolved_at = $4, updated_at = $4
		WHERE id = $1 AND status = 'processing'
		RETURNING `+payoutRequestColumns,
		requestID, reason, resolvedBy, now,
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
// The WHERE is outstandingPayoutRequest, because what belongs in a work queue is
// every ask still awaiting an answer, not only the ones nobody has touched: a
// transfer an operator submitted and nobody confirmed is precisely the work that
// must not fall out of sight. The name still says Pending — the badge's public
// field is `pending_count` and renaming across the operator service, its handler
// and the API contract is not this prefactor's business (#183) — but the read
// means outstanding and widens with it.
//
// It must stay in step with the predicate of payout_requests_pending_queue_idx
// from migration 043, which was created for this query: the index stays the size
// of the backlog rather than the size of the history, so the queue does not slow
// down as answered requests accumulate behind it. Widen one without the other
// and the queue silently stops using its index.
func (r *Repository) ListPendingPayoutRequests(ctx context.Context, limit, offset int) ([]PayoutRequestRow, int, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+payoutRequestColumns+`, COUNT(*) OVER() AS total
		FROM payout_requests
		WHERE `+outstandingPayoutRequest+`
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
// are written against outstandingPayoutRequest — the SAME constant, which is the
// only reason they cannot disagree. The badge counts the work OUTSTANDING rather
// than the work untouched: a number that dropped when an operator submitted a
// transfer would tell the queue it was emptier than it is.
func (r *Repository) CountPendingPayoutRequests(ctx context.Context) (int, error) {
	var count int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM payout_requests WHERE `+outstandingPayoutRequest+`
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

// PayoutNoticeOrganization is the little an email needs to know about an
// Organization: what to call it, and the currency its money is stated in.
//
// It is deliberately not the identity module's Organization. The three Payout
// Request notices (#179) need two fields, and reaching across a module boundary
// for a whole aggregate — logo key, slug, created_at — to render a subject line
// would teach this path far more than it uses. `payouts.organization_id` already
// references `organizations` from inside this module (ADR 0026), so reading a
// name here is the same reach the balance query above already makes.
type PayoutNoticeOrganization struct {
	Name     string
	Currency string
}

// GetPayoutNoticeOrganization returns the name and currency for a notice about
// one Organization's Payout Request, or nil when the id names none.
//
// Nil is answerable rather than an error because of what the caller does with
// it: composing an email. A notice that cannot be composed is a notice that is
// not sent, and the money record it accompanies stands either way (ADR 0026).
func (r *Repository) GetPayoutNoticeOrganization(ctx context.Context, orgID string) (*PayoutNoticeOrganization, error) {
	var org PayoutNoticeOrganization
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT name, currency FROM organizations WHERE id = $1
	`, orgID).Scan(&org.Name, &org.Currency)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &org, nil
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
	var note, resolutionReason, resolvedBy, payoutID sql.NullString
	var transferSubmittedBy, transferReference sql.NullString
	var resolvedAt, transferSubmittedAt sql.NullTime

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
		&resolutionReason,
		&resolvedBy,
		&resolvedAt,
		&payoutID,
		&transferSubmittedBy,
		&transferSubmittedAt,
		&transferReference,
	); err != nil {
		return nil, err
	}

	if note.Valid {
		row.Note = &note.String
	}
	if resolutionReason.Valid {
		row.ResolutionReason = &resolutionReason.String
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
	if transferSubmittedBy.Valid {
		row.TransferSubmittedBy = &transferSubmittedBy.String
	}
	if transferSubmittedAt.Valid {
		at := transferSubmittedAt.Time
		row.TransferSubmittedAt = &at
	}
	if transferReference.Valid {
		row.TransferReference = &transferReference.String
	}
	return &row, nil
}
