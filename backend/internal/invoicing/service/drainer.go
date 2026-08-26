package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
	"github.com/peter/ticket_pos/backend/internal/invoicing/sri"
	"github.com/peter/ticket_pos/backend/internal/platform/apperror"
)

// The Sale Invoice Drainer (#474, parent #471, ADR 0060): what turns an owed
// Sale Invoice into an authorized factura with nobody waiting — no buyer on
// the page, no operator pressing Issue.
//
// It is the Reversal Reconciler's shape (sales/service/reconciler.go, read
// that first): an authenticated internal endpoint a scheduler ticks and a
// human can curl, a queue that is a column on the feature's own table
// (invoicing_invoices.next_attempt_at), one document claimed at a time under
// a lease so that two drains never work the same one, and a run bounded by
// a budget of its own. What is written here is only where this drain
// differs.
//
// THE NUMBER IS CONSUMED HERE AND NOWHERE EARLIER. An owed row has no
// secuencial; signing it — inside one transaction that allocates the number,
// builds the factura from the row's own snapshot and signs it — is what
// consumes one, and everything that can refuse does so BEFORE that
// transaction, exactly as the manual issue refuses before its own. A
// document that cannot be signed (no Issuer, no certificate, an expired one,
// an Issuer the schema will not take) is parked needs_attention with no
// number consumed, so the sequence has no holes for documents that never
// existed (#471 story 23).
//
// A DOCUMENT THE AUTHORITY HOLDS IS POLLED, NEVER RESUBMITTED. Once any
// attempt has come back "received", every later round asks autorización and
// nothing else; only a document the authority never acknowledged — every
// submit a transport failure — is sent again, under the same clave. That is
// the SRI's rule (research §2.5) and the reason a resend can never make a
// duplicate.
//
// THE LADDER is 1 min, 5 min, 15 min, then hourly, measured from the
// signing instant (issued_at), for transport failures and for "received /
// en procesamiento" alike. A definite refusal parks the document
// needs_attention and stops the ladder: the SRI's messages are the
// operator's to read, and Check status / Resend are the remedies. Twenty-four
// hours without a definite answer parks it needs_attention too, but the
// ladder goes on — hourly — so that a late AUTORIZADO heals it without
// anyone pressing anything. An unsignable document is retried hourly for the
// same reason: an operator who uploads the certificate has fixed it, and the
// next hour's tick proves so.
//
// A CREDIT NOTE RIDES THE SAME MACHINE (#476). Claimed only once the
// factura it credits is terminal (repository.ClaimDueInvoice), it is then
// one of two things: its factura is authorized, and it is signed under
// codDoc 04 from the factura's number and date, submitted, polled, parked
// and delivered exactly as a Sale Invoice is; or its factura died —
// withdrawn, annulled — and it is withdrawn with it, unsigned, no number
// consumed, nothing sent, because a nota de crédito can name only an
// authorized factura (#471 story 29).
//
// DELIVERY IS THIS DRAINER'S LAST STEP (#475). An authorized document is
// mailed to the buyer in the same round that saw it authorized — and, if
// the sender refused, is claimed again on the same ladder for delivery
// alone: an authorized document without a delivered_at is due work exactly
// as an owed one is, and the authorization is never touched by whatever
// the mail does. Delivery asks the authority nothing. See delivery.go.
//
// LOG LINES COUNT DOCUMENTS AND NAME STATES, never buyers (#471 story 35).

// SaleInvoiceLadder is the delay between rounds after the first, second and
// third round without a definite answer, and every round after.
var SaleInvoiceLadder = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}

// SaleInvoiceAttentionAfter is how long a signed document may go without a
// definite answer before it is parked needs_attention (still polled).
const SaleInvoiceAttentionAfter = 24 * time.Hour

// saleInvoiceDrainBatch and saleInvoiceDrainBudget bound one run, on the
// reconciler's terms: the budget is the real bound and the count is the
// belt. Signing and submitting one owed document can cost the whole poll
// budget (DefaultPollBudget, ~15 s), so a run stops STARTING documents at
// the budget and the last one may still be paying that on top.
//
// The deadline chain, as the reconciler states it:
//
//	saleInvoiceDrainBudget  <  Cloud Scheduler's attempt_deadline  <  Cloud Run's request timeout
//	         60s            <              120s                    <           300s
//
// The middle term is terraform's sale_invoice_drainer_attempt_deadline_seconds;
// TestTheSaleInvoiceDrainBudgetIsStrictlyInsideTheSchedulerDeadline reads it
// rather than mirroring it.
const (
	saleInvoiceDrainBatch  = 50
	saleInvoiceDrainBudget = 60 * time.Second
)

// saleInvoiceClaimLease is how long a claimed document is hidden from other
// drains: the fallback for an instance that died between claiming and
// writing what it learned. An ordinary round overwrites it within seconds.
const saleInvoiceClaimLease = 5 * time.Minute

// saleInvoiceDrainerName is what a Sale Invoice's issued_by says: the
// platform's own hand, where a manual document names the operator.
const saleInvoiceDrainerName = "sale-invoice-drainer"

// platformMessageType is the type a message the PLATFORM puts on a document
// carries, beside the authority's ERROR / ADVERTENCIA: why an owed document
// could not be signed, in the operator's own vocabulary. The page shows it
// in the same list; the type says who is speaking.
const platformMessageType = "PLATFORM"

// SaleInvoiceDrainResult is what one run did, and what is standing
// afterwards — counts and states only.
type SaleInvoiceDrainResult struct {
	// Claimed is how many documents this run took up. Zero is the ordinary
	// answer: nothing was due.
	Claimed int `json:"claimed"`
	// Authorized, Pending and NeedsAttention are where those documents
	// ended: authorized this run; still undecided and due again on the
	// ladder; parked for an operator (refused, unsignable, or 24 h without an
	// answer). With Failed they sum to Claimed.
	Authorized     int `json:"authorized"`
	Pending        int `json:"pending"`
	NeedsAttention int `json:"needs_attention"`
	// Delivered is how many authorized documents this run mailed to their
	// buyers (#475). Each is counted under Authorized as well; an authorized
	// document the sender refused is not counted here and is due again.
	Delivered int `json:"delivered"`
	// Withdrawn is how many Credit Notes this run withdrew because the
	// factura they would have credited died (#476). Counted beside the
	// three above rather than under any of them.
	Withdrawn int `json:"withdrawn"`
	// Failed is documents whose round errored on the database itself. Each
	// logged its own line; the claim lease returns them to the queue. With
	// Authorized, Pending, NeedsAttention and Withdrawn it sums to Claimed.
	Failed int `json:"failed"`
	// Standing is how many Sale Invoices sit in each state once this run
	// finished, so two curls apart say whether a backlog is shrinking.
	Standing map[string]int `json:"standing"`
}

// DrainSaleInvoices works the Sale Invoices that are due, one at a time,
// until the queue is empty or the run reaches its bound. Safe to call by
// hand at any time; a no-op on an empty queue.
func (s *Service) DrainSaleInvoices(ctx context.Context) (*SaleInvoiceDrainResult, error) {
	return s.drainSaleInvoices(ctx, "")
}

// KickSaleInvoiceDrainer implements the sales module's seam for "the
// transaction that owed a document has committed": one immediate round over
// that Sale's documents, in the background, so the normal path completes in
// seconds while the checkout response has already gone out. The caller's
// context is detached, because the request it belongs to is finishing; the
// round bounds itself.
//
// It is fire-and-forget on purpose: nothing about the checkout waits on it
// and nothing about the checkout can fail because of it. Whatever it does
// not finish, the scheduled drain finds due exactly as it left it.
func (s *Service) KickSaleInvoiceDrainer(ctx context.Context, ticketSaleID string) {
	if !s.kick {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), saleInvoiceDrainBudget)
		defer cancel()
		if _, err := s.drainSaleInvoices(ctx, ticketSaleID); err != nil {
			s.logger.Warn("invoicing: the post-commit kick could not drain; the scheduled drain will", "error", err)
		}
	}()
}

// WithSaleInvoiceKick turns the post-commit kick on or off. It is on in
// every deployment; the test harness turns it off so that a drain is
// something a test drives, and turns it on for the test that proves it.
func (s *Service) WithSaleInvoiceKick(enabled bool) *Service {
	s.kick = enabled
	return s
}

// WithSaleInvoiceDrainBatch narrows how many documents one run works, so a
// test can prove the bound is a bound.
func (s *Service) WithSaleInvoiceDrainBatch(batch int) *Service {
	s.drainBatch = batch
	return s
}

func (s *Service) saleInvoiceDrainBatch() int {
	if s.drainBatch > 0 {
		return s.drainBatch
	}
	return saleInvoiceDrainBatch
}

func (s *Service) drainSaleInvoices(ctx context.Context, ticketSaleID string) (*SaleInvoiceDrainResult, error) {
	out := &SaleInvoiceDrainResult{Standing: map[string]int{}}
	deadline := s.clock().Add(saleInvoiceDrainBudget)
	wall := time.Now()

	for out.Claimed < s.saleInvoiceDrainBatch() {
		if ctx.Err() != nil {
			break
		}
		now := s.clock()
		// The budget is measured on the wall as well as on the service clock:
		// the clock is fixed in tests, and a run that could not tell time had
		// passed would have no bound at all.
		if !now.Before(deadline) || time.Since(wall) >= saleInvoiceDrainBudget {
			break
		}
		row, err := s.repo.ClaimDueInvoice(ctx, now, now.Add(saleInvoiceClaimLease), ticketSaleID)
		if err != nil {
			return nil, err
		}
		if row == nil {
			break
		}
		out.Claimed++
		status, delivered, err := s.workSaleInvoice(ctx, row)
		if err != nil {
			s.logger.Warn("invoicing: the drainer could not finish a document; it stays leased until the claim expires",
				"invoice_id", row.Invoice.ID, "error", err)
			out.Failed++
			continue
		}
		if delivered {
			out.Delivered++
		}
		switch status {
		case invoicing.InvoiceStatusAuthorized:
			out.Authorized++
		case invoicing.InvoiceStatusNeedsAttention:
			out.NeedsAttention++
		case invoicing.InvoiceStatusWithdrawn:
			out.Withdrawn++
		default:
			out.Pending++
		}
	}

	standing, err := s.repo.CountSaleInvoicesByStatus(ctx)
	if err != nil {
		s.logger.Warn("invoicing: could not count the standing Sale Invoices for the drain response", "error", err)
	} else {
		out.Standing = standing
	}
	s.logger.Info("invoicing: sale invoices drained",
		"claimed", out.Claimed, "authorized", out.Authorized, "pending", out.Pending,
		"needs_attention", out.NeedsAttention, "delivered", out.Delivered, "withdrawn", out.Withdrawn, "failed", out.Failed)
	return out, nil
}

// workSaleInvoice does one round on one claimed document and reports the
// state it left it in, and whether this round delivered it to the buyer.
//
// A document claimed already authorized is here for delivery alone: an
// earlier round authorized it and the sender refused (#475). Nothing about
// it is asked of the authority again.
func (s *Service) workSaleInvoice(ctx context.Context, row *repository.InvoiceRow) (invoicing.InvoiceStatus, bool, error) {
	if row.Invoice.Status == invoicing.InvoiceStatusAuthorized {
		delivered, err := s.deliverDocument(ctx, row)
		return invoicing.InvoiceStatusAuthorized, delivered, err
	}
	if !row.Invoice.Signed() {
		if row.Invoice.Kind == invoicing.DocumentKindCreditNote {
			// Its factura is terminal, or it would not have been claimed.
			// Dead factura: the Credit Note goes with it, and there is
			// nothing further to do this round.
			withdrawn, err := s.withdrawCreditNoteOfADeadFactura(ctx, row)
			if err != nil || withdrawn {
				return invoicing.InvoiceStatusWithdrawn, false, err
			}
		}
		signed, err := s.signOwedInvoice(ctx, row)
		if err != nil {
			return "", false, err
		}
		if signed == nil {
			// Parked unsignable; signOwedInvoice wrote why.
			return invoicing.InvoiceStatusNeedsAttention, false, nil
		}
		row = signed
		s.submitAndPoll(ctx, row, nil)
	} else if heldByAuthority(row.Attempts) {
		s.pollOnce(ctx, row)
	} else {
		// Every submit so far failed to reach the authority: send the bytes
		// on file again, under the same clave.
		s.submitAndPoll(ctx, row, nil)
	}
	status, err := s.settleRound(ctx, row.Invoice.ID)
	if err != nil || status != invoicing.InvoiceStatusAuthorized {
		return status, false, err
	}
	// Authorized this round: hand it over now rather than on the next tick,
	// so the buyer's second mail follows the receipt within seconds.
	authorized, err := s.repo.GetInvoice(ctx, row.Invoice.ID)
	if err != nil {
		return status, false, err
	}
	delivered, err := s.deliverDocument(ctx, authorized)
	return status, delivered, err
}

// withdrawCreditNoteOfADeadFactura reads the factura an owed Credit Note
// credits and, if that factura will never be authorized — withdrawn, or
// annulled by the operator at the portal — withdraws the Credit Note too,
// off the queue, with a platform message saying why, and reports so. An
// authorized factura reports false: the Credit Note is signed next. Any
// other state is a claim that should not have happened and is left
// leased to expire, reported as neither.
func (s *Service) withdrawCreditNoteOfADeadFactura(ctx context.Context, row *repository.InvoiceRow) (bool, error) {
	factura, err := s.repo.GetInvoice(ctx, row.Invoice.CreditsInvoiceID)
	if err != nil {
		return false, err
	}
	if factura == nil {
		return false, fmt.Errorf("invoicing: credit note %s credits invoice %s, which does not exist", row.Invoice.ID, row.Invoice.CreditsInvoiceID)
	}
	switch factura.Invoice.Status {
	case invoicing.InvoiceStatusAuthorized:
		return false, nil
	case invoicing.InvoiceStatusWithdrawn, invoicing.InvoiceStatusAnnulled:
	default:
		return false, fmt.Errorf("invoicing: credit note %s was claimed while its factura is %s", row.Invoice.ID, factura.Invoice.Status)
	}
	now := s.clock()
	msgs := []invoicing.AuthorityMessage{{
		Identifier: creditNoteWithdrawnCode,
		Message:    fmt.Sprintf("The Sale Invoice this Credit Note would have credited is %s; nothing was sent to the SRI.", factura.Invoice.Status),
		Type:       platformMessageType,
	}}
	if err := s.repo.Reschedule(context.WithoutCancel(ctx), row.Invoice.ID, invoicing.InvoiceStatusWithdrawn, msgs, nil, now); err != nil {
		return false, err
	}
	s.logger.Info("invoicing: credit note withdrawn; the factura it credits died unauthorized",
		"invoice_id", row.Invoice.ID, "credits_invoice_id", factura.Invoice.ID, "factura_status", factura.Invoice.Status)
	return true, nil
}

// creditNoteWithdrawnCode is the platform message a withdrawn Credit Note
// carries, in the operator's vocabulary beside the authority's codes.
const creditNoteWithdrawnCode = "CREDIT_NOTE_FACTURA_NOT_AUTHORIZED"

// pollOnce asks autorización once and applies a definite answer; an
// undecided one only reschedules.
func (s *Service) pollOnce(ctx context.Context, row *repository.InvoiceRow) {
	callCtx, cancel := context.WithTimeout(ctx, s.pollBudget)
	defer cancel()
	authority := s.authority(row.Invoice.Environment)
	outcome, err := s.attempt(callCtx, row.Invoice.ID, invoicing.AttemptQuery, func() (invoicing.Outcome, error) {
		return authority.QueryOutcome(callCtx, row.Ecuador.AccessKey)
	})
	if err == nil {
		s.applyOutcome(ctx, row, outcome)
	}
}

// settleRound reads the document back and, if the round left it undecided
// without an answer to schedule from (a transport failure writes no
// outcome), puts it on the ladder itself. A refusal and an unsignable
// document are left exactly as applyOutcome and signOwedInvoice parked
// them.
func (s *Service) settleRound(ctx context.Context, id string) (invoicing.InvoiceStatus, error) {
	row, err := s.repo.GetInvoice(ctx, id)
	if err != nil {
		return "", err
	}
	if row == nil {
		return "", invoicing.ErrInvoiceNotFound()
	}
	inv := &row.Invoice
	switch inv.Status {
	case invoicing.InvoiceStatusPending:
		return undecidedStatus(inv, s.clock()), s.reschedule(ctx, row)
	case invoicing.InvoiceStatusNeedsAttention:
		if len(row.Attempts) == 0 {
			// Unsignable: parked with its hourly retry already written.
			return inv.Status, nil
		}
		switch row.Attempts[len(row.Attempts)-1].Outcome {
		case string(invoicing.OutcomeReceived), invoicing.AttemptOutcomeError:
			// Past 24 h and still undecided: parked, and still polled.
			return inv.Status, s.reschedule(ctx, row)
		}
	}
	return inv.Status, nil
}

// reschedule puts an undecided document on the ladder from its signing
// instant, parking it needs_attention once 24 h have passed.
func (s *Service) reschedule(ctx context.Context, row *repository.InvoiceRow) error {
	now := s.clock()
	next := now.Add(ladderDelay(now.Sub(row.Invoice.IssuedAt)))
	return s.repo.Reschedule(context.WithoutCancel(ctx), row.Invoice.ID, undecidedStatus(&row.Invoice, now), nil, &next, now)
}

// signOwedInvoice turns an owed row into a signed, pending one, consuming a
// secuencial — or parks it needs_attention when it cannot be signed and
// returns nil. Everything that can refuse does so before the number is
// allocated.
func (s *Service) signOwedInvoice(ctx context.Context, row *repository.InvoiceRow) (*repository.InvoiceRow, error) {
	inv := &row.Invoice
	now := s.clock()
	park := func(code, message string) (*repository.InvoiceRow, error) {
		next := now.Add(SaleInvoiceLadder[len(SaleInvoiceLadder)-1])
		msgs := []invoicing.AuthorityMessage{{Identifier: code, Message: message, Type: platformMessageType}}
		if err := s.repo.Reschedule(context.WithoutCancel(ctx), inv.ID, invoicing.InvoiceStatusNeedsAttention, msgs, &next, now); err != nil {
			return nil, err
		}
		s.logger.Warn("invoicing: sale invoice cannot be signed; parked needs_attention with no number consumed", "invoice_id", inv.ID, "reason", code)
		return nil, nil
	}

	issuer, err := s.repo.GetEcuadorIssuer(ctx)
	if err != nil {
		return nil, err
	}
	if issuer == nil {
		e := invoicing.ErrIssuerNotFound()
		return park(e.Code(), e.Message())
	}
	cert, err := s.OpenEcuadorSigningKey(ctx)
	if err != nil {
		var domain apperror.DomainError
		if errors.As(err, &domain) {
			return park(domain.Code(), domain.Message())
		}
		return nil, err
	}
	if !cert.Metadata.NotAfter.After(now) {
		e := invoicing.ErrCertificateExpired()
		return park(e.Code(), e.Message())
	}

	// The Issuer's CURRENT environment and details: an Issuer moved from
	// test to production signs still-owed documents in production.
	snapshot := invoicing.SnapshotOf(issuer.Details)
	env := issuer.Issuer.Environment
	base := sri.Factura{
		Environment:   sri.AmbienteFor(env),
		Issuer:        sri.IssuerFromSnapshot(snapshot),
		IssuedOn:      now,
		PaymentMethod: sri.PaymentMethod(inv.PaymentMethod),
	}
	parts, err := s.saleDocumentParts(ctx, base, row)
	if err != nil {
		e := invoicing.ErrInvoiceInvalid(reason(err))
		return park(e.Code(), e.Message())
	}
	if err := s.dryRun(base, probeRecipient, probeLines, nil); err != nil {
		e := invoicing.ErrIssuerIncomplete(reason(err))
		return park(e.Code(), e.Message())
	}
	if err := s.dryRunParts(parts); err != nil {
		e := invoicing.ErrInvoiceInvalid(reason(err))
		return park(e.Code(), e.Message())
	}

	signed, err := s.repo.SignOwedInvoice(ctx, inv.ID, repository.SignedInvoice{
		IssuerID:    issuer.Issuer.ID,
		Environment: env,
		IssuedOn:    guayaquilDate(now),
		IssuedAt:    now,
		IssuedBy:    saleInvoiceDrainerName,
		Snapshot:    snapshot,
		CodDoc:      parts.documentType(),
		Estab:       snapshot.Establecimiento,
		PtoEmi:      snapshot.PuntoEmision,
	}, numberAndSign(parts, cert, now))
	if err != nil {
		return nil, err
	}
	s.logger.Info("invoicing: sale document signed",
		"invoice_id", signed.Invoice.ID, "kind", inv.Kind, "cod_doc", parts.documentType(), "environment", env, "secuencial", signed.Ecuador.Secuencial)
	return signed, nil
}

// saleDocumentParts assembles the parts of an owed document by its kind: a
// Sale Invoice's factura, or a Credit Note's nota de crédito, which needs
// the factura it credits read for its number and date (#476).
func (s *Service) saleDocumentParts(ctx context.Context, base sri.Factura, row *repository.InvoiceRow) (facturaParts, error) {
	parts, err := saleFacturaParts(base, &row.Invoice)
	if err != nil {
		return facturaParts{}, err
	}
	if row.Invoice.Kind != invoicing.DocumentKindCreditNote {
		return parts, nil
	}
	factura, err := s.repo.GetInvoice(ctx, row.Invoice.CreditsInvoiceID)
	if err != nil {
		return facturaParts{}, err
	}
	return creditNoteParts(parts, &row.Invoice, factura)
}

// creditNoteParts turns a Credit Note's factura-shaped parts into a nota
// de crédito's: codDoc 04, the credited factura's printed number and
// emission date, and the reversal route as the motivo. The credited
// factura must be signed and authorized; anything else is a document the
// Drainer should never have reached here with.
func creditNoteParts(parts facturaParts, note *invoicing.Invoice, factura *repository.InvoiceRow) (facturaParts, error) {
	if factura == nil || factura.Ecuador == nil || !factura.Invoice.Signed() {
		return facturaParts{}, fmt.Errorf("credit note %s credits an unsigned or missing invoice %s", note.ID, note.CreditsInvoiceID)
	}
	if factura.Invoice.Status != invoicing.InvoiceStatusAuthorized {
		return facturaParts{}, fmt.Errorf("credit note %s credits invoice %s, which is %s and not authorized", note.ID, note.CreditsInvoiceID, factura.Invoice.Status)
	}
	// The factura's emission date as the factura printed it. issued_on is
	// the Guayaquil calendar day stored as a date-only value (guayaquilDate),
	// so it is re-read as a day and NOT shifted through a zone again: a
	// midnight-UTC date moved to Guayaquil would print the day before.
	y, m, d := factura.Invoice.IssuedOn.Date()
	parts.docType = sri.DocumentTypeNotaCredito
	parts.modifies = sri.ModifiedDocument{
		DocumentType: factura.Ecuador.CodDoc,
		Number:       FormatNumber(factura.Ecuador.Estab, factura.Ecuador.PtoEmi, factura.Ecuador.Secuencial),
		IssuedOn:     time.Date(y, m, d, 12, 0, 0, 0, sri.Guayaquil),
	}
	parts.motivo = invoicing.CreditNoteMotivo(note.ReversalReason)
	return parts, nil
}

// saleFacturaParts assembles the factura from the owed row's own snapshot:
// the Recipient as the Sale transacted them, one IVA-inclusive line per
// stored line at the price paid, and no additional fields beyond the email
// the builder adds itself.
func saleFacturaParts(base sri.Factura, inv *invoicing.Invoice) (facturaParts, error) {
	recipient, err := sriRecipient(inv.Recipient)
	if err != nil {
		return facturaParts{}, err
	}
	lines := make([]sri.Line, 0, len(inv.Lines))
	for _, l := range inv.Lines {
		code, err := sri.IVACodeFor(l.IVARate)
		if err != nil {
			return facturaParts{}, err
		}
		lines = append(lines, sri.Line{
			Description:    l.Description,
			Quantity:       sri.Quantity(l.QuantityMillionths),
			UnitPriceCents: l.UnitPriceCents,
			IVA:            code,
			IVAInclusive:   true,
		})
	}
	return facturaParts{base: base, recipient: recipient, lines: lines, fields: nil}, nil
}

// heldByAuthority reports whether any attempt came back "received": the
// authority holds the document, so it is polled and never sent again.
func heldByAuthority(attempts []invoicing.Attempt) bool {
	for _, a := range attempts {
		if a.Outcome == string(invoicing.OutcomeReceived) {
			return true
		}
	}
	return false
}

// undecidedStatus is where a signed document without a definite answer
// stands at now: pending, or needs_attention once 24 h have passed since it
// was signed.
func undecidedStatus(inv *invoicing.Invoice, now time.Time) invoicing.InvoiceStatus {
	if now.Sub(inv.IssuedAt) >= SaleInvoiceAttentionAfter {
		return invoicing.InvoiceStatusNeedsAttention
	}
	return invoicing.InvoiceStatusPending
}

// ladderDelay is the delay before the next round, from how long the
// document has been signed: the first rung until the first delay has
// elapsed, the second until the first two have, and so on; the last rung
// forever after. Read at the moments a healthy drain arrives, that is 1 min,
// 5 min, 15 min, then hourly.
func ladderDelay(elapsed time.Duration) time.Duration {
	var cumulative time.Duration
	for i, d := range SaleInvoiceLadder {
		if i == len(SaleInvoiceLadder)-1 || elapsed < cumulative+d {
			return d
		}
		cumulative += d
	}
	return SaleInvoiceLadder[len(SaleInvoiceLadder)-1]
}
