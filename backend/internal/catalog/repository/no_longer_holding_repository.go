package repository

import (
	"context"
	"database/sql"
)

// The read behind the No Longer Holding mail's second cause (#327, parent #322,
// ADR 0046).
//
// ONE RULE, TWO CAUSES, AND ONLY ONE OF THEM NEEDS A QUERY. A Holder stops
// holding a Ticket because the buyer reassigned it or because the Ticket Sale
// was reversed. The reassignment answers itself from inside the write that
// caused it — AssignTicketToHolder reports the address it displaced, from under
// the row lock, which is the only place that answer is knowable exactly once.
// The reversal has no such write in this module: it happens in sales, over a
// whole Sale, and lands here as "these Ticket Sales are reversed, who was
// holding their Tickets". That is what this file answers.

// DisplacedHolder is one person who has just stopped holding a Ticket, and the
// three things the mail telling them so is composed from.
//
// IT IS A DELIBERATELY POOR STRUCT, and every column absent from it is absent on
// purpose. There is no buyer name, no buyer email, no price, no Tax ID, no Sale
// Confirmation reference, no Ticket Sale id, no reversal timestamp and no
// account of WHY this person stopped holding anything. The message is forbidden
// to name a cause or a buyer (ADR 0044's disclosure rule, carried over unchanged
// by ADR 0046), and the cheapest way to keep a forbidden fact out of a message
// is for the value that composes it never to have selected the column.
//
// THERE IS NO TICKET TYPE HERE EITHER. The mail names the Event and stops:
// "which kind of ticket you no longer have" is a distinction with nothing behind
// it for a reader who has none.
type DisplacedHolder struct {
	// TicketID is here for the log line and for nothing that reaches a reader.
	TicketID string
	// HolderEmail is the address that PROVED itself by clicking, never one a
	// buyer merely typed. See the query's accepted_at clause.
	HolderEmail string
	// EventName is the whole of what the mail says about the ticket, and it is
	// already public: the Event has a Storefront page anybody can read.
	EventName string
	// SaleLocale is the language the BUYER bought in, invalid on an imported or
	// door sale that recorded none. It is the WEAKER candidate for this mail's
	// language and never the stronger — the reader is not party to the sale — and
	// is resolved behind the recipient's own remembered Mail Locale by the
	// service. See service.assignmentMailLocale, which this reuses.
	SaleLocale sql.NullString
}

// ListDisplacedHoldersForSales returns every Holder who had ACCEPTED a Ticket on
// the named Ticket Sales.
//
// IT IS CALLED AFTER THE REVERSAL HAS COMMITTED, so it deliberately does NOT
// filter on the Sale's status: by the time anybody asks this question the Sales
// are already reversed, and a clause insisting they were active would return
// nobody and mail nobody. What bounds it instead is the caller — the shared
// reversal primitive returns an empty set for a Sale somebody else already
// reversed, so this is asked exactly once per reversal and each Holder is told
// exactly once.
//
// ACCEPTED ONLY, AND THAT IS THE TICKET'S SHARPEST RULE. `accepted_at IS NOT
// NULL` is what keeps this from returning an address that a buyer typed and
// nobody ever claimed. That person was never told they had anything, so telling
// them now that they have lost it would be the platform's first and only word to
// a stranger — see #327 and the copy in platform.NoLongerHolding. A Ticket in
// `assigned` matches nothing here, however long it sat there.
//
// A REVERSAL IS ALWAYS WHOLE-SALE, and nothing in this file changes that. It
// READS; it reverses nothing, voids nothing and touches no part of a Ticket Sale
// (CONTEXT.md: "no part of a Ticket Sale can be reversed on its own"). The
// reversed Sale itself is untouched and stays visible to the buyer and to the
// Organization exactly as it did before this ticket existed — the only view that
// loses it is the Holder's.
//
// TAKES A SLICE because a Sale Import undo reverses a whole batch at once, and
// one query per Sale would be a batch of a thousand undone one round trip at a
// time. An empty slice is an ordinary answer and asks the database nothing.
//
// UNSCOPED BY ORGANIZATION, exactly as GetAssignmentLinkTicket is: the caller
// has already resolved and reversed these Sales through its own scoped write,
// and a second, differently-worded scope here would be a second place for it to
// be wrong.
func (r *Repository) ListDisplacedHoldersForSales(ctx context.Context, ticketSaleIDs []string) ([]DisplacedHolder, error) {
	if len(ticketSaleIDs) == 0 {
		return nil, nil
	}

	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT tk.id, tk.holder_email, e.name, s.locale
		`+answerableTicketFrom+`
		WHERE l.ticket_sale_id = ANY($1)
		  AND tk.accepted_at IS NOT NULL
		  AND tk.holder_email IS NOT NULL
		ORDER BY tk.id ASC
	`, ticketSaleIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]DisplacedHolder, 0, len(ticketSaleIDs))
	for rows.Next() {
		var h DisplacedHolder
		if err := rows.Scan(&h.TicketID, &h.HolderEmail, &h.EventName, &h.SaleLocale); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
