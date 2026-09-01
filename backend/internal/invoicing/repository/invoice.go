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
// Issuer whether or not one exists yet. A Sale Invoice a reissue produced
// (#483) carries the factura it supersedes and the reissue's trail the same
// way; on every other document those are "" and nil, written as NULL.
func (r *Repository) OweInvoice(ctx context.Context, tx *sql.Tx, inv invoicing.Invoice) (string, error) {
	var id string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO invoicing_invoices
			(kind, country, status, ticket_sale_id, credits_invoice_id, credit_note_reason, iva_rate,
			 recipient_tax_id_type, recipient_tax_id, recipient_legal_name, recipient_address, recipient_email,
			 currency, subtotal_cents, discount_cents, iva_cents, total_cents, payment_method,
			 next_attempt_at, last_messages,
			 supersedes_invoice_id, reissued_by, reissued_at, reissue_note,
			 backfilled_by, backfilled_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid, NULLIF($6, ''), $7,
		        $8, $9, $10, $11, $12,
		        $13, $14, $15, $16, $17, $18,
		        $19, '[]'::jsonb,
		        NULLIF($20, '')::uuid, NULLIF($21, ''), $22, NULLIF($23, ''),
		        NULLIF($24, ''), $25)
		RETURNING id
	`,
		inv.Kind, inv.Country, invoicing.InvoiceStatusOwed, inv.TicketSaleID, inv.CreditsInvoiceID, inv.CreditNoteReason, inv.IVARate,
		inv.Recipient.TaxIDType, inv.Recipient.TaxID, inv.Recipient.LegalName, inv.Recipient.Address, inv.Recipient.Email,
		inv.Currency, inv.SubtotalCents, inv.DiscountCents, inv.IVACents, inv.TotalCents, inv.PaymentMethod,
		inv.NextAttemptAt,
		inv.SupersedesInvoiceID, inv.ReissuedBy, inv.ReissuedAt, inv.ReissueNote,
		inv.BackfilledBy, inv.BackfilledAt,
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
	// RecipientWarning says the answer carried the authority's warning
	// about the Recipient's Tax ID (#482). It is ORed onto the row, never
	// written over it: once raised the warning stands until the document
	// is superseded, whatever a later answer says.
	RecipientWarning bool
}

// ApplyOutcome writes the authority's latest answer onto the invoice: its
// status, the messages verbatim, when the Drainer looks again, and — when
// authorized — the authorization number, date and XML. The signed document
// is never touched here. attention_since is set when the answer parks the
// document and kept while it stays parked; any other answer clears it
// (#477). recipient_warning is raised when the answer says so and never
// lowered on the row itself (#482). The one write that clears a warning is
// here too, on ANOTHER row (#484, ADR 0061): an answer that authorizes a
// Credit Note is the moment the factura it credits stops declaring
// anything to the authority — whether a reissue's nota or a reversal's —
// so that factura's warning is cleared in the same transaction. Not at the
// reissue, whose Credit Note may yet die and hand the factura back; not at
// the corrected factura's authorization, which an annulled corrected
// factura or a reversed Sale never reaches. The corrected factura's own
// warning, if the answer carried one, is raised as any other.
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
			recipient_warning = recipient_warning OR $9,
			updated_at = NOW()
		WHERE id = $1 AND status = $8
	`, invoiceID, u.Status, messages, u.AuthorizationXML, u.NextAttemptAt, u.At, u.KeepNextAttemptAt, u.From, u.RecipientWarning)
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
	if u.Status == invoicing.InvoiceStatusAuthorized {
		if _, err := tx.ExecContext(ctx, `
			UPDATE invoicing_invoices f SET recipient_warning = FALSE, updated_at = NOW()
			FROM invoicing_invoices c
			WHERE c.id = $1 AND c.kind = 'credit_note' AND f.id = c.credits_invoice_id AND f.recipient_warning
		`, invoiceID); err != nil {
			return false, fmt.Errorf("clear credited recipient warning: %w", err)
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
//
// A DEAD CREDIT NOTE TAKES ITS CORRECTED FACTURA WITH IT (#484, ADR 0061).
// A Sale Invoice Reissue's corrected factura waits, owed and unsigned, for
// the Credit Note against the factura it supersedes to be authorized; an
// annulled Credit Note never will be, and a corrected factura issued after
// it would leave the Sale with two authorized facturas. So in the same
// transaction, an unsigned successor (owed, or parked unsignable) of the
// factura the annulled Credit Note credits is withdrawn — no number consumed, nothing sent, off
// the queue — carrying successorMessages as the platform's word on why. The
// superseded factura is then current again, credited by nothing live (the
// credited_by read skips dead Credit Notes), and reissuable. The
// withdrawn successor's id is returned, "" when there was none: a Credit
// Note owed by a reversal has no successor to take, and one whose
// successor a reversal already withdrew finds nothing to do.
func (r *Repository) AnnulInvoice(ctx context.Context, invoiceID, annulledBy string, annulledAt time.Time, successorMessages []invoicing.AuthorityMessage) (annulled bool, withdrawnSuccessorID string, err error) {
	encoded, err := json.Marshal(messagesOrEmpty(successorMessages))
	if err != nil {
		return false, "", fmt.Errorf("marshal messages: %w", err)
	}
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return false, "", fmt.Errorf("begin annul invoice: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
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
		return false, "", fmt.Errorf("annul invoice: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, "", fmt.Errorf("annul invoice: %w", err)
	}
	if n != 1 {
		return false, "", nil
	}
	err = tx.QueryRowContext(ctx, `
		UPDATE invoicing_invoices s SET
			status = $3,
			last_messages = $4,
			next_attempt_at = NULL,
			attention_since = NULL,
			updated_at = $5
		FROM invoicing_invoices n
		WHERE n.id = $1 AND n.kind = 'credit_note'
		  AND s.kind = 'sale' AND s.supersedes_invoice_id = n.credits_invoice_id
		  AND s.status IN ($2, $6) AND s.signed_xml IS NULL
		RETURNING s.id
	`, invoiceID, invoicing.InvoiceStatusOwed, invoicing.InvoiceStatusWithdrawn, encoded, annulledAt, invoicing.InvoiceStatusNeedsAttention).Scan(&withdrawnSuccessorID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, "", fmt.Errorf("withdraw the annulled credit note's successor: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, "", fmt.Errorf("commit annul invoice: %w", err)
	}
	return true, withdrawnSuccessorID, nil
}

// AbandonInvoice records that the Tax Authority never took the document and
// never will (#578, parent #575, ADR 0068): the status, who, when and the
// operator's optional note, AND NOTHING ELSE. The number, the clave de
// acceso, the signed XML and the authority's last messages stay exactly as
// they were — the document's issuance is not undone, it is accounted for —
// and the secuencial is never touched, so the abandoned number stays
// consumed and the sequence only moves forward. next_attempt_at is cleared
// so the Drainer never claims the row again, and attention_since with it.
//
// GUARDED ON THE THREE STATES ABANDON IS ALLOWED FROM, and on the row still
// being signed, so a second press, or a press racing a late AUTORIZADO that
// healed the row, finds nothing to update; the service reads the row back to
// say which. The three are needs_attention — where a Sale Invoice's refusal
// parks it — and rejected and not_authorized, where a manual document's
// refusals are recorded.
//
// The note is stored NULL when none was left, never an empty string, as the
// reissue's is (migration 102): "no note" and "an empty note" are the same
// thing, and only one of them should be representable.
func (r *Repository) AbandonInvoice(ctx context.Context, invoiceID, abandonedBy, note string, abandonedAt time.Time) (bool, error) {
	res, err := r.db.Pool.ExecContext(ctx, `
		UPDATE invoicing_invoices SET
			status = $2,
			abandoned_by = $3,
			abandoned_at = $4,
			abandon_note = NULLIF($5, ''),
			next_attempt_at = NULL,
			attention_since = NULL,
			updated_at = NOW()
		WHERE id = $1 AND status IN ($6, $7, $8) AND signed_xml IS NOT NULL
	`, invoiceID, invoicing.InvoiceStatusAbandoned, abandonedBy, abandonedAt, note,
		invoicing.InvoiceStatusNeedsAttention, invoicing.InvoiceStatusRejected, invoicing.InvoiceStatusNotAuthorized)
	if err != nil {
		return false, fmt.Errorf("abandon invoice: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("abandon invoice: %w", err)
	}
	return n == 1, nil
}

// liveSuccessorID is THE expression for "the document that stands in this
// one's place", stated once because two readers now rest on it: the list
// row's superseded marker (#486, ADR 0061) and the needs-attention queue's
// second half (#581), which is exactly "an abandoned document with no live
// successor". The queue must never re-derive it — two spellings of "live"
// would drift, and a Sale would then be both replaced and queued.
//
// LIVE MEANS NOT TERMINALLY DEAD (#579, ADR 0068); the invoiceFrom comment
// below spells the three deaths and why a successor that died supersedes
// nothing.
const liveSuccessorID = `(SELECT s.id FROM invoicing_invoices s WHERE s.supersedes_invoice_id = i.id AND s.status NOT IN ('withdrawn', 'annulled', 'abandoned') ORDER BY s.created_at DESC, s.id DESC LIMIT 1)`

const invoiceColumns = `
	i.id, i.kind, i.issuer_id, i.country, i.environment, i.status, i.issued_on, i.issued_at, i.issued_by,
	i.recipient_tax_id_type, i.recipient_tax_id, i.recipient_legal_name, i.recipient_address, i.recipient_email,
	i.issuer_snapshot, i.currency, i.subtotal_cents, i.discount_cents, i.iva_cents, i.total_cents, i.payment_method,
	i.signed_xml, i.authorization_xml, i.last_messages,
	i.ticket_sale_id, ts.confirmation_ref, i.credits_invoice_id, i.credit_note_reason, i.iva_rate, i.delivered_at, i.next_attempt_at,
	(SELECT c.id FROM invoicing_invoices c WHERE c.credits_invoice_id = i.id AND c.status NOT IN ('withdrawn', 'annulled', 'abandoned') ORDER BY c.created_at DESC, c.id DESC LIMIT 1),
	i.attention_since, i.annulled_by, i.annulled_at, i.recipient_warning,
	i.abandoned_by, i.abandoned_at, i.abandon_note,
	i.supersedes_invoice_id,
	` + liveSuccessorID + `,
	rr.reissued_by, rr.reissued_at, rr.reissue_note,
	i.backfilled_by, i.backfilled_at,
	i.created_at, i.updated_at,
	e.cod_doc, e.estab, e.pto_emi, e.secuencial, e.access_key, e.authorization_number, e.authorization_date`

// The Ecuador detail row is absent until signing, and a Ticket Sale only a
// Sale-side document has; both joins are LEFT for that reason. The Sale's
// Confirmation reference is read beside the row rather than copied onto it:
// it is the Sale's fact, and the operator surfaces want it on every read.
// The Credit Note crediting a Sale Invoice is read the same way (#476): the
// link is stored once, on the Credit Note, and walked back here — taking
// the newest LIVE one (#484): a Credit Note that died — withdrawn,
// annulled, or abandoned because the authority refused its number (#578,
// ADR 0068) — credits nothing, and a factura it alone names is uncredited,
// current again and reissuable; the dead Credit Note stays on file beside
// it, listed with the Sale's documents and readable by its own id.
//
// The Sale Invoice Reissue's links are read the same way again (#483, ADR
// 0061). supersedes_invoice_id is stored once, on the corrected factura;
// the superseded factura's "superseded by" is walked back from it, taking
// the live successor, of which the schema allows one (migration 120).
//
// LIVE MEANS NOT TERMINALLY DEAD (#579, parent #575, ADR 0068), and the
// three deaths are spelled here exactly as they are in the partial unique
// index that admits one of them: withdrawn — never sent, and never will be;
// annulled — held by the authority and disowned by hand at its portal;
// abandoned — sent, never held, never a legal document. A successor that
// died any of those ways supersedes nothing: the factura it corrected is
// current again, its "superseded by" is null, and nothing about the Sale is
// blocked by a document that no longer stands. Anything else a successor
// can be — owed, pending, parked, refused with a remedy, authorized — is
// live and holds the slot, so one Sale never carries two competing
// corrected facturas. This was `<> 'withdrawn'` until #579, which is why a
// Sale whose corrected factura died at the authority was unreachable: the
// old factura answered INVOICE_SUPERSEDED, naming a ghost, and the dead one
// answered INVOICE_NOT_AUTHORIZED (#480).
// The reissue's trail (who, when, the note) is stored on the corrected
// factura too, and the lateral join reads it beside every document the
// reissue concerns: the corrected factura's own, the superseded factura's
// live successor, and — for a reissue Credit Note — the successor of the
// factura it credits. A document's own trail wins over one that later
// superseded it.
const invoiceFrom = `
	FROM invoicing_invoices i
	LEFT JOIN invoicing_invoices_ec e ON e.invoice_id = i.id
	LEFT JOIN ticket_sales ts ON ts.id = i.ticket_sale_id
	LEFT JOIN LATERAL (
		SELECT r.reissued_by, r.reissued_at, r.reissue_note
		FROM invoicing_invoices r
		WHERE r.supersedes_invoice_id IS NOT NULL
		  AND (r.id = i.id
		       OR (r.status NOT IN ('withdrawn', 'annulled', 'abandoned')
		           AND (r.supersedes_invoice_id = i.id
		                OR (i.kind = 'credit_note' AND i.credit_note_reason = 'reissue'
		                    AND r.supersedes_invoice_id = i.credits_invoice_id))))
		ORDER BY (r.id = i.id) DESC, r.created_at DESC, r.id DESC
		LIMIT 1
	) rr ON TRUE`

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

// InvoiceFilter is what narrows the invoices list (#477, #482). The zero
// value is every document.
type InvoiceFilter struct {
	// Kind narrows the page to one document kind; "" is every kind.
	Kind invoicing.DocumentKind
	// Status narrows the page to one status (#578, ADR 0068); "" is every
	// status. Added so that every number the platform has given up on can be
	// audited — `abandoned` is the reason it exists — but written for the
	// whole vocabulary rather than for that one word, since a filter that
	// answers one status and not the eight beside it is a surface that has
	// to be extended again by the next person who needs one.
	Status invoicing.InvoiceStatus
	// RecipientWarning narrows the page to the documents carrying a
	// Recipient Warning.
	RecipientWarning bool
}

// ListInvoices reads a page of Tax Invoices newest first — by emission
// instant once signed, by owing instant before — without their lines,
// fields or attempts, and the total count under the same filter.
func (r *Repository) ListInvoices(ctx context.Context, filter InvoiceFilter, page, pageSize int) ([]InvoiceRow, int, error) {
	var total int
	if err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM invoicing_invoices
		WHERE ($1 = '' OR kind = $1) AND ($2 = '' OR status = $2) AND (NOT $3 OR recipient_warning)
	`, string(filter.Kind), string(filter.Status), filter.RecipientWarning).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count invoices: %w", err)
	}
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+invoiceColumns+invoiceFrom+`
		WHERE ($1 = '' OR i.kind = $1) AND ($2 = '' OR i.status = $2) AND (NOT $3 OR i.recipient_warning)
		ORDER BY COALESCE(i.issued_at, i.created_at) DESC, i.created_at DESC, i.id DESC
		LIMIT $4 OFFSET $5
	`, string(filter.Kind), string(filter.Status), filter.RecipientWarning, pageSize, (page-1)*pageSize)
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

// needsAttentionWhere is THE needs-attention queue's membership rule, and
// it is A UNION OF TWO CONDITIONS rather than a status equality (#581,
// parent #575, ADR 0068). It is written once, here, because the list and
// the count must be incapable of disagreeing: the badge an operator watches
// daily and the page it opens are one claim.
//
//	needs_attention                          — a document parked for an
//	                                           operator: refused with a
//	                                           remedy, unanswered and still
//	                                           polled, or unsignable.
//	OR abandoned, sale-kind, unreplaced,     — a Ticket Sale that stands and
//	   on a Sale that still stands             has no factura, and nothing
//	                                           yet owes it one.
//
// WHY THE SECOND HALF IS HERE AT ALL. Abandon (#578) and Issue again (#580)
// are deliberately two presses, so a Sale can sit abandoned-and-unreplaced
// between them — its buyer holding no valid tax document — and until this,
// nothing on any surface said so, because `abandoned` is not
// `needs_attention`. The queue that already means "unfinished invoicing
// work" widens to say what its name promises, rather than a second badge
// beside it: two badges meaning the same thing is how an operator learns to
// ignore one.
//
// IT SELF-CLEARS, which is what makes a union safe here. The moment Issue
// again owes a replacement, the abandoned document has a live successor and
// drops out with no second act to remember.
//
// EACH CLAUSE OF THE SECOND HALF IS LOAD-BEARING:
//
//   - sale-kind. A Credit Note and a manual Tax Invoice are never issued
//     again — #580 refuses both by their own codes, and a manual document is
//     typed again by hand — so a row for either could never be cleared by
//     any press the platform offers, and a permanent row is not a queue.
//   - no live successor, read from liveSuccessorID and NEVER re-derived.
//     It is #579's live: a replacement that is itself withdrawn, annulled or
//     abandoned stands for nothing, so its predecessor's Sale is unreplaced
//     again and returns to the queue, where the operator can still act.
//   - the Sale still stands. Income that no longer stands is never declared
//     at all, so a reversed Sale is owed nothing; it is also the fact Issue
//     again refuses on (INVOICE_SALE_REVERSED), so a queued reversed Sale
//     would be an entry nobody could clear. `ts` is LEFT-joined, so a
//     document with no Ticket Sale answers NULL here and is excluded, which
//     is the same answer as the kind clause and deliberately redundant with
//     it.
const needsAttentionWhere = `
	i.status = 'needs_attention'
	OR (
		i.status = 'abandoned'
		AND i.kind = 'sale'
		AND ts.status <> 'reversed'
		AND ` + liveSuccessorID + ` IS NULL
	)`

// needsAttentionOrder is the composition of the queue's TWO ORDERINGS, and
// the reason it needed a second sort key (#581): `attention_since` is NULL
// on an abandoned row — the abandonment clears it in the same write that
// takes the document off the Drainer — and `abandoned_at` is the instant it
// entered the queue by the other door.
//
// They compose into ONE CLOCK, "since when has this needed an operator",
// and the two halves therefore INTERLEAVE. A document parked at noon and
// abandoned at three sorts behind one parked at one: its wait AS A PARKED
// DOCUMENT ended when the operator acted on it, and what it waits for now —
// a replacement — has been outstanding only since three. Sorting all of one
// status ahead of the other was rejected: the badge's oldest entry would
// then not be the oldest work, which is the one thing the ordering is for.
//
// Ties, which one drain round can produce, break on the row's own age as
// they always have. Should both instants somehow be NULL, Postgres sorts
// NULLs last on ASC and the row lands at the back rather than anywhere
// undefined.
const needsAttentionOrder = `ORDER BY COALESCE(i.attention_since, i.abandoned_at) ASC, i.created_at ASC, i.id ASC`

// needsAttentionFrom is the least the membership rule can be decided from:
// the row and the Ticket Sale it is about. The count reads through it and
// the list through invoiceFrom, whose first two joins are these two; every
// other join invoiceFrom adds is a LEFT one that yields at most one row, so
// the two can no more disagree than the one WHERE clause they share can.
const needsAttentionFrom = `
	FROM invoicing_invoices i
	LEFT JOIN ticket_sales ts ON ts.id = i.ticket_sale_id`

// ListNeedsAttention reads a page of the needs-attention queue — documents
// parked needs_attention, of every kind, AND abandoned Sale Invoices with
// no live successor whose Ticket Sale still stands (needsAttentionWhere) —
// LONGEST WAITING FIRST by the instant each entered the queue
// (needsAttentionOrder), and how many there are in all (#477, #581).
func (r *Repository) ListNeedsAttention(ctx context.Context, page, pageSize int) ([]InvoiceRow, int, error) {
	total, err := r.CountNeedsAttention(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+invoiceColumns+invoiceFrom+`
		WHERE `+needsAttentionWhere+`
		`+needsAttentionOrder+`
		LIMIT $1 OFFSET $2
	`, pageSize, (page-1)*pageSize)
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

// CountNeedsAttention counts the needs-attention queue: the Operator
// Dashboard's badge, and exactly what the queue lists — the same
// needsAttentionWhere, over the joins that rule needs and no others.
func (r *Repository) CountNeedsAttention(ctx context.Context) (int, error) {
	var n int
	if err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*) `+needsAttentionFrom+`
		WHERE `+needsAttentionWhere+`
	`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count needs attention: %w", err)
	}
	return n, nil
}

// CountRecipientWarnings counts the documents carrying a Recipient Warning
// (#482): the Operator Dashboard's second badge, beside the needs_attention
// count, and exactly what the list's filter finds.
func (r *Repository) CountRecipientWarnings(ctx context.Context) (int, error) {
	var n int
	if err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM invoicing_invoices WHERE recipient_warning
	`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count recipient warnings: %w", err)
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
		abandonedBy, abandonNote           sql.NullString
		abandonedAt                        sql.NullTime
		supersedesID, supersededByID       sql.NullString
		reissuedBy, reissueNote            sql.NullString
		reissuedAt                         sql.NullTime
		backfilledBy                       sql.NullString
		backfilledAt                       sql.NullTime
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
		&attentionSince, &annulledBy, &annulledAt, &inv.RecipientWarning,
		&abandonedBy, &abandonedAt, &abandonNote,
		&supersedesID, &supersededByID, &reissuedBy, &reissuedAt, &reissueNote,
		&backfilledBy, &backfilledAt,
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
	inv.AbandonedBy = abandonedBy.String
	inv.AbandonNote = abandonNote.String
	if abandonedAt.Valid {
		t := abandonedAt.Time
		inv.AbandonedAt = &t
	}
	inv.SupersedesInvoiceID = supersedesID.String
	inv.SupersededByInvoiceID = supersededByID.String
	inv.ReissuedBy = reissuedBy.String
	inv.ReissueNote = reissueNote.String
	if reissuedAt.Valid {
		t := reissuedAt.Time
		inv.ReissuedAt = &t
	}
	inv.BackfilledBy = backfilledBy.String
	if backfilledAt.Valid {
		t := backfilledAt.Time
		inv.BackfilledAt = &t
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
