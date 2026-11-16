package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The buyer's Annulment Notice on the Drainer's tick (#748, spec #742, ADR
// 0078; CONTEXT.md's Annulment Notice): the buyer of a Sale Invoice is told,
// once, that a factura they were sent was annulled at the SRI.
//
// OWED AT MARK ANNULLED on a Sale Invoice the buyer was sent (its delivery
// was made), never on a manual Tax Invoice or on a factura the buyer never
// received (repository.AnnulFactura writes which). HELD while the
// annulment's check is checking or at mismatch, so the buyer is never told
// to discard a factura the SRI still holds; RELEASED once it is confirmed or
// unconfirmed (invoicing.AnnulmentNoticeReleased). Nothing is written to
// release it: the claim reads the verdict and the mismatch predicate.
//
// THE PASS RUNS RIGHT AFTER THE ANNULMENT CHECK'S, above the flag as that
// one does, so the tick whose ask reads ANULADO also sends. A manual Tax
// Invoice never owes a notice, so the flag being closed only ever finds
// Sale Invoices annulled before it closed, whose buyers are owed the words
// all the same.
//
// WORDED AT SEND TIME by the Ticket Sale as it then stands: a notice held
// while the Sale stood and sent after an Operator Reversal says nothing
// follows. Sent to the factura's frozen Recipient email, in the Sale's
// Locale with the Customer's as fallback, with the Customer Area link when a
// storefront URL is configured, and no attachments.
//
// AT MOST ONCE, ON THE DELIVERY'S LADDER. A refused send stays owed and is
// due again a minute, five, fifteen, then hourly after each refusal; the
// sent stamp is written only after the sender took it, and is guarded on
// the notice still being owed. As with the delivery mail, an instance that
// dies between the send and the stamp leaves the lease to expire and the
// notice to go again: "once" is per stamped send, not proof against a crash.
//
// A FOLLOW-UP CAN BE MAILED FIRST. Issue again and Reissue are admitted
// while the check is still checking, so the successor factura may reach the
// buyer before the notice is released; the notice's "a new factura will
// follow" then refers to one already in the inbox. Accepted: holding the
// successor on the notice would hold the buyer's valid factura on the SRI's
// slow answer, and the spec gates the follow-ups on mismatch alone.

// annulmentNoticeBudget bounds the pass inside the tick's: no send is
// started once it is spent, and the one in flight is bounded by the mail
// client's own timeout. Kept short because it sits after the check's pass
// in the tick's worst case (annulmentAskBound says the sum). A notice left
// unsent is due on the next tick. annulmentNoticeBatch caps the sends one
// pass tries.
const (
	annulmentNoticeBudget = 5 * time.Second
	annulmentNoticeBatch  = 20
)

// annulmentNoticeLease hides a claimed notice from other ticks: the fallback
// for an instance that died mid-send.
const annulmentNoticeLease = 5 * time.Minute

// AnnulmentNoticeResult is what one pass did: notices sent, and sends the
// sender refused (each due again on the ladder).
type AnnulmentNoticeResult struct {
	Sent   int `json:"sent"`
	Failed int `json:"failed"`
}

// sendAnnulmentNotices is the pass: every due notice, one at a time, until
// none is due or the budget is spent. It never fails the tick.
func (s *Service) sendAnnulmentNotices(ctx context.Context, budget time.Duration) AnnulmentNoticeResult {
	var out AnnulmentNoticeResult
	wall := time.Now()
	for out.Sent+out.Failed < annulmentNoticeBatch {
		if ctx.Err() != nil || time.Since(wall) >= budget {
			break
		}
		now := s.clock()
		claim, err := s.repo.ClaimDueAnnulmentNotice(ctx, now, now.Add(annulmentNoticeLease))
		if err != nil {
			s.logger.Warn("invoicing: could not claim an annulment notice", "error", err)
			break
		}
		if claim == nil {
			break
		}
		sent, err := s.sendAnnulmentNotice(ctx, claim, now)
		if err != nil {
			s.logger.Warn("invoicing: could not record an annulment notice; it is due again once its lease expires",
				"invoice_id", claim.Invoice.Invoice.ID, "error", err)
		}
		if sent {
			out.Sent++
		} else {
			out.Failed++
		}
	}
	if out.Sent+out.Failed > 0 {
		s.logger.Info("invoicing: annulment notices worked", "sent", out.Sent, "failed", out.Failed)
	}
	return out
}

// sendAnnulmentNotice sends one claimed notice and records it sent, or
// reschedules it on the ladder when the sender refused or the Sale cannot
// be read. It reports whether the buyer was mailed.
func (s *Service) sendAnnulmentNotice(ctx context.Context, claim *repository.AnnulmentNoticeClaim, now time.Time) (bool, error) {
	row := claim.Invoice
	inv := &row.Invoice
	next := now.Add(annulmentNoticeDelay(claim.Attempts + 1))
	facts, err := s.repo.GetSaleDeliveryFacts(ctx, inv.TicketSaleID)
	if err != nil || facts == nil || row.Ecuador == nil {
		s.logger.Error("invoicing: an annulment notice cannot be worded; its Sale or its number cannot be read",
			"invoice_id", inv.ID, "error", err)
		return false, s.repo.RescheduleAnnulmentNotice(context.WithoutCancel(ctx), claim.AnnulmentID, next)
	}
	notice := platform.AnnulmentNotice{
		To:           inv.Recipient.Email,
		CustomerName: inv.Recipient.LegalName,
		EventName:    facts.EventName,
		Reference:    inv.SaleConfirmationRef,
		Number:       FormatNumber(row.Ecuador.Estab, row.Ecuador.PtoEmi, row.Ecuador.Secuencial),
		SaleReversed: facts.Reversed,
		Locale:       platform.ResolveMailLocale(facts.SaleLocale, facts.CustomerLocale),
	}
	if s.storefrontBaseURL != "" {
		notice.CustomerAreaURL = s.storefrontBaseURL + customerAreaPath + "#sale-" + inv.TicketSaleID
	}
	if err := s.email.SendAnnulmentNotice(ctx, notice); err != nil {
		s.logger.Warn("invoicing: annulment notice failed; due again on the ladder",
			"invoice_id", inv.ID, "next_attempt_at", next, "error", err)
		return false, s.repo.RescheduleAnnulmentNotice(context.WithoutCancel(ctx), claim.AnnulmentID, next)
	}
	if _, err := s.repo.MarkAnnulmentNoticeSent(context.WithoutCancel(ctx), claim.AnnulmentID, now); err != nil {
		return true, err
	}
	s.logger.Info("invoicing: annulment notice sent", "invoice_id", inv.ID,
		"sale_reversed", notice.SaleReversed, "locale", notice.Locale)
	return true, nil
}

// annulmentNoticeDelay is the delivery's ladder (SaleInvoiceLadder) by the
// number of sends tried: a minute after the first refusal, five after the
// second, fifteen after the third, then hourly.
func annulmentNoticeDelay(tried int) time.Duration {
	i := min(max(tried, 1), len(SaleInvoiceLadder)) - 1
	return SaleInvoiceLadder[i]
}

// AnnulmentNoticeView is the buyer's Annulment Notice on one annulment, as
// the panel's trail draws it (#748): `held` (owed, waiting for the SRI to
// report ANULADO: the check is checking or at mismatch), `due` (owed and
// released, waiting for the next tick or a retry), `sent` with its instant,
// `dropped` (a Reinstate withdrew it) or `not_owed`.
type AnnulmentNoticeView struct {
	State  string     `json:"state"`
	SentAt *time.Time `json:"sent_at"`
}

// The notice's states as the detail names them.
const (
	annulmentNoticeHeld = "held"
	annulmentNoticeDue  = "due"
)

// annulmentNoticeView reads one annulment's notice, held or due by its
// check's verdict.
func annulmentNoticeView(a *invoicing.Annulment) *AnnulmentNoticeView {
	state := string(a.Notice.State)
	if a.Notice.State == invoicing.AnnulmentNoticeOwed {
		state = annulmentNoticeHeld
		if a.ReinstatedAt == nil && invoicing.AnnulmentNoticeReleased(a.Check) {
			state = annulmentNoticeDue
		}
	}
	return &AnnulmentNoticeView{State: state, SentAt: a.Notice.SentAt}
}
