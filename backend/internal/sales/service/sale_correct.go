package service

import (
	"context"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales/importfile"
	"github.com/peter/ticket_pos/backend/internal/sales/repository"
)

// CorrectSaleInput is the Sale Correction form: the Sale Import template's
// columns for the replacement, as typed, plus whether the buyer is to be sent
// the replacement's Sale Confirmation.
//
// Every cell arrives as the template would carry it — quantity and the amount
// included — and is judged by the import row validator, so a correction can
// record nothing an import would have refused (ADR 0050).
type CorrectSaleInput struct {
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
	// SendConfirmation mails the replacement's Sale Confirmation to the
	// replacement's email. Off by default: the buyer dealt with a sales rep,
	// not this platform, and already holds a Confirmation from the import.
	SendConfirmation bool
}

// CorrectSaleResult is the outcome of a Sale Correction: the two halves, each
// named by id and Sale Confirmation reference, and whether the buyer was mailed.
type CorrectSaleResult struct {
	ReversedSaleID             string `json:"reversed_sale_id"`
	ReversedConfirmationRef    string `json:"reversed_confirmation_ref"`
	ReplacementSaleID          string `json:"replacement_sale_id"`
	ReplacementConfirmationRef string `json:"replacement_confirmation_ref"`
	ConfirmationSent           bool   `json:"confirmation_sent"`
}

// CorrectImportedSale is a Sale Correction (#351, ADR 0050): reverses ONE
// imported Ticket Sale and records a replacement in the same transaction, each
// pointing at the other. Never an edit — the mistaken sale keeps every field it
// was recorded with and reads "corrected"; the replacement is a fresh sale with
// fresh `unassigned` Tickets, a new Sale Confirmation reference and no batch.
//
// THE REPLACEMENT IS AN IMPORT ROW. It is judged by the same validator the Sale
// Import preview and file commit use, against a catalog snapshot with the old
// sale's quantities already given back and a Purchase Limit tally that leaves
// the old sale out: capacity and the limit are counted NET of what is being
// reversed, so re-recording the same quantity always fits. A refusal returns
// the row's verdict with nothing written and the original sale still active;
// the transaction's own capacity check under lock is the last word on a race.
//
// WHO IS TOLD. Every accepted Holder on the old sale, unconditionally, through
// the displaced-Holder mail every reversal route shares (#327). The buyer gets
// NO voided mail, ever, and the replacement's Sale Confirmation only when the
// form asked for it — with the outstanding-answers line, because fresh Tickets
// owe every answer (#315).
func (s *Service) CorrectImportedSale(ctx context.Context, actor ActorContext, eventID, saleID string, in CorrectSaleInput) (*CorrectSaleResult, *importfile.ValidateResult, error) {
	event, types, loc, err := s.loadImportContext(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, nil, err
	}

	oldLines, err := s.repo.ImportedSaleLines(ctx, actor.OrganizationID, eventID, saleID)
	if err != nil {
		return nil, nil, mapReverseSaleError(err)
	}
	types = netOfSale(types, oldLines)

	validated, err := s.validateCorrection(ctx, eventID, saleID, types, loc, in)
	if err != nil {
		return nil, nil, err
	}
	if !validated.Valid() {
		return nil, validated, nil
	}
	row := validated.Rows[0]

	ref, err := generateConfirmationRef()
	if err != nil {
		return nil, nil, err
	}
	now := s.now()
	corrected, err := s.repo.CorrectImportedSale(ctx, repository.CorrectImportedSaleInput{
		EventID:        eventID,
		OrganizationID: actor.OrganizationID,
		SaleID:         saleID,
		Now:            now,
		UpsertCustomer: s.customers.UpsertForSale,
		Replacement: repository.CommitSale{
			Customer: platform.SaleCustomer{
				Email:     row.CustomerEmail,
				FirstName: row.CustomerFirstName,
				LastName:  row.CustomerLastName,
				TaxID:     platform.SaleTaxID{Type: row.CustomerTaxIDType, Number: row.CustomerTaxIDNumber},
			},
			PaymentMethod:   row.PaymentMethod,
			SoldAt:          row.SoldAtTime(),
			ConfirmationRef: ref,
			Lines: []repository.CommitLine{{
				TicketTypeID:   row.TicketTypeID,
				Quantity:       row.Quantity,
				UnitPriceCents: row.AmountCents,
			}},
		},
	})
	if err != nil {
		return nil, nil, mapCommitError(mapReverseSaleError(err))
	}

	s.tellDisplacedHolders(ctx, []string{corrected.Reversed.ID})

	rs := corrected.Replacement
	sent := false
	if in.SendConfirmation {
		if err := s.email.SendSaleConfirmation(ctx, platform.SaleConfirmation{
			To:                    rs.CustomerEmail,
			CustomerName:          displayName(rs.CustomerFirstName, rs.CustomerLastName),
			EventName:             event.Name,
			Reference:             rs.ConfirmationRef,
			AmountCents:           rs.AmountCents,
			Currency:              event.Currency,
			ConfirmationLink:      s.confirmationLink(rs.ID, event.End()),
			HasOutstandingAnswers: s.hasOutstandingAnswers(ctx, rs.ID),
			TaxID:                 rs.CustomerTaxID,
			Locale:                s.mailLocale(ctx, rs.ID, rs.Locale, rs.CustomerEmail),
		}); err == nil {
			sent = true
		}
	}

	return &CorrectSaleResult{
		ReversedSaleID:             corrected.Reversed.ID,
		ReversedConfirmationRef:    corrected.Reversed.ConfirmationRef,
		ReplacementSaleID:          rs.ID,
		ReplacementConfirmationRef: rs.ConfirmationRef,
		ConfirmationSent:           sent,
	}, nil, nil
}

// validateCorrection judges the replacement exactly as the import preview
// judges a row — field rules, Ticket Type, Tax ID, Purchase Limit net of the
// sale being reversed — and then holds it to capacity, which the import leaves
// to the batch commit: a correction is one row, so its one overage is a
// complaint on the quantity cell rather than a batch failure.
//
// It is the whole of the verdict, kept apart so a preview endpoint can return
// the same ValidateResult the commit decides on (#352).
func (s *Service) validateCorrection(ctx context.Context, eventID, saleID string, types []importfile.TicketTypeRef, loc *time.Location, in CorrectSaleInput) (*importfile.ValidateResult, error) {
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
	validated := importfile.Validate(importfile.ValidateInput{
		Rows:     []importfile.RawRow{raw},
		Types:    types,
		Now:      s.now(),
		Location: loc,
	})
	if err := s.refuseImportRowsOverPurchaseLimit(ctx, eventID, types, &validated, saleID); err != nil {
		return nil, err
	}
	for _, impact := range validated.CapacityImpact {
		if impact.Oversold && validated.Rows[0].Valid {
			validated.RejectRow(0, importfile.RowError{
				Field:   importfile.ColQuantity,
				Message: fmt.Sprintf("exceeds the %d remaining on %s once this sale is reversed", impact.Remaining, impact.TicketTypeName),
			})
		}
	}
	return &validated, nil
}

// netOfSale gives the sale's own quantities back to the catalog snapshot, so
// the validator's capacity picture is the one the replacement will actually
// meet once the old sale is reversed.
func netOfSale(types []importfile.TicketTypeRef, lines []repository.SaleLineQuantity) []importfile.TicketTypeRef {
	out := make([]importfile.TicketTypeRef, len(types))
	copy(out, types)
	for i := range out {
		for _, l := range lines {
			if l.TicketTypeID == out[i].ID {
				out[i].SoldCount -= l.Quantity
			}
		}
	}
	return out
}
