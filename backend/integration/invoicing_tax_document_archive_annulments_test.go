package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing/sri"
)

// THE NEXT ARCHIVE CORRECTS THE LAST (#751, spec #742, ADR 0078; CONTEXT.md
// Tax Document Archive): the readme lists, without their XML, every
// production factura emitted in the range that is now annulled (number,
// clave, date annulled) and every one the Closing Sweep found annulled at
// the SRI before the platform recorded it (number, clave, date found). The
// pre-download summary counts both. A reinstated factura is back in the XML
// and off the list; a test-environment factura is on neither.
//
// Every factura is emitted at fixedClock (2026-07-07), so July is the range.

const (
	julyFrom = "2026-07-01"
	julyTo   = "2026-07-31"
)

// readmeListed is the line a readme list writes for one factura.
func readmeListed(number, clave, dated, day string) string {
	return "  " + number + "  " + clave + "  " + dated + " " + day + "\r\n"
}

// guayaquilDay is the Ecuador calendar day of an API timestamp.
func guayaquilDay(t *testing.T, at *string) string {
	t.Helper()
	return timeOf(t, at).In(sri.Guayaquil).Format("2006-01-02")
}

// wantArchiveXML asserts the archive's XML entries are exactly these claves.
func wantArchiveXML(t *testing.T, what string, pack downloadedPack, claves ...string) {
	t.Helper()
	var got []string
	for _, name := range pack.Names {
		if strings.HasSuffix(name, ".xml") {
			got = append(got, strings.TrimSuffix(name, ".xml"))
		}
	}
	if !equalStrings(sortedStrings(got), sortedStrings(claves)) {
		t.Fatalf("%s: the archive's XML is %v; want exactly %v", what, got, claves)
	}
}

func wantReadmeHas(t *testing.T, what string, readme []byte, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if !strings.Contains(string(readme), line) {
			t.Fatalf("%s: the readme does not say %q; it reads:\n%s", what, line, readme)
		}
	}
}

func wantReadmeLacks(t *testing.T, what string, readme []byte, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if strings.Contains(string(readme), fragment) {
			t.Fatalf("%s: the readme names %q; it reads:\n%s", what, fragment, readme)
		}
	}
}

// TestTheArchiveListsAnAnnulledFacturaWithoutItsXMLUntilItIsReinstated: an
// annulled production factura is listed in English and in Spanish with its
// number, clave and the day it was annulled, its XML is not shipped, and the
// summary counts it; a test-environment factura annulled the same day is
// listed nowhere. Once reinstated it is back in the XML and off the list.
func TestTheArchiveListsAnAnnulledFacturaWithoutItsXMLUntilItIsReinstated(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	sid := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sid)
	testEnvironment := issueOK(t, sid, validInvoiceBody())
	if testEnvironment.Environment != "test" {
		t.Fatalf("setup: %s; want a test-environment factura", testEnvironment.Environment)
	}
	annulOK(t, sid, testEnvironment.ID)
	productionIssuer(t, sid)
	annulled, annulledClave := productionManualInvoice(t, sid)
	_, keptClave := productionManualInvoice(t, sid)
	annulOK(t, sid, annulled)
	number := numberOfInvoice(t, sid, annulled)
	day := guayaquilDay(t, getReinstated(t, sid, annulled).AnnulledAt)

	pack := downloadPack(t, sriEnv, sid, taxArchiveQuery(julyFrom, julyTo))
	wantArchiveXML(t, "annulled", pack, keptClave)
	readme := pack.Files["README.txt"]
	wantReadmeHas(t, "annulled, en", readme,
		"Facturas: 1\r\n",
		"Facturas emitted in the period that are now annulled: 1\r\n",
		"Facturas emitted in the period the SRI reports annulled, not recorded on the platform: 0\r\n",
		"Annulled facturas (their XML is not in this archive; correct anything booked from an earlier one):\r\n"+
			readmeListed(number, annulledClave, "annulled on", day))
	wantReadmeLacks(t, "annulled, en", readme, testEnvironment.Ecuador.AccessKey, "Unrecorded Annulments:")
	if got := fetchTaxArchiveSummary(t, sid, julyFrom, julyTo); got != (taxArchiveSummaryView{Facturas: 1, Annulled: 1}) {
		t.Fatalf("summary = %+v; want one factura and one annulled", got)
	}

	setStaffLocale(t, env, sid, "es")
	leeme := downloadPack(t, sriEnv, sid, taxArchiveQuery(julyFrom, julyTo)).Files["LEEME.txt"]
	wantReadmeHas(t, "annulled, es", leeme,
		"Facturas emitidas en el periodo que hoy están anuladas: 1\r\n",
		"Facturas emitidas en el periodo que el SRI reporta anuladas sin registro en la plataforma: 0\r\n",
		"Facturas anuladas (su XML no está en este archivo; corrija lo registrado a partir de un archivo anterior):\r\n"+
			readmeListed(number, annulledClave, "anulada el", day))
	wantReadmeLacks(t, "annulled, es", leeme, testEnvironment.Ecuador.AccessKey)
	setStaffLocale(t, env, sid, "en")

	checkToMismatch(t, sid, annulled)
	atInvoicingClock(t, afterTheWindow)
	if view := reinstateOK(t, sid, annulled, "the SRI still reports it authorized"); view.Status != "authorized" {
		t.Fatalf("setup: reinstate left the factura %s", view.Status)
	}

	pack = downloadPack(t, sriEnv, sid, taxArchiveQuery(julyFrom, julyTo))
	wantArchiveXML(t, "reinstated", pack, keptClave, annulledClave)
	if got := pack.Files[annulledClave+".xml"]; string(got) != string(storedSignedXML(t, sid, annulled)) {
		t.Fatal("the reinstated factura's XML is not the stored signed XML")
	}
	readme = pack.Files["README.txt"]
	wantReadmeHas(t, "reinstated", readme, "Facturas: 2\r\n", "Facturas emitted in the period that are now annulled: 0\r\n")
	wantReadmeLacks(t, "reinstated", readme, "Annulled facturas (", "annulled on")
	if got := fetchTaxArchiveSummary(t, sid, julyFrom, julyTo); got != (taxArchiveSummaryView{Facturas: 2}) {
		t.Fatalf("summary after Reinstate = %+v; want two facturas and nothing annulled", got)
	}
}

// TestTheArchiveListsAnUnrecordedAnnulmentWithTheDayItWasFound: a factura
// the Closing Sweep found annulled at the SRI is still authorized on the
// platform, but the archive lists it with the day found and ships no XML for
// it; a flagged test-environment factura (no sweep asks about one, so it is
// seeded) is listed nowhere. Once Mark annulled records it, it moves to the
// annulled list.
func TestTheArchiveListsAnUnrecordedAnnulmentWithTheDayItWasFound(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	sid := operatorSession(t, env, "operator@example.com")
	issuerReady(t, sid)
	testEnvironment := issueOK(t, sid, validInvoiceBody())
	if _, err := sriEnv.db.Exec(`UPDATE invoicing_invoices SET unrecorded_annulment_found_at = $2 WHERE id = $1`,
		testEnvironment.ID, gye(time.August, 8, 0, 0)); err != nil {
		t.Fatalf("setup: flag the test-environment factura: %v", err)
	}
	productionIssuer(t, sid)
	flagged, flaggedClave := productionManualInvoice(t, sid)
	_, keptClave := productionManualInvoice(t, sid)
	sriStub.setConsulta(consultaBy(map[string]func(string) (int, string){flaggedClave: consultaAnnulled}, consultaAuthorized))
	if result := sweepTickAt(t, gye(time.August, 8, 0, 0)); result.ClosingSweep.Annulled != 1 {
		t.Fatalf("closing_sweep = %+v; want the factura found annulled", result.ClosingSweep)
	}
	detail := getSweepDetail(t, sid, flagged)
	if detail.Status != "authorized" || detail.UnrecordedAnnulment == nil {
		t.Fatalf("setup: detail = %+v; want authorized and flagged", detail)
	}
	number := numberOfInvoice(t, sid, flagged)
	found := guayaquilDay(t, &detail.UnrecordedAnnulment.FoundAt)
	if found != "2026-08-08" {
		t.Fatalf("setup: found on %s; want 2026-08-08", found)
	}

	pack := downloadPack(t, sriEnv, sid, taxArchiveQuery(julyFrom, julyTo))
	wantArchiveXML(t, "flagged", pack, keptClave)
	readme := pack.Files["README.txt"]
	wantReadmeHas(t, "flagged, en", readme,
		"Facturas: 1\r\n",
		"Facturas emitted in the period that are now annulled: 0\r\n",
		"Facturas emitted in the period the SRI reports annulled, not recorded on the platform: 1\r\n",
		"their annulment yet (their XML is not in this archive):\r\n"+readmeListed(number, flaggedClave, "found on", found))
	wantReadmeLacks(t, "flagged, en", readme, testEnvironment.Ecuador.AccessKey, "Annulled facturas (")
	if got := fetchTaxArchiveSummary(t, sid, julyFrom, julyTo); got != (taxArchiveSummaryView{Facturas: 1, Unrecorded: 1}) {
		t.Fatalf("summary = %+v; want one factura and one unrecorded", got)
	}

	setStaffLocale(t, env, sid, "es")
	leeme := downloadPack(t, sriEnv, sid, taxArchiveQuery(julyFrom, julyTo)).Files["LEEME.txt"]
	wantReadmeHas(t, "flagged, es", leeme,
		"Facturas emitidas en el periodo que el SRI reporta anuladas sin registro en la plataforma: 1\r\n",
		"su anulación (su XML no está en este archivo):\r\n"+readmeListed(number, flaggedClave, "detectada el", found))
	wantReadmeLacks(t, "flagged, es", leeme, testEnvironment.Ecuador.AccessKey)
	setStaffLocale(t, env, sid, "en")

	recorded := gye(time.August, 10, 9, 0)
	atInvoicingClock(t, recorded)
	annulOK(t, sid, flagged)
	pack = downloadPack(t, sriEnv, sid, taxArchiveQuery(julyFrom, julyTo))
	wantArchiveXML(t, "recorded", pack, keptClave)
	readme = pack.Files["README.txt"]
	wantReadmeHas(t, "recorded", readme,
		"Facturas emitted in the period the SRI reports annulled, not recorded on the platform: 0\r\n",
		readmeListed(number, flaggedClave, "annulled on", "2026-08-10"))
	wantReadmeLacks(t, "recorded", readme, "Unrecorded Annulments:")
	if got := fetchTaxArchiveSummary(t, sid, julyFrom, julyTo); got != (taxArchiveSummaryView{Facturas: 1, Annulled: 1}) {
		t.Fatalf("summary once recorded = %+v; want one factura and one annulled", got)
	}
}
