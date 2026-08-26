package service

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
	"github.com/peter/ticket_pos/backend/internal/invoicing/sri"
)

// The issue flow (#454, parent #450): a Platform Operator presses Issue, and
// in the usual case a few seconds later they are looking at an authorized
// factura.
//
// TWO HALVES, ONE REQUEST. First, in one transaction, the next secuencial is
// allocated, the clave de acceso computed, the Issuer and Recipient
// snapshotted, the document built and signed, and the invoice inserted as
// pending — so a number is consumed exactly when Issue is pressed and never
// for an abandoned form. Then, still in the request, the document goes to the
// authority and the outcome is polled within a budget. Whatever the second
// half does, the first half has happened: a pending invoice with its number
// kept is the worst case, never a lost document and never a double issue.
//
// Everything that can refuse does so BEFORE the transaction — no Issuer, no
// certificate, an Issuer the schema will not take, an invoice the builder will
// not take — so that a refusal never costs a number.

// Default poll schedule: on RECIBIDA, ask autorización at roughly 2 s, 4 s
// and 8 s within a ~15 s budget. Tests inject shorter ones.
var (
	DefaultPollDelays = []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}
	DefaultPollBudget = 15 * time.Second
)

// WithClock replaces the clock the emission date and signing time are taken
// from (tests).
func (s *Service) WithClock(now func() time.Time) *Service {
	s.clock = now
	return s
}

// WithTaxAuthority replaces the per-environment adapter constructor: the
// base-URL override in local development and the fake SRI in tests reach
// the service through here.
func (s *Service) WithTaxAuthority(factory func(invoicing.Environment) invoicing.TaxAuthority) *Service {
	s.authority = factory
	return s
}

// WithPollSchedule replaces the delays between autorización polls and the
// overall budget one issue request may spend waiting on the authority.
func (s *Service) WithPollSchedule(delays []time.Duration, budget time.Duration) *Service {
	s.pollDelays = delays
	s.pollBudget = budget
	return s
}

// IssueInput is a Tax Invoice as the operator entered it, already validated
// for shape by the handler: the Tax ID is a Tax ID, there is at least one
// line, and no more than the allowed additional fields.
type IssueInput struct {
	IssuedBy         string
	Recipient        invoicing.Recipient
	Lines            []LineInput
	PaymentMethod    string
	AdditionalFields []invoicing.AdditionalField
}

// LineInput is one line as entered.
type LineInput struct {
	Description        string
	QuantityMillionths int64
	UnitPriceCents     int64
	DiscountCents      int64
	IVARate            invoicing.IVARate
}

// IssueInvoice issues a Tax Invoice to the Ecuador Issuer's authority and
// returns it as it stands when the budget is spent: authorized in the usual
// case, otherwise not authorized, rejected or pending, with the authority's
// messages verbatim.
func (s *Service) IssueInvoice(ctx context.Context, in IssueInput) (*InvoiceDetail, error) {
	issuer, err := s.repo.GetEcuadorIssuer(ctx)
	if err != nil {
		return nil, err
	}
	if issuer == nil {
		return nil, invoicing.ErrIssuerNotFound()
	}
	cert, err := s.OpenEcuadorSigningKey(ctx)
	if err != nil {
		return nil, err
	}

	now := s.clock()
	snapshot := invoicing.SnapshotOf(issuer.Details)
	env := issuer.Issuer.Environment
	base := sri.Factura{
		Environment:   sri.AmbienteFor(env),
		Issuer:        sri.IssuerFromSnapshot(snapshot),
		IssuedOn:      now,
		PaymentMethod: sri.PaymentMethod(in.PaymentMethod),
	}

	// Pre-flight one: can a factura be built from this Issuer at all? Probed
	// with a canned Recipient and line so that a failure here is the Issuer's
	// and nothing else's.
	if err := s.dryRun(base, probeRecipient, probeLines, nil); err != nil {
		return nil, invoicing.ErrIssuerIncomplete(reason(err))
	}

	// Pre-flight two: can THIS invoice be built? Same builder, real input, a
	// placeholder number. Whatever it refuses, it refuses before a number is
	// consumed.
	recipient, err := sriRecipient(in.Recipient)
	if err != nil {
		return nil, invoicing.ErrInvoiceInvalid(reason(err))
	}
	lines, err := sriLines(in.Lines)
	if err != nil {
		return nil, invoicing.ErrInvoiceInvalid(reason(err))
	}
	fields := sriFields(in.AdditionalFields)
	if err := s.dryRun(base, recipient, lines, fields); err != nil {
		return nil, invoicing.ErrInvoiceInvalid(reason(err))
	}
	totals, err := sri.ComputeTotals(lines)
	if err != nil {
		return nil, invoicing.ErrInvoiceInvalid(reason(err))
	}

	inv := invoicing.Invoice{
		IssuerID:      issuer.Issuer.ID,
		Country:       issuer.Issuer.Country,
		Environment:   env,
		Status:        invoicing.InvoiceStatusPending,
		IssuedOn:      guayaquilDate(now),
		IssuedAt:      now,
		IssuedBy:      in.IssuedBy,
		Recipient:     in.Recipient,
		Issuer:        &snapshot,
		Currency:      "USD",
		SubtotalCents: totals.SubtotalCents,
		DiscountCents: totals.DiscountCents,
		IVACents:      totals.IVACents,
		TotalCents:    totals.TotalCents,
		PaymentMethod: string(base.PaymentMethod),
	}
	for i, l := range in.Lines {
		lt := totals.Lines[i]
		inv.Lines = append(inv.Lines, invoicing.InvoiceLine{
			Position:           i + 1,
			Description:        l.Description,
			QuantityMillionths: l.QuantityMillionths,
			UnitPriceCents:     l.UnitPriceCents,
			DiscountCents:      l.DiscountCents,
			IVARate:            l.IVARate,
			BaseCents:          lt.BaseCents,
			IVACents:           lt.IVACents,
		})
	}
	for i, f := range in.AdditionalFields {
		inv.AdditionalFields = append(inv.AdditionalFields, invoicing.AdditionalField{Position: i + 1, Name: f.Name, Value: f.Value})
	}

	row, err := s.repo.CreateInvoice(ctx, repository.NewInvoice{
		Invoice: inv,
		CodDoc:  sri.DocumentTypeFactura,
		Estab:   snapshot.Establecimiento,
		PtoEmi:  snapshot.PuntoEmision,
	}, numberAndSign(facturaParts{base: base, recipient: recipient, lines: lines, fields: fields}, cert, now))
	if err != nil {
		return nil, err
	}
	s.logger.Info("invoicing: tax invoice issued",
		"invoice_id", row.Invoice.ID, "environment", env, "secuencial", row.Ecuador.Secuencial, "issued_by", in.IssuedBy)

	s.submitAndPoll(ctx, row, nil)
	return s.GetInvoice(ctx, row.Invoice.ID)
}

// facturaParts is a factura minus its number: everything the builder needs
// that is known before a secuencial is allocated. The manual issue and the
// Drainer (#474) both assemble one, dry-run it, and hand it to numberAndSign
// inside the transaction that allocates the number.
type facturaParts struct {
	base      sri.Factura
	recipient sri.Recipient
	lines     []sri.Line
	fields    []sri.AdditionalField
}

// numberAndSign is the prepare callback both signing writes run with the
// secuencial they just allocated: the clave de acceso is computed, the
// factura built and signed, and the two returned for storage. Any failure
// rolls the allocation back with it.
func numberAndSign(parts facturaParts, cert *sri.Certificate, now time.Time) func(secuencial int64) (*repository.PreparedInvoice, error) {
	return func(secuencial int64) (*repository.PreparedInvoice, error) {
		sequential, err := sri.FormatSequential(secuencial)
		if err != nil {
			return nil, err
		}
		numericCode, err := sri.RandomNumericCode()
		if err != nil {
			return nil, err
		}
		base := parts.base
		key, err := sri.NewAccessKey(sri.AccessKeyInput{
			IssuedOn:      now,
			DocumentType:  sri.DocumentTypeFactura,
			RUC:           base.Issuer.RUC,
			Environment:   base.Environment,
			Establishment: base.Issuer.Establishment,
			EmissionPoint: base.Issuer.EmissionPoint,
			Sequential:    sequential,
			NumericCode:   numericCode,
		})
		if err != nil {
			return nil, err
		}
		f := base
		f.AccessKey = key
		f.Sequential = sequential
		f.Recipient = parts.recipient
		f.Lines = parts.lines
		f.AdditionalFields = parts.fields
		built, err := sri.BuildFactura(f)
		if err != nil {
			return nil, err
		}
		signed, err := sri.Sign(built.XML, cert, sri.SignOptions{SigningTime: now})
		if err != nil {
			return nil, err
		}
		return &repository.PreparedInvoice{AccessKey: key, SignedXML: signed}, nil
	}
}

// submitAndPoll is the second half: hand the document to the authority and,
// if it was received, ask for the outcome on the poll schedule until it is
// settled or the budget is spent. Every call is an attempts row. A failure
// anywhere leaves the invoice pending with its number kept.
//
// #455's Check status is one query without the submit (CheckInvoice); its
// Resend is this function over a re-signed document, with onReceived (may be
// nil) called once the authority has taken it — where the document on file
// is replaced.
func (s *Service) submitAndPoll(ctx context.Context, row *repository.InvoiceRow, onReceived func(context.Context, invoicing.Outcome)) {
	ctx, cancel := context.WithTimeout(ctx, s.pollBudget)
	defer cancel()
	authority := s.authority(row.Invoice.Environment)
	id := row.Invoice.ID

	outcome, err := s.attempt(ctx, id, invoicing.AttemptSubmit, func() (invoicing.Outcome, error) {
		return authority.Submit(ctx, invoicing.PreparedDocument{AuthorityReference: row.Ecuador.AccessKey, Body: row.Invoice.SignedXML})
	})
	if err != nil {
		return
	}
	if outcome.State != invoicing.OutcomeReceived {
		s.applyOutcome(ctx, row, outcome)
		return
	}
	// Received: the messages so far (warnings, or the 43/70 "it is there")
	// are worth keeping while it stays pending.
	s.applyOutcome(ctx, row, outcome)
	if onReceived != nil {
		onReceived(ctx, outcome)
	}

	for _, delay := range s.pollDelays {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
		outcome, err := s.attempt(ctx, id, invoicing.AttemptQuery, func() (invoicing.Outcome, error) {
			return authority.QueryOutcome(ctx, row.Ecuador.AccessKey)
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		if outcome.State == invoicing.OutcomeReceived {
			continue
		}
		s.applyOutcome(ctx, row, outcome)
		return
	}
}

// attempt makes one call to the authority and writes it to the ledger
// whatever happens. The ledger write is detached from the request's
// cancellation: a budget that ran out is exactly the kind of thing the
// ledger exists to show.
func (s *Service) attempt(ctx context.Context, invoiceID string, op invoicing.AttemptOperation, call func() (invoicing.Outcome, error)) (invoicing.Outcome, error) {
	started := s.clock()
	wall := time.Now()
	outcome, err := call()
	a := invoicing.Attempt{
		InvoiceID: invoiceID,
		Operation: op,
		StartedAt: started,
		Duration:  time.Since(wall),
		Messages:  outcome.Messages,
		Outcome:   string(outcome.State),
	}
	if err != nil {
		a.Outcome = invoicing.AttemptOutcomeError
		a.Error = err.Error()
		s.logger.Warn("invoicing: tax authority call failed", "invoice_id", invoiceID, "operation", op, "error", err)
	}
	if rerr := s.repo.RecordAttempt(context.WithoutCancel(ctx), a); rerr != nil {
		s.logger.Error("invoicing: could not record attempt", "invoice_id", invoiceID, "error", rerr)
	}
	return outcome, err
}

// applyOutcome maps the authority's verdict onto the invoice's status.
//
// A MANUAL DOCUMENT AND A SALE INVOICE READ THE SAME VERDICT DIFFERENTLY
// (#474, ADR 0060). Authorized is authorized for both. A definite refusal
// leaves a manual document not_authorized or rejected, for the operator who
// pressed Issue to read and act on; it parks a Sale Invoice needs_attention,
// the one word the Operator Dashboard's queue is built on, with the
// authority's messages beside it. An undecided answer leaves a manual
// document pending until the operator checks; it leaves a Sale Invoice
// pending AND due again on the ladder — or needs_attention-still-polled once
// 24 hours have passed since signing without a definite answer. And an
// authorized Sale Invoice is due again at once, for its delivery (#475);
// an authorized manual document is the operator's to hand over.
func (s *Service) applyOutcome(ctx context.Context, row *repository.InvoiceRow, o invoicing.Outcome) {
	inv := &row.Invoice
	u := repository.OutcomeUpdate{Messages: o.Messages}
	switch o.State {
	case invoicing.OutcomeAuthorized:
		u.Status = invoicing.InvoiceStatusAuthorized
		u.Authorization = &invoicing.Authorization{Number: o.AuthorizationNumber, Date: o.AuthorizationDate}
		u.AuthorizationXML = o.AuthorityXML
	case invoicing.OutcomeNotAuthorized:
		u.Status = invoicing.InvoiceStatusNotAuthorized
	case invoicing.OutcomeRejected:
		u.Status = invoicing.InvoiceStatusRejected
	default:
		u.Status = invoicing.InvoiceStatusPending
	}
	if inv.Kind != invoicing.DocumentKindManual {
		switch u.Status {
		case invoicing.InvoiceStatusAuthorized:
			// Due at once, for delivery (#475): the Drainer mails it in this
			// round or, when the round was an operator's Check status, on
			// its next tick. MarkDelivered clears it.
			now := s.clock()
			u.NextAttemptAt = &now
		case invoicing.InvoiceStatusNotAuthorized, invoicing.InvoiceStatusRejected:
			u.Status = invoicing.InvoiceStatusNeedsAttention
		case invoicing.InvoiceStatusPending:
			now := s.clock()
			u.Status = undecidedStatus(inv, now)
			next := now.Add(ladderDelay(now.Sub(inv.IssuedAt)))
			u.NextAttemptAt = &next
		}
	}
	if err := s.repo.ApplyOutcome(context.WithoutCancel(ctx), inv.ID, u); err != nil {
		s.logger.Error("invoicing: could not apply outcome", "invoice_id", inv.ID, "status", u.Status, "error", err)
	}
}

// dryRun builds — never signs, never stores — a factura from the pieces with
// a placeholder number, to learn whether the builder would refuse it.
func (s *Service) dryRun(base sri.Factura, recipient sri.Recipient, lines []sri.Line, fields []sri.AdditionalField) error {
	const sequential = "000000001"
	key, err := sri.NewAccessKey(sri.AccessKeyInput{
		IssuedOn:      base.IssuedOn,
		DocumentType:  sri.DocumentTypeFactura,
		RUC:           base.Issuer.RUC,
		Environment:   base.Environment,
		Establishment: base.Issuer.Establishment,
		EmissionPoint: base.Issuer.EmissionPoint,
		Sequential:    sequential,
		NumericCode:   "00000000",
	})
	if err != nil {
		return err
	}
	f := base
	f.AccessKey = key
	f.Sequential = sequential
	f.Recipient = recipient
	f.Lines = lines
	f.AdditionalFields = fields
	_, err = sri.BuildFactura(f)
	return err
}

// probeCedula is a syntactically valid cédula — ten digits, province 17,
// correct check digit — and not a real person: it exists only so the probe
// passes the Recipient rules and can find nothing wrong but the Issuer.
const probeCedula = "1710034065"

// The canned Recipient and line the Issuer probe uses: valid by every rule,
// so the only thing the probe can find wrong is the Issuer.
var (
	probeRecipient = sri.Recipient{IDType: sri.RecipientIDCedula, ID: probeCedula, LegalName: "PROBE"}
	probeLines     = []sri.Line{{Description: "probe", Quantity: sri.QuantityFromInt(1), UnitPriceCents: 100, IVA: sri.IVACode15}}
)

func sriRecipient(r invoicing.Recipient) (sri.Recipient, error) {
	idType, err := sri.RecipientIDTypeFromTaxIDType(r.TaxIDType)
	if err != nil {
		return sri.Recipient{}, err
	}
	return sri.Recipient{IDType: idType, ID: r.TaxID, LegalName: r.LegalName, Address: r.Address, Email: r.Email}, nil
}

func sriLines(in []LineInput) ([]sri.Line, error) {
	out := make([]sri.Line, 0, len(in))
	for _, l := range in {
		code, err := sri.IVACodeFor(l.IVARate)
		if err != nil {
			return nil, err
		}
		out = append(out, sri.Line{
			Description:    l.Description,
			Quantity:       sri.Quantity(l.QuantityMillionths),
			UnitPriceCents: l.UnitPriceCents,
			DiscountCents:  l.DiscountCents,
			IVA:            code,
		})
	}
	return out, nil
}

func sriFields(in []invoicing.AdditionalField) []sri.AdditionalField {
	out := make([]sri.AdditionalField, 0, len(in))
	for _, f := range in {
		out = append(out, sri.AdditionalField{Name: f.Name, Value: f.Value})
	}
	return out
}

// reason strips the builder's "sri: invalid factura: " prefix so the
// operator reads what was wrong and not which package said so.
func reason(err error) string {
	msg := err.Error()
	msg = strings.TrimPrefix(msg, sri.ErrInvalidFactura.Error()+": ")
	msg = strings.TrimPrefix(msg, sri.ErrInvalidAccessKey.Error()+": ")
	return msg
}

// guayaquilDate is the emission date: the calendar day in Ecuador of the
// instant Issue was pressed, as a date-only value.
func guayaquilDate(now time.Time) time.Time {
	y, m, d := now.In(sri.Guayaquil).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// PreviewTotals does the factura arithmetic for a set of lines without
// issuing anything: what the form shows as the operator types, and the same
// numbers the document will carry.
func (s *Service) PreviewTotals(lines []LineInput) (*TotalsView, error) {
	sl, err := sriLines(lines)
	if err != nil {
		return nil, invoicing.ErrInvoiceInvalid(reason(err))
	}
	totals, err := sri.ComputeTotals(sl)
	if err != nil {
		return nil, invoicing.ErrInvoiceInvalid(reason(err))
	}
	view := totalsView(totals)
	return &view, nil
}

// GetInvoice returns one Tax Invoice in full.
func (s *Service) GetInvoice(ctx context.Context, id string) (*InvoiceDetail, error) {
	row, err := s.repo.GetInvoice(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, invoicing.ErrInvoiceNotFound()
	}
	return invoiceDetailView(row), nil
}

// ListInvoices returns a page of Tax Invoices, newest first.
func (s *Service) ListInvoices(ctx context.Context, page, pageSize int) (*InvoiceList, error) {
	rows, total, err := s.repo.ListInvoices(ctx, page, pageSize)
	if err != nil {
		return nil, err
	}
	items := make([]InvoiceListItem, 0, len(rows))
	for i := range rows {
		items = append(items, invoiceListItem(&rows[i]))
	}
	return &InvoiceList{
		Data: items,
		InvoicePagination: InvoicePagination{
			Page:       page,
			PageSize:   pageSize,
			Total:      total,
			TotalPages: int(math.Ceil(float64(total) / float64(pageSize))),
		},
	}, nil
}

// ---- views ------------------------------------------------------------

// InvoicePagination is the ADR-0006 page envelope.
type InvoicePagination struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// RecipientView is the Recipient as recorded on the invoice.
type RecipientView struct {
	TaxIDType string `json:"tax_id_type"`
	TaxID     string `json:"tax_id"`
	LegalName string `json:"legal_name"`
	Address   string `json:"address"`
	Email     string `json:"email"`
}

// InvoiceListItem is one row of the invoices list.
//
// THE ISSUE FACTS ARE NULL UNTIL SIGNED (#473). An owed Sale Invoice has no
// number, no environment, no emission date and no signer, and the list says
// null rather than "" so a reader can tell "not yet" from "blank". A manual
// Tax Invoice is signed at birth and never null here.
type InvoiceListItem struct {
	ID string `json:"id"`
	// Kind is why the document exists: manual, sale or credit_note.
	Kind    string `json:"kind"`
	Country string `json:"country"`
	// Environment is the authority environment the document was signed
	// under; null until signed.
	Environment *string `json:"environment"`
	Status      string  `json:"status"`
	// Number is the document number as printed: estab-ptoEmi-secuencial,
	// e.g. 001-001-000000012. Null until signed.
	Number *string `json:"number"`
	// IssuedOn is the emission date, YYYY-MM-DD in the Issuer's country;
	// IssuedAt the instant; IssuedBy the operator (or the Drainer). All null
	// until signed.
	IssuedOn  *string       `json:"issued_on"`
	IssuedAt  *time.Time    `json:"issued_at"`
	IssuedBy  *string       `json:"issued_by"`
	Recipient RecipientView `json:"recipient"`
	// TicketSaleID and SaleConfirmationRef name the Ticket Sale a sale
	// document or credit note is about; null on a manual document.
	TicketSaleID        *string `json:"ticket_sale_id"`
	SaleConfirmationRef *string `json:"sale_confirmation_ref"`
	TotalCents          int64   `json:"total_cents"`
	Currency            string  `json:"currency"`
}

// InvoiceList is the ADR-0006 nested envelope for the list.
type InvoiceList struct {
	Data              []InvoiceListItem `json:"data"`
	InvoicePagination InvoicePagination `json:"pagination"`
}

// LineView is one line as recorded, with its arithmetic.
type LineView struct {
	Position    int    `json:"position"`
	Description string `json:"description"`
	// Quantity is a decimal string with up to six decimals ("1", "2.5").
	Quantity       string `json:"quantity"`
	UnitPriceCents int64  `json:"unit_price_cents"`
	DiscountCents  int64  `json:"discount_cents"`
	IVARate        string `json:"iva_rate"`
	RatePercent    int    `json:"rate_percent"`
	BaseCents      int64  `json:"base_cents"`
	IVACents       int64  `json:"iva_cents"`
}

// AdditionalFieldView is one operator-entered name/value pair.
type AdditionalFieldView struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// RateTotalView is the subtotal for one IVA rate.
type RateTotalView struct {
	IVARate     string `json:"iva_rate"`
	RatePercent int    `json:"rate_percent"`
	BaseCents   int64  `json:"base_cents"`
	IVACents    int64  `json:"iva_cents"`
}

// TotalsView is the invoice's arithmetic in cents.
type TotalsView struct {
	ByRate        []RateTotalView `json:"by_rate"`
	SubtotalCents int64           `json:"subtotal_cents"`
	DiscountCents int64           `json:"discount_cents"`
	IVACents      int64           `json:"iva_cents"`
	TotalCents    int64           `json:"total_cents"`
}

// AuthorityMessageView is one message from the authority, verbatim.
type AuthorityMessageView = invoicing.AuthorityMessage

// EcuadorInvoiceView is the SRI's numbering and authorization of the invoice.
type EcuadorInvoiceView struct {
	AccessKey  string `json:"access_key"`
	CodDoc     string `json:"cod_doc"`
	Estab      string `json:"estab"`
	PtoEmi     string `json:"pto_emi"`
	Secuencial int64  `json:"secuencial"`
	Ambiente   string `json:"ambiente"`
	// AuthorizationNumber and AuthorizationDate are null until authorized.
	AuthorizationNumber *string    `json:"authorization_number"`
	AuthorizationDate   *time.Time `json:"authorization_date"`
}

// AttemptView is one row of the attempts ledger.
type AttemptView struct {
	ID         int64                  `json:"id"`
	Operation  string                 `json:"operation"`
	Outcome    string                 `json:"outcome"`
	Messages   []AuthorityMessageView `json:"messages"`
	Error      string                 `json:"error"`
	StartedAt  time.Time              `json:"started_at"`
	DurationMS int64                  `json:"duration_ms"`
}

// InvoiceDetail is one Tax Invoice in full.
type InvoiceDetail struct {
	InvoiceListItem
	// Issuer is the Issuer as snapshotted at signing; null until signed.
	Issuer             *invoicing.IssuerSnapshot `json:"issuer"`
	Lines              []LineView                `json:"lines"`
	AdditionalFields   []AdditionalFieldView     `json:"additional_fields"`
	PaymentMethod      string                    `json:"payment_method"`
	PaymentMethodLabel string                    `json:"payment_method_label"`
	Totals             TotalsView                `json:"totals"`
	// Messages are the authority's messages from its last answer.
	Messages []AuthorityMessageView `json:"messages"`
	// Ecuador is the SRI's numbering and authorization; null until signed.
	Ecuador  *EcuadorInvoiceView `json:"ecuador"`
	Attempts []AttemptView       `json:"attempts"`
	// The Sale side (#473): the one IVA rate a platform-priced document was
	// priced under, when its authorized document was mailed to the buyer,
	// when the Drainer next works it, and — on a Credit Note — the Sale
	// Invoice it credits and the reversal route that made it owed. All null
	// on a manual Tax Invoice.
	IVARate          *string    `json:"iva_rate"`
	DeliveredAt      *time.Time `json:"delivered_at"`
	NextAttemptAt    *time.Time `json:"next_attempt_at"`
	CreditsInvoiceID *string    `json:"credits_invoice_id"`
	ReversalReason   *string    `json:"reversal_reason"`
	// HasAuthorizationXML says whether the authority's document is on file
	// (#456 serves it).
	HasAuthorizationXML bool `json:"has_authorization_xml"`
	// CheckStatusHint is true when the invoice is pending and the authority
	// holds the document (received, in processing, or 43/70 on a resend): the
	// page says "check status" rather than showing an error (#455).
	CheckStatusHint bool      `json:"check_status_hint"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// FormatNumber renders the printed document number.
func FormatNumber(estab, ptoEmi string, secuencial int64) string {
	return fmt.Sprintf("%s-%s-%09d", estab, ptoEmi, secuencial)
}

func invoiceListItem(row *repository.InvoiceRow) InvoiceListItem {
	inv := &row.Invoice
	item := InvoiceListItem{
		ID:      inv.ID,
		Kind:    string(inv.Kind),
		Country: string(inv.Country),
		Status:  string(inv.Status),
		Recipient: RecipientView{
			TaxIDType: inv.Recipient.TaxIDType,
			TaxID:     inv.Recipient.TaxID,
			LegalName: inv.Recipient.LegalName,
			Address:   inv.Recipient.Address,
			Email:     inv.Recipient.Email,
		},
		TicketSaleID:        optional(inv.TicketSaleID),
		SaleConfirmationRef: optional(inv.SaleConfirmationRef),
		TotalCents:          inv.TotalCents,
		Currency:            inv.Currency,
	}
	if inv.Signed() {
		item.Environment = optional(string(inv.Environment))
		item.IssuedOn = optional(inv.IssuedOn.Format("2006-01-02"))
		issuedAt := inv.IssuedAt.UTC()
		item.IssuedAt = &issuedAt
		item.IssuedBy = optional(inv.IssuedBy)
	}
	if row.Ecuador != nil {
		item.Number = optional(FormatNumber(row.Ecuador.Estab, row.Ecuador.PtoEmi, row.Ecuador.Secuencial))
	}
	return item
}

// optional renders "" as null: the value is absent, not blank.
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func invoiceDetailView(row *repository.InvoiceRow) *InvoiceDetail {
	inv := &row.Invoice
	d := &InvoiceDetail{
		InvoiceListItem:     invoiceListItem(row),
		Issuer:              inv.Issuer,
		Lines:               []LineView{},
		AdditionalFields:    []AdditionalFieldView{},
		PaymentMethod:       inv.PaymentMethod,
		PaymentMethodLabel:  sri.PaymentMethodLabel(sri.PaymentMethod(inv.PaymentMethod)),
		Messages:            messagesView(inv.Messages),
		Attempts:            []AttemptView{},
		HasAuthorizationXML: len(inv.AuthorizationXML) > 0,
		CheckStatusHint:     checkStatusHint(inv, row.Attempts),
		IVARate:             optional(string(inv.IVARate)),
		CreditsInvoiceID:    optional(inv.CreditsInvoiceID),
		ReversalReason:      optional(inv.ReversalReason),
		CreatedAt:           inv.CreatedAt.UTC(),
		UpdatedAt:           inv.UpdatedAt.UTC(),
	}
	if inv.DeliveredAt != nil {
		t := inv.DeliveredAt.UTC()
		d.DeliveredAt = &t
	}
	if inv.NextAttemptAt != nil {
		t := inv.NextAttemptAt.UTC()
		d.NextAttemptAt = &t
	}
	if e := row.Ecuador; e != nil {
		d.Ecuador = &EcuadorInvoiceView{
			AccessKey:  e.AccessKey,
			CodDoc:     e.CodDoc,
			Estab:      e.Estab,
			PtoEmi:     e.PtoEmi,
			Secuencial: e.Secuencial,
			Ambiente:   string(sri.AmbienteFor(inv.Environment)),
		}
		if a := e.Authorization; a != nil {
			number := a.Number
			date := a.Date.UTC()
			d.Ecuador.AuthorizationNumber = &number
			d.Ecuador.AuthorizationDate = &date
		}
	}

	// The totals are re-derived from the stored per-line arithmetic, never
	// recomputed from prices: what the document carries is what is shown.
	byRate := map[invoicing.IVARate]*RateTotalView{}
	var order []invoicing.IVARate
	for _, l := range inv.Lines {
		percent := ratePercent(l.IVARate)
		d.Lines = append(d.Lines, LineView{
			Position:       l.Position,
			Description:    l.Description,
			Quantity:       sri.Quantity(l.QuantityMillionths).String(),
			UnitPriceCents: l.UnitPriceCents,
			DiscountCents:  l.DiscountCents,
			IVARate:        string(l.IVARate),
			RatePercent:    percent,
			BaseCents:      l.BaseCents,
			IVACents:       l.IVACents,
		})
		rt, ok := byRate[l.IVARate]
		if !ok {
			rt = &RateTotalView{IVARate: string(l.IVARate), RatePercent: percent}
			byRate[l.IVARate] = rt
			order = append(order, l.IVARate)
		}
		rt.BaseCents += l.BaseCents
		rt.IVACents += l.IVACents
	}
	d.Totals = TotalsView{
		ByRate:        []RateTotalView{},
		SubtotalCents: inv.SubtotalCents,
		DiscountCents: inv.DiscountCents,
		IVACents:      inv.IVACents,
		TotalCents:    inv.TotalCents,
	}
	for _, r := range order {
		d.Totals.ByRate = append(d.Totals.ByRate, *byRate[r])
	}
	for _, f := range inv.AdditionalFields {
		d.AdditionalFields = append(d.AdditionalFields, AdditionalFieldView{Name: f.Name, Value: f.Value})
	}
	for _, a := range row.Attempts {
		d.Attempts = append(d.Attempts, AttemptView{
			ID:         a.ID,
			Operation:  string(a.Operation),
			Outcome:    a.Outcome,
			Messages:   messagesView(a.Messages),
			Error:      a.Error,
			StartedAt:  a.StartedAt.UTC(),
			DurationMS: a.Duration.Milliseconds(),
		})
	}
	return d
}

func totalsView(t sri.Totals) TotalsView {
	v := TotalsView{
		ByRate:        []RateTotalView{},
		SubtotalCents: t.SubtotalCents,
		DiscountCents: t.DiscountCents,
		IVACents:      t.IVACents,
		TotalCents:    t.TotalCents,
	}
	for _, r := range t.ByRate {
		v.ByRate = append(v.ByRate, RateTotalView{
			IVARate:     string(rateFor(r.IVA)),
			RatePercent: r.RatePercent,
			BaseCents:   r.BaseCents,
			IVACents:    r.IVACents,
		})
	}
	return v
}

func rateFor(code sri.IVACode) invoicing.IVARate {
	for _, r := range invoicing.IVARates {
		if c, err := sri.IVACodeFor(r); err == nil && c == code {
			return r
		}
	}
	return ""
}

func ratePercent(rate invoicing.IVARate) int {
	if code, err := sri.IVACodeFor(rate); err == nil {
		if p, err := code.RatePercent(); err == nil {
			return p
		}
	}
	return 0
}

func messagesView(in []invoicing.AuthorityMessage) []AuthorityMessageView {
	if in == nil {
		return []AuthorityMessageView{}
	}
	return in
}
