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
	// IsTheBuyer says this Holder and the buyer of the Sale are the same person
	// (#392, ADR 0055), which is the one distinction the notice's policy now turns
	// on: a Holder who is the buyer holds by PRESUMPTION on an imported Sale and
	// by PAYING on an Online one, and either way is the person the buyer-facing
	// toggles exist to protect. Anybody else here accepted by clicking an
	// Assignment Link and is told unconditionally.
	//
	// IT IS THE PERSON, NOT THE ROUTE, AND THAT IS A CHOICE. A buyer may assign a
	// Ticket to their OWN address and accept it by clicking, which the assignment
	// API expressly permits. Such a Ticket was held the way an unconditional
	// notice's warrant describes — proved, clicked, accepted — and this column
	// still reports it as the buyer's, so the toggle silences it too. That is
	// deliberate: the toggle exists to decide whether the Organization is opening
	// a conversation with the person who bought, and it would be a strange
	// setting that fell to whether that person happened to route a second Ticket
	// through their own inbox. The alternative also mails them once PER TICKET —
	// nothing here dedupes by address — so honouring the route would turn one
	// silenced buyer into two mails. Reversible in one clause if the ruling goes
	// the other way.
	//
	// A COMPARISON AND NOT AN ADDRESS, which is what keeps the struct as poor as
	// its doc insists. The buyer's email is compared inside the query and never
	// selected, so no value in this package can name the buyer even by accident,
	// and the mail composed from it still could not disclose one if the copy
	// asked.
	IsTheBuyer bool
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

	// THE BUYER IS COMPARED, NEVER SELECTED. The fourth column answers "is this
	// Holder the buyer?" and no column of this query can answer "who is the
	// buyer?" — see DisplacedHolder.IsTheBuyer. It folds case and trims, which is
	// how every other comparison of these two addresses is written (migration 084,
	// platform.NormalizeEmail), so a Holder seated from a file with a capitalised
	// address is still recognised as the person who bought.
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT tk.id, tk.holder_email, e.name, s.locale,
		       lower(btrim(tk.holder_email)) = lower(btrim(s.customer_email))
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
		if err := rows.Scan(&h.TicketID, &h.HolderEmail, &h.EventName, &h.SaleLocale, &h.IsTheBuyer); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
