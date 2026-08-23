package service

import (
	"context"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/sales/importfile"
)

// ImportRowInput is ONE Sale Import row as typed rather than uploaded: the
// template's columns, cell for cell, arriving from a form instead of a
// spreadsheet.
//
// Two routes carry one now — a Sale Correction's replacement (#351, ADR 0050)
// and a Manually Recorded Sale (#366, ADR 0052) — and they must be judged
// identically, or the same act would succeed one way and fail the other for
// reasons nobody can see. That is kept true by construction: this struct is the
// only shape the shared verdict below accepts, and every cell on it is judged by
// the import row validator the file upload uses.
//
// It is deliberately NOT the whole of either caller's form. Whether the buyer is
// mailed, and whether a sale is being reversed to make room for this one, are
// each caller's own business and live on each caller's own input.
type ImportRowInput struct {
	CustomerEmail       string
	CustomerFirstName   string
	CustomerLastName    string
	CustomerTaxIDType   string
	CustomerTaxIDNumber string
	TicketTypeID        string
	Quantity            int
	PaymentMethod       string
	SoldAt              string
	// AmountCents overrides the catalog price; nil uses it, as a blank cell does.
	AmountCents *int
}

// rawRow renders the typed cells back into the shape a parsed spreadsheet row
// arrives in, so the one validator can judge both without knowing which it has.
//
// Quantity and the amount are re-stringified on purpose. They reach the form as
// numbers and reach the file as text, and pushing the typed values through the
// text rules is what makes "0", "1.5" and a negative amount fail identically on
// both routes. Line 1 because there is exactly one row; the row index never
// reaches the caller's field names (see the handler's bare-column flattening).
func (in ImportRowInput) rawRow() importfile.RawRow {
	raw := importfile.RawRow{
		Line:                1,
		CustomerEmail:       in.CustomerEmail,
		CustomerFirstName:   in.CustomerFirstName,
		CustomerLastName:    in.CustomerLastName,
		CustomerTaxIDType:   in.CustomerTaxIDType,
		CustomerTaxIDNumber: in.CustomerTaxIDNumber,
		TicketTypeID:        in.TicketTypeID,
		Quantity:            fmt.Sprint(in.Quantity),
		PaymentMethod:       in.PaymentMethod,
		SoldAt:              in.SoldAt,
	}
	if in.AmountCents != nil {
		raw.Amount = fmt.Sprintf("%d.%02d", *in.AmountCents/100, *in.AmountCents%100)
	}
	return raw
}

// typedRowContext is everything about judging one typed import row that differs
// between the routes that record one — supplied by the caller rather than
// assumed by the path (#367).
//
// A Sale Correction is a reversal and a record in one breath, so it hands in the
// sale it is about to reverse and words its capacity complaint accordingly. A
// Manually Recorded Sale reverses nothing and hands in the zero value. Neither
// concern belongs to the shared verdict, and the verdict does not go looking for
// one: whatever netting the snapshot needs has already been done to Types by the
// time it gets here.
type typedRowContext struct {
	// ExcludeSaleID is one Ticket Sale left out of both the duplicate signal and
	// the Purchase Limit tally: the sale a Sale Correction is reversing, which
	// would otherwise always read as a duplicate of its own replacement and
	// would spend an allowance it is about to give back. Empty when nothing is
	// being reversed.
	ExcludeSaleID string
	// OverCapacity words the complaint on the quantity cell when the row asks
	// for more than the Ticket Type has left. The caller words it because the
	// caller knows what the snapshot means: net of a reversal, or as it stands.
	// Nil falls back to the plain statement of the shortfall.
	OverCapacity func(impact importfile.CapacityImpact) string
}

// validateTypedImportRow is the whole verdict on ONE typed Sale Import row, and
// the single place both typed routes get it from (#367).
//
// It judges the row exactly as the Sale Import preview judges a file row — field
// rules, Ticket Type resolved on the Event, the Tax ID pair, the sold-at date in
// the Event timezone, the duplicate-of-an-active-sale warning, the Purchase
// Limit — and then holds it to CAPACITY, which the file import deliberately does
// not: an upload defers capacity to the batch commit, because one overage there
// is a property of the whole file. A typed row is one row on a form, so its one
// overage is a complaint on the quantity cell the organizer can still fix, and
// refusing it here means nothing is written to find out (ADR 0050, ADR 0052).
//
// It writes NOTHING and is therefore the whole of both routes' preview as well
// as the gate their commit passes through, so the live verdict and the refusal
// can never disagree. The transaction's own capacity check under lock remains
// the last word on a race lost after this returns.
//
// Types is the catalog snapshot AS THE CALLER WANTS IT READ — see typedRowContext.
func (s *Service) validateTypedImportRow(
	ctx context.Context,
	orgID, eventID string,
	types []importfile.TicketTypeRef,
	loc *time.Location,
	in ImportRowInput,
	rules typedRowContext,
) (*importfile.ValidateResult, error) {
	validated := importfile.Validate(importfile.ValidateInput{
		Rows:     []importfile.RawRow{in.rawRow()},
		Types:    types,
		Now:      s.now(),
		Location: loc,
	})

	existing, err := s.repo.ListActiveSaleKeys(ctx, orgID, eventID, rules.ExcludeSaleID)
	if err != nil {
		return nil, err
	}
	flagDuplicates(&validated, existing, loc)

	if err := s.refuseImportRowsOverPurchaseLimit(ctx, eventID, types, &validated, rules.ExcludeSaleID); err != nil {
		return nil, err
	}

	// Only a row that survived everything else is told about capacity: a row
	// already being rewritten for a bad email gains nothing from a second
	// complaint about a quantity that may not survive the fix.
	for _, impact := range validated.CapacityImpact {
		if impact.Oversold && validated.Rows[0].Valid {
			validated.RejectRow(0, importfile.RowError{
				Field:   importfile.ColQuantity,
				Message: overCapacityComplaint(rules.OverCapacity, impact),
			})
		}
	}
	return &validated, nil
}

// overCapacityComplaint applies the caller's wording, falling back to the plain
// shortfall when it supplied none — a caller that says nothing about why the
// snapshot reads as it does should not silently say nothing at all.
func overCapacityComplaint(word func(importfile.CapacityImpact) string, impact importfile.CapacityImpact) string {
	if word != nil {
		return word(impact)
	}
	return fmt.Sprintf("exceeds the %d remaining on %s", impact.Remaining, impact.TicketTypeName)
}
