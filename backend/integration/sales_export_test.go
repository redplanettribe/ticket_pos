package integration

import (
	"bytes"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// The Sales Export (#236): an Org Admin or Event Owner presses Download on the
// Sales list and receives an .xlsx of exactly the Ticket Sales on screen. The
// seam is this one — the file is fetched over HTTP and read back with
// excelize.OpenReader, exactly as TestSaleImportTemplateDownload does — because
// what matters is what the person who downloads it observes: the status, the
// content type, the filename, and the values in named cells on a named sheet.
//
// Nothing here reaches for a column letter. Cells are resolved through the
// header row, so the columns a later ticket adds (Net Proceeds, the per-Ticket-
// Type quantities, the reversal pair) shift nothing below.

// salesExportSheet is the data sheet's name — deliberately not "Sales", so an
// export uploaded as a Sale Import cannot be parsed as one.
const salesExportSheet = "Ticket Sales"

// salesExportSpreadsheetType is the .xlsx content type the download carries.
const salesExportSpreadsheetType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// downloadSalesExport GETs the Sales Export with the given raw query string
// (the Sales list's own filter parameters), returning the response and its body.
func downloadSalesExport(t *testing.T, env *testEnv, sessionID, eventID, query string) (*http.Response, []byte) {
	t.Helper()
	url := env.server.URL + "/api/v1/staff/events/" + eventID + "/sales/export"
	if query != "" {
		url += "?" + query
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if sessionID != "" {
		req.Header.Set("Authorization", "Bearer "+sessionID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, buf.Bytes()
}

// exportSheet is a downloaded workbook's data sheet, indexed by its header row.
// Every accessor takes a column header rather than a letter, which is what keeps
// these assertions stable as columns are added.
type exportSheet struct {
	f *excelize.File
	// header is the first row's values, in order.
	header []string
	// index maps a column header to its zero-based position.
	index map[string]int
	// dataRows is how many rows follow the header.
	dataRows int
}

// openSalesExport opens a downloaded export and indexes its data sheet.
func openSalesExport(t *testing.T, data []byte) *exportSheet {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open .xlsx: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	rows, err := f.GetRows(salesExportSheet)
	if err != nil {
		t.Fatalf("get rows from %q: %v", salesExportSheet, err)
	}
	if len(rows) == 0 {
		t.Fatalf("export has no header row")
	}
	index := map[string]int{}
	for i, h := range rows[0] {
		index[h] = i
	}
	return &exportSheet{f: f, header: rows[0], index: index, dataRows: len(rows) - 1}
}

// ref resolves a zero-based data row and a column header to a cell reference.
func (s *exportSheet) ref(t *testing.T, row int, header string) string {
	t.Helper()
	col, ok := s.index[header]
	if !ok {
		t.Fatalf("no %q column in header %v", header, s.header)
	}
	// +1 for the 1-based column, +2 for the 1-based row under the header.
	ref, err := excelize.CoordinatesToCellName(col+1, row+2)
	if err != nil {
		t.Fatalf("cell name: %v", err)
	}
	return ref
}

// value returns a cell as a reader of the workbook sees it — the number format
// applied, so a date cell reads back as the date it renders.
func (s *exportSheet) value(t *testing.T, row int, header string) string {
	t.Helper()
	v, err := s.f.GetCellValue(salesExportSheet, s.ref(t, row, header))
	if err != nil {
		t.Fatalf("get %s on row %d: %v", header, row, err)
	}
	return v
}

// raw returns a cell's stored value, unformatted. A number cell stores a number;
// a text cell stores its text — which is how "25.00, not '$25.00'" is asserted.
func (s *exportSheet) raw(t *testing.T, row int, header string) string {
	t.Helper()
	v, err := s.f.GetCellValue(salesExportSheet, s.ref(t, row, header), excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatalf("get raw %s on row %d: %v", header, row, err)
	}
	return v
}

// blank asserts a cell is empty — not zero, not a placeholder, nothing at all.
//
// This is the assertion the whole absent-versus-zero rule rests on, and it is
// deliberately not `== 0`: a zero in a money column is a claim (the platform
// took nothing, this sale earned nothing) and it SUMs, while a blank says the
// figure does not apply to this row. The check is on the RAW cell, because a
// formatted read of an empty cell and a formatted read of a 0 under "0.00" are
// not the same thing and only the raw value can tell them apart.
func (s *exportSheet) blank(t *testing.T, row int, header string) {
	t.Helper()
	if got := s.raw(t, row, header); got != "" {
		t.Fatalf("%s on row %d = %q, want a blank cell — a zero would assert a figure that does not apply", header, row, got)
	}
}

// number reads a cell as the number it stores, failing if it holds text or
// nothing. Money cells are read this way so "25.00, not '$25.00'" is asserted
// rather than assumed.
func (s *exportSheet) number(t *testing.T, row int, header string) float64 {
	t.Helper()
	raw := s.raw(t, row, header)
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		t.Fatalf("%s on row %d = %q, want a number", header, row, raw)
	}
	return v
}

// column returns every data row's value in a column, sorted, for set assertions.
func (s *exportSheet) column(t *testing.T, header string) []string {
	t.Helper()
	out := make([]string, 0, s.dataRows)
	for row := 0; row < s.dataRows; row++ {
		out = append(out, s.value(t, row, header))
	}
	sort.Strings(out)
	return out
}

// rowOf finds the data row for a customer email, so a test can name the sale it
// means rather than counting rows.
func (s *exportSheet) rowOf(t *testing.T, email string) int {
	t.Helper()
	for row := 0; row < s.dataRows; row++ {
		if s.value(t, row, "customer_email") == email {
			return row
		}
	}
	t.Fatalf("no row for %s in %v", email, s.column(t, "customer_email"))
	return -1
}

// exportEmails downloads an export under a query and returns its customer
// emails, sorted — the shape the Sales list's own emails are compared against.
func exportEmails(t *testing.T, env *testEnv, sessionID, eventID, query string) []string {
	t.Helper()
	resp, data := downloadSalesExport(t, env, sessionID, eventID, query)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export %q status=%d", query, resp.StatusCode)
	}
	return openSalesExport(t, data).column(t, "customer_email")
}

// listEmails runs the equivalent Sales list call and returns its customer
// emails, sorted. The export must agree with it filter for filter.
func listEmails(t *testing.T, env *testEnv, sessionID, eventID, query string) []string {
	t.Helper()
	path := "/api/v1/staff/events/" + eventID + "/sales"
	if query != "" {
		path += "?" + query
	}
	resp, body := env.get(t, path, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sales list %q status=%d error=%+v", query, resp.StatusCode, body.Error)
	}
	return listSalesEmails(salesList(t, body))
}

// TestSalesExportAccess: the file is for the Org Admin and the Event Owner. It
// concentrates every buyer's email and Tax ID for an Event into something that
// gets forwarded and kept, so it takes the Sales summary's owner-only guard
// rather than the Sales list's looser one — Event Staff keep the screen and get
// no file.
func TestSalesExportAccess(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Export Fest", "export-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)
	commitBatch(t, env, sessionID, eventID, "export-access-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})

	resp, data := downloadSalesExport(t, env, sessionID, eventID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("org admin export status=%d body=%s", resp.StatusCode, string(data))
	}
	if ct := resp.Header.Get("Content-Type"); ct != salesExportSpreadsheetType {
		t.Fatalf("content-type = %q, want the spreadsheet type", ct)
	}

	// The filename is decided by the backend so it is decided in one place: the
	// Event's slug and the date, in a Downloads folder that stays navigable.
	disposition := resp.Header.Get("Content-Disposition")
	if !strings.Contains(disposition, "export-fest") {
		t.Fatalf("content-disposition = %q, want the Event slug", disposition)
	}
	if !regexp.MustCompile(`\d{4}-\d{2}-\d{2}\.xlsx`).MatchString(disposition) {
		t.Fatalf("content-disposition = %q, want a YYYY-MM-DD dated .xlsx filename", disposition)
	}

	addMember := func(email, role string) string {
		t.Helper()
		resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
			"email": email,
			"role":  role,
		}, authHeader(sessionID))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("add %s status=%d error=%+v", role, resp.StatusCode, body.Error)
		}
		return verifyOTP(t, env, email)
	}

	ownerSessionID := addMember("owner@example.com", "event_owner")
	if resp, data := downloadSalesExport(t, env, ownerSessionID, eventID, ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("event owner export status=%d body=%s", resp.StatusCode, string(data))
	}

	staffSessionID := addMember("doorstaff@example.com", "event_staff")
	if resp, _ := downloadSalesExport(t, env, staffSessionID, eventID, ""); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("event staff export status=%d, want 403", resp.StatusCode)
	}
	// Their Sales list is untouched by the refusal.
	if resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales", authHeader(staffSessionID)); resp.StatusCode != http.StatusOK {
		t.Fatalf("event staff sales list status=%d error=%+v", resp.StatusCode, body.Error)
	}

	if resp, _ := downloadSalesExport(t, env, "", eventID, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated export status=%d, want 401", resp.StatusCode)
	}
}

// TestSalesExportWorkbookShape: the sheet the rows live on, the columns it
// carries and the order they carry them in.
func TestSalesExportWorkbookShape(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Shape Fest", "shape-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)
	commitBatch(t, env, sessionID, eventID, "shape-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})

	resp, data := downloadSalesExport(t, env, sessionID, eventID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open .xlsx: %v", err)
	}
	defer func() { _ = f.Close() }()

	// The data sheet is "Ticket Sales" and there is no sheet called "Sales".
	// That is a safety catch, not a naming preference: the Sale Import parser
	// picks its sheet by that name, so an export uploaded as an import must not
	// be able to look like one.
	sheets := f.GetSheetList()
	found := false
	for _, name := range sheets {
		if name == salesExportSheet {
			found = true
		}
		if name == "Sales" {
			t.Fatalf("workbook has a sheet named \"Sales\": %v — an export must never look like a Sale Import", sheets)
		}
	}
	if !found {
		t.Fatalf("sheets = %v, want one named %q", sheets, salesExportSheet)
	}

	rows, err := f.GetRows(salesExportSheet)
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	want := []string{
		"confirmation_ref", "sold_at",
		"customer_first_name", "customer_last_name", "customer_email",
		"tax_id_type", "tax_id_number",
		"amount", "net_proceeds", "currency",
		"channel", "source", "payment_method", "status",
	}
	if len(rows) < 1 || !equalStrings(rows[0], want) {
		t.Fatalf("header = %v, want %v", rows[0], want)
	}
	// Nothing sits above the header, so select-all, autofilter and a pivot
	// source range all work without deleting a preamble first.
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want a header and one sale", len(rows))
	}
}

// TestSalesExportCellsAreTyped: what should be arithmetic is a number, and what
// should be a date is a date. A file whose money is text sums to nothing, and
// one whose money is in cents sums to a hundred times too much.
func TestSalesExportCellsAreTyped(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Typed Fest", "typed-fest")
	// The Event's own timezone, five hours behind UTC: the sale below is made
	// at 02:00 UTC, which is the previous evening where the Event happens.
	setEventTimezone(t, env, eventID, "America/Guayaquil")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)
	amount := 2500
	commitBatch(t, env, sessionID, eventID, "typed-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T02:00:00Z", "amount_cents": amount},
	})

	resp, data := downloadSalesExport(t, env, sessionID, eventID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}
	sheet := openSalesExport(t, data)
	row := sheet.rowOf(t, "ana@example.com")

	// amount is a number in major units: 25.00, never 2500, never "$25.00".
	rawAmount := sheet.raw(t, row, "amount")
	parsed, err := strconv.ParseFloat(rawAmount, 64)
	if err != nil {
		t.Fatalf("amount = %q, want a number in major units", rawAmount)
	}
	if parsed != 25 {
		t.Fatalf("amount = %v, want 25 (major units, not %d cents)", parsed, amount)
	}
	if sheet.value(t, row, "currency") != "USD" {
		t.Fatalf("currency = %q, want USD in its own column", sheet.value(t, row, "currency"))
	}

	// sold_at is a real date cell: its stored value is an Excel serial number,
	// and it renders in the Event's timezone — 2026-07-02T02:00Z is the evening
	// of the 1st in Guayaquil, and the file must not disagree with the sold-at
	// range that selected the row.
	rawSold := sheet.raw(t, row, "sold_at")
	if _, err := strconv.ParseFloat(rawSold, 64); err != nil {
		t.Fatalf("sold_at = %q, want a date cell (an Excel serial), not text", rawSold)
	}
	if got := sheet.value(t, row, "sold_at"); got != "2026-07-01 21:00" {
		t.Fatalf("sold_at renders %q, want 2026-07-01 21:00 in the Event's timezone", got)
	}
}

// TestSalesExportCarriesTheTaxIDPair: the pair is the feature's motivation. A
// Tax ID is mandatory to record a Ticket Sale on the native Sales Channels
// expressly so the buyer can be invoiced, and until now there was no way to read
// it back. A sale that never collected one is blank rather than filled with a
// placeholder, so the sales that cannot be invoiced are visible at a glance.
func TestSalesExportCarriesTheTaxIDPair(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Tax Export Fest", "tax-export-fest", 1000, 10)

	begun := beginCheckoutOK(t, env, "test-org", "tax-export-fest",
		taxIDCheckoutBody("online@example.com", "Olga", "Nieto", "ruc", naturalRUC,
			map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if confirmed := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved"); confirmed.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirmed.Status)
	}

	commitBatch(t, env, sessionID, eventID, "tax-export-batch", []map[string]any{
		{"customer_email": "legacy@example.com", "customer_first_name": "Leo", "customer_last_name": "Vera", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})

	resp, data := downloadSalesExport(t, env, sessionID, eventID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}
	sheet := openSalesExport(t, data)

	online := sheet.rowOf(t, "online@example.com")
	if got := sheet.value(t, online, "tax_id_type"); got != "ruc" {
		t.Fatalf("online tax_id_type = %q, want ruc", got)
	}
	if got := sheet.value(t, online, "tax_id_number"); got != naturalRUC {
		t.Fatalf("online tax_id_number = %q, want %s", got, naturalRUC)
	}
	if got := sheet.value(t, online, "channel"); got != "online" {
		t.Fatalf("online channel = %q, want online", got)
	}

	// The imported sale carried none, so both halves are blank — not "none",
	// not "-", and not an inherited value from the row above.
	legacy := sheet.rowOf(t, "legacy@example.com")
	if got := sheet.value(t, legacy, "tax_id_type"); got != "" {
		t.Fatalf("imported tax_id_type = %q, want blank", got)
	}
	if got := sheet.value(t, legacy, "tax_id_number"); got != "" {
		t.Fatalf("imported tax_id_number = %q, want blank", got)
	}
	// It still carries everything the sale does have.
	if got := sheet.value(t, legacy, "payment_method"); got != "cash" {
		t.Fatalf("imported payment_method = %q, want cash", got)
	}
	if got := sheet.value(t, legacy, "source"); got != "direct" {
		t.Fatalf("imported source = %q, want direct", got)
	}
	if sheet.value(t, legacy, "confirmation_ref") == "" {
		t.Fatalf("imported confirmation_ref is blank; it is the sale's identity")
	}
}

// TestSalesExportMirrorsSalesListFilters: the export takes the same query
// parameters as the Sales list and runs them through the same parsing, so what
// was asked for on screen and what arrived in the file cannot disagree.
func TestSalesExportMirrorsSalesListFilters(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Filter Export Fest", "filter-export-fest", 1000, 50)
	setEventTimezone(t, env, eventID, "America/Guayaquil")
	vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 5000, 50)

	// An Online Sale on the native channel...
	begun := beginCheckoutOK(t, env, "test-org", "filter-export-fest",
		taxIDCheckoutBody("online@example.com", "Olga", "Nieto", "ruc", naturalRUC,
			map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if confirmed := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved"); confirmed.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirmed.Status)
	}
	// ...and imported sales spread across Ticket Types and days.
	commitBatch(t, env, sessionID, eventID, "filter-export-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T15:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": vipID, "quantity": 1, "payment_method": "transfer", "sold_at": "2026-07-05T15:00:00Z"},
	})

	for _, query := range []string{
		"channel=import",
		"channel=online",
		"ticket_type_id=" + vipID,
		"ticket_type_id=" + gaID,
		"sold_from=2026-07-01&sold_to=2026-07-01",
		"sold_from=2026-07-02&sold_to=2026-07-31",
		"payment_method=transfer",
		"source=direct",
		"q=bob@",
	} {
		want := listEmails(t, env, sessionID, eventID, query)
		got := exportEmails(t, env, sessionID, eventID, query)
		if !equalStrings(got, want) {
			t.Fatalf("export under %q = %v, list = %v", query, got, want)
		}
		if len(want) == 0 {
			t.Fatalf("filter %q matched nothing on the list; the comparison proves nothing", query)
		}
	}

	// A filter value the list rejects, the export rejects the same way — the
	// same helper judges both, so they cannot drift.
	resp, data := downloadSalesExport(t, env, sessionID, eventID, "channel=carrier-pigeon")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid channel status=%d, want 400; body=%s", resp.StatusCode, string(data))
	}
	if !strings.Contains(string(data), "VALIDATION_FAILED") {
		t.Fatalf("invalid channel body = %s, want the standard validation envelope", string(data))
	}
}

// TestSalesExportStatusFilterMatchesTheList: the default file is the default
// screen — active sales only — and the same status lever reaches the reversed
// ones in the file that reaches them on screen.
func TestSalesExportStatusFilterMatchesTheList(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Status Export Fest", "status-export-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)

	commitBatch(t, env, sessionID, eventID, "kept-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	undoneBatch := commitBatch(t, env, sessionID, eventID, "undone-batch", []map[string]any{
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
	})
	undoBatch(t, env, sessionID, eventID, undoneBatch)

	if got := exportEmails(t, env, sessionID, eventID, ""); !equalStrings(got, []string{"ana@example.com"}) {
		t.Fatalf("default export = %v, want the active sale only", got)
	}
	if got := exportEmails(t, env, sessionID, eventID, "status=reversed"); !equalStrings(got, []string{"bob@example.com"}) {
		t.Fatalf("status=reversed export = %v, want the reversed sale", got)
	}

	// And the status a row carries is stated on the row itself.
	_, data := downloadSalesExport(t, env, sessionID, eventID, "status=reversed")
	sheet := openSalesExport(t, data)
	if got := sheet.value(t, sheet.rowOf(t, "bob@example.com"), "status"); got != "reversed" {
		t.Fatalf("reversed row status = %q, want reversed", got)
	}
}

// TestSalesExportEmptyEventHasHeadersOnly: "no sales yet" is an answer, not an
// error. The file arrives with its header row and nothing under it.
func TestSalesExportEmptyEventHasHeadersOnly(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Quiet Fest", "quiet-fest")
	createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)

	resp, data := downloadSalesExport(t, env, sessionID, eventID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}
	sheet := openSalesExport(t, data)
	if sheet.dataRows != 0 {
		t.Fatalf("data rows = %d, want none", sheet.dataRows)
	}
	if len(sheet.header) == 0 || sheet.header[0] != "confirmation_ref" {
		t.Fatalf("header = %v, want the full header row", sheet.header)
	}
}

// TestSalesExportIsRejectedAsASaleImport: the round trip is actively prevented.
// Uploading an export back as a Sale Import would insert every sale a second
// time and re-email every buyer, so the file must fail the importer immediately
// rather than parse.
//
// The assertion is on the refusal, not on the reason for it, because the reason
// changes as the file grows: today the export is a single-sheet workbook, and
// the parser falls back to the sole sheet of one of those, so the refusal comes
// from the export's columns being nothing like an import's. Once the file gains
// a second sheet the sheet-name catch is what bites first. Either way it is
// refused, which is the whole of what this test is for.
func TestSalesExportIsRejectedAsASaleImport(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Round Trip Fest", "round-trip-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)
	commitBatch(t, env, sessionID, eventID, "round-trip-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})

	resp, data := downloadSalesExport(t, env, sessionID, eventID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}

	previewResp, body := postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports/preview",
		"sales-export.xlsx", data, nil, authHeader(sessionID))
	if previewResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("preview of an export status=%d, want 400; error=%+v", previewResp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "IMPORT_FILE_INVALID" {
		t.Fatalf("preview error = %+v, want IMPORT_FILE_INVALID", body.Error)
	}
}

// Net Proceeds per row (#237). Each row states what the sale left the
// Organization once the Platform Fee and its Fee IVA were withheld, read off the
// per-line snapshots the sale froze — the same arithmetic the Event's Sales
// summary already sums, grouped per sale instead of per Event.
//
// The figures below are the fee suite's: a $7.99 ticket withholds 80¢ of
// Platform Fee and 12¢ of Fee IVA, so a pass-on buyer pays 891¢ and the
// Organization nets the 799¢ it set, while an absorb buyer pays 799¢ and the
// Organization nets 707¢. Prices are $7.99-shaped on purpose: every figure here
// depends on the rounding rather than on round numbers.

// moveSaleToTheDoor puts a recorded sale onto the in_person channel, staging in
// SQL the state a POS would leave behind (there is no in-person recording
// endpoint yet — see sale_paths_external_event_test.go).
//
// It deliberately moves a sale that WAS sold online, so the row keeps the
// non-zero fee snapshot no in-person path could have produced. That is what
// makes the blank below prove the rule rather than an accident of arithmetic: if
// the export summed the snapshot without asking what channel the sale was on,
// this row would carry a figure.
func moveSaleToTheDoor(t *testing.T, env *testEnv, confirmationRef string) {
	t.Helper()
	res, err := env.db.Exec(`
		UPDATE ticket_sales SET channel = 'in_person', payment_method = 'cash'
		WHERE confirmation_ref = $1
	`, confirmationRef)
	if err != nil {
		t.Fatalf("move the sale onto the in-person channel: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("moving %s in-person affected %d rows, want 1", confirmationRef, n)
	}
}

// TestSalesExportStatesNetProceeds: an Online Sale's row says what it left the
// Organization, and a sale the platform's money never passed through leaves the
// column blank rather than claiming a zero.
//
// The blank is the point. Only an Online Sale produces Net Proceeds; on any
// other Sales Channel the platform held no money and withheld none, so a 0 would
// assert something false — and in a spreadsheet that false assertion is silently
// added into a SUM. This is ADR-0019's "absent rather than zero", applied to the
// column an Organization will reconcile its bank deposit against.
func TestSalesExportStatesNetProceeds(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Net Export Fest", "net-export-fest", feeTestBaseCents, 20)

	// Sold online under the default pass_on: the buyer paid the all-in price and
	// the Organization nets the price it set.
	online := beginCheckoutOK(t, env, "test-org", "net-export-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	confirmCheckoutOK(t, env, online.ClientTransactionID, "approved")

	// A second Online Sale, then moved to the door: same frozen snapshot, but the
	// platform never held this money.
	door := beginCheckoutOK(t, env, "test-org", "net-export-fest",
		checkoutBody("caro@example.com", "Caro", "Diaz", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	doorConfirm := confirmCheckoutOK(t, env, door.ClientTransactionID, "approved")
	moveSaleToTheDoor(t, env, doorConfirm.ConfirmationRef)

	// Cash recorded through a Sale Import: never near the platform's money either.
	commitBatch(t, env, sessionID, eventID, "net-export-batch", []map[string]any{
		{"customer_email": "leo@example.com", "customer_first_name": "Leo", "customer_last_name": "Vera",
			"ticket_type_id": gaID, "quantity": 4, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})

	resp, data := downloadSalesExport(t, env, sessionID, eventID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}
	sheet := openSalesExport(t, data)

	// 2 × (891 paid − 80 fee − 12 IVA) = 2 × 799¢, stated in major units so a
	// SUM of the column gives dollars rather than a figure a hundred times too
	// large.
	onlineRow := sheet.rowOf(t, "ana@example.com")
	wantNet := float64(2*(feeTestAllInCents-feeTestFeeCents-feeTestIVACents)) / 100
	if got := sheet.number(t, onlineRow, "net_proceeds"); got != wantNet {
		t.Fatalf("online net_proceeds = %v, want %v", got, wantNet)
	}
	// And it is genuinely a different number from what the buyer paid — the
	// column would be worthless if it just repeated the amount.
	wantAmount := float64(2*feeTestAllInCents) / 100
	if got := sheet.number(t, onlineRow, "amount"); got != wantAmount {
		t.Fatalf("online amount = %v, want the all-in %v the buyer paid", got, wantAmount)
	}

	// The two rows the platform's money never touched: blank, never zero.
	doorRow := sheet.rowOf(t, "caro@example.com")
	sheet.blank(t, doorRow, "net_proceeds")
	if got := sheet.value(t, doorRow, "channel"); got != "in_person" {
		t.Fatalf("door channel = %q, want in_person", got)
	}
	// The row is otherwise whole: only the figure that does not apply is missing.
	if got := sheet.number(t, doorRow, "amount"); got != float64(feeTestAllInCents)/100 {
		t.Fatalf("door amount = %v, want the recorded %v", got, float64(feeTestAllInCents)/100)
	}

	importRow := sheet.rowOf(t, "leo@example.com")
	sheet.blank(t, importRow, "net_proceeds")
	if got := sheet.value(t, importRow, "channel"); got != "import" {
		t.Fatalf("imported channel = %q, want import", got)
	}
	if sheet.number(t, importRow, "amount") == 0 {
		t.Fatalf("imported amount is 0; the sale's own money is still its own")
	}
}

// TestSalesExportNetProceedsReadsTheSnapshotNotTheMode: the figure is correct
// under absorb as well as pass_on, and it stays correct after the Event's Fee
// Handling is flipped.
//
// This is the reason the column exists at all. An Org Admin reconciling per sale
// would otherwise have to recompute 10% plus 15% of that by hand and remember
// which mode the Event was in at the time — and would get it wrong the moment
// the mode had changed since. The export never branches on the mode: it reads
// the snapshot the sale froze, which is the same subtraction either way.
func TestSalesExportNetProceedsReadsTheSnapshotNotTheMode(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Absorb Export Fest", "absorb-export-fest", feeTestBaseCents, 20)
	setFeeHandling(t, env, sessionID, eventID, "Absorb Export Fest", "absorb-export-fest", "absorb")

	begin := beginCheckoutOK(t, env, "test-org", "absorb-export-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	// The organizer flips the Event afterwards. Nothing already recorded moves.
	setFeeHandling(t, env, sessionID, eventID, "Absorb Export Fest", "absorb-export-fest", "pass_on")

	resp, data := downloadSalesExport(t, env, sessionID, eventID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}
	sheet := openSalesExport(t, data)
	row := sheet.rowOf(t, "ana@example.com")

	// Under absorb the buyer paid the set price and the withholding came out of
	// it: 2 × (799 − 80 − 12) = 2 × 707¢.
	wantNet := float64(2*(feeTestBaseCents-feeTestFeeCents-feeTestIVACents)) / 100
	if got := sheet.number(t, row, "net_proceeds"); got != wantNet {
		t.Fatalf("absorb net_proceeds = %v, want %v — the snapshot, not the Event's current mode", got, wantNet)
	}
	if got := sheet.number(t, row, "amount"); got != float64(2*feeTestBaseCents)/100 {
		t.Fatalf("absorb amount = %v, want the set price %v", got, float64(2*feeTestBaseCents)/100)
	}
}

// TestSalesExportReversedSaleHasBlankNetProceeds: a reversed sale keeps its row
// — a Sale Reversal should be visible in the file rather than a row that
// silently vanished — but drops out of the money, as it does on every other
// surface. Money given back was never proceeds.
func TestSalesExportReversedSaleHasBlankNetProceeds(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Undone Export Fest", "undone-export-fest", feeTestBaseCents, 20)

	begin := beginCheckoutOK(t, env, "test-org", "undone-export-fest",
		checkoutBody("bea@example.com", "Bea", "Ruiz", map[string]any{"ticket_type_id": gaID, "quantity": 3}))
	confirmed := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	// Through the Customer's own endpoint, so the figure is measured against a
	// reversal the system actually performed rather than an UPDATE.
	undoOwnSale(t, env, "bea@example.com", confirmed.ConfirmationRef)

	// Reversed sales are off the default file, exactly as they are off the
	// default screen; the status filter is the lever that reaches them.
	resp, data := downloadSalesExport(t, env, sessionID, eventID, "status=reversed")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}
	sheet := openSalesExport(t, data)
	row := sheet.rowOf(t, "bea@example.com")

	if got := sheet.value(t, row, "status"); got != "reversed" {
		t.Fatalf("status = %q, want reversed", got)
	}
	sheet.blank(t, row, "net_proceeds")
	// The row is still a row: the sale happened, and the file says so.
	if sheet.value(t, row, "confirmation_ref") == "" {
		t.Fatalf("reversed row lost its confirmation_ref; a reversal must be visible, not vanished")
	}
	if got := sheet.number(t, row, "amount"); got != float64(3*feeTestAllInCents)/100 {
		t.Fatalf("reversed amount = %v, want what the buyer paid %v", got, float64(3*feeTestAllInCents)/100)
	}
}

// TestSalesExportNeverItemisesThePlatformFee: the file states what the
// Organization nets and never what the platform took.
//
// This is the stance already recorded on the Sales summary handler — "the
// platform's cut is never returned as a number" — held on the surface most
// tempting to break it, because the per-line fee snapshots are sitting right
// there and emitting them would be one more column. Somebody determined can
// subtract net_proceeds from amount, but the product still does not state it.
func TestSalesExportNeverItemisesThePlatformFee(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Quiet Export Fest", "quiet-export-fest", feeTestBaseCents, 20)
	begin := beginCheckoutOK(t, env, "test-org", "quiet-export-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	resp, data := downloadSalesExport(t, env, sessionID, eventID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}
	sheet := openSalesExport(t, data)

	if _, ok := sheet.index["net_proceeds"]; !ok {
		t.Fatalf("header = %v, want a net_proceeds column", sheet.header)
	}
	for _, forbidden := range []string{
		"platform_fee", "platform_fee_cents", "fee", "fee_cents",
		"fee_iva", "fee_iva_cents", "fee_basis_points", "fee_iva_basis_points",
		"base_price", "gross",
	} {
		if _, ok := sheet.index[forbidden]; ok {
			t.Fatalf("header carries %q: %v — the Organization is told what it nets, never what the platform took", forbidden, sheet.header)
		}
	}

	// net_proceeds sits immediately after amount: what the buyer paid, then what
	// the sale left the Organization, side by side where they are compared.
	if sheet.index["net_proceeds"] != sheet.index["amount"]+1 {
		t.Fatalf("header = %v, want net_proceeds immediately after amount", sheet.header)
	}
}
