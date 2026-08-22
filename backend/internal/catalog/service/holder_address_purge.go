package service

import (
	"context"
	"time"
)

// The Holder Address Purge: an address a friend typed, that its owner never
// accepted, gets a guaranteed end when the Event starts (#331, parent #322,
// ADR 0046).
//
// WHY THIS JOB IS WHAT MAKES THE WHOLE FEATURE DEFENSIBLE. Every other piece of
// personal data on this platform belongs to somebody who came here: a buyer who
// transacted, a Member who signed in, a Customer who proved an address. A holder
// address is the one exception — it is a contact detail for a person who has
// never been here, supplied at the word of somebody with no authority to supply
// it, and until an Assignment Link is clicked they do not know it is held. ADR
// 0046 priced that cost explicitly and this job is the payment: the address has
// an end, it arrives without anybody asking for it, and what is left afterwards
// is the record without the liability.
//
// IT IS A JOB IN THE SHAPE THIS BACKEND ALREADY HAS ONE — an internal endpoint a
// scheduler calls and a human can curl — and not a goroutine. Nothing in this
// process outlives a request: the API runs with min_instances 0 and cpu_idle, so
// a ticker would fire only while somebody happened to be browsing, which is the
// reason ADR 0024 gave the Reversal Reconciler an endpoint instead and the
// reason the Abandoned Answer Purge has one.
//
// THERE IS NO QUEUE, NO CLAIM, NO LEASE AND NO TIME BUDGET, exactly as there is
// none in the Abandoned Answer Purge and unlike the Reversal Reconciler. This
// run issues ONE UPDATE against rows matched by a predicate: two ticks
// overlapping write disjoint sets and both report honestly, and a tick killed
// halfway leaves the rest for the next run.
//
// IT IS NOT GATED ON TICKET_ASSIGNMENT_ENABLED, deliberately, and this is the
// one decision in the file that will look like an oversight to somebody tidying
// up. Read it beside TicketSalesDueAnswerReminder, which IS gated, and the line
// between them is what a run produces: that one SENDS, and a send is the thing a
// flag exists to hold back. This one DELETES, and it is the promise the platform
// made about data the flag's open period produced. A deployment that shut
// assignment after a month of addresses would, if this were gated, keep every
// one of those addresses forever precisely because it had stopped collecting
// them. The switch that turns a deletion off must never be the switch that turns
// collection off — the only switch that stops this job is the Cloud Scheduler
// one, which exists so a purge suspected of taking more than it should can be
// halted before more rows are gone.

// HolderAddressPurgeResult is what one run did, and what is left.
//
// A tally rather than a bare 200, on the Abandoned Answer Purge's terms: this
// endpoint is the runbook as much as it is the automation's entry point, and an
// operator running it by hand needs to be able to tell "nothing was due" from
// "the query matched nothing because nothing is there".
type HolderAddressPurgeResult struct {
	// AddressesPurged is how many holder addresses this run took. Zero is the
	// ordinary answer, and while TICKET_ASSIGNMENT_ENABLED is closed it is the
	// only answer.
	AddressesPurged int `json:"addresses_purged"`
	// EventsPurged is how many Events those addresses came off, which is the
	// figure that means something in human terms: forty addresses off one Event
	// is a festival that has just happened, and forty off forty Events is a
	// month of ordinary attrition.
	EventsPurged int `json:"events_purged"`
	// PurgedAt is the instant this run read the clock at (RFC3339, UTC): every
	// Event that had started by it lost the addresses nobody had accepted.
	//
	// Echoed back because the moment is the whole correctness argument, exactly
	// as the Abandoned Answer Purge echoes its cutoff. An operator staring at an
	// unexpected count should be able to see, without a deploy or a database
	// session, which instant the job actually compared Event starts against.
	PurgedAt string `json:"purged_at"`
	// AddressesHeld is how many unaccepted holder addresses are sitting on
	// Tickets across the platform once this run finished — the standing backlog,
	// in the Reconciler's sense.
	//
	// IT IS WHAT MAKES A RUN THAT DELETED NOTHING LEGIBLE. Zero purged and a
	// rising held figure is a healthy job on a platform whose Events have not
	// started yet; zero purged and zero held is a platform where nobody is
	// assigning anything. Neither is the same as a scheduler that is paused, and
	// the log line below is where that distinction is actually recorded.
	AddressesHeld int `json:"addresses_held"`
}

// PurgeUnacceptedHolderAddresses takes the address off every Ticket still in
// `assigned` whose Event has started.
//
// THE MOMENT IS COMPUTED HERE, from this service's clock, and is never taken
// from the caller. That is the internal namespace's standing rule — a caller who
// could name it could name a date years out and purge the holder address of
// every future Event on the platform — and it is the same reason the Abandoned
// Answer Purge will not let a caller name a cutoff and the Follow Digest's
// enqueue will not let one name a week.
//
// AN ACCEPTED TICKET LOSES NOTHING, which is decided in the SQL rather than
// here; see the repository for why that clause is the most important one in the
// statement. A Holder who accepted proved the address from their own inbox and
// is an ordinary Customer under ordinary Customer retention.
//
// A failure is returned rather than swallowed. Unlike the Reconciler, which
// works a queue item at a time and must survive one bad row, this run is a
// single statement that either happened or did not, and a scheduler told 200
// when the write failed would hide a retention promise quietly breaking. The
// backlog read afterwards is the one exception — it is reporting, not the job,
// and a run that purged correctly must not be reported as failed because a
// COUNT(*) did not come back.
func (s *Service) PurgeUnacceptedHolderAddresses(ctx context.Context) (*HolderAddressPurgeResult, error) {
	now := s.now().UTC()

	addresses, events, err := s.repo.PurgeUnacceptedHolderAddresses(ctx, now)
	if err != nil {
		return nil, err
	}

	result := &HolderAddressPurgeResult{
		AddressesPurged: int(addresses),
		EventsPurged:    int(events),
		PurgedAt:        now.Format(time.RFC3339),
	}

	held, err := s.repo.CountUnacceptedHolderAddresses(ctx)
	if err != nil {
		// Logged and dropped. The write above is the job and it succeeded; the
		// backlog is context for whoever is reading the response.
		if s.logger != nil {
			s.logger.Error("holder address purge backlog read failed", "error", err)
		}
	} else {
		result.AddressesHeld = int(held)
	}

	// ONE LINE PER RUN, AND IT IS THE ONLY RECORD THAT A RUN HAPPENED AT ALL.
	// Nothing is written to the database by a purge that took nothing, so
	// without this an operator asking "is the job running" cannot tell a quiet
	// platform from a paused scheduler — which is the exact question the first
	// move in a suspected-over-deletion incident depends on being able to
	// answer. It is emitted on every run, including the ones that purged zero.
	//
	// IT NAMES NO ADDRESS, NO TICKET, NO BUYER AND NO EVENT, and nobody may
	// widen it. A log aggregator is a wider audience than the database, and a
	// line listing what had just been deleted would publish precisely what the
	// deletion exists to remove.
	//
	// The nil check is because this service's logger is optional (see the field
	// on Service); the server wires a real one, and a construction that did not
	// would lose this line rather than panic in the middle of a deletion.
	if s.logger != nil {
		s.logger.Info("unaccepted holder addresses purged",
			"addresses_purged", result.AddressesPurged,
			"events_purged", result.EventsPurged,
			"purged_at", result.PurgedAt,
			"addresses_held", result.AddressesHeld,
		)
	}
	return result, nil
}
