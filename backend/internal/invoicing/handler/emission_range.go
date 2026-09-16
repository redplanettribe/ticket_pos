package handler

import (
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// emissionDateRange is THE ONE PARSER of an Emission Date range on this
// module's routes. The Tax Invoices list's filter (#596, spec #593) and the
// Tax Document Archive (#630, spec #629) both read their two days through it,
// so a range is refused for the same reasons, under the same codes and
// messages, wherever an operator types one. Only two things differ between
// callers: the field names (`issued_from`/`issued_to` beside the list's other
// filters, `from`/`to` on the archive) and whether a bound may be left open.
// The list's may; the archive's may not, because a period has two ends.
//
// The Sales list's sold_from/sold_to is the prior art, down to the code and
// the message (its validateDate is unexported in its own handler package, and
// catalog's Holder List carries its own copy for the same reason). What is NOT
// copied is the interpretation: the Sales list reads its days in the Event's
// timezone because sold_at is an instant, while issued_on is already a
// calendar day in the Issuer's country, so nothing here converts anything.
//
// A MALFORMED DATE AND AN INVERTED RANGE ARE BOTH REFUSED rather than answered
// with an empty result: "no document was emitted in that window" and "that
// window is not a window" are different facts, and an empty page or an empty
// archive sends an operator looking for documents that are there. The
// inversion is blamed on the start field (CodeStartAfterEnd names the start of
// a range), and only when both bounds parsed: one field error per thing wrong
// with the request, never a second one derived from a value already refused.
type emissionDateRange struct {
	FromField string
	ToField   string
	// Required refuses a blank bound under CodeRequired instead of reading it
	// as an open one.
	Required bool
}

// parse returns both bounds as "YYYY-MM-DD" ("" for an open one) and every
// field error at once.
func (spec emissionDateRange) parse(rawFrom, rawTo string) (string, string, []platform.FieldError) {
	var fields []platform.FieldError
	from := spec.day(rawFrom, spec.FromField, &fields)
	to := spec.day(rawTo, spec.ToField, &fields)
	if from != "" && to != "" && from > to {
		// Lexicographic on YYYY-MM-DD is chronological, which is the whole
		// reason the wire format is that one.
		fields = append(fields, platform.FieldError{
			Field:   spec.FromField,
			Code:    platform.CodeStartAfterEnd,
			Message: "must be on or before " + spec.ToField,
		})
	}
	return from, to, fields
}

// day trims one bound and checks it is a calendar day. Blank is an open bound
// and returns "", unless the range requires both bounds.
func (spec emissionDateRange) day(raw, field string, fields *[]platform.FieldError) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		if spec.Required {
			*fields = append(*fields, required(field))
		}
		return ""
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		*fields = append(*fields, platform.FieldError{Field: field, Code: platform.CodeInvalidDate, Message: "must be a date (YYYY-MM-DD)"})
		return ""
	}
	return value
}

// issuedRange is the Tax Invoices list's Emission Date filter: both bounds
// optional.
var issuedRange = emissionDateRange{FromField: "issued_from", ToField: "issued_to"}

// archiveRange is the Tax Document Archive's period: both bounds required.
// The archive's summary (#631) reads its range through this same value.
var archiveRange = emissionDateRange{FromField: "from", ToField: "to", Required: true}
