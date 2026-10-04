package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// The commit half of Named Tickets (ADR 0076, #670): the Holder addresses a
// buyer named at checkout, read off the Payment and written onto the Tickets the
// commit mints, in the transaction that records the Ticket Sale.

// listHeldHoldersByPayment returns the Holder addresses a Payment is holding,
// each keyed by the Ticket Type its line sold and the index of the Ticket on
// that line (migration 126).
//
// Read inside the commit transaction, as listHeldAnswersByPaymentLine is, so
// the assignments and the Tickets they name cannot come apart. Keyed by Ticket
// Type rather than by Payment Line because the spine never sees a Payment Line:
// it sees the Ticket Sale Lines it writes, one per Ticket Type of a checkout
// (see paymentLineIDsByTicketType).
func listHeldHoldersByPayment(ctx context.Context, tx *sql.Tx, paymentID string) ([]HeldHolder, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT l.ticket_type_id, h.ticket_index, h.holder_email
		FROM payment_ticket_holders h
		JOIN payment_lines l ON l.id = h.payment_line_id
		WHERE l.payment_id = $1
		ORDER BY l.ticket_type_id, h.ticket_index
	`, paymentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var held []HeldHolder
	for rows.Next() {
		var holder HeldHolder
		if err := rows.Scan(&holder.TicketTypeID, &holder.TicketIndex, &holder.HolderEmail); err != nil {
			return nil, err
		}
		held = append(held, holder)
	}
	return held, rows.Err()
}

// namedTicket is one Ticket the commit just minted that the buyer named a
// Holder for.
type namedTicket struct {
	ticketID    string
	holderEmail string
}

// namedTicketsOf pairs the held addresses with the Tickets this Sale minted,
// by Ticket Type and ordinal.
//
// `minted` maps each Ticket Type to the ordinal-keyed map mintTickets returned
// for its line: index n on the Payment becomes ordinal n on the Ticket, exactly
// as it does for the held Answers.
//
// AN ADDRESS THAT NAMES NO TICKET IS SKIPPED, on writeTicketAnswers' reason:
// begin-checkout bounded every index by its line's quantity, so this is only
// reachable by a line the commit did not write - a same-basket Upgrade's
// surrendered free line, which begin-checkout already asks no Holder of - and
// failing here would fail the commit of a sale the Payment Provider has already
// been paid for.
//
// THE BUYER'S SEAT IS NEVER NAMED, whatever is held for it. Begin-checkout asks
// the same SelfHeldSeatOf predicate and holds no address for the seat, so this
// is a guard and not a case; what it buys is that no held row can ever take
// the buyer's own Ticket away from them in the act of buying it.
func namedTicketsOf(held []HeldHolder, minted map[string]map[int]string, seatTicketID string) []namedTicket {
	named := make([]namedTicket, 0, len(held))
	for _, holder := range held {
		ticketID, ok := minted[holder.TicketTypeID][holder.TicketIndex]
		if !ok || ticketID == seatTicketID {
			continue
		}
		named = append(named, namedTicket{ticketID: ticketID, holderEmail: holder.HolderEmail})
	}
	return named
}

// assignNamedTickets writes each Ticket the buyer named at checkout as a Ticket
// Assignment, in the commit's transaction (ADR 0076).
//
//   - THE BUYER'S OWN ADDRESS is `accepted` at once with the buyer as Holder,
//     by holdOwnTicket - the very write that seats the Self-held Ticket, and the
//     row shape catalog's own-address assignment writes after the sale. Whether
//     an address is the buyer's is catalog.IsBuyersOwnAddress's answer and
//     nobody else's. It owes no mail.
//   - ANY OTHER ADDRESS is `assigned`, and an Assignment mail is OWED for it:
//     one row in `owed_assignment_mails` (migration 127) carrying the
//     assigned_at it was owed for, due at once. The swept sender (#671) sends
//     it, retries it, and drops it if the Ticket has changed hands by then.
//
// NOTHING IS MAILED HERE AND NOTHING IS RATIONED. A transaction cannot send
// mail, and the money has already moved, so the per-buyer rolling window
// neither refuses nor is spent by these (ADR 0076). Each one's first mail
// counts against its Ticket's lifetime allowance when the sweep sends it.
//
// ALL OR NOTHING WITH THE SALE, unlike the Answers' savepoint. These statements
// update rows minted moments ago in this same transaction, with addresses
// normalised and bounded at begin-checkout, so nothing about the data can make
// them fail; a failure here is the database failing, which takes the commit
// down anyway. A savepoint would only add a way to record a Named Tickets sale
// half-named, which is the one outcome the requirement exists to rule out.
func assignNamedTickets(
	ctx context.Context, tx *sql.Tx,
	named []namedTicket, customerID, buyerEmail string, now time.Time,
) error {
	for _, ticket := range named {
		if catalog.IsBuyersOwnAddress(ticket.holderEmail, buyerEmail) {
			if err := holdOwnTicket(ctx, tx, ticket.ticketID, customerID, buyerEmail, now); err != nil {
				return err
			}
			continue
		}
		// One statement, so the owed mark carries the assigned_at AS STORED -
		// rounded to the column's microseconds - which is the value the
		// Assignment Link will be signed over and the sweep compares against.
		if _, err := tx.ExecContext(ctx, `
			WITH assigned AS (
				UPDATE tickets
				SET holder_email = $2, assigned_at = $3
				WHERE id = $1
				RETURNING id, assigned_at
			)
			INSERT INTO owed_assignment_mails (ticket_id, assigned_at, next_attempt_at, created_at)
			SELECT id, assigned_at, $3, $3 FROM assigned
		`, ticket.ticketID, ticket.holderEmail, now); err != nil {
			return err
		}
	}
	return nil
}
