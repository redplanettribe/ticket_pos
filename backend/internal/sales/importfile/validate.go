package importfile

import (
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// TicketTypeRef is the catalog snapshot of one Ticket Type on the Event, used to
// match rows and compute capacity impact. It carries no DB dependency so the
// validation core stays unit-testable.
type TicketTypeRef struct {
	ID         string
	Name       string
	PriceCents int
	Capacity   int
	SoldCount  int
	// MaxPerCustomer is the Ticket Type's Purchase Limit — the most of it one
	// Customer may hold at once — nil being the unrestricted state (ADR 0025).
	//
	// The validation core carries it without reading it. Judging a row against it
	// needs what that Customer already holds, which is a database read this
	// package deliberately has no way to make, so the sales service decides the
	// refusal from this snapshot and reports it back through RejectRow.
	MaxPerCustomer *int
}

// ValidateInput is everything the validation core needs: the parsed rows, the
// Event's Ticket Types, the service clock, and the Event timezone used to
// interpret naive sold_at values.
type ValidateInput struct {
	Rows     []RawRow
	Types    []TicketTypeRef
	Now      time.Time
	Location *time.Location
}

// RowError is a single per-row validation failure.
type RowError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// RowResult is the validated outcome for one file row: the matched Ticket Type
// (if any), the normalized field values, and every problem found on the row.
type RowResult struct {
	Row               int    `json:"row"`
	CustomerEmail     string `json:"customer_email"`
	CustomerFirstName string `json:"customer_first_name"`
	CustomerLastName  string `json:"customer_last_name"`
	// CustomerTaxIDType and CustomerTaxIDNumber are the row's Tax ID once
	// accepted, with the number in the form it will be stored in (trimmed,
	// passports uppercased). Both stay empty when the row supplies no Tax ID or
	// when the one it supplies was rejected, so the preview never shows a value
	// that will not be recorded.
	CustomerTaxIDType   string     `json:"customer_tax_id_type,omitempty"`
	CustomerTaxIDNumber string     `json:"customer_tax_id_number,omitempty"`
	TicketType          string     `json:"ticket_type"`
	TicketTypeID        string     `json:"ticket_type_id,omitempty"`
	TicketTypeName      string     `json:"ticket_type_name,omitempty"`
	Quantity            int        `json:"quantity"`
	PaymentMethod       string     `json:"payment_method"`
	SoldAt              string     `json:"sold_at,omitempty"`
	AmountCents         *int       `json:"amount_cents,omitempty"`
	Valid               bool       `json:"valid"`
	Errors              []RowError `json:"errors,omitempty"`

	// PossibleDuplicate flags a valid row that matches an existing active Ticket
	// Sale on customer_email + ticket type + sold_at date. Soft signal only: it
	// never blocks commit; the organizer resolves it by skipping or keeping.
	PossibleDuplicate bool `json:"possible_duplicate,omitempty"`
	// DuplicateOfDate is the prior sale's sold_at date (YYYY-MM-DD, Event tz) that
	// this row appears to duplicate.
	DuplicateOfDate string `json:"duplicate_of_date,omitempty"`

	// soldAt is the parsed sold_at, retained for the commit path (not serialized).
	soldAt time.Time
}

// SoldAtTime returns the parsed sold_at timestamp for a valid row.
func (r RowResult) SoldAtTime() time.Time { return r.soldAt }

// CapacityImpact summarizes the requested quantity against remaining capacity
// for one Ticket Type referenced by the import.
type CapacityImpact struct {
	TicketTypeID   string `json:"ticket_type_id"`
	TicketTypeName string `json:"ticket_type_name"`
	Requested      int    `json:"requested"`
	SoldCount      int    `json:"sold_count"`
	Capacity       int    `json:"capacity"`
	Remaining      int    `json:"remaining"`
	// Overage is how many the requested quantity exceeds remaining capacity by
	// (0 when it fits). Oversold is true whenever Overage > 0.
	Overage  int  `json:"overage"`
	Oversold bool `json:"oversold"`
}

// ValidateResult is the full preview: per-row verdicts, the per-Ticket-Type
// capacity impact, and the valid/total counts.
type ValidateResult struct {
	Rows           []RowResult      `json:"rows"`
	CapacityImpact []CapacityImpact `json:"capacity_impact"`
	ValidRows      int              `json:"valid_rows"`
	TotalRows      int              `json:"total_rows"`
	// Committable is true only when every row is valid and no Ticket Type is
	// oversold. Possible-duplicate flags never affect it (soft signal). A
	// commit-blocked preview must not be committed until re-previewed.
	Committable bool `json:"committable"`
}

// Valid reports whether every parsed row passed validation.
func (v ValidateResult) Valid() bool { return v.ValidRows == v.TotalRows }

// anyOversold reports whether any Ticket Type the file touches is asked for more
// than it has left.
func (v ValidateResult) anyOversold() bool {
	for _, impact := range v.CapacityImpact {
		if impact.Oversold {
			return true
		}
	}
	return false
}

// RejectRow turns a row that passed field validation into a rejected one,
// attaching the complaint and restating the counters so Valid, ValidRows and
// Committable can never disagree with the rows themselves.
//
// It exists so a check that needs the database — the Purchase Limit, whose count
// of what a Customer already holds cannot be made in this package (ADR 0025) —
// reports through the very channel the spreadsheet's own problems use. The
// preview panel already renders a per-row complaint beside the offending cell, so
// the refusal needs no second response concept and no warnings channel: it is
// blocking, exactly like a malformed email.
//
// Rejecting an already-invalid row only appends the complaint; it was never
// counted valid, so the counters do not move.
func (v *ValidateResult) RejectRow(index int, complaint RowError) {
	if index < 0 || index >= len(v.Rows) {
		panic(fmt.Sprintf("importfile: RejectRow index %d out of range for %d rows", index, len(v.Rows)))
	}
	row := &v.Rows[index]
	row.Errors = append(row.Errors, complaint)
	if !row.Valid {
		// Already refused for some other reason, and its quantity was never
		// counted into CapacityImpact — so there is nothing to take back.
		return
	}
	row.Valid = false
	v.ValidRows--
	// The row's quantity leaves the capacity figures with it. Validate counts a
	// row's quantity into CapacityImpact only while the row is valid, so a
	// rejected row that stayed in Requested would tell the organizer this import
	// wants tickets no longer being asked for — and could report the whole batch
	// oversold on the strength of rows it has just refused.
	v.withdrawFromCapacityImpact(row.TicketTypeID, row.Quantity)
	v.Committable = v.Valid() && !v.anyOversold()
}

// withdrawFromCapacityImpact takes a refused row's quantity back out of the
// per-Ticket-Type figures, restating Overage and Oversold from what is left.
func (v *ValidateResult) withdrawFromCapacityImpact(ticketTypeID string, quantity int) {
	if ticketTypeID == "" || quantity <= 0 {
		return
	}
	for i := range v.CapacityImpact {
		impact := &v.CapacityImpact[i]
		if impact.TicketTypeID != ticketTypeID {
			continue
		}
		impact.Requested -= quantity
		if impact.Requested < 0 {
			impact.Requested = 0
		}
		impact.Overage = impact.Requested - impact.Remaining
		if impact.Overage < 0 {
			impact.Overage = 0
		}
		impact.Oversold = impact.Overage > 0
		return
	}
}

// Validate checks every row, reporting all problems at once, and computes the
// capacity impact per Ticket Type. It performs no writes.
func Validate(in ValidateInput) ValidateResult {
	loc := in.Location
	if loc == nil {
		loc = time.UTC
	}

	byID := make(map[string]TicketTypeRef, len(in.Types))
	byName := make(map[string]TicketTypeRef, len(in.Types))
	for _, tt := range in.Types {
		byID[tt.ID] = tt
		byName[normalizeName(tt.Name)] = tt
	}

	requested := map[string]int{}
	result := ValidateResult{
		Rows:      make([]RowResult, 0, len(in.Rows)),
		TotalRows: len(in.Rows),
	}

	for _, raw := range in.Rows {
		row := validateRow(raw, byID, byName, loc, in.Now)
		if row.Valid {
			result.ValidRows++
			requested[row.TicketTypeID] += row.Quantity
		} else if row.TicketTypeID != "" && row.Quantity > 0 {
			// A row can be invalid for an unrelated field yet still target a real
			// Ticket Type; count it so the capacity picture is complete.
			requested[row.TicketTypeID] += row.Quantity
		}
		result.Rows = append(result.Rows, row)
	}

	for _, tt := range in.Types {
		req, ok := requested[tt.ID]
		if !ok {
			continue
		}
		remaining := tt.Capacity - tt.SoldCount
		overage := req - remaining
		if overage < 0 {
			overage = 0
		}
		result.CapacityImpact = append(result.CapacityImpact, CapacityImpact{
			TicketTypeID:   tt.ID,
			TicketTypeName: tt.Name,
			Requested:      req,
			SoldCount:      tt.SoldCount,
			Capacity:       tt.Capacity,
			Remaining:      remaining,
			Overage:        overage,
			Oversold:       overage > 0,
		})
	}

	// A batch may be committed only when every row is valid and nothing is
	// oversold. Possible-duplicate flags are a soft signal and never block.
	result.Committable = result.Valid() && !result.anyOversold()

	return result
}

func validateRow(raw RawRow, byID, byName map[string]TicketTypeRef, loc *time.Location, now time.Time) RowResult {
	row := RowResult{
		Row:               raw.Line,
		CustomerEmail:     raw.CustomerEmail,
		CustomerFirstName: raw.CustomerFirstName,
		CustomerLastName:  raw.CustomerLastName,
		TicketType:        raw.TicketType,
		PaymentMethod:     strings.ToLower(raw.PaymentMethod),
	}
	var errs []RowError
	add := func(field, message string) { errs = append(errs, RowError{Field: field, Message: message}) }

	if raw.CustomerEmail == "" {
		add(colCustomerEmail, "is required")
	} else if _, err := mail.ParseAddress(raw.CustomerEmail); err != nil {
		add(colCustomerEmail, "must be a valid email")
	}

	if raw.CustomerFirstName == "" {
		add(colCustomerFirstName, "is required")
	}
	if raw.CustomerLastName == "" {
		add(colCustomerLastName, "is required")
	}

	// The Tax ID: optional on this channel (ADR 0016), but held to the shared
	// validator the moment either half is filled in — a wrong number is worse
	// than none, because it is declared and the Organization carries it. Each
	// failure blames one half of the pair so the organizer knows which cell to
	// fix, and a half-filled pair blames the missing half.
	taxIDType, taxIDNumber := validateTaxIDCells(raw, add)
	row.CustomerTaxIDType = taxIDType
	row.CustomerTaxIDNumber = taxIDNumber

	// Match the Ticket Type by hidden id first, then by name (case/space-insensitive).
	if tt, ok := matchTicketType(raw, byID, byName); ok {
		row.TicketTypeID = tt.ID
		row.TicketTypeName = tt.Name
	} else if raw.TicketType == "" && raw.TicketTypeID == "" {
		add(colTicketType, "is required")
	} else {
		add(colTicketType, "does not match a Ticket Type on this Event")
	}

	if raw.Quantity == "" {
		add(colQuantity, "is required")
	} else if q, err := strconv.Atoi(raw.Quantity); err != nil {
		add(colQuantity, "must be a whole number")
	} else if q <= 0 {
		add(colQuantity, "must be greater than zero")
	} else {
		row.Quantity = q
	}

	switch row.PaymentMethod {
	case "cash", "transfer":
	case "":
		add(colPaymentMethod, "is required")
	default:
		add(colPaymentMethod, "must be 'cash' or 'transfer'")
	}

	if raw.SoldAt == "" {
		add(colSoldAt, "is required")
	} else if t, ok := parseSoldAt(raw.SoldAt, loc); !ok {
		add(colSoldAt, "must be an ISO 8601 date")
	} else if t.After(now) {
		add(colSoldAt, "must not be in the future")
	} else {
		row.soldAt = t
		row.SoldAt = t.Format(time.RFC3339)
	}

	if raw.Amount != "" {
		if cents, ok := parseAmount(raw.Amount); !ok {
			add(colAmount, "must be a non-negative amount")
		} else {
			row.AmountCents = &cents
		}
	}

	row.Errors = errs
	row.Valid = len(errs) == 0
	return row
}

// validateTaxIDCells resolves a row's two Tax ID cells, reporting problems
// through add and returning the accepted pair (both empty when the row carries
// no Tax ID or when the one it carries was rejected). Blank/blank is the normal
// case for an imported sale and is not an error.
func validateTaxIDCells(raw RawRow, add func(field, message string)) (string, string) {
	taxIDType := strings.TrimSpace(raw.CustomerTaxIDType)
	number := strings.TrimSpace(raw.CustomerTaxIDNumber)

	switch {
	case taxIDType == "" && number == "":
		return "", ""
	case taxIDType == "":
		add(colCustomerTaxIDType, "is required when a Tax ID number is given")
		return "", ""
	case number == "":
		add(colCustomerTaxIDNumber, "is required when a Tax ID type is given")
		return "", ""
	}

	normalized, err := platform.ValidateTaxID(taxIDType, number)
	switch {
	case errors.Is(err, platform.ErrTaxIDTypeUnknown):
		add(colCustomerTaxIDType, platform.TaxIDTypeMessage)
		return "", ""
	case err != nil:
		add(colCustomerTaxIDNumber, platform.TaxIDNumberMessage(taxIDType))
		return "", ""
	}
	return taxIDType, normalized
}

func matchTicketType(raw RawRow, byID, byName map[string]TicketTypeRef) (TicketTypeRef, bool) {
	if raw.TicketTypeID != "" {
		if tt, ok := byID[raw.TicketTypeID]; ok {
			return tt, true
		}
		return TicketTypeRef{}, false
	}
	if raw.TicketType != "" {
		if tt, ok := byName[normalizeName(raw.TicketType)]; ok {
			return tt, true
		}
	}
	return TicketTypeRef{}, false
}

// normalizeName lower-cases and collapses internal whitespace so "GA" and
// " ga " match the same Ticket Type.
func normalizeName(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// parseSoldAt accepts an ISO-8601 timestamp (offset honoured), a naive
// datetime/date (interpreted in the Event timezone), or an Excel date serial.
func parseSoldAt(s string, loc *time.Location) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	naiveLayouts := []string{
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, layout := range naiveLayouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, true
		}
	}
	// Excel date serial (raw cell value from an .xlsx date cell).
	if serial, err := strconv.ParseFloat(s, 64); err == nil && serial > 0 {
		t, err := excelize.ExcelDateToTime(serial, false)
		if err == nil {
			// ExcelDateToTime returns a UTC wall-clock; reinterpret in the Event tz.
			return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc), true
		}
	}
	return time.Time{}, false
}

// parseAmount converts a major-currency amount (e.g. "10", "10.50", "0") to
// integer cents. Blank amounts are handled by the caller (catalog price).
func parseAmount(s string) (int, bool) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")
	if s == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(s, 64)
	if err != nil || value < 0 {
		return 0, false
	}
	// Round to the nearest cent to avoid binary float drift (e.g. 10.10).
	return int(value*100 + 0.5), true
}
