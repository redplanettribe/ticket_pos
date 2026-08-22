package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// The reads and the one write behind an Assignment Link (#325, parent #322,
// ADR 0046).
//
// EVERYTHING HERE IS REACHED BY A SIGNED TOKEN AND BY NOTHING ELSE. There is no
// Organization parameter and no Customer parameter on the read below, for the
// same reason the retired Answer Link's lookup had none: the authorization IS the signature,
// checked in catalog.AssignmentLinkSigner.Parse before any of this runs, and it
// names exactly one Ticket. A scope parameter would have to come from somewhere,
// and the only place it could come from is this row.

// AssignmentLinkTicket is one Ticket as the accept flow needs to see it: the two
// things the page shows, the facts the window is decided from, and the
// assignment's own state.
//
// A SEPARATE TYPE FROM AnswerableTicket, AND THE SEPARATION IS THE SECURITY
// PROPERTY, exactly as it was for the retired Answer Link. AnswerableTicket carries the
// Ticket Sale's id, its Sale Confirmation reference and the Ticket's ordinal —
// every one a fact about the purchase, and the ordinal says how many Tickets the
// Sale has. A column that was never selected cannot be leaked by somebody adding
// a `json:` tag.
//
// Note what is absent and must stay absent: the buyer's name and email, the
// price, the Tax ID, the Sale Confirmation reference, the Sale's id, the
// ordinal, and the Sale's other Tickets.
type AssignmentLinkTicket struct {
	ID             string
	TicketTypeID   string
	TicketTypeName string
	EventName      string
	// SaleStatus is 'active' or 'reversed', read LIVE rather than baked into the
	// token: a Sale reversed after the mail went out must stop the link opening,
	// which no fact frozen at mint time could do.
	SaleStatus string
	// EventStartsAt is the instant the doors open, invalid on an Event that has
	// not said when. It IS the Event's start read in the Event's timezone — the
	// column was written from the Event's local clock and stores the instant that
	// produced — so comparing it to now needs no timezone arithmetic and must not
	// grow any.
	EventStartsAt sql.NullTime
	// HolderEmail is the address currently named on this Ticket, invalid when
	// none is. It never reaches the wire on this route: the Holder knows their
	// own address, and printing it back would put a third party's address on a
	// page reachable by whoever came to hold the link.
	HolderEmail sql.NullString
	// AssignedAt is when the CURRENT address was named, and is what the token's
	// signed timestamp is compared against. A reassignment moves it, and every
	// link mailed to a previous address dies at that moment.
	AssignedAt sql.NullTime
	// AcceptedAt and HolderCustomerID are the acceptance, both invalid until a
	// Holder has clicked. They are what makes accepting twice idempotent: the
	// second click finds them set and mints nothing.
	AcceptedAt       sql.NullTime
	HolderCustomerID sql.NullString
	// SaleLocale is the language the BUYER completed the purchase in, or invalid
	// on an imported or door sale that recorded none (migration 059).
	//
	// It is the WEAKER of the two candidates for the Assignment mail's language
	// and never the stronger, which inverts ADR 0033's usual order — see the
	// service's assignmentMailLocale. The reader of that mail is not the party to
	// this sale.
	SaleLocale sql.NullString
}

// GetAssignmentLinkTicket loads one Ticket by id for the accept flow, UNSCOPED.
//
// It returns the Ticket whether or not it may still be accepted, exactly as
// the retired Answer Link's lookup did: the window and the freshness of the token are
// decisions taken in one place in the service, from the columns above. Deciding
// them in the WHERE clause would make "reversed", "reassigned" and "no such
// Ticket" the same row count — which is the right ANSWER to give a caller and
// the wrong way to arrive at it, because a second copy of the rule would then
// live in SQL.
func (r *Repository) GetAssignmentLinkTicket(ctx context.Context, ticketID string) (*AssignmentLinkTicket, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT tk.id, l.ticket_type_id, tt.name, e.name, s.status, e.starts_at,
		       tk.holder_email, tk.assigned_at, tk.accepted_at, tk.holder_customer_id,
		       s.locale
		`+answerableTicketFrom+`
		WHERE tk.id = $1
	`, ticketID)

	var t AssignmentLinkTicket
	if err := row.Scan(
		&t.ID, &t.TicketTypeID, &t.TicketTypeName, &t.EventName, &t.SaleStatus, &t.EventStartsAt,
		&t.HolderEmail, &t.AssignedAt, &t.AcceptedAt, &t.HolderCustomerID,
		&t.SaleLocale,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

// AcceptTicketAssignment records that the Holder proved the address: the moment
// they clicked, and the Customer that click minted or matched.
//
// IT IS THE ONE WRITE THAT MAKES `accepted` REACHABLE. Migration 080 declared
// the two columns and #324 left them permanently NULL, because there was no mail
// and so no link to click. This statement is the whole of what changed.
//
// IDEMPOTENT BY CONSTRUCTION, which is an acceptance criterion of #325 and not a
// nicety: a Holder who clicks the link again a week later, or whose mail client
// prefetched it, must land on their own page rather than on a refusal. The
// COALESCE keeps the FIRST acceptance's instant — the moment a row became a
// person is a fact worth not overwriting — and the Customer cannot change,
// because the address it was minted from has not.
//
// THE ASSIGNMENT IS RE-CHECKED IN THE WHERE CLAUSE, and this is the part that
// must not be simplified away. The caller has already read the row and compared
// its assigned_at against the token, but between that read and this write the
// buyer may have reassigned the Ticket to somebody else — and accepting then
// would attach a Customer to an address that is no longer named, which the
// database's own CHECK would happily allow because both halves are set. Naming
// assigned_at here makes the write lose that race rather than win it.
//
// It reports whether anything was written. False means the assignment moved
// under the caller, which the service reports as a link that does not open — the
// same answer a forged token gets, because "your friend gave your ticket to
// somebody else" is a fact about the buyer's decisions that this page never
// discloses.
func (r *Repository) AcceptTicketAssignment(
	ctx context.Context,
	ticketID, customerID string,
	assignedAt time.Time,
	now time.Time,
) (bool, error) {
	result, err := r.db.Pool.ExecContext(ctx, `
		UPDATE tickets
		SET accepted_at = COALESCE(accepted_at, $4),
		    holder_customer_id = $2
		WHERE id = $1
		  AND assigned_at = $3
		  AND holder_email IS NOT NULL
	`, ticketID, customerID, assignedAt, now)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}
