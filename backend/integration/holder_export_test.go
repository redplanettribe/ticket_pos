package integration

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The HOLDER EXPORT (#529, parent #518, ADR 0065): an Org Admin or Event Owner
// presses Download among the Holder List's filters and receives an .xlsx of who
// is coming, mirroring exactly the view that was on screen.
//
// ITS OWN FILE RATHER THAN MORE OF holder_list_test.go, which is already 3,000
// lines about what is ON the roster. This one is about the ARTIFACT: its
// columns, its sheets, what it refuses to say, and the record it leaves behind.
//
// THE SEAM IS HTTP AND THE ASSERTIONS ARE ON THE FILE, exactly as the Sales
// Export's are: the workbook is fetched over the wire and read back with
// excelize, because what matters is what the person who downloads it observes.
// The one deliberate exception is the disclosure test, which goes UNDER excelize
// and searches the raw zip — see it for why.

// holderExportSheet is the data sheet's name — deliberately not "Sales", so a
// roster uploaded back as a Sale Import cannot be parsed as one.
const holderExportSheet = "Ticket Holders"

// holderExportPath is the download's address: the Holder List's own path with
// /export on it, so the file is named after the list it mirrors.
func holderExportPath(eventID string) string {
	return holderListPath(eventID) + "/export"
}

// downloadHolderExport GETs the Holder Export with the given raw query string
// (the Holder List's own filter parameters), returning the response and its
// body.
func downloadHolderExport(t *testing.T, env *testEnv, sessionID, eventID, query string) (*http.Response, []byte) {
	t.Helper()
	target := env.server.URL + holderExportPath(eventID)
	if query != "" {
		target += "?" + query
	}
	req, err := http.NewRequest(http.MethodGet, target, nil)
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

// holderExportFile is a downloaded roster, indexed by its header row. Every
// accessor takes a column HEADING rather than a letter, which is what keeps
// these assertions stable as columns are added.
type holderExportFile struct {
	f        *excelize.File
	header   []string
	index    map[string]int
	dataRows int
	// raw is the .xlsx bytes, kept for the assertions that must go under
	// excelize and read the zip itself.
	raw []byte
}

// openHolderExport downloads the export and indexes its data sheet, failing on
// any refusal — for the tests whose subject is what is IN the file.
func openHolderExport(t *testing.T, env *testEnv, sessionID, eventID, query string) *holderExportFile {
	t.Helper()
	resp, data := downloadHolderExport(t, env, sessionID, eventID, query)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holder export %q status=%d body=%s", query, resp.StatusCode, string(data))
	}
	if ct := resp.Header.Get("Content-Type"); ct != salesExportSpreadsheetType {
		t.Fatalf("content-type = %q, want the .xlsx type", ct)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open .xlsx: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	rows, err := f.GetRows(holderExportSheet)
	if err != nil {
		t.Fatalf("get rows from %q: %v", holderExportSheet, err)
	}
	if len(rows) == 0 {
		t.Fatalf("export has no header row")
	}
	index := map[string]int{}
	for i, h := range rows[0] {
		index[h] = i
	}
	return &holderExportFile{f: f, header: rows[0], index: index, dataRows: len(rows) - 1, raw: data}
}

// value returns a cell as a reader of the workbook sees it — the number format
// applied, so a date cell reads back as the date it renders.
func (s *holderExportFile) value(t *testing.T, row int, heading string) string {
	t.Helper()
	col, ok := s.index[heading]
	if !ok {
		t.Fatalf("no %q column in header %v", heading, s.header)
	}
	// +1 for the 1-based column, +2 for the 1-based row under the header.
	ref, err := excelize.CoordinatesToCellName(col+1, row+2)
	if err != nil {
		t.Fatalf("cell name: %v", err)
	}
	value, err := s.f.GetCellValue(holderExportSheet, ref)
	if err != nil {
		t.Fatalf("get %s on row %d: %v", heading, row, err)
	}
	return value
}

// column reads one heading down every data row, which is how a test says "who
// is on this roster" rather than "how many".
func (s *holderExportFile) column(t *testing.T, heading string) []string {
	t.Helper()
	out := make([]string, 0, s.dataRows)
	for row := 0; row < s.dataRows; row++ {
		out = append(out, s.value(t, row, heading))
	}
	return out
}

// info is the Info sheet flattened to one string, for assertions about what it
// SAYS rather than about which row says it.
func (s *holderExportFile) info(t *testing.T) string {
	t.Helper()
	rows, err := s.f.GetRows("Info")
	if err != nil {
		t.Fatalf("get Info rows: %v", err)
	}
	var b strings.Builder
	for _, row := range rows {
		for _, cell := range row {
			b.WriteString(cell)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// holderExportRefusal reads the field errors out of a refused export's standard
// validation envelope, failing the test if the response is not one.
func holderExportRefusal(t *testing.T, resp *http.Response, data []byte) []platform.FieldError {
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

// withHolderExportCap lowers the Holder Export's row cap for one test and
// restores the deployed one afterwards.
//
// Reaching the real cap would mean seeding fifty thousand and one Tickets, which
// takes minutes and buys nothing: what is under test is the behaviour AT the
// bound, and the bound is configuration. This mirrors withSalesExportCap next
// door, and the deployed default is pinned separately by
// TestHolderExportCapIsItsOwnNumber so the two can never quietly become one.
func withHolderExportCap(t *testing.T, rows int) {
	t.Helper()
	original := sharedApp.CatalogService.HolderExportRowCap()
	sharedApp.CatalogService.WithHolderExportRowCap(rows)
	t.Cleanup(func() { sharedApp.CatalogService.WithHolderExportRowCap(original) })
}

// withCatalogLogger captures what the catalog service logs for one test, so the
// Holder Export's audit line can be read back the way a log aggregator would see
// it. The sibling of withSalesLogger, over the other service.
func withCatalogLogger(t *testing.T) *captureLogger {
	t.Helper()
	capture := &captureLogger{}
	sharedApp.CatalogService.WithLogger(capture)
	t.Cleanup(func() {
		sharedApp.CatalogService.WithLogger(platform.NewSlogLogger(sharedApp.Logger))
	})
	return capture
}

// THE FILE MIRRORS THE VIEW: the same filters and the same order.
//
// It is asserted against the LIST's own answer to the same query string rather
// than against a hand-written expectation, which is the only form of this test
// that cannot drift: the promise is not "the file contains these four people",
// it is "the file is this screen", and the screen is the thing to compare it to.
func TestTheHolderExportMirrorsTheViewsFiltersAndSort(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	f := newHolderFilterFixture(t, env)

	for _, query := range []string{
		"",
		"?channel=online",
		"?ticket_type_id=" + f.vipID,
		"?sold_from=2026-07-01&sold_to=2026-07-01",
		"?sort=buyer&dir=desc",
		"?channel=online&sort=buyer&dir=desc",
	} {
		page := holderRoster(t, env, f.sessionID, f.eventID, query)
		file := openHolderExport(t, env, f.sessionID, f.eventID, strings.TrimPrefix(query, "?"))

		want := holderBuyers(page)
		if got := file.column(t, "customer_email"); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: file = %v, screen = %v — the file must be the view, in the view's order", query, got, want)
		}
		if file.dataRows != page.Pagination.Total {
			t.Errorf("%q: file has %d rows, the view holds %d", query, file.dataRows, page.Pagination.Total)
		}
	}
}

// ONE ROW PER TICKET, the agreed columns, and NO MONEY COLUMNS OF ANY KIND.
//
// The money half is the acceptance criterion and the reason this file exists
// separately from the Sales Export. A roster repeats a sale's details once per
// Ticket, so an amount on it would be summed several times over — and a file
// with money on it can be forwarded as a financial document, which this one must
// never be mistaken for.
func TestTheHolderExportIsOneRowPerTicketAndCarriesNoMoney(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Roster Fest", "roster-fest")
	ticketTypeID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2000, 50)
	commitBatch(t, env, sessionID, eventID, "roster-batch", []map[string]any{{
		"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
		"ticket_type_id": ticketTypeID, "quantity": 3, "payment_method": "cash",
		"sold_at": "2026-07-01T10:00:00Z",
	}})

	file := openHolderExport(t, env, sessionID, eventID, "")

	// ONE ROW PER TICKET and not per sale: three tickets, three rows.
	if file.dataRows != 3 {
		t.Fatalf("data rows = %d, want one per Ticket (3)", file.dataRows)
	}
	want := []string{
		"confirmation_ref", "sold_at", "channel", "ticket_type", "ticket_ordinal",
		"customer_first_name", "customer_last_name", "customer_email",
		"assignment_state", "never_accepted",
		"holder_first_name", "holder_last_name", "holder_email",
	}
	if !reflect.DeepEqual(file.header, want) {
		t.Fatalf("header = %v, want %v", file.header, want)
	}
	// The ordinal is the one thing telling two Tickets of one sale line apart,
	// and it is here precisely because a row is a Ticket — the Sales Export's
	// per-Ticket sheet omits it, where two identical rows are two Tickets that
	// answered alike.
	if got, want := file.column(t, "ticket_ordinal"), []string{"1", "2", "3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ordinals = %v, want %v", got, want)
	}

	// NO MONEY, asserted on the header rather than on a cell: the failure this
	// guards against is a column being ADDED, and a cell assertion cannot see one.
	for _, heading := range file.header {
		lower := strings.ToLower(heading)
		for _, forbidden := range []string{"amount", "net_proceeds", "net proceeds", "currency", "price", "cents"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("header %q looks like money (%q); the Holder Export carries none", heading, forbidden)
			}
		}
	}
}

// THE QUESTION AND OPTION COLUMNS COME FROM THE SHARED BUILDER (#520), not a
// second implementation.
//
// The distinctive behaviour is asserted rather than the wiring: a
// multiple-choice question contributes NO column of its own and one per OPTION,
// keyed by the Option's identity — so correcting an Option's wording MOVES A
// HEADING and forks no column, and the Tickets that chose it before the
// correction sit in the same column as the ones that chose it after. A second
// implementation would have to reproduce all of that to pass, which is the
// point.
func TestTheHolderExportQuestionColumnsComeFromTheSharedBuilder(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, ticketIDs := answeredFixture(t, env)

	sizes := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "multi_choice", "required": true,
		"option_labels": []string{"S", "Mediun", "L"},
	})
	flight := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "Flight number", "kind": "short_text", "required": false,
	})
	medium := sizes.Options[1]

	putAnswer(t, env, sessionID, eventID, ticketIDs[0], sizes.ID,
		map[string]any{"option_ids": []string{medium.ID}})
	putAnswer(t, env, sessionID, eventID, ticketIDs[0], flight.ID,
		map[string]any{"text": "LA 1234"})

	file := openHolderExport(t, env, sessionID, eventID, "")

	// The question itself takes no column; its Options each take one.
	for _, heading := range file.header {
		if heading == "T-shirt size" {
			t.Fatalf("the multiple-choice question has a column of its own: %v", file.header)
		}
	}
	for _, heading := range []string{"S", "Mediun", "L", "Flight number"} {
		if _, ok := file.index[heading]; !ok {
			t.Fatalf("no %q column in header %v", heading, file.header)
		}
	}
	// An ANSWERED multiple-choice question writes TRUE under what it chose and
	// FALSE under the rest — the FALSEs are what make the column countable.
	if got := file.value(t, 0, "Mediun"); got != "TRUE" {
		t.Errorf("chosen Option = %q, want TRUE", got)
	}
	if got := file.value(t, 0, "S"); got != "FALSE" {
		t.Errorf("unchosen Option of an answered question = %q, want FALSE", got)
	}
	// An UNANSWERED one leaves its whole block blank: an Outstanding Answer is a
	// debt, not a FALSE.
	if got := file.value(t, 1, "Mediun"); got != "" {
		t.Errorf("unanswered Option cell = %q, want blank", got)
	}
	if got := file.value(t, 0, "Flight number"); got != "LA 1234" {
		t.Errorf("short-text answer = %q, want the text", got)
	}

	// THE RENAME: `Mediun` becomes `Medium`. The heading moves and the column
	// does not fork, because the columns are keyed by the Option's identity.
	correctOptionLabel(t, env, medium.ID, "Medium")
	renamed := openHolderExport(t, env, sessionID, eventID, "")
	if _, ok := renamed.index["Mediun"]; ok {
		t.Errorf("the old wording still has a column: %v", renamed.header)
	}
	if got := renamed.value(t, 0, "Medium"); got != "TRUE" {
		t.Errorf("after the rename the Ticket's choice = %q, want TRUE in the SAME column", got)
	}
	if len(renamed.header) != len(file.header) {
		t.Errorf("the rename changed the column count: %v then %v", file.header, renamed.header)
	}
}

// AN UNACCEPTED HOLDER'S ADDRESS IS ABSENT FROM THE RAW BYTES OF THE WHOLE
// WORKBOOK. This is the acceptance criterion with the most at stake and the
// assertion is deliberately made the hard way.
//
// A CELL-LEVEL ASSERTION WOULD NOT DO IT. An .xlsx is a ZIP, and excelize puts
// every text cell's value in xl/sharedStrings.xml; an address could reach that
// table through a cell later deleted, through a cached value, through a defined
// name or through a comment, and the person it belongs to would be no less
// exposed for it — a recipient can open the archive with a text editor. So this
// unzips the workbook and searches EVERY entry. Searching the file's bytes as
// downloaded would find nothing whatever it said, because they are compressed.
//
// TWO ADDRESSES, TWO WAYS OF NOT BEING THERE. `diego@` was typed by the buyer
// and never accepted, so ADR 0047 refuses to disclose it and it is still in the
// database. `erika@` was typed, never accepted, and then taken by the retention
// purge, so it is gone from the database too — and this asserts that nothing
// downstream (a join, an audit trail, a cached name) puts it back.
//
// THE CONTROL AT THE END IS LOAD-BEARING: an accepted Holder's address IS in the
// file, so a workbook that was simply empty could not pass this test.
func TestTheHolderExportNeverCarriesAnUnacceptedAddress(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Disclosure Fest", "disclosure-fest")
	scheduleEvent(t, env, sessionID, eventID, "Disclosure Fest", "disclosure-fest",
		env.fixedClock.Add(30*24*time.Hour))
	ticketTypeID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2000, 50)

	// Committed while assignment is DARK so all four Tickets start unassigned: a
	// Sale Import with the flag open hands the buyer its first Ticket by
	// presumption (ADR 0055), which would start the roster with an `accepted` row
	// nobody chose.
	commitBatch(t, env, sessionID, eventID, "disclosure-batch", []map[string]any{{
		"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
		"ticket_type_id": ticketTypeID, "quantity": 4, "payment_method": "cash",
		"sold_at": "2026-07-01T10:00:00Z",
	}})
	var saleID string
	if err := env.db.QueryRow(`
		SELECT id FROM ticket_sales WHERE event_id = $1 AND customer_email = 'ana@example.com'
	`, eventID).Scan(&saleID); err != nil {
		t.Fatalf("read Ana's Ticket Sale: %v", err)
	}
	tickets := ticketIDsOfSale(t, env, saleID)

	enableTicketAssignment(t)
	ana := customerSignIn(t, env, "ana@example.com")
	// `accepted`: the whole walk — named, mailed, clicked. This one MAY be shown.
	assignTicketOK(t, env, ana, saleID, tickets[1], "carla@example.com")
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))
	// `assigned`: named and waiting. This one may NOT be shown.
	assignTicketOK(t, env, ana, saleID, tickets[2], "diego@example.com")
	// Named, then purged. This one may not be shown either, and its address is
	// gone from the database as well.
	assignTicketOK(t, env, ana, saleID, tickets[3], "erika@example.com")
	stageHolderAddressPurge(t, env, tickets[3])

	file := openHolderExport(t, env, sessionID, eventID, "")

	// The rows read as the screen reads: the word and no name.
	states := file.column(t, "assignment_state")
	if got, want := states, []string{"unassigned", "accepted", "assigned", "assigned"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("assignment states = %v, want %v — a purged Ticket reads `assigned`, never a fourth word", got, want)
	}
	if got, want := file.column(t, "never_accepted"),
		[]string{"FALSE", "FALSE", "FALSE", "TRUE"}; !reflect.DeepEqual(got, want) {
		t.Errorf("never_accepted = %v, want %v", got, want)
	}

	for _, secret := range []string{"diego@example.com", "erika@example.com"} {
		assertAbsentFromExport(t, file.raw, secret)
	}
	// THE CONTROL. Without it a broken export that produced an empty workbook
	// would pass every assertion above.
	assertPresentInExport(t, file.raw, "carla@example.com")
}

// assertAbsentFromExport unzips an .xlsx and fails if any entry contains the
// needle. See the test above for why this goes under excelize.
func assertAbsentFromExport(t *testing.T, data []byte, needle string) {
	t.Helper()
	for name, content := range exportZipEntries(t, data) {
		if strings.Contains(content, needle) {
			t.Fatalf("%q appears in workbook entry %s — an unaccepted address must be nowhere in the file", needle, name)
		}
	}
}

// assertPresentInExport is assertAbsentFromExport's control.
func assertPresentInExport(t *testing.T, data []byte, needle string) {
	t.Helper()
	for _, content := range exportZipEntries(t, data) {
		if strings.Contains(content, needle) {
			return
		}
	}
	t.Fatalf("%q appears in NO workbook entry; the absence assertions are vacuous", needle)
}

// exportZipEntries reads every entry of an .xlsx zip into memory, by name, and
// insists xl/sharedStrings.xml is among them — that is where every text cell's
// value lives, so a search that missed it would miss the thing it is looking
// for.
func exportZipEntries(t *testing.T, data []byte) map[string]string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open .xlsx as a zip: %v", err)
	}
	entries := map[string]string{}
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open %s: %v", file.Name, err)
		}
		content, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", file.Name, err)
		}
		entries[file.Name] = string(content)
	}
	if _, ok := entries["xl/sharedStrings.xml"]; !ok {
		t.Fatalf("workbook has no xl/sharedStrings.xml; the search would miss every text cell")
	}
	return entries
}

// THE INFO SHEET explains the file to somebody who did not download it: the
// Event, the moment, the timezone named as the EVENT's, the row count, and the
// filters in words.
//
// It is a SEPARATE SHEET and comes FIRST, and the data sheet keeps row 1 as its
// header — rows above a header break select-all, autofilter and pivot source
// ranges, which is the whole reason the stamp is not a preamble.
func TestTheHolderExportInfoSheetExplainsTheFile(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	f := newHolderFilterFixture(t, env)

	file := openHolderExport(t, env, f.sessionID, f.eventID,
		"channel=online&ticket_type_id="+f.gaID+"&sold_from=2026-07-01&sold_to=2026-07-01&sort=buyer&dir=desc")

	if sheets := file.f.GetSheetList(); !reflect.DeepEqual(sheets, []string{"Info", holderExportSheet}) {
		t.Fatalf("sheets = %v, want Info first then %q", sheets, holderExportSheet)
	}
	if file.header[0] != "confirmation_ref" {
		t.Fatalf("row 1 of the data sheet is not the header: %v", file.header)
	}

	text := file.info(t)
	for _, want := range []string{
		"Holder Export",
		"Event: Filter Fest",
		"Generated: ",
		"America/New_York",
		"the Event's timezone",
		"Rows: ",
		"Sold between 2026-07-01 and 2026-07-01",
		"only Tickets of GA",
		"online sales only",
		"Sorted by the buyer's name, descending",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("Info sheet does not say %q; it said:\n%s", want, text)
		}
	}
	// The row count is the rows the file carries, so a reader can check nothing
	// was truncated between the screen and the file.
	if !strings.Contains(text, "Rows: 1 Ticket\n") {
		t.Errorf("Info sheet does not state this file's one row; it said:\n%s", text)
	}
}

// A FILTER THAT WAS IGNORED IS DESCRIBED NOWHERE — not on the Info sheet and not
// in the audit line.
//
// This is the criterion the whole honoured-filters mechanism exists for. The
// Holder List IGNORES a filter belonging to a dark feature rather than refusing
// it, so a request can name `outstanding=true` on a build with Ticket Questions
// closed and be answered with the WHOLE ROSTER. A file whose cover sheet then
// said "every Ticket in this file still owes an answer" would have somebody read
// a complete roster believing it was a filtered one, and act on the difference.
//
// Ticket Assignment is opened and Ticket Questions is left DARK, which is a real
// deployment state: the two flags are separate on purpose.
func TestTheHolderExportNeverDescribesAnIgnoredFilter(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	f := newHolderFilterFixture(t, env)

	logs := withCatalogLogger(t)
	// Every parameter here belongs to Ticket Questions, which is dark: the
	// outstanding filter, a named question, and the `owes` sort.
	file := openHolderExport(t, env, f.sessionID, f.eventID,
		"outstanding=true&question_id=11111111-1111-4111-8111-111111111111&sort=owes&dir=desc")

	// The roster came back WHOLE, which is the behaviour under test — the filter
	// was ignored, not refused.
	if file.dataRows != 4 {
		t.Fatalf("data rows = %d, want the whole roster (4): the dark filter should have been ignored", file.dataRows)
	}
	text := file.info(t)
	for _, absent := range []string{"Outstanding Answers only", "Owing one named question", "how many required answers"} {
		if strings.Contains(text, absent) {
			t.Errorf("Info sheet claims %q, which this build never applied; it said:\n%s", absent, text)
		}
	}
	// And it says what IS true: nothing narrowed this file.
	if !strings.Contains(text, "No filters were applied") {
		t.Errorf("Info sheet does not say the file is the whole roster; it said:\n%s", text)
	}
	// The dropped sort is not described either: `sort=owes&dir=desc` came back in
	// the DEFAULT order, and half a sort is not a sort.
	if !strings.Contains(text, "Sorted by when the sale was made, oldest first (the default order)") {
		t.Errorf("Info sheet does not state the default order it actually used; it said:\n%s", text)
	}

	// THE AUDIT LINE MAKES THE SAME CALL, from the same honoured set.
	line := logs.only(t, "holder export")
	if got := line.arg(t, "outstanding"); got != false {
		t.Errorf("log outstanding = %v, want false — the filter was dropped", got)
	}
	if got, _ := line.arg(t, "question_id").(string); got != "" {
		t.Errorf("log question_id = %q, want blank — the filter was dropped", got)
	}
	if got, _ := line.arg(t, "sort").(string); got != "" {
		t.Errorf("log sort = %q, want blank — the sort was dropped", got)
	}
}

// A SEARCH IS REPORTED AS APPLIED WITHOUT THE TERM APPEARING ANYWHERE — in the
// file or in the logs.
//
// The term searched for here matches NOBODY on purpose. A term that matched
// would legitimately appear in the file as a data value — searching for a
// buyer's address returns that buyer's row, address and all — and the assertion
// would prove nothing about whether the term was RECORDED. With a term that
// matches nothing, any occurrence of it in the workbook is the search itself
// having been written down.
func TestTheHolderExportReportsASearchAndNeverTheTerm(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	f := newHolderFilterFixture(t, env)

	const term = "zzz-nobody-matches-this@example.com"
	logs := withCatalogLogger(t)
	file := openHolderExport(t, env, f.sessionID, f.eventID, "q="+url.QueryEscape(term))

	if file.dataRows != 0 {
		t.Fatalf("data rows = %d, want 0 — the term was chosen to match nobody", file.dataRows)
	}
	if text := file.info(t); !strings.Contains(text, "A search was applied") {
		t.Fatalf("Info sheet does not report the search; it said:\n%s", text)
	}
	assertAbsentFromExport(t, file.raw, term)

	line := logs.only(t, "holder export")
	if got := line.arg(t, "search"); got != true {
		t.Errorf("log search = %v, want the boolean true", got)
	}
	if strings.Contains(logs.rendered(), term) {
		t.Fatalf("the search term reached the log:\n%s", logs.rendered())
	}

	// And a real term, over a roster it does narrow: still a boolean, still
	// unquoted. This is the shape a support lookup takes, and it is where a
	// customer's address would leak.
	logs.reset()
	narrowed := openHolderExport(t, env, f.sessionID, f.eventID, "q="+url.QueryEscape("vip-online@example.com"))
	if narrowed.dataRows != 1 {
		t.Fatalf("searched roster = %d rows, want 1", narrowed.dataRows)
	}
	if strings.Contains(narrowed.info(t), "vip-online@example.com") {
		t.Errorf("the Info sheet quotes the search term:\n%s", narrowed.info(t))
	}
	if strings.Contains(logs.rendered(), "vip-online@example.com") {
		t.Fatalf("the search term reached the log:\n%s", logs.rendered())
	}
}

// THE DATA SHEET IS NOT NAMED "Sales", and no sheet of the workbook is.
//
// The Sale Import parser selects its sheet by that name, so a roster named that
// way could be uploaded back as an import — inserting every sale a second time
// and re-emailing every buyer. This is the third file in the product to carry
// that catch and it is asserted at the same seam as the other two.
func TestTheHolderExportDataSheetIsNeverNamedSales(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	f := newHolderFilterFixture(t, env)

	file := openHolderExport(t, env, f.sessionID, f.eventID, "")
	for _, sheet := range file.f.GetSheetList() {
		if sheet == "Sales" {
			t.Fatalf("a sheet is named %q; the Sale Import parser selects by that name", sheet)
		}
	}
	if got := file.f.GetSheetList()[1]; got != holderExportSheet {
		t.Fatalf("data sheet = %q, want %q", got, holderExportSheet)
	}
	// The filename says holders, so it can never be confused in a Downloads
	// folder with the "sales-…" file sitting beside it.
	resp, _ := downloadHolderExport(t, env, f.sessionID, f.eventID, "")
	if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, "holders-filter-fest-") {
		t.Fatalf("content-disposition = %q, want a holders-{slug}-{date}.xlsx filename", got)
	}
}

// OVER THE CAP THE REQUEST IS REFUSED, NAMING HOW MANY TICKETS MATCHED, and
// nothing is truncated. Exactly the cap still succeeds.
//
// The refusal is the feature: generation is synchronous and the workbook is
// buffered whole, so an unbounded Event is a request that hangs and then takes
// something down — on the busiest day of the Event, which is exactly when
// somebody reaches for this. A TRUNCATED file would be the one failure nobody
// can detect from the file itself, so the answer is "narrow your filters", and
// the message names the count because that is how the person knows how much
// narrower to go.
func TestTheHolderExportRefusesOverTheCapRatherThanTruncating(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	f := newHolderFilterFixture(t, env)

	withHolderExportCap(t, 3)
	resp, data := downloadHolderExport(t, env, f.sessionID, f.eventID, "")
	fields := holderExportRefusal(t, resp, data)
	if len(fields) != 1 {
		t.Fatalf("refusal carries %d fields, want one: %+v", len(fields), fields)
	}
	// The field is `filters` and not any one parameter: no single filter is at
	// fault, and blaming sold_from would be wrong for somebody whose lever is the
	// channel.
	if fields[0].Field != "filters" {
		t.Errorf("refusal field = %q, want filters", fields[0].Field)
	}
	if fields[0].Code != platform.CodeTooManyItems {
		t.Errorf("refusal code = %q, want %q", fields[0].Code, platform.CodeTooManyItems)
	}
	// THE COUNT IS THE MATCHED TOTAL and not the cap: the roster holds four, and
	// a person told only "too many" cannot tell whether to narrow a little or a
	// lot.
	if !strings.Contains(fields[0].Message, "4 tickets") {
		t.Errorf("refusal message = %q, want it to name the 4 matching tickets", fields[0].Message)
	}
	if !strings.Contains(fields[0].Message, "3") {
		t.Errorf("refusal message = %q, want it to name the cap", fields[0].Message)
	}

	// NOTHING WAS TRUNCATED: no file at all came back.
	if strings.HasPrefix(string(data), "PK") {
		t.Fatalf("a refusal returned a workbook; it must build nothing")
	}

	// THE FILTERS ARE THE LEVER, and they work: the same Event narrowed to one
	// channel is under the cap and downloads.
	if file := openHolderExport(t, env, f.sessionID, f.eventID, "channel=online"); file.dataRows != 2 {
		t.Fatalf("narrowed export = %d rows, want 2", file.dataRows)
	}

	// EXACTLY THE CAP SUCCEEDS. The bound is inclusive, which is the off-by-one
	// worth a test of its own.
	withHolderExportCap(t, 4)
	file := openHolderExport(t, env, f.sessionID, f.eventID, "")
	if file.dataRows != 4 {
		t.Fatalf("at exactly the cap the export has %d rows, want 4", file.dataRows)
	}
}

// THE CAP IS ITS OWN NUMBER WITH ITS OWN REASON — 50,000 Tickets, a synchronous
// generation ceiling — and deliberately NOT the Sales Export's, whose reason is
// symmetry with what a Sale Import would take back and does not transfer to a
// file nobody imports.
//
// This pins the two apart. Referencing the other constant is the obvious wrong
// move, and it would pass every other test in this file.
func TestTheHolderExportCapIsItsOwnNumber(t *testing.T) {
	if got := sharedApp.CatalogService.HolderExportRowCap(); got != 50_000 {
		t.Fatalf("deployed Holder Export cap = %d, want 50,000 Tickets", got)
	}
	if sharedApp.CatalogService.HolderExportRowCap() == sharedApp.SalesService.ExportRowCap() {
		t.Fatal("the Holder Export and the Sales Export share a cap; they count different things for different reasons")
	}
}

// AN AUDIT LINE IS WRITTEN PER GENERATED FILE, naming who took it, from which
// Organization and Event, under which structural filters, and how many rows.
//
// This file is the platform's densest concentration of attendee personal data,
// and "who pulled the guest list" cannot be answered retroactively: the line is
// written here or it is never written. There is no audit table behind it on
// purpose — that implies a reading surface, a retention policy and an access
// rule, and should be designed once across the platform rather than growing out
// of this feature.
func TestTheHolderExportLogsWhoTookWhatAndHowMuch(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	f := newHolderFilterFixture(t, env)

	logs := withCatalogLogger(t)
	file := openHolderExport(t, env, f.sessionID, f.eventID,
		"channel=online&ticket_type_id="+f.vipID+"&assignment_state=unassigned&sold_from=2026-06-01&sort=buyer&dir=asc")

	line := logs.only(t, "holder export")
	for _, key := range []string{"member_id", "organization_id", "event_id"} {
		if got, _ := line.arg(t, key).(string); got == "" {
			t.Fatalf("log line %s is blank; it is how the file is traced back", key)
		}
	}
	if got, _ := line.arg(t, "event_id").(string); got != f.eventID {
		t.Errorf("log event_id = %q, want the exported Event %q", got, f.eventID)
	}
	if got := line.arg(t, "row_count"); got != file.dataRows {
		t.Errorf("log row_count = %v, want the %d rows handed over", got, file.dataRows)
	}
	// The structural filters, which say what was asked for without saying
	// anything about any one person.
	for _, tc := range []struct{ key, want string }{
		{"channel", "online"},
		{"ticket_type_id", f.vipID},
		{"assignment_state", "unassigned"},
		{"sold_from", "2026-06-01"},
		{"sort", "buyer"},
		{"dir", "asc"},
	} {
		if got, _ := line.arg(t, tc.key).(string); got != tc.want {
			t.Errorf("log %s = %q, want %q", tc.key, got, tc.want)
		}
	}
	if got := line.arg(t, "search"); got != false {
		t.Errorf("log search = %v, want false on an unsearched export", got)
	}

	// AN EXPORT THAT MATCHED NOTHING STILL SAYS SO: the record of who asked is
	// not conditional on the answer being non-empty.
	logs.reset()
	empty := openHolderExport(t, env, f.sessionID, f.eventID, "channel=in_person&ticket_type_id="+f.gaID)
	if empty.dataRows != 0 {
		t.Fatalf("that view holds %d rows, want none", empty.dataRows)
	}
	if got := logs.only(t, "holder export").arg(t, "row_count"); got != 0 {
		t.Errorf("empty export logged row_count = %v, want 0", got)
	}

	// AND A REFUSAL LOGS NOTHING, because no file was taken. The line claims a
	// file that was actually handed over.
	logs.reset()
	withHolderExportCap(t, 1)
	if resp, data := downloadHolderExport(t, env, f.sessionID, f.eventID, ""); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("over-cap status=%d, want 400; body=%s", resp.StatusCode, string(data))
	}
	for _, logged := range logs.lines {
		if strings.Contains(logged.msg, "holder export") {
			t.Fatalf("a refused export logged a line claiming a file: %s", logs.rendered())
		}
	}
}

// ORG ADMIN AND EVENT OWNER MAY DOWNLOAD; EVENT STAFF ARE REFUSED.
//
// The same gate as the Holder List read and the Sales Export beside it (#521,
// ADR 0065): one rule for holder data. A gate on the page with a wider one on
// the download of it — or the reverse — is the arrangement #521 was opened to
// end. Event Staff work the door; the roster is not theirs, in a file any more
// than on a screen.
func TestTheHolderExportIsForTheOrgAdminAndTheEventOwner(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	f := newHolderFilterFixture(t, env)

	addMember := func(email, role string) string {
		t.Helper()
		resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
			"email": email, "role": role,
		}, authHeader(f.sessionID))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("add %s status=%d error=%+v", role, resp.StatusCode, body.Error)
		}
		return verifyOTP(t, env, email)
	}

	admin := openHolderExport(t, env, f.sessionID, f.eventID, "")
	if admin.dataRows == 0 {
		t.Fatal("the Org Admin's export is empty; this test cannot tell a widened gate from an empty roster")
	}

	ownerSession := addMember("exportowner@example.com", "event_owner")
	owner := openHolderExport(t, env, ownerSession, f.eventID, "")
	if owner.dataRows != admin.dataRows {
		t.Errorf("event owner's file has %d rows where the Org Admin's has %d — the same roster, or the gate widened onto a different one",
			owner.dataRows, admin.dataRows)
	}

	// Refused a 403 rather than a 404: this Event is theirs to see; the roster on
	// it is not.
	doorSession := addMember("exportdoor@example.com", "event_staff")
	resp, data := downloadHolderExport(t, env, doorSession, f.eventID, "")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("event staff status=%d, want 403; body=%s", resp.StatusCode, string(data))
	}
	// Unauthenticated is refused too, and before anything is built.
	if resp, _ := downloadHolderExport(t, env, "", f.eventID, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d, want 401", resp.StatusCode)
	}
}

// THE EXPORT IS INVISIBLE WHILE BOTH FLAGS ARE DARK, exactly as the list it
// mirrors is: a build with neither feature answers 404, which is what a build
// that never had them would answer (ADR 0045).
func TestTheHolderExportIsInvisibleWhileBothFlagsAreOff(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Dark Fest", "dark-fest")

	resp, data := downloadHolderExport(t, env, sessionID, eventID, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("export with both flags dark status=%d, want 404; body=%s", resp.StatusCode, string(data))
	}
}
