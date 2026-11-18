package integration

import (
	"net/http"
	"testing"
	"time"
)

// Platform Trends money (#812, spec #808): money sold and Platform Fee earned,
// one series per currency and never converted or added together. The fixed
// clock makes today 2026-07-07, so a 7-day span is 2026-06-30..2026-07-06 and
// the span before it 2026-06-23..2026-06-29.
//
// At the fee test's price an Online Sale's ticket costs the buyer
// feeTestAllInCents, of which feeTestFeeCents is Platform Fee and
// feeTestIVACents Fee IVA. An imported ticket is sold at the price its Ticket
// Type states and withholds nothing.

// importedPriceCents is the price of the Ticket Type the money tests import
// sales into.
const importedPriceCents = 1500

// moveSaleSoldAtByRef restages when one Online Sale was made, as
// moveSaleSoldAt does for an Event with a single one: a checkout is sold at
// the fixed clock, which is today, and no route backdates one.
func moveSaleSoldAtByRef(t *testing.T, env *testEnv, confirmationRef string, soldAt time.Time) {
	t.Helper()
	res, err := env.db.Exec(`UPDATE ticket_sales SET sold_at = $1 WHERE confirmation_ref = $2`, soldAt, confirmationRef)
	if err != nil {
		t.Fatalf("move sale %s: %v", confirmationRef, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("moving sale %s affected %d rows, want 1", confirmationRef, n)
	}
}

// buyOnlineOn makes an Online Sale of quantity tickets and restages it as sold
// at soldAt, returning its Sale Confirmation reference.
func buyOnlineOn(t *testing.T, env *testEnv, orgSlug, eventSlug, ticketTypeID, email string, quantity int, soldAt time.Time) string {
	t.Helper()
	begun := beginCheckoutOK(t, env, orgSlug, eventSlug, checkoutBody(email, "Ana", "Lopez", cartLine(ticketTypeID, quantity)))
	settled := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	moveSaleSoldAtByRef(t, env, settled.ConfirmationRef, soldAt)
	return settled.ConfirmationRef
}

// euroOrganization creates a second Organization selling in EUR, with one
// published Event at the fee test's price, and returns its session and
// Ticket Type.
func euroOrganization(t *testing.T, env *testEnv) (sessionID, ticketTypeID string) {
	t.Helper()
	sessionID = verifyOTP(t, env, "euro@example.com")
	createOrganization(t, env, sessionID, "Euro Org", "euro-org")
	resp, body := env.patch(t, "/api/v1/staff/organization", map[string]string{
		"name": "Euro Org", "currency": "EUR",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set currency status=%d error=%+v", resp.StatusCode, body.Error)
	}
	_, ticketTypeID = publishCheckoutEvent(t, env, sessionID, "Euro Fest", "euro-fest", feeTestBaseCents, 100)
	return sessionID, ticketTypeID
}

// moneySeries returns one currency's series of a money figure.
func moneySeries(t *testing.T, figure platformTrendsFigure, currency string) platformTrendsSeries {
	t.Helper()
	for _, series := range figure.Series {
		if series.Key == currency {
			return series
		}
	}
	t.Fatalf("figure has no %q series: %+v", currency, figure.Series)
	return platformTrendsSeries{}
}

// assertPerCurrencyOnly: a money figure states one series per currency and
// nothing across them.
func assertPerCurrencyOnly(t *testing.T, name string, figure platformTrendsFigure, currencies ...string) {
	t.Helper()
	if !figure.Recorded || figure.ExactSince != nil {
		t.Fatalf("%s recorded=%v exact_since=%v; want recorded and always exact", name, figure.Recorded, figure.ExactSince)
	}
	if figure.Total != nil || figure.ComparisonTotal != nil || figure.Change != nil {
		t.Fatalf("%s total=%v comparison_total=%v change=%v; want all null: currencies are never added together",
			name, intOrNil(figure.Total), intOrNil(figure.ComparisonTotal), figure.Change)
	}
	if len(figure.Series) != len(currencies) {
		t.Fatalf("%s series = %+v; want one per currency %v", name, figure.Series, currencies)
	}
	for i, currency := range currencies {
		if figure.Series[i].Key != currency {
			t.Fatalf("%s series[%d] = %q, want %q", name, i, figure.Series[i].Key, currency)
		}
	}
}

// TestPlatformTrendsStatesMoneyPerCurrency: sales on every Sales Channel in
// two currencies. Each figure has a series per currency, with its own total,
// comparison and change, and no figure across them. Import and in-person
// sales are money sold and earn the platform nothing, and the fee is stated
// without its Fee IVA.
func TestPlatformTrendsStatesMoneyPerCurrency(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "Money Fest", "money-fest", feeTestBaseCents, 100)
	buyOnlineOn(t, env, testOrgSlug, "money-fest", gaID, "ana@example.com", 2, time.Date(2026, 7, 6, 18, 0, 0, 0, time.UTC))
	// One online ticket in the span before, to compare against.
	buyOnlineOn(t, env, testOrgSlug, "money-fest", gaID, "bea@example.com", 1, time.Date(2026, 6, 25, 18, 0, 0, 0, time.UTC))

	importEventID := createDraftEvent(t, env, adminSessionID, "Import Fest", "import-fest")
	importGAID := createTicketTypeWithCapacity(t, env, adminSessionID, importEventID, "GA", importedPriceCents, 100)
	commitBatch(t, env, adminSessionID, importEventID, "imported", []map[string]any{
		importSale("cris@example.com", importGAID, 3, "2026-07-06T15:00:00Z"),
	})
	doorBatch := commitBatch(t, env, adminSessionID, importEventID, "door", []map[string]any{
		importSale("dani@example.com", importGAID, 5, "2026-07-05T15:00:00Z"),
	})
	moveImportToTheDoor(t, env, doorBatch)

	_, euroGAID := euroOrganization(t, env)
	buyOnlineOn(t, env, "euro-org", "euro-fest", euroGAID, "eva@example.com", 1, time.Date(2026, 7, 4, 18, 0, 0, 0, time.UTC))

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	trends := readPlatformTrends(t, env, operatorSessionID, "?span=7d")
	sold, fee := trends.Figures.MoneySold, trends.Figures.PlatformFeeEarned
	assertPerCurrencyOnly(t, "money_sold", sold, "EUR", "USD")
	assertPerCurrencyOnly(t, "platform_fee_earned", fee, "EUR", "USD")
	if sold.DataSince == nil || *sold.DataSince != "2026-06-25" || fee.DataSince == nil || *fee.DataSince != "2026-06-25" {
		t.Fatalf("data_since = %v / %v, want 2026-06-25, the first day anything was sold", sold.DataSince, fee.DataSince)
	}

	usdSold := moneySeries(t, sold, "USD")
	for day, want := range map[string]int{
		"2026-07-06": 2*feeTestAllInCents + 3*importedPriceCents,
		"2026-07-05": 5 * importedPriceCents,
		"2026-07-04": 0,
	} {
		if got := bucketOn(t, usdSold, day).Value; got != want {
			t.Fatalf("USD money sold on %s = %d, want %d", day, got, want)
		}
	}
	if want := 2*feeTestAllInCents + 8*importedPriceCents; usdSold.Total != want {
		t.Fatalf("USD money sold total = %d, want %d", usdSold.Total, want)
	}
	if usdSold.ComparisonTotal == nil || *usdSold.ComparisonTotal != feeTestAllInCents {
		t.Fatalf("USD money sold comparison_total = %v, want %d", intOrNil(usdSold.ComparisonTotal), feeTestAllInCents)
	}
	wantChange(t, "USD money sold", usdSold.Change,
		float64(usdSold.Total-feeTestAllInCents)/float64(feeTestAllInCents))
	if len(usdSold.ComparisonBuckets) != len(usdSold.Buckets) {
		t.Fatalf("USD money sold has %d comparison buckets beside %d buckets", len(usdSold.ComparisonBuckets), len(usdSold.Buckets))
	}

	// The fee is the Online Sales' alone, and carries no Fee IVA.
	usdFee := moneySeries(t, fee, "USD")
	if got := bucketOn(t, usdFee, "2026-07-06").Value; got != 2*feeTestFeeCents {
		t.Fatalf("USD Platform Fee earned on 2026-07-06 = %d, want %d: two online tickets, Fee IVA excluded", got, 2*feeTestFeeCents)
	}
	if got := bucketOn(t, usdFee, "2026-07-05").Value; got != 0 {
		t.Fatalf("USD Platform Fee earned on 2026-07-05 = %d, want 0: an in-person sale earns the platform nothing", got)
	}
	if usdFee.Total != 2*feeTestFeeCents {
		t.Fatalf("USD Platform Fee earned total = %d, want %d: import and in-person add nothing", usdFee.Total, 2*feeTestFeeCents)
	}
	if usdFee.ComparisonTotal == nil || *usdFee.ComparisonTotal != feeTestFeeCents {
		t.Fatalf("USD Platform Fee earned comparison_total = %v, want %d", intOrNil(usdFee.ComparisonTotal), feeTestFeeCents)
	}
	wantChange(t, "USD Platform Fee earned", usdFee.Change, 1)

	eurSold, eurFee := moneySeries(t, sold, "EUR"), moneySeries(t, fee, "EUR")
	if got := bucketOn(t, eurSold, "2026-07-04").Value; got != feeTestAllInCents || eurSold.Total != feeTestAllInCents {
		t.Fatalf("EUR money sold on 2026-07-04 = %d, total %d; want %d each", got, eurSold.Total, feeTestAllInCents)
	}
	if got := bucketOn(t, eurFee, "2026-07-04").Value; got != feeTestFeeCents || eurFee.Total != feeTestFeeCents {
		t.Fatalf("EUR Platform Fee earned on 2026-07-04 = %d, total %d; want %d each", got, eurFee.Total, feeTestFeeCents)
	}
	// EUR sold nothing in the span before, which is measured: a zero to
	// compare against, and so no ratio.
	if eurSold.ComparisonTotal == nil || *eurSold.ComparisonTotal != 0 || eurSold.Change != nil {
		t.Fatalf("EUR money sold comparison_total=%v change=%v; want 0 and no change", intOrNil(eurSold.ComparisonTotal), eurSold.Change)
	}
}

// TestPlatformTrendsMoneyFollowsAReversal: a reversed Ticket Sale drops out of
// money sold on its sold day, whatever its channel. Its Platform Fee stays
// earned only when the Operator Reversal said the platform kept it.
func TestPlatformTrendsMoneyFollowsAReversal(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, adminSessionID, "Reversal Fest", "reversal-fest", feeTestBaseCents, 100)
	soldAt := time.Date(2026, 7, 3, 18, 0, 0, 0, time.UTC)
	buyOnlineOn(t, env, testOrgSlug, "reversal-fest", gaID, "ana@example.com", 1, soldAt)
	keptRef := buyOnlineOn(t, env, testOrgSlug, "reversal-fest", gaID, "bea@example.com", 1, soldAt)
	returnedRef := buyOnlineOn(t, env, testOrgSlug, "reversal-fest", gaID, "cris@example.com", 1, soldAt)
	undone := commitBatch(t, env, adminSessionID, eventID, "undone", []map[string]any{
		importSale("dani@example.com", gaID, 4, "2026-07-03T15:00:00Z"),
	})

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	before := readPlatformTrends(t, env, operatorSessionID, "?span=7d")
	if got := bucketOn(t, moneySeries(t, before.Figures.MoneySold, "USD"), "2026-07-03").Value; got != 3*feeTestAllInCents+4*feeTestBaseCents {
		t.Fatalf("money sold before = %d, want %d", got, 3*feeTestAllInCents+4*feeTestBaseCents)
	}
	if got := bucketOn(t, moneySeries(t, before.Figures.PlatformFeeEarned, "USD"), "2026-07-03").Value; got != 3*feeTestFeeCents {
		t.Fatalf("Platform Fee earned before = %d, want %d", got, 3*feeTestFeeCents)
	}

	operatorReverseOK(t, env, operatorSessionID, keptRef, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(1)), PlatformFeeKept: boolPtr(true), Note: strPtr("fee kept"),
	})
	operatorReverseOK(t, env, operatorSessionID, returnedRef, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(1)), PlatformFeeKept: boolPtr(false), Note: strPtr("fee returned"),
	})
	undoBatch(t, env, adminSessionID, eventID, undone)

	after := readPlatformTrends(t, env, operatorSessionID, "?span=7d")
	sold := moneySeries(t, after.Figures.MoneySold, "USD")
	if got := bucketOn(t, sold, "2026-07-03").Value; got != feeTestAllInCents || sold.Total != feeTestAllInCents {
		t.Fatalf("money sold after = %d, total %d; want %d: the three reversed sales left their sold day", got, sold.Total, feeTestAllInCents)
	}
	fee := moneySeries(t, after.Figures.PlatformFeeEarned, "USD")
	if got := bucketOn(t, fee, "2026-07-03").Value; got != 2*feeTestFeeCents || fee.Total != 2*feeTestFeeCents {
		t.Fatalf("Platform Fee earned after = %d, total %d; want %d: the active sale's fee and the kept one, not the returned one",
			got, fee.Total, 2*feeTestFeeCents)
	}
}

// TestPlatformTrendsPlatformFeeEarnedOverAllTimeIsTheOperatorSummarys: with
// sales in two currencies, a kept fee and a returned one among them, a span
// reaching back before the first sale states the Platform Fee the operator
// summary states, currency by currency.
func TestPlatformTrendsPlatformFeeEarnedOverAllTimeIsTheOperatorSummarys(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, adminSessionID, "Ledger Fest", "ledger-fest", feeTestBaseCents, 100)
	buyOnlineOn(t, env, testOrgSlug, "ledger-fest", gaID, "ana@example.com", 3, time.Date(2025, 2, 10, 18, 0, 0, 0, time.UTC))
	buyOnlineOn(t, env, testOrgSlug, "ledger-fest", gaID, "bea@example.com", 1, time.Date(2026, 7, 6, 18, 0, 0, 0, time.UTC))
	keptRef := buyOnlineOn(t, env, testOrgSlug, "ledger-fest", gaID, "cris@example.com", 2, time.Date(2025, 11, 20, 18, 0, 0, 0, time.UTC))
	returnedRef := buyOnlineOn(t, env, testOrgSlug, "ledger-fest", gaID, "dani@example.com", 1, time.Date(2026, 1, 5, 18, 0, 0, 0, time.UTC))
	commitBatch(t, env, adminSessionID, eventID, "imported", []map[string]any{
		importSale("eva@example.com", gaID, 6, "2025-06-01T15:00:00Z"),
	})

	_, euroGAID := euroOrganization(t, env)
	buyOnlineOn(t, env, "euro-org", "euro-fest", euroGAID, "fede@example.com", 2, time.Date(2025, 9, 9, 18, 0, 0, 0, time.UTC))
	euroKeptRef := buyOnlineOn(t, env, "euro-org", "euro-fest", euroGAID, "gabi@example.com", 1, time.Date(2026, 3, 3, 18, 0, 0, 0, time.UTC))

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	operatorReverseOK(t, env, operatorSessionID, keptRef, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(2)), PlatformFeeKept: boolPtr(true), Note: strPtr("fee kept"),
	})
	operatorReverseOK(t, env, operatorSessionID, returnedRef, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(1)), PlatformFeeKept: boolPtr(false), Note: strPtr("fee returned"),
	})
	operatorReverseOK(t, env, operatorSessionID, euroKeptRef, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(1)), PlatformFeeKept: boolPtr(true), Note: strPtr("fee kept"),
	})

	var summary operatorSummary
	operatorGetOK(t, env, operatorSessionID, "/api/v1/operator/summary", &summary)
	if len(summary.Totals) != 2 {
		t.Fatalf("operator totals = %+v, want EUR and USD", summary.Totals)
	}
	want := map[string]int{"USD": (3 + 1 + 2) * feeTestFeeCents, "EUR": (2 + 1) * feeTestFeeCents}

	trends := readPlatformTrends(t, env, operatorSessionID, "?span=custom&from=2024-01-01&to=2026-07-06")
	fee := trends.Figures.PlatformFeeEarned
	assertPerCurrencyOnly(t, "platform_fee_earned", fee, "EUR", "USD")
	for _, totals := range summary.Totals {
		if totals.PlatformFeeCents != want[totals.Currency] || totals.KeptFeeCents == 0 {
			t.Fatalf("operator summary %s = fee %d (kept %d), want %d with a kept fee in it",
				totals.Currency, totals.PlatformFeeCents, totals.KeptFeeCents, want[totals.Currency])
		}
		if got := moneySeries(t, fee, totals.Currency).Total; got != totals.PlatformFeeCents {
			t.Fatalf("%s Platform Fee earned over all time = %d, want the operator summary's %d",
				totals.Currency, got, totals.PlatformFeeCents)
		}
	}
}
