package repository

import "time"

// CommitTerms is the Sale Commit Terms (CONTEXT.md): the terms every Ticket
// Sale is recorded on, whatever channel records it — when the write happens, how
// the buyer's Customer is resolved inside the transaction, and whether the buyer
// is seated on Ticket 1 as it is minted.
//
// IT EXISTS BECAUSE THESE THREE TRAVELLED TOGETHER AND WERE RESTATED APART. The
// sale-commit spine, the Sale Import batch, the Manually Recorded Sale and the
// Sale Correction each took the same three fields and hand-forwarded them to the
// next, so adding SelfHeld for ADR 0055 cost an edit in every one of them — a
// Data Clump wearing Shotgun Surgery (#396). Passed as one value, the next term
// the commit path grows lands here and nowhere else.
//
// IT IS NOT THE SALE AND NOT THE CHANNEL. What is being sold, to whom, on which
// Sales Channel and against which Sale Import batch all vary by route and stay
// on the route's own input; these three do not vary by route at all.
type CommitTerms struct {
	// Now is the instant the whole commit is written at — the sale's
	// recorded_at, its Tickets' minting, the Customer upsert and the sold_count
	// increment all share it, so one commit reads as one moment.
	Now time.Time
	// UpsertCustomer resolves each sale's Customer within the transaction.
	// Required: every Ticket Sale must reference a Customer.
	UpsertCustomer UpsertCustomer
	// SelfHeld makes one Ticket of each sale the buyer's own: the first Ticket
	// of the line whose Ticket Type sorts first in the catalog is assigned to
	// the buyer and accepted in the same transaction that mints it (ADR 0048).
	//
	// Set by the online checkout and by all three import routes while
	// TICKET_ASSIGNMENT_ENABLED is on (ADR 0055), and by nothing else: an
	// In-Person Sale's buyer has no surface to reassign from, so a Holder
	// written onto a door sale could be removed by nobody.
	//
	// THE FLAG IS THE WHOLE OF THE CHANNEL RULE. Nothing in the spine reads the
	// channel to decide this and nothing should: the spine mints Tickets the
	// same way for every channel, and the decision about which channels presume
	// a Holder belongs to the services that know the flag.
	//
	// NEITHER MAY IT DEFAULT ON A CORRECTION: a Sale Correction whose
	// replacement forgot it would drop the buyer off the roster in the act of
	// correcting their details, which is the bug ADR 0055 named.
	SelfHeld bool
}
