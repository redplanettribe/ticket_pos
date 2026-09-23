package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales/importfile"
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

// typeHeaders returns the headings of the per-Ticket-Type block: everything
// between tax_id_number and total_quantity, which is where the spec fixes it.
//
// It is read positionally rather than by name because a Ticket Type is named by
// an organizer and may be called anything at all — including "amount". The
// header index is a map, so a name that collides with a fixed column would
// resolve to whichever came last; the block's boundaries are what identify it.
func (s *exportSheet) typeHeaders(t *testing.T) []string {
	t.Helper()
	from, ok := s.index["tax_id_number"]
	if !ok {
		t.Fatalf("no tax_id_number column in header %v", s.header)
	}
	to, ok := s.index["total_quantity"]
	if !ok {
		t.Fatalf("no total_quantity column in header %v", s.header)
	}
	if to < from {
		t.Fatalf("header = %v, want the Ticket Type columns between tax_id_number and total_quantity", s.header)
	}
	return s.header[from+1 : to]
}

// rawAt reads a cell by its zero-based column position rather than its heading,
// for the one case where a heading cannot identify a column: a Ticket Type whose
// name is also a fixed column's.
func (s *exportSheet) rawAt(t *testing.T, row, col int) string {
	t.Helper()
	ref, err := excelize.CoordinatesToCellName(col+1, row+2)
	if err != nil {
		t.Fatalf("cell name: %v", err)
	}
	v, err := s.f.GetCellValue(salesExportSheet, ref, excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatalf("get raw cell %s: %v", ref, err)
	}
	return v
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
	// A response header and not the file, so the file itself is unchanged
	// (#655 story 34).
	assertNoStore(t, resp.Header)

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
		// The Event's catalog, one column per Ticket Type, then the roll-up.
		"GA", "total_quantity",
		"amount", "net_proceeds", "currency",
		// The origin (#373) sits with the other provenance facts, after the
		// channel and source it says what neither of them can.
		"channel", "source", "origin", "payment_method", "status",
		// The reversal pair after the status it elaborates, then the Sale
		// Correction linkage, last.
		"reversed_at", "reversed_by", "corrected_by", "corrects",
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
// Now that the file carries an Info sheet as well as its data sheet (#240), the
// catch that bites is the intended one. The parser selects the sheet named
// "Sales" and falls back to the sole sheet only of a single-sheet workbook; a
// two-sheet export has no "Sales" and no fallback, so it is refused for the
// reason the format was shaped around — the data sheet is deliberately called
// "Ticket Sales" — rather than incidentally, for having the wrong columns.
//
// The reason is asserted here precisely because it moved. Renaming the data
// sheet to "Sales", or collapsing the workbook back to one sheet, would each
// leave the refusal working today (the columns are still wrong) while quietly
// removing the guard the spec relies on. This test is what notices.
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
	// The sheet-name catch, not the column check: the organizer is told the
	// 'Sales' sheet is missing, which is exactly what an export never has.
	if !strings.Contains(body.Error.Message, "'Sales' sheet") {
		t.Fatalf("preview message = %q, want the missing 'Sales' sheet — the catch the export's sheet name exists for", body.Error.Message)
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

// Per-Ticket-Type quantity columns (#238). The file gains one numeric column per
// Ticket Type in the Event's catalog, plus a total_quantity roll-up, so an Event
// Owner can pivot or SUMIF on ticket type instead of parsing "General x2, VIP x1"
// out of a text cell.
//
// The columns come from the Event's LIVE catalog rather than from the Ticket
// Types present in the rows, which is what keeps the shape of the sheet stable
// under the filters. That is safe because ticket_sale_lines references
// ticket_types ON DELETE RESTRICT: a Ticket Type that has ever sold cannot be
// deleted, so the catalog is always a superset of what the rows reference.

// renameTicketType renames a Ticket Type through the catalog PATCH the organizer
// uses, so a rename in a test is the rename the product performs.
func renameTicketType(t *testing.T, env *testEnv, sessionID, eventID, ticketTypeID, name string, priceCents, capacity int) {
	t.Helper()
	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID+"/ticket-types/"+ticketTypeID, map[string]any{
		"name":        name,
		"price_cents": priceCents,
		"capacity":    capacity,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rename ticket type status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// TestSalesExportHasAColumnPerTicketType: the whole catalog in display order, a
// quantity where the sale included the type, and a blank — never a zero — where
// it did not.
//
// The blank follows the same absent-versus-zero rule the money columns do. A 0
// would assert that this sale considered and bought none of that type; a blank
// says the type is not part of this sale at all. A Ticket Type nobody bought
// still gets its column: an empty column is information, while a missing one
// makes a reader wonder whether they filtered something out.
func TestSalesExportHasAColumnPerTicketType(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Catalog Fest", "catalog-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)
	vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 5000, 50)
	// Created third, and never sold: it must still get a column.
	createTicketTypeWithCapacity(t, env, sessionID, eventID, "Balcony", 2000, 20)

	commitBatch(t, env, sessionID, eventID, "catalog-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": vipID, "quantity": 3, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
	})

	resp, data := downloadSalesExport(t, env, sessionID, eventID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}
	sheet := openSalesExport(t, data)

	// The catalog, in the order the Event lists it — including the type nobody
	// bought.
	if got := sheet.typeHeaders(t); !equalStrings(got, []string{"GA", "VIP", "Balcony"}) {
		t.Fatalf("Ticket Type columns = %v, want the whole catalog in display order; header = %v", got, sheet.header)
	}

	// The columns sit where the spec fixes them: after the Tax ID pair, with
	// total_quantity immediately before amount.
	if sheet.index["total_quantity"] != sheet.index["amount"]-1 {
		t.Fatalf("header = %v, want total_quantity immediately before amount", sheet.header)
	}

	ana := sheet.rowOf(t, "ana@example.com")
	if got := sheet.raw(t, ana, "GA"); got != "2" {
		t.Fatalf("GA on Ana's row = %q, want the whole number 2", got)
	}
	// The types this sale did not include are blank, not zero.
	sheet.blank(t, ana, "VIP")
	sheet.blank(t, ana, "Balcony")
	if got := sheet.number(t, ana, "total_quantity"); got != 2 {
		t.Fatalf("total_quantity on Ana's row = %v, want 2", got)
	}

	bob := sheet.rowOf(t, "bob@example.com")
	sheet.blank(t, bob, "GA")
	if got := sheet.raw(t, bob, "VIP"); got != "3" {
		t.Fatalf("VIP on Bob's row = %q, want the whole number 3", got)
	}
	sheet.blank(t, bob, "Balcony")
	if got := sheet.number(t, bob, "total_quantity"); got != 3 {
		t.Fatalf("total_quantity on Bob's row = %v, want 3", got)
	}
}

// TestSalesExportMultiLineSaleIsOneRow: a sale of three Ticket Types is one row
// with its quantities spread across three columns, not three rows.
//
// This is the whole reason the quantities became columns. Flattening a sale to
// its Ticket Sale Lines would repeat the sale's amount on every line, and the
// first thing anybody does with a spreadsheet is sum a column — so the amount
// must appear exactly once per sale.
func TestSalesExportMultiLineSaleIsOneRow(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Multi Line Fest", "multi-line-fest", 1000, 50)
	vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 5000, 50)
	balconyID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "Balcony", 2000, 50)

	begun := beginCheckoutOK(t, env, "test-org", "multi-line-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez",
			map[string]any{"ticket_type_id": gaID, "quantity": 2},
			map[string]any{"ticket_type_id": vipID, "quantity": 1},
			map[string]any{"ticket_type_id": balconyID, "quantity": 4}))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	resp, data := downloadSalesExport(t, env, sessionID, eventID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}
	sheet := openSalesExport(t, data)

	// One Ticket Sale, one row — not one row per Ticket Sale Line.
	if sheet.dataRows != 1 {
		t.Fatalf("data rows = %d, want exactly 1: a three-type sale is one Ticket Sale", sheet.dataRows)
	}
	row := sheet.rowOf(t, "ana@example.com")

	for header, want := range map[string]string{"GA": "2", "VIP": "1", "Balcony": "4"} {
		if got := sheet.raw(t, row, header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}
	if got := sheet.number(t, row, "total_quantity"); got != 7 {
		t.Fatalf("total_quantity = %v, want 7 — the sum of the row's type quantities", got)
	}

	// The amount is the sale's, stated once: summing the column gives what the
	// buyer paid rather than three times it.
	wantAmount := float64(begun.AmountCents) / 100
	if got := sheet.number(t, row, "amount"); got != wantAmount {
		t.Fatalf("amount = %v, want the sale's %v", got, wantAmount)
	}
}

// TestSalesExportTypeColumnsSurviveATicketTypeFilter: filtering the export to one
// Ticket Type narrows the rows and nothing else. The columns cover the Event's
// whole catalog either way, so two downloads of the same Event always have the
// same shape and can be compared side by side.
func TestSalesExportTypeColumnsSurviveATicketTypeFilter(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Narrow Fest", "narrow-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)
	vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 5000, 50)
	createTicketTypeWithCapacity(t, env, sessionID, eventID, "Balcony", 2000, 20)

	commitBatch(t, env, sessionID, eventID, "narrow-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": vipID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
	})

	resp, data := downloadSalesExport(t, env, sessionID, eventID, "ticket_type_id="+vipID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}
	sheet := openSalesExport(t, data)

	if got := sheet.typeHeaders(t); !equalStrings(got, []string{"GA", "VIP", "Balcony"}) {
		t.Fatalf("Ticket Type columns under a VIP filter = %v, want the whole catalog: only the rows respond to filters", got)
	}
	if got := sheet.column(t, "customer_email"); !equalStrings(got, []string{"bob@example.com"}) {
		t.Fatalf("rows under a VIP filter = %v, want the VIP sale only", got)
	}
	row := sheet.rowOf(t, "bob@example.com")
	if got := sheet.raw(t, row, "VIP"); got != "1" {
		t.Fatalf("VIP = %q, want 1", got)
	}
	sheet.blank(t, row, "GA")
	sheet.blank(t, row, "Balcony")
}

// TestSalesExportHeadingFollowsATicketTypeRename: a heading is the Ticket Type's
// CURRENT name, joined live, so a rename changes the heading on the next export
// — including over sales recorded before the rename.
//
// A renamed Ticket Type rewriting its own history is accepted deliberately.
// ticket_sale_lines snapshots the unit price, because that is a fact of the sale,
// and not the name, because that is a fact of the catalog. Snapshotting the name
// here would make the export the only surface reporting historical names, and it
// would then contradict the screen it was downloaded from.
func TestSalesExportHeadingFollowsATicketTypeRename(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Rename Fest", "rename-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)

	commitBatch(t, env, sessionID, eventID, "rename-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})

	_, before := downloadSalesExport(t, env, sessionID, eventID, "")
	if got := openSalesExport(t, before).typeHeaders(t); !equalStrings(got, []string{"GA"}) {
		t.Fatalf("Ticket Type columns = %v, want GA", got)
	}

	renameTicketType(t, env, sessionID, eventID, gaID, "General Admission", 1000, 100)

	resp, after := downloadSalesExport(t, env, sessionID, eventID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(after))
	}
	sheet := openSalesExport(t, after)
	if got := sheet.typeHeaders(t); !equalStrings(got, []string{"General Admission"}) {
		t.Fatalf("Ticket Type columns after the rename = %v, want the current name", got)
	}
	if _, ok := sheet.index["GA"]; ok {
		t.Fatalf("header = %v, still carries the old name: no name is snapshotted onto a sale line", sheet.header)
	}
	// The sale recorded under the old name is still counted, under the new one.
	if got := sheet.raw(t, sheet.rowOf(t, "ana@example.com"), "General Admission"); got != "2" {
		t.Fatalf("General Admission = %q, want the 2 sold before the rename", got)
	}
}

// TestSalesExportTicketTypeNamedLikeAFixedColumn: an organizer may call a Ticket
// Type anything, including "amount". The builder addresses every column by
// identity — a fixed column by its own key, a Ticket Type by its id — and never
// by the heading it writes, so a collision costs the reader a repeated heading
// and costs the data nothing.
func TestSalesExportTicketTypeNamedLikeAFixedColumn(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Collision Fest", "collision-fest")
	oddID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "amount", 1000, 100)

	commitBatch(t, env, sessionID, eventID, "collision-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": oddID, "quantity": 3, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z", "amount_cents": 1500},
	})

	resp, data := downloadSalesExport(t, env, sessionID, eventID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}
	sheet := openSalesExport(t, data)

	// The Ticket Type gets its own column, headed with its name, in the block.
	if got := sheet.typeHeaders(t); !equalStrings(got, []string{"amount"}) {
		t.Fatalf("Ticket Type columns = %v, want the type's own name", got)
	}
	row := sheet.rowOf(t, "ana@example.com")
	quantityCol := sheet.index["tax_id_number"] + 1
	if got := sheet.rawAt(t, row, quantityCol); got != "3" {
		t.Fatalf("the Ticket Type column = %q, want the quantity 3", got)
	}
	// And the money column of the same name still holds the money: 3 × $15.00.
	if got := sheet.number(t, row, "amount"); got != 45 {
		t.Fatalf("amount = %v, want the 45.00 the buyer paid", got)
	}
	if got := sheet.number(t, row, "total_quantity"); got != 3 {
		t.Fatalf("total_quantity = %v, want 3", got)
	}
}

// Reversal columns (#239). A reversed Ticket Sale keeps its row — it is never
// deleted, it keeps its Sale Confirmation reference, and it stays visible to the
// Organization — so the file states the reversal rather than leaving a row with
// a mysteriously empty money column.
//
// `reversed_by` names the ROUTE and nothing else: `customer`, `platform`, or
// `import_undo`, the three ways a Sale Reversal is reachable. The stored column
// speaks a different vocabulary (`customer`, `staff`, `operator`) and sits one
// column away from the acting operator's email and their note, so this file is
// exactly where ADR-0019's boundary — an Operator Reversal is invisible to the
// Organization beyond the sale showing as reversed by the platform — would be
// breached by a pass-through.

// reversedSalesExport downloads the reversed sales and opens the sheet. Every
// test below needs the status filter: the default file is the default screen,
// and the default screen is active sales only.
func reversedSalesExport(t *testing.T, env *testEnv, sessionID, eventID string) *exportSheet {
	t.Helper()
	resp, data := downloadSalesExport(t, env, sessionID, eventID, "status=reversed")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reversed export status=%d body=%s", resp.StatusCode, string(data))
	}
	return openSalesExport(t, data)
}

// TestSalesExportReversalColumnsAreLastAndBlankOnAnActiveSale: the pair sits at
// the end of the row, after status, and says nothing at all about a sale that
// was never reversed.
//
// Blank rather than "active", "-", or "n/a": the columns describe an event that
// did not happen, and a placeholder would have to be filtered out by anybody
// counting reversals in the sheet.
func TestSalesExportReversalColumnsAreLastAndBlankOnAnActiveSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Standing Fest", "standing-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)
	commitBatch(t, env, sessionID, eventID, "standing-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})

	resp, data := downloadSalesExport(t, env, sessionID, eventID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}
	sheet := openSalesExport(t, data)

	// Last, and in this order: the sale's status, then when it was undone and by
	// which route.
	tail := sheet.header[len(sheet.header)-5:]
	if !equalStrings(tail, []string{"status", "reversed_at", "reversed_by", "corrected_by", "corrects"}) {
		t.Fatalf("header tail = %v, want status, reversed_at, reversed_by, corrected_by, corrects last", tail)
	}

	row := sheet.rowOf(t, "ana@example.com")
	if got := sheet.value(t, row, "status"); got != "active" {
		t.Fatalf("status = %q, want active", got)
	}
	sheet.blank(t, row, "reversed_at")
	sheet.blank(t, row, "reversed_by")
	sheet.blank(t, row, "corrected_by")
	sheet.blank(t, row, "corrects")
}

// TestSalesExportNamesTheReversalRoute: all three routes into a Sale Reversal,
// in one file, each named by the route and not by the actor behind it.
//
// The three are genuinely different mechanisms — a buyer pressing Undo inside
// the Reversal Window, a Platform Operator recording an off-platform refund, and
// staff undoing a whole Sale Import batch — and telling them apart is the whole
// point of the column: an Org Admin reading the file can distinguish a buyer
// changing their mind from the platform stepping in from their own import being
// rolled back.
func TestSalesExportNamesTheReversalRoute(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	// publishCheckoutEvent puts the Event in America/Guayaquil, five hours behind
	// UTC — which is what makes the reversed_at rendering below an assertion
	// about the Event's timezone rather than about UTC.
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Routes Fest", "routes-fest", feeTestBaseCents, 50)

	// The Customer's own route: the buyer undoes their Online Sale from the
	// Storefront, inside the Reversal Window.
	customerBegin := beginCheckoutOK(t, env, "test-org", "routes-fest",
		taxIDCheckoutBody("bea@example.com", "Bea", "Ruiz", "ruc", naturalRUC,
			map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	customerSale := confirmCheckoutOK(t, env, customerBegin.ClientTransactionID, "approved")
	undoOwnSale(t, env, "bea@example.com", customerSale.ConfirmationRef)

	// The platform's route: an Operator Reversal, recording a refund the platform
	// made off-platform at the Organization's request.
	operatorBegin := beginCheckoutOK(t, env, "test-org", "routes-fest",
		checkoutBody("caro@example.com", "Caro", "Diaz", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	operatorSale := confirmCheckoutOK(t, env, operatorBegin.ClientTransactionID, "approved")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	operatorReverseOK(t, env, operatorSessionID, operatorSale.ConfirmationRef, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(1)),
		PlatformFeeKept:     boolPtr(false),
		Note:                strPtr("bank transfer, waived the fee as goodwill"),
	})

	// The Sale Import's route: staff undoing a whole committed batch.
	batchID := commitBatch(t, env, sessionID, eventID, "routes-batch", []map[string]any{
		{"customer_email": "leo@example.com", "customer_first_name": "Leo", "customer_last_name": "Vera", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	undoBatch(t, env, sessionID, eventID, batchID)

	sheet := reversedSalesExport(t, env, sessionID, eventID)
	if sheet.dataRows != 3 {
		t.Fatalf("reversed rows = %d, want the three routes", sheet.dataRows)
	}
	for email, want := range map[string]string{
		"bea@example.com":  "customer",
		"caro@example.com": "platform",
		"leo@example.com":  "import_undo",
	} {
		row := sheet.rowOf(t, email)
		if got := sheet.value(t, row, "reversed_by"); got != want {
			t.Fatalf("%s reversed_by = %q, want %q", email, got, want)
		}
		if got := sheet.value(t, row, "status"); got != "reversed" {
			t.Fatalf("%s status = %q, want reversed", email, got)
		}
	}

	// reversed_at is a real date cell like sold_at — an Excel serial, sortable
	// and subtractable — drawn in the Event's timezone. The suite's clock reads
	// 2026-07-07 12:00 UTC, which is 07:00 in Guayaquil.
	customerRow := sheet.rowOf(t, "bea@example.com")
	rawReversed := sheet.raw(t, customerRow, "reversed_at")
	if _, err := strconv.ParseFloat(rawReversed, 64); err != nil {
		t.Fatalf("reversed_at = %q, want a date cell (an Excel serial), not text", rawReversed)
	}
	if got := sheet.value(t, customerRow, "reversed_at"); got != "2026-07-07 07:00" {
		t.Fatalf("reversed_at renders %q, want 2026-07-07 07:00 in the Event's timezone", got)
	}

	// A reversed sale is a sale that happened. It keeps its Sale Confirmation
	// reference — the value somebody pastes back into the product when the buyer
	// emails about it — and every buyer column it was recorded with, including
	// the Tax ID pair the Organization may still have to invoice against.
	if got := sheet.value(t, customerRow, "confirmation_ref"); got != customerSale.ConfirmationRef {
		t.Fatalf("reversed confirmation_ref = %q, want %q — a reversal must be visible, not vanished",
			got, customerSale.ConfirmationRef)
	}
	for header, want := range map[string]string{
		"customer_first_name": "Bea",
		"customer_last_name":  "Ruiz",
		"customer_email":      "bea@example.com",
		"tax_id_type":         "ruc",
		"tax_id_number":       naturalRUC,
		"channel":             "online",
	} {
		if got := sheet.value(t, customerRow, header); got != want {
			t.Fatalf("reversed row %s = %q, want %q", header, got, want)
		}
	}
	// The money it collected is still its own; only the figure that no longer
	// applies is blank (#237's rule, relied on here rather than restated).
	if got := sheet.number(t, customerRow, "amount"); got != float64(2*feeTestAllInCents)/100 {
		t.Fatalf("reversed amount = %v, want what the buyer paid %v", got, float64(2*feeTestAllInCents)/100)
	}
	sheet.blank(t, customerRow, "net_proceeds")
}

// TestSalesExportNeverNamesTheOperatorOrTheirNote is the ADR-0019 boundary, and
// it is deliberately paranoid.
//
// An Operator Reversal is invisible to the Organization beyond the sale showing
// as reversed by the platform. The operator's email and their free-text note are
// stored on the ticket_sales row itself, a column away from reversed_by, so the
// export is precisely where a pass-through happens by accident — a SELECT
// widened, a struct field copied across, a "helpful" provenance column added.
//
// So the assertion is not on the reversal columns. It is on EVERY cell of EVERY
// sheet of the workbook, read both formatted and raw, plus the sheet names: the
// operator's identity and their words must not be anywhere in the file the
// Organization receives, however they got there.
func TestSalesExportNeverNamesTheOperatorOrTheirNote(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Boundary Fest", "boundary-fest", feeTestBaseCents, 20)

	begin := beginCheckoutOK(t, env, "test-org", "boundary-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	sale := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	const operatorEmail = "regina.silva@platform.example"
	const note = "refunded by bank transfer after the chargeback thread; fee waived"
	operatorSessionID := operatorSession(t, env, operatorEmail)
	operatorReverseOK(t, env, operatorSessionID, sale.ConfirmationRef, operatorReversalBody{
		RefundedAmountCents: intPtr(paidRefund(1)),
		PlatformFeeKept:     boolPtr(true),
		Note:                strPtr(note),
	})

	// The record itself exists — otherwise the sweep below would prove nothing,
	// because there would be nothing available to leak.
	storedOperator, storedNote, _, _ := operatorMemo(t, env, sale.ConfirmationRef)
	if !storedOperator.Valid || storedOperator.String != operatorEmail {
		t.Fatalf("stored operator = %+v, want %q — the leak test needs something to leak", storedOperator, operatorEmail)
	}
	if !storedNote.Valid || storedNote.String != note {
		t.Fatalf("stored note = %+v, want the operator's own words", storedNote)
	}

	resp, data := downloadSalesExport(t, env, sessionID, eventID, "status=reversed")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}

	// What the Organization IS told: the sale is reversed, by the platform.
	sheet := openSalesExport(t, data)
	row := sheet.rowOf(t, "ana@example.com")
	if got := sheet.value(t, row, "reversed_by"); got != "platform" {
		t.Fatalf("reversed_by = %q, want platform — the route, never the operator", got)
	}

	// And what it is not. Every needle is checked whole and in the pieces a
	// careless emission would leave behind: an email's local part, its domain, a
	// distinctive word of the note.
	needles := []string{
		operatorEmail, "regina.silva", "regina", "platform.example",
		note, "chargeback", "bank transfer", "waived",
		// The operator-facing money memo travels with the identity and is just
		// as far out of bounds.
		"platform_fee_kept", "refunded_amount", "reversal_note", "reversed_by_operator",
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open .xlsx: %v", err)
	}
	defer func() { _ = f.Close() }()

	check := func(where, text string) {
		t.Helper()
		for _, needle := range needles {
			if strings.Contains(strings.ToLower(text), strings.ToLower(needle)) {
				t.Fatalf("%s carries %q in %q — ADR-0019: an Operator Reversal is invisible to the Organization beyond the sale showing as reversed by the platform",
					where, needle, text)
			}
		}
	}
	for _, name := range f.GetSheetList() {
		check("sheet name", name)
		// Both readings of every cell: a value can hide in the stored form or in
		// the rendered one, and only reading both rules out either.
		for _, opts := range []excelize.Options{{}, {RawCellValue: true}} {
			rows, err := f.GetRows(name, opts)
			if err != nil {
				t.Fatalf("get rows from %q: %v", name, err)
			}
			for r, cells := range rows {
				for c, cell := range cells {
					check(fmt.Sprintf("sheet %q cell r%dc%d", name, r+1, c+1), cell)
				}
			}
		}
	}
}

// The Info sheet (#240). The export mirrors whatever filters were on screen, so
// the file is not canonical: two Owners can produce different files both called
// "sales". The Info sheet is what makes that safe — it is the file explaining
// itself to somebody who did not download it, the colleague it was forwarded to
// or the same person three months later.
//
// It is a SEPARATE SHEET rather than a header block above the data, and that is
// the load-bearing part of the shape: preamble rows above a header break
// select-all, break autofilter, and hand a pivot table the wrong source range.
// The data sheet keeps row 1 as its header, with nothing above it.

// salesExportInfoSheet is the sheet that explains the file: first in the
// workbook and active on open, mirroring how the Sale Import template greets the
// organizer with its Instructions sheet.
const salesExportInfoSheet = "Info"

// infoSheet is a downloaded export's Info sheet, read as the prose it is.
//
// The assertions here are on MEANING rather than on cell addresses: the sheet is
// sentences a person reads, not a grid anything parses, so a test that pinned
// copy to A7 would break on every rewording while proving nothing about what the
// reader is actually told.
type infoSheet struct {
	// lines is every non-empty line of the sheet, lowercased.
	lines []string
	// text is all of them joined, for "is this anywhere in the file" checks.
	text string
}

// openSalesExportInfo opens a downloaded export and reads its Info sheet.
func openSalesExportInfo(t *testing.T, data []byte) infoSheet {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open .xlsx: %v", err)
	}
	defer func() { _ = f.Close() }()

	rows, err := f.GetRows(salesExportInfoSheet)
	if err != nil {
		t.Fatalf("get rows from %q: %v", salesExportInfoSheet, err)
	}
	out := infoSheet{}
	for _, cells := range rows {
		line := strings.ToLower(strings.TrimSpace(strings.Join(cells, " ")))
		if line == "" {
			continue
		}
		out.lines = append(out.lines, line)
	}
	out.text = strings.Join(out.lines, "\n")
	return out
}

// says asserts that one LINE of the sheet carries all the given fragments —
// one line, so a claim is read as the sentence it is rather than assembled out
// of words scattered across the sheet.
func (i infoSheet) says(t *testing.T, fragments ...string) {
	t.Helper()
	for _, line := range i.lines {
		found := true
		for _, fragment := range fragments {
			if !strings.Contains(line, strings.ToLower(fragment)) {
				found = false
				break
			}
		}
		if found {
			return
		}
	}
	t.Fatalf("no line of the Info sheet says %v; it says:\n%s", fragments, i.text)
}

// silentAbout asserts a fragment appears nowhere on the sheet.
func (i infoSheet) silentAbout(t *testing.T, fragment string) {
	t.Helper()
	if strings.Contains(i.text, strings.ToLower(fragment)) {
		t.Fatalf("the Info sheet carries %q:\n%s", fragment, i.text)
	}
}

// TestSalesExportInfoSheetExplainsTheFile: the workbook is exactly two sheets,
// Info greets the reader, and the data sheet is untouched by its arrival.
func TestSalesExportInfoSheetExplainsTheFile(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Info Fest", "info-fest")
	setEventTimezone(t, env, eventID, "America/Guayaquil")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)
	commitBatch(t, env, sessionID, eventID, "info-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
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

	// Exactly two sheets, in this order. No hidden helper sheet and no leftover
	// "Sheet1": everything in the file is something the reader was meant to get.
	if got := f.GetSheetList(); !equalStrings(got, []string{salesExportInfoSheet, salesExportSheet}) {
		t.Fatalf("sheets = %v, want exactly %q then %q", got, salesExportInfoSheet, salesExportSheet)
	}
	// And Info is the one that opens, so the file explains itself before it
	// shows itself.
	if got := f.GetSheetName(f.GetActiveSheetIndex()); got != salesExportInfoSheet {
		t.Fatalf("active sheet = %q, want %q", got, salesExportInfoSheet)
	}

	// The data sheet is untouched by the stamp's arrival: row 1 is still the
	// header. This is the whole reason the stamp is a sheet of its own — a
	// preamble above the header would break select-all, autofilter and a pivot
	// table's source range.
	rows, err := f.GetRows(salesExportSheet)
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	if len(rows) == 0 || len(rows[0]) == 0 || rows[0][0] != "confirmation_ref" {
		t.Fatalf("data sheet row 1 = %v, want the header row with nothing above it", rows)
	}
	if len(rows) != 3 {
		t.Fatalf("data sheet rows = %d, want a header and two sales", len(rows))
	}

	info := openSalesExportInfo(t, data)
	// Which Event, and when the file was taken — the generated-at also tells the
	// reader which moment's Ticket Type catalog the headings reflect. The suite's
	// clock reads 2026-07-07 12:00 UTC, which is 07:00 in Guayaquil.
	info.says(t, "Info Fest")
	info.says(t, "2026-07-07 07:00")
	// The timezone named outright and identified as the Event's. Excel date cells
	// carry no timezone of their own, so this is the only place the file can say
	// which clock its dates were drawn on.
	info.says(t, "America/Guayaquil", "the Event's timezone")
	// The row count, so a reader can check nothing was truncated...
	info.says(t, "rows: 2")
	// ...and the currency the amounts are denominated in.
	info.says(t, "USD")
}

// TestSalesExportInfoSheetStatesTheStatusFilterPlainly is the point of the sheet.
//
// The Sales list's status filter defaults to `active`, so the DEFAULT download
// silently omits every reversed sale — the most common file in the product
// leaves out a whole category of sale. The Info sheet is what makes that honest
// rather than a trap, and its wording matters more than it looks: naming a
// filter value ("status: active") and leaving the reader to work out what it
// excluded is exactly the failure this line exists to prevent. It must SAY that
// reversed sales were excluded.
func TestSalesExportInfoSheetStatesTheStatusFilterPlainly(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Status Info Fest", "status-info-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)
	commitBatch(t, env, sessionID, eventID, "status-info-kept", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	undone := commitBatch(t, env, sessionID, eventID, "status-info-undone", []map[string]any{
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
	})
	undoBatch(t, env, sessionID, eventID, undone)

	// The default file: the reversed sale is missing from it, and the Info sheet
	// says so in words rather than leaving it to be discovered.
	_, data := downloadSalesExport(t, env, sessionID, eventID, "")
	def := openSalesExportInfo(t, data)
	def.says(t, "reversed sales", "excluded")
	// Not a query parameter echoed back at the reader, which would tell somebody
	// who never saw the screen nothing at all.
	def.silentAbout(t, "status=active")
	def.silentAbout(t, "status: active")

	// The other file says the other thing. An export that reaches the reversed
	// sales must not carry a line claiming they were left out.
	_, reversedData := downloadSalesExport(t, env, sessionID, eventID, "status=reversed")
	rev := openSalesExportInfo(t, reversedData)
	rev.says(t, "reversed sales only")
	for _, line := range rev.lines {
		if strings.Contains(line, "reversed sales") && strings.Contains(line, "excluded") {
			t.Fatalf("the reversed export claims reversed sales were excluded: %q", line)
		}
	}
	rev.silentAbout(t, "status=reversed")
}

// TestSalesExportInfoSheetRendersFiltersInProse: a reader who never saw the
// screen understands what produced the file.
//
// In WORDS, not query parameters: a Ticket Type by its name rather than its id,
// a date range as a range, a channel and a payment method as sentences. And the
// free-text search is stated as having happened WITHOUT its term — the search
// matches customer email and Tax ID number, so the term is very often one
// particular buyer's PII, and it is the searcher's input rather than a fact
// about any sale in the file. The spec makes the same call for the export's log
// line, where the term is recorded as a boolean; this is that decision applied
// to the artifact itself.
func TestSalesExportInfoSheetRendersFiltersInProse(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Prose Fest", "prose-fest")
	setEventTimezone(t, env, eventID, "America/Guayaquil")
	createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)
	vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 5000, 50)
	commitBatch(t, env, sessionID, eventID, "prose-batch", []map[string]any{
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": vipID, "quantity": 1, "payment_method": "transfer", "sold_at": "2026-07-03T15:00:00Z"},
	})

	const searchTerm = "bob@example.com"
	query := "ticket_type_id=" + vipID +
		"&sold_from=2026-07-01&sold_to=2026-07-05" +
		"&channel=import&source=direct&payment_method=transfer" +
		"&q=" + searchTerm
	resp, data := downloadSalesExport(t, env, sessionID, eventID, query)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}
	info := openSalesExportInfo(t, data)

	// The Ticket Type by the name the reader sees on screen, never by the id
	// that means nothing to them.
	info.says(t, "VIP")
	info.silentAbout(t, vipID)
	// The date range as a range, and said to be in the Event's clock — the same
	// zone the sold-at filter was interpreted in, so the stamp cannot contradict
	// the rows it selected.
	info.says(t, "2026-07-01", "2026-07-05")
	// The rest of the dimensions, each as a sentence.
	info.says(t, "import")
	info.says(t, "transfer")
	info.says(t, "direct")

	// A search happened, and the reader is told so — otherwise a file narrower
	// than its stated filters would be inexplicable.
	info.says(t, "search")
	// But not the term. It is very often a buyer's email or Tax ID, and the
	// person who typed it is not the person the file gets forwarded to.
	info.silentAbout(t, searchTerm)
	info.silentAbout(t, "bob@")

	// No query parameters anywhere: the sheet is prose, and the reader it is
	// written for never saw the URL.
	for _, raw := range []string{"ticket_type_id", "sold_from", "sold_to", "payment_method", "q="} {
		info.silentAbout(t, raw)
	}
}

// TestSalesExportInfoSheetWithNoFiltersIsCoherent: the plain download still
// explains itself. Nothing is left dangling — no empty "Ticket Type:" label
// under a "Filters applied" heading with nothing beneath it — and the status
// line, which is a filter whether or not anybody chose it, is still stated.
func TestSalesExportInfoSheetWithNoFiltersIsCoherent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Plain Fest", "plain-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)
	commitBatch(t, env, sessionID, eventID, "plain-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})

	resp, data := downloadSalesExport(t, env, sessionID, eventID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}
	info := openSalesExportInfo(t, data)

	info.says(t, "Plain Fest")
	info.says(t, "USD")
	info.says(t, "rows: 1")
	// An Event with no timezone of its own is read in UTC, and the sheet still
	// says whose clock that is rather than leaving the line out.
	info.says(t, "UTC", "the Event's timezone")
	// The one filter that is always in force is still stated.
	info.says(t, "reversed sales", "excluded")
	// And the ones nobody chose are absent rather than blank.
	for _, dangling := range []string{"ticket type:", "sales channel:", "payment method:", "sales source:", "sold between", "search"} {
		info.silentAbout(t, dangling)
	}
}

// TestSalesExportInfoRowCountMatchesTheRows: the count is what a reader checks
// nothing was truncated against, so it must be the number of rows the file
// actually carries rather than a total from somewhere else.
func TestSalesExportInfoRowCountMatchesTheRows(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Count Fest", "count-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)
	rows := []map[string]any{}
	for _, name := range []string{"ana", "bob", "caro", "dan", "eve"} {
		rows = append(rows, map[string]any{
			"customer_email": name + "@example.com", "customer_first_name": name, "customer_last_name": "Test",
			"ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z",
		})
	}
	commitBatch(t, env, sessionID, eventID, "count-batch", rows)

	// Filtered down, so the count cannot pass by accidentally matching the
	// Event's total.
	resp, data := downloadSalesExport(t, env, sessionID, eventID, "q=ana@example.com")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}
	if got := openSalesExport(t, data).dataRows; got != 1 {
		t.Fatalf("data rows = %d, want the one searched sale", got)
	}
	openSalesExportInfo(t, data).says(t, "rows: 1")

	// Unfiltered, it states the whole five.
	_, all := downloadSalesExport(t, env, sessionID, eventID, "")
	if got := openSalesExport(t, all).dataRows; got != 5 {
		t.Fatalf("unfiltered data rows = %d, want 5", got)
	}
	openSalesExportInfo(t, all).says(t, "rows: 5")

	// And a file that matched nothing says none, rather than leaving the reader
	// to wonder whether the count was simply forgotten.
	_, empty := downloadSalesExport(t, env, sessionID, eventID, "channel=online")
	if got := openSalesExport(t, empty).dataRows; got != 0 {
		t.Fatalf("online data rows = %d, want none", got)
	}
	openSalesExportInfo(t, empty).says(t, "rows: 0")
}

// --- the row cap and the log line (#241) ----------------------------------
//
// The export is generated synchronously and buffered in memory, so an unbounded
// Event would produce a request that hangs and then either times out at the
// proxy or takes the process's memory with it — on the busiest day, which is
// exactly when somebody reaches for this. The cap is what makes a synchronous
// export survivable, and the refusal is what makes the cap survivable: the
// person always has the filters as a lever, so the message has to name the
// count and point at them.

// withSalesExportCap lowers the export's row cap for one test and restores the
// deployed one afterwards.
//
// Reaching the real cap would mean seeding ten thousand and one Ticket Sales,
// which takes minutes and bloats the suite for a guarantee that is not about
// the number at all: what is under test is the behaviour AT the bound, and the
// bound is configuration. This mirrors withGlobalCeiling on the OTP ceiling,
// which lowers its number for exactly the same reason. That the deployed
// default is the Sale Import's own row limit is pinned separately, by
// TestSalesExportCapIsTheSaleImportRowLimit, so the two can never drift.
//
// The sales service is shared by the whole package and integration tests run
// serially, so swapping it here is safe in the same way the harness's clock
// swaps are.
func withSalesExportCap(t *testing.T, rows int) {
	t.Helper()
	original := sharedApp.SalesService.ExportRowCap()
	sharedApp.SalesService.WithExportRowCap(rows)
	t.Cleanup(func() { sharedApp.SalesService.WithExportRowCap(original) })
}

// salesExportRefusal reads the field errors out of a refused export's standard
// validation envelope, failing the test if the response is not one.
func salesExportRefusal(t *testing.T, resp *http.Response, data []byte) []platform.FieldError {
	t.Helper()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("refusal status=%d, want 400; body=%s", resp.StatusCode, string(data))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("refusal content-type = %q, want JSON — a refusal is not a file", ct)
	}
	var body struct {
		Error *struct {
			Code    string                          `json:"code"`
			Message string                          `json:"message"`
			Details platform.ValidationErrorDetails `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("decode refusal %s: %v", string(data), err)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("refusal = %s, want the standard VALIDATION_FAILED envelope", string(data))
	}
	if len(body.Error.Details.Fields) == 0 {
		t.Fatalf("refusal carries no fields: %s", string(data))
	}
	return body.Error.Details.Fields
}

// TestSalesExportRefusesAboveTheRowCap is the acceptance criterion: over the cap
// the request is refused with the standard validation envelope carrying the
// matched count, at the cap it succeeds, and narrowing the filters afterwards
// produces the download.
func TestSalesExportRefusesAboveTheRowCap(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Cap Fest", "cap-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)
	commitBatch(t, env, sessionID, eventID, "cap-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T11:00:00Z"},
		{"customer_email": "caro@example.com", "customer_first_name": "Caro", "customer_last_name": "Diaz", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-05T10:00:00Z"},
	})
	withSalesExportCap(t, 2)

	resp, data := downloadSalesExport(t, env, sessionID, eventID, "")
	fields := salesExportRefusal(t, resp, data)
	// Blamed on the filters as a whole, with a stable code beside the sentence —
	// no one filter is at fault, and which lever to pull is the person's call.
	if fields[0].Field != "filters" {
		t.Fatalf("refusal blames %q, want the filters", fields[0].Field)
	}
	if fields[0].Code != platform.CodeTooManyItems {
		t.Fatalf("refusal code = %q, want %q", fields[0].Code, platform.CodeTooManyItems)
	}
	message := fields[0].Message
	// The message is the whole reason a synchronous cap is acceptable: it says
	// how many matched, how many may travel at once, and what to do about it.
	for _, want := range []string{"3 matching sales", "up to 2", "Narrow"} {
		if !strings.Contains(message, want) {
			t.Fatalf("refusal message = %q, want it to contain %q", message, want)
		}
	}
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		t.Fatalf("a refused export still returned a workbook")
	}

	// Exactly at the cap succeeds: only ABOVE it is refused.
	resp, data = downloadSalesExport(t, env, sessionID, eventID, "sold_from=2026-07-01&sold_to=2026-07-01")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export at the cap status=%d body=%s", resp.StatusCode, string(data))
	}
	if got := openSalesExport(t, data).dataRows; got != 2 {
		t.Fatalf("narrowed export rows = %d, want the two sales at the cap", got)
	}
	if ct := resp.Header.Get("Content-Type"); ct != salesExportSpreadsheetType {
		t.Fatalf("narrowed export content-type = %q, want the spreadsheet type", ct)
	}

	// Every filter is a lever, not only the dates.
	if resp, data := downloadSalesExport(t, env, sessionID, eventID, "q=caro@example.com"); resp.StatusCode != http.StatusOK {
		t.Fatalf("export narrowed by search status=%d body=%s", resp.StatusCode, string(data))
	}

	// The refusal is decided on the FILTERED count, not the Event's: the whole
	// Event is over the cap while this file is not.
	if resp, data := downloadSalesExport(t, env, sessionID, eventID, "sold_from=2026-07-05&sold_to=2026-07-05"); resp.StatusCode != http.StatusOK {
		t.Fatalf("export narrowed to one day status=%d body=%s", resp.StatusCode, string(data))
	}
}

// TestSalesExportCapIsTheSaleImportRowLimit pins the deployed default to the
// Sale Import's row limit — the same constant, not a second number that happens
// to agree today. The system has one answer to how many sale rows travel in a
// file, and an export can never exceed what the importer would accept.
func TestSalesExportCapIsTheSaleImportRowLimit(t *testing.T) {
	if got := sharedApp.SalesService.ExportRowCap(); got != importfile.MaxRows {
		t.Fatalf("deployed export row cap = %d, want the Sale Import's MaxRows (%d)", got, importfile.MaxRows)
	}
}

// captureLogger records what a service logged during one test, so the log line
// can be read back the way a log aggregator would see it.
//
// It is safe for concurrent use: a streamed export logs from the server's
// goroutine while the test reads, and two exports may log at once.
type captureLogger struct {
	mu    sync.Mutex
	lines []capturedLine
}

type capturedLine struct {
	level string
	msg   string
	args  []any
}

func (l *captureLogger) Info(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, capturedLine{level: "info", msg: msg, args: args})
}

func (l *captureLogger) Warn(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, capturedLine{level: "warn", msg: msg, args: args})
}

func (l *captureLogger) Error(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, capturedLine{level: "error", msg: msg, args: args})
}

func (l *captureLogger) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = nil
}

// snapshot is a copy of what has been logged so far.
func (l *captureLogger) snapshot() []capturedLine {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]capturedLine(nil), l.lines...)
}

// rendered is everything logged, flattened — the message and every key and
// value — which is the shape the assertion "no buyer PII reaches the
// aggregator" needs.
func (l *captureLogger) rendered() string {
	var b strings.Builder
	for _, line := range l.snapshot() {
		b.WriteString(line.level)
		b.WriteString(" ")
		b.WriteString(line.msg)
		for _, a := range line.args {
			b.WriteString(fmt.Sprintf(" %v", a))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// only returns the single logged line whose message contains the fragment.
func (l *captureLogger) only(t *testing.T, fragment string) capturedLine {
	t.Helper()
	var found []capturedLine
	for _, line := range l.snapshot() {
		if strings.Contains(line.msg, fragment) {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("logged %d lines matching %q, want exactly one; log was:\n%s", len(found), fragment, l.rendered())
	}
	return found[0]
}

// arg reads a structured key's value off a log line.
func (line capturedLine) arg(t *testing.T, key string) any {
	t.Helper()
	for i := 0; i+1 < len(line.args); i += 2 {
		if k, ok := line.args[i].(string); ok && k == key {
			return line.args[i+1]
		}
	}
	t.Fatalf("log line %q carries no %q; args = %v", line.msg, key, line.args)
	return nil
}

// withSalesLogger captures what the sales service logs for one test.
func withSalesLogger(t *testing.T) *captureLogger {
	t.Helper()
	capture := &captureLogger{}
	sharedApp.SalesService.WithLogger(capture)
	t.Cleanup(func() {
		sharedApp.SalesService.WithLogger(platform.NewSlogLogger(sharedApp.Logger))
	})
	return capture
}

// TestSalesExportLogsWhoTookWhatAndNeverTheSearchTerm: this file is the largest
// concentration of buyer PII the product can emit, and without a deliberate log
// line there is no answering "who pulled the customer list" after the fact — a
// question that cannot be answered retroactively.
//
// The free-text search is logged as a BOOLEAN and never as its value. The search
// matches customer email and Tax ID number, so a support lookup for one buyer
// puts that buyer's PII into the filter, and a log aggregator typically has
// broader access and longer retention than the database. The Info sheet made
// exactly this call in #240; this is the same call in the same words.
func TestSalesExportLogsWhoTookWhatAndNeverTheSearchTerm(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Log Fest", "log-fest", 1000, 10)

	// An Online Sale carrying a Tax ID: the sharpest pair of values the search
	// reaches, and the pair that must never appear in a log line.
	begun := beginCheckoutOK(t, env, "test-org", "log-fest",
		taxIDCheckoutBody("buyer@example.com", "Bea", "Ruiz", "ruc", naturalRUC,
			map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if confirmed := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved"); confirmed.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirmed.Status)
	}

	logs := withSalesLogger(t)

	resp, data := downloadSalesExport(t, env, sessionID, eventID,
		"channel=online&q="+url.QueryEscape("buyer@example.com"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data))
	}

	line := logs.only(t, "sales export")
	for _, key := range []string{"member_id", "organization_id", "event_id"} {
		if got, _ := line.arg(t, key).(string); got == "" {
			t.Fatalf("log line %s is blank; it is how the file is traced back", key)
		}
	}
	if got, _ := line.arg(t, "event_id").(string); got != eventID {
		t.Fatalf("log event_id = %q, want the exported Event %q", got, eventID)
	}
	if got := line.arg(t, "row_count"); got != 1 {
		t.Fatalf("log row_count = %v, want the one exported row", got)
	}
	// The structural filters, which say nothing about any one buyer.
	if got, _ := line.arg(t, "channel").(string); got != "online" {
		t.Fatalf("log channel = %q, want online", got)
	}
	if got, _ := line.arg(t, "status").(string); got != "active" {
		t.Fatalf("log status = %q, want the resolved default", got)
	}
	// The search: THAT one happened, never what it was.
	if got := line.arg(t, "search"); got != true {
		t.Fatalf("log search = %v, want the boolean true", got)
	}
	if strings.Contains(logs.rendered(), "buyer@example.com") {
		t.Fatalf("the buyer's email reached the log:\n%s", logs.rendered())
	}

	// The same again with the Tax ID number as the search term, because that is
	// the other thing the search matches and the more damaging of the two.
	logs.reset()
	resp, data = downloadSalesExport(t, env, sessionID, eventID, "q="+url.QueryEscape(naturalRUC))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tax id search export status=%d body=%s", resp.StatusCode, string(data))
	}
	if got := logs.only(t, "sales export").arg(t, "search"); got != true {
		t.Fatalf("log search = %v, want the boolean true", got)
	}
	rendered := logs.rendered()
	for _, secret := range []string{naturalRUC, "buyer@example.com", "Ruiz"} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("%q reached the log:\n%s", secret, rendered)
		}
	}

	// An export that matched nothing still says so: the record of who asked is
	// not conditional on the answer being non-empty.
	logs.reset()
	if resp, _ := downloadSalesExport(t, env, sessionID, eventID, "channel=in_person"); resp.StatusCode != http.StatusOK {
		t.Fatalf("empty export status=%d", resp.StatusCode)
	}
	if got := logs.only(t, "sales export").arg(t, "row_count"); got != 0 {
		t.Fatalf("empty export logged row_count = %v, want 0", got)
	}
	if got := logs.only(t, "sales export").arg(t, "search"); got != false {
		t.Fatalf("unsearched export logged search = %v, want false", got)
	}
}

// THE SALE CORRECTION LINKAGE (#352, ADR 0050). A corrected sale is a reversed
// row that says which sale replaced it, and the replacement says which sale it
// corrects — each by the other's Sale Confirmation reference, the value an
// accountant can follow in either direction within the same file. The route
// column tells the three staff levers apart: `import_undo` for a whole batch,
// `staff_reversal` for one sale reversed on its own, `correction` for one sale
// replaced — all of them `staff` in the database, which tells the reader only
// that their own Organization acted.
func TestSalesExportCarriesTheCorrectionLinkage(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Linkage Fest", "linkage-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)
	first := commitBatch(t, env, sessionID, eventID, "linkage-1", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "cai@example.com", "customer_first_name": "Cai", "customer_last_name": "Wu", "ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	ana := saleRowByEmail(t, env, sessionID, eventID, "ana@example.com", "active")
	bob := saleRowByEmail(t, env, sessionID, eventID, "bob@example.com", "active")

	// Ana's is corrected (to a new email), Bob's is reversed on its own, and
	// then the whole batch is undone — which sweeps Cai's and leaves the other
	// two exactly as their own routes left them.
	corrected := correctImportedSaleOK(t, env, sessionID, eventID, ana.ID,
		correctionBody("anna@example.com", "Anna", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z"))
	reverseImportedSaleOK(t, env, sessionID, eventID, bob.ID)
	// The undo comes later, as it would: on the fixed clock it would share
	// Bob's instant, and the instant is what tells the two levers apart.
	later := env.fixedClock.Add(time.Hour)
	sharedApp.SalesService.WithClock(func() time.Time { return later })
	undoBatch(t, env, sessionID, eventID, first)
	sharedApp.SalesService.WithClock(func() time.Time { return fixedClock })

	reversed := reversedSalesExport(t, env, sessionID, eventID)
	if reversed.dataRows != 3 {
		t.Fatalf("reversed rows = %d, want Ana, Bob and Cai", reversed.dataRows)
	}
	anaRow := reversed.rowOf(t, "ana@example.com")
	if got := reversed.value(t, anaRow, "reversed_by"); got != "correction" {
		t.Errorf("corrected sale reversed_by = %q, want correction", got)
	}
	if got := reversed.value(t, anaRow, "corrected_by"); got != corrected.ReplacementConfirmationRef {
		t.Errorf("corrected_by = %q, want the replacement's reference %s", got, corrected.ReplacementConfirmationRef)
	}
	reversed.blank(t, anaRow, "corrects")

	bobRow := reversed.rowOf(t, "bob@example.com")
	if got := reversed.value(t, bobRow, "reversed_by"); got != "staff_reversal" {
		t.Errorf("singly reversed sale reversed_by = %q, want staff_reversal — even though its batch was undone later", got)
	}
	reversed.blank(t, bobRow, "corrected_by")
	reversed.blank(t, bobRow, "corrects")

	caiRow := reversed.rowOf(t, "cai@example.com")
	if got := reversed.value(t, caiRow, "reversed_by"); got != "import_undo" {
		t.Errorf("batch-undone sale reversed_by = %q, want import_undo", got)
	}
	reversed.blank(t, caiRow, "corrected_by")

	// The replacement is active, and points back.
	resp, data := downloadSalesExport(t, env, sessionID, eventID, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("active export status=%d body=%s", resp.StatusCode, string(data))
	}
	active := openSalesExport(t, data)
	if active.dataRows != 1 {
		t.Fatalf("active rows = %d, want only the replacement", active.dataRows)
	}
	annaRow := active.rowOf(t, "anna@example.com")
	if got := active.value(t, annaRow, "corrects"); got != corrected.ReversedConfirmationRef {
		t.Errorf("corrects = %q, want the corrected sale's reference %s", got, corrected.ReversedConfirmationRef)
	}
	active.blank(t, annaRow, "corrected_by")
	active.blank(t, annaRow, "reversed_by")
}
