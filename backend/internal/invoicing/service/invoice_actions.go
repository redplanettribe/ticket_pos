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
// answer that decides nothing (still processing, or nothing known under the
// clave) leaves the status exactly as it was.
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
	if err == nil && outcome.State != invoicing.OutcomeReceived {
		s.applyOutcome(ctx, row, outcome)
	}
	s.logger.Info("invoicing: tax invoice checked", "invoice_id", id, "outcome", outcome.State, "error", err)
	return s.GetInvoice(ctx, id)
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
	signed, err := s.rebuildAndSign(row, snapshot, cert)
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
	return s.GetInvoice(ctx, id)
}

// actionable loads the invoice both actions work on, refusing an authorized
// one — a legal artifact is neither asked about nor sent again — and one
// not yet signed (#473): an owed document has no clave to ask about and no
// bytes to send, and the Drainer is what issues it.
func (s *Service) actionable(ctx context.Context, id string) (*repository.InvoiceRow, error) {
	row, err := s.repo.GetInvoice(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, invoicing.ErrInvoiceNotFound()
	}
	if row.Invoice.Status == invoicing.InvoiceStatusAuthorized {
		return nil, invoicing.ErrInvoiceAlreadyAuthorized()
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

// rebuildAndSign builds the factura from what the invoice recorded — the
// same number, clave and emission instant — and signs it now.
func (s *Service) rebuildAndSign(row *repository.InvoiceRow, snapshot invoicing.IssuerSnapshot, cert *sri.Certificate) ([]byte, error) {
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
		})
	}
	sequential, err := sri.FormatSequential(row.Ecuador.Secuencial)
	if err != nil {
		return nil, invoicing.ErrInvoiceInvalid(reason(err))
	}
	f := sri.Factura{
		Environment:      sri.AmbienteFor(inv.Environment),
		Issuer:           sri.IssuerFromSnapshot(snapshot),
		AccessKey:        row.Ecuador.AccessKey,
		Sequential:       sequential,
		IssuedOn:         inv.IssuedAt,
		Recipient:        recipient,
		Lines:            lines,
		PaymentMethod:    sri.PaymentMethod(inv.PaymentMethod),
		AdditionalFields: sriFields(inv.AdditionalFields),
	}
	built, err := sri.BuildFactura(f)
	if err != nil {
		// The invoice built once; what can have changed since is the Issuer.
		return nil, invoicing.ErrIssuerIncomplete(reason(err))
	}
	signed, err := sri.Sign(built.XML, cert, sri.SignOptions{SigningTime: s.clock()})
	if err != nil {
		return nil, err
	}
	return signed, nil
}

// checkStatusHint says whether the page should tell the operator "the
// authority has this document — check status" rather than offer an error: the
// invoice is pending and the last thing the authority said was that it holds
// it (RECIBIDA, EN PROCESAMIENTO, or 43/70 on a resend). A pending invoice
// whose last call failed carries no hint; a resend is the thing to do there.
//
// A Sale Invoice parked needs_attention by the 24-hour rule (#474) is in
// the same position — the authority holds it and has not decided — and
// carries the hint too; one parked by a refusal does not, since its last
// answer was the refusal.
func checkStatusHint(inv *invoicing.Invoice, attempts []invoicing.Attempt) bool {
	if len(attempts) == 0 {
		return false
	}
	if inv.Status != invoicing.InvoiceStatusPending && inv.Status != invoicing.InvoiceStatusNeedsAttention {
		return false
	}
	return attempts[len(attempts)-1].Outcome == string(invoicing.OutcomeReceived)
}
