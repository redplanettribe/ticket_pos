package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
)

// What the Sale Invoice Drainer needs of storage (#474, parent #471, ADR
// 0060): a claim on one due document that no other drain can take at the
// same time, the signing of an owed row — the one write that consumes a
// secuencial for a Sale Invoice — and a reschedule for a round that learned
// nothing from the authority.

// ClaimDueInvoice takes ONE Sale Invoice that is due to be worked — the
// longest overdue first — and hands it to this caller alone, in the Reversal
// Reconciler's lease pattern: the row's next_attempt_at is moved to leaseUntil
// in the same statement that selects it, under FOR UPDATE SKIP LOCKED, so a
// second drain running at the same moment either finds the next document or
// nothing. It returns nil when nothing is due, which is the ordinary answer
// and the reason a drain against an empty queue costs one query.
//
// ticketSaleID narrows the claim to one Sale's documents — the post-commit
// kick works the document it was kicked for and nothing else — and "" means
// any. Only documents the platform owes itself (kind `sale` or
// `credit_note`) in a workable state are claimed: owed (to sign and
// submit), pending (to poll), needs_attention still carrying a
// next_attempt_at (parked, but still polled or still unsignable), and
// authorized but not yet delivered (to mail, #475).
//
// A CREDIT NOTE WAITS FOR ITS FACTURA (#476, ADR 0060). An owed Credit Note
// is claimable only once the Sale Invoice it credits is terminal —
// authorized, or dead (withdrawn, annulled) — because a nota de crédito
// must name an authorized factura, and one whose factura is still pending
// or parked has nothing to name yet. The wait is decided HERE, at claim
// time, rather than by an event when the factura settles: the factura
// settles by the Drainer's own rounds, by an operator's Check status and
// by an operator's Mark annulled, and a claim that reads the factura's
// state on every tick needs none of them to remember the Credit Note. A
// signed Credit Note is past the wait and is polled like any other.
//
// A CREDIT NOTE WAITS FOR ITS SIBLING TOO (#484). Two Credit Notes may be
// owed against one factura — a reissue's, and then a reversal's while the
// reissue was in flight — and the SRI must see at most one: an owed Credit
// Note is claimable only while no OTHER Credit Note crediting the same
// factura is signed and undecided (pending, needs_attention). Once the
// sibling is answered, the claim admits it and the service decides:
// sibling authorized → this one is withdrawn, the factura is credited
// already; sibling dead → this one is issued.
//
// A CORRECTED FACTURA WAITS FOR ITS CREDIT NOTE (#483, ADR 0061), the
// sibling wait. An owed Sale Invoice that supersedes another is claimable
// only once a Credit Note crediting the factura it supersedes is
// authorized: the SRI must see the cancellation before the replacement,
// and a Sale must never hold two authorized facturas. Decided here at
// claim time for the same reason as the Credit Note's wait, and the reason
// the Credit Note is always worked first — both are due at once, and only
// one of them is claimable. It is claimable as well once NO Credit Note
// crediting the superseded factura is live any more (#484) — its Credit
// Note died, annulled at the portal — so that the service can withdraw it
// unsigned and leave the old factura current, exactly as an owed Credit
// Note is claimed to be withdrawn once its factura has died. Mark annulled
// withdraws it in its own act already; this is the Drainer's own net.
func (r *Repository) ClaimDueInvoice(ctx context.Context, now, leaseUntil time.Time, ticketSaleID string) (*InvoiceRow, error) {
	var id string
	err := r.db.Pool.QueryRowContext(ctx, `
		UPDATE invoicing_invoices
		SET next_attempt_at = $2
		WHERE id = (
			SELECT i.id
			FROM invoicing_invoices i
			WHERE i.next_attempt_at <= $1
			  AND i.kind IN ('sale', 'credit_note')
			  AND (i.status IN ('owed', 'pending', 'needs_attention')
			       OR (i.status = 'authorized' AND i.delivered_at IS NULL))
			  AND (i.kind <> 'credit_note' OR i.signed_xml IS NOT NULL OR (
			        EXISTS (
			          SELECT 1 FROM invoicing_invoices f
			          WHERE f.id = i.credits_invoice_id
			            AND f.status IN ('authorized', 'withdrawn', 'annulled'))
			        AND NOT EXISTS (
			          SELECT 1 FROM invoicing_invoices o
			          WHERE o.kind = 'credit_note' AND o.id <> i.id
			            AND o.credits_invoice_id = i.credits_invoice_id
			            AND o.status IN ('pending', 'needs_attention'))))
			  AND (i.supersedes_invoice_id IS NULL OR i.signed_xml IS NOT NULL OR NOT EXISTS (
			        SELECT 1 FROM invoicing_invoices n
			        WHERE n.kind = 'credit_note'
			          AND n.credits_invoice_id = i.supersedes_invoice_id
			          AND n.status IN ('owed', 'pending', 'needs_attention')))
			  AND ($3 = '' OR i.ticket_sale_id = NULLIF($3, '')::uuid)
			ORDER BY i.next_attempt_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id
	`, now, leaseUntil, ticketSaleID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim due invoice: %w", err)
	}
	return r.GetInvoice(ctx, id)
}

// SignedInvoice is what signing fixes on an owed row: the Issuer and the
// environment as they stand NOW (an Issuer moved to production signs
// still-owed documents in production), the emission facts, the snapshot the
// document was built from, and the numbering the secuencial is allocated
// under.
type SignedInvoice struct {
	IssuerID    string
	Environment invoicing.Environment
	IssuedOn    time.Time
	IssuedAt    time.Time
	IssuedBy    string
	Snapshot    invoicing.IssuerSnapshot
	CodDoc      string
	Estab       string
	PtoEmi      string
}

// SignOwedInvoice allocates the next secuencial and turns an owed row into a
// pending, signed one in ONE transaction — the Sale Invoice's counterpart of
// CreateInvoice. The prepare callback runs inside the transaction with the
// number, exactly as it does for a manual document: the clave de acceso, the
// build and the signature happen there, and if it fails the transaction
// rolls back and the number is not consumed.
//
// The UPDATE is guarded on the row being UNSIGNED AND STILL OWED — owed, or
// parked needs_attention while it could not be signed — so two drains that
// somehow both held the row could never sign it twice: the second finds no
// row to update and its number rolls back with it. A row parked unsignable
// leaves needs_attention here, and its attention_since with it (#477). A
// row a reversal withdrew between the claim and this write (#476) is found
// by the same guard, and the number rolls back with it: a withdrawn
// document is never signed, whoever got to it first.
func (r *Repository) SignOwedInvoice(ctx context.Context, invoiceID string, in SignedInvoice, prepare func(secuencial int64) (*PreparedInvoice, error)) (*InvoiceRow, error) {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin sign owed invoice: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	secuencial, err := allocateSecuencial(ctx, tx, in.IssuerID, in.Environment, in.CodDoc, in.Estab, in.PtoEmi)
	if err != nil {
		return nil, err
	}
	prepared, err := prepare(secuencial)
	if err != nil {
		return nil, err
	}
	snapshot, err := json.Marshal(in.Snapshot)
	if err != nil {
		return nil, fmt.Errorf("marshal issuer snapshot: %w", err)
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE invoicing_invoices SET
			issuer_id = $2,
			environment = $3,
			status = $4,
			issued_on = $5,
			issued_at = $6,
			issued_by = $7,
			issuer_snapshot = $8,
			signed_xml = $9,
			attention_since = NULL,
			updated_at = NOW()
		WHERE id = $1 AND signed_xml IS NULL AND status IN ('owed', 'needs_attention')
	`, invoiceID, in.IssuerID, in.Environment, invoicing.InvoiceStatusPending, in.IssuedOn, in.IssuedAt, in.IssuedBy,
		snapshot, prepared.SignedXML)
	if err != nil {
		return nil, fmt.Errorf("sign owed invoice: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return nil, fmt.Errorf("sign owed invoice: invoice %s is already signed or withdrawn: %w", invoiceID, sql.ErrNoRows)
	}
	if err := insertEcuadorDetails(ctx, tx, invoiceID, in.IssuerID, in.Environment,
		NewInvoice{CodDoc: in.CodDoc, Estab: in.Estab, PtoEmi: in.PtoEmi}, secuencial, prepared.AccessKey); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit sign owed invoice: %w", err)
	}
	return r.GetInvoice(ctx, invoiceID)
}

// Reschedule writes where a document stands after a round that got no
// answer from the authority — or could not ask it at all — and when the
// Drainer looks again. The messages are what the platform has to say about
// it (why it could not be signed, for instance) and replace the last ones;
// nil leaves them as they were. Nothing on the signed side is touched. `at`
// is the round's instant, recorded as attention_since when the round parks
// the document (#477).
//
// GUARDED ON THE STATE THE ROUND READ. A claim is a lease, not a lock: a
// reversal may withdraw the row under it (#476), an operator may mark it
// annulled (#477), and a write that assumed the row was still `from` would
// resurrect a document that is dead — a withdrawn Sale Invoice parked
// needs_attention is signed on the next round, for a sale that no longer
// stands. It reports false, having written nothing, when the row is no
// longer in `from`; the round then leaves the row to whoever moved it.
func (r *Repository) Reschedule(ctx context.Context, invoiceID string, from, to invoicing.InvoiceStatus, messages []invoicing.AuthorityMessage, nextAttemptAt *time.Time, at time.Time) (bool, error) {
	var encoded []byte
	if messages != nil {
		var err error
		if encoded, err = json.Marshal(messages); err != nil {
			return false, fmt.Errorf("marshal messages: %w", err)
		}
	}
	res, err := r.db.Pool.ExecContext(ctx, `
		UPDATE invoicing_invoices SET
			status = $2,
			last_messages = COALESCE($3::jsonb, last_messages),
			next_attempt_at = $4,
			attention_since = `+attentionSinceExpr("$2", "$5")+`,
			updated_at = NOW()
		WHERE id = $1 AND status = $6
	`, invoiceID, to, encoded, nextAttemptAt, at, from)
	if err != nil {
		return false, fmt.Errorf("reschedule invoice: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("reschedule invoice: %w", err)
	}
	return n == 1, nil
}

// DueForDelivery makes an authorized, undelivered document due at once —
// and only one that is off the queue: next_attempt_at NULL. It is the
// operator's Check status and Resend that need it (#475): the Drainer
// keeps its own claim through delivery, but a document those actions heal
// was parked with nothing due, and would otherwise stay authorized and
// never mailed. One that is on the ladder, or under a round's lease, is
// left exactly as it is: the round that holds it delivers it, and a due
// time written over a lease would let a second round mail it again. It
// reports whether the row was made due.
func (r *Repository) DueForDelivery(ctx context.Context, invoiceID string, at time.Time) (bool, error) {
	res, err := r.db.Pool.ExecContext(ctx, `
		UPDATE invoicing_invoices SET
			next_attempt_at = $2,
			updated_at = NOW()
		WHERE id = $1 AND kind <> 'manual' AND status = 'authorized'
		  AND delivered_at IS NULL AND next_attempt_at IS NULL
	`, invoiceID, at)
	if err != nil {
		return false, fmt.Errorf("due for delivery: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("due for delivery: %w", err)
	}
	return n == 1, nil
}

// CountSaleInvoicesByStatus counts the Sale Invoices standing in each state,
// for the drain response: how big the backlog is and how many documents wait
// for an operator, with no buyer in sight.
func (r *Repository) CountSaleInvoicesByStatus(ctx context.Context) (map[string]int, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT status, COUNT(*) FROM invoicing_invoices WHERE kind <> 'manual' GROUP BY status
	`)
	if err != nil {
		return nil, fmt.Errorf("count sale invoices: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, fmt.Errorf("scan sale invoice count: %w", err)
		}
		out[status] = n
	}
	return out, rows.Err()
}

// MarkDelivered records that the authorized document was handed to its
// buyer, and takes it off the queue: delivered_at is written and
// next_attempt_at cleared in one statement, guarded on delivered_at still
// being NULL so that a document is never recorded delivered twice.
func (r *Repository) MarkDelivered(ctx context.Context, invoiceID string, at time.Time) error {
	res, err := r.db.Pool.ExecContext(ctx, `
		UPDATE invoicing_invoices SET
			delivered_at = $2,
			next_attempt_at = NULL,
			updated_at = NOW()
		WHERE id = $1 AND delivered_at IS NULL
	`, invoiceID, at)
	if err != nil {
		return fmt.Errorf("mark delivered: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("mark delivered: invoice %s is already delivered: %w", invoiceID, sql.ErrNoRows)
	}
	return nil
}

// SaleDeliveryFacts is what the delivery mail needs of the Sale beside the
// document's own row: the Event's name, the Sale Locale as stored, and the
// buyer's remembered Mail Locale — the two raw halves of the chain
// platform.ResolveMailLocale walks for the receipt, so the second mail
// about a sale is written in the language of the first.
type SaleDeliveryFacts struct {
	EventName      string
	SaleLocale     string
	CustomerLocale string
}

// GetSaleDeliveryFacts reads the delivery facts of one Ticket Sale, or nil
// when there is no such Sale.
func (r *Repository) GetSaleDeliveryFacts(ctx context.Context, ticketSaleID string) (*SaleDeliveryFacts, error) {
	var f SaleDeliveryFacts
	var saleLocale, customerLocale sql.NullString
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT e.name, ts.locale, c.mail_locale
		FROM ticket_sales ts
		JOIN events e ON e.id = ts.event_id
		LEFT JOIN customers c ON c.id = ts.customer_id
		WHERE ts.id = $1
	`, ticketSaleID).Scan(&f.EventName, &saleLocale, &customerLocale)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get sale delivery facts: %w", err)
	}
	f.SaleLocale = saleLocale.String
	f.CustomerLocale = customerLocale.String
	return &f, nil
}
