package integration

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/xuri/excelize/v2"
)

// The Tax ID at the Sale Import seam (#101, ADR 0016). Imports describe sales
// transacted elsewhere, so the Tax ID is *optional* here — a file that never
// mentions it must import exactly as it did before the columns existed. When a
// row does supply one it is held to the same shared validator as the Storefront
// checkout, pinpointed to its row in the preview before anything commits, and
// snapshotted onto the recorded Ticket Sale. Write-back follows the shared
// rules; an import is never self-asserted, so a Verified Customer's stored Tax
// ID is untouchable through this channel.
//
// Assertions are at HTTP (template download, preview, commit) plus SQL reads of
// the snapshot columns, which is how every other Customer/sale identity fact is
// checked in this package — no API surfaces them yet (#103 adds the Sales list
// columns).

// taxIDImportHeader is the template's header row including the two optional Tax
// ID columns.
const taxIDImportHeader = "customer_email,customer_first_name,customer_last_name,ticket_type,quantity,payment_method,sold_at,amount,customer_tax_id_type,customer_tax_id_number\n"

// plainImportHeader is the header row as it was before the Tax ID columns
// existed — the file an organizer downloaded last month still uploads.
const plainImportHeader = "customer_email,customer_first_name,customer_last_name,ticket_type,quantity,payment_method,sold_at,amount\n"

// importCSV assembles a Sale Import file from a header and its data rows.
func importCSV(header string, rows ...string) []byte {
	out := header
	for _, r := range rows {
		out += r + "\n"
	}
	return []byte(out)
}

// previewImportFile previews a Sale Import file and returns the decoded preview.
func previewImportFile(t *testing.T, env *testEnv, sessionID, eventID string, content []byte) previewResultBody {
	t.Helper()
	resp, body := postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports/preview",
		"sales.csv", content, nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d error=%+v", resp.StatusCode, body.Error)
	}
	return decodePreview(t, body)
}

// commitImportFileOK commits a Sale Import file, requiring it to be recorded.
func commitImportFileOK(t *testing.T, env *testEnv, sessionID, eventID, idempotencyKey string, content []byte) importResultBody {
	t.Helper()
	resp, body := postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports",
		"sales.csv", content, map[string]string{"idempotency_key": idempotencyKey, "source": "direct"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("commit status = %d error=%+v", resp.StatusCode, body.Error)
	}
	return importResult(t, body)
}

// latestSaleRef returns the Sale Confirmation reference of a buyer's
// newest-by-sold_at active Ticket Sale on an Event, so the caller can read that
// exact sale's snapshot back. Ordering is by sold_at rather than created_at
// because the harness clock is fixed: every sale in a run shares a created_at.
func latestSaleRef(t *testing.T, env *testEnv, eventID, email string) string {
	t.Helper()
	var ref string
	if err := env.db.QueryRow(`
		SELECT confirmation_ref FROM ticket_sales
		WHERE event_id = $1 AND customer_email = $2 AND status = 'active'
		ORDER BY sold_at DESC LIMIT 1
	`, eventID, email).Scan(&ref); err != nil {
		t.Fatalf("read latest sale ref for %q: %v", email, err)
	}
	return ref
}

// rowErrorFields flattens one preview row's errors into field → message, the
// shape the import preview panel renders.
func rowErrorFields(row previewRow) map[string]string {
	out := map[string]string{}
	for _, e := range row.Errors {
		out[e.Field] = e.Message
	}
	return out
}

// TestSaleImportTemplateCarriesTaxIDColumns pins the downloadable template's
// columns: the two optional Tax ID columns join the visible set, named after the
// Customer fields beside them, with the hidden ticket_type_id reference still
// last.
func TestSaleImportTemplateCarriesTaxIDColumns(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Tax Template Fest", "tax-template-fest")
	createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	req, _ := http.NewRequest(http.MethodGet, env.server.URL+"/api/v1/staff/events/"+eventID+"/sale-imports/template", nil)
	req.Header.Set("Authorization", "Bearer "+sessionID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("open .xlsx: %v", err)
	}
	defer func() { _ = f.Close() }()
	rows, err := f.GetRows("Sales")
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("template has no header row")
	}
	want := []string{
		"customer_email", "customer_first_name", "customer_last_name", "ticket_type",
		"quantity", "payment_method", "sold_at", "amount",
		"customer_tax_id_type", "customer_tax_id_number", "ticket_type_id",
	}
	if len(rows[0]) != len(want) {
		t.Fatalf("template header = %v, want %v", rows[0], want)
	}
	for i, h := range want {
		if rows[0][i] != h {
			t.Fatalf("template header[%d] = %q, want %q (full: %v)", i, rows[0][i], h, rows[0])
		}
	}
}

// TestSaleImportWithoutTaxIDImportsAsBefore is the compatibility guarantee: a
// file that omits the Tax ID columns entirely and a file that has them but
// leaves them blank both preview clean and commit, recording sales with no Tax
// ID snapshot at all.
func TestSaleImportWithoutTaxIDImportsAsBefore(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "No Tax Fest", "no-tax-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	// Columns absent entirely.
	absent := importCSV(plainImportHeader, "ana@example.com,Ana,Lopez,GA,2,cash,2026-07-01T10:00:00Z,")
	if res := previewImportFile(t, env, sessionID, eventID, absent); !res.Committable || res.ValidRows != 1 {
		t.Fatalf("preview of a file without Tax ID columns = %+v, want 1/1 committable", res)
	}
	if r := commitImportFileOK(t, env, sessionID, eventID, "tax-absent", absent); r.SaleCount != 1 {
		t.Fatalf("sale count = %d, want 1", r.SaleCount)
	}
	if got := readSaleTaxID(t, env, latestSaleRef(t, env, eventID, "ana@example.com")); !got.isNone() {
		t.Fatalf("sale tax id = %s, want none", got)
	}
	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.isNone() {
		t.Fatalf("customer tax id = %s, want none", got)
	}

	// Columns present but empty.
	empty := importCSV(taxIDImportHeader, "bob@example.com,Bob,Ng,GA,1,transfer,2026-07-02T10:00:00Z,,,")
	if res := previewImportFile(t, env, sessionID, eventID, empty); !res.Committable || res.ValidRows != 1 {
		t.Fatalf("preview of blank Tax ID columns = %+v, want 1/1 committable", res)
	}
	if r := commitImportFileOK(t, env, sessionID, eventID, "tax-empty", empty); r.SaleCount != 1 {
		t.Fatalf("sale count = %d, want 1", r.SaleCount)
	}
	if got := readSaleTaxID(t, env, latestSaleRef(t, env, eventID, "bob@example.com")); !got.isNone() {
		t.Fatalf("sale tax id with blank columns = %s, want none", got)
	}
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 3 {
		t.Fatalf("sold_count = %d, want 3", got)
	}
}

// TestSaleImportPreviewPinpointsInvalidTaxID covers the four ways a
// present-but-invalid Tax ID can be wrong. Each is a row-level error naming the
// half of the pair at fault, in the preview's existing per-row error format, and
// each blames exactly one field so the organizer knows which cell to fix.
func TestSaleImportPreviewPinpointsInvalidTaxID(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Bad Tax Fest", "bad-tax-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	content := importCSV(taxIDImportHeader,
		"ok@example.com,Ana,Lopez,GA,1,cash,2026-07-01T10:00:00Z,,cedula,"+validCedula,
		"badcheck@example.com,Bea,Ruiz,GA,1,cash,2026-07-01T10:00:00Z,,cedula,"+invalidCedula,
		"badtype@example.com,Caro,Diaz,GA,1,cash,2026-07-01T10:00:00Z,,dni,"+validCedula,
		"notype@example.com,Dan,Mora,GA,1,cash,2026-07-01T10:00:00Z,,,"+validCedula,
		"nonumber@example.com,Eva,Paz,GA,1,cash,2026-07-01T10:00:00Z,,cedula,",
	)

	res := previewImportFile(t, env, sessionID, eventID, content)
	if res.TotalRows != 5 || res.ValidRows != 1 {
		t.Fatalf("counts = %d valid / %d total, want 1/5", res.ValidRows, res.TotalRows)
	}
	if res.Committable {
		t.Fatalf("committable = true, want false with invalid Tax IDs")
	}
	if !res.Rows[0].Valid {
		t.Fatalf("the valid row was rejected: %+v", res.Rows[0])
	}

	cases := []struct {
		index int
		name  string
		field string
	}{
		{1, "bad check digit", "customer_tax_id_number"},
		{2, "unknown type", "customer_tax_id_type"},
		{3, "number without a type", "customer_tax_id_type"},
		{4, "type without a number", "customer_tax_id_number"},
	}
	for _, tc := range cases {
		row := res.Rows[tc.index]
		if row.Valid {
			t.Fatalf("%s: row %d is valid, want invalid", tc.name, row.Row)
		}
		fields := rowErrorFields(row)
		if fields[tc.field] == "" {
			t.Fatalf("%s: errors = %+v, want one on %s", tc.name, row.Errors, tc.field)
		}
		if len(fields) != 1 {
			t.Fatalf("%s: errors = %+v, want only %s blamed", tc.name, row.Errors, tc.field)
		}
	}

	// Committing the same file is refused row by row, and writes nothing.
	resp, body := postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports",
		"sales.csv", content, map[string]string{"idempotency_key": "bad-tax-1", "source": "direct"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("commit status = %d, want 400; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("error = %+v, want VALIDATION_FAILED", body.Error)
	}
	fields := fieldErrors(t, body)
	if fields["rows[3].customer_tax_id_number"] == "" {
		t.Fatalf("commit fields = %+v, want rows[3].customer_tax_id_number", fields)
	}
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 0 {
		t.Fatalf("sold_count = %d, want 0 (all-or-nothing)", got)
	}
}

// TestSaleImportSnapshotsValidTaxID walks the three Tax ID Types through the
// import seam: each committed row carries the normalised snapshot on its Ticket
// Sale, and the Customer it created is filled from it.
func TestSaleImportSnapshotsValidTaxID(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Snapshot Fest", "snapshot-fest")
	createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	content := importCSV(taxIDImportHeader,
		"cedula-buyer@example.com,Ana,Lopez,GA,1,cash,2026-07-01T10:00:00Z,,cedula,"+validCedula,
		"ruc-buyer@example.com,Bob,Ng,GA,1,cash,2026-07-01T10:00:00Z,,ruc,"+companyRUC,
		"passport-buyer@example.com,Cara,Diaz,GA,1,transfer,2026-07-01T10:00:00Z,,passport,"+lowercasePassprt,
		"padded-buyer@example.com,Dee,Paz,GA,1,cash,2026-07-01T10:00:00Z,,cedula,  "+otherCedula+"  ",
	)

	// The preview echoes back what will be stored, normalised.
	res := previewImportFile(t, env, sessionID, eventID, content)
	if !res.Committable || res.ValidRows != 4 {
		t.Fatalf("preview = %+v, want 4/4 committable", res)
	}
	if res.Rows[2].CustomerTaxIDNumber != "AB123456" {
		t.Fatalf("preview passport number = %q, want the normalised AB123456", res.Rows[2].CustomerTaxIDNumber)
	}
	if res.Rows[3].CustomerTaxIDNumber != otherCedula {
		t.Fatalf("preview padded number = %q, want the trimmed %s", res.Rows[3].CustomerTaxIDNumber, otherCedula)
	}

	if r := commitImportFileOK(t, env, sessionID, eventID, "tax-snapshot", content); r.SaleCount != 4 {
		t.Fatalf("sale count = %d, want 4", r.SaleCount)
	}

	cases := []struct {
		email      string
		taxIDType  string
		wantNumber string
	}{
		{"cedula-buyer@example.com", "cedula", validCedula},
		{"ruc-buyer@example.com", "ruc", companyRUC},
		{"passport-buyer@example.com", "passport", "AB123456"},
		{"padded-buyer@example.com", "cedula", otherCedula},
	}
	for _, tc := range cases {
		if got := readSaleTaxID(t, env, latestSaleRef(t, env, eventID, tc.email)); !got.is(tc.taxIDType, tc.wantNumber) {
			t.Fatalf("%s: sale tax id = %s, want %s:%s", tc.email, got, tc.taxIDType, tc.wantNumber)
		}
		if got := readCustomerTaxID(t, env, tc.email); !got.is(tc.taxIDType, tc.wantNumber) {
			t.Fatalf("%s: customer tax id = %s, want %s:%s", tc.email, got, tc.taxIDType, tc.wantNumber)
		}
	}
}

// TestSaleImportFillsNeverSetTaxIDOnVerifiedCustomer is the "fill" rule on the
// import channel: someone who signed in before ever buying holds a verified
// record with nothing in it, and an imported sale may supply the Tax ID rather
// than leaving the profile permanently blank.
func TestSaleImportFillsNeverSetTaxIDOnVerifiedCustomer(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Import Fill Fest", "import-fill-fest")
	createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	customerSignIn(t, env, "ana@example.com")
	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.isNone() {
		t.Fatalf("tax id after sign-in = %s, want none", got)
	}

	commitImportFileOK(t, env, sessionID, eventID, "import-fill", importCSV(taxIDImportHeader,
		"ana@example.com,Ana,Lopez,GA,1,cash,2026-07-01T10:00:00Z,,cedula,"+validCedula))

	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.is("cedula", validCedula) {
		t.Fatalf("customer tax id = %s, want the filled cedula:%s", got, validCedula)
	}
}

// TestSaleImportRefreshesUnverifiedTaxID is the "refresh" rule on the import
// channel: a record assembled on someone's behalf self-corrects until they claim
// it, while each sale keeps what it was transacted under.
func TestSaleImportRefreshesUnverifiedTaxID(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Import Refresh Fest", "import-refresh-fest")
	createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	commitImportFileOK(t, env, sessionID, eventID, "import-refresh-1", importCSV(taxIDImportHeader,
		"ana@example.com,Ana,Lopez,GA,1,cash,2026-07-01T10:00:00Z,,cedula,"+validCedula))
	firstRef := latestSaleRef(t, env, eventID, "ana@example.com")

	commitImportFileOK(t, env, sessionID, eventID, "import-refresh-2", importCSV(taxIDImportHeader,
		"ana@example.com,Ana,Lopez,GA,1,cash,2026-07-02T10:00:00Z,,ruc,"+companyRUC))
	secondRef := latestSaleRef(t, env, eventID, "ana@example.com")

	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.is("ruc", companyRUC) {
		t.Fatalf("customer tax id = %s, want the refreshed ruc:%s", got, companyRUC)
	}
	if got := readSaleTaxID(t, env, firstRef); !got.is("cedula", validCedula) {
		t.Fatalf("first sale tax id = %s, want the immutable cedula:%s", got, validCedula)
	}
	if got := readSaleTaxID(t, env, secondRef); !got.is("ruc", companyRUC) {
		t.Fatalf("second sale tax id = %s, want ruc:%s", got, companyRUC)
	}
}

// TestSaleImportNeverOverwritesVerifiedTaxID is the guard: an import is never
// self-asserted — a spreadsheet is a Member's account of what happened, not the
// buyer proving anything — so a Verified Customer's stored Tax ID stands, while
// the sale still records what the Organization will declare it under.
func TestSaleImportNeverOverwritesVerifiedTaxID(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Import Guard Fest", "import-guard-fest")
	createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	// She buys once (filling her Tax ID), then claims the record by signing in.
	commitImportFileOK(t, env, sessionID, eventID, "import-guard-1", importCSV(taxIDImportHeader,
		"ana@example.com,Ana,Lopez,GA,1,cash,2026-07-01T10:00:00Z,,cedula,"+validCedula))
	customerSignIn(t, env, "ana@example.com")

	// A later import types a different number against her address.
	commitImportFileOK(t, env, sessionID, eventID, "import-guard-2", importCSV(taxIDImportHeader,
		"ana@example.com,Ana,Lopez,GA,1,cash,2026-07-02T10:00:00Z,,cedula,"+otherCedula))

	if got := readCustomerTaxID(t, env, "ana@example.com"); !got.is("cedula", validCedula) {
		t.Fatalf("customer tax id = %s, want the person-owned cedula:%s", got, validCedula)
	}
	if got := readSaleTaxID(t, env, latestSaleRef(t, env, eventID, "ana@example.com")); !got.is("cedula", otherCedula) {
		t.Fatalf("imported sale tax id = %s, want the recorded cedula:%s", got, otherCedula)
	}
}
