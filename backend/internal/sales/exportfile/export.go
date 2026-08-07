// Package exportfile builds the Sales Export: an .xlsx of an Event's Ticket
// Sales, one row per sale, reflecting exactly the filters the Sales list was
// showing when the download was pressed.
//
// It is the mirror image of the importfile package next door — that one hands an
// organizer a sheet to fill in, this one hands them the sales back — and it
// follows the same house style: the column order is decided once, in a slice,
// and every column letter is derived from it.
package exportfile

import (
	"time"

	"github.com/xuri/excelize/v2"
)

// DataSheet is the sheet the rows live on. It is deliberately NOT "Sales".
//
// This is a safety catch, not a naming preference: the Sale Import parser
// selects its sheet by the name "Sales", so a file named that way could be
// uploaded back as an import, inserting every sale a second time and re-emailing
// every buyer. Naming the sheet something else means an export can never be
// mistaken for an import. Do not rename this to "Sales".
const DataSheet = "Ticket Sales"

// The export's columns. Named constants rather than bare strings so a rename is
// a compile error rather than a silently mismatched header.
const (
	colConfirmationRef   = "confirmation_ref"
	colSoldAt            = "sold_at"
	colCustomerFirstName = "customer_first_name"
	colCustomerLastName  = "customer_last_name"
	colCustomerEmail     = "customer_email"
	colTaxIDType         = "tax_id_type"
	colTaxIDNumber       = "tax_id_number"
	colAmount            = "amount"
	colCurrency          = "currency"
	colChannel           = "channel"
	colSource            = "source"
	colPaymentMethod     = "payment_method"
	colStatus            = "status"
)

// headers are the columns in order, left to right. This slice is the only place
// the layout is decided: the header row is written from it and every column
// letter is derived from it.
//
// The order tells the sale's story in the order a reader needs it. The
// confirmation ref leads because it is the sale's human-readable identity and
// the value somebody pastes back into the product when a buyer emails them.
// Then when it happened, then who bought — including the Tax ID pair, which is
// why this feature exists at all: a Tax ID is mandatory to record a sale on the
// native Sales Channels expressly for the buyer's tax declarations, and until
// this file there was no way to read it back out. Then the transaction facts.
var headers = []string{
	colConfirmationRef,
	colSoldAt,
	colCustomerFirstName,
	colCustomerLastName,
	colCustomerEmail,
	colTaxIDType,
	colTaxIDNumber,
	colAmount,
	colCurrency,
	colChannel,
	colSource,
	colPaymentMethod,
	colStatus,
}

// dateFormat is how a sold-at cell renders. Real Excel date cells carry no
// timezone of their own, so the moment is converted into the Event's zone before
// it is written — the same zone the sold-at filter is interpreted in, so the
// file can never contradict the date range that selected its rows.
const dateFormat = "yyyy-mm-dd hh:mm"

// moneyFormat renders an amount to the cent. The cell holds a number in major
// units; this only decides that 25 reads as "25.00".
const moneyFormat = "0.00"

// Sale is one Ticket Sale as the export writes it: the sale's identity, its
// buyer, and its transaction facts.
//
// The nullable fields are pointers because a blank cell and a zero say different
// things in a spreadsheet, and the difference becomes a SUM. A sale that
// collected no Tax ID leaves both halves blank rather than carrying a
// placeholder, so at a glance the organizer can see which sales they cannot
// invoice.
type Sale struct {
	ConfirmationRef   string
	SoldAt            time.Time
	CustomerFirstName string
	CustomerLastName  string
	CustomerEmail     string
	TaxIDType         *string
	TaxIDNumber       *string
	// AmountCents is what the buyer paid, as the system stores it. The file
	// writes it in major units: a column that sums to a hundred times too much
	// is worse than no column.
	AmountCents   int
	Currency      string
	Channel       string
	Source        *string
	PaymentMethod *string
	Status        string
}

// Build produces the .xlsx: a single "Ticket Sales" sheet holding a header row
// and one row per Ticket Sale, with nothing above the header so select-all,
// autofilter and pivot source ranges all work without deleting a preamble.
//
// Cells are really typed — dates as date cells, money as numbers in major units
// — because the recipient's next move is to sort, subtract and SUM, and a
// column of strings that look like numbers cannot be done arithmetic to. loc is
// the Event's timezone, which every date is drawn in.
func Build(sales []Sale, loc *time.Location) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	if err := f.SetSheetName(f.GetSheetName(0), DataSheet); err != nil {
		return nil, err
	}

	for i, h := range headers {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return nil, err
		}
		if err := f.SetCellStr(DataSheet, cell, h); err != nil {
			return nil, err
		}
	}

	dateStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: ptr(dateFormat)})
	if err != nil {
		return nil, err
	}
	moneyStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: ptr(moneyFormat)})
	if err != nil {
		return nil, err
	}

	for i, sale := range sales {
		row := i + 2 // the header is row 1

		text := map[string]string{
			colConfirmationRef:   sale.ConfirmationRef,
			colCustomerFirstName: sale.CustomerFirstName,
			colCustomerLastName:  sale.CustomerLastName,
			colCustomerEmail:     sale.CustomerEmail,
			colCurrency:          sale.Currency,
			colChannel:           sale.Channel,
			colStatus:            sale.Status,
		}
		for header, value := range text {
			if err := setStr(f, header, row, value); err != nil {
				return nil, err
			}
		}
		// The optional ones are written only when the sale has them; an absent
		// value leaves the cell untouched, and so blank.
		for header, value := range map[string]*string{
			colTaxIDType:     sale.TaxIDType,
			colTaxIDNumber:   sale.TaxIDNumber,
			colSource:        sale.Source,
			colPaymentMethod: sale.PaymentMethod,
		} {
			if value == nil {
				continue
			}
			if err := setStr(f, header, row, *value); err != nil {
				return nil, err
			}
		}

		// A real date cell, drawn in the Event's timezone: excelize reads the
		// value's zone offset off the time itself, so converting first is what
		// puts the Event's wall clock in the cell.
		soldAt, err := cellRef(colSoldAt, row)
		if err != nil {
			return nil, err
		}
		if err := f.SetCellValue(DataSheet, soldAt, sale.SoldAt.In(loc)); err != nil {
			return nil, err
		}
		// Set after the value: excelize stamps a default date style of its own
		// when writing a time, and this replaces it.
		if err := f.SetCellStyle(DataSheet, soldAt, soldAt, dateStyle); err != nil {
			return nil, err
		}

		// Money as a number in major units — 25.00, never 2500 and never
		// "$25.00" — with the currency in its own column so the amounts stay
		// arithmetic rather than becoming text.
		amount, err := cellRef(colAmount, row)
		if err != nil {
			return nil, err
		}
		if err := f.SetCellFloat(DataSheet, amount, float64(sale.AmountCents)/100, 2, 64); err != nil {
			return nil, err
		}
		if err := f.SetCellStyle(DataSheet, amount, amount, moneyStyle); err != nil {
			return nil, err
		}
	}

	// Wide enough that an email or a confirmation ref is readable without the
	// recipient having to widen every column first.
	first, err := columnName(headers[0])
	if err != nil {
		return nil, err
	}
	last, err := columnName(headers[len(headers)-1])
	if err != nil {
		return nil, err
	}
	if err := f.SetColWidth(DataSheet, first, last, 22); err != nil {
		return nil, err
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// setStr writes a text cell in the column with the given header.
func setStr(f *excelize.File, header string, row int, value string) error {
	cell, err := cellRef(header, row)
	if err != nil {
		return err
	}
	return f.SetCellStr(DataSheet, cell, value)
}

// cellRef resolves a column header and a 1-based row to a cell reference, so
// nothing in this file names a column letter.
func cellRef(header string, row int) (string, error) {
	return excelize.CoordinatesToCellName(columnNumber(header), row)
}

// columnName is a header's column letter, derived from the headers slice.
func columnName(header string) (string, error) {
	return excelize.ColumnNumberToName(columnNumber(header))
}

// columnNumber is a header's 1-based position in the layout.
func columnNumber(header string) int {
	for i, h := range headers {
		if h == header {
			return i + 1
		}
	}
	return 0
}

func ptr[T any](v T) *T { return &v }
