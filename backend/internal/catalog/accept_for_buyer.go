package catalog

import (
	"context"
	"database/sql"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// AcceptForBuyer makes one Ticket the buyer's own: assigned to the buyer's
// address and accepted at once with the buyer as Holder, in the caller's
// transaction. It returns the assigned_at the row now carries, as Postgres
// stored it.
//
// THE ONE WAY "ACCEPT FOR THE BUYER" HAPPENS (#668, ADR 0048, ADR 0076), and
// every route that does it calls this:
//
//   - the commit spine, seating the Self-held Ticket on a Ticket it has just
//     minted, and accepting each Ticket a Named Tickets checkout named with
//     the buyer's own address;
//   - the buyer's assign route after the sale, when the address they name is
//     the Sale's own (IsBuyersOwnAddress).
//
// One statement so the row shape cannot differ between them: a Ticket the
// buyer holds by purchase and one they took back from their sale page are the
// same accepted Ticket to the Holder List, the Upgrade's eligibility read and
// the Holder Address Purge.
//
// ACCEPTED BY THE ACT THAT NAMES IT, NEVER BY LINK. Checking out, transcribing
// a Sale the buyer already made elsewhere (ADR 0055), and naming one's own
// address from a Customer Session or a Confirmation Link session are each the
// buyer's own act, so no Assignment Link is mailed and no Assignment mail is
// owed. It writes holder_customer_id and accepted_at together, as migration
// 080's CHECK requires, and touches nothing on the customers row: none of
// these is Proof of Email Ownership, and whether the buyer is Verified stays
// the sign-in module's authority.
//
// assigned_at MOVES ONLY WHEN THE ADDRESS DOES. A freshly minted Ticket, or one
// carrying somebody else's address, is assigned now; a Ticket that already
// carries the buyer's address and was left `assigned` (from before own
// addresses were accepted at once) keeps the assigned_at it was named at,
// because the same person naming themself again is not a new assignment.
// The comparison reads the row as it was before this UPDATE, which is what
// the right-hand side of a SET sees.
//
// The address is normalised here, so the caller may pass it as typed or as
// the Sale recorded it. It takes a transaction and not a pool: every caller
// writes this beside something it must not come apart from - the Tickets the
// commit minted, or the Answers a reassignment clears.
func AcceptForBuyer(
	ctx context.Context, tx *sql.Tx,
	ticketID, buyerCustomerID, buyerEmail string, now time.Time,
) (time.Time, error) {
	var assignedAt time.Time
	err := tx.QueryRowContext(ctx, `
		UPDATE tickets
		SET holder_email = $2,
		    holder_customer_id = $3,
		    accepted_at = $4,
		    assigned_at = CASE
		        WHEN holder_email IS NOT DISTINCT FROM $2 AND assigned_at IS NOT NULL THEN assigned_at
		        ELSE $4
		    END
		WHERE id = $1
		RETURNING assigned_at
	`, ticketID, platform.NormalizeEmail(buyerEmail), buyerCustomerID, now).Scan(&assignedAt)
	return assignedAt, err
}
