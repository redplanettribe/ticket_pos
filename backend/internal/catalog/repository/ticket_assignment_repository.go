package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
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
// a Customer who proved a different address entirely. Unreachable in #324, where
// nothing ever sets them; correct from the day #325 does.
//
// NO ORGANIZATION, EVENT OR CUSTOMER CLAUSE HERE. This takes a Ticket id that
// the caller has ALREADY resolved through a scoped read — ListAnswerableTickets
// ForBuyer and its customer_id clause — exactly as UpsertTicketAnswer does. A
// second, differently-worded scope on the write is a second place for it to be
// wrong, and the two would eventually disagree.
func (r *Repository) AssignTicketToHolder(
	ctx context.Context,
	ticketID, holderEmail string,
	now time.Time,
) (AssignTicketResult, error) {
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
	var previous sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT holder_email FROM tickets WHERE id = $1 FOR UPDATE
	`, ticketID).Scan(&previous); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// The caller resolved this Ticket a moment ago through its own scoped
			// read; it is gone now. Reported as "nothing changed" rather than as an
			// error, because there is no state left for the caller to reconcile.
			return result, nil
		}
		return result, err
	}

	if previous.Valid && previous.String == holderEmail {
		// The same address again. Nothing is written at all — see Changed.
		return result, nil
	}
	result.Changed = true

	if _, err := tx.ExecContext(ctx, `
		UPDATE tickets
		SET holder_email = $2,
		    assigned_at = $3,
		    -- Both cleared with the address, always. See above: unreachable in
		    -- #324 because nothing sets them, and load-bearing from #325.
		    accepted_at = NULL,
		    holder_customer_id = NULL
		WHERE id = $1
	`, ticketID, holderEmail, now); err != nil {
		return result, err
	}

	// The Answers go only when there WAS a previous Holder — a change of person
	// rather than the naming of one. The Options a choice Answer chose follow by
	// the ON DELETE CASCADE on ticket_answer_options (migration 073).
	if previous.Valid {
		res, err := tx.ExecContext(ctx, `DELETE FROM ticket_answers WHERE ticket_id = $1`, ticketID)
		if err != nil {
			return result, err
		}
		result.AnswersCleared, _ = res.RowsAffected()
	}

	if err := tx.Commit(); err != nil {
		return AssignTicketResult{}, err
	}
	return result, nil
}
