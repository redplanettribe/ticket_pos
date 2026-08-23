package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// A Holder is told when they stop holding a Ticket (#327, parent #322,
// ADR 0046).
//
// ONE RULE, AND THIS FILE IS THE WHOLE OF IT. Whenever an ACCEPTED Holder stops
// holding a Ticket, they are told once, and the Event leaves their Customer
// Area. There are two causes — the buyer reassigned it, or the Ticket Sale was
// reversed — and from where the Holder sits the outcome is identical: they had a
// ticket and now they do not. So the platform must not be talkative in one case
// and silent in the other, and the way that is guaranteed rather than remembered
// is that BOTH CAUSES ARRIVE AT THE SAME FUNCTION. There is one composer below,
// one message type, one piece of copy and one locale rule; the two entry points
// differ only in how they discover WHO was displaced.
//
// THE MAIL GIVES NO CAUSE AND NAMES NO BUYER. Both causes are facts about the
// buyer's decisions — they asked for their money back, or they handed the ticket
// to another friend — and a Holder is never told who the buyer is, let alone
// what they chose. ADR 0044's disclosure rule, carried over unchanged by
// ADR 0046, binds this message exactly as it binds the Assignment mail. It is
// enforced structurally as well as here: platform.NoLongerHolding has no field
// for a name and no field for a cause, and repository.DisplacedHolder never
// selects a column that could fill one.
//
// A HOLDER WHO IS THE BUYER FOLLOWS THE BUYER'S NOTIFICATION POLICY (#392,
// ADR 0055), AND THAT IS THE ONE BRANCH IN THIS FILE. Since ADR 0048 and 0055 a
// Ticket Sale's buyer holds one of its own Tickets — by paying, on an Online
// Sale, and by PRESUMPTION on an imported one, where the address was
// transcribed off a file and nobody clicked anything. Such a Holder is not the
// person #327 wrote its unconditional rule for; they are the buyer, and the
// buyer-facing toggles (a Sale Import undo's notify switch, a Sale Correction's
// Sale Confirmation checkbox) exist precisely to decide whether the platform
// writes to them at all. So the reversal path is handed one fact by sales — is
// the buyer being written to about this? — and a Holder who is the buyer is
// mailed only when the answer is yes. EVERYBODY ELSE IS TOLD UNCONDITIONALLY,
// exactly as before: they came here, proved their address and accepted.
//
// THE BRANCH IS ON WHO THE HOLDER IS AND NEVER ON THE CHANNEL. ADR 0055
// considered and rejected silencing `import` wholesale, because it would gag a
// Holder who genuinely clicked an Assignment Link. Nothing here knows what
// channel a Sale was made on, and `online` is unaffected for a structural
// reason rather than a written one: every route that reverses an Online Sale
// writes to its buyer anyway, so the policy it passes is always yes.
//
// SOMEBODY WHO NEVER ACCEPTED IS NEVER MAILED, in either case, and this is the
// rule most easily got wrong. An address in `assigned` was typed by a buyer and
// ignored by whoever received it; the platform never told that person they had
// anything. A mail saying they have lost it would be the platform's FIRST AND
// ONLY word to a stranger, about a ticket they never knew existed — a worse
// intrusion than the silence, and about nothing. Neither entry point below can
// reach such an address: the reassignment path reads `accepted_at` under the row
// lock, and the reversal path's query filters on it.
//
// THE EVENT LEAVES THE HOLDER'S CUSTOMER AREA IN BOTH CASES, AND NO CODE HERE
// DOES IT. That is #325's read, and it already answers both causes without a
// clause per cause: reassignment clears `holder_customer_id` with the address,
// so the row stops matching, and the read excludes a reversed Sale outright.
// Nothing in this ticket changed it — see the decision recorded on
// tellHolderTheyStoppedHolding about why no tombstone row was added.
//
// THE BUYER KEEPS THEIR REVERSED SALE. Nothing in this file writes to a Ticket
// Sale, a Ticket or a Customer: it reads, and it mails. A reversed Ticket Sale
// is never deleted and stays visible to both the Customer and the Organization
// (CONTEXT.md); only the HOLDER's view loses it. And a Sale Reversal remains
// whole-Sale — nothing here reverses or voids part of one, because nothing here
// reverses anything at all.
//
// THE HOLDER STAYS A CUSTOMER. No Customer is deleted, unverified, renamed or
// stripped of their Answers by anything in this flow. They proved an address and
// gave a name; losing a ticket is not a reason to take a person's record away,
// and the mail below says so in as many words.

// NoLongerHoldingMailer is the seam this ticket's message travels through.
//
// A SECOND ONE-METHOD INTERFACE BESIDE AssignmentMailer rather than a second
// method on it, for the reason the first one is narrow at all: this service has
// two messages to send and each seam says exactly which. Nothing in catalog can
// reach the Sale Confirmation, the passcode or the Follow Digest through either.
//
// ITS ZERO VALUE IS nil, AND A SERVICE WITHOUT ONE TELLS NOBODY. That is the
// same posture mailTicketAssignment takes: a deployment that wired no sender
// still reassigns and still reverses, it just says nothing — silent by design,
// and visible in the log rather than in a failed request.
type NoLongerHoldingMailer interface {
	SendNoLongerHolding(ctx context.Context, notice platform.NoLongerHolding) error
}

// TellHoldersOfReversedSales mails every accepted Holder on the named Ticket
// Sales that they are no longer holding a ticket.
//
// THE SECOND CAUSE, AND THE ONLY ONE THAT CROSSES A MODULE BOUNDARY. A Sale
// Reversal happens in sales — through the Customer's own undo, a Sale Import
// undo, or an Operator Reversal — and all three go through one shared reversal
// primitive. Tickets, Holders and this mail are catalog's, so sales declares a
// narrow interface and this satisfies it, exactly as it does for Outstanding
// Answers and the Answer Reminder sweep. Sales learns nothing about what a
// Holder is; catalog learns nothing about why a Sale was reversed, which is
// fitting, since the message is forbidden to mention it.
//
// IT IS CALLED AFTER THE REVERSAL HAS COMMITTED, never inside it, and the
// ordering is not negotiable: a mail cannot be rolled back, so the only honest
// order is to record the fact and then tell somebody about it. The reverse would
// risk telling a Holder they had lost a ticket they still held.
//
// EXACTLY ONCE PER REVERSAL, and that bound belongs to the caller rather than to
// this function. The shared primitive returns an empty set for a Sale somebody
// else already reversed, so a second press, a race between an Operator Reversal
// and the buyer's own undo, or a drain finishing a request whose Sale was
// already voided all reverse nothing and therefore mail nobody.
//
// IT RETURNS AN ERROR AND EXPECTS THE CALLER TO SWALLOW IT. It is stated as an
// error rather than logged silently because the seam's other side may one day
// want to count them; what the caller must not do is undo a reversal because a
// mail failed. The money has moved and the tickets are gone whether or not
// anybody was told.
//
// IT WRITES NOTHING. Not to the Sale, not to the Tickets, not to the Customers.
// The Holders remain Customers, Verified, with the names and Answers they gave;
// the reversed Sale stays whole and stays visible to its buyer. This function
// reads and mails, and that is the whole of it.
//
// THE POLICY ARGUMENT IS THE WHOLE OF WHAT SALES CONTRIBUTES TO THE DECISION
// (#392, ADR 0055): whether the BUYER of these Sales is hearing from the
// platform about the reversal. Which Holder is the buyer is answered here, off
// the read, because that is a fact about Holders and sales must not learn it —
// the boundary is the same one that keeps the cause of the reversal on the far
// side of it.
func (s *Service) TellHoldersOfReversedSales(
	ctx context.Context,
	ticketSaleIDs []string,
	buyer platform.BuyerNoticePolicy,
) error {
	// THE FLAG FIRST, before anything is read. A deployment that never opened
	// Ticket Assignment has no accepted Holders to tell and must not start
	// querying for them (ADR 0045). Its OWN flag: a build running Ticket
	// Questions has said nothing about assignment.
	if !s.ticketAssignmentEnabled || s.noLongerHoldingMailer == nil {
		return nil
	}
	if len(ticketSaleIDs) == 0 {
		return nil
	}

	displaced, err := s.repo.ListDisplacedHoldersForSales(ctx, ticketSaleIDs)
	if err != nil {
		return err
	}
	for _, holder := range displaced {
		// THE ONE SKIP: the buyer holds this Ticket themselves, and this act says
		// nothing to buyers. Silence here is not the platform withholding news from
		// somebody who was chasing it — it is the platform not opening a
		// conversation the Organization chose not to have, with a person who never
		// asked to be in one.
		if holder.IsTheBuyer && !buyer.WritesToTheBuyer() {
			continue
		}
		s.tellHolderTheyStoppedHolding(ctx, holder)
	}
	return nil
}

// tellHolderDisplacedByReassignment is the FIRST cause: the buyer pointed this
// Ticket at somebody else's address.
//
// IT IS HANDED THE ADDRESS RATHER THAN LOOKING IT UP, because by the time it
// runs there is nothing left to look up: the write cleared holder_email and
// accepted_at together, and the Ticket now names its NEW Holder. The displaced
// address comes out of that write, decided under its row lock, which is the only
// place the answer is knowable exactly once — see
// repository.AssignTicketResult.DisplacedHolderEmail.
//
// THE EVENT AND THE SALE'S LANGUAGE COME FROM A RE-READ of the Ticket, through
// the same read the accept flow and the Assignment mail use. That read never
// selected the buyer, the price, the Tax ID or the Sale Confirmation reference —
// so this message is composed from a struct that could not name them if the copy
// asked it to. Reassignment changes neither the Event nor the Sale's language,
// so reading after the write is as good as reading before it and costs one fewer
// query on the ordinary path where nobody was displaced.
//
// THE DISPLACED HOLDER IS MAILED AND THE NEW ONE IS TOO, and they are two
// separate messages to two different people saying opposite things. Neither
// names the other, and neither names the buyer.
//
// #392'S POLICY DOES NOT REACH THIS CAUSE, and the zero value below says so.
// That policy is about a reversal the platform performs on the buyer's Sale
// while deciding whether to write to them at all; a reassignment is the BUYER'S
// OWN ACT, made on the buyer's own page, and a buyer who hands on the Ticket they
// were holding themselves has just been told what happened by doing it. Nobody
// is being spared an unsolicited mail here, so nothing is gated.
func (s *Service) tellHolderDisplacedByReassignment(
	ctx context.Context,
	ticket *repository.AssignmentLinkTicket,
	displacedEmail string,
) {
	if displacedEmail == "" || s.noLongerHoldingMailer == nil {
		return
	}
	s.tellHolderTheyStoppedHolding(ctx, repository.DisplacedHolder{
		TicketID:    ticket.ID,
		HolderEmail: displacedEmail,
		EventName:   ticket.EventName,
		SaleLocale:  ticket.SaleLocale,
	})
}

// tellHolderTheyStoppedHolding composes and sends the one message, and is the
// ONLY place in the platform where it is composed.
//
// BOTH CAUSES MEET HERE, which is how "one rule, one behaviour" stops being a
// promise in a document and becomes a property of the code. A future change that
// wanted to say something different about a reversal would have to add a branch
// in this function, in plain view, rather than quietly diverging in one of two
// call sites.
//
// THE LANGUAGE IS RESOLVED RECIPIENT-FIRST, through assignmentMailLocale — the
// same inversion of ADR 0033's usual chain, for the same reason and by the same
// code. This reader is not party to the sale: they did not buy anything, were
// not on the page the buyer paid on, and may not share the buyer's language at
// all. Unlike the Assignment mail's reader, though, this one is CERTAINLY a
// Customer — they accepted, which minted or matched a record — so the remembered
// Mail Locale is usually there to be found, and the sale's is the fallback.
//
// NO TOMBSTONE ROW WAS ADDED TO THE CUSTOMER AREA, and this is the decision
// #327 took where it could have gone either way. #325's `holding` read drops the
// row on reassignment and excludes a reversed Sale, so the Event simply leaves.
// The alternative — leaving a struck-through row saying "you no longer hold
// this" — was rejected for three reasons. It would say the same thing twice, and
// the mail is the telling this ticket is built around; it would keep an Event a
// person no longer holds a ticket to sitting in their account indefinitely, with
// no rule for when it ages out; and a row that persisted past a reassignment
// would be a standing record that this Customer once held a Ticket that now
// belongs to somebody else, which is exactly the assignment history ADR 0046
// refused to keep. The Area shows what somebody holds. They hold nothing.
//
// A FAILURE IS SWALLOWED AND LOGGED, never returned to whoever caused it. The
// buyer's reassignment stands and the Sale Reversal stands: neither may be undone
// because a provider was unwell, and a Holder who was not told is a fact for an
// operator to count rather than an error for a buyer to see. It reuses
// logAssignmentMailFailure, which is careful to log NEITHER the address nor the
// Event — a log aggregator is a wider audience than an inbox, and which Event
// somebody has stopped holding a ticket to is a fact about their plans.
func (s *Service) tellHolderTheyStoppedHolding(ctx context.Context, holder repository.DisplacedHolder) {
	if s.noLongerHoldingMailer == nil || holder.HolderEmail == "" {
		return
	}
	notice := platform.NoLongerHolding{
		To:        holder.HolderEmail,
		EventName: holder.EventName,
		Locale:    s.assignmentMailLocale(ctx, holder.HolderEmail, holder.SaleLocale.String),
	}
	if err := s.noLongerHoldingMailer.SendNoLongerHolding(ctx, notice); err != nil {
		s.logAssignmentMailFailure("no longer holding notice delivery failed", err)
	}
}
