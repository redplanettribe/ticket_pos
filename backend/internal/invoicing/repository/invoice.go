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

// InvoiceRow is one Tax Invoice as stored, with its Ecuador numbering and
// its attempts.
type InvoiceRow struct {
	Invoice  invoicing.Invoice
	Ecuador  invoicing.EcuadorInvoiceDetails
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
func (r *Repository) CreateInvoice(ctx context.Context, in NewInvoice, prepare func(secuencial int64) (*PreparedInvoice, error)) (*InvoiceRow, error) {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create invoice: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	inv := in.Invoice
	var secuencial int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO invoicing_sequences_ec (issuer_id, environment, cod_doc, estab, pto_emi, last_secuencial)
		VALUES ($1, $2, $3, $4, $5, 1)
		ON CONFLICT (issuer_id, environment, cod_doc, estab, pto_emi) DO UPDATE SET
			last_secuencial = invoicing_sequences_ec.last_secuencial + 1,
			updated_at = NOW()
		RETURNING last_secuencial
	`, inv.IssuerID, inv.Environment, in.CodDoc, in.Estab, in.PtoEmi).Scan(&secuencial); err != nil {
		return nil, fmt.Errorf("allocate secuencial: %w", err)
	}

	prepared, err := prepare(secuencial)
	if err != nil {
		return nil, err
	}

	snapshot, err := json.Marshal(inv.Issuer)
	if err != nil {
		return nil, fmt.Errorf("marshal issuer snapshot: %w", err)
	}
	var id string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO invoicing_invoices
			(issuer_id, country, environment, status, issued_on, issued_at, issued_by,
			 recipient_tax_id_type, recipient_tax_id, recipient_legal_name, recipient_address, recipient_email,
			 issuer_snapshot, currency, subtotal_cents, discount_cents, iva_cents, total_cents, payment_method,
			 signed_xml, last_messages)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, '[]'::jsonb)
		RETURNING id
	`,
		inv.IssuerID, inv.Country, inv.Environment, invoicing.InvoiceStatusPending, inv.IssuedOn, inv.IssuedAt, inv.IssuedBy,
		inv.Recipient.TaxIDType, inv.Recipient.TaxID, inv.Recipient.LegalName, inv.Recipient.Address, inv.Recipient.Email,
		snapshot, inv.Currency, inv.SubtotalCents, inv.DiscountCents, inv.IVACents, inv.TotalCents, inv.PaymentMethod,
		prepared.SignedXML,
	).Scan(&id); err != nil {
		return nil, fmt.Errorf("insert invoice: %w", err)
	}

	for _, l := range inv.Lines {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO invoicing_invoice_lines
				(invoice_id, position, description, quantity_millionths, unit_price_cents, discount_cents, iva_rate, base_cents, iva_cents)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`, id, l.Position, l.Description, l.QuantityMillionths, l.UnitPriceCents, l.DiscountCents, l.IVARate, l.BaseCents, l.IVACents); err != nil {
			return nil, fmt.Errorf("insert invoice line %d: %w", l.Position, err)
		}
	}
	for _, f := range inv.AdditionalFields {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO invoicing_additional_fields (invoice_id, position, name, value)
			VALUES ($1, $2, $3, $4)
		`, id, f.Position, f.Name, f.Value); err != nil {
			return nil, fmt.Errorf("insert additional field %d: %w", f.Position, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO invoicing_invoices_ec (invoice_id, issuer_id, environment, cod_doc, estab, pto_emi, secuencial, access_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, id, inv.IssuerID, inv.Environment, in.CodDoc, in.Estab, in.PtoEmi, secuencial, prepared.AccessKey); err != nil {
		return nil, fmt.Errorf("insert ecuador invoice details: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create invoice: %w", err)
	}
	return r.GetInvoice(ctx, id)
}

// OutcomeUpdate is what an authority's answer changes on the invoice row.
type OutcomeUpdate struct {
	Status   invoicing.InvoiceStatus
	Messages []invoicing.AuthorityMessage
	// Authorization and AuthorizationXML are set only when Status is
	// authorized.
	Authorization    *invoicing.Authorization
	AuthorizationXML []byte
}

// ApplyOutcome writes the authority's latest answer onto the invoice: its
// status, the messages verbatim, and — when authorized — the authorization
// number, date and XML. The signed document is never touched here.
func (r *Repository) ApplyOutcome(ctx context.Context, invoiceID string, u OutcomeUpdate) error {
	messages, err := json.Marshal(messagesOrEmpty(u.Messages))
	if err != nil {
		return fmt.Errorf("marshal messages: %w", err)
	}
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin apply outcome: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE invoicing_invoices SET
			status = $2,
			last_messages = $3,
			authorization_xml = COALESCE($4, authorization_xml),
			updated_at = NOW()
		WHERE id = $1
	`, invoiceID, u.Status, messages, u.AuthorizationXML)
	if err != nil {
		return fmt.Errorf("apply outcome: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("apply outcome: invoice %s: %w", invoiceID, sql.ErrNoRows)
	}
	if u.Authorization != nil {
		if _, err := tx.ExecContext(ctx, `
			UPDATE invoicing_invoices_ec SET authorization_number = $2, authorization_date = $3
			WHERE invoice_id = $1
		`, invoiceID, u.Authorization.Number, u.Authorization.Date); err != nil {
			return fmt.Errorf("apply authorization: %w", err)
		}
	}
	return tx.Commit()
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

const invoiceColumns = `
	i.id, i.issuer_id, i.country, i.environment, i.status, i.issued_on, i.issued_at, i.issued_by,
	i.recipient_tax_id_type, i.recipient_tax_id, i.recipient_legal_name, i.recipient_address, i.recipient_email,
	i.issuer_snapshot, i.currency, i.subtotal_cents, i.discount_cents, i.iva_cents, i.total_cents, i.payment_method,
	i.signed_xml, i.authorization_xml, i.last_messages, i.created_at, i.updated_at,
	e.cod_doc, e.estab, e.pto_emi, e.secuencial, e.access_key, e.authorization_number, e.authorization_date`

const invoiceFrom = `
	FROM invoicing_invoices i
	JOIN invoicing_invoices_ec e ON e.invoice_id = i.id`

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

// ListInvoices reads a page of Tax Invoices newest first, without their
// lines, fields or attempts, and the total count.
func (r *Repository) ListInvoices(ctx context.Context, page, pageSize int) ([]InvoiceRow, int, error) {
	var total int
	if err := r.db.Pool.QueryRowContext(ctx, `SELECT COUNT(*) FROM invoicing_invoices`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count invoices: %w", err)
	}
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+invoiceColumns+invoiceFrom+`
		ORDER BY i.issued_at DESC, i.created_at DESC, i.id DESC
		LIMIT $1 OFFSET $2
	`, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list invoices: %w", err)
	}
	defer rows.Close()
	var out []InvoiceRow
	for rows.Next() {
		row, err := scanInvoice(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan invoice: %w", err)
		}
		out = append(out, *row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list invoices: %w", err)
	}
	return out, total, nil
}

func (r *Repository) loadInvoiceChildren(ctx context.Context, row *InvoiceRow) error {
	id := row.Invoice.ID
	lines, err := r.db.Pool.QueryContext(ctx, `
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

	fields, err := r.db.Pool.QueryContext(ctx, `
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

	attempts, err := r.db.Pool.QueryContext(ctx, `
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
		snapshot, messages []byte
		authNumber         sql.NullString
		authDate           sql.NullTime
	)
	if err := scanner.Scan(
		&inv.ID, &inv.IssuerID, &inv.Country, &inv.Environment, &inv.Status, &inv.IssuedOn, &inv.IssuedAt, &inv.IssuedBy,
		&inv.Recipient.TaxIDType, &inv.Recipient.TaxID, &inv.Recipient.LegalName, &inv.Recipient.Address, &inv.Recipient.Email,
		&snapshot, &inv.Currency, &inv.SubtotalCents, &inv.DiscountCents, &inv.IVACents, &inv.TotalCents, &inv.PaymentMethod,
		&inv.SignedXML, &inv.AuthorizationXML, &messages, &inv.CreatedAt, &inv.UpdatedAt,
		&row.Ecuador.CodDoc, &row.Ecuador.Estab, &row.Ecuador.PtoEmi, &row.Ecuador.Secuencial, &row.Ecuador.AccessKey,
		&authNumber, &authDate,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(snapshot, &inv.Issuer); err != nil {
		return nil, fmt.Errorf("decode issuer snapshot: %w", err)
	}
	if err := json.Unmarshal(messages, &inv.Messages); err != nil {
		return nil, fmt.Errorf("decode messages: %w", err)
	}
	if authNumber.Valid {
		row.Ecuador.Authorization = &invoicing.Authorization{Number: authNumber.String, Date: authDate.Time}
	}
	return &row, nil
}

func messagesOrEmpty(in []invoicing.AuthorityMessage) []invoicing.AuthorityMessage {
	if in == nil {
		return []invoicing.AuthorityMessage{}
	}
	return in
}
