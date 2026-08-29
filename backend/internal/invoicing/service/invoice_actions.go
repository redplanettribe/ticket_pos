package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
	"github.com/peter/ticket_pos/backend/internal/invoicing/sri"
)

// Check status and Resend (#455, parent #450 stories 31–34): what a Platform
// Operator does with a Tax Invoice the authority has not authorized.
//
// CHECK STATUS ASKS; IT NEVER SENDS. One query to autorización, one attempts
// row, and the invoice updated only if the authority decided something. An
// answer that decides nothing leaves the status exactly as it was — whether
// the SRI is still processing the document (received) or has no record of
// the clave at all (unknown, #514). The two are told apart on the ledger and
// in the page's hints, never in what the check does to the invoice.
//
// RESEND SENDS THE SAME DOCUMENT AGAIN. Same clave de acceso, same
// secuencial — the Ficha's rule for a fixable rejection (research §2.5) —
// rebuilt from the invoice's own Recipient, lines and fields, with the
// Issuer's editable details as they now stand (a corrected razón social goes
// on the resend; the RUC, establecimiento and punto de emisión come from the
// snapshot because the clave was computed from them), and signed afresh with
// the certificate now in custody. The document on file is replaced only when
// the authority took the resend, so the artifact the platform holds is always
// the one the authority holds.

// CheckInvoice asks the authority what became of a non-authorized invoice
// and returns it as it then stands.
func (s *Service) CheckInvoice(ctx context.Context, id string) (*InvoiceDetail, error) {
	row, err := s.actionable(ctx, id)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, s.pollBudget)
	defer cancel()
	authority := s.authority(row.Invoice.Environment)
	outcome, err := s.attempt(callCtx, id, invoicing.AttemptQuery, func() (invoicing.Outcome, error) {
		return authority.QueryOutcome(callCtx, row.Ecuador.AccessKey)
	})
	if err == nil && !undecided(outcome.State) {
		s.applyOutcome(ctx, row, outcome)
	}
	s.logger.Info("invoicing: tax invoice checked", "invoice_id", id, "outcome", outcome.State, "error", err)
	s.dueForDelivery(ctx, row)
	return s.GetInvoice(ctx, id)
}

// dueForDelivery makes a Sale-side document the operator's action has just
// healed due at once, so the Drainer's next round mails it to the buyer
// (#475). Only a document that was off the queue is touched: one under a
// round's lease is that round's to deliver (repository.DueForDelivery).
func (s *Service) dueForDelivery(ctx context.Context, row *repository.InvoiceRow) {
	if row.Invoice.Kind == invoicing.DocumentKindManual {
		return
	}
	due, err := s.repo.DueForDelivery(context.WithoutCancel(ctx), row.Invoice.ID, s.clock())
	if err != nil {
		s.logger.Error("invoicing: could not make the healed document due for delivery", "invoice_id", row.Invoice.ID, "error", err)
		return
	}
	if due {
		s.logger.Info("invoicing: healed document due for delivery", "invoice_id", row.Invoice.ID, "kind", row.Invoice.Kind)
	}
}

// ResendInvoice rebuilds, re-signs and resubmits a non-authorized invoice
// under its existing clave and secuencial, then polls as an issue does, and
// returns it as it then stands.
func (s *Service) ResendInvoice(ctx context.Context, id string) (*InvoiceDetail, error) {
	row, err := s.actionable(ctx, id)
	if err != nil {
		return nil, err
	}
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

	snapshot := refreshedSnapshot(*row.Invoice.Issuer, issuer.Details)
	signed, err := s.rebuildAndSign(ctx, row, snapshot, cert)
	if err != nil {
		return nil, err
	}
	s.logger.Info("invoicing: tax invoice resent",
		"invoice_id", id, "environment", row.Invoice.Environment, "secuencial", row.Ecuador.Secuencial)

	// The resend goes out as the bytes just signed; what is on file changes
	// only once the authority has taken them.
	resend := *row
	resend.Invoice.SignedXML = signed
	s.submitAndPoll(ctx, &resend, func(ctx context.Context, outcome invoicing.Outcome) {
		if outcome.AlreadyHeld {
			return
		}
		if err := s.repo.ReplaceSignedDocument(context.WithoutCancel(ctx), id, signed, snapshot); err != nil {
			s.logger.Error("invoicing: could not replace the signed document after a received resend", "invoice_id", id, "error", err)
		}
	})
	s.dueForDelivery(ctx, row)
	return s.GetInvoice(ctx, id)
}

// actionable loads the invoice both actions work on, refusing an authorized
// one — a legal artifact is neither asked about nor sent again — an
// annulled one (#477): the operator recorded that the authority no longer
// holds it as valid, and a Check that found a late AUTORIZADO would undo
// that record — a withdrawn one (#476): never sent, and never will be, its
// sale having been reversed first — and one not yet signed (#473): an owed
// document has no clave to ask about and no bytes to send, and the Drainer
// is what issues it.
func (s *Service) actionable(ctx context.Context, id string) (*repository.InvoiceRow, error) {
	row, err := s.repo.GetInvoice(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, invoicing.ErrInvoiceNotFound()
	}
	switch row.Invoice.Status {
	case invoicing.InvoiceStatusAuthorized:
		return nil, invoicing.ErrInvoiceAlreadyAuthorized()
	case invoicing.InvoiceStatusAnnulled:
		return nil, invoicing.ErrInvoiceAnnulled()
	case invoicing.InvoiceStatusWithdrawn:
		return nil, invoicing.ErrInvoiceWithdrawn()
	}
	if !row.Invoice.Signed() || row.Ecuador == nil {
		return nil, invoicing.ErrInvoiceNotIssued()
	}
	return row, nil
}

// refreshedSnapshot is the Issuer as the resend prints it: every editable
// detail as the Issuer now reads, and the three the clave de acceso was
// computed from as the invoice recorded them. Those three are frozen once
// documents exist, so in practice they agree; taking them from the snapshot
// is what makes that a fact about the document rather than a hope about the
// Issuer.
func refreshedSnapshot(stored invoicing.IssuerSnapshot, live invoicing.EcuadorIssuerDetails) invoicing.IssuerSnapshot {
	refreshed := invoicing.SnapshotOf(live)
	refreshed.RUC = stored.RUC
	refreshed.Establecimiento = stored.Establecimiento
	refreshed.PuntoEmision = stored.PuntoEmision
	return refreshed
}

// rebuildAndSign builds the document from what the invoice recorded — the
// same number, clave and emission instant — and signs it now. A Credit
// Note is rebuilt as the nota de crédito it was (#476), from the factura
// it credits.
func (s *Service) rebuildAndSign(ctx context.Context, row *repository.InvoiceRow, snapshot invoicing.IssuerSnapshot, cert *sri.Certificate) ([]byte, error) {
	inv := &row.Invoice
	recipient, err := sriRecipient(inv.Recipient)
	if err != nil {
		return nil, invoicing.ErrInvoiceInvalid(reason(err))
	}
	lines := make([]sri.Line, 0, len(inv.Lines))
	for _, l := range inv.Lines {
		code, err := sri.IVACodeFor(l.IVARate)
		if err != nil {
			return nil, invoicing.ErrInvoiceInvalid(reason(err))
		}
		lines = append(lines, sri.Line{
			Description:    l.Description,
			Quantity:       sri.Quantity(l.QuantityMillionths),
			UnitPriceCents: l.UnitPriceCents,
			DiscountCents:  l.DiscountCents,
			IVA:            code,
			// A platform-priced document's lines are priced as paid, IVA
			// inside (#474); a resend must render them as the original did.
			IVAInclusive: inv.Kind != invoicing.DocumentKindManual,
		})
	}
	sequential, err := sri.FormatSequential(row.Ecuador.Secuencial)
	if err != nil {
		return nil, invoicing.ErrInvoiceInvalid(reason(err))
	}
	parts := facturaParts{
		base: sri.Factura{
			Environment:   sri.AmbienteFor(inv.Environment),
			Issuer:        sri.IssuerFromSnapshot(snapshot),
			IssuedOn:      inv.IssuedAt,
			PaymentMethod: sri.PaymentMethod(inv.PaymentMethod),
		},
		recipient: recipient,
		lines:     lines,
		fields:    sriFields(inv.AdditionalFields),
	}
	if inv.Kind == invoicing.DocumentKindCreditNote {
		factura, err := s.repo.GetInvoice(ctx, inv.CreditsInvoiceID)
		if err != nil {
			return nil, err
		}
		if parts, err = creditNoteParts(parts, inv, factura); err != nil {
			return nil, invoicing.ErrInvoiceInvalid(reason(err))
		}
	}
	unsigned, err := parts.build(row.Ecuador.AccessKey, sequential)
	if err != nil {
		// The invoice built once; what can have changed since is the Issuer.
		return nil, invoicing.ErrIssuerIncomplete(reason(err))
	}
	signed, err := sri.Sign(unsigned, cert, sri.SignOptions{SigningTime: s.clock()})
	if err != nil {
		return nil, err
	}
	return signed, nil
}

// checkStatusHint says whether the page should tell the operator "the
// authority has this document — check status" rather than offer an error.
// It means exactly one thing (#516, parent #513): the SRI holds this
// document and has not decided about it. It holds it when some Submit came
// back received — RECIBIDA, or 43/70 on a resend, which is the authority
// saying the clave is already registered with it. It has decided nothing
// while the invoice is pending, or parked needs_attention by the 24-hour
// rule (#474) — the same position seen from a day later — and the last
// thing it said was not a decision. A Sale Invoice the authority refused is
// also parked needs_attention (drainer), and that one carries no hint: the
// refusal is the answer, and there is nothing left to check.
//
// A QUERY ANSWER IS NOT ACKNOWLEDGEMENT, whatever it says
// (invoicing.AcknowledgedByAuthority holds that rule). Until #514 this hint
// read the last attempt's outcome instead, so the ledger production invoice
// 001-001-000000001 actually had — a Submit that died in transport, then
// query after query answered "received" — turned the hint on and steered
// the operator away from the resend that was the one thing that would fix
// it. An `unknown` answer is the authority saying it has no record of the
// clave at all; a `received` one describes a document it is processing, and
// it can only be processing one it took from a Submit.
//
// The resend hint (resendHint) is this hint's opposite, and the two are
// mutually exclusive: a document the authority never acknowledged carries
// no check-status hint, and one it did carries no resend hint.
func checkStatusHint(inv *invoicing.Invoice, attempts []invoicing.Attempt) bool {
	if inv.Status != invoicing.InvoiceStatusPending && inv.Status != invoicing.InvoiceStatusNeedsAttention {
		return false
	}
	if !invoicing.AcknowledgedByAuthority(attempts) {
		return false
	}
	return undecidedAttempt(attempts[len(attempts)-1].Outcome)
}

// resendHint says whether the page should tell the operator "the SRI has no
// record of this document — it was never received; resend it" (#516).
//
// It is true when the last thing the authority said was `unknown` (#514) —
// asked directly about the clave, autorización answered with no comprobante
// under it — and no Submit was ever acknowledged. That is the ledger a
// Submit killed in transport leaves behind, and the document is nowhere: not
// with the platform's authority to wait on, not with the SRI. Sending the
// same bytes again under the same clave and secuencial is what fixes it, and
// cannot duplicate anything (invoicing.AcknowledgedByAuthority explains why).
//
// The latest attempt is what is read, not any attempt, because an `unknown`
// answered before a later Submit or a later decision is history: the page
// speaks about where the document is now.
//
// A hint is only worth showing where the action behind it can be taken, so
// this reads the status for the same reason checkStatusHint does: a
// withdrawn or annulled document may carry the ledger of a Submit that died
// in transport, and asking for a resend the service would refuse
// (ErrInvoiceWithdrawn, ErrInvoiceAnnulled) is telling the operator to do
// something impossible.
func resendHint(inv *invoicing.Invoice, attempts []invoicing.Attempt) bool {
	if len(attempts) == 0 {
		return false
	}
	if inv.Status != invoicing.InvoiceStatusPending && inv.Status != invoicing.InvoiceStatusNeedsAttention {
		return false
	}
	if invoicing.AcknowledgedByAuthority(attempts) {
		return false
	}
	return attempts[len(attempts)-1].Outcome == string(invoicing.OutcomeUnknown)
}
