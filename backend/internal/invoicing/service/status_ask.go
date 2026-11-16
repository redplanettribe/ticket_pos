package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
)

// One ask of the authority's status service (#744, spec #742, ADR 0078).
//
// ConsultaComprobante answers wrongly about two times in ten (#716): it
// refuses a document it holds with "fuera del rango permitido", and its edge
// redirects to an HTML page. So the unit the platform reasons with is not one
// call but ONE ASK: up to StatusAskCalls calls, StatusAskSpacing apart,
// stopping at the first real reading (annulled or authorized). An ask whose
// every call was no reading is itself no reading, and that is an answer,
// never an error: nothing about the SRI's unreliability may abort the ask's
// caller - Check status today, the Annulment Check and the Closing Sweep
// after it.
//
// EVERY CALL IS A ROW ON THE ATTEMPTS LEDGER, under AttemptStatus and never
// AttemptQuery, with its reading as the outcome, the SRI's messages, the
// error and the timing. Being its own operation is what keeps it out of
// Abandon's fresh-Check rule, which reads queries only.

// StatusAskCalls is how many calls one ask may make.
const StatusAskCalls = 3

// DefaultStatusAskSpacing is the pause between two calls of one ask: the
// spacing the #716 probe found answers 8 in 10. Tests collapse it
// (WithStatusAskSpacing).
const DefaultStatusAskSpacing = 5 * time.Second

// WithStatusAskSpacing replaces the pause between the calls of one ask
// (tests).
func (s *Service) WithStatusAskSpacing(d time.Duration) *Service {
	s.statusAskSpacing = d
	return s
}

// statusAsk is what one ask learned.
type statusAsk struct {
	// Reading is the first real reading, or StatusNone when no call had one.
	Reading invoicing.StatusReading
	// Calls is how many calls the ask made.
	Calls int
	// LastAttemptID is the ledger row of the last call (zero if its write
	// failed): the answer the ask ended on.
	LastAttemptID int64
}

// askStatus makes one ask about a signed document. A cancelled context ends
// it early with what it has; it never returns an error.
func (s *Service) askStatus(ctx context.Context, row *repository.InvoiceRow) statusAsk {
	authority := s.authority(row.Invoice.Environment)
	ask := statusAsk{Reading: invoicing.StatusNone}
	for ask.Calls < StatusAskCalls {
		if ask.Calls > 0 {
			select {
			case <-time.After(s.statusAskSpacing):
			case <-ctx.Done():
				return ask
			}
		}
		answer, id := s.statusAttempt(ctx, row.Invoice.ID, func() invoicing.StatusAnswer {
			return authority.QueryStatus(ctx, row.Ecuador.AccessKey)
		})
		ask.Calls++
		ask.LastAttemptID = id
		if answer.Reading.Real() {
			ask.Reading = answer.Reading
			return ask
		}
	}
	return ask
}

// statusAttempt makes one call to the status service and writes it to the
// ledger whatever it answered, detached from the caller's cancellation as
// every attempt is (attempt). A reading the adapter could not make, an
// unrecognised state among them, is logged with why.
func (s *Service) statusAttempt(ctx context.Context, invoiceID string, call func() invoicing.StatusAnswer) (invoicing.StatusAnswer, int64) {
	started := s.clock()
	wall := time.Now()
	answer := call()
	if answer.Reading == "" {
		answer.Reading = invoicing.StatusNone
	}
	a := invoicing.Attempt{
		InvoiceID: invoiceID,
		Operation: invoicing.AttemptStatus,
		Outcome:   string(answer.Reading),
		Messages:  answer.Messages,
		Error:     answer.Error,
		StartedAt: started,
		Duration:  time.Since(wall),
	}
	if answer.Error != "" {
		s.logger.Warn("invoicing: tax authority status call had no reading", "invoice_id", invoiceID, "error", answer.Error)
	}
	recorded, err := s.repo.RecordAttempt(context.WithoutCancel(ctx), a)
	if err != nil {
		s.logger.Error(ctx, "invoicing: could not record attempt", "invoice_id", invoiceID, "operation", a.Operation, "error", err)
	}
	return answer, recorded
}
