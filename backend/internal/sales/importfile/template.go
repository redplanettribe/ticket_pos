package importfile

import (
	"bytes"
	"fmt"

	"github.com/xuri/excelize/v2"
)

// templateSheet is the sheet organizers fill in.
const templateSheet = "Sales"

// templateRefSheet holds the Ticket Type name→id reference the dropdown and the
// hidden id column resolve against. It is hidden from the organizer.
const templateRefSheet = "_ticket_types"

// templateHeaders are the visible columns, in order. ticket_type_id (column I)
// is a hidden reference the preview matches on when present.
var templateHeaders = []string{
	colCustomerEmail, colCustomerFirstName, colCustomerLastName, colTicketType,
	colQuantity, colPaymentMethod, colSoldAt, colAmount, colTicketTypeID,
}

// BuildTemplate produces a per-event .xlsx: a header row, a locked dropdown of
// the Event's Ticket Types in the ticket_type column, a cash|transfer dropdown
// for payment_method, and a hidden ticket_type_id column that resolves the
// selected name to its internal id via the hidden reference sheet.
func BuildTemplate(eventName string, types []TicketTypeRef) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	if err := f.SetSheetName("Sheet1", templateSheet); err != nil {
		return nil, err
	}

	for i, h := range templateHeaders {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return nil, err
		}
		if err := f.SetCellStr(templateSheet, cell, h); err != nil {
			return nil, err
		}
	}

	// Hidden reference sheet: column A = Ticket Type name, column B = id.
	if _, err := f.NewSheet(templateRefSheet); err != nil {
		return nil, err
	}
	for i, tt := range types {
		row := i + 1
		if err := f.SetCellStr(templateRefSheet, fmt.Sprintf("A%d", row), tt.Name); err != nil {
			return nil, err
		}
		if err := f.SetCellStr(templateRefSheet, fmt.Sprintf("B%d", row), tt.ID); err != nil {
			return nil, err
		}
	}
	if err := f.SetSheetVisible(templateRefSheet, false); err != nil {
		return nil, err
	}

	const lastRow = MaxRows + 1 // header is row 1

	// Locked dropdown for ticket_type (column D) sourced from the reference sheet.
	if len(types) > 0 {
		dv := excelize.NewDataValidation(true)
		dv.Sqref = fmt.Sprintf("D2:D%d", lastRow)
		dv.SetSqrefDropList(fmt.Sprintf("%s!$A$1:$A$%d", templateRefSheet, len(types)))
		dv.ShowDropDown = false // false => show the dropdown arrow
		if err := f.AddDataValidation(templateSheet, dv); err != nil {
			return nil, err
		}

		// Hidden id column (I) resolves the chosen name back to its id.
		for row := 2; row <= lastRow; row++ {
			formula := fmt.Sprintf(
				`=IFERROR(VLOOKUP(D%d,%s!$A$1:$B$%d,2,FALSE),"")`,
				row, templateRefSheet, len(types),
			)
			if err := f.SetCellFormula(templateSheet, fmt.Sprintf("I%d", row), formula); err != nil {
				return nil, err
			}
		}
	}

	// payment_method (column F) is a fixed cash|transfer dropdown.
	pm := excelize.NewDataValidation(true)
	pm.Sqref = fmt.Sprintf("F2:F%d", lastRow)
	if err := pm.SetDropList([]string{"cash", "transfer"}); err != nil {
		return nil, err
	}
	pm.ShowDropDown = false
	if err := f.AddDataValidation(templateSheet, pm); err != nil {
		return nil, err
	}

	// Hide the ticket_type_id reference column (I) from the organizer.
	if err := f.SetColVisible(templateSheet, "I", false); err != nil {
		return nil, err
	}
	if err := f.SetColWidth(templateSheet, "A", "H", 22); err != nil {
		return nil, err
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return bytes.NewBuffer(buf.Bytes()).Bytes(), nil
}
