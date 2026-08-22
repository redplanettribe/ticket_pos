package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/sales"
)

// The Abandoned Answer Purge: the Answers a buyer typed into a checkout that
// never became a sale, deleted 30 days later (#316, ADR 0044).
//
// It is a job in the shape this backend already has one — an internal endpoint a
// scheduler calls and a human can curl — and not a goroutine. Nothing in this
// process outlives a request: the API runs with min_instances 0 and cpu_idle, so
// a ticker would fire only while somebody happened to be browsing, which is
// exactly the reason ADR 0024 gave the Reversal Reconciler an endpoint instead.
//
// WHAT MAKES THIS DIFFERENT FROM THE RECONCILER, and why it is fifty lines
// rather than four hundred: there is no queue, no claim, no lease, no backoff
// and no time budget. The Reconciler talks to a Payment Provider one stuck
// request at a time, and every one of those mechanisms exists to stop two ticks
// refunding the same buyer twice. This run issues ONE DELETE against rows matched
// by a predicate. Two ticks overlapping delete disjoint sets and both report
// honestly; a tick killed halfway leaves the rows it had already deleted deleted
// and the rest for the next run. Adding a claim table here would add a way for
// the job to get stuck, in exchange for nothing.
//
// IT IS NOT GATED ON TICKET_QUESTIONS_ENABLED, deliberately, and this is the one
// decision in the file that will look like an oversight to somebody tidying up.
// That flag gates COLLECTION — the authoring surface and the checkout's answer
// section (ADR 0045) — and the purge is the promise the platform made about the
// data collection produces. A deployment that turned the flag off after a month
// of answers would, if this were gated, keep every one of those answers forever
// precisely because it had stopped asking. The switch that turns a deletion off
// must never be the switch that turns collection off.

// AnswerPurgeResult is what one run did, and what is left.
//
// A tally rather than a bare 200, on the Reversal Reconciler's terms: this
// endpoint is the runbook as much as it is the automation's entry point, and an
// operator running it by hand needs to be able to tell "nothing was due" from
// "the query matched nothing because nothing is there".
type AnswerPurgeResult struct {
	// AnswersPurged is how many held Answers this run deleted. Zero is the
	// ordinary answer, and on a platform where the feature ships dark it is the
	// only answer.
	AnswersPurged int `json:"answers_purged"`
	// PaymentsPurged is how many Payments those Answers came off, which is the
	// figure that means something in human terms: forty answers off one abandoned
	// cart of twenty tickets is one buyer changing their mind, and forty off forty
	// Payments is a month of ordinary attrition.
	PaymentsPurged int `json:"payments_purged"`
	// Cutoff is the moment the window closed for this run (RFC3339, UTC): every
	// Payment begun at or before it that is not approved lost its Answers. It is
	// echoed back because the window is the whole correctness argument — an
	// operator staring at an unexpected count should be able to see, without a
	// deploy or a database session, which 30 days the job actually used.
	Cutoff string `json:"cutoff"`
	// AnswersHeld is how many Answers are riding Payments across the platform once
	// this run finished — the standing backlog, in the Reconciler's sense. It is
	// what makes two runs a day apart legible: rising means the checkout is
	// capturing, flat at zero means the flag is closed and there is nothing here
	// to purge.
	AnswersHeld int `json:"answers_held"`
}

// PurgeAbandonedAnswers deletes the Answers held on Payments that never reached
// 'approved' and are older than sales.AbandonedAnswerRetention.
//
// The cutoff is computed HERE, from this service's clock, and is never taken
// from the caller. That is the internal namespace's standing rule — a caller who
// could name the window could purge every Answer on the platform by asking for a
// cutoff of tomorrow — and it is the same reason the Follow Digest's enqueue
// will not let a caller name a week.
//
// A failure is returned rather than swallowed: unlike the Reconciler, which
// works a queue item at a time and must survive one bad row, this run is a single
// statement that either happened or did not, and a scheduler that is told 200
// when the delete failed would hide a retention promise quietly breaking. The
// backlog read afterwards is the one exception — it is reporting, not the job,
// and a run that deleted correctly must not be reported as failed because a
// COUNT(*) did not come back.
func (s *Service) PurgeAbandonedAnswers(ctx context.Context) (*AnswerPurgeResult, error) {
	cutoff := s.now().Add(-sales.AbandonedAnswerRetention).UTC()

	answers, payments, err := s.repo.PurgeAbandonedCheckoutAnswers(ctx, cutoff)
	if err != nil {
		return nil, err
	}

	result := &AnswerPurgeResult{
		AnswersPurged:  int(answers),
		PaymentsPurged: int(payments),
		Cutoff:         cutoff.Format(time.RFC3339),
	}

	held, err := s.repo.CountHeldCheckoutAnswers(ctx)
	if err != nil {
		// Logged and dropped. The deletion above is the job and it succeeded; the
		// backlog is context for whoever is reading the response.
		s.logger.Error("answer purge backlog read failed", "error", err)
	} else {
		result.AnswersHeld = int(held)
	}

	// One line per run, and it is the only record that a run happened at all:
	// nothing is written to the database by a purge that deleted nothing, so
	// without this an operator asking "is the job running" has no way to tell it
	// from a paused scheduler.
	s.logger.Info("abandoned checkout answers purged",
		"answers_purged", result.AnswersPurged,
		"payments_purged", result.PaymentsPurged,
		"cutoff", result.Cutoff,
		"answers_held", result.AnswersHeld,
	)
	return result, nil
}
