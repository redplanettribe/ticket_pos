package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
)

// The buyer assigns a Ticket to an email address (#324, parent #322).
//
// WHAT THIS SLICE IS. A Customer who bought four tickets gives an address for
// each one, from their own sale page after paying, and can finally see which
// Ticket went to which address — the thing four identical Answer Links never let
// them do, deliberately, since a link that disclosed whose it was would disclose
// it to the group chat too.
//
// NO MAIL IS SENT HERE. Not one line of this file writes to anybody. The
// Assignment mail, the Assignment Link and the accept flow that makes `accepted`
// reachable are #325's; until they land, an assignment is a note the buyer keeps
// for themselves and the platform holds an address it does nothing with. That is
// stated plainly rather than treated as an implementation detail, because it is
// also the honest cost of shipping this slice on its own: the platform is
// briefly holding third-party addresses with no purpose, closed by the mail that
// gives them one and by #322's purge that gives them an end. The flag is what
// keeps that window shut until both exist.
//
// ASSIGNMENT HAPPENS AFTER PURCHASE AND NEVER AT CHECKOUT. Nothing in the
// checkout path reaches this file, so an abandoned Payment leaves no third
// party's contact details behind — and nothing here can block or delay a
// checkout, a door sale or a Sale Import, none of which call it.
//
// TICKETS SOLD, CAPACITY AND THE PURCHASE LIMIT ARE UNTOUCHED. Those all sum
// ticket_sale_lines.quantity (ADR 0043); this writes columns on `tickets`, which
// none of them read. Assignment moves no published figure.

// AssignOwnTicket names the email address that holds one Ticket of the buyer's
// own Ticket Sale, creating the assignment or replacing the one that was there.
//
// ONE ENTRY POINT FOR ASSIGN, REASSIGN AND CORRECT-A-TYPO, because all three are
// the same statement: this Ticket's Holder address is now X. A separate
// "reassign" route would be a second place for the Answer-clearing rule to be
// remembered, and the first place it would be forgotten.
//
// PARTIAL ASSIGNMENT IS THE NORMAL CASE. It takes ONE Ticket id, so a sale of
// four with two addresses known is two calls and not a form that refuses until
// all four are filled. Nothing here reads how many of the Sale's other Tickets
// are assigned.
//
// customerID and sessionTicketSaleID come from the Customer Session the
// middleware validated, and NEITHER comes from the request — the same rule
// AnswerOwnTicketQuestion beside it is held to. A Confirmation Link session may
// assign, and may only reach the one Sale it names.
func (s *Service) AssignOwnTicket(
	ctx context.Context,
	customerID, sessionTicketSaleID, ticketSaleID, ticketID string,
	holderEmail string,
) ([]BuyerTicketAnswersView, error) {
	// THE FLAG FIRST, before anything is read and before the address is so much
	// as parsed, so that a closed build answers exactly as a build that never
	// had the feature (ADR 0045). Its OWN flag: a deployment that opened Ticket
	// Questions has said nothing about assignment.
	if !s.ticketAssignmentEnabled {
		return nil, catalog.ErrTicketAssignmentUnavailable()
	}
	// The link session's narrowing, before the query rather than after it. A
	// session asking about a Sale other than its own is answered as if that Sale
	// did not exist.
	if sessionTicketSaleID != "" && sessionTicketSaleID != ticketSaleID {
		return nil, catalog.ErrTicketNotFound()
	}

	// THE SAME SCOPED READ THE LISTING USES, with its customer_id clause, and
	// never a lookup by Ticket id alone. Another Customer's Ticket Sale is
	// answered as if it did not exist — not refused, not distinguished from a
	// Ticket that was never minted — so that ids cannot be probed here any more
	// than they can on the read beside it.
	tickets, err := s.repo.ListAnswerableTicketsForBuyer(ctx, customerID, ticketSaleID)
	if err != nil {
		return nil, err
	}
	ticket := findBuyerTicket(tickets, ticketID)
	if ticket == nil {
		return nil, catalog.ErrTicketNotFound()
	}

	// THE ADDRESS IS PARSED AFTER THE TICKET IS RESOLVED, deliberately. Parsing
	// first would answer "that is not an email address" for a Ticket the caller
	// does not own, which tells them their id was at least reachable — the same
	// oracle the empty-list rule on the read exists to deny.
	email, ok := catalog.ParseHolderEmail(holderEmail)
	if !ok {
		return nil, catalog.ErrInvalidHolderEmail()
	}

	if err := s.assignmentWindowOpen(ticket); err != nil {
		return nil, err
	}

	// THE RATIONING, BEFORE THE WRITE AND NEVER AFTER IT (#332). See
	// assignmentMailAllowed for why a refusal here refuses the ASSIGNMENT and
	// not merely the mail.
	if err := s.assignmentMailAllowed(ctx, customerID, ticket, email); err != nil {
		return nil, err
	}

	// A BUYER MAY ASSIGN A TICKET TO THEIR OWN ADDRESS, and nothing here checks
	// otherwise. A parent buying for three children holds one themselves; a
	// rule that refused the buyer's own address would refuse the commonest shape
	// this feature has.
	assignment, err := s.repo.AssignTicketToHolder(ctx, ticket.ID, email, s.now())
	if err != nil {
		return nil, err
	}

	// ONE MAIL PER ASSIGNMENT THAT ACTUALLY CHANGED SOMETHING (#325). A buyer who
	// presses save twice, or who "corrects" a typo back to what it already said,
	// has changed nothing about who holds this Ticket — and the person at that
	// address has already been written to once. `Changed` is the repository's
	// report of whether a row moved, and it is the whole of the rationing this
	// route needs: without it, a doubled click is a doubled mail to somebody who
	// never asked for the first.
	//
	// A REASSIGNMENT MAILS BOTH ENDS, AND THEY ARE TWO SEPARATE MESSAGES (#327).
	// The new address gets the Assignment mail; a Holder who had ACCEPTED and has
	// just been displaced gets the No Longer Holding mail. Neither names the
	// other, and neither names the buyer.
	//
	// ONLY SOMEBODY WHO ACCEPTED IS TOLD THEY LOST IT. An address that was typed
	// and ignored was never told it had anything, so telling it now that it has
	// lost something would be the platform's first and only word to that person.
	// The write reports the displaced address ONLY when the row it replaced was
	// accepted — see repository.AssignTicketResult.DisplacedHolderEmail — so an
	// `assigned` address is unreachable from here rather than filtered out here.
	//
	// THE SEND HAPPENS AFTER THE WRITE COMMITTED, never inside it. A mail cannot
	// be rolled back, so the only honest order is to record the fact and then
	// tell somebody about it; the reverse would risk a stranger holding a link to
	// an assignment that never happened.
	//
	// AND THE LEDGER ROW IS WRITTEN AFTER THE MAIL WENT, never before and never
	// instead (#332). Both allowances are spent by SENDS, so a mail the provider
	// refused costs the buyer nothing: the alternative would ration somebody out
	// of a message that never reached an inbox, permanently, since the
	// per-Ticket allowance never refills.
	if assignment.Changed {
		if s.mailAssignedTicket(
			ctx, ticket.ID, email, assignment.AssignedAt, assignment.DisplacedHolderEmail,
		) {
			s.recordAssignmentMailSent(ctx, ticket.ID, customerID)
		}
	}

	// THE WHOLE SALE COMES BACK rather than the one Ticket that changed, exactly
	// as it does after an Answer is written. The buyer's page is a list whose
	// rows are read together — "two of my four are assigned" is derived from all
	// of them, and a reassignment may have sent one row's outstanding count back
	// up — so returning a single row would leave the surface holding a stale
	// total beside a fresh one.
	//
	// RE-READ AND NOT PATCHED IN MEMORY: the rows loaded above are the state
	// BEFORE the write, and the Answers this call may have cleared are still on
	// them.
	fresh, err := s.repo.ListAnswerableTicketsForBuyer(ctx, customerID, ticketSaleID)
	if err != nil {
		return nil, err
	}
	return s.buyerTicketAnswersViews(ctx, fresh)
}

// assignmentWindowOpen turns the domain's reading of the assignment window into
// the refusal a caller hears.
//
// ONLY THE WRITE PATH CONSULTS IT. The read reports the window as a token on
// every row and refuses nothing: a buyer looking at a door sale, a reversed sale
// or an Event that has already happened must still see who they assigned their
// Tickets to, exactly as a reversed Sale keeps its Answers readable.
func (s *Service) assignmentWindowOpen(ticket *repository.AnswerableTicket) error {
	switch catalog.AssignmentWindow(
		ticket.Channel, ticket.SaleStatus, nullTimeOrNil(ticket.EventStartsAt), s.now(),
	) {
	case catalog.AssignmentRefusedChannel:
		return catalog.ErrAssignmentChannelUnsupported()
	case catalog.AssignmentRefusedSaleReversed:
		return catalog.ErrAssignmentSaleReversed()
	case catalog.AssignmentRefusedEventStarted:
		return catalog.ErrAssignmentEventStarted()
	default:
		return nil
	}
}

// fillBuyerAssignment puts one Ticket's assignment onto the buyer's row, or
// leaves the row exactly as it was while the flag is closed.
//
// THE EARLY RETURN IS THE FLAG'S WHOLE EFFECT ON THE READ. Every field it would
// otherwise set is `omitempty`, so a closed build sends the payload a build
// without the feature sends — no `assignment_state`, no `holder_email`, not even
// an `assignable: false`. That is what makes ADR 0045's "no surface differs"
// hold for this feature on the one page it touches, and it is what the flag test
// asserts against the raw bytes.
func (s *Service) fillBuyerAssignment(view *BuyerTicketAnswersView, ticket repository.AnswerableTicket) {
	if !s.ticketAssignmentEnabled {
		return
	}

	holderEmail := ""
	if ticket.HolderEmail.Valid {
		holderEmail = ticket.HolderEmail.String
	}
	assignedAt := nullTimeOrNil(ticket.AssignedAt)
	acceptedAt := nullTimeOrNil(ticket.AcceptedAt)

	// DERIVED, NEVER STORED, and derived HERE through the one shared function so
	// that this page, the Organization's guest list and the export cannot mean
	// different things by the same word.
	view.AssignmentState = string(catalog.AssignmentState(holderEmail, assignedAt, acceptedAt))
	view.HolderEmail = holderEmail
	view.AssignedAt = assignedAt
	view.AcceptedAt = acceptedAt

	refusal := catalog.AssignmentWindow(
		ticket.Channel, ticket.SaleStatus, nullTimeOrNil(ticket.EventStartsAt), s.now(),
	)
	view.Assignable = refusal == catalog.AssignmentWindowOpen
	view.AssignableRefusal = assignmentRefusalToken(refusal)
}

// assignmentRefusalToken names a closed assignment window for the wire.
//
// A TOKEN AND NEVER A SENTENCE, exactly as answerRefusalToken is: the Storefront
// owns the words, in the reader's language, and a sentence composed here would
// be composed in whatever language this file happens to be written in.
func assignmentRefusalToken(refusal catalog.AssignmentRefusal) string {
	switch refusal {
	case catalog.AssignmentRefusedChannel:
		return "channel_unsupported"
	case catalog.AssignmentRefusedSaleReversed:
		return "sale_reversed"
	case catalog.AssignmentRefusedEventStarted:
		return "event_started"
	default:
		return ""
	}
}

// mailAssignedTicket re-reads the one Ticket through the accept flow's own read
// and sends the mail (or mails) this assignment owes: the Assignment mail to the
// new address always, and the No Longer Holding mail to a Holder this
// reassignment displaced (#327).
//
// A SECOND READ RATHER THAN THE ROW ALREADY IN HAND, and it buys two things. The
// buyer's row (repository.AnswerableTicket) carries the Sale's id and its
// Confirmation reference and does not carry the Event's name — so composing the
// mail from it would mean either adding the Event name to the buyer's read or
// carrying a struct full of things this message must never mention past the one
// place that could mention them. Reading through GetAssignmentLinkTicket means
// the mail is composed from a struct that never selected the buyer, the price,
// the Tax ID or the reference at all.
//
// It is also the read the ACCEPT flow uses, so the Event name a Holder sees in
// their inbox and the one they see on the page cannot come from two places and
// differ.
//
// A FAILURE HERE IS SWALLOWED, deliberately, and the assignment stands. The
// buyer's record of who they gave which ticket to is worth keeping even when the
// mail did not go out, and they can send a fresh one by correcting the address.
//
// IT REPORTS WHETHER AN ASSIGNMENT MAIL ACTUALLY WENT (#332), which is what the
// ledger row beside the call site is a claim about. A mail that was never
// composed — no sender wired, no link secret, an unreadable Ticket — or one the
// provider refused must not spend anybody's allowance, because both allowances
// ration the writing to strangers and nothing was written to anybody.
//
// THE RETURN SPEAKS ONLY FOR THE ASSIGNMENT MAIL, never for the No Longer
// Holding mail this also sends (#327). The two are rationed differently and must
// not be conflated: the assignment mail writes to a stranger and is capped, while
// telling a Holder who ACCEPTED that they have lost the Ticket is owed to a
// Customer and is not somebody's allowance to spend.
func (s *Service) mailAssignedTicket(
	ctx context.Context,
	ticketID, holderEmail string,
	assignedAt time.Time,
	displacedHolderEmail string,
) bool {
	if s.mailer == nil && s.noLongerHoldingMailer == nil {
		return false
	}
	ticket, err := s.repo.GetAssignmentLinkTicket(ctx, ticketID)
	if err != nil || ticket == nil {
		s.logAssignmentMailFailure("assignment mail not composed: ticket unreadable", err)
		return false
	}
	sent := false
	if s.mailer != nil {
		sent = s.mailTicketAssignment(ctx, ticket, holderEmail, assignedAt)
	}
	// THE DISPLACED HOLDER IS TOLD SECOND, and told nothing about who took the
	// Ticket from them (#327). The read above is shared between the two messages
	// on purpose: the Event name the new Holder is invited to and the one the old
	// Holder is told they have lost come from one struct, which never selected the
	// buyer, the price or the reference at all.
	//
	// ORDER BUYS NOTHING HERE and neither send depends on the other: both are best
	// effort, and a failure of either leaves the assignment standing. It reads
	// naturally as the order the two facts happened in.
	s.tellHolderDisplacedByReassignment(ctx, ticket, displacedHolderEmail)
	return sent
}

// assignmentMailAllowed is the whole of #332's refusal, and it stands BEFORE the
// write rather than beside the send.
//
// WHY IT REFUSES THE ASSIGNMENT AND NOT MERELY THE MAIL, which is the decision
// this ticket most wants recorded. The cheaper-looking design is to write the
// new address and skip the send when an allowance is spent. It is a trap.
// `assigned_at` is what every Assignment Link is signed over, so moving it kills
// every outstanding link for this Ticket — and a write with no send would leave
// the Holder who already had one holding a dead link, the new address holding
// nothing, and the buyer's page cheerfully showing an address that was never
// told. Refusing outright leaves the previous Holder's live link alive and the
// buyer with something to read. A refusal is recoverable; a silently unreachable
// Ticket is not.
//
// AN UNCHANGED ADDRESS IS NOT RATIONED. Submitting the address a Ticket already
// carries mails nobody and writes nothing (repository.AssignTicketResult.
// Changed), so it may not be refused either: a buyer pressing save twice on a
// Ticket whose allowance is spent would otherwise be told their own current
// state is forbidden. Only a change of ADDRESS is a would-be mail.
//
// THE COUNT IS READ, NOT LOCKED. Two saves racing on one Ticket can each see
// room and both send, one over the cap. That is accepted for the same reason the
// OTP limits accept it: the control is protecting a sending reputation against
// volume, and an off-by-one under a race is not the shape of the attack. The
// cost of the alternative — holding a lock across a network send — is not worth
// paying here.
func (s *Service) assignmentMailAllowed(
	ctx context.Context,
	customerID string,
	ticket *repository.AnswerableTicket,
	holderEmail string,
) error {
	if ticket.HolderEmail.Valid && ticket.HolderEmail.String == holderEmail {
		return nil
	}

	sentForTicket, err := s.repo.CountAssignmentMailsForTicket(ctx, ticket.ID)
	if err != nil {
		return err
	}
	// The window's start is derived from THIS SERVICE'S clock and is never a
	// caller's: a caller who could name it could name a moment a second ago and
	// lift the limit on demand.
	sentByBuyer, err := s.repo.CountAssignmentMailsForBuyer(
		ctx, customerID, s.now().Add(-catalog.AssignmentMailWindow),
	)
	if err != nil {
		return err
	}

	switch catalog.MayMailAssignment(catalog.AssignmentMailInputs{
		SentForTicket:       sentForTicket,
		SentByBuyerInWindow: sentByBuyer,
		Limits:              s.assignmentMailLimits,
	}) {
	case catalog.AssignmentMailRefusedTicketCap:
		return catalog.ErrAssignmentMailCapReached()
	case catalog.AssignmentMailRefusedBuyerRate:
		return catalog.ErrAssignmentRateLimited()
	default:
		return nil
	}
}

// recordAssignmentMailSent spends one unit of both allowances at once.
//
// A FAILURE IS SWALLOWED AND LOGGED, on the same reasoning the mail's own
// failure is: the mail has already gone and the assignment already stands, so
// there is nothing left to roll back. The cost of a lost row is one extra mail
// this Ticket may later send, which is the direction worth failing in — the
// other one rations a buyer out of a message somebody did receive.
//
// THE ADDRESS IS NOT PASSED AND NOT STORED. The ledger says a mail went out for
// this Ticket at this moment; where it went is the Ticket's own column, which
// the Holder Address Purge takes at Event start (migration 081). A copy here
// would be that promise written backwards.
func (s *Service) recordAssignmentMailSent(ctx context.Context, ticketID, buyerCustomerID string) {
	if err := s.repo.RecordAssignmentMailSent(ctx, ticketID, buyerCustomerID, s.now()); err != nil {
		s.logAssignmentMailFailure("assignment mail ledger not written: the cap under-counts by one", err)
	}
}
