package repository

import (
	"context"
	"time"
)

// The Holder Address Purge's two statements: the one that takes the addresses
// and the one that says how many are left (#331, parent #322, ADR 0046).
//
// THIS IS THE ONLY PLACE ON THE PLATFORM THAT DELETES A HOLDER ADDRESS, and it
// is the reason the feature is defensible at all. Assignment is the one surface
// here that holds contact details for a person who never came to this platform
// and consented to nothing: a friend's address, typed by somebody with no
// authority to supply it. An address that is never accepted has no purpose once
// the Event has started — the Assignment Link expires then too — so it goes.

// PurgeUnacceptedHolderAddresses takes the address off every Ticket still in
// `assigned` whose Event has started, and reports how many went and off how many
// Events they came.
//
// `now` IS THE SERVICE'S CLOCK AND NEVER A CALLER'S. See the service and the
// handler above it: a caller who could name this instant could name a date years
// out and purge the holder address of every future Event on the platform in one
// request.
//
// THE PREDICATE IS THE WHOLE TICKET, and each of its three clauses is
// load-bearing in a different direction:
//
//   - `tk.holder_email IS NOT NULL` — there is an address to take. Combined
//     with the clause below this is exactly catalog.TicketAssigned, spelled in
//     SQL rather than derived in Go because the set has to be selected by the
//     database. `assigned_at` is not tested: migration 080's
//     tickets_assignment_pair_ck makes it present exactly when the address is,
//     and testing both would be asserting a constraint rather than a rule.
//
//   - `tk.accepted_at IS NULL` — NOBODY HAS ACCEPTED IT, and this is the clause
//     whose absence would be the most serious bug this job could have. An
//     accepted Ticket has a Holder who proved that address from their own inbox
//     and became an ordinary Customer; their address is held under ordinary
//     Customer retention, and it is on `customers` as well as here. Purging it
//     would strip the Organization's guest list of the people who actually
//     answered, for no privacy gain at all.
//
//   - `e.starts_at IS NOT NULL AND e.starts_at <= now` — THE EVENT HAS STARTED.
//     Read as an INSTANT, and the Event's timezone is already inside it:
//     `events.starts_at` is a TIMESTAMPTZ fixed when the Organization set the
//     start in its own zone, so comparing instants gives the moment the
//     Organization meant. Converting either side into the Event's zone first
//     would change nothing when correct and silently move the deletion when not
//     — the same reasoning catalog.AnswerWindow and catalog.AssignmentWindow are
//     both written down with. The comparison is `<=`, so a Ticket assigned to
//     an Event starting exactly now is purged: the assignment window is
//     half-open and closes at the same instant, so this is the moment there is
//     provably nobody left who could accept.
//
// AN EVENT THAT NEVER SAID WHEN IT STARTS IS NEVER PURGED, which is the one
// place this job is deliberately conservative rather than deliberately thorough.
// A NULL start read as "started" would take the addresses off every Ticket of
// every unscheduled Event the moment this shipped, and a deletion is the wrong
// thing to be wrong about in that direction. It is the same reading
// AssignmentWindow gives a nil start, so an Event whose assignment window has
// never closed never has its addresses taken either — the two rules agree, and
// an Event that acquires a start later is swept by the next tick.
//
// A REVERSED TICKET SALE IS PURGED LIKE ANY OTHER, and there is no sale-status
// clause here on purpose. Nobody is coming on a refunded ticket, so an address
// on one has even less purpose than an ordinary unaccepted address, not more.
// The Ticket, the Sale and the Reversal all survive this, exactly as they
// survive for a sale nobody reversed.
//
// IT TAKES THE ADDRESS AND NOTHING ELSE. `tickets` is UPDATEd, never DELETEd
// FROM; `ticket_answers` is not named in this statement at all, so what the
// buyer or a holder typed about that Ticket stays; and the Ticket Sale, its
// lines and its money are not reachable from here. This is a record-keeping
// requirement rather than an optimisation: the platform is entitled to remember
// that it sold a ticket and that somebody was named for it, and is not entitled
// to keep the name.
//
// `assigned_at` GOES WITH THE ADDRESS because migration 080 says a pair travels
// together, and `holder_address_purged_at` is what keeps the fact of the
// assignment once the pair is gone. Without that third column a purged Ticket
// would be indistinguishable from one nobody was ever named for, which is the
// one outcome #322 rules out.
//
// IDEMPOTENT BY CONSTRUCTION. A second run finds `holder_email IS NOT NULL`
// false on every row the first took and reports zeros; two runs racing update
// disjoint sets, since each row leaves the predicate the moment it is written;
// and a run killed halfway leaves the rows it had already purged purged and the
// rest for the next tick. There is no queue, no claim and no cursor — the same
// shape as PurgeAbandonedCheckoutAnswers, and for the same reason: adding a
// claim here would add a way for the job to get stuck in exchange for nothing.
func (r *Repository) PurgeUnacceptedHolderAddresses(
	ctx context.Context,
	now time.Time,
) (addresses, events int64, err error) {
	// One statement, in two CTEs, on the Abandoned Answer Purge's pattern.
	// `doomed` names the rows and the Events they belong to BEFORE the write,
	// because that is the only moment the Event can still be counted: once the
	// address is gone the row no longer distinguishes itself from any other
	// Ticket of that Event, and an UPDATE ... RETURNING would have to re-walk
	// four tables to say whose it was.
	err = r.db.Pool.QueryRowContext(ctx, `
		WITH doomed AS (
			SELECT tk.id, s.event_id
			FROM tickets tk
			JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
			JOIN ticket_sales s ON s.id = l.ticket_sale_id
			JOIN events e ON e.id = s.event_id
			WHERE tk.holder_email IS NOT NULL
			  AND tk.accepted_at IS NULL
			  AND e.starts_at IS NOT NULL
			  AND e.starts_at <= $1
		),
		purged AS (
			UPDATE tickets
			SET holder_email = NULL,
			    assigned_at = NULL,
			    holder_address_purged_at = $1
			WHERE id IN (SELECT id FROM doomed)
			RETURNING id
		)
		SELECT (SELECT COUNT(*) FROM purged), (SELECT COUNT(DISTINCT event_id) FROM doomed)
	`, now).Scan(&addresses, &events)
	if err != nil {
		return 0, 0, err
	}
	return addresses, events, nil
}

// PurgeUnacceptedCorrectedAddresses takes the corrected address off every Sale
// Re-addressing still pending whose Event has started, and reports how many
// went (#424, parent #419, ADR 0058).
//
// THE SAME KIND OF FACT AS AN UNACCEPTED HOLDER ADDRESS, ended the same way. A
// corrected address is typed by a Platform Operator on the buyer's word and
// held until its owner accepts by Re-addressing Link; until that click it is
// an address supplied by somebody who is not its owner, exactly as a holder
// address is. Once the Event has started nothing pending can be accepted — the
// derived state reads `expired` from the Event alone (sales.
// DeriveReAddressingState) — so the address has no purpose left and goes on the
// same tick, from the same job, judged against the same instant.
//
// IT LIVES IN CATALOG'S REPOSITORY RATHER THAN SALES', because the purge is
// catalog's job and sales cannot be reached from here without a cycle. The
// statement names sales' table directly, on the terms the statement above
// already names `ticket_sales`: a purge is a cross-cutting retention promise
// and its predicate reads whatever tables carry the data it exists to remove.
//
// THE PREDICATE:
//
//   - `accepted_at IS NULL AND withdrawn_at IS NULL` — the record has not
//     ended, which is migration 093's one-pending predicate exactly: what this
//     takes is what the Operator lookup would otherwise still call pending. An
//     accepted row keeps its address FOREVER: once proven from the corrected
//     inbox it is the Customer's own, it is on `customers` as well, and
//     migration 093's CHECK would refuse the NULL anyway. A withdrawn row is
//     left alone too — the Operator ended it themself and it is not what the
//     ticket names — which means an address the Operator withdrew is kept
//     unaccepted past the doors. That is a gap this job does not close, noted
//     rather than quietly widened into.
//
//   - `corrected_email IS NOT NULL` — there is an address to take, which is
//     what makes a second run find nothing.
//
//   - `e.starts_at IS NOT NULL AND e.starts_at <= now` — the Event has
//     started, read as an instant for the reasons the statement above sets
//     out. An unscheduled Event never purges.
//
// NO SALE-STATUS CLAUSE, as above: a pending record on a reversed Sale already
// reads `expired` and its address has even less purpose. But a reversal alone
// does not bring a row here — the Event's start does — because the reversal
// paths write to the Sale and to nothing else, on the derived-state rule.
//
// IT TAKES THE ADDRESS AND NOTHING ELSE. The operator's email, the address the
// Sale was sold to, the note and every timestamp stay, so the row keeps saying
// "somebody tried, when, and from where" once it no longer says to whom. There
// is no marker column: a NULL corrected address on an unended row IS the
// marker, since nothing else ever writes NULL there.
func (r *Repository) PurgeUnacceptedCorrectedAddresses(ctx context.Context, now time.Time) (int64, error) {
	result, err := r.db.Pool.ExecContext(ctx, `
		UPDATE sale_re_addressings ra
		SET corrected_email = NULL
		FROM ticket_sales s
		JOIN events e ON e.id = s.event_id
		WHERE s.id = ra.ticket_sale_id
		  AND ra.accepted_at IS NULL
		  AND ra.withdrawn_at IS NULL
		  AND ra.corrected_email IS NOT NULL
		  AND e.starts_at IS NOT NULL
		  AND e.starts_at <= $1
	`, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// CountUnacceptedHolderAddresses is how many addresses are sitting unaccepted on
// Tickets right now, across the platform.
//
// Reported by every run for the reason the Abandoned Answer Purge reports its
// held count and the Reversal Reconciler its in-flight backlog: a run that says
// "0 purged" is otherwise indistinguishable from a job that is not firing at
// all, and on a feature that ships dark behind TICKET_ASSIGNMENT_ENABLED (ADR
// 0045) zero is the expected answer for a long time. Two curls a day apart say
// whether anybody is assigning anything.
//
// IT COUNTS THE UNACCEPTED ONLY, which is the figure this job is accountable
// for. An accepted Ticket's Holder is an ordinary Customer whose address the
// platform keeps under ordinary Customer retention, so counting those here would
// report a backlog that this job must never reduce — a number that only ever
// rises, presented beside one it is supposed to drive down.
func (r *Repository) CountUnacceptedHolderAddresses(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM tickets
		WHERE holder_email IS NOT NULL AND accepted_at IS NULL
	`).Scan(&n)
	return n, err
}
