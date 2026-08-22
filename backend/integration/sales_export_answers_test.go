package integration

import (
	"bytes"
	"net/http"
	"sort"
	"testing"

	"github.com/xuri/excelize/v2"
)

// Answers reach the Sales Export (#314): the workbook gains a second sheet, one
// row per Ticket, so an Organization can pivot "how many larges do I order".
//
// The seam is the same one the rest of the export is asserted at — the file is
// fetched over HTTP and read back with excelize — because what matters is what
// the person who downloads it observes. Nothing here reaches for a column letter
// on the data sheet; the new sheet's letters ARE asserted in the exportfile
// package's own tests, where the layout is decided.

// salesExportAnswersSheet is the per-Ticket sheet's name. Deliberately not
// "Sales", for the same reason the data sheet is not — see
// TestSalesExportAnswersSheetIsNotNamedSales.
const salesExportAnswersSheet = "Ticket Answers"

// answersSheet is a downloaded workbook's per-Ticket sheet, read as its rows.
type answersSheet struct {
	header []string
	rows   [][]string
}

// openSalesExportAnswers opens a downloaded export and reads its per-Ticket
// sheet, failing when the workbook has none.
func openSalesExportAnswers(t *testing.T, data []byte) answersSheet {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open .xlsx: %v", err)
	}
	defer func() { _ = f.Close() }()

	rows, err := f.GetRows(salesExportAnswersSheet)
	if err != nil {
		t.Fatalf("get rows from %q (sheets: %v): %v", salesExportAnswersSheet, f.GetSheetList(), err)
	}
	if len(rows) == 0 {
		t.Fatalf("%q has no header row", salesExportAnswersSheet)
	}
	width := len(rows[0])
	// GetRows trims trailing empty cells, so every row is padded back out to the
	// header's width: a blank cell is a value here — it is an Outstanding Answer
	// — and a short row would silently swallow it.
	padded := make([][]string, 0, len(rows)-1)
	for _, row := range rows[1:] {
		for len(row) < width {
			row = append(row, "")
		}
		padded = append(padded, row)
	}
	return answersSheet{header: rows[0], rows: padded}
}

// cell reads one row's value under a named column.
func (s answersSheet) cell(t *testing.T, row int, header string) string {
	t.Helper()
	for i, h := range s.header {
		if h == header {
			return s.rows[row][i]
		}
	}
	t.Fatalf("no %q column in header %v", header, s.header)
	return ""
}

// sheetNames is every sheet in a downloaded workbook, in order.
func sheetNames(t *testing.T, data []byte) []string {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open .xlsx: %v", err)
	}
	defer func() { _ = f.Close() }()
	return f.GetSheetList()
}

// exportAnswersFixture is an Event whose Ticket Type asks things, with the
// Tickets to answer them: an Org Admin session, the Event, its Ticket Type, and
// the Tickets of one two-ticket Sale Import.
func exportAnswersFixture(t *testing.T, env *testEnv, slug string) (sessionID, eventID, ticketTypeID string, ticketIDs []string) {
	t.Helper()
	enableTicketQuestions(t)
	sessionID = orgAdminSession(t, env)
	eventID = createDraftEvent(t, env, sessionID, "Answer Export Fest", slug)
	ticketTypeID = createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2000, 50)
	commitBatch(t, env, sessionID, eventID, slug+"-batch", []map[string]any{{
		"customer_email":      "ana@example.com",
		"customer_first_name": "Ana",
		"customer_last_name":  "Lopez",
		"ticket_type_id":      ticketTypeID,
		"quantity":            2,
		"payment_method":      "cash",
		"sold_at":             "2026-07-01T10:00:00Z",
	}})
	return sessionID, eventID, ticketTypeID, ticketsOfEvent(t, env, eventID)
}

// ticketsOfEvent reads an Event's Ticket ids in the order they were minted.
// Through SQL because a Ticket has no identity anybody is shown, and these tests
// need to address one to answer it.
func ticketsOfEvent(t *testing.T, env *testEnv, eventID string) []string {
	t.Helper()
	rows, err := env.db.Query(`
		SELECT tk.id
		FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		JOIN ticket_sales s ON s.id = l.ticket_sale_id
		WHERE s.event_id = $1
		ORDER BY s.sold_at, l.id, tk.ordinal
	`, eventID)
	if err != nil {
		t.Fatalf("read Tickets: %v", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan Ticket: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

// downloadOK downloads the export and fails on anything but a 200.
func downloadOK(t *testing.T, env *testEnv, sessionID, eventID, query string) []byte {
	t.Helper()
	resp, data := downloadSalesExport(t, env, sessionID, eventID, query)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export %q status=%d body=%s", query, resp.StatusCode, string(data))
	}
	return data
}

// TestSalesExportAnswersSheetIsNotNamedSales is the assertion the whole sheet is
// one rename away from failing.
//
// The Sale Import parser selects its sheet by the name "Sales", so a workbook
// carrying one could be uploaded back as an import — inserting every sale a
// second time and re-emailing every buyer. The data sheet has always been named
// around that; the sheet this ticket adds must be too, and the workbook must
// still be one an importer refuses.
func TestSalesExportAnswersSheetIsNotNamedSales(t *testing.T) {
	env := setupTest(t)
	sessionID, eventID, ticketTypeID, ticketIDs := exportAnswersFixture(t, env, "not-sales")
	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text",
	})
	putAnswer(t, env, sessionID, eventID, ticketIDs[0], question.ID, map[string]any{"text": "M"})

	data := downloadOK(t, env, sessionID, eventID, "")
	names := sheetNames(t, data)
	for _, name := range names {
		if name == "Sales" {
			t.Fatalf("sheets = %v — an export must never carry a sheet a Sale Import would parse", names)
		}
	}
	want := []string{salesExportInfoSheet, salesExportSheet, salesExportAnswersSheet}
	if !equalStrings(names, want) {
		t.Fatalf("sheets = %v, want %v", names, want)
	}
}

// TestSalesExportAnswersSheetAppearsOnlyWhenSomethingIsAsked: the sheet is for
// Events that ask things. Every other Event — which today is every Event — gets
// the workbook it has always got, and so does every Event on a deployment where
// the feature has not been turned on.
func TestSalesExportAnswersSheetAppearsOnlyWhenSomethingIsAsked(t *testing.T) {
	env := setupTest(t)
	sessionID, eventID, ticketTypeID, _ := exportAnswersFixture(t, env, "asked-nothing")

	// Nothing asked: two sheets, as before.
	want := []string{salesExportInfoSheet, salesExportSheet}
	if got := sheetNames(t, downloadOK(t, env, sessionID, eventID, "")); !equalStrings(got, want) {
		t.Fatalf("sheets = %v, want %v when the Event asks nothing", got, want)
	}

	createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text",
	})
	if got := sheetNames(t, downloadOK(t, env, sessionID, eventID, "")); len(got) != 3 {
		t.Fatalf("sheets = %v, want a third once the Event has a Ticket Question", got)
	}

	// And the flag is a real off switch, not just a hidden authoring surface:
	// with it closed the file is the two-sheet one again, questions and all. An
	// Answer given before the Privacy Policy describes the collection must not
	// leave the building in a spreadsheet either (ADR 0045).
	sharedApp.SalesService.WithTicketQuestions(false)
	sharedApp.CatalogService.WithTicketQuestions(false)
	if got := sheetNames(t, downloadOK(t, env, sessionID, eventID, "")); !equalStrings(got, want) {
		t.Fatalf("sheets = %v, want %v while the feature is dark", got, want)
	}
}

// TestSalesExportDataSheetIsUnchangedByAnswers is the promise the money columns
// rest on. The existing sheet is one row per Ticket SALE — a sale of two tickets
// is one row with one amount on it — and the arrival of a per-Ticket sheet
// beside it must move nothing, column for column.
func TestSalesExportDataSheetIsUnchangedByAnswers(t *testing.T) {
	env := setupTest(t)
	sessionID, eventID, ticketTypeID, ticketIDs := exportAnswersFixture(t, env, "unchanged")

	// The file before anything is asked.
	before, err := excelize.OpenReader(bytes.NewReader(downloadOK(t, env, sessionID, eventID, "")))
	if err != nil {
		t.Fatalf("open .xlsx: %v", err)
	}
	defer func() { _ = before.Close() }()
	beforeRows, err := before.GetRows(salesExportSheet)
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}

	// And the same Event once it asks two questions and both Tickets answer.
	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "single_choice", "option_labels": []string{"S", "M", "L"},
	})
	guests := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "Guests", "kind": "number",
	})
	putAnswer(t, env, sessionID, eventID, ticketIDs[0], question.ID,
		map[string]any{"option_ids": optionIDsAt(question, []int{2})})
	putAnswer(t, env, sessionID, eventID, ticketIDs[1], guests.ID, map[string]any{"number": "2"})

	after, err := excelize.OpenReader(bytes.NewReader(downloadOK(t, env, sessionID, eventID, "")))
	if err != nil {
		t.Fatalf("open .xlsx: %v", err)
	}
	defer func() { _ = after.Close() }()
	afterRows, err := after.GetRows(salesExportSheet)
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}

	if len(beforeRows) != len(afterRows) {
		t.Fatalf("data sheet rows = %d, was %d — the sale must stay ONE row however many Tickets answered", len(afterRows), len(beforeRows))
	}
	for i := range beforeRows {
		if !equalStrings(beforeRows[i], afterRows[i]) {
			t.Fatalf("data sheet row %d = %v, was %v — the data sheet is unchanged, column for column", i, afterRows[i], beforeRows[i])
		}
	}
	// Two Tickets, one sale, one row, and the quantity still says two: nothing
	// about the new sheet flattened the sale into its Tickets.
	sheet := openSalesExport(t, downloadOK(t, env, sessionID, eventID, ""))
	if sheet.dataRows != 1 {
		t.Fatalf("data rows = %d, want the one Ticket Sale", sheet.dataRows)
	}
	if got := sheet.value(t, 0, "total_quantity"); got != "2" {
		t.Fatalf("total_quantity = %q, want 2", got)
	}
}

// TestSalesExportAnswersSheetColumnsAndCells: what the sheet says, and what its
// cells are made of.
//
// A number is a number and a date is a date, because the whole promise of the
// file is that the next thing the recipient does is sort, sum and pivot — and a
// column of text that looks like numbers sorts 10 before 9.
func TestSalesExportAnswersSheetColumnsAndCells(t *testing.T) {
	env := setupTest(t)
	sessionID, eventID, ticketTypeID, ticketIDs := exportAnswersFixture(t, env, "columns")

	meal := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "Meal", "kind": "single_choice", "option_labels": []string{"Chicken", "Vegan"},
	})
	extras := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "Extras", "kind": "multi_choice", "option_labels": []string{"Tote", "Poster", "Sticker"},
	})
	guests := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "Guests", "kind": "number",
	})
	birthday := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "Birthday", "kind": "date",
	})
	dinner := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "Attending dinner", "kind": "checkbox",
	})

	// The first Ticket answers everything. The second answers nothing at all,
	// which is what an Outstanding Answer looks like in the file.
	putAnswer(t, env, sessionID, eventID, ticketIDs[0], meal.ID,
		map[string]any{"option_ids": optionIDsAt(meal, []int{1})})
	putAnswer(t, env, sessionID, eventID, ticketIDs[0], extras.ID,
		map[string]any{"option_ids": optionIDsAt(extras, []int{0, 2})})
	putAnswer(t, env, sessionID, eventID, ticketIDs[0], guests.ID, map[string]any{"number": "3"})
	putAnswer(t, env, sessionID, eventID, ticketIDs[0], birthday.ID, map[string]any{"date": "1990-03-04"})
	putAnswer(t, env, sessionID, eventID, ticketIDs[0], dinner.ID, map[string]any{"checked": false})

	sheet := openSalesExportAnswers(t, downloadOK(t, env, sessionID, eventID, ""))

	// The join column, the Ticket Type, then the Event's questions in the order
	// the Organization arranged them — with the multiple-choice one fanned out
	// into ONE COLUMN PER OPTION rather than collapsed into a delimited cell,
	// which is the one shape that cannot be pivoted.
	want := []string{
		"confirmation_ref", "ticket_type",
		"Meal",
		"Tote", "Poster", "Sticker",
		"Guests", "Birthday", "Attending dinner",
	}
	if !equalStrings(sheet.header, want) {
		t.Fatalf("header = %v, want %v", sheet.header, want)
	}

	// One row per TICKET: the sale was for two, so there are two rows, both
	// carrying the same Sale Confirmation reference to join back on.
	if len(sheet.rows) != 2 {
		t.Fatalf("rows = %d, want one per Ticket of the two-ticket sale", len(sheet.rows))
	}
	data := openSalesExport(t, downloadOK(t, env, sessionID, eventID, ""))
	ref := data.value(t, 0, "confirmation_ref")
	for i := range sheet.rows {
		if got := sheet.cell(t, i, "confirmation_ref"); got != ref {
			t.Fatalf("row %d confirmation_ref = %q, want %q — the join back to the sale", i, got, ref)
		}
		if got := sheet.cell(t, i, "ticket_type"); got != "GA" {
			t.Fatalf("row %d ticket_type = %q, want GA", i, got)
		}
	}

	// The answered Ticket. A single_choice Answer is the Option's words in one
	// cell; a multi_choice one is TRUE under what was ticked and FALSE under
	// what was not, which is what makes "how many totes do I order" a COUNTIF.
	answered := sheet.rows[0]
	for _, tc := range []struct{ header, want string }{
		{"Meal", "Vegan"},
		{"Tote", "TRUE"},
		{"Poster", "FALSE"},
		{"Sticker", "TRUE"},
		{"Guests", "3"},
		{"Birthday", "1990-03-04"},
		// FALSE is an Answer: somebody read the question and left it unticked.
		{"Attending dinner", "FALSE"},
	} {
		if got := sheet.cell(t, 0, tc.header); got != tc.want {
			t.Fatalf("%s = %q, want %q (row: %v)", tc.header, got, tc.want, answered)
		}
	}

	// The unanswered Ticket is blank all the way across — including under every
	// Option of the multiple-choice question. A FALSE there would claim this
	// Ticket read the question and declined every Option, which is a different
	// fact from an Outstanding Answer.
	for i := 2; i < len(sheet.header); i++ {
		if got := sheet.rows[1][i]; got != "" {
			t.Fatalf("%s on the unanswered Ticket = %q, want blank: an unanswered question is an Answer that is owed, not a FALSE",
				sheet.header[i], got)
		}
	}

	// And the typed cells are really typed. Read raw, a date is an Excel serial
	// and a number is a number — neither is a picture of one.
	f, err := excelize.OpenReader(bytes.NewReader(downloadOK(t, env, sessionID, eventID, "")))
	if err != nil {
		t.Fatalf("open .xlsx: %v", err)
	}
	defer func() { _ = f.Close() }()
	raw := func(cell string) string {
		t.Helper()
		v, err := f.GetCellValue(salesExportAnswersSheet, cell, excelize.Options{RawCellValue: true})
		if err != nil {
			t.Fatalf("get %s: %v", cell, err)
		}
		return v
	}
	if got := raw("G2"); got != "3" {
		t.Fatalf("Guests stored as %q, want the number 3", got)
	}
	// 32936 is 1990-03-04 as an Excel date serial: a whole number, so the cell
	// carries a calendar date and no time — and no timezone to move it by a day.
	if got := raw("H2"); got != "32936" {
		t.Fatalf("Birthday stored as %q, want the Excel date serial 32936", got)
	}
}

// TestSalesExportAnswersKeepRetiredOptionsAndFollowRenames is decision 10 of the
// design session, at the seam.
//
// An Option is retired and never deleted, and it keeps its column in the Sales
// Export — nothing answered ever disappears. And a column is the OPTION, not its
// wording: correcting a typo moves a heading and forks nothing, so the Tickets
// that chose it before the correction sit in the same column as the ones that
// chose it after.
func TestSalesExportAnswersKeepRetiredOptionsAndFollowRenames(t *testing.T) {
	env := setupTest(t)
	sessionID, eventID, ticketTypeID, ticketIDs := exportAnswersFixture(t, env, "retired")

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "multi_choice", "option_labels": []string{"Mediun", "L"},
	})
	medium := question.Options[0].ID
	large := question.Options[1].ID

	// One Ticket chooses the misspelt Option and the other chooses both.
	putAnswer(t, env, sessionID, eventID, ticketIDs[0], question.ID,
		map[string]any{"option_ids": []string{medium}})
	putAnswer(t, env, sessionID, eventID, ticketIDs[1], question.ID,
		map[string]any{"option_ids": []string{medium, large}})

	// Then the typo is corrected, and the other Option is retired.
	if resp, body := env.patch(t, optionPath(eventID, ticketTypeID, question.ID, medium),
		map[string]any{"label": "Medium"}, authHeader(sessionID)); resp.StatusCode != http.StatusOK {
		t.Fatalf("rename option status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if resp, body := env.deleteJSON(t, optionPath(eventID, ticketTypeID, question.ID, large),
		nil, authHeader(sessionID)); resp.StatusCode != http.StatusOK {
		t.Fatalf("retire option status=%d error=%+v", resp.StatusCode, body.Error)
	}

	sheet := openSalesExportAnswers(t, downloadOK(t, env, sessionID, eventID, ""))
	// The retired Option still has a column, and the renamed one has ONE column
	// headed with its current words rather than two headed with each of them.
	want := []string{"confirmation_ref", "ticket_type", "Medium", "L"}
	if !equalStrings(sheet.header, want) {
		t.Fatalf("header = %v, want %v — a retired Option keeps its column and a rename forks none", sheet.header, want)
	}
	// Both Tickets land in the one Medium column, whichever spelling they chose
	// it under.
	for i := range sheet.rows {
		if got := sheet.cell(t, i, "Medium"); got != "TRUE" {
			t.Fatalf("row %d Medium = %q, want TRUE — the Answer follows the Option's identity, not its label", i, got)
		}
	}
	// And what was answered under the retired Option is still in the file.
	trues := 0
	for i := range sheet.rows {
		if sheet.cell(t, i, "L") == "TRUE" {
			trues++
		}
	}
	if trues != 1 {
		t.Fatalf("the retired Option reads TRUE on %d rows, want 1 — nothing answered ever disappears", trues)
	}
}

// TestSalesExportAnswersRespectTheFilters: the sheet mirrors whatever the Sales
// list was showing, exactly as the rest of the file does. Two sheets in one
// workbook disagreeing about which sales it is about would be worse than no
// second sheet at all.
func TestSalesExportAnswersRespectTheFilters(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Filtered Answers Fest", "filtered-answers")
	ticketTypeID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2000, 50)
	batchID := commitBatch(t, env, sessionID, eventID, "filtered-answers-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
			"ticket_type_id": ticketTypeID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng",
			"ticket_type_id": ticketTypeID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
	})
	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text",
	})
	for i, ticketID := range ticketsOfEvent(t, env, eventID) {
		putAnswer(t, env, sessionID, eventID, ticketID, question.ID,
			map[string]any{"text": []string{"S", "M", "L"}[i]})
	}

	// Unfiltered: every Ticket of both sales.
	all := openSalesExportAnswers(t, downloadOK(t, env, sessionID, eventID, ""))
	if len(all.rows) != 3 {
		t.Fatalf("rows = %d, want one per Ticket of both sales", len(all.rows))
	}

	// Narrowed to one buyer: only that sale's Ticket, and the sheet's rows carry
	// the same Sale Confirmation reference the data sheet was narrowed to.
	data := downloadOK(t, env, sessionID, eventID, "q=bob@example.com")
	narrowed := openSalesExportAnswers(t, data)
	if len(narrowed.rows) != 1 {
		t.Fatalf("rows = %d under a search, want the one Ticket of the one matching sale", len(narrowed.rows))
	}
	sales := openSalesExport(t, data)
	if sales.dataRows != 1 {
		t.Fatalf("data rows = %d, want the one matching sale", sales.dataRows)
	}
	if got, want := narrowed.cell(t, 0, "confirmation_ref"), sales.value(t, 0, "confirmation_ref"); got != want {
		t.Fatalf("the two sheets disagree about which sale the file is about: %q and %q", got, want)
	}
	if got := narrowed.cell(t, 0, "T-shirt size"); got != "L" {
		t.Fatalf("T-shirt size = %q, want the answer of the Ticket that was left in", got)
	}

	// And the status filter, which is the one the reader did not choose: the
	// default file omits reversed sales, and it must omit their Tickets too.
	sizes := func(sheet answersSheet) []string {
		out := make([]string, 0, len(sheet.rows))
		for i := range sheet.rows {
			out = append(out, sheet.cell(t, i, "T-shirt size"))
		}
		sort.Strings(out)
		return out
	}
	undoBatch(t, env, sessionID, eventID, batchID)
	active := openSalesExportAnswers(t, downloadOK(t, env, sessionID, eventID, ""))
	if len(active.rows) != 0 {
		t.Fatalf("rows = %v, want none once every sale is reversed and the default filter is active", active.rows)
	}
	reversed := openSalesExportAnswers(t, downloadOK(t, env, sessionID, eventID, "status=reversed"))
	if got := sizes(reversed); !equalStrings(got, []string{"L", "M", "S"}) {
		t.Fatalf("reversed export sizes = %v, want every Ticket of the reversed sales", got)
	}
}
