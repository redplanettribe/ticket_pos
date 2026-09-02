package integration

import (
	"net/http"
	"testing"
	"time"
)

// NARROWING THE TAX INVOICES LIST TO AN EMISSION DATE RANGE (#596, spec
// #593). "Everything emitted in August" is the view an operator hands an
// accountant, and it is one link.
//
// The property with teeth is what the range compares against: the EMISSION
// DATE ALONE — the calendar day in the Issuer's country the document itself
// carries and the Tax Authority reads, which is the day the list's Date
// column shows. A document with no Emission Date, an owed Sale Invoice the
// Drainer has not signed, therefore falls OUT of any date-bounded view: a
// fiscal period must never count a document the authority has not seen. That
// is deliberately the opposite of what the default order does with the same
// column, which keeps un-issued documents at the top by falling back to when
// they were created — and both are right, which is why the exclusion is
// asserted here from both sides rather than assumed.

// The days the fixture below emits on, and the range asked for. Three
// consecutive days so that "the start day", "the end day" and "the day after
// the end" are each one document, which is the whole of the inclusivity
// question.
const (
	emissionRangeStart = "2026-07-05"
	emissionRangeEnd   = "2026-07-06"
	emissionDayAfter   = "2026-07-07"
)

// invoiceDateRangeFixture is four documents: three manual facturas emitted on
// three consecutive days, and one owed Sale Invoice with no Emission Date at
// all.
type invoiceDateRangeFixture struct {
	operatorSessionID string
	// onStart is emitted on the range's first day, and is the only one whose
	// Recipient is named ZEBRA — so the range can be shown to combine with a
	// search rather than to be overridden by one.
	onStart string
	// onEnd is emitted on the range's last day.
	onEnd string
	// dayAfter is emitted the day AFTER the range's end: the document that
	// proves the upper bound is a bound and the range is not open-ended.
	dayAfter string
	// owed is the Sale Invoice a paid House checkout owes — kind `sale`,
	// status `owed`, and NO Emission Date, which is the point of it here.
	owed string
}

func newInvoiceDateRangeFixture(t *testing.T, env *testEnv) invoiceDateRangeFixture {
	t.Helper()
	sriStub.reset()
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "House Fest", "house-fest", 1000, 10)

	// The House checkout runs BEFORE the Issuer exists, so the Sale Invoice it
	// owes stays owed and never acquires an Emission Date — which is the one
	// document this whole filter has to keep out.
	paidCheckoutApproved(t, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 2)))

	issuerReady(t, operatorSessionID)
	zebra := validInvoiceBody()
	zebra["recipient"].(map[string]any)["legal_name"] = "FUNDACION ZEBRA S.A."

	f := invoiceDateRangeFixture{
		operatorSessionID: operatorSessionID,
		onStart:           issueOnEmissionDate(t, operatorSessionID, emissionRangeStart, zebra).ID,
		onEnd:             issueOnEmissionDate(t, operatorSessionID, emissionRangeEnd, validInvoiceBody()).ID,
		dayAfter:          issueOnEmissionDate(t, operatorSessionID, emissionDayAfter, validInvoiceBody()).ID,
	}
	for _, row := range invoiceSearch(t, operatorSessionID, "").Data {
		if row.Kind == "sale" {
			if row.IssuedOn != nil {
				t.Fatalf("the fixture's Sale Invoice was emitted on %q; it must have no Emission Date", *row.IssuedOn)
			}
			f.owed = row.ID
		}
	}
	if f.owed == "" {
		t.Fatal("the paid House checkout owed no Sale Invoice; the fixture is not set up")
	}
	return f
}

// issueOnEmissionDate issues a factura whose Emission Date is the given
// calendar day.
//
// THE CLOCK IS THE SEAM, not a written-in issued_on: the Emission Date is
// derived at signing from the invoicing service's clock, read as a Guayaquil
// calendar day, so moving that clock is how a document comes to be emitted on
// a chosen day through the same route a real one takes. Noon UTC is the
// morning of the SAME day in Guayaquil (UTC-5), so the day asked for is the
// day the document carries — which the assertion below insists on, since a
// fixture that quietly emitted on the day before would make every range test
// here mean something other than it says. The clock is put back afterwards,
// as every other clock-moving test in this package does.
func issueOnEmissionDate(t *testing.T, sessionID, day string, body map[string]any) invoiceDetailView {
	t.Helper()
	at, err := time.Parse("2006-01-02", day)
	if err != nil {
		t.Fatalf("fixture day %q: %v", day, err)
	}
	noon := at.Add(12 * time.Hour)
	sriApp.InvoicingService.WithClock(func() time.Time { return noon })
	defer sriApp.InvoicingService.WithClock(func() time.Time { return fixedClock })
	invoice := issueOK(t, sessionID, body)
	if invoice.IssuedOn != day {
		t.Fatalf("issued with the clock at %s, the document carries Emission Date %q; want %q", noon, invoice.IssuedOn, day)
	}
	return invoice
}

// invoicesEmittedBetween reads the list under an Emission Date range, either
// bound blank for an open one, plus any further filters spelled as the
// address bar spells them.
func invoicesEmittedBetween(t *testing.T, sessionID, from, to, extra string) saleInvoiceListView {
	t.Helper()
	return invoiceSearch(t, sessionID, emissionRangeQuery(from, to)+extra)
}

// emissionRangeQuery spells a range the way the address bar carries it. A
// blank bound is written as a blank parameter rather than omitted, so the
// tests below also cover an operator clearing one date input and leaving the
// other set — the API must read that as an open bound, not refuse it.
func emissionRangeQuery(from, to string) string {
	return "issued_from=" + from + "&issued_to=" + to
}

// TestTheTaxInvoicesListNarrowsToAnEmissionDateRange: both bounds are
// INCLUSIVE, and the day after the end is outside. This is the month-end
// view: hand the accountant everything emitted in the window and nothing
// emitted outside it.
func TestTheTaxInvoicesListNarrowsToAnEmissionDateRange(t *testing.T) {
	env := setupTest(t)
	f := newInvoiceDateRangeFixture(t, env)

	list := invoicesEmittedBetween(t, f.operatorSessionID, emissionRangeStart, emissionRangeEnd, "")
	assertFinds(t, list, []string{f.onStart, f.onEnd},
		emissionRangeQuery(emissionRangeStart, emissionRangeEnd))

	// A one-day range is the same rule twice over: a day is a range whose
	// bounds meet, and it must return exactly the documents emitted that day.
	assertFinds(t, invoicesEmittedBetween(t, f.operatorSessionID, emissionRangeEnd, emissionRangeEnd, ""),
		[]string{f.onEnd}, emissionRangeQuery(emissionRangeEnd, emissionRangeEnd))

	// The day after the end has its own document, and it is reachable — the
	// range excluded it rather than the fixture failing to emit it.
	assertFinds(t, invoicesEmittedBetween(t, f.operatorSessionID, emissionDayAfter, emissionDayAfter, ""),
		[]string{f.dayAfter}, emissionRangeQuery(emissionDayAfter, emissionDayAfter))

	// A window nothing was emitted in is empty, not the whole list.
	assertFindsNothing(t, invoicesEmittedBetween(t, f.operatorSessionID, "2026-08-01", "2026-08-31", ""),
		emissionRangeQuery("2026-08-01", "2026-08-31"),
		"A period the platform emitted nothing in is an empty list, never an unnarrowed one.")
}

// TestAnEmissionDateBoundWorksOnItsOwn: "everything since July 1st" and
// "everything up to the 5th" are each ONE parameter, so an operator never has
// to invent the other end of a range they do not have one for.
func TestAnEmissionDateBoundWorksOnItsOwn(t *testing.T) {
	env := setupTest(t)
	f := newInvoiceDateRangeFixture(t, env)

	// issued_from alone: the start day and everything after it.
	assertFinds(t, invoicesEmittedBetween(t, f.operatorSessionID, emissionRangeEnd, "", ""),
		[]string{f.onEnd, f.dayAfter}, emissionRangeQuery(emissionRangeEnd, ""))

	// issued_to alone: the end day and everything before it.
	assertFinds(t, invoicesEmittedBetween(t, f.operatorSessionID, "", emissionRangeEnd, ""),
		[]string{f.onStart, f.onEnd}, emissionRangeQuery("", emissionRangeEnd))
}

// TestADocumentWithNoEmissionDateFallsOutOfEveryDateBoundedView: the fiscal
// rule, and the one this filter exists to get right (#596, spec #593 story
// 9). An owed Sale Invoice has no Emission Date because the Drainer has not
// signed it, and a month it was never declared in must not count it.
//
// Asserted from BOTH sides — present when neither bound is given, absent when
// either one is — because "excluded" is only meaningful against a view that
// includes it, and because the default order deliberately keeps exactly this
// document at the top by falling back to when it was created. The sort's
// fallback must not have leaked into the filter.
func TestADocumentWithNoEmissionDateFallsOutOfEveryDateBoundedView(t *testing.T) {
	env := setupTest(t)
	f := newInvoiceDateRangeFixture(t, env)

	// Unbounded, the owed document is on the list — and first, since the
	// order falls back to when it was created.
	assertFinds(t, invoiceSearch(t, f.operatorSessionID, ""),
		[]string{f.owed, f.onStart, f.onEnd, f.dayAfter}, "(no range)")

	// A range wide enough to hold every emitted document still leaves it out:
	// what excludes it is having no Emission Date, not the width of the
	// window.
	assertFinds(t, invoicesEmittedBetween(t, f.operatorSessionID, "2000-01-01", "2099-12-31", ""),
		[]string{f.onStart, f.onEnd, f.dayAfter}, emissionRangeQuery("2000-01-01", "2099-12-31"))

	// EITHER bound alone is enough to drop it, so a half-open range is no
	// loophole.
	assertFinds(t, invoicesEmittedBetween(t, f.operatorSessionID, "2000-01-01", "", ""),
		[]string{f.onStart, f.onEnd, f.dayAfter}, emissionRangeQuery("2000-01-01", ""))
	assertFinds(t, invoicesEmittedBetween(t, f.operatorSessionID, "", "2099-12-31", ""),
		[]string{f.onStart, f.onEnd, f.dayAfter}, emissionRangeQuery("", "2099-12-31"))

	// Narrowed to the owed documents AND a date range, the answer is empty
	// rather than the owed one: the two filters intersect, and an owed
	// document is in no period.
	assertFindsNothing(t, invoicesEmittedBetween(t, f.operatorSessionID, "2000-01-01", "2099-12-31", "&status=owed"),
		"a wide range + status=owed",
		"An owed Sale Invoice has no Emission Date, so no period contains it.")
}

// TestTheEmissionDateRangeCombinesWithTheOtherFilters: "authorized, emitted
// in this window, for this Recipient" is ONE view. The filters intersect;
// none of them wins.
func TestTheEmissionDateRangeCombinesWithTheOtherFilters(t *testing.T) {
	env := setupTest(t)
	f := newInvoiceDateRangeFixture(t, env)

	whole := emissionRangeQuery(emissionRangeStart, emissionDayAfter)

	// With the status: every emitted document here is authorized, and none of
	// them is owed.
	assertFinds(t, invoicesEmittedBetween(t, f.operatorSessionID, emissionRangeStart, emissionDayAfter, "&status=authorized"),
		[]string{f.onStart, f.onEnd, f.dayAfter}, whole+" + authorized")
	assertFindsNothing(t, invoicesEmittedBetween(t, f.operatorSessionID, emissionRangeStart, emissionDayAfter, "&status=owed"),
		whole+" + owed", "The range and the status INTERSECT; neither wins.")

	// With the search (#595): ZEBRA is emitted on the range's first day and
	// nowhere else, so the term narrows inside the window and the window
	// narrows around the term.
	assertFinds(t, invoicesEmittedBetween(t, f.operatorSessionID, emissionRangeStart, emissionDayAfter, "&q=ZEBRA"),
		[]string{f.onStart}, whole+" + ZEBRA")
	assertFindsNothing(t, invoicesEmittedBetween(t, f.operatorSessionID, emissionRangeEnd, emissionDayAfter, "&q=ZEBRA"),
		emissionRangeQuery(emissionRangeEnd, emissionDayAfter)+" + ZEBRA",
		"The one document declaring ZEBRA was emitted before this window opened.")

	// And with the kind, which is the filter that was here first.
	assertFinds(t, invoicesEmittedBetween(t, f.operatorSessionID, emissionRangeStart, emissionRangeEnd, "&kind=manual"),
		[]string{f.onStart, f.onEnd}, whole+" + manual")
}

// TestAMalformedOrInvertedEmissionDateRangeIsRefused: a range that is not a
// range is REFUSED, never answered with an empty list. The two facts "nothing
// was emitted then" and "that is not a date" lead to different next moves,
// and an empty page silently sends an operator looking for documents that are
// there.
func TestAMalformedOrInvertedEmissionDateRangeIsRefused(t *testing.T) {
	env := setupTest(t)
	f := newInvoiceDateRangeFixture(t, env)

	cases := []struct {
		name  string
		query string
		field string
	}{
		{"a start that is not a date", "issued_from=August", "issued_from"},
		{"an end that is not a date", "issued_to=2026-13-45", "issued_to"},
		{"a timestamp where a calendar day belongs", "issued_from=2026-07-05T00:00:00Z", "issued_from"},
		{"a start after the end", emissionRangeQuery(emissionDayAfter, emissionRangeStart), "issued_from"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := sriEnv.get(t, invoicesPath+"?"+tc.query, authHeader(f.operatorSessionID))
			if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
				t.Fatalf("list %q: status=%d error=%+v; want 400 VALIDATION_FAILED", tc.query, resp.StatusCode, body.Error)
			}
			if !fieldNamed(body.Error.Details, tc.field) {
				t.Fatalf("list %q: error=%+v; want a %s field error naming what to fix", tc.query, body.Error, tc.field)
			}
		})
	}
}
