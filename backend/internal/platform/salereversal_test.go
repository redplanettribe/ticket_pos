package platform_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// EligibilityAt is the one decision two modules share: the customers module asks
// it to decide whether to OFFER an undo, the sales module to decide whether to
// PERFORM one. These tests are what stops the two from being told different
// things, so they are written about the answer rather than about either caller.

// reversibleSale is an ordinary paid Online Sale, bought in the Ecuadorian
// morning for a show weeks away — every check passing, so each case below can
// break exactly one of them.
func reversibleSale() platform.SaleReversalFacts {
	return platform.SaleReversalFacts{
		Channel:       platform.OnlineSalesChannel,
		Status:        platform.ActiveSaleStatus,
		SoldAt:        time.Date(2026, 7, 7, 14, 0, 0, 0, time.UTC), // 09:00 in Ecuador
		PaymentMethod: sql.NullString{String: platform.PayPhoneProviderName, Valid: true},
		EventStartsAt: sql.NullTime{Time: time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC), Valid: true},
	}
}

// midMorning is inside the sale above's window: after the purchase, well before
// that day's 20:00 Ecuadorian cutoff.
var midMorning = time.Date(2026, 7, 7, 15, 0, 0, 0, time.UTC)

func testReversal() platform.PaymentReversal {
	return platform.NewPaymentReversal(platform.NewStubPaymentProvider("http://storefront.example"))
}

func TestEligibilityAllowsAnOrdinaryOnlineSaleInsideItsWindow(t *testing.T) {
	t.Parallel()

	window, refusal := testReversal().EligibilityAt(reversibleSale(), midMorning)
	if refusal != platform.ReversalAllowed {
		t.Fatalf("refusal = %v, want an ordinary paid Online Sale inside its window to be allowed", refusal)
	}
	// The window comes back with the answer, because the caller that offers the
	// undo has to publish its deadline and must not recompute it.
	want := time.Date(2026, 7, 8, 1, 0, 0, 0, time.UTC) // 20:00 on 7 July in Ecuador
	if !window.ClosesAt.Equal(want) {
		t.Fatalf("ClosesAt = %s, want %s", window.ClosesAt.UTC().Format(time.RFC3339), want.Format(time.RFC3339))
	}

	// A free claim reaches the same answer with no Payment Provider anywhere in
	// it: the window is a platform rule, not a provider one (ADR 0018).
	free := reversibleSale()
	free.PaymentMethod = sql.NullString{String: platform.FreePaymentMethod, Valid: true}
	if _, refusal := (platform.PaymentReversal{}).EligibilityAt(free, midMorning); refusal != platform.ReversalAllowed {
		t.Fatalf("refusal = %v on a free claim with no provider configured, want it allowed", refusal)
	}
}

func TestEligibilityRefusals(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// break mutates the ordinary reversible sale into the one thing this case
		// is about.
		breaks func(*platform.SaleReversalFacts)
		now    time.Time
		want   platform.ReversalRefusal
	}{
		{
			// Money the platform never touched: there is nothing here to give back
			// and nobody here to ask.
			name:   "an In-Person Sale",
			breaks: func(s *platform.SaleReversalFacts) { s.Channel = "in_person" },
			now:    midMorning,
			want:   platform.ReversalNotAnOnlineSale,
		},
		{
			name:   "an imported sale",
			breaks: func(s *platform.SaleReversalFacts) { s.Channel = "import" },
			now:    midMorning,
			want:   platform.ReversalNotAnOnlineSale,
		},
		{
			name:   "a sale already undone",
			breaks: func(s *platform.SaleReversalFacts) { s.Status = "reversed" },
			now:    midMorning,
			want:   platform.ReversalAlreadyReversed,
		},
		{
			// A provider this deployment cannot ask: the money was collected by
			// somebody who is not on the other end of our boundary.
			name: "a Payment settled by another provider",
			breaks: func(s *platform.SaleReversalFacts) {
				s.PaymentMethod = sql.NullString{String: "some-other-provider", Valid: true}
			},
			now:  midMorning,
			want: platform.ReversalPaymentNotReversible,
		},
		{
			// 2026-07-08T01:00:00Z is exactly 20:00 in Ecuador. The window runs
			// UNTIL the cutoff, so arriving on it is arriving late.
			name:   "the cutoff has arrived",
			breaks: func(*platform.SaleReversalFacts) {},
			now:    time.Date(2026, 7, 8, 1, 0, 0, 0, time.UTC),
			want:   platform.ReversalWindowClosed,
		},
		{
			name: "the doors have opened first",
			breaks: func(s *platform.SaleReversalFacts) {
				s.EventStartsAt = sql.NullTime{Time: time.Date(2026, 7, 7, 14, 30, 0, 0, time.UTC), Valid: true}
			},
			now:  midMorning,
			want: platform.ReversalWindowClosed,
		},
		{
			// A sale whose Event has no recorded start has no window rather than an
			// unbounded one. Inventing a deadline would be inventing permission.
			name:   "an Event with no start recorded",
			breaks: func(s *platform.SaleReversalFacts) { s.EventStartsAt = sql.NullTime{} },
			now:    midMorning,
			want:   platform.ReversalWindowClosed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sale := reversibleSale()
			tc.breaks(&sale)

			window, refusal := testReversal().EligibilityAt(sale, tc.now)
			if refusal != tc.want {
				t.Fatalf("refusal = %v, want %v", refusal, tc.want)
			}
			// A refusal never carries a deadline. A closing time on a sale nobody
			// may reverse is a countdown to an action the API would refuse.
			if !window.ClosesAt.IsZero() || !window.OpensAt.IsZero() {
				t.Fatalf("a refusal returned a window %+v; a refused sale has no deadline to publish", window)
			}
		})
	}
}

// TestEligibilityReportsTheMostPermanentReasonFirst pins the order of the checks,
// which is part of the answer rather than an implementation detail. A sale that
// fails several at once is explained by the reason that will never change, not by
// a clock that was never running for it.
func TestEligibilityReportsTheMostPermanentReasonFirst(t *testing.T) {
	t.Parallel()

	// An In-Person Sale, already reversed, long past any cutoff. It was never
	// this buyer's to undo here, and that is what they are told.
	sale := reversibleSale()
	sale.Channel = "in_person"
	sale.Status = "reversed"

	if _, refusal := testReversal().EligibilityAt(sale, midMorning); refusal != platform.ReversalNotAnOnlineSale {
		t.Fatalf("refusal = %v on an already-reversed In-Person Sale, want ReversalNotAnOnlineSale — "+
			"a sale off the platform is not explained by a window it never had", refusal)
	}
}
