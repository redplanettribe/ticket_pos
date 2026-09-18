package repository

import "time"

// CommitTerms is the Sale Commit Terms (CONTEXT.md): the terms every Ticket
// Sale is recorded on, whatever channel records it — when the write happens, how
// the buyer's Customer is resolved inside the transaction, and whether the buyer
// is seated on a Ticket of their own as it is minted.
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
	// of the sale's dearest line is assigned to the buyer and accepted in the
	// same transaction that mints it (ADR 0048, seated on the dearest by ADR
	// 0074). WHICH line that is belongs to the spine and to selfHeldSeat beside
	// it; this flag says only whether a buyer is seated at all.
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
	// UpgradeElected is the buyer's election that the paid Ticket this commit is
	// seating them on takes the place of a free one (ADR 0074, #649/#650) — one
	// on an earlier Sale, which is reversed here, or one in this very basket,
	// whose line is then never written. It is the Upgrade Prompt's answer and
	// nothing else: not a verdict, not an instruction, and never a reason to
	// refuse anything.
	//
	// IT HAS EXACTLY ONE SOURCE, and it is not the caller. No checkout leg sets
	// this term: ApprovePaymentAndCommitSale overwrites it from the Payment's own
	// `upgrade_elected` column (migration 124), read under the FOR UPDATE that
	// already makes the settlement atomic. That is deliberate. The election is
	// made once, at begin-checkout, with the prompt in front of the buyer; the
	// free leg and the Payment Provider's return leg both settle a Payment, and a
	// term two legs could each state their own way is a term they will one day
	// state differently. No body is consulted here, on either leg.
	//
	// FALSE IS KEEP BOTH, and false is what every route that never asks says. The
	// three import routes and the Sale Correction have no buyer at a keyboard to
	// ask and never touch a Payment row, so the zero value is the truth about
	// them.
	//
	// AN ELECTION THE BACKEND DID NOT OFFER IS IGNORED, NEVER REFUSED. Eligibility
	// is re-evaluated inside this transaction (UpgradeEligibilityFor) and the
	// election simply does nothing when it no longer holds — so this field is a
	// request and the spine's own predicate is the answer.
	//
	// IT DOES NOTHING WITHOUT SelfHeld ABOVE, at every point that reads it: an
	// Upgrade surrenders the Ticket a buyer holds for the Ticket they are being
	// seated on, and in a build where nobody is seated there is neither.
	UpgradeElected bool
}
