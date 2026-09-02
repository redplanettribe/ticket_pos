package integration

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

// FINDING A TAX INVOICE (#595, spec #593). One box, four fields: an operator
// with a buyer on the phone does not know whether what they have been read is
// a name, a cédula, a number off the SRI portal or a Sale Confirmation
// reference off a receipt, so the list takes any of them and matches a
// case-insensitive substring of each.
//
// The two properties with teeth, restated from the Sales list because "search"
// must mean the same thing on both screens: the match is case-insensitive, and
// a LIKE metacharacter is a LITERAL — without the escape, a typed `%` returns
// EVERY document under a filter bar claiming to be narrowed.

// invoiceSearchFixture is three documents that share nothing searchable: two
// manual facturas, told apart by their Recipient's name, Tax ID and number,
// and one owed Sale Invoice, which has no number at all and is reachable only
// by its Sale's Confirmation reference.
type invoiceSearchFixture struct {
	operatorSessionID string
	// byName is the factura to "FUNDACION ZEBRA S.A." — number ...0000001.
	byName string
	// byTaxID is the factura to a Recipient identified by otherCedula —
	// number ...0000002.
	byTaxID string
	// owed is the Sale Invoice a paid House checkout owes: kind `sale`,
	// status `owed`, no number, and the Sale's reference beside it.
	owed    string
	owedRef string
}

func newInvoiceSearchFixture(t *testing.T, env *testEnv) invoiceSearchFixture {
	t.Helper()
	sriStub.reset()
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "House Fest", "house-fest", 1000, 10)

	// The House checkout runs BEFORE the Issuer exists, so the Sale Invoice
	// it owes stays owed: an unsigned document is the one that tests whether
	// a document with no number can still be found.
	ref := paidCheckoutApproved(t, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 2)))

	issuerReady(t, operatorSessionID)
	zebra := validInvoiceBody()
	zebra["recipient"].(map[string]any)["legal_name"] = "FUNDACION ZEBRA S.A."
	byName := issueOK(t, operatorSessionID, zebra)

	cedula := validInvoiceBody()
	cedula["recipient"].(map[string]any)["tax_id_type"] = "cedula"
	cedula["recipient"].(map[string]any)["tax_id"] = otherCedula
	cedula["recipient"].(map[string]any)["legal_name"] = "COMERCIAL DEL SUR CIA. LTDA."
	byTaxID := issueOK(t, operatorSessionID, cedula)

	f := invoiceSearchFixture{operatorSessionID: operatorSessionID, byName: byName.ID, byTaxID: byTaxID.ID, owedRef: ref}
	for _, row := range invoiceSearch(t, operatorSessionID, "").Data {
		if row.Kind == "sale" {
			f.owed = row.ID
		}
	}
	if f.owed == "" {
		t.Fatal("the paid House checkout owed no Sale Invoice; the fixture is not set up")
	}
	if byName.Number != "001-001-000000001" || byTaxID.Number != "001-001-000000002" {
		t.Fatalf("fixture numbers = %q / %q; want ...0000001 and ...0000002", byName.Number, byTaxID.Number)
	}
	return f
}

// invoiceSearch reads the list under a raw query string (the leading "?" and
// its own escaping are the caller's, so a test can spell a filter exactly as
// the address bar does).
func invoiceSearch(t *testing.T, sessionID, query string) saleInvoiceListView {
	t.Helper()
	path := invoicesPath
	if query != "" {
		path += "?" + query
	}
	resp, env := sriEnv.get(t, path, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list %q status=%d error=%+v", query, resp.StatusCode, env.Error)
	}
	var list saleInvoiceListView
	if err := json.Unmarshal(env.Data, &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return list
}

// searchInvoices runs one term through the box, plus any further filters.
func searchInvoices(t *testing.T, sessionID, term, extra string) saleInvoiceListView {
	t.Helper()
	return invoiceSearch(t, sessionID, "q="+url.QueryEscape(term)+extra)
}

// assertFinds states the whole answer, never a membership: a search that also
// returned the other two documents has failed even though it "found" the one
// asked about. The pagination total is asserted with the rows because that
// number is what the page reports the search found.
func assertFinds(t *testing.T, list saleInvoiceListView, want []string, term string) {
	t.Helper()
	got := make([]string, 0, len(list.Data))
	for _, row := range list.Data {
		got = append(got, row.ID)
	}
	if len(got) != len(want) || list.Pagination.Total != len(want) {
		t.Fatalf("searching %q returned %v (total %d); want exactly %v", term, got, list.Pagination.Total, want)
	}
	for _, id := range want {
		found := false
		for _, g := range got {
			if g == id {
				found = true
			}
		}
		if !found {
			t.Fatalf("searching %q returned %v; want %v", term, got, want)
		}
	}
}

func assertFindsNothing(t *testing.T, list saleInvoiceListView, term, why string) {
	t.Helper()
	if len(list.Data) != 0 || list.Pagination.Total != 0 {
		t.Fatalf("searching %q returned %d rows (total %d); want none.\n%s", term, len(list.Data), list.Pagination.Total, why)
	}
}

// TestSearchingTheTaxInvoicesListFindsADocumentByAnyOfItsFourFields: the
// number, the Recipient's legal name, the Recipient's Tax ID and the Sale
// Confirmation reference each find their own document and NOT the others, and
// a term that is nowhere finds nothing rather than everything.
func TestSearchingTheTaxInvoicesListFindsADocumentByAnyOfItsFourFields(t *testing.T) {
	env := setupTest(t)
	f := newInvoiceSearchFixture(t, env)

	// The printed number, as pasted off the SRI portal.
	assertFinds(t, searchInvoices(t, f.operatorSessionID, "001-001-000000002", ""),
		[]string{f.byTaxID}, "001-001-000000002")
	// A tail of it is enough: the number is a substring match like the rest.
	assertFinds(t, searchInvoices(t, f.operatorSessionID, "000000001", ""),
		[]string{f.byName}, "000000001")

	// The Recipient's legal name — the document's OWN snapshot.
	assertFinds(t, searchInvoices(t, f.operatorSessionID, "ZEBRA", ""),
		[]string{f.byName}, "ZEBRA")
	// The buyer of the House sale is found by the name the Sale Invoice
	// declares her under, which is what the SRI will hold.
	assertFinds(t, searchInvoices(t, f.operatorSessionID, "Ana Lopez", ""),
		[]string{f.owed}, "Ana Lopez")

	// The Recipient's Tax ID: every document declared to one taxpayer.
	assertFinds(t, searchInvoices(t, f.operatorSessionID, otherCedula, ""),
		[]string{f.byTaxID}, otherCedula)

	// The Sale Confirmation reference: the way from a Ticket Sale a buyer
	// quotes to the document that declares it. This document has no number,
	// so it is reachable by nothing else.
	assertFinds(t, searchInvoices(t, f.operatorSessionID, f.owedRef, ""),
		[]string{f.owed}, f.owedRef)

	assertFindsNothing(t, searchInvoices(t, f.operatorSessionID, "NOSUCHTHING", ""), "NOSUCHTHING",
		"A term that matches nothing must empty the list, not widen it.")
}

// TestTheTaxInvoicesSearchIsCaseInsensitiveAndTreatsWildcardsAsLiterals: the
// Sales list's two properties (#595 copies its semantics clause for clause).
//
// The wildcard half is the one with teeth: unescaped, `%` is "match
// everything" and `_` is "match any one character", so an operator who typed
// either would be handed every document the platform has ever issued under a
// search box claiming to have narrowed the list.
func TestTheTaxInvoicesSearchIsCaseInsensitiveAndTreatsWildcardsAsLiterals(t *testing.T) {
	env := setupTest(t)
	f := newInvoiceSearchFixture(t, env)

	for _, term := range []string{"zebra", "ZeBrA", "fundacion zebra s.a."} {
		assertFinds(t, searchInvoices(t, f.operatorSessionID, term, ""), []string{f.byName}, term)
	}
	assertFinds(t, searchInvoices(t, f.operatorSessionID, "ana lopez", ""), []string{f.owed}, "ana lopez")

	// Nothing in this fixture contains either character, so a literal match is
	// empty where an unescaped one would return all three documents.
	for _, wildcard := range []string{"%", "_", "%%", "a%z", "00_001"} {
		assertFindsNothing(t, searchInvoices(t, f.operatorSessionID, wildcard, ""), wildcard,
			"A LIKE metacharacter typed into the box is a LITERAL, never a pattern (platform.LikeEscape).\n"+
				"Unescaped, this term matches every document and the page shows the whole list under a "+
				"search box claiming to be narrowed.")
	}

	// Blank is no search at all: the box emptied and submitted is the whole
	// list back, not a search for the empty string.
	assertFinds(t, searchInvoices(t, f.operatorSessionID, "   ", ""),
		[]string{f.byName, f.byTaxID, f.owed}, "(blank)")
}

// TestTheTaxInvoicesSearchCombinesWithTheKindAndStatusFilters: the filters
// INTERSECT rather than one of them winning, which is what makes "the owed
// Sale Invoice for this buyer" one view.
func TestTheTaxInvoicesSearchCombinesWithTheKindAndStatusFilters(t *testing.T) {
	env := setupTest(t)
	f := newInvoiceSearchFixture(t, env)

	assertFinds(t, searchInvoices(t, f.operatorSessionID, "ZEBRA", "&kind=manual"), []string{f.byName}, "ZEBRA + manual")
	assertFindsNothing(t, searchInvoices(t, f.operatorSessionID, "ZEBRA", "&kind=sale"), "ZEBRA + sale",
		"The search and the kind INTERSECT; neither wins.")

	assertFinds(t, searchInvoices(t, f.operatorSessionID, "Lopez", "&status=owed"), []string{f.owed}, "Lopez + owed")
	assertFindsNothing(t, searchInvoices(t, f.operatorSessionID, "Lopez", "&status=authorized"), "Lopez + authorized",
		"The one document declaring Ana is owed, not authorized.")

	assertFinds(t, searchInvoices(t, f.operatorSessionID, "001-001-", "&status=authorized&kind=manual"),
		[]string{f.byName, f.byTaxID}, "001-001- + authorized + manual")
}
