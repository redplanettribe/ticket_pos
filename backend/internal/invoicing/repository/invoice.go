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

// The Tax Invoice's storage (#454): the core row, its lines and additional
// fields, the Ecuador detail row, and the append-only attempts ledger.
// Nothing here is ever deleted.
//
// From #473 a row may exist UNSIGNED: an owed Sale Invoice or Credit Note
// has no Issuer, no environment, no emission facts, no snapshot, no bytes
// and no Ecuador detail row until the Drainer signs it. The reads here LEFT
// JOIN the detail row and scan the signed-side columns as nullable, and
// OweInvoice is the one write that produces such a row.

// InvoiceRow is one Tax Invoice as stored, with its Ecuador numbering (nil
// until signed) and its attempts.
type InvoiceRow struct {
	Invoice  invoicing.Invoice
	Ecuador  *invoicing.EcuadorInvoiceDetails
	Attempts []invoicing.Attempt
}

// NewInvoice is everything the service knows before a number is allocated:
// the invoice minus its id, secuencial and signed document.
type NewInvoice struct {
	Invoice invoicing.Invoice
	CodDoc  string
	Estab   string
	PtoEmi  string
}

// PreparedInvoice is what the service produces once it holds the secuencial:
// the clave de acceso and the signed bytes.
type PreparedInvoice struct {
	AccessKey string
	SignedXML []byte
}

// execer is what the child-row inserts need of a transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// CreateInvoice allocates the next secuencial and stores the invoice in ONE
// transaction.
//
// The secuencial comes from a single INSERT … ON CONFLICT DO UPDATE …
// RETURNING on the sequence row — created on first use, bumped after — which
// takes a row lock that serialises two operators pressing Issue at the same
// moment, so they get distinct consecutive numbers (#450 story 28). The
// prepare callback runs inside the transaction with that number: it is where
// the service computes the clave de acceso and builds and signs the document,
// none of which this package knows about. If prepare fails the transaction
// rolls back and the number is not consumed.
//
// This is the MANUAL document's birth (kind manual, status pending, signed
// at once). A Sale Invoice is born by OweInvoice and numbered later.
func (r *Repository) CreateInvoice(ctx context.Context, in NewInvoice, prepare func(secuencial int64) (*PreparedInvoice, error)) (*InvoiceRow, error) {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create invoice: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	inv := in.Invoice
	secuencial, err := allocateSecuencial(ctx, tx, inv.IssuerID, inv.Environment, in.CodDoc, in.Estab, in.PtoEmi)
	if err != nil {
		return nil, err
	}

	prepared, err := prepare(secuencial)
	if err != nil {
		return nil, err
	}

	if inv.Issuer == nil {
		return nil, errors.New("create invoice: a signed document needs its Issuer snapshot")
	}
	snapshot, err := json.Marshal(inv.Issuer)
	if err != nil {
		return nil, fmt.Errorf("marshal issuer snapshot: %w", err)
	}
	var id string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO invoicing_invoices
			(kind, issuer_id, country, environment, status, issued_on, issued_at, issued_by,
			 recipient_tax_id_type, recipient_tax_id, recipient_legal_name, recipient_address, recipient_email,
			 issuer_snapshot, currency, subtotal_cents, discount_cents, iva_cents, total_cents, payment_method,
			 signed_xml, last_messages)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, '[]'::jsonb)
		RETURNING id
	`,
		invoicing.DocumentKindManual, inv.IssuerID, inv.Country, inv.Environment, invoicing.InvoiceStatusPending, inv.IssuedOn, inv.IssuedAt, inv.IssuedBy,
		inv.Recipient.TaxIDType, inv.Recipient.TaxID, inv.Recipient.LegalName, inv.Recipient.Address, inv.Recipient.Email,
		snapshot, inv.Currency, inv.SubtotalCents, inv.DiscountCents, inv.IVACents, inv.TotalCents, inv.PaymentMethod,
		prepared.SignedXML,
	).Scan(&id); err != nil {
		return nil, fmt.Errorf("insert invoice: %w", err)
	}

	if err := insertInvoiceChildren(ctx, tx, id, inv); err != nil {
		return nil, err
	}

	if err := insertEcuadorDetails(ctx, tx, id, inv.IssuerID, inv.Environment, in, secuencial, prepared.AccessKey); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create invoice: %w", err)
	}
	return r.GetInvoice(ctx, id)
}

// allocateSecuencial takes the next number under the Issuer's sequence for
// the document type, establecimiento and punto de emisión, in the caller's
// transaction: a single INSERT … ON CONFLICT DO UPDATE … RETURNING on the
// sequence row — created on first use, bumped after — whose row lock
// serialises two signers at the same moment into distinct consecutive
// numbers. If the transaction rolls back the number is not consumed.
func allocateSecuencial(ctx context.Context, tx *sql.Tx, issuerID string, env invoicing.Environment, codDoc, estab, ptoEmi string) (int64, error) {
	var secuencial int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO invoicing_sequences_ec (issuer_id, environment, cod_doc, estab, pto_emi, last_secuencial)
		VALUES ($1, $2, $3, $4, $5, 1)
		ON CONFLICT (issuer_id, environment, cod_doc, estab, pto_emi) DO UPDATE SET
			last_secuencial = invoicing_sequences_ec.last_secuencial + 1,
			updated_at = NOW()
		RETURNING last_secuencial
	`, issuerID, env, codDoc, estab, ptoEmi).Scan(&secuencial); err != nil {
		return 0, fmt.Errorf("allocate secuencial: %w", err)
	}
	return secuencial, nil
}

func insertEcuadorDetails(ctx context.Context, tx execer, id, issuerID string, env invoicing.Environment, in NewInvoice, secuencial int64, accessKey string) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO invoicing_invoices_ec (invoice_id, issuer_id, environment, cod_doc, estab, pto_emi, secuencial, access_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, id, issuerID, env, in.CodDoc, in.Estab, in.PtoEmi, secuencial, accessKey); err != nil {
		return fmt.Errorf("insert ecuador invoice details: %w", err)
	}
	return nil
}

// OweInvoice records a document the platform owes — a Sale Invoice, or later
// a Credit Note — in the CALLER'S transaction, which is the one recording
// the Ticket Sale it is about (#473, ADR 0060). It writes the core row in
// state owed with its Recipient, lines and totals, and nothing on the signed
// side: no Issuer, no number, no bytes, no attempt. The Drainer fills those
// in later; if this insert fails, the sale it rides with is not recorded.
//
// Kind, ticket_sale_id, iva_rate and — for a Credit Note — what it credits
// and why (a reversal route or a reissue, #481) come from the Invoice;
// country comes from it too, since the Sale is invoiced by the country's
// Issuer whether or not one exists yet.
func (r *Repository) OweInvoice(ctx context.Context, tx *sql.Tx, inv invoicing.Invoice) (string, error) {
	var id string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO invoicing_invoices
			(kind, country, status, ticket_sale_id, credits_invoice_id, credit_note_reason, iva_rate,
			 recipient_tax_id_type, recipient_tax_id, recipient_legal_name, recipient_address, recipient_email,
			 currency, subtotal_cents, discount_cents, iva_cents, total_cents, payment_method,
			 next_attempt_at, last_messages)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid, NULLIF($6, ''), $7,
		        $8, $9, $10, $11, $12,
		        $13, $14, $15, $16, $17, $18,
		        $19, '[]'::jsonb)
		RETURNING id
	`,
		inv.Kind, inv.Country, invoicing.InvoiceStatusOwed, inv.TicketSaleID, inv.CreditsInvoiceID, inv.CreditNoteReason, inv.IVARate,
		inv.Recipient.TaxIDType, inv.Recipient.TaxID, inv.Recipient.LegalName, inv.Recipient.Address, inv.Recipient.Email,
		inv.Currency, inv.SubtotalCents, inv.DiscountCents, inv.IVACents, inv.TotalCents, inv.PaymentMethod,
		inv.NextAttemptAt,
	).Scan(&id); err != nil {
		return "", fmt.Errorf("owe invoice: %w", err)
	}
	if err := insertInvoiceChildren(ctx, tx, id, inv); err != nil {
		return "", err
	}
	return id, nil
}

func insertInvoiceChildren(ctx context.Context, tx execer, id string, inv invoicing.Invoice) error {
	for _, l := range inv.Lines {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO invoicing_invoice_lines
				(invoice_id, position, description, quantity_millionths, unit_price_cents, discount_cents, iva_rate, base_cents, iva_cents)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`, id, l.Position, l.Description, l.QuantityMillionths, l.UnitPriceCents, l.DiscountCents, l.IVARate, l.BaseCents, l.IVACents); err != nil {
			return fmt.Errorf("insert invoice line %d: %w", l.Position, err)
		}
	}
	for _, f := range inv.AdditionalFields {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO invoicing_additional_fields (invoice_id, position, name, value)
			VALUES ($1, $2, $3, $4)
		`, id, f.Position, f.Name, f.Value); err != nil {
			return fmt.Errorf("insert additional field %d: %w", f.Position, err)
		}
	}
	return nil
}

// OutcomeUpdate is what an authority's answer changes on the invoice row.
type OutcomeUpdate struct {
	// From is the status the row was read in: the write is guarded on it
	// (see ApplyOutcome).
	From     invoicing.InvoiceStatus
	Status   invoicing.InvoiceStatus
	Messages []invoicing.AuthorityMessage
	// Authorization and AuthorizationXML are set only when Status is
	// authorized.
	Authorization    *invoicing.Authorization
	AuthorizationXML []byte
	// NextAttemptAt is when the Sale Invoice Drainer should look again (#474);
	// nil when nothing is due, which is every manual document and every
	// settled one. Written on every answer, so a claim's lease never outlives
	// the answer it was taken for — except when KeepNextAttemptAt says so.
	NextAttemptAt *time.Time
	// KeepNextAttemptAt leaves next_attempt_at exactly as it stands: an
	// authorized Sale Invoice stays under the round's lease until that
	// round has delivered it (#475), so a second round cannot claim it —
	// and mail the buyer again — between the authorization and the
	// delivery. NextAttemptAt is ignored when set.
	KeepNextAttemptAt bool
	// At is the service clock's instant the answer was applied: what
	// attention_since records when the answer parks the document (#477).
	At time.Time
}

// ApplyOutcome writes the authority's latest answer onto the invoice: its
// status, the messages verbatim, when the Drainer looks again, and — when
// authorized — the authorization number, date and XML. The signed document
// is never touched here. attention_since is set when the answer parks the
// document and kept while it stays parked; any other answer clears it
// (#477).
//
// GUARDED ON u.From, the status the answer was asked for. An operator may
// have marked the document annulled while the authority was being asked
// (#477), and an answer written over that record would undo it. It reports
// false, having written nothing, when the row is no longer in u.From.
func (r *Repository) ApplyOutcome(ctx context.Context, invoiceID string, u OutcomeUpdate) (bool, error) {
	messages, err := json.Marshal(messagesOrEmpty(u.Messages))
	if err != nil {
		return false, fmt.Errorf("marshal messages: %w", err)
	}
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin apply outcome: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE invoicing_invoices SET
			status = $2,
			last_messages = $3,
			authorization_xml = COALESCE($4, authorization_xml),
			next_attempt_at = CASE WHEN $7 THEN next_attempt_at ELSE $5::timestamptz END,
			attention_since = `+attentionSinceExpr("$2", "$6")+`,
			updated_at = NOW()
		WHERE id = $1 AND status = $8
	`, invoiceID, u.Status, messages, u.AuthorizationXML, u.NextAttemptAt, u.At, u.KeepNextAttemptAt, u.From)
	if err != nil {
		return false, fmt.Errorf("apply outcome: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("apply outcome: %w", err)
	}
	if n == 0 {
		return false, nil
	}
	if u.Authorization != nil {
		if _, err := tx.ExecContext(ctx, `
			UPDATE invoicing_invoices_ec SET authorization_number = $2, authorization_date = $3
			WHERE invoice_id = $1
		`, invoiceID, u.Authorization.Number, u.Authorization.Date); err != nil {
			return false, fmt.Errorf("apply authorization: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit apply outcome: %w", err)
	}
	return true, nil
}

// RecordAttempt appends one request to the attempts ledger.
func (r *Repository) RecordAttempt(ctx context.Context, a invoicing.Attempt) error {
	messages, err := json.Marshal(messagesOrEmpty(a.Messages))
	if err != nil {
		return fmt.Errorf("marshal attempt messages: %w", err)
	}
	if _, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO invoicing_attempts (invoice_id, operation, outcome, messages, error, started_at, duration_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, a.InvoiceID, a.Operation, a.Outcome, messages, a.Error, a.StartedAt, a.Duration.Milliseconds()); err != nil {
		return fmt.Errorf("record attempt: %w", err)
	}
	return nil
}

// attentionSinceExpr is the one rule for attention_since, stated once for
// every write that moves a status: set at `at` when the row enters
// needs_attention, kept while it stays there, NULL in any other state — so
// the queue's "since when" is the instant the document was parked and not
// the last time it was polled.
func attentionSinceExpr(status, at string) string {
	return `CASE WHEN ` + status + ` = 'needs_attention' THEN COALESCE(attention_since, ` + at + `) ELSE NULL END`
}

// AnnulInvoice records the operator's manual portal annulment (#477): the
// status, who and when, and nothing else — the number, the clave, the
// signed XML and the authority's last messages stay exactly as they were.
// next_attempt_at is cleared so the Drainer never claims it again, and
// attention_since with it. Guarded on the two states Mark annulled is
// allowed from, so a second press, or a press racing a late AUTORIZADO,
// finds no row to update; the service reads the row back to say which.
func (r *Repository) AnnulInvoice(ctx context.Context, invoiceID, annulledBy string, annulledAt time.Time) (bool, error) {
	res, err := r.db.Pool.ExecContext(ctx, `
		UPDATE invoicing_invoices SET
			status = $2,
			annulled_by = $3,
			annulled_at = $4,
			next_attempt_at = NULL,
			attention_since = NULL,
			updated_at = NOW()
		WHERE id = $1 AND status IN ($5, $6) AND signed_xml IS NOT NULL
	`, invoiceID, invoicing.InvoiceStatusAnnulled, annulledBy, annulledAt,
		invoicing.InvoiceStatusNeedsAttention, invoicing.InvoiceStatusPending)
	if err != nil {
		return false, fmt.Errorf("annul invoice: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("annul invoice: %w", err)
	}
	return n == 1, nil
}

const invoiceColumns = `
	i.id, i.kind, i.issuer_id, i.country, i.environment, i.status, i.issued_on, i.issued_at, i.issued_by,
	i.recipient_tax_id_type, i.recipient_tax_id, i.recipient_legal_name, i.recipient_address, i.recipient_email,
	i.issuer_snapshot, i.currency, i.subtotal_cents, i.discount_cents, i.iva_cents, i.total_cents, i.payment_method,
	i.signed_xml, i.authorization_xml, i.last_messages,
	i.ticket_sale_id, ts.confirmation_ref, i.credits_invoice_id, i.credit_note_reason, i.iva_rate, i.delivered_at, i.next_attempt_at,
	(SELECT c.id FROM invoicing_invoices c WHERE c.credits_invoice_id = i.id ORDER BY c.created_at DESC, c.id DESC LIMIT 1),
	i.attention_since, i.annulled_by, i.annulled_at,
	i.created_at, i.updated_at,
	e.cod_doc, e.estab, e.pto_emi, e.secuencial, e.access_key, e.authorization_number, e.authorization_date`

// The Ecuador detail row is absent until signing, and a Ticket Sale only a
// Sale-side document has; both joins are LEFT for that reason. The Sale's
// Confirmation reference is read beside the row rather than copied onto it:
// it is the Sale's fact, and the operator surfaces want it on every read.
// The Credit Note crediting a Sale Invoice is read the same way (#476): the
// link is stored once, on the Credit Note, and walked back here.
const invoiceFrom = `
	FROM invoicing_invoices i
	LEFT JOIN invoicing_invoices_ec e ON e.invoice_id = i.id
	LEFT JOIN ticket_sales ts ON ts.id = i.ticket_sale_id`

// GetInvoice reads one Tax Invoice in full, or nil when none has the id.
func (r *Repository) GetInvoice(ctx context.Context, id string) (*InvoiceRow, error) {
	row, err := scanInvoice(r.db.Pool.QueryRowContext(ctx, `SELECT `+invoiceColumns+invoiceFrom+` WHERE i.id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get invoice: %w", err)
	}
	if err := r.loadInvoiceChildren(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

// ListInvoices reads a page of Tax Invoices newest first — by emission
// instant once signed, by owing instant before — without their lines,
// fields or attempts, and the total count. kind narrows the page to one
// document kind (#477); "" is every kind.
func (r *Repository) ListInvoices(ctx context.Context, kind invoicing.DocumentKind, page, pageSize int) ([]InvoiceRow, int, error) {
	var total int
	if err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM invoicing_invoices WHERE ($1 = '' OR kind = $1)
	`, string(kind)).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count invoices: %w", err)
	}
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+invoiceColumns+invoiceFrom+`
		WHERE ($1 = '' OR i.kind = $1)
		ORDER BY COALESCE(i.issued_at, i.created_at) DESC, i.created_at DESC, i.id DESC
		LIMIT $2 OFFSET $3
	`, string(kind), pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list invoices: %w", err)
	}
	defer rows.Close()
	out, err := scanInvoices(rows)
	if err != nil {
		return nil, 0, fmt.Errorf("list invoices: %w", err)
	}
	return out, total, nil
}

// ListNeedsAttention reads a page of the documents parked needs_attention,
// of every kind, LONGEST WAITING FIRST — by the instant each was parked —
// and how many there are in all (#477). Ties, which the same drain round
// can produce, break on the row's own age.
func (r *Repository) ListNeedsAttention(ctx context.Context, page, pageSize int) ([]InvoiceRow, int, error) {
	total, err := r.CountNeedsAttention(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+invoiceColumns+invoiceFrom+`
		WHERE i.status = $1
		ORDER BY i.attention_since ASC, i.created_at ASC, i.id ASC
		LIMIT $2 OFFSET $3
	`, invoicing.InvoiceStatusNeedsAttention, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list needs attention: %w", err)
	}
	defer rows.Close()
	out, err := scanInvoices(rows)
	if err != nil {
		return nil, 0, fmt.Errorf("list needs attention: %w", err)
	}
	return out, total, nil
}

// CountNeedsAttention counts the documents parked needs_attention: the
// Operator Dashboard's badge, and exactly what the queue lists.
func (r *Repository) CountNeedsAttention(ctx context.Context) (int, error) {
	var n int
	if err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM invoicing_invoices WHERE status = $1
	`, invoicing.InvoiceStatusNeedsAttention).Scan(&n); err != nil {
		return 0, fmt.Errorf("count needs attention: %w", err)
	}
	return n, nil
}

// ListInvoicesBySale reads every document about one Ticket Sale — its Sale
// Invoice and, later, its Credit Note — oldest first, for the operator's
// Sale lookup (#477). Without lines, fields or attempts.
func (r *Repository) ListInvoicesBySale(ctx context.Context, ticketSaleID string) ([]InvoiceRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+invoiceColumns+invoiceFrom+`
		WHERE i.ticket_sale_id = $1
		ORDER BY i.created_at ASC, i.id ASC
	`, ticketSaleID)
	if err != nil {
		return nil, fmt.Errorf("list invoices by sale: %w", err)
	}
	defer rows.Close()
	out, err := scanInvoices(rows)
	if err != nil {
		return nil, fmt.Errorf("list invoices by sale: %w", err)
	}
	return out, nil
}

func scanInvoices(rows *sql.Rows) ([]InvoiceRow, error) {
	var out []InvoiceRow
	for rows.Next() {
		row, err := scanInvoice(rows)
		if err != nil {
			return nil, fmt.Errorf("scan invoice: %w", err)
		}
		out = append(out, *row)
	}
	return out, rows.Err()
}

// querier is what the child-row reads need of a connection: the pool, or
// a transaction that must see its own writes (#476).
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func (r *Repository) loadInvoiceChildren(ctx context.Context, row *InvoiceRow) error {
	return loadInvoiceChildrenFrom(ctx, r.db.Pool, row)
}

func loadInvoiceChildrenFrom(ctx context.Context, q querier, row *InvoiceRow) error {
	id := row.Invoice.ID
	lines, err := q.QueryContext(ctx, `
		SELECT position, description, quantity_millionths, unit_price_cents, discount_cents, iva_rate, base_cents, iva_cents
		FROM invoicing_invoice_lines WHERE invoice_id = $1 ORDER BY position
	`, id)
	if err != nil {
		return fmt.Errorf("invoice lines: %w", err)
	}
	defer lines.Close()
	for lines.Next() {
		var l invoicing.InvoiceLine
		if err := lines.Scan(&l.Position, &l.Description, &l.QuantityMillionths, &l.UnitPriceCents, &l.DiscountCents, &l.IVARate, &l.BaseCents, &l.IVACents); err != nil {
			return fmt.Errorf("scan invoice line: %w", err)
		}
		row.Invoice.Lines = append(row.Invoice.Lines, l)
	}
	if err := lines.Err(); err != nil {
		return fmt.Errorf("invoice lines: %w", err)
	}

	fields, err := q.QueryContext(ctx, `
		SELECT position, name, value FROM invoicing_additional_fields WHERE invoice_id = $1 ORDER BY position
	`, id)
	if err != nil {
		return fmt.Errorf("additional fields: %w", err)
	}
	defer fields.Close()
	for fields.Next() {
		var f invoicing.AdditionalField
		if err := fields.Scan(&f.Position, &f.Name, &f.Value); err != nil {
			return fmt.Errorf("scan additional field: %w", err)
		}
		row.Invoice.AdditionalFields = append(row.Invoice.AdditionalFields, f)
	}
	if err := fields.Err(); err != nil {
		return fmt.Errorf("additional fields: %w", err)
	}

	attempts, err := q.QueryContext(ctx, `
		SELECT id, operation, outcome, messages, error, started_at, duration_ms
		FROM invoicing_attempts WHERE invoice_id = $1 ORDER BY id
	`, id)
	if err != nil {
		return fmt.Errorf("attempts: %w", err)
	}
	defer attempts.Close()
	for attempts.Next() {
		var a invoicing.Attempt
		var messages []byte
		var durationMS int64
		if err := attempts.Scan(&a.ID, &a.Operation, &a.Outcome, &messages, &a.Error, &a.StartedAt, &durationMS); err != nil {
			return fmt.Errorf("scan attempt: %w", err)
		}
		a.InvoiceID = id
		a.Duration = time.Duration(durationMS) * time.Millisecond
		if err := json.Unmarshal(messages, &a.Messages); err != nil {
			return fmt.Errorf("decode attempt messages: %w", err)
		}
		row.Attempts = append(row.Attempts, a)
	}
	return attempts.Err()
}

func scanInvoice(scanner interface{ Scan(dest ...any) error }) (*InvoiceRow, error) {
	var row InvoiceRow
	inv := &row.Invoice
	var (
		issuerID, environment, issuedBy    sql.NullString
		issuedOn, issuedAt                 sql.NullTime
		snapshot, messages                 []byte
		ticketSaleID, confirmationRef      sql.NullString
		creditsInvoiceID, creditNoteReason sql.NullString
		creditedByInvoiceID                sql.NullString
		ivaRate                            sql.NullString
		deliveredAt, nextAttemptAt         sql.NullTime
		attentionSince, annulledAt         sql.NullTime
		annulledBy                         sql.NullString
		codDoc, estab, ptoEmi, accessKey   sql.NullString
		secuencial                         sql.NullInt64
		authNumber                         sql.NullString
		authDate                           sql.NullTime
	)
	if err := scanner.Scan(
		&inv.ID, &inv.Kind, &issuerID, &inv.Country, &environment, &inv.Status, &issuedOn, &issuedAt, &issuedBy,
		&inv.Recipient.TaxIDType, &inv.Recipient.TaxID, &inv.Recipient.LegalName, &inv.Recipient.Address, &inv.Recipient.Email,
		&snapshot, &inv.Currency, &inv.SubtotalCents, &inv.DiscountCents, &inv.IVACents, &inv.TotalCents, &inv.PaymentMethod,
		&inv.SignedXML, &inv.AuthorizationXML, &messages,
		&ticketSaleID, &confirmationRef, &creditsInvoiceID, &creditNoteReason, &ivaRate, &deliveredAt, &nextAttemptAt,
		&creditedByInvoiceID,
		&attentionSince, &annulledBy, &annulledAt,
		&inv.CreatedAt, &inv.UpdatedAt,
		&codDoc, &estab, &ptoEmi, &secuencial, &accessKey, &authNumber, &authDate,
	); err != nil {
		return nil, err
	}
	inv.IssuerID = issuerID.String
	inv.Environment = invoicing.Environment(environment.String)
	inv.IssuedOn = issuedOn.Time
	inv.IssuedAt = issuedAt.Time
	inv.IssuedBy = issuedBy.String
	if len(snapshot) > 0 {
		inv.Issuer = &invoicing.IssuerSnapshot{}
		if err := json.Unmarshal(snapshot, inv.Issuer); err != nil {
			return nil, fmt.Errorf("decode issuer snapshot: %w", err)
		}
	}
	if err := json.Unmarshal(messages, &inv.Messages); err != nil {
		return nil, fmt.Errorf("decode messages: %w", err)
	}
	inv.TicketSaleID = ticketSaleID.String
	inv.SaleConfirmationRef = confirmationRef.String
	inv.CreditsInvoiceID = creditsInvoiceID.String
	inv.CreditNoteReason = creditNoteReason.String
	inv.CreditedByInvoiceID = creditedByInvoiceID.String
	inv.IVARate = invoicing.IVARate(ivaRate.String)
	if deliveredAt.Valid {
		t := deliveredAt.Time
		inv.DeliveredAt = &t
	}
	if nextAttemptAt.Valid {
		t := nextAttemptAt.Time
		inv.NextAttemptAt = &t
	}
	if attentionSince.Valid {
		t := attentionSince.Time
		inv.AttentionSince = &t
	}
	inv.AnnulledBy = annulledBy.String
	if annulledAt.Valid {
		t := annulledAt.Time
		inv.AnnulledAt = &t
	}
	if accessKey.Valid {
		row.Ecuador = &invoicing.EcuadorInvoiceDetails{
			CodDoc:     codDoc.String,
			Estab:      estab.String,
			PtoEmi:     ptoEmi.String,
			Secuencial: secuencial.Int64,
			AccessKey:  accessKey.String,
		}
		if authNumber.Valid {
			row.Ecuador.Authorization = &invoicing.Authorization{Number: authNumber.String, Date: authDate.Time}
		}
	}
	return &row, nil
}

func messagesOrEmpty(in []invoicing.AuthorityMessage) []invoicing.AuthorityMessage {
	if in == nil {
		return []invoicing.AuthorityMessage{}
	}
	return in
}
