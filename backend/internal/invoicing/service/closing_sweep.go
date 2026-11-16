package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
	"github.com/peter/ticket_pos/backend/internal/invoicing/sri"
)

// The Closing Sweep on the Drainer's tick, and the Unrecorded Annulment it
// flags (#750, spec #742, ADR 0078).
//
// ONCE A MONTH, from 00:00 Guayaquil on day 8 of the following month through
// the end of day 12 (invoicing.ClosingSweepRun), the tick asks the SRI's
// ConsultaComprobante about every production factura of the month just
// closed that the platform holds as authorized, credited ones included
// (repository.sweepPopulationSQL). Nothing can be annulled once a factura's
// Annulment Window has closed, so ONE READING PER FACTURA IS FINAL:
// AUTORIZADO marks it swept, ANULADO marks it swept and raises an Unrecorded
// Annulment. A no reading leaves it due again on a later tick, never the
// same one; once day 12 is over it is UNSWEPT, counted on the Operator
// Dashboard (ClosingSweepStatus) until an operator's Sweep again makes just
// those due on the next tick.
//
// PACED AS THE ANNULMENT CHECK, never a burst: one ask at a time (askStatus,
// the same up-to-three calls, spaced), the next factura's ask only after the
// status-ask spacing, at most closingSweepBatch asks a tick, claimed under a
// lease so two concurrent ticks never ask about one factura. It SHARES the
// Annulment Check's budget: it starts asks only in what the check's pass
// left of annulmentCheckBudget, so the tick's worst case is unchanged
// (annulmentAskBound past that budget). The check goes first: its rungs are
// time-sensitive, the sweep has five days.
//
// IT RUNS ABOVE THE FLAG, as the check does (ADR 0063 section 1): manual Tax
// Invoices are declared with Sale Invoicing closed.
//
// THE PLATFORM NEVER RECORDS THE ANNULMENT ITSELF. `annulled` always has an
// author and the follow-up stays an operator's choice: the flag puts the
// factura in the needs-attention queue and warns on its list row and detail,
// and nothing changes for the buyer. Mark annulled records it (its check
// then starts confirmed with no asks, repository.AnnulFactura), or, where a
// live Credit Note refuses Mark annulled, an operator acknowledges it
// (AcknowledgeUnrecordedAnnulment): the entry clears and the flag stays for
// the archive.

// closingSweepBatch is the most asks one tick's sweep makes.
const closingSweepBatch = 10

// closingSweepLease hides a claimed factura from other ticks: the fallback
// for an instance that died mid-ask.
const closingSweepLease = 5 * time.Minute

// ClosingSweepResult is what one tick's sweep did: the asks it made and how
// each read - authorized (swept), annulled (swept and flagged), none (due
// again on a later tick) - and how many it could not finish (cut short, or
// the database refused the record; due again either way).
type ClosingSweepResult struct {
	Asked      int `json:"asked"`
	Authorized int `json:"authorized"`
	Annulled   int `json:"annulled"`
	NoReading  int `json:"no_reading"`
	Unfinished int `json:"unfinished"`
}

// sweepClosedMonth is the tick's sweep: due asks one at a time, spaced,
// until none is due, the batch is made or the budget is spent. It never
// fails the tick.
func (s *Service) sweepClosedMonth(ctx context.Context, budget time.Duration) ClosingSweepResult {
	var out ClosingSweepResult
	wall := time.Now()
	tickStart := s.clock()
	deadline := tickStart.Add(budget)
	var runMonth *time.Time
	if month, running := invoicing.ClosingSweepRun(tickStart, sri.Guayaquil); running {
		runMonth = &month
	}

	for out.Asked+out.Unfinished < closingSweepBatch {
		if out.Asked+out.Unfinished > 0 {
			// Paced: the next factura's ask waits the spacing between calls,
			// so the sweep is never a burst against the SRI.
			select {
			case <-time.After(s.statusAskSpacing):
			case <-ctx.Done():
			}
		}
		if ctx.Err() != nil {
			break
		}
		now := s.clock()
		if !now.Before(deadline) || time.Since(wall) >= budget {
			break
		}
		row, err := s.repo.ClaimDueSweep(ctx, now, tickStart, now.Add(closingSweepLease), runMonth)
		if err != nil {
			s.logger.Warn("invoicing: could not claim a closing sweep ask", "error", err)
			break
		}
		if row == nil {
			break
		}
		invoiceID := row.Invoice.ID
		if row.Ecuador == nil {
			s.logger.Error("invoicing: authorized factura has no clave to sweep", "invoice_id", invoiceID)
			out.Unfinished++
			continue
		}
		askCtx, cancel := context.WithTimeout(ctx, annulmentAskBound)
		ask := s.askStatus(askCtx, row)
		cut := cutShort(askCtx, ask)
		cancel()
		if cut {
			if err := s.repo.ReleaseSweep(context.WithoutCancel(ctx), invoiceID); err != nil {
				s.logger.Warn("invoicing: could not release a cut sweep ask; its lease will", "invoice_id", invoiceID, "error", err)
			}
			out.Unfinished++
			break
		}
		recorded, err := s.repo.RecordSweepAsk(context.WithoutCancel(ctx), invoiceID, now, ask.Reading, ask.LastAttemptID)
		if err != nil {
			s.logger.Warn("invoicing: could not record a sweep ask; it is due again once its lease expires", "invoice_id", invoiceID, "error", err)
			out.Unfinished++
			continue
		}
		if !recorded {
			// Annulled by an operator under the ask: out of the population.
			out.Unfinished++
			continue
		}
		out.Asked++
		switch ask.Reading {
		case invoicing.StatusAnnulled:
			out.Annulled++
			s.logger.Warn("invoicing: closing sweep found an unrecorded annulment", "invoice_id", invoiceID, "calls", ask.Calls)
		case invoicing.StatusAuthorized:
			out.Authorized++
		default:
			out.NoReading++
		}
	}
	if out.Asked+out.Unfinished > 0 {
		s.logger.Info("invoicing: closing sweep worked", "asked", out.Asked, "authorized", out.Authorized,
			"annulled", out.Annulled, "no_reading", out.NoReading, "unfinished", out.Unfinished)
	}
	return out
}

// ClosingSweepStatus is the Operator Dashboard's line (#750): the latest
// month whose sweep has ended ("YYYY-MM"), how many of its facturas
// couldn't be swept (no reading yet), and how many of those an operator's
// Sweep again has made due that no tick has asked yet. Zero unswept is the
// ordinary answer, and the dashboard then says nothing.
type ClosingSweepStatus struct {
	Month         string `json:"month"`
	UnsweptCount  int    `json:"unswept_count"`
	DueAgainCount int    `json:"due_again_count"`
}

// GetClosingSweepStatus counts the latest ended month's unswept facturas.
func (s *Service) GetClosingSweepStatus(ctx context.Context) (*ClosingSweepStatus, error) {
	month := invoicing.ClosingSweepLastEnded(s.clock(), sri.Guayaquil)
	n, err := s.repo.CountUnswept(ctx, month)
	if err != nil {
		return nil, err
	}
	return &ClosingSweepStatus{Month: month.Format("2006-01"), UnsweptCount: n.Unswept, DueAgainCount: n.DueAgain}, nil
}

// SweepAgain makes the latest ended month's unswept facturas, and just
// those, due again for the next tick, and answers the dashboard's line as
// it then stands. Safe to press twice; with nothing unswept it does
// nothing.
func (s *Service) SweepAgain(ctx context.Context, by string) (*ClosingSweepStatus, error) {
	now := s.clock()
	month := invoicing.ClosingSweepLastEnded(now, sri.Guayaquil)
	n, err := s.repo.SweepAgain(ctx, month, now)
	if err != nil {
		return nil, err
	}
	s.logger.Info("invoicing: closing sweep due again", "month", month.Format("2006-01"), "facturas", n, "by", by)
	return s.GetClosingSweepStatus(ctx)
}

// AcknowledgeUnrecordedAnnulment records that an operator has seen a
// factura's Unrecorded Annulment where a live Credit Note (or a live
// Reissue successor) keeps Mark annulled from recording it: who and when.
// The queue entry clears; the flag stays, for the archive, and the
// platform's record stays authorized and credited. Whether that Credit
// Note holds against a factura the SRI holds as annulled is the
// accountant's question.
func (s *Service) AcknowledgeUnrecordedAnnulment(ctx context.Context, id, by string) (*InvoiceDetail, error) {
	row, err := s.repo.GetInvoice(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, invoicing.ErrInvoiceNotFound()
	}
	if err := acknowledgeRefusal(row); err != nil {
		return nil, err
	}
	done, err := s.repo.AcknowledgeUnrecordedAnnulment(ctx, id, by, s.clock())
	if err != nil {
		return nil, err
	}
	if !done {
		// The row moved between the read and the guarded write: say what it
		// now earns.
		fresh, err := s.repo.GetInvoice(ctx, id)
		if err != nil {
			return nil, err
		}
		if fresh != nil {
			if refusal := acknowledgeRefusal(fresh); refusal != nil {
				return nil, refusal
			}
		}
		return nil, invoicing.ErrInvoiceNoUnrecordedAnnulment()
	}
	s.logger.Info("invoicing: unrecorded annulment acknowledged", "invoice_id", id, "acknowledged_by", by)
	return s.GetInvoice(ctx, id)
}

// acknowledgeRefusal says whether an Unrecorded Annulment may be
// acknowledged on the document as it stands: it carries one (an authorized
// factura the sweep flagged), it is not acknowledged yet, and Mark annulled
// is refused on it - otherwise recording the annulment is the true act.
func acknowledgeRefusal(row *repository.InvoiceRow) error {
	inv := &row.Invoice
	if !invoicing.IsFactura(inv.Kind) || inv.Status != invoicing.InvoiceStatusAuthorized || inv.UnrecordedAnnulment == nil {
		return invoicing.ErrInvoiceNoUnrecordedAnnulment()
	}
	if inv.UnrecordedAnnulment.AcknowledgedAt != nil {
		return invoicing.ErrInvoiceUnrecordedAnnulmentAcknowledged()
	}
	if annulFacturaRefusal(row) == nil {
		return invoicing.ErrInvoiceAnnulInstead()
	}
	return nil
}

// UnrecordedAnnulmentView is the warning a flagged factura carries on its
// list row and detail (#750): when the sweep found the SRI reporting it
// annulled, the call that read it, and the acknowledgement, null until
// given. Null on every factura the sweep has not flagged, and on one Mark
// annulled has recorded.
type UnrecordedAnnulmentView struct {
	FoundAt        time.Time  `json:"found_at"`
	AttemptID      *int64     `json:"attempt_id"`
	AcknowledgedBy *string    `json:"acknowledged_by"`
	AcknowledgedAt *time.Time `json:"acknowledged_at"`
}

func unrecordedAnnulmentView(u *invoicing.UnrecordedAnnulment) *UnrecordedAnnulmentView {
	if u == nil {
		return nil
	}
	v := &UnrecordedAnnulmentView{
		FoundAt:        u.FoundAt.UTC(),
		AcknowledgedBy: optional(u.AcknowledgedBy),
		AcknowledgedAt: optionalTime(u.AcknowledgedAt),
	}
	if u.AttemptID != 0 {
		id := u.AttemptID
		v.AttemptID = &id
	}
	return v
}

// AcknowledgeView is the detail's `annulment.acknowledge` (#750): whether
// the acknowledgement is admitted on a flagged factura as it stands, or the
// refusal the press would earn. Null on a factura carrying no Unrecorded
// Annulment.
type AcknowledgeView struct {
	Admitted bool         `json:"admitted"`
	Refusal  *RefusalView `json:"refusal"`
}

func acknowledgeView(row *repository.InvoiceRow) *AcknowledgeView {
	if row.Invoice.UnrecordedAnnulment == nil || row.Invoice.Status != invoicing.InvoiceStatusAuthorized {
		return nil
	}
	refusal := refusalView(acknowledgeRefusal(row))
	return &AcknowledgeView{Admitted: refusal == nil, Refusal: refusal}
}
