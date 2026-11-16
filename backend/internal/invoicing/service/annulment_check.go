package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
)

// The Annulment Check's pass on the Drainer's tick (#746, spec #742, ADR
// 0078): after an operator records an annulment, the platform asks the
// SRI's ConsultaComprobante on a schedule and reaches a verdict - confirmed,
// mismatch or unconfirmed - by the one pure rule
// (invoicing.AnnulmentVerdictOf), never trusting one negative answer.
//
// IT RUNS ABOVE THE FLAG, exactly as the Certificate Expiry Warning does
// (ADR 0063 section 1): DrainSaleInvoices works it before it asks whether
// SALE_INVOICING_ENABLED is open, because a manual Tax Invoice is annulled
// and must be checked with the flag closed. No new endpoint, scheduler job
// or Terraform: the 5-minute drain schedule already exceeds the +1 min
// rung, so "+1 min" means "due from +1 min". The checkout-kicked round does
// not run it.
//
// A BUDGET OF ITS OWN INSIDE THE TICK'S. The pass stops starting asks at
// annulmentCheckBudget and every ask runs under the pass's deadline, so the
// pass as a whole ends by it; the drain that follows gets what is left of
// the tick's budget. An ask the deadline cut short, with no real reading, is
// NO ASK: its calls stay on the attempts ledger as evidence, but it is not
// recorded on the check, the claim is released and the same ask is due on
// the next tick. A truncated ask that counted would let one call stand for
// the +60 min ask.
//
// CLAIMED UNDER A LEASE (repository.ClaimDueAnnulmentCheck), one annulment
// at a time, so two concurrent ticks never ask about one factura twice.
//
// ONLY ConsultaComprobante, through askStatus: autorización returns the
// stored authorization record, which may keep saying AUTORIZADO after an
// annulment and would make a permanent false mismatch.
//
// CHECK STATUS ON AN ANNULLED FACTURA IS ONE MORE ASK (checkAnnulled): it
// feeds the same verdict rule and leaves the schedule alone. The rule's own
// +30 min floor is what keeps two quick presses from faking a mismatch.
//
// An annulment recorded before the check existed has none: it reads
// unconfirmed, is never claimed, and a Check status on it asks but records
// nothing on a check.

// annulmentCheckBudget bounds the pass inside the tick's
// saleInvoiceDrainBudget: asks are started until it and cut at it.
// annulmentCheckBatch is the belt beside it.
const (
	annulmentCheckBudget = 20 * time.Second
	annulmentCheckBatch  = 20
)

// annulmentCheckLease is how long a claimed check is hidden from other
// ticks: the fallback for an instance that died mid-ask. An ordinary ask
// frees it within seconds.
const annulmentCheckLease = 5 * time.Minute

// AnnulmentCheckResult is what one pass did: how many asks it made, the
// verdict each left its check at, and how many it could not finish (the
// budget cut the ask short, or the database refused the record; either way
// the same ask is due again).
type AnnulmentCheckResult struct {
	Asked       int `json:"asked"`
	Checking    int `json:"checking"`
	Confirmed   int `json:"confirmed"`
	Mismatch    int `json:"mismatch"`
	Unconfirmed int `json:"unconfirmed"`
	Unfinished  int `json:"unfinished"`
}

// checkAnnulments is the pass: every due ask, one at a time, until none is
// due or the budget is spent. It never fails the tick; a database error is
// logged and the pass ends with what it has.
func (s *Service) checkAnnulments(ctx context.Context, budget time.Duration) AnnulmentCheckResult {
	var out AnnulmentCheckResult
	wall := time.Now()
	passCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	deadline := s.clock().Add(budget)

	for out.Asked+out.Unfinished < annulmentCheckBatch {
		if passCtx.Err() != nil {
			break
		}
		now := s.clock()
		// On the wall as well as on the service clock: the clock is fixed in
		// tests, as the drain's budget says.
		if !now.Before(deadline) || time.Since(wall) >= budget {
			break
		}
		claim, err := s.repo.ClaimDueAnnulmentCheck(passCtx, now, now.Add(annulmentCheckLease))
		if err != nil {
			s.logger.Warn("invoicing: could not claim an annulment check", "error", err)
			break
		}
		if claim == nil {
			break
		}
		if claim.Invoice.Ecuador == nil {
			// An annulled factura is signed, or it could not have been
			// annulled; one without its Ecuador row is a fault, left leased.
			s.logger.Error("invoicing: annulled factura has no clave to ask about", "invoice_id", claim.Invoice.Invoice.ID)
			out.Unfinished++
			continue
		}
		at := now
		ask := s.askStatus(passCtx, claim.Invoice)
		if cutShort(passCtx, ask) {
			if err := s.repo.ReleaseAnnulmentCheck(context.WithoutCancel(ctx), claim.AnnulmentID); err != nil {
				s.logger.Warn("invoicing: could not release a cut annulment check; its lease will", "invoice_id", claim.Invoice.Invoice.ID, "error", err)
			}
			out.Unfinished++
			break
		}
		check, err := s.recordAnnulmentAsk(context.WithoutCancel(ctx), claim.Invoice, claim.AnnulmentID, at, ask, false)
		if err != nil || check == nil {
			s.logger.Warn("invoicing: could not record an annulment ask; it is due again once its lease expires", "invoice_id", claim.Invoice.Invoice.ID, "error", err)
			out.Unfinished++
			continue
		}
		out.Asked++
		switch check.Verdict {
		case invoicing.AnnulmentConfirmed:
			out.Confirmed++
		case invoicing.AnnulmentMismatch:
			out.Mismatch++
		case invoicing.AnnulmentUnconfirmed:
			out.Unconfirmed++
		default:
			out.Checking++
		}
		s.logger.Info("invoicing: annulment checked", "invoice_id", claim.Invoice.Invoice.ID,
			"reading", ask.Reading, "calls", ask.Calls, "verdict", check.Verdict)
	}
	if out.Asked+out.Unfinished > 0 {
		s.logger.Info("invoicing: annulment checks worked", "asked", out.Asked, "checking", out.Checking,
			"confirmed", out.Confirmed, "mismatch", out.Mismatch, "unconfirmed", out.Unconfirmed, "unfinished", out.Unfinished)
	}
	return out
}

// cutShort reports whether an ask ended because its context did, before it
// made every call and without a real reading: no ask at all.
func cutShort(ctx context.Context, ask statusAsk) bool {
	return ctx.Err() != nil && !ask.Reading.Real() && ask.Calls < StatusAskCalls
}

// recordAnnulmentAsk writes one finished ask on an annulment's check; a
// manual one (Check status) leaves the schedule alone.
func (s *Service) recordAnnulmentAsk(ctx context.Context, row *repository.InvoiceRow, annulmentID string, at time.Time, ask statusAsk, manual bool) (*invoicing.AnnulmentCheck, error) {
	return s.repo.RecordAnnulmentAsk(ctx, annulmentID, repository.AnnulmentAskRecord{
		Ask:            invoicing.AnnulmentAsk{At: at, Reading: ask.Reading, Manual: manual},
		Calls:          ask.Calls,
		WindowClosesAt: annulmentWindowOf(&row.Invoice).ClosesAt,
	})
}

// currentAnnulment is a factura's standing annulment, the one no Reinstate
// has closed, or nil.
func currentAnnulment(row *repository.InvoiceRow) *invoicing.Annulment {
	for i := range row.Annulments {
		if row.Annulments[i].ReinstatedAt == nil {
			return &row.Annulments[i]
		}
	}
	return nil
}

// AnnulmentCheckView is where an annulled factura's Annulment Check stands,
// as the detail page's panel draws it (#746): the verdict, and the instant
// of the ask that reached it (null while checking); when the next scheduled
// ask falls due (null when none will) and when the schedule ends; every ask
// made, oldest first; and every answer behind them - each ConsultaComprobante
// call on the ledger since the annulment was recorded, with the SRI's raw
// messages and the error on a no reading.
//
// Scheduled is false for an annulment recorded before the check existed: it
// reads unconfirmed, has no asks of its own and is never asked by the tick.
type AnnulmentCheckView struct {
	Verdict   string                `json:"verdict"`
	Scheduled bool                  `json:"scheduled"`
	Since     *time.Time            `json:"since"`
	NextAskAt *time.Time            `json:"next_ask_at"`
	AsksUntil *time.Time            `json:"asks_until"`
	Asks      []AnnulmentAskView    `json:"asks"`
	Answers   []AnnulmentAnswerView `json:"answers"`
}

// AnnulmentAskView is one ask of a check: when, what it read (annulled,
// authorized or none) and whether an operator's Check status made it.
type AnnulmentAskView struct {
	AskedAt time.Time `json:"asked_at"`
	Reading string    `json:"reading"`
	Manual  bool      `json:"manual"`
}

// AnnulmentAnswerView is one call to the authority's status service: the
// attempt, when it started, the service asked (always ConsultaComprobante),
// its reading, and on a no reading the SRI's own messages and the reason
// the platform could not read it.
type AnnulmentAnswerView struct {
	AttemptID int64                  `json:"attempt_id"`
	StartedAt time.Time              `json:"started_at"`
	Service   string                 `json:"service"`
	Reading   string                 `json:"reading"`
	Messages  []AuthorityMessageView `json:"messages"`
	Error     *string                `json:"error"`
}

// statusServiceName is the service every answer of a check is from.
const statusServiceName = "ConsultaComprobante"

// annulmentCheckView is the check of an annulled factura's standing
// annulment, or the unconfirmed, never-scheduled check of one recorded
// before the check existed; nil on a factura that is not annulled.
func annulmentCheckView(row *repository.InvoiceRow) *AnnulmentCheckView {
	if row.Invoice.Status != invoicing.InvoiceStatusAnnulled {
		return nil
	}
	view := &AnnulmentCheckView{
		Verdict: string(invoicing.AnnulmentUnconfirmed),
		Asks:    []AnnulmentAskView{},
		Answers: []AnnulmentAnswerView{},
	}
	from := row.Invoice.AnnulledAt
	current := currentAnnulment(row)
	if current != nil {
		at := current.AnnulledAt
		from = &at
	}
	if current != nil && current.Check != nil {
		c := current.Check
		view.Verdict = string(c.Verdict)
		view.Scheduled = true
		view.Since = c.Since
		view.NextAskAt = c.NextAskAt
		if c.Verdict != invoicing.AnnulmentConfirmed {
			until := invoicing.AnnulmentChecksUntil(current.AnnulledAt, annulmentWindowOf(&row.Invoice).ClosesAt)
			view.AsksUntil = &until
		}
		for _, a := range c.Asks {
			view.Asks = append(view.Asks, AnnulmentAskView{AskedAt: a.At, Reading: string(a.Reading), Manual: a.Manual})
		}
	}
	for _, a := range row.Attempts {
		if a.Operation != invoicing.AttemptStatus || (from != nil && a.StartedAt.Before(*from)) {
			continue
		}
		answer := AnnulmentAnswerView{
			AttemptID: a.ID,
			StartedAt: a.StartedAt,
			Service:   statusServiceName,
			Reading:   a.Outcome,
			Messages:  []AuthorityMessageView{},
		}
		if a.Outcome == string(invoicing.StatusNone) {
			answer.Messages = messagesView(a.Messages)
			answer.Error = optional(a.Error)
		}
		view.Answers = append(view.Answers, answer)
	}
	return view
}
