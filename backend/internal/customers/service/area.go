package service

import (
	"context"
	"database/sql"
	"sort"
	"time"

	"github.com/peter/ticket_pos/backend/internal/customers/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// TicketSaleLineView is one Ticket Type and the quantity bought within a Ticket
// Sale.
type TicketSaleLineView struct {
	TicketTypeName string `json:"ticket_type_name"`
	Quantity       int    `json:"quantity"`
	UnitPriceCents int    `json:"unit_price_cents"`
}

// EventView is the Event a Ticket Sale was for, as the Customer Area shows it.
type EventView struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Slug      string  `json:"slug"`
	StartsAt  *string `json:"starts_at"`
	EndsAt    *string `json:"ends_at"`
	Timezone  *string `json:"timezone"`
	VenueName *string `json:"venue_name"`
}

// OrganizationView is the Organization that sold a Ticket Sale.
type OrganizationView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// TicketSaleView is one of the Customer's Ticket Sales: the Event and its date,
// the Organization, what was bought, and the Sale Confirmation reference they can
// quote to a promoter.
type TicketSaleView struct {
	ID              string               `json:"id"`
	ConfirmationRef string               `json:"confirmation_ref"`
	SoldAt          string               `json:"sold_at"`
	Status          string               `json:"status"`
	AmountCents     int                  `json:"amount_cents"`
	Currency        string               `json:"currency"`
	Lines           []TicketSaleLineView `json:"lines"`
	Event           EventView            `json:"event"`
	Organization    OrganizationView     `json:"organization"`
	// TaxIDType and TaxIDNumber are the Tax ID this sale was transacted under
	// (ADR 0016), so a Customer can tell a personal purchase from one made under
	// a company RUC. They are the sale's immutable snapshot, never the
	// Customer's current stored assertion: editing a profile or buying again
	// under a different Tax ID changes nothing here.
	//
	// Both are null together on sales recorded before the feature and on
	// imported sales that never carried one; history is not backfilled, and the
	// client draws "—" rather than a value nobody supplied.
	TaxIDType   *string `json:"tax_id_type"`
	TaxIDNumber *string `json:"tax_id_number"`
	// Reversible reports whether the Customer could undo this Ticket Sale right
	// now (ADR 0018) — and it is the exact question the reversal endpoint asks
	// itself, so a true here is an offer the API will honour if taken promptly.
	//
	// It is an answer about this instant and nothing more. It is not a promise:
	// the window is offered, not guaranteed, and a true read a minute ago may be
	// a refusal a minute from now.
	//
	// Three things must all hold. The sale is an active Online Sale — an
	// In-Person Sale or an imported one was never collected by the platform, so
	// the platform has nothing to give back, and a sale already reversed cannot
	// be reversed again. Its Reversal Window is open. And its Payment can in fact
	// be undone: a free claim always can, since there is nothing to return, while
	// a paid one can only when the Payment Provider that collected it supports
	// reversal. That last clause is why a paid purchase inside its window can
	// report false — the alternative is an Undo button that fails when pressed.
	Reversible bool `json:"reversible"`
	// ReversibleUntil is the instant this offer runs out, RFC3339 in UTC, so the
	// Customer can be told how long they have.
	//
	// It is named for Reversible, not for the Reversal Window, because it is the
	// deadline on the offer above rather than a publication of the platform's
	// rule. The two are the same instant today only because every Payment this
	// deployment accepts happens to be reversible; a Payment Provider that could
	// not give money back would leave a sale with an open Window and no offer at
	// all, and this field would correctly be null rather than reporting a
	// deadline nobody could act on.
	//
	// Null whenever Reversible is false, and deliberately so: a deadline on a sale
	// nobody may reverse means nothing, and a client that cannot draw one cannot
	// mislead somebody with it.
	ReversibleUntil *string `json:"reversible_until"`
	// ReversalPending is true exactly while a reversal of this sale is in
	// progress: the Customer asked to undo it and the platform has not finished
	// (ADR 0024). See repository.TicketSaleRow.ReversalPending for what counts.
	//
	// It is not a status and must never be drawn as one. The sale's own Status is
	// still "active" and its tickets are still valid, because no money is known to
	// have moved — what this field says is that the platform is finding out, and
	// the honest thing to show is "we're processing your refund" beside tickets
	// that still work.
	//
	// It sits beside Reversible rather than folding into it, because the two
	// answer different questions: whether this sale MAY be undone, and whether it
	// already HAS been asked about. Reversible stays the eligibility rule alone,
	// shared verbatim with the guest surfaces and the endpoint, so that it keeps
	// meaning one thing. A surface withdraws its Undo action on either — pressing
	// while a request is in flight makes nothing new happen — and that composing
	// is the surface's job, not this rule's.
	ReversalPending bool `json:"reversal_pending"`
	// ReversalStatus is where this sale's most recent Reversal Request stands —
	// "in_flight", "succeeded", "refused" or "needs_attention" — and null when the
	// buyer has never asked (ADR 0024).
	//
	// It exists because ReversalPending alone cannot tell a surface why a pending
	// refund stopped being pending. An Unresolved Reversal leaves the sale active
	// and ReversalPending false, exactly as a refusal does, and the two owe the
	// buyer opposite things: a refusal is theirs to be told, while an Unresolved
	// Reversal must be met with silence, because nobody knows whether the money
	// went back. Only "refused" may be presented as a refusal.
	ReversalStatus *string `json:"reversal_status"`
}

// CustomerAreaView is the signed-in Customer's purchases, split into what is
// still to come and what has already happened, across every Organization they
// have ever bought from.
type CustomerAreaView struct {
	Upcoming []TicketSaleView `json:"upcoming"`
	Past     []TicketSaleView `json:"past"`
	// Holding is the Events somebody ELSE bought a ticket for and assigned to
	// this Customer, which they accepted (#325, parent #322, ADR 0046).
	//
	// A THIRD LIST RATHER THAN ROWS MIXED INTO THE FIRST TWO, and the separation
	// is the disclosure rule made structural. These are not this person's
	// purchases: the Ticket Sale, the money, the Sale Confirmation and the
	// Reversal Window all stayed with the buyer. A held Ticket rendered as a
	// TicketSaleView would need an amount, a confirmation reference and a
	// reversal offer, and a Holder may see none of them — so it is a different
	// shape carrying different facts, and no surface can accidentally draw an
	// Undo button on somebody else's purchase.
	//
	// Empty for almost everybody, and empty for a Confirmation Link session by
	// construction: that credential opens ONE Ticket Sale, and a Ticket held on
	// somebody else's sale is not it.
	Holding []HeldTicketView `json:"holding"`
}

// HeldTicketView is one Ticket this Customer holds because a Holder accepted it,
// as their Customer Area shows it.
//
// WHAT IS ABSENT IS THE WHOLE OF THE TYPE'S DESIGN: no buyer, no price, no
// amount, no Sale Confirmation reference, no Tax ID, no reversal state, no Undo,
// and no sibling Tickets. A Holder holds the ticket and nothing else
// (CONTEXT.md); everything about the PURCHASE belongs to the person who made it.
//
// The Event and the Organization are here because they are already public — the
// Event has a Storefront page anybody can read — and because they are the whole
// reason this list exists: a Holder wanted a way back to the Event that does not
// depend on keeping the mail.
type HeldTicketView struct {
	// TicketID is this person's handle on the thing they hold. It is not a
	// credential: every route that acts on a Ticket is reached either by a signed
	// token or by the buyer's own session, and neither takes an id from here.
	TicketID string `json:"ticket_id"`
	// AcceptedAt is when they accepted, RFC3339 in UTC.
	AcceptedAt string `json:"accepted_at"`
	// TicketTypeName is what kind of ticket it is — public, and a row on the
	// Event's own page with its price beside it. What is NOT here is the price
	// this Ticket was actually sold at, which a Promotion may have made different
	// and which is the buyer's business either way.
	TicketTypeName string           `json:"ticket_type_name"`
	Event          EventView        `json:"event"`
	Organization   OrganizationView `json:"organization"`
}

// GetCustomerArea returns the Customer Area for the session behind the token.
//
// This is the read the whole feature turns on. Its scope comes from
// Authenticate — the Customer id on the session the caller proved they hold — and
// from nowhere else. The function takes no Customer, email, Organization, or sale
// identifier from its caller, so a handler physically cannot pass one in from the
// request: parameters supplied by a client are not merely ignored here, they have
// nowhere to go. Getting this wrong would expose one Customer's purchase history to
// another.
func (s *Service) GetCustomerArea(ctx context.Context, token string) (*CustomerAreaView, error) {
	actor, err := s.Authenticate(ctx, token)
	if err != nil {
		return nil, err
	}

	// Loading the Area is what makes the platform ask the Payment Provider again
	// about this Customer's own stuck reversal (ADR 0024). It runs BEFORE the read
	// below, so a request that resolves on this visit is rendered as the reversal
	// it became rather than as pending for one more page load.
	//
	// It is the opportunistic drain and not the whole recovery: the Reversal
	// Reconciler pursues a request nobody comes back to look at. This is the
	// accelerator for the buyer who did come back, so their answer arrives in
	// seconds rather than on the next tick.
	//
	// Only a full session drives it. A Confirmation Link session is a read
	// credential scoped to one sale — it cannot ask for a reversal in the first
	// place — and letting it act on every request the Customer has open would
	// widen a forwarded email beyond the one purchase it names.
	//
	// A failure is logged and swallowed. The provider being unwell is exactly the
	// situation that created these requests, and it must not also stop somebody
	// reading their own tickets.
	if actor.TicketSaleID == "" && s.reversals != nil {
		if err := s.reversals.ResolveInFlightReversalRequests(ctx, actor.CustomerID); err != nil {
			s.logger.Warn("could not resolve this Customer's in-flight Reversal Requests while loading their Area; any pending reversal stays pending",
				"customer_id", actor.CustomerID,
				"error", err,
			)
		}
	}

	// actor.TicketSaleID is empty for a full session and can only narrow the
	// result, never widen it. It is the Confirmation Link scope: a session minted
	// by a link sees that one Ticket Sale and no other, not even another
	// belonging to the same Customer.
	sales, err := s.repo.ListTicketSalesForCustomer(ctx, actor.CustomerID, actor.TicketSaleID)
	if err != nil {
		return nil, err
	}

	now := s.now()
	var upcoming, past []repository.TicketSaleRow
	for _, sale := range sales {
		if isUpcoming(sale, now) {
			upcoming = append(upcoming, sale)
		} else {
			past = append(past, sale)
		}
	}

	// Both halves are ordered by the Event, not by when the ticket was bought:
	// upcoming reads soonest-first, because the next Event is the one that
	// matters, and past reads most-recent-first. An Event with no date yet sits
	// at the end of the upcoming list, since there is nothing to place it by.
	sortByEventDate(upcoming, true)
	sortByEventDate(past, false)

	// The Tickets this person HOLDS rather than bought (#325). Read only for a
	// full session: a Confirmation Link session is a credential scoped to one
	// Ticket Sale, and a Ticket held on somebody else's sale is not that sale —
	// listing it would widen a forwarded email past the one purchase it names.
	var holding []repository.HeldTicketRow
	if actor.TicketSaleID == "" {
		holding, err = s.repo.ListHeldTicketsForCustomer(ctx, actor.CustomerID)
		if err != nil {
			return nil, err
		}
	}

	area := &CustomerAreaView{
		Upcoming: ticketSaleViews(upcoming, now, s.reversal),
		Past:     ticketSaleViews(past, now, s.reversal),
		Holding:  heldTicketViews(holding),
	}
	return area, nil
}

// sortByEventDate orders sales by their Event's moment — ascending when ascending
// is true, descending otherwise — with undated Events always last. Ties break on
// the newest purchase and then the sale id, so the order is total and a page
// never reshuffles between reads.
func sortByEventDate(sales []repository.TicketSaleRow, ascending bool) {
	sort.SliceStable(sales, func(i, j int) bool {
		a, aDated := eventMoment(sales[i])
		b, bDated := eventMoment(sales[j])
		switch {
		case aDated != bDated:
			return aDated
		case aDated && !a.Equal(b):
			if ascending {
				return a.Before(b)
			}
			return a.After(b)
		case !sales[i].SoldAt.Equal(sales[j].SoldAt):
			return sales[i].SoldAt.After(sales[j].SoldAt)
		default:
			return sales[i].ID < sales[j].ID
		}
	})
}

// eventMoment is the instant a sale's Event is placed at: when it starts, or
// failing that when it ends. An Event with neither has no moment at all.
func eventMoment(sale repository.TicketSaleRow) (time.Time, bool) {
	switch {
	case sale.EventStartsAt.Valid:
		return sale.EventStartsAt.Time, true
	case sale.EventEndsAt.Valid:
		return sale.EventEndsAt.Time, true
	default:
		return time.Time{}, false
	}
}

func ticketSaleViews(sales []repository.TicketSaleRow, now time.Time, reversal platform.PaymentReversal) []TicketSaleView {
	views := make([]TicketSaleView, 0, len(sales))
	for _, sale := range sales {
		views = append(views, ticketSaleView(sale, now, reversal))
	}
	return views
}

// ReversalOffer is what any surface may say about undoing one Ticket Sale: that
// it can be undone right now, until when, and which sale it is.
//
// The fields answer together. Every one but Reversible is nil whenever Reversible
// is false, deliberately: a deadline on a sale nobody may reverse is a deadline
// that means nothing, and a surface that cannot draw one cannot mislead somebody
// with it.
//
// This is also the guest checkout endpoint's whole response, which is why it says
// so little. It carries no email, no name, no confirmation reference and no
// amount — a boolean, an instant, and an opaque sale id that unlocks nothing on
// its own — so an unauthenticated caller holding a client transaction id learns
// no more about the buyer than a clock would tell them.
type ReversalOffer struct {
	Reversible bool `json:"reversible"`
	// ReversibleUntil is the instant this offer runs out, RFC3339 in UTC. Named
	// for Reversible rather than for the Reversal Window: it is the deadline on
	// the offer, and an offer can be absent while the Window is still open (a
	// Payment Provider that cannot reverse), in which case it is null.
	ReversibleUntil *string `json:"reversible_until"`
	// TicketSaleID names the sale this offer is about, so a guest surface can
	// send somebody to their own purchase in the Customer Area rather than to the
	// whole list (#121).
	//
	// It is an identifier and not a credential: reaching that sale still requires
	// a Customer Session, which this endpoint cannot mint and does not check. It
	// is null exactly when the offer is, because a destination for an undo that
	// is not on offer is a link to nothing.
	TicketSaleID *string `json:"ticket_sale_id"`
}

// reversalOffer turns the platform's reversal decision into what this module's
// surfaces publish, and is the single place in this module that mentions the
// decision at all.
//
// Every surface that mentions undo goes through here — the Customer Area's cards,
// the checkout success page a guest lands on seconds after paying, and the
// Confirmation Link page they come back to (#121) — so the deadline the three
// print is not merely computed the same way, it is computed by the same call on
// the same facts. Forking it is what would let a buyer read 8:00 PM on one page
// and something else on another for one sale.
//
// The decision itself is NOT here. It is platform.PaymentReversal.EligibilityAt,
// shared with the sales module's reversal endpoint, which enforces the same four
// checks in order to perform the undo this function merely offers. That sharing
// is the point: an offer computed by a different rule than the one the endpoint
// enforces is a button that appears on a sale the API then refuses.
//
// What stays here is the output. Every refusal collapses to the same silence —
// this module has no interest in which of the four it was, and a surface that
// cannot draw a deadline cannot mislead somebody with one — while the sales
// module maps each refusal to its own typed error and status.
//
// It is an answer about this instant and nothing more. It is not a promise: a
// true read a minute ago may be a refusal a minute from now, and the reversal
// endpoint re-asks the question for itself.
func reversalOffer(sale platform.SaleReversalFacts, now time.Time, reversal platform.PaymentReversal) ReversalOffer {
	window, refusal := reversal.EligibilityAt(sale, now)
	if refusal != platform.ReversalAllowed {
		return ReversalOffer{}
	}
	return ReversalOffer{Reversible: true, ReversibleUntil: timePtr(true, window.ClosesAt)}
}

// isUpcoming reports whether a Ticket Sale's Event is still ahead of the
// Customer. An Event is past once it has ended, or once it has started when no
// end is recorded. An Event with no schedule at all has certainly not happened
// yet, so it counts as upcoming rather than being filed away under history.
func isUpcoming(sale repository.TicketSaleRow, now time.Time) bool {
	switch {
	case sale.EventEndsAt.Valid:
		return !now.After(sale.EventEndsAt.Time)
	case sale.EventStartsAt.Valid:
		return !now.After(sale.EventStartsAt.Time)
	default:
		return true
	}
}

func ticketSaleView(sale repository.TicketSaleRow, now time.Time, reversal platform.PaymentReversal) TicketSaleView {
	// Whether this card may offer an undo, and until when, is not decided here:
	// it is the same call the guest surfaces make (#121), so the Customer Area
	// and the pages a guest sees before they ever sign in cannot disagree about
	// one sale.
	offer := reversalOffer(sale.ReversalFacts(), now, reversal)

	lines := make([]TicketSaleLineView, 0, len(sale.Lines))
	for _, l := range sale.Lines {
		lines = append(lines, TicketSaleLineView{
			TicketTypeName: l.TicketTypeName,
			Quantity:       l.Quantity,
			UnitPriceCents: l.UnitPriceCents,
		})
	}

	return TicketSaleView{
		ID:              sale.ID,
		ConfirmationRef: sale.ConfirmationRef,
		SoldAt:          sale.SoldAt.UTC().Format(time.RFC3339),
		Status:          sale.Status,
		AmountCents:     sale.AmountCents,
		Currency:        sale.Currency,
		Lines:           lines,
		Event: EventView{
			ID:        sale.EventID,
			Name:      sale.EventName,
			Slug:      sale.EventSlug,
			StartsAt:  timePtr(sale.EventStartsAt.Valid, sale.EventStartsAt.Time),
			EndsAt:    timePtr(sale.EventEndsAt.Valid, sale.EventEndsAt.Time),
			Timezone:  stringPtr(sale.EventTimezone.Valid, sale.EventTimezone.String),
			VenueName: stringPtr(sale.EventVenueName.Valid, sale.EventVenueName.String),
		},
		Organization: OrganizationView{
			ID:   sale.OrganizationID,
			Name: sale.OrganizationName,
			Slug: sale.OrganizationSlug,
		},
		TaxIDType:       stringPtr(sale.TaxIDType.Valid, sale.TaxIDType.String),
		TaxIDNumber:     stringPtr(sale.TaxIDNumber.Valid, sale.TaxIDNumber.String),
		Reversible:      offer.Reversible,
		ReversibleUntil: offer.ReversibleUntil,
		ReversalPending: sale.ReversalPending,
		ReversalStatus:  nullStringPtr(sale.ReversalStatus),
	}
}

// heldTicketViews renders the Tickets a Holder accepted.
//
// NEVER NIL, always at least an empty array, so a Storefront never has to tell
// "holds nothing" apart from "this build does not have the feature" — the
// distinction ADR 0045 says no surface may make.
//
// THERE IS NO UPCOMING/PAST SPLIT HERE, unlike the sales above, and the reason
// is the query: a Ticket on a reversed Sale is already gone from this list,
// because a Holder who no longer holds a ticket must not be shown an Event they
// cannot get into. What remains is ordered soonest first, which is the order
// somebody reading "what am I going to" wants.
func heldTicketViews(tickets []repository.HeldTicketRow) []HeldTicketView {
	out := make([]HeldTicketView, 0, len(tickets))
	for _, ticket := range tickets {
		out = append(out, HeldTicketView{
			TicketID:       ticket.TicketID,
			AcceptedAt:     ticket.AcceptedAt.UTC().Format(time.RFC3339),
			TicketTypeName: ticket.TicketTypeName,
			Event: EventView{
				ID:        ticket.EventID,
				Name:      ticket.EventName,
				Slug:      ticket.EventSlug,
				StartsAt:  timePtr(ticket.EventStartsAt.Valid, ticket.EventStartsAt.Time),
				EndsAt:    timePtr(ticket.EventEndsAt.Valid, ticket.EventEndsAt.Time),
				Timezone:  stringPtr(ticket.EventTimezone.Valid, ticket.EventTimezone.String),
				VenueName: stringPtr(ticket.EventVenueName.Valid, ticket.EventVenueName.String),
			},
			Organization: OrganizationView{
				ID:   ticket.OrganizationID,
				Name: ticket.OrganizationName,
				Slug: ticket.OrganizationSlug,
			},
		})
	}
	return out
}

func timePtr(valid bool, t time.Time) *string {
	if !valid {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func stringPtr(valid bool, s string) *string {
	if !valid {
		return nil
	}
	return &s
}

// nullStringPtr renders a nullable database string as a pointer, so an absent
// value reaches JSON as null rather than as the empty string. The two are not
// the same answer anywhere in this file: "" would say a Reversal Request exists
// and stands nowhere.
func nullStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}
