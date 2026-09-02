package integration

import (
	"net/http"
	"strconv"
	"testing"
)

// READING THE TAX INVOICES LIST IN THE ORDER A TASK NEEDS (#597, spec #593).
// An operator chasing one buyer's factura, an accountant checking a run of
// numbers for a gap, and a month-end reconciliation each want the same
// documents in a different order, and the order is a link like every other
// part of this view.
//
// The properties with teeth here are not "descending means descending":
//
//   - THE DEFAULT DID NOT MOVE. Absent sort and direction is the order this
//     list has always had — Emission Date descending, falling back to when a
//     document was created so an un-issued one sits at the newest end.
//   - A TIED PAIR HAS ONE ORDER. Two documents that tie on the column an
//     operator chose are broken apart by creation time and then id, so a
//     fifty-row window over two reads cannot show one document twice and
//     another not at all.
//   - A KEY THE LIST DOES NOT KNOW IS REFUSED, never quietly read as the
//     default: a link that ordered the page by something other than what it
//     names is a page the operator cannot trust.
//
// Everything is asserted through the list endpoint. Nothing here knows what
// column an order is built from — only which document comes first.

// The Emission Dates the fixture emits on: three consecutive days, with two
// documents sharing the middle one so that the tie is a real one.
const (
	sortDayEarliest = "2026-07-05"
	sortDayMiddle   = "2026-07-06"
	sortDayLatest   = "2026-07-07"
)

// The Recipients the fixture declares to. Chosen so that their alphabetical
// order is decided by the FIRST letter alone — Ana (the House buyer),
// BOLIVAR, MERIDIAN, ZEBRA — since a name pair that differed later could be
// ordered one way by one database collation and the other way by another,
// and this test is about the sort key, not about Postgres' locale.
const (
	sortRecipientEarly = "BOLIVAR DEL PACIFICO S.A."
	sortRecipientTied  = "MERIDIAN INDUSTRIAL CIA. LTDA."
	sortRecipientLate  = "ZEBRA FOUNDATION S.A."
)

// invoiceSortFixture is five documents arranged so that EVERY sort key puts
// them in a DIFFERENT order — otherwise a page ordered by the wrong column
// would still pass. Two of them tie on the date, the total and the Recipient
// at once, which is the tiebreaker's whole test.
type invoiceSortFixture struct {
	operatorSessionID string
	// zebra: emitted last of all, number ...0001, the SMALLEST total, and the
	// Recipient last in the alphabet. Every key disagrees about where it goes.
	zebra string
	// bolivar: emitted first, number ...0002, the LARGEST total, and the
	// first Recipient after the House buyer.
	bolivar string
	// tiedEarly and tiedLate are the tied pair: same Emission Date, same
	// total, same Recipient, issued one after the other — so on three of the
	// four keys nothing but the tiebreakers can tell them apart. They carry
	// numbers ...0003 and ...0004, which is the one key that does.
	tiedEarly string
	tiedLate  string
	// owed is the Sale Invoice a paid House checkout owes: NO Emission Date
	// and NO number, a total between zebra's and the tied pair's, and the
	// Recipient first in the alphabet.
	owed string
}

// everyDocument is the fixture's five, in no particular order: what the list
// must return under any ordering, so an assertion about ORDER is never
// quietly an assertion about membership.
func (f invoiceSortFixture) everyDocument() []string {
	return []string{f.zebra, f.bolivar, f.tiedEarly, f.tiedLate, f.owed}
}

func newInvoiceSortFixture(t *testing.T, env *testEnv) invoiceSortFixture {
	t.Helper()
	sriStub.reset()
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "House Fest", "house-fest", 1000, 10)

	// The House checkout runs BEFORE the Issuer exists, so the Sale Invoice it
	// owes stays owed: no Emission Date and no number, which is the document
	// that decides where the un-issued ones land in each order.
	paidCheckoutApproved(t, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 2)))

	issuerReady(t, operatorSessionID)
	f := invoiceSortFixture{
		operatorSessionID: operatorSessionID,
		zebra:             issueOnEmissionDate(t, operatorSessionID, sortDayLatest, invoiceBodyFor(sortRecipientLate, 1000)).ID,
		bolivar:           issueOnEmissionDate(t, operatorSessionID, sortDayEarliest, invoiceBodyFor(sortRecipientEarly, 30000)).ID,
		tiedEarly:         issueOnEmissionDate(t, operatorSessionID, sortDayMiddle, invoiceBodyFor(sortRecipientTied, 10000)).ID,
		tiedLate:          issueOnEmissionDate(t, operatorSessionID, sortDayMiddle, invoiceBodyFor(sortRecipientTied, 10000)).ID,
	}

	// The owed document and the arrangement the tests below rest on are read
	// back off the list rather than assumed: a fixture that quietly stopped
	// separating the totals would make "sorted by total" pass while ordering
	// by something else entirely.
	totals := map[string]int64{}
	for _, row := range invoiceSearch(t, operatorSessionID, "").Data {
		totals[row.ID] = row.TotalCents
		if row.Kind == "sale" {
			if row.IssuedOn != nil || row.Number != nil {
				t.Fatalf("the fixture's Sale Invoice is already emitted (%+v); it must have neither Emission Date nor number", row)
			}
			f.owed = row.ID
		}
	}
	if f.owed == "" {
		t.Fatal("the paid House checkout owed no Sale Invoice; the fixture is not set up")
	}
	if !(totals[f.zebra] < totals[f.owed] && totals[f.owed] < totals[f.tiedEarly] &&
		totals[f.tiedEarly] == totals[f.tiedLate] && totals[f.tiedLate] < totals[f.bolivar]) {
		t.Fatalf("fixture totals %v do not run zebra < owed < tied = tied < bolivar; the total order below would not mean what it says", totals)
	}
	return f
}

// invoiceBodyFor is a factura to one Recipient for one price: the two things
// the sort keys read off a manual document.
func invoiceBodyFor(legalName string, unitPriceCents int) map[string]any {
	body := validInvoiceBody()
	body["recipient"].(map[string]any)["legal_name"] = legalName
	body["lines"] = []map[string]any{
		{"description": "Platform Fee - July 2026", "quantity": "1", "unit_price_cents": unitPriceCents, "iva_rate": "15"},
	}
	return body
}

// invoicesSorted reads the list under one ordering, spelled as the address
// bar spells it, plus any further filters.
func invoicesSorted(t *testing.T, sessionID, sort, dir, extra string) saleInvoiceListView {
	t.Helper()
	return invoiceSearch(t, sessionID, "sort="+sort+"&dir="+dir+extra)
}

// assertListOrder states the WHOLE page, in sequence: an ordering that returned
// the right documents in the wrong order has failed, and so has one that
// dropped a document while ordering the rest correctly.
func assertListOrder(t *testing.T, list saleInvoiceListView, want []string, ordering string) {
	t.Helper()
	got := make([]string, 0, len(list.Data))
	for _, row := range list.Data {
		got = append(got, row.ID)
	}
	if len(got) != len(want) {
		t.Fatalf("the list ordered by %q returned %v; want %v", ordering, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the list ordered by %q returned %v; want %v (they differ at position %d)", ordering, got, want, i)
		}
	}
}

// TestTheTaxInvoicesListDefaultsToTheNewestEmissionDateFirst: the order an
// operator already knows must not have moved under them (spec #593 story
// 12). Absent sort and direction IS `date desc`, and an un-issued document —
// which has no Emission Date at all — sits at the newest end rather than
// falling out of sight at the bottom of the page.
func TestTheTaxInvoicesListDefaultsToTheNewestEmissionDateFirst(t *testing.T) {
	env := setupTest(t)
	f := newInvoiceSortFixture(t, env)

	// The owed document leads: with no Emission Date the order falls back to
	// when it was created, and it is the newest thing on the page.
	newestFirst := []string{f.owed, f.zebra, f.tiedLate, f.tiedEarly, f.bolivar}
	assertListOrder(t, invoiceSearch(t, f.operatorSessionID, ""), newestFirst, "(no sort)")

	// Asking for the default in so many words is the same page: the default
	// is a value of the vocabulary, not a separate code path.
	assertListOrder(t, invoicesSorted(t, f.operatorSessionID, "date", "desc", ""), newestFirst, "date desc")
}

// TestTheTaxInvoicesListSortsByEachOfItsFourColumnsInBothDirections: Date,
// Number, Total and Recipient, each way. The fixture is arranged so that no
// two of these eight answers coincide, so a page ordered by the wrong column
// cannot pass for one ordered by the right one.
func TestTheTaxInvoicesListSortsByEachOfItsFourColumnsInBothDirections(t *testing.T) {
	env := setupTest(t)
	f := newInvoiceSortFixture(t, env)

	cases := []struct {
		sort string
		dir  string
		want []string
		why  string
	}{
		{"date", "asc", []string{f.bolivar, f.tiedEarly, f.tiedLate, f.zebra, f.owed},
			"Oldest Emission Date first, and the un-issued document at the far end from where it leads."},
		{"date", "desc", []string{f.owed, f.zebra, f.tiedLate, f.tiedEarly, f.bolivar},
			"Newest Emission Date first: the list's default, restated as an explicit choice."},

		// The accountant's read: within one establishment and point of
		// emission the printed number IS the sequence, so a run read in
		// ascending order shows a gap where one exists.
		{"number", "asc", []string{f.zebra, f.bolivar, f.tiedEarly, f.tiedLate, f.owed},
			"...0001 through ...0004 in sequence, and the document with NO number after all of them."},
		{"number", "desc", []string{f.tiedLate, f.tiedEarly, f.bolivar, f.zebra, f.owed},
			"The sequence reversed — and the un-issued document STILL last, since it is not part of the run at either end."},

		{"total", "asc", []string{f.zebra, f.owed, f.tiedEarly, f.tiedLate, f.bolivar}, "Smallest total first."},
		{"total", "desc", []string{f.bolivar, f.tiedLate, f.tiedEarly, f.owed, f.zebra}, "Largest total first."},

		// The Recipient is the document's OWN snapshot, which is why the
		// House buyer sorts under the name her Sale Invoice declares.
		{"recipient", "asc", []string{f.owed, f.bolivar, f.tiedEarly, f.tiedLate, f.zebra}, "Recipients A to Z."},
		{"recipient", "desc", []string{f.zebra, f.tiedLate, f.tiedEarly, f.bolivar, f.owed}, "Recipients Z to A."},
	}
	for _, tc := range cases {
		t.Run(tc.sort+" "+tc.dir, func(t *testing.T) {
			list := invoicesSorted(t, f.operatorSessionID, tc.sort, tc.dir, "")
			if list.Pagination.Total != len(f.everyDocument()) {
				t.Fatalf("ordering the list by %s %s returned %d documents; an order narrows nothing and must return all %d",
					tc.sort, tc.dir, list.Pagination.Total, len(f.everyDocument()))
			}
			assertListOrder(t, list, tc.want, tc.sort+" "+tc.dir+": "+tc.why)
		})
	}
}

// TestTiedTaxInvoicesKeepOneOrder: the property that makes paging safe (spec
// #593 story 14). Two documents emitted on the same day, for the same total,
// to the same Recipient tie on three of the four keys — and on every one of
// them the pair is still ordered, by creation time and then by id.
//
// Without it Postgres may return a tied pair either way round, and a page
// boundary falling between them shows one document twice and hides the
// other. That is asserted here by reading the SAME ordering one document at
// a time and finding the pair whole.
func TestTiedTaxInvoicesKeepOneOrder(t *testing.T) {
	env := setupTest(t)
	f := newInvoiceSortFixture(t, env)

	// Ascending, the earlier-created of the pair leads; descending, the later
	// one does. The pair reverses WITH the list rather than floating inside
	// it.
	for _, sort := range []string{"date", "total", "recipient"} {
		t.Run(sort, func(t *testing.T) {
			assertPairInOrder(t, invoicesSorted(t, f.operatorSessionID, sort, "asc", ""), f.tiedEarly, f.tiedLate, sort+" asc")
			assertPairInOrder(t, invoicesSorted(t, f.operatorSessionID, sort, "desc", ""), f.tiedLate, f.tiedEarly, sort+" desc")
		})
	}

	// And the pair survives a page boundary drawn straight through it: read
	// one document at a time, the five pages are the one order, with each
	// tied document appearing exactly once.
	var walked []string
	for page := 1; page <= 5; page++ {
		list := invoiceSearch(t, f.operatorSessionID, "sort=total&dir=asc&page_size=1&page="+strconv.Itoa(page))
		if len(list.Data) != 1 {
			t.Fatalf("page %d of a one-per-page total-ordered list returned %d rows; want 1", page, len(list.Data))
		}
		walked = append(walked, list.Data[0].ID)
	}
	want := []string{f.zebra, f.owed, f.tiedEarly, f.tiedLate, f.bolivar}
	for i := range want {
		if walked[i] != want[i] {
			t.Fatalf("walking the total-ordered list one document per page gave %v; want %v.\n"+
				"A tied pair with no tiebreaker can repeat one document across pages and skip the other.", walked, want)
		}
	}
}

// assertPairInOrder finds two documents in a page and insists the first
// named comes before the second.
func assertPairInOrder(t *testing.T, list saleInvoiceListView, first, second, ordering string) {
	t.Helper()
	posOf := map[string]int{}
	for i, row := range list.Data {
		posOf[row.ID] = i
	}
	a, okA := posOf[first]
	b, okB := posOf[second]
	if !okA || !okB {
		t.Fatalf("the list ordered by %q did not return both of the tied documents", ordering)
	}
	if a > b {
		t.Fatalf("the list ordered by %q put the tied documents at positions %d and %d; want %s before %s.\n"+
			"Documents that tie on the chosen column are ordered by creation time and then id, so the pair never swaps.",
			ordering, a, b, first, second)
	}
}

// TestSortingTheTaxInvoicesListRefusesAKeyOrDirectionItDoesNotKnow: a sort
// the list cannot honour is REFUSED, never quietly answered in the default
// order (spec #593 story 25). A page silently ordered by something other
// than what its own address bar names is a page an operator reads wrong
// without ever being told.
func TestSortingTheTaxInvoicesListRefusesAKeyOrDirectionItDoesNotKnow(t *testing.T) {
	env := setupTest(t)
	f := newInvoiceSortFixture(t, env)

	cases := []struct {
		name  string
		query string
		field string
	}{
		{"a column that is not sortable", "sort=status", "sort"},
		{"a column that does not exist", "sort=colour", "sort"},
		// The wire spells the sorts in this list's own words; a raw column
		// name is not one of them, which is the point — nothing a request
		// carries is ever a column name.
		{"a database column name", "sort=recipient_legal_name", "sort"},
		{"a direction that is neither", "dir=sideways", "dir"},
		{"a direction in the wrong case", "sort=number&dir=ASC", "dir"},
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

// TestASortedTaxInvoicesListNarrowsAndPagesInThatOrder: the order and the
// filters are independent, and paging a sorted list CONTINUES it rather than
// starting it again. "Everything authorized, cheapest first, page two" is
// one view.
func TestASortedTaxInvoicesListNarrowsAndPagesInThatOrder(t *testing.T) {
	env := setupTest(t)
	f := newInvoiceSortFixture(t, env)

	// With the status: the owed document leaves the page, and the four that
	// remain keep the order they had.
	assertListOrder(t, invoicesSorted(t, f.operatorSessionID, "total", "asc", "&status=authorized"),
		[]string{f.zebra, f.tiedEarly, f.tiedLate, f.bolivar},
		"total asc + authorized: the filter removes a document; it does not reorder the rest")

	// And with the search (#595) and the Emission Date range (#596), which
	// narrow inside the order rather than replacing it.
	assertListOrder(t, invoicesSorted(t, f.operatorSessionID, "number", "desc", "&q=MERIDIAN"),
		[]string{f.tiedLate, f.tiedEarly}, "number desc + MERIDIAN")
	assertListOrder(t, invoicesSorted(t, f.operatorSessionID, "date", "asc", "&issued_from="+sortDayEarliest+"&issued_to="+sortDayMiddle),
		[]string{f.bolivar, f.tiedEarly, f.tiedLate}, "date asc + the first two days")

	// Page two continues where page one stopped: two pages of two documents
	// each, in one recipient order, with nothing repeated and nothing lost.
	pageOne := invoiceSearch(t, f.operatorSessionID, "sort=recipient&dir=desc&page_size=2&page=1")
	pageTwo := invoiceSearch(t, f.operatorSessionID, "sort=recipient&dir=desc&page_size=2&page=2")
	assertListOrder(t, pageOne, []string{f.zebra, f.tiedLate}, "recipient desc, page 1 of 2 per page")
	assertListOrder(t, pageTwo, []string{f.tiedEarly, f.bolivar}, "recipient desc, page 2 of 2 per page")
	if pageOne.Pagination.Total != len(f.everyDocument()) || pageTwo.Pagination.Total != len(f.everyDocument()) {
		t.Fatalf("the paged totals are %d and %d; both must be the %d documents the ordering returned",
			pageOne.Pagination.Total, pageTwo.Pagination.Total, len(f.everyDocument()))
	}
}
