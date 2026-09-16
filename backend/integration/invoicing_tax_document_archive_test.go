package integration

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

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
		f.saleOwed:            {"owed", "", ""},
		f.manualTestEnv:       {"authorized", "test", taxArchiveMiddle},
		f.manualDayBefore:     {"authorized", "production", taxArchiveDayBefore},
		f.manualDayAfter:      {"authorized", "production", taxArchiveDayAfter},
		f.pendingDayAfter:     {"pending", "production", taxArchiveDayAfter},
	}
	if len(want) != 20 {
		t.Fatalf("setup: the fixture named %d distinct documents; want 20", len(want))
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
// Date filter's refusal, on the archive's two required bounds.
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
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := sriEnv.getRaw(t, taxArchivePath+tc.query, authHeader(sid))
			refusal := decodeErrorEnvelope(t, body)
			if resp.StatusCode != http.StatusBadRequest || refusal.Error == nil || refusal.Error.Code != "VALIDATION_FAILED" {
				t.Fatalf("archive %q: status=%d body=%s; want 400 VALIDATION_FAILED", tc.query, resp.StatusCode, body)
			}
			if !fieldNamed(refusal.Error.Details, tc.field) {
				t.Fatalf("archive %q: error=%+v; want a %s field error", tc.query, refusal.Error, tc.field)
			}
		})
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
	path := taxArchiveQuery(taxArchiveFrom, taxArchiveTo)

	resp, body := sriEnv.getRaw(t, path, authHeader(member))
	if refusal := decodeErrorEnvelope(t, body); resp.StatusCode != http.StatusForbidden || refusal.Error == nil || refusal.Error.Code != "FORBIDDEN" {
		t.Fatalf("a Member's archive: status=%d body=%s; want 403 FORBIDDEN", resp.StatusCode, body)
	}
	resp, body = sriEnv.getRaw(t, path, nil)
	if refusal := decodeErrorEnvelope(t, body); resp.StatusCode != http.StatusUnauthorized || refusal.Error == nil || refusal.Error.Code != "UNAUTHORIZED" {
		t.Fatalf("an anonymous archive: status=%d body=%s; want 401 UNAUTHORIZED", resp.StatusCode, body)
	}
}

// TestTheTaxDocumentArchiveWithNoIssuerIsIssuerNotFound: the module's own
// refusal, before any byte of a ZIP.
func TestTheTaxDocumentArchiveWithNoIssuerIsIssuerNotFound(t *testing.T) {
	env := setupTest(t)
	sid := operatorSession(t, env, "operator@example.com")

	resp, body := sriEnv.getRaw(t, taxArchiveQuery(taxArchiveFrom, taxArchiveTo), authHeader(sid))
	if refusal := decodeErrorEnvelope(t, body); resp.StatusCode != http.StatusNotFound || refusal.Error == nil || refusal.Error.Code != "ISSUER_NOT_FOUND" {
		t.Fatalf("archive with no Issuer: status=%d body=%s; want 404 ISSUER_NOT_FOUND", resp.StatusCode, body)
	}
}
