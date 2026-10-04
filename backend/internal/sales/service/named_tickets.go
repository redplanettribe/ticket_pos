package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/sales/repository"
)

// Named Tickets at begin-checkout (ADR 0076, #669): on an Event that requires
// them, the checkout is refused until every Ticket but the buyer's own names a
// Holder, and every Ticket answers every required Ticket Question of its Ticket
// Type. What the buyer typed then waits on the Payment for the commit (#670).

// CheckoutHolderInput is one Holder address as the buyer named it at checkout:
// which Ticket Type, which of that Ticket Type's Tickets (1..quantity, the same
// numbering CheckoutAnswerInput uses), and the address.
//
// THE ADDRESS IS ALREADY NORMALISED by the handler through
// catalog.ParseHolderEmail, which is where a malformed one became a field
// error. The Ticket Type and the index are untrusted: one naming nothing the
// basket holds is dropped, exactly as an Answer naming nothing is, because only
// this side knows the basket.
type CheckoutHolderInput struct {
	TicketTypeID string
	TicketIndex  int
	HolderEmail  string
}

// judgeNamedTickets decides whether this basket may be bought under the
// Event's Named Tickets requirement, and what to hold on the Payment if it may.
//
// It returns nil and no error when the requirement does not bind - the setting
// is off, Ticket Assignment is dark, or the doors have opened - and then the
// checkout proceeds exactly as it always has, its Answers held best-effort and
// any addresses in the body ignored. It returns ErrNamedTicketsIncomplete when
// a Ticket still owes something, and the hold otherwise.
//
// THE SEAT IS THE COMMIT'S. The basket is built the way the commit builds it -
// each Payment Line's CommitLine, priced at the buyer unit price it will be
// sold at - and a same-basket Upgrade's surrendered free line is dropped by the
// commit's own SameBasketUpgrade before SelfHeldSeatOf picks, under the same
// gate the commit applies (an election, in a build that seats anybody). So the
// Ticket excused from naming a Holder here is the one the commit seats the
// buyer on, and a Ticket that will never exist is asked for nothing.
//
// The seat is carried across as (Ticket Type, index), never as a slice
// position: this basket is in catalog order and the commit's is in whatever
// order it reads the Payment's lines, and the Ticket Type is what both agree on
// because a Payment holds one line per Ticket Type.
func (s *Service) judgeNamedTickets(
	ctx context.Context,
	event *repository.CheckoutEvent,
	byID map[string]repository.EventTicketType,
	paymentLines []repository.PaymentLine,
	in BeginCheckoutInput,
	buyerEmail string,
	now time.Time,
) (*repository.NamedHold, error) {
	if !catalog.NamedTicketsApply(event.RequiresNamedTickets, s.ticketAssignmentEnabled, event.StartsAt, now) {
		return nil, nil
	}

	lookup := func(ticketTypeID string) repository.CatalogEntry {
		tt := byID[ticketTypeID]
		return repository.CatalogEntry{PriceCents: tt.PriceCents, SortOrder: tt.SortOrder, Name: tt.Name}
	}
	basket := make([]repository.CommitLine, 0, len(paymentLines))
	for _, line := range paymentLines {
		basket = append(basket, line.CommitLine())
	}
	if in.UpgradeElected {
		kept, _, err := repository.SameBasketUpgrade(basket, lookup, func() (repository.UpgradeEligibility, error) {
			customerID, _, err := s.customers.ResolveByEmail(ctx, buyerEmail)
			if err != nil {
				return repository.UpgradeEligibility{}, err
			}
			return s.repo.ReadUpgradeEligibility(ctx, event.ID, customerID)
		})
		if err != nil {
			return nil, err
		}
		basket = kept
	}

	seatTicketTypeID, seatIndex := "", 0
	if seat, ok := repository.SelfHeldSeatOf(basket, lookup); ok {
		seatTicketTypeID, seatIndex = basket[seat.Line].TicketTypeID, seat.TicketIndex
	}

	quantities := make(map[string]int, len(basket))
	ticketTypeIDs := make([]string, 0, len(basket))
	for _, line := range basket {
		quantities[line.TicketTypeID] = line.Quantity
		ticketTypeIDs = append(ticketTypeIDs, line.TicketTypeID)
	}

	// The questions asked now, read through the query the Storefront's form is
	// drawn from, so a question awaiting review is neither shown nor owed.
	// None at all while Ticket Questions are dark: then only the addresses are.
	var asked []catalog.AskedQuestion
	var answered []catalog.HeldAnswer
	if s.ticketQuestionsEnabled {
		var err error
		asked, err = s.repo.ListCheckoutQuestions(ctx, ticketTypeIDs)
		if err != nil {
			return nil, err
		}
		answered = catalog.HoldableCheckoutAnswers(asked, quantities, submittedAnswers(in.Answers))
	}

	// The first address given for a Ticket is the one meant, as the first
	// Answer is (catalog.HoldableCheckoutAnswers); one naming a Ticket the
	// basket does not hold is dropped.
	type ticketKey struct {
		ticketTypeID string
		index        int
	}
	named := make(map[ticketKey]string, len(in.Holders))
	for _, given := range in.Holders {
		key := ticketKey{given.TicketTypeID, given.TicketIndex}
		if quantity, ok := quantities[key.ticketTypeID]; !ok || key.index < 1 || key.index > quantity {
			continue
		}
		if _, seen := named[key]; !seen && given.HolderEmail != "" {
			named[key] = given.HolderEmail
		}
	}

	var tickets []catalog.NamedTicket
	var holders []repository.HeldHolder
	for _, line := range basket {
		for index := 1; index <= line.Quantity; index++ {
			key := ticketKey{line.TicketTypeID, index}
			selfHeld := line.TicketTypeID == seatTicketTypeID && index == seatIndex
			ticket := catalog.NamedTicket{TicketTypeID: line.TicketTypeID, TicketIndex: index, SelfHeld: selfHeld}
			// The buyer's own Ticket holds no address even when the body named
			// one: the commit seats the buyer on it, and an address here would
			// be a second answer to "whose is this".
			if !selfHeld {
				ticket.HolderEmail = named[key]
				if ticket.HolderEmail != "" {
					holders = append(holders, repository.HeldHolder{
						TicketTypeID: line.TicketTypeID,
						TicketIndex:  index,
						HolderEmail:  ticket.HolderEmail,
					})
				}
			}
			tickets = append(tickets, ticket)
		}
	}

	if owed := catalog.OwedByNamedTickets(tickets, asked, answered); len(owed) > 0 {
		return nil, catalog.ErrNamedTicketsIncomplete(owed)
	}
	return &repository.NamedHold{Holders: holders, Answers: answered}, nil
}
