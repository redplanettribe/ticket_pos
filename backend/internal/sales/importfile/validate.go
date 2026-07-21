package importfile

import (
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
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
	Row            int        `json:"row"`
	CustomerEmail  string     `json:"customer_email"`
	CustomerName   string     `json:"customer_name"`
	TicketType     string     `json:"ticket_type"`
	TicketTypeID   string     `json:"ticket_type_id,omitempty"`
	TicketTypeName string     `json:"ticket_type_name,omitempty"`
	Quantity       int        `json:"quantity"`
	PaymentMethod  string     `json:"payment_method"`
	SoldAt         string     `json:"sold_at,omitempty"`
	AmountCents    *int       `json:"amount_cents,omitempty"`
	Valid          bool       `json:"valid"`
	Errors         []RowError `json:"errors,omitempty"`

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

	anyOversold := false
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
		if overage > 0 {
			anyOversold = true
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
	result.Committable = result.Valid() && !anyOversold

	return result
}

func validateRow(raw RawRow, byID, byName map[string]TicketTypeRef, loc *time.Location, now time.Time) RowResult {
	row := RowResult{
		Row:           raw.Line,
		CustomerEmail: raw.CustomerEmail,
		CustomerName:  raw.CustomerName,
		TicketType:    raw.TicketType,
		PaymentMethod: strings.ToLower(raw.PaymentMethod),
	}
	var errs []RowError
	add := func(field, message string) { errs = append(errs, RowError{Field: field, Message: message}) }

	if raw.CustomerEmail == "" {
		add(colCustomerEmail, "is required")
	} else if _, err := mail.ParseAddress(raw.CustomerEmail); err != nil {
		add(colCustomerEmail, "must be a valid email")
	}

	if raw.CustomerName == "" {
		add(colCustomerName, "is required")
	}

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
