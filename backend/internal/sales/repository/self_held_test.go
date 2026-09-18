package repository

import "testing"

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
