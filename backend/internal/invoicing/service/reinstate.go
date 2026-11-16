package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
)

// Reinstate (#749, spec #742, ADR 0078 section 5; CONTEXT.md's Reinstate):
// a Platform Operator returns an annulled factura to authorized because the
// SRI never took the annulment and its Annulment Window has closed, so the
// platform's record follows the SRI's.
//
// ADMITTED ONLY AT MISMATCH, AFTER THE WINDOW (reinstateRefusal). Inside the
// window it would be the undo #713 rejected, which makes `annulled`
// reversible exactly while Issue again and Reissue may act on it; and only
// the check's mismatch - AUTORIZADO from two asks - is the SRI saying the
// annulment did not take. Confirmed, checking and unconfirmed are refused.
//
// ONE RULE DECIDES WHAT IT OWES (reinstateCreditNote): a Credit Note if the
// Ticket Sale was reversed (the reversal's route as its reason) or another
// factura stands for the Sale in its place (the supersede reason, so the
// pair takes a Reissue's normal shape); otherwise nothing, and the factura
// is the Sale's current one again. A manual Tax Invoice simply returns to
// authorized. The Credit Note is owed in the same transaction and drained
// by the ordinary owed path, and an Issue again successor still unsigned
// waits for it as a Reissue's corrected factura does: the Drainer's claim
// holds a successor while a Credit Note against what it supersedes is
// owed or undecided (repository.ClaimDueInvoice), and its predecessor is no
// longer terminally dead (withdrawCorrectedFacturaOfADeadCreditNote).
//
// A FACTURA ANNULLED FROM PENDING OR NEEDS_ATTENTION never stored its
// authorization. Reinstate asks autorizacionComprobante for it when the act
// is taken, recorded on the attempts ledger as any query is, and is refused
// unless the SRI answers AUTORIZADO with the authorized XML, which it then
// stores. The Annulment Check itself still asks ConsultaComprobante alone.
//
// THE BUYER. A held Annulment Notice is dropped, never sent. One already
// sent is followed by the Credit Note's own mail when one is owed, and
// otherwise by a reinstatement notice carrying the RIDE and XML again,
// sent on the Drainer's tick (sendReinstatementNotices). A Sale Invoice the
// buyer was never sent is delivered as any authorized one is.

// ReinstateInvoice reinstates an annulled factura and returns it as it then
// stands.
func (s *Service) ReinstateInvoice(ctx context.Context, id, reinstatedBy string, req invoicing.ReinstateRequest) (*InvoiceDetail, error) {
	row, err := s.repo.GetInvoice(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, invoicing.ErrInvoiceNotFound()
	}
	now := s.clock()
	if err := reinstateRefusal(row, now); err != nil {
		return nil, err
	}
	if row.Ecuador == nil || strings.TrimSpace(req.Number) != FormatNumber(row.Ecuador.Estab, row.Ecuador.PtoEmi, row.Ecuador.Secuencial) {
		return nil, invoicing.ErrInvoiceNumberMismatch()
	}
	rein := repository.FacturaReinstatement{ReinstatedBy: reinstatedBy, ReinstatedAt: now, Note: req.Note}
	if len(row.Invoice.AuthorizationXML) == 0 || row.Ecuador.Authorization == nil {
		auth, err := s.fetchAuthorization(ctx, row)
		if err != nil {
			return nil, err
		}
		rein.Authorization = auth
	}

	found, creditNoteID, err := s.repo.ReinstateFactura(ctx, id, rein, func(locked *repository.InvoiceRow, facts repository.ReinstateFacts) (repository.ReinstateDecision, error) {
		if err := reinstateLockedRefusal(locked, facts, now); err != nil {
			return repository.ReinstateDecision{}, err
		}
		return repository.ReinstateDecision{CreditNote: reinstateCreditNote(&locked.Invoice, facts, now)}, nil
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, invoicing.ErrInvoiceNotFound()
	}
	s.logger.Info("invoicing: factura reinstated",
		"invoice_id", id, "kind", row.Invoice.Kind, "reinstated_by", reinstatedBy,
		"credit_note_id", creditNoteID, "authorization_fetched", rein.Authorization != nil)
	if row.Invoice.Kind == invoicing.DocumentKindSale && s.saleInvoicingEnabled {
		s.KickSaleInvoiceDrainer(ctx, row.Invoice.TicketSaleID)
	}
	return s.GetInvoice(ctx, id)
}

// reinstateRefusal says whether Reinstate may be pressed on a document as it
// stands, whatever is typed: the facts the panel reads to offer it. In
// order: an annulled factura; its Annulment Window closed; its standing
// annulment's check at mismatch. An annulment recorded before the check
// existed reads unconfirmed, as the detail shows it, and is refused.
func reinstateRefusal(row *repository.InvoiceRow, now time.Time) error {
	if err := reinstateDocumentRefusal(&row.Invoice, now); err != nil {
		return err
	}
	verdict := invoicing.AnnulmentUnconfirmed
	if current := currentAnnulment(row); current != nil && current.Check != nil {
		verdict = current.Check.Verdict
	}
	if verdict != invoicing.AnnulmentMismatch {
		return invoicing.ErrInvoiceReinstateNotMismatch(verdict)
	}
	return nil
}

// reinstateDocumentRefusal is the half of the gate the factura's own row
// answers, shared by the read and the locked decision: an annulled factura,
// and its Annulment Window closed.
func reinstateDocumentRefusal(inv *invoicing.Invoice, now time.Time) error {
	if !invoicing.IsFactura(inv.Kind) || inv.Status != invoicing.InvoiceStatusAnnulled {
		return invoicing.ErrInvoiceNotReinstatable(inv.Status)
	}
	if w := annulmentWindowOf(inv); w.Open(now) {
		return invoicing.ErrInvoiceReinstateWindowOpen(w.Deadline.Format(time.DateOnly))
	}
	return nil
}

// reinstateLockedRefusal is reinstateRefusal again on the factura as the
// transaction locked it, with the mismatch read under the annulment's lock:
// a check that settled ANULADO, or a second press, between the read and the
// write is refused rather than written over.
func reinstateLockedRefusal(locked *repository.InvoiceRow, facts repository.ReinstateFacts, now time.Time) error {
	if err := reinstateDocumentRefusal(&locked.Invoice, now); err != nil {
		return err
	}
	if !facts.AnnulmentAtMismatch {
		return invoicing.ErrInvoiceReinstateNotMismatch(facts.AnnulmentVerdict)
	}
	return nil
}

// reinstateCreditNote is THE ONE RULE (ADR 0078 section 5): the Credit Note
// a reinstated factura owes, nil when none.
//
//   - A manual Tax Invoice owes none: the platform issues no Credit Note for
//     a document it does not issue.
//   - A factura a live Credit Note already credits owes no second one.
//   - A reversed Sale owes one with the reversal's route as its reason: the
//     SRI is told the sale no longer stands.
//   - Another factura standing for the Sale (an Issue again or Reissue
//     successor) owes one with the supersede reason, so the pair takes a
//     Reissue's normal shape and the Sale never holds two standing
//     facturas.
//   - Otherwise none: the factura is the Sale's current one again.
//
// Pure, so the rule is tabled without a database.
func reinstateCreditNote(factura *invoicing.Invoice, facts repository.ReinstateFacts, now time.Time) *invoicing.Invoice {
	if factura.Kind != invoicing.DocumentKindSale || factura.CreditedByInvoiceID != "" {
		return nil
	}
	reason := ""
	switch {
	case facts.SaleStatus == saleStatusReversed:
		reason = facts.ReversalRoute
	case facts.SaleHasLiveFactura:
		reason = invoicing.CreditNoteReasonReissue
	default:
		return nil
	}
	note := creditNoteOf(factura, reason, now)
	return &note
}

// saleStatusReversed is the Ticket Sale status a reversal leaves.
const saleStatusReversed = "reversed"

// fetchAuthorization asks autorizacionComprobante for the authorization of
// a factura annulled before it stored one, recorded on the attempts ledger
// as a query. Anything but AUTORIZADO with the authorized XML refuses.
func (s *Service) fetchAuthorization(ctx context.Context, row *repository.InvoiceRow) (*repository.FetchedAuthorization, error) {
	callCtx, cancel := context.WithTimeout(ctx, s.pollBudget)
	defer cancel()
	authority := s.authority(row.Invoice.Environment)
	outcome, _, err := s.attempt(callCtx, row.Invoice.ID, invoicing.AttemptQuery, func() (invoicing.Outcome, error) {
		return authority.QueryOutcome(callCtx, row.Ecuador.AccessKey)
	})
	if err != nil || outcome.State != invoicing.OutcomeAuthorized || len(outcome.AuthorityXML) == 0 {
		s.logger.Info("invoicing: reinstate refused; the SRI did not return the factura's authorization",
			"invoice_id", row.Invoice.ID, "outcome", outcome.State, "error", err)
		return nil, invoicing.ErrInvoiceReinstateUnauthorized()
	}
	return &repository.FetchedAuthorization{
		XML:    outcome.AuthorityXML,
		Number: outcome.AuthorizationNumber,
		Date:   outcome.AuthorizationDate,
	}, nil
}

// The reinstatement notice on the Drainer's tick, run right after the
// Annulment Notice's pass and above the flag as that one is: owed when a
// Reinstate followed an Annulment Notice the buyer was sent and no Credit
// Note follows; sent at most once on the delivery's ladder, to the
// factura's frozen Recipient email in the Sale's Locale, as the factura's
// own delivery mail worded as a reinstatement (platform.TaxDocumentDelivery
// with Reinstated), carrying the RIDE and XML again.

// ReinstatementNoticeResult is what one pass did: notices sent, and sends
// refused (each due again on the ladder).
type ReinstatementNoticeResult struct {
	Sent   int `json:"sent"`
	Failed int `json:"failed"`
}

// reinstatementNoticeBudget, reinstatementNoticeBatch and
// reinstatementNoticeLease bound the pass as the Annulment Notice's are.
const (
	reinstatementNoticeBudget = 5 * time.Second
	reinstatementNoticeBatch  = 20
	reinstatementNoticeLease  = 5 * time.Minute
)

// sendReinstatementNotices is the pass: every due notice, one at a time,
// until none is due or the budget is spent. It never fails the tick.
func (s *Service) sendReinstatementNotices(ctx context.Context, budget time.Duration) ReinstatementNoticeResult {
	var out ReinstatementNoticeResult
	wall := time.Now()
	for out.Sent+out.Failed < reinstatementNoticeBatch {
		if ctx.Err() != nil || time.Since(wall) >= budget {
			break
		}
		now := s.clock()
		claim, err := s.repo.ClaimDueReinstatementNotice(ctx, now, now.Add(reinstatementNoticeLease))
		if err != nil {
			s.logger.Warn("invoicing: could not claim a reinstatement notice", "error", err)
			break
		}
		if claim == nil {
			break
		}
		sent, err := s.sendReinstatementNotice(ctx, claim, now)
		if err != nil {
			s.logger.Warn("invoicing: could not record a reinstatement notice; it is due again once its lease expires",
				"invoice_id", claim.Invoice.Invoice.ID, "error", err)
		}
		if sent {
			out.Sent++
		} else {
			out.Failed++
		}
	}
	if out.Sent+out.Failed > 0 {
		s.logger.Info("invoicing: reinstatement notices worked", "sent", out.Sent, "failed", out.Failed)
	}
	return out
}

// sendReinstatementNotice sends one claimed notice and records it sent, or
// reschedules it on the ladder when the sender refused or the mail cannot
// be put together. It reports whether the buyer was mailed.
func (s *Service) sendReinstatementNotice(ctx context.Context, claim *repository.ReinstatementNoticeClaim, now time.Time) (bool, error) {
	row := claim.Invoice
	inv := &row.Invoice
	next := now.Add(annulmentNoticeDelay(claim.Attempts + 1))
	delivery, err := s.taxDocumentDeliveryOf(ctx, row)
	if errors.Is(err, errNotDeliverable) {
		// Stored data that cannot become the mail; on the ladder all the
		// same, so a fix to the data is picked up without an operator.
		s.logger.Error("invoicing: a reinstatement notice cannot be put together", "invoice_id", inv.ID, "error", err)
		return false, s.repo.RescheduleReinstatementNotice(context.WithoutCancel(ctx), claim.AnnulmentID, next)
	}
	if err != nil {
		// The database's: the lease expires and the notice is due again.
		return false, err
	}
	delivery.Reinstated = true
	delivery.Number = FormatNumber(row.Ecuador.Estab, row.Ecuador.PtoEmi, row.Ecuador.Secuencial)
	if err := s.email.SendTaxDocumentDelivery(ctx, *delivery); err != nil {
		s.logger.Warn("invoicing: reinstatement notice failed; due again on the ladder",
			"invoice_id", inv.ID, "next_attempt_at", next, "error", err)
		return false, s.repo.RescheduleReinstatementNotice(context.WithoutCancel(ctx), claim.AnnulmentID, next)
	}
	if _, err := s.repo.MarkReinstatementNoticeSent(context.WithoutCancel(ctx), claim.AnnulmentID, now); err != nil {
		return true, err
	}
	s.logger.Info("invoicing: reinstatement notice sent", "invoice_id", inv.ID, "locale", delivery.Locale)
	return true, nil
}

// ReinstatementNoticeView is the reinstatement notice as the panel's trail
// draws it: `owed`, `sent` with its instant, or `not_owed`.
type ReinstatementNoticeView struct {
	State  string     `json:"state"`
	SentAt *time.Time `json:"sent_at"`
}

// ReinstateView says whether Reinstate is admitted on an annulled factura
// as it stands, or the refusal the press would earn.
type ReinstateView struct {
	Admitted bool         `json:"admitted"`
	Refusal  *RefusalView `json:"refusal"`
}

// reinstateView is the detail's `annulment.reinstate`, nil unless the
// factura is annulled.
func reinstateView(row *repository.InvoiceRow, now time.Time) *ReinstateView {
	if row.Invoice.Status != invoicing.InvoiceStatusAnnulled {
		return nil
	}
	refusal := refusalView(reinstateRefusal(row, now))
	return &ReinstateView{Admitted: refusal == nil, Refusal: refusal}
}

// reinstatementNoticeView reads a reinstated annulment's notice.
func reinstatementNoticeView(r *invoicing.Reinstatement) *ReinstatementNoticeView {
	if r == nil {
		return nil
	}
	return &ReinstatementNoticeView{State: string(r.Notice.State), SentAt: r.Notice.SentAt}
}
