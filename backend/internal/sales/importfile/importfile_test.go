package importfile

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func sampleTypes() []TicketTypeRef {
	return []TicketTypeRef{
		{ID: "tt-ga", Name: "GA", PriceCents: 1000, Capacity: 50, SoldCount: 5},
		{ID: "tt-vip", Name: "VIP Pass", PriceCents: 5000, Capacity: 10, SoldCount: 0},
	}
}

func TestParseCSVHeadersAndRows(t *testing.T) {
	csv := "customer_email,customer_first_name,customer_last_name,ticket_type,quantity,payment_method,sold_at,amount\n" +
		"ana@example.com,Ana,Lopez,GA,2,cash,2026-07-01T10:00:00Z,10.50\n" +
		"\n" + // blank row skipped
		"bob@example.com,Bob,Ng,VIP Pass,1,transfer,2026-07-02,\n"

	rows, err := Parse("sales.csv", strings.NewReader(csv))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].CustomerEmail != "ana@example.com" || rows[0].CustomerFirstName != "Ana" || rows[0].CustomerLastName != "Lopez" || rows[0].TicketType != "GA" || rows[0].Amount != "10.50" {
		t.Fatalf("row0 = %+v", rows[0])
	}
	// encoding/csv collapses blank lines, so bob is the second data record.
	if rows[1].CustomerEmail != "bob@example.com" || rows[1].CustomerLastName != "Ng" || rows[1].TicketType != "VIP Pass" {
		t.Fatalf("row1 = %+v", rows[1])
	}
}

func TestParseMissingRequiredColumn(t *testing.T) {
	csv := "customer_email,customer_first_name,customer_last_name,ticket_type,quantity,payment_method\n" +
		"ana@example.com,Ana,Lopez,GA,2,cash\n"
	_, err := Parse("sales.csv", strings.NewReader(csv))
	if !IsUnreadable(err) {
		t.Fatalf("err = %v, want unreadable (missing sold_at)", err)
	}
}

func TestParseMissingLastNameColumnIsUnreadable(t *testing.T) {
	csv := "customer_email,customer_first_name,ticket_type,quantity,payment_method,sold_at\n" +
		"ana@example.com,Ana,GA,2,cash,2026-07-01T10:00:00Z\n"
	_, err := Parse("sales.csv", strings.NewReader(csv))
	if !IsUnreadable(err) {
		t.Fatalf("err = %v, want unreadable (missing customer_last_name)", err)
	}
}

// xlsxWorkbook builds an in-memory .xlsx. sheets maps sheet name → rows (each
// row is the ordered cell values); order preserves the given sheet names, with
// the first becoming the active sheet.
func xlsxWorkbook(t *testing.T, order []string, sheets map[string][][]string) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	// The workbook starts with "Sheet1"; rename it to the first requested sheet
	// and create the rest in order.
	if err := f.SetSheetName("Sheet1", order[0]); err != nil {
		t.Fatalf("rename sheet: %v", err)
	}
	for _, name := range order[1:] {
		if _, err := f.NewSheet(name); err != nil {
			t.Fatalf("new sheet %s: %v", name, err)
		}
	}
	for name, rows := range sheets {
		for i, row := range rows {
			cell, err := excelize.CoordinatesToCellName(1, i+1)
			if err != nil {
				t.Fatalf("coords: %v", err)
			}
			r := make([]any, len(row))
			for j, v := range row {
				r[j] = v
			}
			if err := f.SetSheetRow(name, cell, &r); err != nil {
				t.Fatalf("set row on %s: %v", name, err)
			}
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatalf("write xlsx: %v", err)
	}
	return buf.Bytes()
}

func importHeader() []string {
	return []string{"customer_email", "customer_first_name", "customer_last_name", "ticket_type", "quantity", "payment_method", "sold_at", "amount"}
}

func TestParseXLSXSelectsSalesSheetByName(t *testing.T) {
	// A decoy sheet sits first (as a future Instructions sheet would); the Sales
	// sheet is selected by name, not position.
	data := xlsxWorkbook(t, []string{"Instructions", "Sales"}, map[string][][]string{
		"Instructions": {{"read me first"}},
		"Sales": {
			importHeader(),
			{"ana@example.com", "Ana", "Lopez", "GA", "2", "cash", "2026-07-01T10:00:00Z", "10.50"},
		},
	})
	rows, err := Parse("export.xlsx", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rows) != 1 || rows[0].CustomerEmail != "ana@example.com" || rows[0].CustomerLastName != "Lopez" {
		t.Fatalf("rows = %+v, want the Sales sheet row", rows)
	}
}

func TestParseXLSXMissingSalesSheet(t *testing.T) {
	// Multi-sheet workbook whose Sales tab was renamed → dedicated error.
	data := xlsxWorkbook(t, []string{"Instructions", "MySales"}, map[string][][]string{
		"Instructions": {{"read me first"}},
		"MySales":      {importHeader()},
	})
	_, err := Parse("export.xlsx", bytes.NewReader(data))
	if !IsUnreadable(err) {
		t.Fatalf("err = %v, want unreadable (missing Sales sheet)", err)
	}
	if !strings.Contains(err.Error(), "Sales") {
		t.Fatalf("reason = %q, want it to name the Sales sheet", err.Error())
	}
}

func TestParseXLSXSingleSheetFallback(t *testing.T) {
	// A plain single-sheet export (e.g. CSV-origin) without a "Sales" sheet is
	// still read.
	data := xlsxWorkbook(t, []string{"Sheet1"}, map[string][][]string{
		"Sheet1": {
			importHeader(),
			{"ana@example.com", "Ana", "Lopez", "GA", "1", "cash", "2026-07-01T10:00:00Z", ""},
		},
	})
	rows, err := Parse("plain.xlsx", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
}

func TestParseFriendlyReasons(t *testing.T) {
	// Missing columns names the absent headers.
	csv := "customer_email,customer_first_name,customer_last_name,ticket_type,payment_method\n"
	_, err := Parse("sales.csv", strings.NewReader(csv))
	if !IsUnreadable(err) || !strings.Contains(err.Error(), "quantity") || !strings.Contains(err.Error(), "sold_at") {
		t.Fatalf("missing-columns reason = %v, want it to name quantity and sold_at", err)
	}

	// Empty file (no rows at all).
	_, err = Parse("empty.csv", strings.NewReader(""))
	if err != errEmptyFile {
		t.Fatalf("empty reason = %v, want errEmptyFile", err)
	}

	// Corrupt .xlsx (ZIP magic but not a real workbook).
	_, err = Parse("corrupt.xlsx", bytes.NewReader([]byte("PK\x03\x04 not really a workbook")))
	if err != errCorruptFile {
		t.Fatalf("corrupt reason = %v, want errCorruptFile", err)
	}
}

func TestParseRowLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("customer_email,customer_first_name,customer_last_name,ticket_type,quantity,payment_method,sold_at\n")
	for i := 0; i < MaxRows+1; i++ {
		b.WriteString("ana@example.com,Ana,Lopez,GA,1,cash,2026-07-01T10:00:00Z\n")
	}
	_, err := Parse("big.csv", strings.NewReader(b.String()))
	if err != ErrTooLarge {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestValidateReportsAllErrorsAtOnce(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	rows := []RawRow{
		{Line: 2, CustomerEmail: "not-an-email", CustomerFirstName: "", CustomerLastName: "", TicketType: "Nope", Quantity: "0", PaymentMethod: "bitcoin", SoldAt: "2030-01-01T00:00:00Z"},
	}
	res := Validate(ValidateInput{Rows: rows, Types: sampleTypes(), Now: now, Location: time.UTC})
	if res.ValidRows != 0 || res.TotalRows != 1 {
		t.Fatalf("counts = %d/%d", res.ValidRows, res.TotalRows)
	}
	got := map[string]bool{}
	for _, e := range res.Rows[0].Errors {
		got[e.Field] = true
	}
	for _, field := range []string{"customer_email", "customer_first_name", "customer_last_name", "ticket_type", "quantity", "payment_method", "sold_at"} {
		if !got[field] {
			t.Fatalf("expected error on %s; got %+v", field, res.Rows[0].Errors)
		}
	}
}

func TestValidateRequiresBothNameHalves(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	// First name present, last name blank → invalid on customer_last_name only.
	rows := []RawRow{
		{Line: 2, CustomerEmail: "ana@example.com", CustomerFirstName: "Ana", CustomerLastName: "", TicketType: "GA", Quantity: "1", PaymentMethod: "cash", SoldAt: "2026-07-01T10:00:00Z"},
	}
	res := Validate(ValidateInput{Rows: rows, Types: sampleTypes(), Now: now, Location: time.UTC})
	if res.Rows[0].Valid {
		t.Fatalf("row should be invalid with a missing last name: %+v", res.Rows[0])
	}
	got := map[string]bool{}
	for _, e := range res.Rows[0].Errors {
		got[e.Field] = true
	}
	if !got["customer_last_name"] || got["customer_first_name"] {
		t.Fatalf("want only customer_last_name error; got %+v", res.Rows[0].Errors)
	}
}

func TestValidateMatchesAndCapacityImpact(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	rows := []RawRow{
		{Line: 2, CustomerEmail: "ana@example.com", CustomerFirstName: "Ana", CustomerLastName: "Lopez", TicketType: " ga ", Quantity: "2", PaymentMethod: "cash", SoldAt: "2026-07-01T10:00:00Z"},
		{Line: 3, CustomerEmail: "bob@example.com", CustomerFirstName: "Bob", CustomerLastName: "Ng", TicketType: "GA", Quantity: "3", PaymentMethod: "transfer", SoldAt: "2026-07-02", Amount: "0"},
	}
	res := Validate(ValidateInput{Rows: rows, Types: sampleTypes(), Now: now, Location: time.UTC})
	if res.ValidRows != 2 {
		t.Fatalf("valid = %d, want 2; rows=%+v", res.ValidRows, res.Rows)
	}
	if res.Rows[0].TicketTypeID != "tt-ga" || res.Rows[0].TicketTypeName != "GA" {
		t.Fatalf("row0 match = %+v", res.Rows[0])
	}
	if res.Rows[0].CustomerFirstName != "Ana" || res.Rows[0].CustomerLastName != "Lopez" {
		t.Fatalf("row0 name = %+v", res.Rows[0])
	}
	if res.Rows[1].AmountCents == nil || *res.Rows[1].AmountCents != 0 {
		t.Fatalf("row1 comp amount = %+v", res.Rows[1].AmountCents)
	}
	if len(res.CapacityImpact) != 1 {
		t.Fatalf("impact = %+v", res.CapacityImpact)
	}
	imp := res.CapacityImpact[0]
	if imp.TicketTypeID != "tt-ga" || imp.Requested != 5 || imp.Remaining != 45 {
		t.Fatalf("impact = %+v, want requested 5 remaining 45", imp)
	}
}

func TestValidateOversellFlagsAndBlocksCommit(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	// VIP has capacity 10, sold 0; request 12 → overage 2, commit blocked.
	rows := []RawRow{
		{Line: 2, CustomerEmail: "ana@example.com", CustomerFirstName: "Ana", CustomerLastName: "Lopez", TicketType: "VIP Pass", Quantity: "12", PaymentMethod: "cash", SoldAt: "2026-07-01T10:00:00Z"},
	}
	res := Validate(ValidateInput{Rows: rows, Types: sampleTypes(), Now: now, Location: time.UTC})
	if len(res.CapacityImpact) != 1 {
		t.Fatalf("impact = %+v", res.CapacityImpact)
	}
	imp := res.CapacityImpact[0]
	if !imp.Oversold || imp.Overage != 2 {
		t.Fatalf("impact = %+v, want oversold overage 2", imp)
	}
	if res.Committable {
		t.Fatalf("Committable = true, want false when oversold")
	}
}

func TestValidateCommittableWhenValidAndWithinCapacity(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	rows := []RawRow{
		{Line: 2, CustomerEmail: "ana@example.com", CustomerFirstName: "Ana", CustomerLastName: "Lopez", TicketType: "GA", Quantity: "2", PaymentMethod: "cash", SoldAt: "2026-07-01T10:00:00Z"},
	}
	res := Validate(ValidateInput{Rows: rows, Types: sampleTypes(), Now: now, Location: time.UTC})
	if !res.Committable {
		t.Fatalf("Committable = false, want true; rows=%+v impact=%+v", res.Rows, res.CapacityImpact)
	}
	if res.CapacityImpact[0].Oversold || res.CapacityImpact[0].Overage != 0 {
		t.Fatalf("impact = %+v, want not oversold", res.CapacityImpact[0])
	}
}

func TestValidateNotCommittableWhenAnyRowInvalid(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	rows := []RawRow{
		{Line: 2, CustomerEmail: "bad", CustomerFirstName: "", CustomerLastName: "Lopez", TicketType: "GA", Quantity: "1", PaymentMethod: "cash", SoldAt: "2026-07-01T10:00:00Z"},
	}
	res := Validate(ValidateInput{Rows: rows, Types: sampleTypes(), Now: now, Location: time.UTC})
	if res.Committable {
		t.Fatalf("Committable = true, want false when a row is invalid")
	}
}

func TestValidateMatchesByHiddenID(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	rows := []RawRow{
		{Line: 2, CustomerEmail: "ana@example.com", CustomerFirstName: "Ana", CustomerLastName: "Lopez", TicketType: "typo name", TicketTypeID: "tt-vip", Quantity: "1", PaymentMethod: "cash", SoldAt: "2026-07-01T10:00:00Z"},
	}
	res := Validate(ValidateInput{Rows: rows, Types: sampleTypes(), Now: now, Location: time.UTC})
	if !res.Rows[0].Valid || res.Rows[0].TicketTypeID != "tt-vip" {
		t.Fatalf("hidden-id match failed: %+v", res.Rows[0])
	}
}

func TestValidateAmountDecimalToCents(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	rows := []RawRow{
		{Line: 2, CustomerEmail: "ana@example.com", CustomerFirstName: "Ana", CustomerLastName: "Lopez", TicketType: "GA", Quantity: "1", PaymentMethod: "cash", SoldAt: "2026-07-01T10:00:00Z", Amount: "10.50"},
	}
	res := Validate(ValidateInput{Rows: rows, Types: sampleTypes(), Now: now, Location: time.UTC})
	if res.Rows[0].AmountCents == nil || *res.Rows[0].AmountCents != 1050 {
		t.Fatalf("amount cents = %+v, want 1050", res.Rows[0].AmountCents)
	}
}

func TestTemplateRoundTrip(t *testing.T) {
	data, err := BuildTemplate("Summer Fest", sampleTypes())
	if err != nil {
		t.Fatalf("build template: %v", err)
	}

	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open template: %v", err)
	}
	defer func() { _ = f.Close() }()

	header, err := f.GetRows(templateSheet)
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	if len(header) == 0 {
		t.Fatalf("template has no header")
	}
	wantHeaders := []string{"customer_email", "customer_first_name", "customer_last_name", "ticket_type", "quantity", "payment_method", "sold_at", "amount", "ticket_type_id"}
	for i, h := range wantHeaders {
		if i >= len(header[0]) || header[0][i] != h {
			t.Fatalf("header[%d] = %q, want %q", i, header[0], h)
		}
	}

	// The reference sheet lists the Ticket Types and is hidden.
	visible, err := f.GetSheetVisible(templateRefSheet)
	if err != nil {
		t.Fatalf("get visible: %v", err)
	}
	if visible {
		t.Fatalf("reference sheet should be hidden")
	}
	refName, err := f.GetCellValue(templateRefSheet, "A1")
	if err != nil {
		t.Fatalf("read ref: %v", err)
	}
	if refName != "GA" {
		t.Fatalf("ref A1 = %q, want GA", refName)
	}
}
