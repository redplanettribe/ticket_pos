package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// postFile uploads a Sale Import file via multipart/form-data with the given
// extra text fields, returning the response and decoded envelope.
func postFile(t *testing.T, env *testEnv, path, filename string, content []byte, fields map[string]string, headers map[string]string) (*http.Response, envelope) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write file: %v", err)
	}
	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			t.Fatalf("write field %s: %v", k, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, env.server.URL+path, &body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	var envBody envelope
	decodeEnvelope(t, resp, &envBody)
	return resp, envBody
}

// xlsxWithRows builds an in-memory .xlsx with the standard import columns and
// the given data rows (each row is the ordered cell values).
func xlsxWithRows(t *testing.T, rows [][]any) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := f.GetSheetName(0)
	headers := []any{"customer_email", "customer_first_name", "customer_last_name", "ticket_type", "quantity", "payment_method", "sold_at", "amount"}
	if err := f.SetSheetRow(sheet, "A1", &headers); err != nil {
		t.Fatalf("set header: %v", err)
	}
	for i, row := range rows {
		cell := fmt.Sprintf("A%d", i+2)
		r := row
		if err := f.SetSheetRow(sheet, cell, &r); err != nil {
			t.Fatalf("set row: %v", err)
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatalf("write xlsx: %v", err)
	}
	return buf.Bytes()
}

// xlsxRenamedSalesSheet builds a multi-sheet workbook whose data tab is NOT
// named "Sales" (as if the organizer renamed or deleted it), so the parser can
// no longer find the Sales sheet by name.
func xlsxRenamedSalesSheet(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	if err := f.SetSheetName("Sheet1", "Instructions"); err != nil {
		t.Fatalf("rename sheet: %v", err)
	}
	if _, err := f.NewSheet("MySales"); err != nil {
		t.Fatalf("new sheet: %v", err)
	}
	headers := []any{"customer_email", "customer_first_name", "customer_last_name", "ticket_type", "quantity", "payment_method", "sold_at", "amount"}
	if err := f.SetSheetRow("MySales", "A1", &headers); err != nil {
		t.Fatalf("set header: %v", err)
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatalf("write xlsx: %v", err)
	}
	return buf.Bytes()
}

type previewResultBody struct {
	Rows []struct {
		Row               int    `json:"row"`
		CustomerFirstName string `json:"customer_first_name"`
		CustomerLastName  string `json:"customer_last_name"`
		TicketTypeID      string `json:"ticket_type_id"`
		TicketTypeName    string `json:"ticket_type_name"`
		Valid             bool   `json:"valid"`
		PossibleDuplicate bool   `json:"possible_duplicate"`
		DuplicateOfDate   string `json:"duplicate_of_date"`
		Errors            []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"errors"`
	} `json:"rows"`
	CapacityImpact []struct {
		TicketTypeID string `json:"ticket_type_id"`
		Requested    int    `json:"requested"`
		Remaining    int    `json:"remaining"`
		Overage      int    `json:"overage"`
		Oversold     bool   `json:"oversold"`
	} `json:"capacity_impact"`
	ValidRows   int  `json:"valid_rows"`
	TotalRows   int  `json:"total_rows"`
	Committable bool `json:"committable"`
}

func TestSaleImportTemplateDownload(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Template Fest", "template-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

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
	if ct := resp.Header.Get("Content-Type"); ct != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Fatalf("content-type = %q", ct)
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
	if len(rows) == 0 || rows[0][0] != "customer_email" {
		t.Fatalf("unexpected template header: %+v", rows)
	}
	// The Event's Ticket Type id is present on the hidden reference sheet.
	id, err := f.GetCellValue("_ticket_types", "B1")
	if err != nil {
		t.Fatalf("read ref id: %v", err)
	}
	if id != ttID {
		t.Fatalf("ref id = %q, want %q", id, ttID)
	}
}

func TestSaleImportPreviewReportsAllErrors(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Preview Fest", "preview-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	csv := "customer_email,customer_first_name,customer_last_name,ticket_type,quantity,payment_method,sold_at,amount\n" +
		"ana@example.com,Ana,Lopez,GA,2,cash,2026-07-01T10:00:00Z,\n" +
		"not-an-email,,,Bogus,0,bitcoin,2030-01-01T00:00:00Z,\n"

	resp, body := postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports/preview",
		"sales.csv", []byte(csv), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d error=%+v", resp.StatusCode, body.Error)
	}

	var res previewResultBody
	if err := json.Unmarshal(body.Data, &res); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if res.TotalRows != 2 || res.ValidRows != 1 {
		t.Fatalf("counts = %d valid / %d total", res.ValidRows, res.TotalRows)
	}
	if !res.Rows[0].Valid || res.Rows[0].TicketTypeID != ttID {
		t.Fatalf("row0 = %+v, want valid match", res.Rows[0])
	}
	// Row 1 reports ALL problems at once, not just the first.
	fields := map[string]bool{}
	for _, e := range res.Rows[1].Errors {
		fields[e.Field] = true
	}
	for _, f := range []string{"customer_email", "customer_first_name", "customer_last_name", "ticket_type", "quantity", "payment_method", "sold_at"} {
		if !fields[f] {
			t.Fatalf("row1 missing error on %s: %+v", f, res.Rows[1].Errors)
		}
	}
	// The preview exposes the two name halves separately on the valid row.
	if res.Rows[0].CustomerFirstName != "Ana" || res.Rows[0].CustomerLastName != "Lopez" {
		t.Fatalf("row0 names = %q / %q, want Ana / Lopez", res.Rows[0].CustomerFirstName, res.Rows[0].CustomerLastName)
	}
	// Capacity impact counts only valid row 0 (2 of GA), remaining 50.
	if len(res.CapacityImpact) != 1 || res.CapacityImpact[0].Requested != 2 || res.CapacityImpact[0].Remaining != 50 {
		t.Fatalf("capacity impact = %+v", res.CapacityImpact)
	}
	// Preview writes nothing.
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 0 {
		t.Fatalf("sold_count = %d, want 0 (preview writes nothing)", got)
	}
}

func TestSaleImportCommitFromCSVFile(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "CSV Commit Fest", "csv-commit-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	csv := "customer_email,customer_first_name,customer_last_name,ticket_type,quantity,payment_method,sold_at,amount\n" +
		"ana@example.com,Ana,Lopez,GA,2,cash,2026-07-01T10:00:00Z,\n" +
		"bob@example.com,Bob,Ng,GA,3,transfer,2026-07-02T10:00:00Z,0\n"

	resp, body := postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports",
		"sales.csv", []byte(csv), map[string]string{"idempotency_key": "file-csv-1", "source": "direct"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d error=%+v", resp.StatusCode, body.Error)
	}
	res := importResult(t, body)
	if res.SaleCount != 2 || res.Status != "committed" {
		t.Fatalf("result = %+v", res)
	}
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 5 {
		t.Fatalf("sold_count = %d, want 5", got)
	}
	if n := len(env.email.Confirmations()); n != 2 {
		t.Fatalf("confirmations = %d, want 2", n)
	}

	// Idempotent replay: same key is a safe no-op.
	resp, body = postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports",
		"sales.csv", []byte(csv), map[string]string{"idempotency_key": "file-csv-1", "source": "direct"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replay status = %d error=%+v", resp.StatusCode, body.Error)
	}
	if replay := importResult(t, body); !replay.Replayed || replay.BatchID != res.BatchID {
		t.Fatalf("replay = %+v", replay)
	}
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 5 {
		t.Fatalf("sold_count after replay = %d, want 5", got)
	}
}

func TestSaleImportCommitFromXLSXFile(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "XLSX Commit Fest", "xlsx-commit-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	content := xlsxWithRows(t, [][]any{
		{"ana@example.com", "Ana", "Lopez", "GA", 2, "cash", "2026-07-01T10:00:00Z", nil},
		{"bob@example.com", "Bob", "Ng", "ga", 4, "transfer", "2026-07-02T10:00:00Z", 12.50},
	})

	resp, body := postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports",
		"sales.xlsx", content, map[string]string{"idempotency_key": "file-xlsx-1", "source": "direct"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d error=%+v", resp.StatusCode, body.Error)
	}
	res := importResult(t, body)
	if res.SaleCount != 2 {
		t.Fatalf("sale count = %d, want 2", res.SaleCount)
	}
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 6 {
		t.Fatalf("sold_count = %d, want 6", got)
	}

	// Price snapshots: Ana defaults to catalog 1000, Bob's 12.50 → 1250 cents.
	var prices []int
	dbRows, err := env.db.Query(`
		SELECT unit_price_cents FROM ticket_sale_lines
		JOIN ticket_sales ON ticket_sales.id = ticket_sale_lines.ticket_sale_id
		WHERE ticket_sales.event_id = $1 ORDER BY unit_price_cents
	`, eventID)
	if err != nil {
		t.Fatalf("query lines: %v", err)
	}
	defer dbRows.Close()
	for dbRows.Next() {
		var p int
		if err := dbRows.Scan(&p); err != nil {
			t.Fatalf("scan: %v", err)
		}
		prices = append(prices, p)
	}
	if len(prices) != 2 || prices[0] != 1000 || prices[1] != 1250 {
		t.Fatalf("prices = %v, want [1000 1250]", prices)
	}
}

func TestSaleImportCommitFileRejectsInvalidRows(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Invalid Commit Fest", "invalid-commit-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	csv := "customer_email,customer_first_name,customer_last_name,ticket_type,quantity,payment_method,sold_at,amount\n" +
		"ana@example.com,Ana,Lopez,GA,2,cash,2026-07-01T10:00:00Z,\n" +
		"bad,,,Nope,0,x,2030-01-01T00:00:00Z,\n"

	resp, body := postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports",
		"sales.csv", []byte(csv), map[string]string{"idempotency_key": "file-bad-1", "source": "direct"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("error = %+v, want VALIDATION_FAILED", body.Error)
	}
	// All-or-nothing: nothing recorded, no emails.
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 0 {
		t.Fatalf("sold_count = %d, want 0", got)
	}
	if n := len(env.email.Confirmations()); n != 0 {
		t.Fatalf("confirmations = %d, want 0", n)
	}
}

func decodePreview(t *testing.T, body envelope) previewResultBody {
	t.Helper()
	var res previewResultBody
	if err := json.Unmarshal(body.Data, &res); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	return res
}

// TestSaleImportPreviewOversellBlocksThenRaiseThenCommit covers the oversell
// safety rail: preview flags the overage and blocks commit; raising the Ticket
// Type's capacity and re-previewing clears the block; the commit then succeeds.
func TestSaleImportPreviewOversellBlocksThenRaiseThenCommit(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Oversell Fest", "oversell-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 5)

	// Requests 8 against capacity 5 → overage 3, commit blocked.
	csv := "customer_email,customer_first_name,customer_last_name,ticket_type,quantity,payment_method,sold_at,amount\n" +
		"ana@example.com,Ana,Lopez,GA,8,cash,2026-07-01T10:00:00Z,\n"

	resp, body := postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports/preview",
		"sales.csv", []byte(csv), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d error=%+v", resp.StatusCode, body.Error)
	}
	res := decodePreview(t, body)
	if res.Committable {
		t.Fatalf("committable = true, want false when oversold")
	}
	if len(res.CapacityImpact) != 1 || !res.CapacityImpact[0].Oversold || res.CapacityImpact[0].Overage != 3 {
		t.Fatalf("capacity impact = %+v, want oversold overage 3", res.CapacityImpact)
	}

	// Committing while blocked is rejected by the under-lock capacity check.
	resp, body = postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports",
		"sales.csv", []byte(csv), map[string]string{"idempotency_key": "oversell-1", "source": "direct"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("commit-while-blocked status = %d, want 409; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "IMPORT_BATCH_FAILED" {
		t.Fatalf("error = %+v, want IMPORT_BATCH_FAILED", body.Error)
	}

	// Inline raise-capacity: a normal catalog edit lifting GA to 10.
	raiseTicketTypeCapacity(t, env, sessionID, eventID, ttID, "GA", 1000, 10)

	// Re-preview clears the block.
	resp, body = postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports/preview",
		"sales.csv", []byte(csv), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("re-preview status = %d error=%+v", resp.StatusCode, body.Error)
	}
	res = decodePreview(t, body)
	if !res.Committable {
		t.Fatalf("committable = false after raise, want true; impact=%+v", res.CapacityImpact)
	}
	if res.CapacityImpact[0].Oversold {
		t.Fatalf("still oversold after raise: %+v", res.CapacityImpact[0])
	}

	// Commit now succeeds; sold_count reflects the batch.
	resp, body = postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports",
		"sales.csv", []byte(csv), map[string]string{"idempotency_key": "oversell-2", "source": "direct"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("commit status = %d, want 201; error=%+v", resp.StatusCode, body.Error)
	}
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 8 {
		t.Fatalf("sold_count = %d, want 8", got)
	}
}

// TestSaleImportDuplicateFlagSkipVsKeep covers the soft duplicate rail: a row
// matching an existing active sale on email + type + sold_at date is flagged (but
// never blocks); a skipped duplicate is excluded from commit while a kept one is
// recorded, so a genuine repeat buyer is preserved.
func TestSaleImportDuplicateFlagSkipVsKeep(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Dup Fest", "dup-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	// Seed an existing active sale for Ana on 2026-07-01.
	seed := "customer_email,customer_first_name,customer_last_name,ticket_type,quantity,payment_method,sold_at,amount\n" +
		"ana@example.com,Ana,Lopez,GA,2,cash,2026-07-01T10:00:00Z,\n"
	resp, body := postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports",
		"seed.csv", []byte(seed), map[string]string{"idempotency_key": "dup-seed", "source": "direct"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed commit status = %d error=%+v", resp.StatusCode, body.Error)
	}

	// Second file: Ana repeats the same email+type+date (row 2, dup); Carol is new (row 3).
	second := "customer_email,customer_first_name,customer_last_name,ticket_type,quantity,payment_method,sold_at,amount\n" +
		"ana@example.com,Ana,Lopez,GA,2,cash,2026-07-01T18:00:00Z,\n" +
		"carol@example.com,Carol,Diaz,GA,1,cash,2026-07-03T10:00:00Z,\n"

	resp, body = postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports/preview",
		"second.csv", []byte(second), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d error=%+v", resp.StatusCode, body.Error)
	}
	res := decodePreview(t, body)
	if !res.Committable {
		t.Fatalf("committable = false, want true (duplicates are soft)")
	}
	// Row 2 (Ana) flagged referencing the prior date; row 3 (Carol) not flagged.
	if !res.Rows[0].PossibleDuplicate || res.Rows[0].DuplicateOfDate != "2026-07-01" {
		t.Fatalf("row0 = %+v, want possible_duplicate on 2026-07-01", res.Rows[0])
	}
	if res.Rows[1].PossibleDuplicate {
		t.Fatalf("row1 (Carol) flagged as duplicate: %+v", res.Rows[1])
	}

	// Commit skipping the duplicate (row 2): only Carol is recorded.
	resp, body = postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports",
		"second.csv", []byte(second), map[string]string{"idempotency_key": "dup-skip", "source": "direct", "skip_rows": "2"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("skip commit status = %d error=%+v", resp.StatusCode, body.Error)
	}
	if r := importResult(t, body); r.SaleCount != 1 {
		t.Fatalf("skip commit sale_count = %d, want 1 (Carol only)", r.SaleCount)
	}
	// Ana still has exactly her one seeded sale; Carol now has one.
	if n := salesCountByEmail(t, env, eventID, "ana@example.com"); n != 1 {
		t.Fatalf("ana sales = %d, want 1 (duplicate skipped)", n)
	}
	if n := salesCountByEmail(t, env, eventID, "carol@example.com"); n != 1 {
		t.Fatalf("carol sales = %d, want 1", n)
	}
	// sold_count: seed 2 + Carol 1 = 3.
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 3 {
		t.Fatalf("sold_count after skip = %d, want 3", got)
	}

	// Keep the duplicate this time (no skip, new key): Ana's repeat is recorded.
	resp, body = postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports",
		"keep.csv", []byte("customer_email,customer_first_name,customer_last_name,ticket_type,quantity,payment_method,sold_at,amount\nana@example.com,Ana,Lopez,GA,2,cash,2026-07-01T20:00:00Z,\n"),
		map[string]string{"idempotency_key": "dup-keep", "source": "direct"}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("keep commit status = %d error=%+v", resp.StatusCode, body.Error)
	}
	if n := salesCountByEmail(t, env, eventID, "ana@example.com"); n != 2 {
		t.Fatalf("ana sales = %d, want 2 (repeat buyer kept)", n)
	}
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 5 {
		t.Fatalf("sold_count after keep = %d, want 5", got)
	}
}

func TestSaleImportPreviewMalformedFile(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Malformed Fest", "malformed-fest")
	createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	// Missing the required sold_at column.
	csv := "customer_email,customer_first_name,customer_last_name,ticket_type,quantity,payment_method\n" +
		"ana@example.com,Ana,Lopez,GA,2,cash\n"

	resp, body := postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports/preview",
		"sales.csv", []byte(csv), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "IMPORT_FILE_INVALID" {
		t.Fatalf("error = %+v, want IMPORT_FILE_INVALID", body.Error)
	}
	// The whole-file rejection reason is human-readable and names the missing
	// column, not a generic dead-end.
	if !strings.Contains(body.Error.Message, "sold_at") {
		t.Fatalf("message = %q, want it to name the missing sold_at column", body.Error.Message)
	}
}

func TestSaleImportPreviewRenamedSalesSheet(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Renamed Sheet Fest", "renamed-sheet-fest")
	createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	// A multi-sheet workbook whose Sales tab was renamed → dedicated reason.
	content := xlsxRenamedSalesSheet(t)
	resp, body := postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports/preview",
		"renamed.xlsx", content, nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "IMPORT_FILE_INVALID" {
		t.Fatalf("error = %+v, want IMPORT_FILE_INVALID", body.Error)
	}
	if !strings.Contains(body.Error.Message, "Sales") {
		t.Fatalf("message = %q, want it to name the Sales sheet", body.Error.Message)
	}
}
