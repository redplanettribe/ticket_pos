package integration

import (
	"net/http"
	"testing"
)

// THE SALES EXPORT'S ORIGIN COLUMN (#373, parent #366, ADR 0052): an accountant
// reading the file can tell how each Ticket Sale reached the platform without
// opening the platform — the Sale Import batch it arrived in, a Manually
// Recorded Sale somebody typed, or a Sale Correction's replacement.
//
// The file is the surface that gets forwarded and kept, and it is read by
// somebody who never saw the Sales list. Until this column, the only provenance
// in it was `channel`, which says `import` for all three of those routes and so
// answers none of them: a row nobody recognises could not be accounted for from
// the file alone.
//
// The column states the SAME derivation the Sales list states (#370), read from
// sales.DeriveSaleOrigin — the one place the Manually Recorded Sale's three-way
// negative predicate is spelled out. These tests are the export's own, at the
// same seam as every other Sales Export test: the workbook is fetched over HTTP
// and read back cell by cell, by header and never by column letter.

// exportOriginsByEmail downloads an export under a status filter and maps each
// row's buyer to the origin the file states for it — the whole of what these
// tests assert, and deliberately read by header so an added column shifts
// nothing.
func exportOriginsByEmail(t *testing.T, env *testEnv, sessionID, eventID, query string) map[string]string {
	t.Helper()
	resp, data := downloadSalesExport(t, env, sessionID, eventID, query)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export %q status=%d body=%s", query, resp.StatusCode, string(data))
	}
	sheet := openSalesExport(t, data)
	out := map[string]string{}
	for row := 0; row < sheet.dataRows; row++ {
		out[sheet.value(t, row, "customer_email")] = sheet.value(t, row, "origin")
	}
	return out
}

// EVERY ROUTE INTO THE PLATFORM NAMES ITSELF IN THE FILE, and the two batchless
// ones are told apart. One Event carrying one of each: a sale from an uploaded
// Sale Import batch, a sale typed by hand, a Sale Correction's replacement, and
// an Online Sale nobody imported.
//
// The assertion that matters most is the same one the Sales list's own test
// defends: the OTHER batchless imported sale — a correction's replacement — is
// not called hand-typed. The two are distinguishable in the file or the column
// is worse than nothing, because an accountant would trust it.
func TestSalesExportStatesHowEachSaleReachedThePlatform(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Origin Export Fest", "origin-export-fest", 1000, 100)

	// The file: two sales in one uploaded Sale Import batch.
	commitBatch(t, env, sessionID, eventID, "the-file", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "eve@example.com", "customer_first_name": "Eve", "customer_last_name": "Ruiz", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T11:00:00Z"},
	})
	eve := saleRowByEmail(t, env, sessionID, eventID, "eve@example.com", "active")

	// The form: one Manually Recorded Sale, belonging to no batch and standing
	// in for nothing.
	recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("bob@example.com", "Bob", "Ng", gaID, 1, "cash", "2026-07-02T10:00:00Z"))

	// The correction: Eve's sale reversed and replaced under a fixed email. The
	// replacement is ALSO an imported sale with no batch — the one shape that
	// could be mistaken for a hand-typed one.
	correctImportedSaleOK(t, env, sessionID, eventID, eve.ID,
		correctionBody("cara@example.com", "Cara", "Diaz", gaID, 1, "cash", "2026-07-01T11:00:00Z"))

	// The Sales Channel: an Online Sale, which no import ever touched.
	approved := beginCheckoutOK(t, env, "test-org", "origin-export-fest",
		checkoutBody("dora@example.com", "Dora", "Vega", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if confirm := confirmCheckoutOK(t, env, approved.ClientTransactionID, "approved"); confirm.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirm.Status)
	}

	origins := exportOriginsByEmail(t, env, sessionID, eventID, "")
	want := map[string]string{
		"ana@example.com":  "sale_import",
		"bob@example.com":  "manually_recorded",
		"cara@example.com": "correction_replacement",
		"dora@example.com": "channel_sale",
	}
	for email, origin := range want {
		if origins[email] != origin {
			t.Errorf("%s origin = %q, want %q", email, origins[email], origin)
		}
	}
	if len(origins) != len(want) {
		t.Errorf("export rows = %v, want exactly the four seeded sales", origins)
	}
}

// THE COLUMN IS NEVER BLANK, and that is a decision rather than an accident.
//
// The rest of this file's optional cells are blank when the figure or the event
// does not apply — no Net Proceeds, no Sale Reversal, no correction linkage. An
// origin is not like that: every Ticket Sale reached the platform somehow, and
// the derivation is TOTAL. A sale that came in on a Sales Channel of its own
// says so (`channel_sale`) rather than leaving a hole an accountant would have
// to explain, which is also what makes "group by origin" account for every row
// in the file.
func TestSalesExportOriginIsStatedOnEveryRow(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Total Origin Fest", "total-origin-fest", 1000, 100)
	commitBatch(t, env, sessionID, eventID, "total-origin-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	approved := beginCheckoutOK(t, env, "test-org", "total-origin-fest",
		checkoutBody("dora@example.com", "Dora", "Vega", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if confirm := confirmCheckoutOK(t, env, approved.ClientTransactionID, "approved"); confirm.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirm.Status)
	}

	resp, data := downloadSalesExport(t, env, sessionID, eventID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}
	sheet := openSalesExport(t, data)
	for row := 0; row < sheet.dataRows; row++ {
		if got := sheet.raw(t, row, "origin"); got == "" {
			t.Errorf("origin on row %d (%s) is blank — every sale reached the platform somehow",
				row, sheet.value(t, row, "customer_email"))
		}
	}
	if got := sheet.value(t, sheet.rowOf(t, "dora@example.com"), "origin"); got != "channel_sale" {
		t.Errorf("the Online Sale's origin = %q, want channel_sale — it sold where it says it sold", got)
	}
}

// THE COLUMN IS ADDITIVE: it sits with the sale's other provenance facts, and
// the columns that were already there keep their places relative to each other.
//
// `origin` follows `channel` and `source` because it is what those two cannot
// say: `channel` reads `import` for all three import routes. The reversal pair
// and the Sale Correction linkage stay at the very end of the row, after the
// status they elaborate — a reader scanning left to right still reaches the
// whole of the sale before reaching the columns that only speak when it was
// undone.
func TestSalesExportOriginSitsWithTheProvenanceColumnsAndMovesNothing(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Layout Origin Fest", "layout-origin-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)
	commitBatch(t, env, sessionID, eventID, "layout-origin-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})

	resp, data := downloadSalesExport(t, env, sessionID, eventID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}
	sheet := openSalesExport(t, data)

	origin, ok := sheet.index["origin"]
	if !ok {
		t.Fatalf("no origin column in header %v", sheet.header)
	}
	if got := sheet.header[origin-2 : origin+2]; !equalStrings(got, []string{"channel", "source", "origin", "payment_method"}) {
		t.Fatalf("columns around origin = %v, want channel, source, origin, payment_method", got)
	}

	tail := sheet.header[len(sheet.header)-5:]
	if !equalStrings(tail, []string{"status", "reversed_at", "reversed_by", "corrected_by", "corrects"}) {
		t.Fatalf("header tail = %v, want the status, reversal pair and correction linkage still last", tail)
	}
}

// A SALE'S ORIGIN IS WHERE IT CAME FROM, NOT WHAT LATER HAPPENED TO IT — and
// the columns that DO say what happened to it are untouched by its arrival.
//
// Read off the reversed file, the only one that reaches these rows: the
// corrected original still arrived in its uploaded file, and it still names its
// replacement in `corrected_by` and its route in `reversed_by`. A hand-typed
// sale reversed on its own was still typed.
func TestSalesExportOriginSurvivesAReversalAndLeavesTheLinkageAlone(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Undone Export Fest", "undone-export-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	commitBatch(t, env, sessionID, eventID, "undone-export-batch", []map[string]any{
		{"customer_email": "eve@example.com", "customer_first_name": "Eve", "customer_last_name": "Ruiz", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	eve := saleRowByEmail(t, env, sessionID, eventID, "eve@example.com", "active")
	correctImportedSaleOK(t, env, sessionID, eventID, eve.ID,
		correctionBody("eva@example.com", "Eva", "Ruiz", gaID, 1, "cash", "2026-07-01T10:00:00Z"))

	typo := recordManualSaleOK(t, env, sessionID, eventID,
		manualSaleBody("bob@example.com", "Bob", "Ng", gaID, 1, "cash", "2026-07-02T10:00:00Z"))
	reverseImportedSaleOK(t, env, sessionID, eventID, typo.SaleID)

	sheet := reversedSalesExport(t, env, sessionID, eventID)

	corrected := sheet.rowOf(t, "eve@example.com")
	if got := sheet.value(t, corrected, "origin"); got != "sale_import" {
		t.Errorf("the corrected original's origin = %q, want sale_import — being replaced is not a route in", got)
	}
	// The correction linkage and the reversal route say exactly what they said
	// before this column existed.
	if got := sheet.value(t, corrected, "reversed_by"); got != "correction" {
		t.Errorf("the corrected original's reversed_by = %q, want correction", got)
	}
	if got := sheet.value(t, corrected, "corrected_by"); got == "" {
		t.Error("the corrected original's corrected_by is blank, want the replacement's confirmation reference")
	}

	typed := sheet.rowOf(t, "bob@example.com")
	if got := sheet.value(t, typed, "origin"); got != "manually_recorded" {
		t.Errorf("the reversed hand-typed sale's origin = %q, want manually_recorded — it was still typed", got)
	}
	if got := sheet.value(t, typed, "reversed_by"); got != "staff_reversal" {
		t.Errorf("the reversed hand-typed sale's reversed_by = %q, want staff_reversal", got)
	}
	sheet.blank(t, typed, "corrected_by")
	sheet.blank(t, typed, "corrects")
}

// THE INFO SHEET STILL DESCRIBES THE FILE. It is the file explaining itself to
// somebody who did not download it, and a column of tokens nobody has seen
// before is exactly the kind of thing it exists to explain: `manually_recorded`
// and `correction_replacement` are the platform's vocabulary, not an
// accountant's, and there is no other surface in the workbook that can define
// them.
func TestSalesExportInfoSheetExplainsTheOriginColumn(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Origin Info Fest", "origin-info-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)
	commitBatch(t, env, sessionID, eventID, "origin-info-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})

	resp, data := downloadSalesExport(t, env, sessionID, eventID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}
	info := openSalesExportInfo(t, data)

	// Every value the column can hold is named, and named beside what it means:
	// a reader who meets `correction_replacement` in a cell must be able to find
	// out what it is without asking the Organization that sent them the file.
	info.says(t, "origin", "how the sale reached")
	info.says(t, "sale_import", "uploaded")
	info.says(t, "manually_recorded", "typed")
	info.says(t, "correction_replacement", "stands in for")
	info.says(t, "channel_sale", "not imported")
}
