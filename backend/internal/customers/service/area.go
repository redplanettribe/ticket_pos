package service

import (
	"context"
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
	// Reversible reports whether this Ticket Sale is inside its Reversal Window
	// right now (ADR 0018) — whether the Customer could undo it themselves.
	//
	// It is an answer about this instant and nothing more. It is not a promise:
	// the window is offered, not guaranteed, and a true here read a minute ago
	// may be a refusal a minute from now. Nothing acts on it yet; this release
	// only reports it.
	//
	// It is false for everything that is not an Online Sale — an In-Person Sale
	// or an imported one was never collected by the platform, so the platform has
	// nothing to give back — and false for a Ticket Sale already reversed.
	Reversible bool `json:"reversible"`
	// ReversalWindowClosesAt is the instant the Reversal Window shuts, RFC3339 in
	// UTC, so the Customer can be told how long they have.
	//
	// Null whenever Reversible is false, and deliberately so: a closing time on a
	// sale nobody may reverse is a deadline that means nothing, and a client that
	// cannot draw one cannot mislead somebody with it.
	ReversalWindowClosesAt *string `json:"reversal_window_closes_at"`
}

// CustomerAreaView is the signed-in Customer's purchases, split into what is
// still to come and what has already happened, across every Organization they
// have ever bought from.
type CustomerAreaView struct {
	Upcoming []TicketSaleView `json:"upcoming"`
	Past     []TicketSaleView `json:"past"`
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

	area := &CustomerAreaView{
		Upcoming: ticketSaleViews(upcoming, now),
		Past:     ticketSaleViews(past, now),
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

func ticketSaleViews(sales []repository.TicketSaleRow, now time.Time) []TicketSaleView {
	views := make([]TicketSaleView, 0, len(sales))
	for _, sale := range sales {
		views = append(views, ticketSaleView(sale, now))
	}
	return views
}

// onlineChannel is the Sales Channel a Reversal Window can exist on. The other
// two — in_person and import — record money the platform never touched.
const onlineChannel = "online"

// activeStatus is the Ticket Sale that still stands. A reversed one has already
// been undone and cannot be undone again.
const activeStatus = "active"

// reversalWindow is the Reversal Window of one Ticket Sale, and nil when the sale
// can have none at all.
//
// Three things disqualify a sale outright, before any clock is consulted: a
// Sales Channel other than online, a sale already reversed, and an Event with no
// recorded start. The last cannot happen for an Online Sale — publishing requires
// a start and a valid timezone, and only a published Event is sellable — but the
// row type admits a null, and inventing a start for one would invent a deadline.
//
// Notice what is absent: the amount and the Payment Method. A free Online Sale
// settled by the platform, with no Payment Provider anywhere in it, reaches
// platform.NewReversalWindow on exactly the terms a PayPhone sale does. The
// window is a platform rule, not a provider one (ADR 0018).
func reversalWindow(sale repository.TicketSaleRow) *platform.ReversalWindow {
	if sale.Channel != onlineChannel || sale.Status != activeStatus || !sale.EventStartsAt.Valid {
		return nil
	}
	// SoldAt is the Payment approval instant on an Online Sale: the sale is
	// committed in the same transaction that approves the Payment.
	window := platform.NewReversalWindow(sale.SoldAt, sale.EventStartsAt.Time)
	return &window
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

func ticketSaleView(sale repository.TicketSaleRow, now time.Time) TicketSaleView {
	// The Reversal Window is reported only while it is actually open. A closed or
	// never-opened window says nothing at all rather than publishing a deadline
	// that has already gone.
	var reversible bool
	var closesAt *string
	if window := reversalWindow(sale); window != nil && window.IsOpenAt(now) {
		reversible = true
		closesAt = timePtr(true, window.ClosesAt)
	}

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
		TaxIDType:              stringPtr(sale.TaxIDType.Valid, sale.TaxIDType.String),
		TaxIDNumber:            stringPtr(sale.TaxIDNumber.Valid, sale.TaxIDNumber.String),
		Reversible:             reversible,
		ReversalWindowClosesAt: closesAt,
	}
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
