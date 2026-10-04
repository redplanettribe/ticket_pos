package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// The one write path into a Ticket Assignment (#324, parent #322).
//
// THERE IS EXACTLY ONE, AND THAT IS THE DESIGN. Assigning, reassigning and
// correcting a typo are the same statement — "this Ticket's Holder address is
// now X" — so they are one call, idempotent in the address, and there is no
// second door through which a Ticket could acquire an address without the
// Answer-clearing rule below firing.

// AssignTicketResult reports what the write actually did, which the caller
// cannot infer from the address it sent.
type AssignTicketResult struct {
	// Changed is false when the Ticket already carried this exact address, which
	// makes the whole call a no-op: assigned_at does not move, accepted_at and
	// the Holder's Customer survive, and no Answer is cleared.
	//
	// SUBMITTING THE SAME ADDRESS TWICE MUST NOT BE AN EVENT. A buyer who
	// presses save twice, or who "corrects" a typo back to what it already said,
	// has changed nothing about who holds this Ticket — and #325's per-Ticket
	// mail cap reads assigned_at, so moving it would let a doubled click spend
	// somebody's allowance.
	Changed bool
	// AnswersCleared is how many Answers were sent back to Outstanding by this
	// reassignment. Zero on a first assignment and on a no-op.
	AnswersCleared int64
	// AssignedAt is the instant the row now carries, AS POSTGRES STORED IT, and
	// it is why this write reports a time at all (#325).
	//
	// THE TOKEN IS SIGNED FROM THIS VALUE AND NEVER FROM THE Go CLOCK THAT
	// PRODUCED IT. A TIMESTAMPTZ keeps microseconds; time.Now() keeps
	// nanoseconds. An Assignment Link signed over the un-rounded value would
	// carry a timestamp that never equals the one read back from the row, so
	// every link would open exactly nowhere — a failure that would not appear on
	// a machine whose clock happens to tick in whole microseconds.
	//
	// Zero on a no-op, where nothing was written and no mail is sent.
	AssignedAt time.Time
	// DisplacedHolderEmail is the address of a Holder who had ACCEPTED this
	// Ticket and has just stopped holding it, and "" in every other case (#327).
	//
	// IT IS REPORTED FROM INSIDE THE LOCK, which is why it is a field here rather
	// than a read the service does beforehand. The statement below clears
	// holder_email and accepted_at together, so a service that wanted to know who
	// it displaced would have to read the row first — and between that read and
	// this write another buyer surface could reassign, leaving the two callers
	// agreeing to mail the same person twice or nobody at all. The row is locked;
	// this is the only place the answer is knowable exactly once.
	//
	// EMPTY FOR AN ASSIGNMENT THAT WAS NEVER ACCEPTED, and that is the sharpest
	// rule in #327 rather than an optimisation. An address that was typed and
	// ignored was never told it had anything: telling it now that it has lost
	// something would be the platform's first and only word to a stranger, about
	// a ticket they never knew existed. So this reads accepted_at and not merely
	// holder_email — a previous address is not a previous Holder.
	//
	// Empty on a first assignment and on a no-op, where nobody was displaced.
	DisplacedHolderEmail string
	// Accepted is true when this write made the Ticket `accepted` for the buyer
	// (ADR 0076): always on an own-address assignment that changed the address,
	// and also on the one same-address call that is not a no-op - a Ticket
	// already carrying the buyer's address but left `assigned` from before own
	// addresses were accepted at once.
	Accepted bool
}

// AssignedAnswer is one Answer written onto a Ticket in the transaction that
// names its new Holder (#673): already validated and its Options resolved by
// the caller, as UpsertTicketAnswer's are.
type AssignedAnswer struct {
	TicketQuestionID string
	Params           UpsertTicketAnswerParams
}

// AssignTicketInput is one "this Ticket's Holder address is now X", with
// everything the write needs to decide, under the row lock, what that means.
type AssignTicketInput struct {
	// TicketID is a Ticket the caller has ALREADY resolved through a scoped
	// read; see AssignTicketToHolder.
	TicketID string
	// HolderEmail is the address, already parsed and normalised
	// (catalog.ParseHolderEmail).
	HolderEmail string
	// BuyerCustomerID is the Sale's own Customer, the session's.
	BuyerCustomerID string
	// OwnAddress says HolderEmail is the Sale's own address
	// (catalog.IsBuyersOwnAddress): the Ticket is then accepted at once with
	// the buyer as its Holder, through catalog.AcceptForBuyer, and is never
	// left `assigned` waiting on a link nobody will mail (ADR 0076).
	OwnAddress bool
	// NamedTickets is the Named Tickets requirement's verdict on the Answers
	// given with the address, and nil wherever the requirement does not bind
	// (#673, ADR 0076). It is consulted only if the address CHANGES.
	NamedTickets *NamedTicketsVerdict
	Now          time.Time
}

// NamedTicketsVerdict is what the Named Tickets requirement says of one
// reassignment's Answers, judged by the service against the questions the
// checkout asks: the Answers to write if the address changes, or what is still
// owed, which refuses a change of address outright.
type NamedTicketsVerdict struct {
	Answers []AssignedAnswer
	// Owed is non-empty when a required Answer is missing. Refused in the
	// shared NAMED_TICKETS_INCOMPLETE shape, and only when the address changes.
	Owed []catalog.OwedTicket
}

// AssignTicketToHolder names the address that holds one Ticket, creating the
// assignment or replacing the one that was there.
//
// REASSIGNMENT CLEARS THAT TICKET'S ANSWERS, and this is a CORRECTNESS
// REQUIREMENT rather than a tidy-up. An Answer is a fact about a PERSON — a
// t-shirt size, a dietary requirement, an accessibility need — and a Ticket that
// changed hands carrying its Answers would attribute the previous Holder's facts
// to somebody who never said them. The Organization would then order against
// them: the wrong size, or a meal that makes the new Holder ill. So the Answers
// go, and the Ticket returns to Outstanding for the new Holder to answer afresh.
//
// IT IS DONE HERE, IN THE SAME TRANSACTION AS THE ADDRESS, and never as a second
// call the service makes afterwards. A crash between two statements would leave
// a Ticket holding a new Holder and an old Holder's dietary requirement, which
// is precisely the state this rule exists to make impossible.
//
// A FIRST ASSIGNMENT CLEARS NOTHING, deliberately, and the difference is who the
// Answers are about. Answers on an `unassigned` Ticket were given by the buyer,
// for the person they are about to name; naming them is not a change of subject
// and wiping the work the buyer just did would be an unexplained loss. Only a
// change of ADDRESS is a change of person.
//
// ACCEPTANCE IS CLEARED WITH THE ADDRESS. A Ticket handed to somebody new has
// not been accepted by them, so accepted_at and holder_customer_id go together
// with the old address — otherwise the database's own CHECK would be describing
// a Customer who proved a different address entirely.
//
// THE BUYER'S OWN ADDRESS IS ACCEPTED IN THE SAME TRANSACTION (ADR 0076), by
// catalog.AcceptForBuyer: the one write the commit spine also seats the
// Self-held Ticket with, so the two cannot write different rows. Not a second
// call afterwards: a Ticket must never be observable as `assigned` to the
// buyer, waiting on a link nobody will mail.
//
// THE NAMED TICKETS REQUIREMENT IS DECIDED HERE, UNDER THE LOCK (#673, ADR
// 0076). Whether a call names somebody new is only knowable once the row is
// locked, and so is whether it owes the new Holder's Answers: a same-address
// call is a no-op that needs none and writes none (the Ticket's Answers may
// already be an accepted Holder's own, ADR 0049), while a change of address
// with a required Answer missing is refused and writes nothing. A rule judged
// before the lock on the caller's earlier read would leave a window in which
// a concurrent reassignment turned a same-address call into a change made
// without Answers. On a change, the clearing above and the new Holder's
// Answers commit together: there is no instant at which the Ticket is named
// for somebody new with the roster's required Answers gone.
//
// NO ORGANIZATION, EVENT OR CUSTOMER CLAUSE HERE. This takes a Ticket id that
// the caller has ALREADY resolved through a scoped read — ListAnswerableTickets
// ForBuyer and its customer_id clause — exactly as UpsertTicketAnswer does. A
// second, differently-worded scope on the write is a second place for it to be
// wrong, and the two would eventually disagree.
func (r *Repository) AssignTicketToHolder(ctx context.Context, in AssignTicketInput) (AssignTicketResult, error) {
	var result AssignTicketResult

	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()

	// The row is locked before it is read, so that two buyer surfaces saving
	// different addresses at once cannot both read "unassigned", both decide
	// nothing needs clearing, and leave the loser's Answers attached to the
	// winner's Holder.
	//
	// BOTH COLUMNS COME BACK, not just the address: accepted_at is what tells a
	// previous HOLDER from a previous address (#327), and it has to be read
	// before the UPDATE below sets it to NULL.
	var previous sql.NullString
	var previouslyAccepted sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT holder_email, accepted_at FROM tickets WHERE id = $1 FOR UPDATE
	`, in.TicketID).Scan(&previous, &previouslyAccepted); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// The caller resolved this Ticket a moment ago through its own scoped
			// read; it is gone now. Reported as "nothing changed" rather than as an
			// error, because there is no state left for the caller to reconcile.
			return result, nil
		}
		return result, err
	}

	if previous.Valid && previous.String == in.HolderEmail {
		// The same address again. Nothing is written at all - see Changed -
		// and no Answers are asked for or written, unless it is the buyer's own
		// address on a Ticket still waiting to be accepted, which is accepted
		// now: the same person, so no Answer goes and assigned_at stands.
		if !in.OwnAddress || previouslyAccepted.Valid {
			return result, nil
		}
		if _, err := catalog.AcceptForBuyer(ctx, tx, in.TicketID, in.BuyerCustomerID, in.HolderEmail, in.Now); err != nil {
			return result, err
		}
		if err := tx.Commit(); err != nil {
			return AssignTicketResult{}, err
		}
		result.Accepted = true
		return result, nil
	}

	// A CHANGE OF ADDRESS, so the Named Tickets requirement binds this call.
	// Refused before anything is written; the deferred rollback releases the
	// lock having changed nothing.
	if in.NamedTickets != nil && len(in.NamedTickets.Owed) > 0 {
		return AssignTicketResult{}, catalog.ErrNamedTicketsIncomplete(in.NamedTickets.Owed)
	}
	result.Changed = true

	// WHO IS ABOUT TO STOP HOLDING THIS TICKET, decided here and recorded before
	// the columns that say so are cleared. A previous address that never accepted
	// leaves this empty, and the service mails nobody — see DisplacedHolderEmail.
	if previous.Valid && previouslyAccepted.Valid {
		result.DisplacedHolderEmail = previous.String
	}

	// Both writes return the stored timestamp rather than trusting the one sent
	// in: it comes back rounded to the column's microseconds, and #325 signs the
	// Assignment Link over exactly that value.
	if in.OwnAddress {
		result.AssignedAt, err = catalog.AcceptForBuyer(ctx, tx, in.TicketID, in.BuyerCustomerID, in.HolderEmail, in.Now)
		if err != nil {
			return result, err
		}
		result.Accepted = true
	} else if err := tx.QueryRowContext(ctx, `
		UPDATE tickets
		SET holder_email = $2,
		    assigned_at = $3,
		    -- Both cleared with the address, always. A Ticket handed to somebody
		    -- new has not been accepted by them, and every Assignment Link mailed
		    -- to the previous address dies here, because assigned_at is what
		    -- those links were signed over.
		    accepted_at = NULL,
		    holder_customer_id = NULL
		WHERE id = $1
		RETURNING assigned_at
	`, in.TicketID, in.HolderEmail, in.Now).Scan(&result.AssignedAt); err != nil {
		return result, err
	}

	// The Answers go only when there WAS a previous Holder — a change of person
	// rather than the naming of one. The Options a choice Answer chose follow by
	// the ON DELETE CASCADE on ticket_answer_options (migration 073).
	if previous.Valid {
		res, err := tx.ExecContext(ctx, `DELETE FROM ticket_answers WHERE ticket_id = $1`, in.TicketID)
		if err != nil {
			return result, err
		}
		result.AnswersCleared, _ = res.RowsAffected()
	}

	// Upserted rather than inserted: a first assignment cleared nothing, and
	// the Ticket may already carry an Answer the buyer is now restating.
	if in.NamedTickets != nil {
		for _, answer := range in.NamedTickets.Answers {
			if err := writeTicketAnswer(ctx, tx, in.TicketID, answer.TicketQuestionID, answer.Params, in.Now); err != nil {
				return result, err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return AssignTicketResult{}, err
	}
	return result, nil
}

// The Assignment mail ledger: the three statements the rationing needs (#332,
// parent #322, ADR 0046).
//
// TWO COUNTS AND ONE INSERT, AND NOTHING ELSE. Nothing reads this table to build
// a screen, nothing exports it and no Customer ever sees it — it exists to say
// no. See migration 082 for why it is a ledger of sends rather than a pair of
// counters on `tickets`, and for why it holds no address.

// CountAssignmentMailsForTicket is how many Assignment mails this Ticket has
// EVER sent, across every Holder it has ever been pointed at.
//
// NO WINDOW AND NO CUTOFF, deliberately: the per-Ticket allowance is a lifetime
// one. A Ticket that has spent it does not get it back by being left alone for a
// month, because the hole this closes is a slow drip just as much as a burst.
func (r *Repository) CountAssignmentMailsForTicket(ctx context.Context, ticketID string) (int, error) {
	var count int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ticket_assignment_mails WHERE ticket_id = $1
	`, ticketID).Scan(&count)
	return count, err
}

// CountAssignmentMailsForBuyer is how many this buyer has sent across ALL of
// their Tickets since the given moment.
//
// THE CUTOFF IS THE CALLER'S BECAUSE THE CLOCK IS THE SERVICE'S. Every other
// swept read in this module takes its instant the same way, and the service
// derives the cutoff from its own clock rather than accepting one from a
// request — a caller who could name the window's start could name one a second
// ago and lift the limit entirely.
//
// A MAIL OWED BY A NAMED TICKETS CHECKOUT IS NOT COUNTED (migration 128, ADR
// 0076): it spends its Ticket's lifetime allowance and never the buyer's
// window, which rations what the buyer does after the sale.
func (r *Repository) CountAssignmentMailsForBuyer(ctx context.Context, buyerCustomerID string, since time.Time) (int, error) {
	var count int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM ticket_assignment_mails
		WHERE buyer_customer_id = $1 AND sent_at >= $2 AND NOT checkout_named
	`, buyerCustomerID, since).Scan(&count)
	return count, err
}

// RecordAssignmentMailSent writes the row that spends one unit of both
// allowances at once.
//
// THE CALLER MUST HAVE SENT ALREADY, on the rule migration 079 set for the
// Answer Reminder ledger and for the same reason with more force: a row written
// before the send would ration a buyer out of a mail nobody received, and here
// that is permanent — the per-Ticket allowance never refills. The reverse
// failure, a send whose row was lost, costs one extra mail later.
//
// IT IS NOT IN THE ASSIGNMENT'S TRANSACTION. That transaction commits before the
// mail is composed, because a mail cannot be rolled back; a ledger row inside it
// would be a claim about an inbox made before anybody wrote to it.
func (r *Repository) RecordAssignmentMailSent(
	ctx context.Context,
	ticketID, buyerCustomerID string,
	sentAt time.Time,
) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO ticket_assignment_mails (ticket_id, buyer_customer_id, sent_at)
		VALUES ($1, $2, $3)
	`, ticketID, buyerCustomerID, sentAt)
	return err
}
