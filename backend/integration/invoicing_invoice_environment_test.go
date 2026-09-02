package integration

import (
	"net/http"
	"testing"
)

// NARROWING THE TAX INVOICES LIST TO ONE ENVIRONMENT, AND PROVING THE
// FILTERS COMPOSE (#598, spec #593). A certification run against SRI pruebas
// produces documents that are real to the SRI and real to nobody else, and a
// month-end reconciliation that swept one up would hand an accountant a
// factura standing for no income.
//
// The properties with teeth:
//
//   - EACH ENVIRONMENT EXCLUDES THE OTHER, and absent excludes neither. The
//     default is every document, so every link written before this filter
//     existed still names the view it always named.
//   - A DOCUMENT WITH NO ENVIRONMENT is in neither. An owed Sale Invoice has
//     not been signed, so it was issued under no environment at all — the
//     same shape the Emission Date range has, and for the same reason.
//   - AN ENVIRONMENT THE LIST DOES NOT KNOW IS REFUSED, never quietly read
//     as "every environment" (spec #593 story 25).
//   - THE FILTERS COMPOSE. This is the slice that proves it: the
//     environment, the status, the Emission Date range, the search and the
//     order all narrow the SAME query, and none of them wins.
//
// Everything is asserted through the list endpoint.

// The Emission Dates the fixture emits on: two consecutive days, so that a
// date range can cut the production documents in half and the environment
// filter can be shown to be doing something other than the date's work.
const (
	environmentDayOne = "2026-07-05"
	environmentDayTwo = "2026-07-06"
)

// The Recipients: ZEBRA is declared by three of the four emitted documents,
// so the search term crosses the environment boundary and has to be narrowed
// by the environment rather than instead of it.
const (
	environmentRecipientZebra = "FUNDACION ZEBRA S.A."
	environmentRecipientOther = "COMERCIAL DEL SUR CIA. LTDA."
)

// invoiceEnvironmentFixture is five documents: one emitted under SRI pruebas,
// three under producción, and one owed Sale Invoice under neither.
//
// The three production documents differ from each other by DAY and by
// RECIPIENT, so that the composed query below can be shown to be narrowed by
// each of its clauses separately: drop any one and a different named
// document comes back.
type invoiceEnvironmentFixture struct {
	operatorSessionID string
	// testZebra is the certification document: environment `test`, emitted
	// on day one, declared to ZEBRA. It is what a production-only view must
	// never return.
	testZebra string
	// prodZebraDayOne, prodZebraDayTwo and prodOtherDayTwo are the
	// producción documents: two to ZEBRA on either day, and one to another
	// Recipient on the second day.
	prodZebraDayOne string
	prodZebraDayTwo string
	prodOtherDayTwo string
	// owed is the Sale Invoice a paid House checkout owes: no Issuer, no
	// number and NO ENVIRONMENT until the Drainer signs it.
	owed string
	// numbers are the printed numbers the fixture ended up with, read back
	// off the list. The sequence is allocated PER ENVIRONMENT, so the test
	// document and the first production document both carry ...0000001 —
	// which is the sharpest thing here: a search for that number finds two
	// documents, and only the environment can tell them apart.
	numbers map[string]string
}

// everyDocument is the fixture's five: what an unnarrowed list must return,
// so an assertion about an environment is never quietly one about seeding.
func (f invoiceEnvironmentFixture) everyDocument() []string {
	return []string{f.testZebra, f.prodZebraDayOne, f.prodZebraDayTwo, f.prodOtherDayTwo, f.owed}
}

func newInvoiceEnvironmentFixture(t *testing.T, env *testEnv) invoiceEnvironmentFixture {
	t.Helper()
	sriStub.reset()
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "House Fest", "house-fest", 1000, 10)

	// The House checkout runs BEFORE the Issuer exists, so the Sale Invoice
	// it owes stays owed and never acquires an environment — which is the
	// one document this filter has to decide about rather than compare.
	paidCheckoutApproved(t, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 2)))

	// The Issuer starts in `test`, which is where a real one is certified
	// first, and is flipped to `production` afterwards — the operator's own
	// route, and the one TestIssueInvoiceSequencePerEnvironment takes.
	issuerReady(t, operatorSessionID)
	f := invoiceEnvironmentFixture{
		operatorSessionID: operatorSessionID,
		testZebra:         issueOnEmissionDate(t, operatorSessionID, environmentDayOne, invoiceBodyFor(environmentRecipientZebra, 10000)).ID,
	}

	flip := validEcuadorIssuerBody()
	flip["environment"] = "production"
	putEcuadorIssuer(t, sriEnv, operatorSessionID, flip)
	f.prodZebraDayOne = issueOnEmissionDate(t, operatorSessionID, environmentDayOne, invoiceBodyFor(environmentRecipientZebra, 10000)).ID
	f.prodZebraDayTwo = issueOnEmissionDate(t, operatorSessionID, environmentDayTwo, invoiceBodyFor(environmentRecipientZebra, 10000)).ID
	f.prodOtherDayTwo = issueOnEmissionDate(t, operatorSessionID, environmentDayTwo, invoiceBodyFor(environmentRecipientOther, 10000)).ID

	// The arrangement is read back off the list rather than assumed: a
	// fixture that quietly issued everything under one environment would
	// make every assertion below pass while proving nothing.
	f.numbers = map[string]string{}
	environments := map[string]string{}
	for _, row := range invoiceSearch(t, operatorSessionID, "").Data {
		if row.Number != nil {
			f.numbers[row.ID] = *row.Number
		}
		if row.Environment != nil {
			environments[row.ID] = *row.Environment
		}
		if row.Kind == "sale" {
			if row.Environment != nil {
				t.Fatalf("the fixture's Sale Invoice was issued under %q; it must have no environment at all", *row.Environment)
			}
			f.owed = row.ID
		}
	}
	if f.owed == "" {
		t.Fatal("the paid House checkout owed no Sale Invoice; the fixture is not set up")
	}
	if environments[f.testZebra] != "test" {
		t.Fatalf("the fixture's certification document was issued under %q; want test", environments[f.testZebra])
	}
	for _, id := range []string{f.prodZebraDayOne, f.prodZebraDayTwo, f.prodOtherDayTwo} {
		if environments[id] != "production" {
			t.Fatalf("fixture document %s was issued under %q; want production", id, environments[id])
		}
	}
	// The secuencial is allocated per environment, so the first document of
	// each carries the same printed number. The assertions that lean on that
	// say so; this insists the fixture really produced it.
	if f.numbers[f.testZebra] != f.numbers[f.prodZebraDayOne] {
		t.Fatalf("the first document of each environment carries %q and %q; the sequence is allocated per environment and they should collide",
			f.numbers[f.testZebra], f.numbers[f.prodZebraDayOne])
	}
	return f
}

// invoicesInEnvironment reads the list narrowed to one environment, spelled
// as the address bar spells it, plus any further filters.
func invoicesInEnvironment(t *testing.T, sessionID, environment, extra string) saleInvoiceListView {
	t.Helper()
	return invoiceSearch(t, sessionID, "environment="+environment+extra)
}

// TestTheTaxInvoicesListNarrowsToOneEnvironment: production excludes the
// certification document, test excludes the production ones, and absent
// excludes neither (spec #593 stories 15 and 16).
//
// Asserted from all three sides because "excluded" only means something
// against a view that includes it: the unnarrowed list is stated first, so a
// filter that returned nothing at all could not pass for one that narrowed.
func TestTheTaxInvoicesListNarrowsToOneEnvironment(t *testing.T) {
	env := setupTest(t)
	f := newInvoiceEnvironmentFixture(t, env)

	// Absent: every document, whatever environment it was issued under — the
	// view every link written before this filter existed still names.
	assertFinds(t, invoiceSearch(t, f.operatorSessionID, ""), f.everyDocument(), "(no environment)")

	// production: the month-end view. The certification document is gone.
	assertFinds(t, invoicesInEnvironment(t, f.operatorSessionID, "production", ""),
		[]string{f.prodZebraDayOne, f.prodZebraDayTwo, f.prodOtherDayTwo}, "environment=production")

	// test: the other way round, and the production documents are gone.
	assertFinds(t, invoicesInEnvironment(t, f.operatorSessionID, "test", ""),
		[]string{f.testZebra}, "environment=test")
}

// TestADocumentWithNoEnvironmentFallsOutOfEveryEnvironmentBoundedView: the
// owed Sale Invoice. It has not been signed, so it was issued under NO
// environment — not under the one the Issuer happens to point at today, and
// not under both.
//
// The Issuer here points at production by the end of the fixture, which is
// exactly the guess a bounded view must not make: an owed document is not a
// production document waiting to happen, and a reconciliation that counted
// it would declare income against a number that does not exist yet. This is
// the same rule the Emission Date range follows (#596) and is asserted from
// both sides for the same reason.
func TestADocumentWithNoEnvironmentFallsOutOfEveryEnvironmentBoundedView(t *testing.T) {
	env := setupTest(t)
	f := newInvoiceEnvironmentFixture(t, env)

	for _, environment := range []string{"production", "test"} {
		list := invoicesInEnvironment(t, f.operatorSessionID, environment, "")
		for _, row := range list.Data {
			if row.ID == f.owed {
				t.Fatalf("the list narrowed to environment=%s returned the owed Sale Invoice; a document nobody has signed was issued under no environment",
					environment)
			}
		}
	}

	// And it is reachable without the filter, so what excluded it above is
	// having no environment rather than the fixture failing to owe it.
	assertFinds(t, invoiceSearch(t, f.operatorSessionID, "status=owed"), []string{f.owed}, "status=owed")

	// Narrowed to the owed documents AND an environment, the answer is empty
	// rather than the owed one: the two filters intersect.
	assertFindsNothing(t, invoicesInEnvironment(t, f.operatorSessionID, "production", "&status=owed"),
		"environment=production + status=owed",
		"An owed Sale Invoice has no environment, so no environment contains it.")
}

// TestTheTaxInvoicesListRefusesAnEnvironmentItDoesNotKnow: a value the list
// cannot honour is REFUSED, never quietly read as "every environment" (spec
// #593 story 25). A page that silently ignored the filter would show a
// month-end reconciliation the test documents it was opened to exclude.
func TestTheTaxInvoicesListRefusesAnEnvironmentItDoesNotKnow(t *testing.T) {
	env := setupTest(t)
	f := newInvoiceEnvironmentFixture(t, env)

	cases := []struct {
		name  string
		query string
	}{
		{"an environment that does not exist", "environment=staging"},
		{"the SRI's own digit rather than the platform's word", "environment=1"},
		{"the wrong case", "environment=Production"},
		// "all" is the SELECT's word for the unnarrowed view, never the
		// wire's: the URL says "every environment" by leaving the parameter
		// out, so a link that spells it is a link that means nothing.
		{"the select's own word for no filter", "environment=all"},
		// A blank parameter is an operator clearing the select, and is the
		// unnarrowed view rather than a refusal — the same reading a blank
		// Emission Date bound gets.
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := sriEnv.get(t, invoicesPath+"?"+tc.query, authHeader(f.operatorSessionID))
			if resp.StatusCode != http.StatusBadRequest || body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
				t.Fatalf("list %q: status=%d error=%+v; want 400 VALIDATION_FAILED", tc.query, resp.StatusCode, body.Error)
			}
			if !fieldNamed(body.Error.Details, "environment") {
				t.Fatalf("list %q: error=%+v; want an environment field error naming what to fix", tc.query, body.Error)
			}
		})
	}

	// A cleared select is the whole list, not a refusal.
	assertFinds(t, invoicesInEnvironment(t, f.operatorSessionID, "", ""), f.everyDocument(), "environment= (blank)")
}

// TestEveryTaxInvoiceListFilterComposes: the slice's whole point (spec #593
// story 23). "Authorized, production, this day, for this Recipient, in this
// order" is ONE view, and each clause of it is doing its own work.
//
// The composed query is asserted first, and then each clause is DROPPED IN
// TURN: dropping one brings back exactly the document that clause was
// excluding, and no other. That is what tells a query where every filter
// composes apart from one where the last filter written silently won.
func TestEveryTaxInvoiceListFilterComposes(t *testing.T) {
	env := setupTest(t)
	f := newInvoiceEnvironmentFixture(t, env)

	const day = "&issued_from=" + environmentDayTwo + "&issued_to=" + environmentDayTwo
	const composed = "environment=production&status=authorized" + day + "&q=ZEBRA&sort=number&dir=asc"

	// All five clauses at once: one document survives.
	assertFinds(t, invoiceSearch(t, f.operatorSessionID, composed), []string{f.prodZebraDayTwo}, composed)

	// Drop the environment: the certification document to ZEBRA is on day
	// one, so nothing comes back from it — the environment is proved below,
	// where it is the only thing separating two documents. Drop the DAY
	// instead and the ZEBRA factura from day one returns.
	assertFinds(t, invoiceSearch(t, f.operatorSessionID, "environment=production&status=authorized&q=ZEBRA&sort=number&dir=asc"),
		[]string{f.prodZebraDayOne, f.prodZebraDayTwo}, "the composed view without the Emission Date range")

	// Drop the search: the other Recipient's factura, emitted the same day
	// under the same environment, joins it.
	assertFinds(t, invoiceSearch(t, f.operatorSessionID, "environment=production&status=authorized"+day+"&sort=number&dir=asc"),
		[]string{f.prodZebraDayTwo, f.prodOtherDayTwo}, "the composed view without the search")

	// THE SHARPEST CASE FOR THE ENVIRONMENT. The secuencial is allocated per
	// environment, so the first document of each carries the SAME printed
	// number: a search for it finds two documents, and the environment is
	// the only filter that can tell them apart. An operator pasting a number
	// off the SRI's producción portal must not be shown the pruebas document
	// that shares it.
	number := f.numbers[f.prodZebraDayOne]
	assertFinds(t, invoiceSearch(t, f.operatorSessionID, "q="+number),
		[]string{f.testZebra, f.prodZebraDayOne}, "q="+number)
	assertFinds(t, invoicesInEnvironment(t, f.operatorSessionID, "production", "&q="+number),
		[]string{f.prodZebraDayOne}, "environment=production + q="+number)
	assertFinds(t, invoicesInEnvironment(t, f.operatorSessionID, "test", "&q="+number),
		[]string{f.testZebra}, "environment=test + q="+number)

	// And the ORDER survives every narrowing: it reorders the documents the
	// filters left, rather than replacing what they did. Read by number
	// descending, the three production documents are their own sequence.
	assertListOrder(t, invoicesInEnvironment(t, f.operatorSessionID, "production", "&sort=number&dir=desc"),
		[]string{f.prodOtherDayTwo, f.prodZebraDayTwo, f.prodZebraDayOne},
		"environment=production, number desc")
	assertListOrder(t, invoicesInEnvironment(t, f.operatorSessionID, "production", "&sort=number&dir=asc"),
		[]string{f.prodZebraDayOne, f.prodZebraDayTwo, f.prodOtherDayTwo},
		"environment=production, number asc")

	// A combination nothing satisfies is empty rather than widened: the
	// production documents are all authorized, so asking for the abandoned
	// ones among them is an honest nothing.
	assertFindsNothing(t, invoiceSearch(t, f.operatorSessionID, "environment=production&status=abandoned"+day),
		"environment=production + status=abandoned"+day,
		"The filters INTERSECT; a combination nothing satisfies returns nothing, never the widest of them.")
}
