package repository

import (
	"slices"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/sales"
)

// TestOffersUpgradeCountsTheBasketAndEarlierSalesTogether pins the ambiguity
// rule (ADR 0074, #648): an Upgrade is offered where EXACTLY ONE free Ticket is
// in play, and the basket is in play.
//
// WHY THIS IS A UNIT TEST WHEN EVERY OTHER RULE HERE IS PROVEN OVER HTTP. The
// basket half has no HTTP surface yet — the Event page publishes the count of
// earlier qualifying Sales and nothing consumes it, because the Upgrade Prompt
// (#652) and the two commit mechanisms (#649, #650) are not built. The
// arithmetic is what those three will share, so it is worth pinning before any
// of them can restate it in their own words; the read half, and every clause of
// what makes a free Ticket surrenderable at all, is asserted through the API in
// backend/integration/upgrade_eligibility_test.go.
//
// IT IS NOT THE REPOSITORY-INTEGRATION EXCEPTION docs/testing.md narrows to
// concurrency and locking. Nothing here touches Postgres: OffersUpgrade is a
// pure function over two integers, tested the way sales/fees_test.go and
// sales/origin_test.go test theirs, and it runs under `make test` with them. It
// happens to live in this package because the rule does.
func TestOffersUpgradeCountsTheBasketAndEarlierSalesTogether(t *testing.T) {
	cases := []struct {
		name         string
		earlier      int
		freeInBasket int
		wantOffer    bool
	}{
		{"nothing free anywhere", 0, 0, false},
		{"one qualifying earlier Sale, nothing free in the basket", 1, 0, true},
		{"nothing earlier, one free Ticket in the basket", 0, 1, true},
		{"one earlier and one in the basket is ambiguous", 1, 1, false},
		{"two free Tickets in the basket is ambiguous", 0, 2, false},
		{"two qualifying earlier Sales is ambiguous", 2, 0, false},
		{"several of each is ambiguous", 3, 4, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := UpgradeEligibility{SurrenderableFreeTickets: tc.earlier}
			if got := e.OffersUpgrade(tc.freeInBasket); got != tc.wantOffer {
				t.Fatalf("OffersUpgrade(%d) with %d earlier = %t, want %t",
					tc.freeInBasket, tc.earlier, got, tc.wantOffer)
			}
		})
	}
}

// TestUpgradeEligibilityNamesNoTicketWhenTheBuyerHasNoSales is the zero value's
// contract, which the read path leans on: an unidentified reader and a buyer
// with nothing surrenderable are the same answer here, and neither names a
// Ticket anybody could act on.
func TestUpgradeEligibilityNamesNoTicketWhenTheBuyerHasNoSales(t *testing.T) {
	var e UpgradeEligibility
	if e.SurrenderableFreeTickets != 0 {
		t.Fatalf("zero value counts %d, want 0", e.SurrenderableFreeTickets)
	}
	if e.Ticket != (SurrenderableFreeTicket{}) {
		t.Fatalf("zero value names %+v, want no Ticket", e.Ticket)
	}
	if e.OffersUpgrade(0) {
		t.Fatal("zero value offers an Upgrade out of nothing")
	}
}

// catalogOf is a CatalogLookup over a fixed catalog, the way begin-checkout
// builds one from the Event's Ticket Types and the commit from its lock set.
func catalogOf(entries map[string]CatalogEntry) CatalogLookup {
	return func(ticketTypeID string) CatalogEntry { return entries[ticketTypeID] }
}

func soldAt(cents int) *int { return &cents }

// TestSelfHeldSeatIsTheDearestLine pins the seating rule of ADR 0074 (#646) as
// a predicate over a basket of priced lines, asked before any Ticket exists
// (#666): the buyer is seated on the first Ticket of the line sold dearest.
func TestSelfHeldSeatIsTheDearestLine(t *testing.T) {
	catalog := catalogOf(map[string]CatalogEntry{
		"general": {PriceCents: 1000, SortOrder: 0, Name: "General"},
		"vip":     {PriceCents: 5000, SortOrder: 1, Name: "VIP"},
		"free":    {PriceCents: 0, SortOrder: 2, Name: "Free"},
	})
	seat, ok := SelfHeldSeatOf([]CommitLine{
		{TicketTypeID: "general", Quantity: 2},
		{TicketTypeID: "vip", Quantity: 1},
		{TicketTypeID: "free", Quantity: 3},
	}, catalog)
	if !ok {
		t.Fatal("a basket with Tickets in it seated nobody")
	}
	if want := (SelfHeldSeat{Line: 1, TicketIndex: 1}); seat != want {
		t.Fatalf("seat = %+v, want %+v", seat, want)
	}
}

// TestSelfHeldSeatBreaksTiesAndReadsThePriceAsSold walks the rest of the rule:
// the catalog breaks a tie on price and nothing else, the price is what the
// line is sold at rather than what the catalog lists, and a basket of one line
// seats its buyer on that line's first Ticket.
func TestSelfHeldSeatBreaksTiesAndReadsThePriceAsSold(t *testing.T) {
	catalog := catalogOf(map[string]CatalogEntry{
		"early-bird": {PriceCents: 2000, SortOrder: 0, Name: "Early bird"},
		"regular":    {PriceCents: 2000, SortOrder: 1, Name: "Regular"},
		"alpha":      {PriceCents: 3000, SortOrder: 5, Name: "Alpha"},
		"beta":       {PriceCents: 3000, SortOrder: 5, Name: "Beta"},
		"vip":        {PriceCents: 9000, SortOrder: 9, Name: "VIP"},
	})
	cases := []struct {
		name  string
		lines []CommitLine
		want  SelfHeldSeat
	}{
		{
			name: "equal prices seat the buyer on the earlier Ticket Type in the catalog",
			lines: []CommitLine{
				{TicketTypeID: "regular", Quantity: 1},
				{TicketTypeID: "early-bird", Quantity: 1},
			},
			want: SelfHeldSeat{Line: 1, TicketIndex: 1},
		},
		{
			name: "equal prices and catalog places fall to the name",
			lines: []CommitLine{
				{TicketTypeID: "beta", Quantity: 1},
				{TicketTypeID: "alpha", Quantity: 2},
			},
			want: SelfHeldSeat{Line: 1, TicketIndex: 1},
		},
		{
			name: "a Promotional Price ranks the line at what it is sold for, not its List Price",
			lines: []CommitLine{
				{TicketTypeID: "vip", Quantity: 1, UnitPriceCents: soldAt(1500)},
				{TicketTypeID: "regular", Quantity: 1, UnitPriceCents: soldAt(2000)},
			},
			want: SelfHeldSeat{Line: 1, TicketIndex: 1},
		},
		{
			name: "a promoted line still wins when it is still the dearest",
			lines: []CommitLine{
				{TicketTypeID: "regular", Quantity: 3},
				{TicketTypeID: "vip", Quantity: 1, UnitPriceCents: soldAt(4500)},
			},
			want: SelfHeldSeat{Line: 1, TicketIndex: 1},
		},
		{
			name:  "a basket of one line seats its first Ticket",
			lines: []CommitLine{{TicketTypeID: "regular", Quantity: 4}},
			want:  SelfHeldSeat{Line: 0, TicketIndex: 1},
		},
		{
			name: "two lines of one Ticket Type at one price seat the buyer on the earlier",
			lines: []CommitLine{
				{TicketTypeID: "regular", Quantity: 1},
				{TicketTypeID: "regular", Quantity: 1},
			},
			want: SelfHeldSeat{Line: 0, TicketIndex: 1},
		},
		{
			name: "a line that mints no Ticket claims nothing, however dear",
			lines: []CommitLine{
				{TicketTypeID: "vip", Quantity: 0},
				{TicketTypeID: "regular", Quantity: 1},
			},
			want: SelfHeldSeat{Line: 1, TicketIndex: 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seat, ok := SelfHeldSeatOf(tc.lines, catalog)
			if !ok {
				t.Fatal("seated nobody")
			}
			if seat != tc.want {
				t.Fatalf("seat = %+v, want %+v", seat, tc.want)
			}
		})
	}
}

// TestSameBasketUpgradeDropsTheOneFreeLineBeforeSeating pins the basket the
// seating rule is asked of when a buyer elects an Upgrade (ADR 0074, #649):
// the one free line is out of it, so the seat is the paid line's - and a
// basket the platform would not offer an Upgrade on is left whole.
func TestSameBasketUpgradeDropsTheOneFreeLineBeforeSeating(t *testing.T) {
	catalog := catalogOf(map[string]CatalogEntry{
		"free": {PriceCents: 0, SortOrder: 0, Name: "Free"},
		"paid": {PriceCents: 2500, SortOrder: 1, Name: "Paid"},
	})
	none := func() (UpgradeEligibility, error) { return UpgradeEligibility{}, nil }
	oneEarlier := func() (UpgradeEligibility, error) {
		return UpgradeEligibility{SurrenderableFreeTickets: 1}, nil
	}
	cases := []struct {
		name        string
		lines       []CommitLine
		eligibility func() (UpgradeEligibility, error)
		wantKept    []string
		wantDropped string
	}{
		{
			name:        "one free Ticket beside a paid one is dropped",
			lines:       []CommitLine{{TicketTypeID: "free", Quantity: 1}, {TicketTypeID: "paid", Quantity: 2}},
			eligibility: none,
			wantKept:    []string{"paid"},
			wantDropped: "free",
		},
		{
			name:        "a line given away by a Promotional Price counts as free",
			lines:       []CommitLine{{TicketTypeID: "paid", Quantity: 1}, {TicketTypeID: "promoted", Quantity: 1, UnitPriceCents: soldAt(0)}},
			eligibility: none,
			wantKept:    []string{"paid"},
			wantDropped: "promoted",
		},
		{
			name:        "two free Tickets are ambiguous and nothing is dropped",
			lines:       []CommitLine{{TicketTypeID: "free", Quantity: 2}, {TicketTypeID: "paid", Quantity: 1}},
			eligibility: none,
			wantKept:    []string{"free", "paid"},
		},
		{
			name:        "a free Ticket already on file makes two in play",
			lines:       []CommitLine{{TicketTypeID: "free", Quantity: 1}, {TicketTypeID: "paid", Quantity: 1}},
			eligibility: oneEarlier,
			wantKept:    []string{"free", "paid"},
		},
		{
			name:        "nothing paid to move onto is no Upgrade",
			lines:       []CommitLine{{TicketTypeID: "free", Quantity: 1}},
			eligibility: none,
			wantKept:    []string{"free"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kept, dropped, err := SameBasketUpgrade(tc.lines, catalog, tc.eligibility)
			if err != nil {
				t.Fatal(err)
			}
			var gotKept []string
			for _, l := range kept {
				gotKept = append(gotKept, l.TicketTypeID)
			}
			if !slices.Equal(gotKept, tc.wantKept) {
				t.Fatalf("kept %v, want %v", gotKept, tc.wantKept)
			}
			gotDropped := ""
			if dropped != nil {
				gotDropped = dropped.TicketTypeID
			}
			if gotDropped != tc.wantDropped {
				t.Fatalf("dropped %q, want %q", gotDropped, tc.wantDropped)
			}
		})
	}
}

// TestSameBasketUpgradeReadsNoEligibilityForABasketThatCannotQualify: the
// buyer's earlier Sales are only worth reading once the basket itself could
// carry an Upgrade, so an ordinary basket costs the commit no query.
func TestSameBasketUpgradeReadsNoEligibilityForABasketThatCannotQualify(t *testing.T) {
	catalog := catalogOf(map[string]CatalogEntry{"paid": {PriceCents: 2500}})
	read := func() (UpgradeEligibility, error) {
		t.Fatal("eligibility read for a basket with no free Ticket")
		return UpgradeEligibility{}, nil
	}
	if _, _, err := SameBasketUpgrade([]CommitLine{{TicketTypeID: "paid", Quantity: 1}}, catalog, read); err != nil {
		t.Fatal(err)
	}
}

// TestAPaymentLineIsSeatedAtThePriceItWillCommitAt is begin-checkout's half of
// the seam (#666): a Payment Line asked as the CommitLine it becomes ranks on
// the buyer's unit price frozen into it, Promotional Price and passed-on fee
// included, and not on the Ticket Type's List Price.
func TestAPaymentLineIsSeatedAtThePriceItWillCommitAt(t *testing.T) {
	catalog := catalogOf(map[string]CatalogEntry{
		"vip":     {PriceCents: 9000, SortOrder: 0, Name: "VIP"},
		"regular": {PriceCents: 2000, SortOrder: 1, Name: "Regular"},
	})
	payment := []PaymentLine{
		// VIP on a Promotional Price of 15.00, the fee absorbed.
		{TicketTypeID: "vip", Quantity: 1, Fee: sales.FeeSnapshot{BasePriceCents: 1500, BuyerUnitPriceCents: 1500}},
		// Regular at its List Price of 20.00, with 1.20 of fee passed on.
		{TicketTypeID: "regular", Quantity: 2, Fee: sales.FeeSnapshot{BasePriceCents: 2000, FeeCents: 120, BuyerUnitPriceCents: 2120}},
	}
	lines := make([]CommitLine, 0, len(payment))
	for _, p := range payment {
		lines = append(lines, p.CommitLine())
	}
	seat, ok := SelfHeldSeatOf(lines, catalog)
	if !ok {
		t.Fatal("seated nobody")
	}
	if want := (SelfHeldSeat{Line: 1, TicketIndex: 1}); seat != want {
		t.Fatalf("seat = %+v, want %+v (the Regular line, sold dearer than the promoted VIP)", seat, want)
	}
	if got := *lines[1].UnitPriceCents; got != 2120 {
		t.Fatalf("Regular commits at %d, want the buyer's unit price 2120", got)
	}
}

// TestSelfHeldSeatOfAnEmptyBasketIsNobody: a basket that mints no Ticket has
// no seat, and says so rather than naming line zero.
func TestSelfHeldSeatOfAnEmptyBasketIsNobody(t *testing.T) {
	catalog := catalogOf(nil)
	for _, lines := range [][]CommitLine{nil, {{TicketTypeID: "regular", Quantity: 0}}} {
		if seat, ok := SelfHeldSeatOf(lines, catalog); ok {
			t.Fatalf("SelfHeldSeatOf(%+v) = %+v, want no seat", lines, seat)
		}
	}
}

// TestSelfHeldSeatIsTheSameWhicheverOrderTheBasketArrivesIn pins the last
// tie-break (#666): two Ticket Types sold at one price that share a catalog
// place and a name fall to their ids, by byte order. Begin-checkout holds its
// lines in the catalog's order and the commit reads them back unordered, so a
// rule that let the earlier line win a full tie could seat the buyer on a
// different Ticket in each half.
func TestSelfHeldSeatIsTheSameWhicheverOrderTheBasketArrivesIn(t *testing.T) {
	catalog := catalogOf(map[string]CatalogEntry{
		"b-twin":  {PriceCents: 2500, SortOrder: 3, Name: "General"},
		"a-twin":  {PriceCents: 2500, SortOrder: 3, Name: "General"},
		"c-other": {PriceCents: 2500, SortOrder: 3, Name: "Generalísimo"},
		"cheap":   {PriceCents: 1000, SortOrder: 0, Name: "Cheap"},
	})
	cases := []struct {
		name string
		// lines holds one basket; every permutation of it must seat the buyer
		// on wantType.
		lines    []CommitLine
		wantType string
	}{
		{
			name: "same price, catalog place and name fall to the lower id",
			lines: []CommitLine{
				{TicketTypeID: "b-twin", Quantity: 2},
				{TicketTypeID: "a-twin", Quantity: 1},
			},
			wantType: "a-twin",
		},
		{
			name: "the id is asked only after the name",
			lines: []CommitLine{
				{TicketTypeID: "c-other", Quantity: 1},
				{TicketTypeID: "b-twin", Quantity: 1},
			},
			wantType: "b-twin",
		},
		{
			name: "a full tie among several lines beside a cheaper one",
			lines: []CommitLine{
				{TicketTypeID: "cheap", Quantity: 4},
				{TicketTypeID: "c-other", Quantity: 1},
				{TicketTypeID: "b-twin", Quantity: 1},
				{TicketTypeID: "a-twin", Quantity: 3},
			},
			wantType: "a-twin",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, order := range permutations(len(tc.lines)) {
				basket := make([]CommitLine, len(order))
				for i, j := range order {
					basket[i] = tc.lines[j]
				}
				seat, ok := SelfHeldSeatOf(basket, catalog)
				if !ok {
					t.Fatalf("%v seated nobody", basket)
				}
				if got := basket[seat.Line].TicketTypeID; got != tc.wantType || seat.TicketIndex != 1 {
					t.Fatalf("basket %v seats %s ticket %d, want %s ticket 1",
						basket, got, seat.TicketIndex, tc.wantType)
				}
			}
		})
	}
}

// permutations lists every ordering of 0..n-1.
func permutations(n int) [][]int {
	if n == 0 {
		return [][]int{{}}
	}
	var out [][]int
	for _, rest := range permutations(n - 1) {
		for at := 0; at <= len(rest); at++ {
			p := slices.Insert(slices.Clone(rest), at, n-1)
			out = append(out, p)
		}
	}
	return out
}
