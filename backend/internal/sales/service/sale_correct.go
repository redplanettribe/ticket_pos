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
// record nothing an import would have refused (ADR 0050). The columns
// themselves are ImportRowInput, shared with every other route that types one
// import row rather than uploading it (#367); what is left here is the one
// thing that is a correction's alone.
type CorrectSaleInput struct {
	ImportRowInput
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
// fresh Tickets, a new Sale Confirmation reference and no batch.
//
// THE REPLACEMENT RE-SEATS THE BUYER. ADR 0050 said its Tickets all start
// `unassigned`; ADR 0055 amends that for Ticket 1, which the buyer holds as a
// Self-held Ticket exactly as they would on any imported Sale. Correcting a
// typo therefore keeps the buyer on the roster instead of dropping them off it
// — the behaviour 0050 would have chosen had a Self-held Ticket existed then.
//
// THE REPLACEMENT IS AN IMPORT ROW. It is judged by the same validator the Sale
// Import preview and file commit use, against a catalog snapshot with the old
// sale's quantities already given back and a Purchase Limit tally that leaves
// the old sale out: capacity and the limit are counted NET of what is being
// reversed, so re-recording the same quantity always fits. A refusal returns
// the row's verdict with nothing written and the original sale still active;
// the transaction's own capacity check under lock is the last word on a race.
//
// WHO IS TOLD. Every Holder who accepted an Assignment Link on the old sale,
// unconditionally, through the displaced-Holder mail every reversal route
// shares (#327) — but a buyer holding by presumption follows the buyer's own
// notification policy, which on this path is the Sale Confirmation checkbox
// (#392, ADR 0055); see the call below. The buyer gets NO voided mail, ever,
// and the replacement's Sale Confirmation only when the form asked for it —
// with the outstanding-answers line, because fresh Tickets owe every answer
// (#315).
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

	validated, err := s.validateCorrection(ctx, actor.OrganizationID, eventID, saleID, types, loc, in)
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
	terms := s.commitTerms(s.now())
	corrected, err := s.repo.CorrectImportedSale(ctx, repository.CorrectImportedSaleInput{
		EventID:        eventID,
		OrganizationID: actor.OrganizationID,
		SaleID:         saleID,
		// The replacement's buyer holds its Ticket 1 (ADR 0055), so a correction
		// re-seats them on the roster in the same act that took the mistaken sale
		// off it. Were the record path's terms not applied here, this would be a
		// correction that costs the buyer their place — which is what #392's
		// notice policy is written against.
		Terms: terms,
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

	// THE HOLDERS OF THE SALE BEING REPLACED ARE TOLD, and the new Sale
	// Confirmation checkbox is the buyer's notification policy on this path (#392,
	// ADR 0055). A buyer who holds one of these Tickets by presumption is spared
	// by default — the correction re-seats them in the same act, so telling them
	// they had lost a ticket would not even be true by the time they read it — and
	// is told when the Member chooses to write. A Holder who accepted by
	// Assignment Link is told either way: their Ticket really is gone, and the
	// replacement's Tickets are not theirs.
	//
	// THE MEMBER'S CHOICE AND NOT THE DELIVERY. `sent` below can still be false if
	// the provider was unwell; what decides this is what the Member asked for.
	s.tellDisplacedHolders(ctx, []string{corrected.Reversed.ID}, platform.BuyerNoticeFromSaleCorrectionConfirmation(in.SendConfirmation))

	rs := corrected.Replacement
	sent := false
	if in.SendConfirmation {
		if err := s.email.SendSaleConfirmation(ctx, s.importedSaleConfirmation(ctx, event, rs)); err == nil {
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

// PreviewCorrection is the Correct form's live verdict (#352): the replacement
// judged exactly as CorrectImportedSale would judge it — the import row rules,
// capacity and the Purchase Limit net of the sale being corrected, the
// duplicate-of-an-active-sale warning — in the import preview's own shape, and
// nothing written. The same refusals as the commit stand in front of it: a
// sale that is not an imported one, or is already reversed, has no correction
// to preview.
func (s *Service) PreviewCorrection(ctx context.Context, actor ActorContext, eventID, saleID string, in CorrectSaleInput) (*importfile.ValidateResult, error) {
	_, types, loc, err := s.loadImportContext(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	oldLines, err := s.repo.ImportedSaleLines(ctx, actor.OrganizationID, eventID, saleID)
	if err != nil {
		return nil, mapReverseSaleError(err)
	}
	return s.validateCorrection(ctx, actor.OrganizationID, eventID, saleID, netOfSale(types, oldLines), loc, in)
}

// validateCorrection judges the replacement as the shared typed-import-row
// verdict judges any single typed row (#367), supplying the two things that
// make it a CORRECTION rather than a plain record: the sale being reversed is
// left out of the duplicate signal and the Purchase Limit tally — it is about to
// stop existing, and the replacement would otherwise read as a duplicate of
// itself and be refused an allowance it is giving back — and the capacity
// complaint says so, because the snapshot it is measured against has already had
// the old quantities netted back into it by the caller.
//
// It is the whole of the verdict, shared by the commit and the preview so the
// two can never disagree (#352).
func (s *Service) validateCorrection(ctx context.Context, orgID, eventID, saleID string, types []importfile.TicketTypeRef, loc *time.Location, in CorrectSaleInput) (*importfile.ValidateResult, error) {
	return s.validateTypedImportRow(ctx, orgID, eventID, types, loc, in.ImportRowInput, typedRowContext{
		ExcludeSaleID: saleID,
		OverCapacity: func(impact importfile.CapacityImpact) string {
			return fmt.Sprintf("exceeds the %d remaining on %s once this sale is reversed", impact.Remaining, impact.TicketTypeName)
		},
	})
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
