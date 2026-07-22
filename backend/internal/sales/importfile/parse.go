// Package importfile parses and validates Sale Import spreadsheets (.csv/.xlsx)
// into rows that the sales service can preview and commit. It holds the single
// parse/validate core shared by the preview and commit paths.
package importfile

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"
)

// MaxRows caps the number of data rows accepted from a single Sale Import file.
const MaxRows = 10000

// Column headers recognised in the first row of an uploaded file. Matching is
// case- and space-insensitive. The ticket_type_id column is the hidden
// reference the per-event template fills; when present it wins over the name.
const (
	colCustomerEmail     = "customer_email"
	colCustomerFirstName = "customer_first_name"
	colCustomerLastName  = "customer_last_name"
	colTicketType        = "ticket_type"
	colTicketTypeID      = "ticket_type_id"
	colQuantity          = "quantity"
	colPaymentMethod     = "payment_method"
	colSoldAt            = "sold_at"
	colAmount            = "amount"
)

// requiredHeaders must all be present for a file to be parseable at all.
var requiredHeaders = []string{
	colCustomerEmail, colCustomerFirstName, colCustomerLastName, colTicketType,
	colQuantity, colPaymentMethod, colSoldAt,
}

// RawRow is one data row as read from the file, before validation. Fields are
// the raw cell strings (Excel date cells are coerced to their serial/ISO form
// during parsing); interpretation happens in Validate.
type RawRow struct {
	// Line is the 1-based spreadsheet row number (header is row 1).
	Line              int
	CustomerEmail     string
	CustomerFirstName string
	CustomerLastName  string
	TicketType        string
	TicketTypeID      string
	Quantity          string
	PaymentMethod     string
	SoldAt            string
	Amount            string
}

// ErrTooLarge reports that a file exceeds MaxRows data rows.
var ErrTooLarge = fmt.Errorf("import file exceeds %d rows", MaxRows)

// ErrUnreadable reports a malformed or unrecognised file.
type ErrUnreadable struct{ Reason string }

func (e *ErrUnreadable) Error() string { return "import file is unreadable: " + e.Reason }

func unreadable(reason string) error { return &ErrUnreadable{Reason: reason} }

// Parse reads an uploaded Sale Import file into rows, dispatching on the file
// name's extension (.xlsx vs .csv) and falling back to content sniffing. It
// enforces the MaxRows limit and requires the mandatory header columns.
func Parse(filename string, r io.Reader) ([]RawRow, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, unreadable(err.Error())
	}
	name := strings.ToLower(strings.TrimSpace(filename))
	switch {
	case strings.HasSuffix(name, ".xlsx"):
		return parseXLSX(data)
	case strings.HasSuffix(name, ".csv"):
		return parseCSV(data)
	case looksLikeXLSX(data):
		return parseXLSX(data)
	default:
		return parseCSV(data)
	}
}

// looksLikeXLSX reports whether data begins with the ZIP magic bytes an .xlsx
// container starts with.
func looksLikeXLSX(data []byte) bool {
	return len(data) >= 2 && data[0] == 'P' && data[1] == 'K'
}

func parseCSV(data []byte) ([]RawRow, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		return nil, unreadable(err.Error())
	}
	if len(records) == 0 {
		return nil, unreadable("file has no header row")
	}

	index, err := mapHeaders(records[0])
	if err != nil {
		return nil, err
	}
	if len(records)-1 > MaxRows {
		return nil, ErrTooLarge
	}

	rows := make([]RawRow, 0, len(records)-1)
	for i := 1; i < len(records); i++ {
		cells := records[i]
		if isBlankRecord(cells) {
			continue
		}
		rows = append(rows, rowFromCells(i+1, cells, index))
	}
	return rows, nil
}

func parseXLSX(data []byte) ([]RawRow, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, unreadable(err.Error())
	}
	defer func() { _ = f.Close() }()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, unreadable("workbook has no sheets")
	}
	sheet := sheets[0]

	// Read raw cell values so date cells surface as their serial numbers rather
	// than a locale-formatted string; Validate coerces them.
	records, err := f.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, unreadable(err.Error())
	}
	if len(records) == 0 {
		return nil, unreadable("sheet has no header row")
	}

	index, err := mapHeaders(records[0])
	if err != nil {
		return nil, err
	}
	if len(records)-1 > MaxRows {
		return nil, ErrTooLarge
	}

	rows := make([]RawRow, 0, len(records)-1)
	for i := 1; i < len(records); i++ {
		cells := records[i]
		if isBlankRecord(cells) {
			continue
		}
		rows = append(rows, rowFromCells(i+1, cells, index))
	}
	return rows, nil
}

// mapHeaders resolves each recognised column to its zero-based position and
// verifies the required columns are present.
func mapHeaders(header []string) (map[string]int, error) {
	index := map[string]int{}
	for i, h := range header {
		key := normalizeHeader(h)
		if key == "" {
			continue
		}
		if _, seen := index[key]; !seen {
			index[key] = i
		}
	}
	var missing []string
	for _, req := range requiredHeaders {
		if _, ok := index[req]; !ok {
			missing = append(missing, req)
		}
	}
	if len(missing) > 0 {
		return nil, unreadable("missing required columns: " + strings.Join(missing, ", "))
	}
	return index, nil
}

func rowFromCells(line int, cells []string, index map[string]int) RawRow {
	get := func(col string) string {
		i, ok := index[col]
		if !ok || i >= len(cells) {
			return ""
		}
		return strings.TrimSpace(cells[i])
	}
	return RawRow{
		Line:              line,
		CustomerEmail:     get(colCustomerEmail),
		CustomerFirstName: get(colCustomerFirstName),
		CustomerLastName:  get(colCustomerLastName),
		TicketType:        get(colTicketType),
		TicketTypeID:      get(colTicketTypeID),
		Quantity:          get(colQuantity),
		PaymentMethod:     get(colPaymentMethod),
		SoldAt:            get(colSoldAt),
		Amount:            get(colAmount),
	}
}

func isBlankRecord(cells []string) bool {
	for _, c := range cells {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// normalizeHeader lower-cases a header and collapses spaces/dashes to
// underscores so "Customer Email" and "customer-email" both match.
func normalizeHeader(h string) string {
	h = strings.TrimSpace(strings.ToLower(h))
	h = strings.ReplaceAll(h, " ", "_")
	h = strings.ReplaceAll(h, "-", "_")
	return h
}

// IsUnreadable reports whether err indicates a malformed/unrecognised file.
func IsUnreadable(err error) bool {
	var u *ErrUnreadable
	return errors.As(err, &u)
}
