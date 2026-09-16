package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// THE TAX DOCUMENT ARCHIVE (#630, spec #629; CONTEXT.md): every document the
// Ecuador Issuer emitted in a range of Emission Dates that the SRI authorized
// in production and that still stands, as the stored signed XML, in one ZIP
// with a readme, for the platform's accountant.
//
// What has teeth is WHICH documents: the fixture below seeds one of every
// kind of document the rule has an opinion about, around a three-day range,
// so a single archive shows every inclusion and every exclusion at once. The
// pre-download summary (#631) reuses the same fixture, so its counts can be
// asserted equal to what the archive actually holds.

// taxArchivePath is the archive route.
const taxArchivePath = "/api/v1/operator/invoicing/archive"

// taxArchiveSummaryPath is the archive's pre-download summary (#631).
const taxArchiveSummaryPath = "/api/v1/operator/invoicing/archive/summary"

// The range the fixture is built around, and the days either side of it.
// The range starts on fixedClock's day because the Drainer can only sign a
// Sale Invoice at or after the moment its sale was recorded (fixedClock);
// manual documents can be emitted on any day.
const (
	taxArchiveDayBefore = "2026-07-06"
	taxArchiveFrom      = "2026-07-07"
	taxArchiveMiddle    = "2026-07-08"
	taxArchiveTo        = "2026-07-09"
	taxArchiveDayAfter  = "2026-07-10"
)

// taxArchiveRUC is the RUC validEcuadorIssuerBody records.
const taxArchiveRUC = "1790012345001"

// taxDocumentArchiveFixture names every document the fixture seeds.
type taxDocumentArchiveFixture struct {
	operatorSessionID string

	// INCLUDED: authorized, production, emitted in the range.
	manualOnFrom      string // a manual factura on the range's first day
	manualOnTo        string // a manual factura on the range's last day
	saleAuthorized    string // a Sale Invoice
	saleSuperseded    string // a Sale Invoice corrected by a reissue
	reissueCreditNote string // the Credit Note that undid it
	reissueCorrected  string // the corrected Sale Invoice that replaced it
	saleReversed      string // a Sale Invoice whose Sale was reversed
	reversalCredit    string // the Credit Note that reversal owed

	// UNSETTLED: production, emitted in the range, pending or needs_attention.
	manualPending      string
	saleNeedsAttention string

	// EXCLUDED and not counted.
	manualNotAuthorized string
	manualRejected      string
	manualAnnulled      string
	saleAbandoned       string
	saleWithdrawn       string // never signed: no Emission Date
	saleOwed            string // never signed: no Emission Date
	manualTestEnv       string // authorized in the SRI's test environment
	manualDayBefore     string // authorized production, the day before
	manualDayAfter      string // authorized production, the day after
	pendingDayAfter     string // unsettled, but the day after

	// signedWithdrawn is withdrawn WITH an Emission Date in the range. No
	// code path withdraws a signed document (every writer is guarded on
	// signed_xml IS NULL or on reading the row unsigned), but no constraint
	// forbids it either, so it is seeded directly: the status, not a
	// missing date, must be what keeps a withdrawn document out.
	signedWithdrawn string
}

// included is the archive's documents, as ids.
func (f taxDocumentArchiveFixture) included() []string {
	return []string{f.manualOnFrom, f.manualOnTo, f.saleAuthorized, f.saleSuperseded, f.reissueCreditNote, f.reissueCorrected, f.saleReversed, f.reversalCredit}
}

// Expected readme counts for the fixture's range.
const (
	taxArchiveFacturas    = 6 // two manual, four Sale Invoices
	taxArchiveCreditNotes = 2
	taxArchiveUnsettled   = 2
)

// newTaxDocumentArchiveFixture seeds the documents above through the same
// routes a real document takes: manual issue, House checkouts and the
// Drainer, reissue, reversal, annul and abandon. Emission Dates are chosen
// by moving the invoicing clock, which is what the signer reads.
func newTaxDocumentArchiveFixture(t *testing.T, env *testEnv) taxDocumentArchiveFixture {
	t.Helper()
	sriStub.reset()
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	sid := operatorSession(t, env, "operator@example.com")
	designateHouseOrganization(t, env, sid, orgID)
	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "House Fest", "house-fest", 1000, 20)
	f := taxDocumentArchiveFixture{operatorSessionID: sid}

	// The test-environment document first, under an Issuer still pointed at
	// SRI pruebas; then the Issuer moves to production for everything else.
	issuerReady(t, sid)
	f.manualTestEnv = atEmissionDay(t, taxArchiveMiddle, func() string { return issueOK(t, sid, validInvoiceBody()).ID })
	production := validEcuadorIssuerBody()
	production["environment"] = "production"
	putEcuadorIssuer(t, sriEnv, sid, production)

	// Manual documents.
	f.manualOnFrom = atEmissionDay(t, taxArchiveFrom, func() string { return issueOK(t, sid, validInvoiceBody()).ID })
	f.manualOnTo = atEmissionDay(t, taxArchiveTo, func() string { return issueOK(t, sid, validInvoiceBody()).ID })
	f.manualDayBefore = atEmissionDay(t, taxArchiveDayBefore, func() string { return issueOK(t, sid, validInvoiceBody()).ID })
	f.manualDayAfter = atEmissionDay(t, taxArchiveDayAfter, func() string { return issueOK(t, sid, validInvoiceBody()).ID })
	f.manualPending = atEmissionDay(t, taxArchiveMiddle, func() string { return issuePending(t, sid).ID })
	f.pendingDayAfter = atEmissionDay(t, taxArchiveDayAfter, func() string { return issuePending(t, sid).ID })
	f.manualAnnulled = atEmissionDay(t, taxArchiveMiddle, func() string { return issuePending(t, sid).ID })
	if view := annulOK(t, sid, f.manualAnnulled); view.Status != "annulled" {
		t.Fatalf("setup: annul left the manual document %s", view.Status)
	}
	sriStub.answerAsUsual()
	f.manualRejected = atEmissionDay(t, taxArchiveMiddle, func() string { return issueRejected(t, sid).ID })
	sriStub.answerAsUsual()
	f.manualNotAuthorized = atEmissionDay(t, taxArchiveMiddle, func() string {
		sriStub.setAuthorization(func(accessKey string) (int, string) {
			return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("39", "FIRMA INVALIDA", "", "ERROR"))
		})
		view := issueOK(t, sid, validInvoiceBody())
		if view.Status != "not_authorized" {
			t.Fatalf("setup: status=%q, want not_authorized", view.Status)
		}
		return view.ID
	})
	sriStub.answerAsUsual()
	f.signedWithdrawn = atEmissionDay(t, taxArchiveMiddle, func() string { return issueOK(t, sid, validInvoiceBody()).ID })
	if _, err := sriEnv.db.Exec(`UPDATE invoicing_invoices SET status = 'withdrawn' WHERE id = $1`, f.signedWithdrawn); err != nil {
		t.Fatalf("setup: withdraw the signed manual document: %v", err)
	}

	checkout := func() (ref, invoiceID string) {
		t.Helper()
		known := archiveFixtureDocumentIDs(t, sid)
		ref = paidCheckoutApproved(t, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
		return ref, archiveFixtureNewDocument(t, sid, known, "sale")
	}

	// Three Sale Invoices authorized on the range's first day: one stays,
	// one is superseded by a reissue, one is credited by a reversal.
	_, f.saleAuthorized = checkout()
	_, f.saleSuperseded = checkout()
	reversedRef, reversedSale := checkout()
	f.saleReversed = reversedSale
	atEmissionDay(t, taxArchiveFrom, func() string {
		if result := drainSaleInvoices(t); result.Authorized != 3 {
			t.Fatalf("setup drain = %+v; want three Sale Invoices authorized", result)
		}
		return ""
	})

	// The reissue and the reversal, their documents emitted the next day.
	known := archiveFixtureDocumentIDs(t, sid)
	reissueOK(t, sid, f.saleSuperseded, companyRecipient())
	f.reissueCreditNote = archiveFixtureNewDocument(t, sid, known, "credit_note")
	f.reissueCorrected = archiveFixtureNewDocument(t, sid, known, "sale")
	known = archiveFixtureDocumentIDs(t, sid)
	reversalRoutes[1].reverse(t, env, sid, reversedRef)
	f.reversalCredit = archiveFixtureNewDocument(t, sid, known, "credit_note")
	atEmissionDay(t, taxArchiveMiddle, func() string {
		drainUntilQuiet(t)
		return ""
	})
	for _, id := range []string{f.reissueCreditNote, f.reissueCorrected, f.reversalCredit, reversedSale} {
		if view := getDrainedInvoice(t, sid, id); view.Status != "authorized" {
			t.Fatalf("setup: document %s (%s) is %s; want authorized", id, view.Kind, view.Status)
		}
	}

	// A Sale Invoice the SRI refuses to authorize, parked needs_attention.
	_, f.saleNeedsAttention = checkout()
	atEmissionDay(t, taxArchiveTo, func() string {
		sriStub.setAuthorization(func(accessKey string) (int, string) {
			return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("65", "FECHA EMISION EXTEMPORANEA", "", "ERROR"))
		})
		if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
			t.Fatalf("setup drain = %+v; want the Sale Invoice parked", result)
		}
		return ""
	})
	sriStub.answerAsUsual()

	// A Sale Invoice whose number the SRI refuses, then abandoned.
	_, f.saleAbandoned = checkout()
	atEmissionDay(t, taxArchiveTo, func() string {
		sriStub.setReception(func(accessKey string) (int, string) {
			return http.StatusOK, returnedSOAP(accessKey, sriMessage("45", "ERROR SECUENCIAL REGISTRADO", "", "ERROR"))
		})
		if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
			t.Fatalf("setup drain = %+v; want the Sale Invoice parked", result)
		}
		sriStub.setAuthorization(func(accessKey string) (int, string) {
			return http.StatusOK, emptyAuthorizationSOAP(accessKey)
		})
		if resp, body := checkInvoice(t, sid, f.saleAbandoned); resp.StatusCode != http.StatusOK {
			t.Fatalf("setup check status=%d error=%+v", resp.StatusCode, body.Error)
		}
		resp, body := abandonInvoice(t, sid, f.saleAbandoned, map[string]any{"note": "not registered at the portal"})
		if view := abandonedView(t, resp, body); view.Status != "abandoned" {
			t.Fatalf("setup abandon left the Sale Invoice %s", view.Status)
		}
		return ""
	})
	sriStub.answerAsUsual()

	// A Sale Invoice withdrawn before it was ever signed, and one still owed.
	withdrawnRef, withdrawn := checkout()
	reversalRoutes[1].reverse(t, env, sid, withdrawnRef)
	f.saleWithdrawn = withdrawn
	_, f.saleOwed = checkout()

	// Every document is where the fixture says it is, or nothing below means
	// what it claims.
	want := map[string]struct{ status, environment, issuedOn string }{
		f.manualOnFrom:        {"authorized", "production", taxArchiveFrom},
		f.manualOnTo:          {"authorized", "production", taxArchiveTo},
		f.saleAuthorized:      {"authorized", "production", taxArchiveFrom},
		f.saleSuperseded:      {"authorized", "production", taxArchiveFrom},
		f.reissueCreditNote:   {"authorized", "production", taxArchiveMiddle},
		f.reissueCorrected:    {"authorized", "production", taxArchiveMiddle},
		f.saleReversed:        {"authorized", "production", taxArchiveFrom},
		f.reversalCredit:      {"authorized", "production", taxArchiveMiddle},
		f.manualPending:       {"pending", "production", taxArchiveMiddle},
		f.saleNeedsAttention:  {"needs_attention", "production", taxArchiveTo},
		f.manualNotAuthorized: {"not_authorized", "production", taxArchiveMiddle},
		f.manualRejected:      {"rejected", "production", taxArchiveMiddle},
		f.manualAnnulled:      {"annulled", "production", taxArchiveMiddle},
		f.saleAbandoned:       {"abandoned", "production", taxArchiveTo},
		f.saleWithdrawn:       {"withdrawn", "", ""},
		f.signedWithdrawn:     {"withdrawn", "production", taxArchiveMiddle},
		f.saleOwed:            {"owed", "", ""},
		f.manualTestEnv:       {"authorized", "test", taxArchiveMiddle},
		f.manualDayBefore:     {"authorized", "production", taxArchiveDayBefore},
		f.manualDayAfter:      {"authorized", "production", taxArchiveDayAfter},
		f.pendingDayAfter:     {"pending", "production", taxArchiveDayAfter},
	}
	if len(want) != 21 {
		t.Fatalf("setup: the fixture named %d distinct documents; want 21", len(want))
	}
	for id, w := range want {
		view := getDrainedInvoice(t, sid, id)
		if view.Status != w.status || orEmpty(view.Environment) != w.environment || orEmpty(view.IssuedOn) != w.issuedOn {
			t.Fatalf("setup: document %s (%s) is status=%s environment=%q issued_on=%q; want %+v",
				id, view.Kind, view.Status, orEmpty(view.Environment), orEmpty(view.IssuedOn), w)
		}
	}
	return f
}

// atEmissionDay runs fn with the invoicing clock at noon UTC on the given day
// (the morning of the same day in Guayaquil), so whatever it signs carries
// that Emission Date, and puts the clock back.
func atEmissionDay(t *testing.T, day string, fn func() string) string {
	t.Helper()
	at, err := time.Parse("2006-01-02", day)
	if err != nil {
		t.Fatalf("fixture day %q: %v", day, err)
	}
	sriApp.InvoicingService.WithClock(func() time.Time { return at.Add(12 * time.Hour) })
	defer sriApp.InvoicingService.WithClock(func() time.Time { return fixedClock })
	return fn()
}

// drainUntilQuiet drains until a round claims nothing, since a corrected
// Sale Invoice waits for its Credit Note's authorization.
func drainUntilQuiet(t *testing.T) {
	t.Helper()
	for round := 0; round < 5; round++ {
		if result := drainSaleInvoices(t); result.Claimed == 0 {
			return
		}
	}
	t.Fatal("setup: the Drainer kept claiming documents after five rounds")
}

func archiveFixtureDocumentIDs(t *testing.T, sessionID string) map[string]bool {
	t.Helper()
	ids := map[string]bool{}
	for _, row := range getSaleInvoiceList(t, sessionID).Data {
		ids[row.ID] = true
	}
	return ids
}

// archiveFixtureNewDocument is the one document of the kind that is not in
// known.
func archiveFixtureNewDocument(t *testing.T, sessionID string, known map[string]bool, kind string) string {
	t.Helper()
	var found []string
	for _, row := range getSaleInvoiceList(t, sessionID).Data {
		if !known[row.ID] && row.Kind == kind {
			found = append(found, row.ID)
		}
	}
	if len(found) != 1 {
		t.Fatalf("setup: %d new %s documents appeared; want one", len(found), kind)
	}
	return found[0]
}

// orEmpty reads an optional API string with absence as "", which is how the
// fixture spells "no environment" and "no Emission Date". (The package's
// deref prints absence as "<nil>", for failure messages.)
func orEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// taxArchiveQuery spells a range.
func taxArchiveQuery(from, to string) string {
	return taxArchivePath + "?from=" + from + "&to=" + to
}

// archivedDocument is an included document as the test expects to find it.
type archivedDocument struct {
	issuedOn  string
	accessKey string
	signedXML []byte
}

// expectedArchive reads each id's Emission Date, clave and stored signed XML
// through the operator's own routes, in the archive's order.
func expectedArchive(t *testing.T, sessionID string, ids []string) []archivedDocument {
	t.Helper()
	docs := make([]archivedDocument, 0, len(ids))
	for _, id := range ids {
		view := getDrainedInvoice(t, sessionID, id)
		if view.EcuadorFull == nil {
			t.Fatalf("document %s has no Ecuador numbering", id)
		}
		docs = append(docs, archivedDocument{
			issuedOn:  orEmpty(view.IssuedOn),
			accessKey: view.EcuadorFull.AccessKey,
			signedXML: storedSignedXML(t, sessionID, id),
		})
	}
	sort.Slice(docs, func(i, j int) bool {
		if docs[i].issuedOn != docs[j].issuedOn {
			return docs[i].issuedOn < docs[j].issuedOn
		}
		return docs[i].accessKey < docs[j].accessKey
	})
	return docs
}

// assertReadmeCounts checks the readme's three numbers, in its language.
func assertReadmeCounts(t *testing.T, readme []byte, facturas, creditNotes, unsettled int, labels [3]string) {
	t.Helper()
	text := string(readme)
	for i, want := range []int{facturas, creditNotes, unsettled} {
		line := fmt.Sprintf("%s: %d\r\n", labels[i], want)
		if !strings.Contains(text, line) {
			t.Fatalf("the readme does not state %q; it reads:\n%s", line, text)
		}
	}
}

var (
	readmeLabelsEN = [3]string{"Facturas", "Credit Notes", "Documents emitted in the period not yet settled by the SRI"}
	readmeLabelsES = [3]string{"Facturas", "Notas de crédito", "Comprobantes emitidos en el periodo aún sin resolver por el SRI"}
)

// TestTheTaxDocumentArchiveHoldsExactlyThePeriodsStandingProductionDocuments
// is the rule entire: the authorized production documents emitted in the
// range, of every kind, Superseded included, each its stored signed XML named
// by its clave, in Emission Date then clave order, and a readme last whose
// counts are the files written and the pending + needs_attention documents.
func TestTheTaxDocumentArchiveHoldsExactlyThePeriodsStandingProductionDocuments(t *testing.T) {
	env := setupTest(t)
	f := newTaxDocumentArchiveFixture(t, env)
	capture := &captureLogger{}
	sriApp.InvoicingService.WithLogger(capture)
	t.Cleanup(func() { sriApp.InvoicingService.WithLogger(platform.NewSlogLogger(sriApp.Logger)) })

	pack := downloadPack(t, sriEnv, f.operatorSessionID, taxArchiveQuery(taxArchiveFrom, taxArchiveTo))

	if want := "comprobantes-" + taxArchiveRUC + "-" + taxArchiveFrom + "-" + taxArchiveTo + ".zip"; pack.Filename != want {
		t.Fatalf("filename = %q; want %q", pack.Filename, want)
	}

	expected := expectedArchive(t, f.operatorSessionID, f.included())
	wantNames := make([]string, 0, len(expected)+1)
	for _, doc := range expected {
		wantNames = append(wantNames, doc.accessKey+".xml")
	}
	wantNames = append(wantNames, "README.txt")
	if strings.Join(pack.Names, "\n") != strings.Join(wantNames, "\n") {
		t.Fatalf("the archive holds\n%s\nwant exactly, in order\n%s", strings.Join(pack.Names, "\n"), strings.Join(wantNames, "\n"))
	}
	for _, doc := range expected {
		if got := pack.Files[doc.accessKey+".xml"]; string(got) != string(doc.signedXML) {
			t.Fatalf("%s.xml is not the stored signed XML byte for byte", doc.accessKey)
		}
	}
	for _, name := range pack.Names {
		if strings.Contains(name, "/") {
			t.Fatalf("entry %q is in a folder; the archive is flat", name)
		}
	}

	readme := pack.Files["README.txt"]
	assertReadmeCounts(t, readme, taxArchiveFacturas, taxArchiveCreditNotes, taxArchiveUnsettled, readmeLabelsEN)
	for _, fragment := range []string{taxArchiveFrom, taxArchiveTo, taxArchiveRUC} {
		if !strings.Contains(string(readme), fragment) {
			t.Fatalf("the readme does not name %q; it reads:\n%s", fragment, readme)
		}
	}

	// One log line: who, which period, what it held. No Recipient data.
	line := capture.only(t, "tax document archive generated")
	for key, want := range map[string]any{
		"operator_email": "operator@example.com",
		"from":           taxArchiveFrom,
		"to":             taxArchiveTo,
		"facturas":       taxArchiveFacturas,
		"credit_notes":   taxArchiveCreditNotes,
		"unsettled":      taxArchiveUnsettled,
	} {
		if got := line.arg(t, key); got != want {
			t.Fatalf("log %s = %v; want %v", key, got, want)
		}
	}
	for _, pii := range []string{"ORGANIZACION EJEMPLO", "Ana", "Lopez", "guest@example.com", "billing@example.com"} {
		if strings.Contains(capture.rendered(), pii) {
			t.Fatalf("the log carries Recipient data %q:\n%s", pii, capture.rendered())
		}
	}
}

// TestTheTaxDocumentArchiveRangeIsInclusiveAtBothEnds: a one-day range on
// each bound holds that day's documents, and the days either side hold their
// own, which the three-day range left out.
func TestTheTaxDocumentArchiveRangeIsInclusiveAtBothEnds(t *testing.T) {
	env := setupTest(t)
	f := newTaxDocumentArchiveFixture(t, env)

	cases := []struct {
		day  string
		want []string
	}{
		{taxArchiveFrom, []string{f.manualOnFrom, f.saleAuthorized, f.saleSuperseded, f.saleReversed}},
		{taxArchiveTo, []string{f.manualOnTo}},
		{taxArchiveDayBefore, []string{f.manualDayBefore}},
		{taxArchiveDayAfter, []string{f.manualDayAfter}},
	}
	for _, tc := range cases {
		pack := downloadPack(t, sriEnv, f.operatorSessionID, taxArchiveQuery(tc.day, tc.day))
		var want []string
		for _, doc := range expectedArchive(t, f.operatorSessionID, tc.want) {
			want = append(want, doc.accessKey+".xml")
		}
		want = append(want, "README.txt")
		if strings.Join(pack.Names, "\n") != strings.Join(want, "\n") {
			t.Fatalf("archive of %s holds %v; want %v", tc.day, pack.Names, want)
		}
	}

	// The day after holds one unsettled document of its own.
	pack := downloadPack(t, sriEnv, f.operatorSessionID, taxArchiveQuery(taxArchiveDayAfter, taxArchiveDayAfter))
	assertReadmeCounts(t, pack.Files["README.txt"], 1, 0, 1, readmeLabelsEN)
}

// TestAnEmptyPeriodIsAReadmeOnlyArchive: nothing emitted is an answer, not an
// error, and the readme says zero three times.
func TestAnEmptyPeriodIsAReadmeOnlyArchive(t *testing.T) {
	env := setupTest(t)
	f := newTaxDocumentArchiveFixture(t, env)

	pack := downloadPack(t, sriEnv, f.operatorSessionID, taxArchiveQuery("2026-08-01", "2026-08-31"))
	if len(pack.Names) != 1 || pack.Names[0] != "README.txt" {
		t.Fatalf("an empty period's archive holds %v; want the readme alone", pack.Names)
	}
	assertReadmeCounts(t, pack.Files["README.txt"], 0, 0, 0, readmeLabelsEN)
}

// TestTheTaxDocumentArchiveReadmeFollowsTheOperatorsStaffLocale: LEEME.txt
// in Spanish for an operator who reads Spanish, with the same counts.
func TestTheTaxDocumentArchiveReadmeFollowsTheOperatorsStaffLocale(t *testing.T) {
	env := setupTest(t)
	f := newTaxDocumentArchiveFixture(t, env)
	setStaffLocale(t, env, f.operatorSessionID, "es")

	pack := downloadPack(t, sriEnv, f.operatorSessionID, taxArchiveQuery(taxArchiveFrom, taxArchiveTo))
	last := pack.Names[len(pack.Names)-1]
	if last != "LEEME.txt" {
		t.Fatalf("the last entry is %q; want LEEME.txt", last)
	}
	if _, ok := pack.Files["README.txt"]; ok {
		t.Fatal("the Spanish archive also carries README.txt")
	}
	assertReadmeCounts(t, pack.Files["LEEME.txt"], taxArchiveFacturas, taxArchiveCreditNotes, taxArchiveUnsettled, readmeLabelsES)
}

// TestAMissingMalformedOrReversedArchiveRangeIsRefused: the list's Emission
// Date filter's refusal, on the archive's two required bounds and on its
// summary's, which are the same two.
func TestAMissingMalformedOrReversedArchiveRangeIsRefused(t *testing.T) {
	env := setupTest(t)
	sid := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sid)

	cases := []struct {
		name  string
		query string
		field string
	}{
		{"no range at all", "", "from"},
		{"no end", "?from=2026-07-01", "to"},
		{"no start", "?to=2026-07-31", "from"},
		{"a start that is not a date", "?from=July&to=2026-07-31", "from"},
		{"an end that is not a date", "?from=2026-07-01&to=2026-13-45", "to"},
		{"a timestamp where a day belongs", "?from=2026-07-01T00:00:00Z&to=2026-07-31", "from"},
		{"a start after the end", "?from=2026-07-31&to=2026-07-01", "from"},
	}
	for _, path := range []string{taxArchivePath, taxArchiveSummaryPath} {
		for _, tc := range cases {
			t.Run(path+" "+tc.name, func(t *testing.T) {
				resp, body := sriEnv.getRaw(t, path+tc.query, authHeader(sid))
				refusal := decodeErrorEnvelope(t, body)
				if resp.StatusCode != http.StatusBadRequest || refusal.Error == nil || refusal.Error.Code != "VALIDATION_FAILED" {
					t.Fatalf("%s%s: status=%d body=%s; want 400 VALIDATION_FAILED", path, tc.query, resp.StatusCode, body)
				}
				if !fieldNamed(refusal.Error.Details, tc.field) {
					t.Fatalf("%s%s: error=%+v; want a %s field error", path, tc.query, refusal.Error, tc.field)
				}
			})
		}
	}
}

// TestTheTaxDocumentArchiveIsPlatformOperatorsOnly: a Member and a caller with
// no session are refused with the envelope, never a ZIP.
func TestTheTaxDocumentArchiveIsPlatformOperatorsOnly(t *testing.T) {
	env := setupTest(t)
	orgAdminSession(t, env)
	sid := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sid)
	member := verifyOTP(t, env, "admin@example.com")

	for _, path := range []string{taxArchiveQuery(taxArchiveFrom, taxArchiveTo), taxArchiveSummaryQuery(taxArchiveFrom, taxArchiveTo)} {
		resp, body := sriEnv.getRaw(t, path, authHeader(member))
		if refusal := decodeErrorEnvelope(t, body); resp.StatusCode != http.StatusForbidden || refusal.Error == nil || refusal.Error.Code != "FORBIDDEN" {
			t.Fatalf("a Member's %s: status=%d body=%s; want 403 FORBIDDEN", path, resp.StatusCode, body)
		}
		resp, body = sriEnv.getRaw(t, path, nil)
		if refusal := decodeErrorEnvelope(t, body); resp.StatusCode != http.StatusUnauthorized || refusal.Error == nil || refusal.Error.Code != "UNAUTHORIZED" {
			t.Fatalf("an anonymous %s: status=%d body=%s; want 401 UNAUTHORIZED", path, resp.StatusCode, body)
		}
	}
}

// TestTheTaxDocumentArchiveWithNoIssuerIsIssuerNotFound: the module's own
// refusal, before any byte of a ZIP.
func TestTheTaxDocumentArchiveWithNoIssuerIsIssuerNotFound(t *testing.T) {
	env := setupTest(t)
	sid := operatorSession(t, env, "operator@example.com")

	for _, path := range []string{taxArchiveQuery(taxArchiveFrom, taxArchiveTo), taxArchiveSummaryQuery(taxArchiveFrom, taxArchiveTo)} {
		resp, body := sriEnv.getRaw(t, path, authHeader(sid))
		if refusal := decodeErrorEnvelope(t, body); resp.StatusCode != http.StatusNotFound || refusal.Error == nil || refusal.Error.Code != "ISSUER_NOT_FOUND" {
			t.Fatalf("%s with no Issuer: status=%d body=%s; want 404 ISSUER_NOT_FOUND", path, resp.StatusCode, body)
		}
	}
}

// taxArchiveSummaryQuery spells a range on the summary route.
func taxArchiveSummaryQuery(from, to string) string {
	return taxArchiveSummaryPath + "?from=" + from + "&to=" + to
}

// taxArchiveSummaryView is the summary as the staff app reads it.
type taxArchiveSummaryView struct {
	Facturas    int `json:"facturas"`
	CreditNotes int `json:"credit_notes"`
	Unsettled   int `json:"unsettled"`
}

// fetchTaxArchiveSummary reads the summary for a range, which must answer 200.
func fetchTaxArchiveSummary(t *testing.T, sessionID, from, to string) taxArchiveSummaryView {
	t.Helper()
	path := taxArchiveSummaryQuery(from, to)
	resp, body := sriEnv.getRaw(t, path, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, resp.StatusCode, body)
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil || env.Error != nil || env.RequestID == "" {
		t.Fatalf("GET %s body=%s: %v; want a success envelope with a request_id", path, body, err)
	}
	var view taxArchiveSummaryView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode summary %s: %v", env.Data, err)
	}
	return view
}

// TestTheTaxDocumentArchiveSummaryCountsWhatTheArchiveHolds: for the same
// range, the numbers an operator is shown before downloading are the
// readme's numbers AND the files actually in the ZIP, told apart by the
// document type the clave de acceso carries (01 factura, 04 nota de crédito).
func TestTheTaxDocumentArchiveSummaryCountsWhatTheArchiveHolds(t *testing.T) {
	env := setupTest(t)
	f := newTaxDocumentArchiveFixture(t, env)

	ranges := [][2]string{
		{taxArchiveFrom, taxArchiveTo},
		{taxArchiveDayBefore, taxArchiveDayAfter},
		{taxArchiveDayAfter, taxArchiveDayAfter},
	}
	for _, r := range ranges {
		summary := fetchTaxArchiveSummary(t, f.operatorSessionID, r[0], r[1])
		pack := downloadPack(t, sriEnv, f.operatorSessionID, taxArchiveQuery(r[0], r[1]))

		facturas, creditNotes := 0, 0
		for _, name := range pack.Names {
			if !strings.HasSuffix(name, ".xml") {
				continue
			}
			// The clave de acceso is ddmmaaaa then the two-digit codDoc.
			switch codDoc := name[8:10]; codDoc {
			case "01":
				facturas++
			case "04":
				creditNotes++
			default:
				t.Fatalf("archive entry %s has document type %s", name, codDoc)
			}
		}
		if summary.Facturas != facturas || summary.CreditNotes != creditNotes {
			t.Fatalf("%s..%s: summary %+v; the ZIP holds %d facturas and %d Credit Notes", r[0], r[1], summary, facturas, creditNotes)
		}
		assertReadmeCounts(t, pack.Files["README.txt"], summary.Facturas, summary.CreditNotes, summary.Unsettled, readmeLabelsEN)
	}

	// And the fixture's own expectation, so agreeing-but-wrong cannot pass.
	want := taxArchiveSummaryView{Facturas: taxArchiveFacturas, CreditNotes: taxArchiveCreditNotes, Unsettled: taxArchiveUnsettled}
	if got := fetchTaxArchiveSummary(t, f.operatorSessionID, taxArchiveFrom, taxArchiveTo); got != want {
		t.Fatalf("summary = %+v; want %+v", got, want)
	}
}

// TestAnEmptyPeriodsSummaryIsZeros: nothing emitted is an answer, not an
// error.
func TestAnEmptyPeriodsSummaryIsZeros(t *testing.T) {
	env := setupTest(t)
	f := newTaxDocumentArchiveFixture(t, env)

	if got := fetchTaxArchiveSummary(t, f.operatorSessionID, "2026-08-01", "2026-08-31"); got != (taxArchiveSummaryView{}) {
		t.Fatalf("an empty period's summary = %+v; want zeros", got)
	}
}

// failingWriter is a response writer whose client has gone: every write runs
// onWrite (which may cancel the request's context, as net/http does when the
// connection drops) and then fails.
type failingWriter struct{ onWrite func() }

func (w failingWriter) Write([]byte) (int, error) {
	if w.onWrite != nil {
		w.onWrite()
	}
	return 0, io.ErrClosedPipe
}

// TestATaxDocumentArchiveTheClientCancelsIsNotLoggedAsAFailure: a download
// the operator abandons stops the stream either at the query (the request's
// context is cancelled first) or at a write (the connection is gone and the
// context with it). Neither is the platform failing, so neither is an Error
// line; a write failing while the request still stands is.
func TestATaxDocumentArchiveTheClientCancelsIsNotLoggedAsAFailure(t *testing.T) {
	env := setupTest(t)
	newTaxDocumentArchiveFixture(t, env)

	cases := []struct {
		name      string
		write     func(archive *service.TaxDocumentArchive) error
		wantLevel string
		wantMsg   string
	}{
		{
			name: "cancelled before the query",
			write: func(archive *service.TaxDocumentArchive) error {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return archive.WriteTo(ctx, io.Discard)
			},
			wantLevel: "info",
			wantMsg:   "tax document archive download cancelled",
		},
		{
			name: "cancelled while writing",
			write: func(archive *service.TaxDocumentArchive) error {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				return archive.WriteTo(ctx, failingWriter{onWrite: cancel})
			},
			wantLevel: "info",
			wantMsg:   "tax document archive download cancelled",
		},
		{
			name: "a write failing while the request stands",
			write: func(archive *service.TaxDocumentArchive) error {
				return archive.WriteTo(context.Background(), failingWriter{})
			},
			wantLevel: "error",
			wantMsg:   "tax document archive failed mid-stream",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture := &captureLogger{}
			sriApp.InvoicingService.WithLogger(capture)
			t.Cleanup(func() { sriApp.InvoicingService.WithLogger(platform.NewSlogLogger(sriApp.Logger)) })

			archive, err := sriApp.InvoicingService.OpenTaxDocumentArchive(context.Background(), "operator@example.com", taxArchiveFrom, taxArchiveTo)
			if err != nil {
				t.Fatalf("open archive: %v", err)
			}
			if err := tc.write(archive); err == nil {
				t.Fatal("WriteTo succeeded; want the stream to stop with an error")
			}
			if len(capture.lines) != 1 {
				t.Fatalf("logged %d lines; want exactly one:\n%s", len(capture.lines), capture.rendered())
			}
			if line := capture.lines[0]; line.level != tc.wantLevel || !strings.Contains(line.msg, tc.wantMsg) {
				t.Fatalf("logged %s %q; want %s %q", line.level, line.msg, tc.wantLevel, tc.wantMsg)
			}
		})
	}
}
