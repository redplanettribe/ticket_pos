package service

import "context"

// SurrenderableFreeTickets reports how many free Tickets the person at this
// email could give up on this Event through an Upgrade — their own accepted
// Self-held Ticket on an active, single-Ticket, zero-priced Online Sale
// (ADR 0074).
//
// It exists for one caller outside this module: the public Event read, which
// publishes the figure to a SIGNED-IN Customer so the checkout dialog can put
// the Upgrade Prompt in front of a buyer whose free Ticket is unambiguously
// theirs to surrender. Catalog asks sales through this seam rather than
// restating the predicate in a query of its own, for the reason
// CustomerEventHoldings gives: one definition of a sales rule is what keeps the
// page and the commit agreeing, and here that matters more than anywhere else —
// the commit re-evaluates this very predicate inside its own transaction, and a
// surface that offered an Upgrade the commit would refuse is the one failure
// this ticket exists to make impossible.
//
// IT RETURNS A COUNT AND NOT A VERDICT. Whether an Upgrade is offered depends on
// the basket too — one free Ticket in play and no more, counting earlier Sales
// and the cart together — and no basket exists when the Event page is read. The
// arithmetic that joins the two is
// repository.UpgradeEligibility.OffersUpgrade, and it is where any surface
// deciding to offer must go.
//
// THE EMAIL MUST BE ONE THE CALLER HAS PROVEN THE REQUESTER OWNS, exactly as on
// CustomerEventHoldings and for exactly the same reason: nothing here checks it
// and nothing here can, and a route that took an arbitrary address would be an
// oracle answering whether a given person holds a free Ticket to a given Event.
//
// A closed Ticket Assignment flag answers 0 rather than reading anything. No
// Ticket is self-held in that build, so nothing is surrenderable and no Upgrade
// can be performed; the Sales this would find are historical rows the dark
// build must not offer to destroy.
//
// THE GATE IS HERE AND NOT IN THE PREDICATE, and the commit path has its own
// spelling of the same flag: `in.Terms.SelfHeld`, which already decides whether
// CommitSales seats anybody. Both read one env var, so the two cannot mean
// different things — but a third caller of repository.UpgradeEligibilityFor
// that gates on neither would be a build offering what it cannot perform.
func (s *Service) SurrenderableFreeTickets(ctx context.Context, eventID, email string) (int, error) {
	if !s.ticketAssignmentEnabled {
		return 0, nil
	}
	customerID, _, err := s.customers.ResolveByEmail(ctx, email)
	if err != nil {
		return 0, err
	}
	eligibility, err := s.repo.ReadUpgradeEligibility(ctx, eventID, customerID)
	if err != nil {
		return 0, err
	}
	return eligibility.SurrenderableFreeTickets, nil
}
