package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales/importfile"
	"github.com/peter/ticket_pos/backend/internal/sales/repository"
)

// RecordedManualSale is what a Manually Recorded Sale hands back: the sale, its
// Sale Confirmation reference, and enough of the sale itself for the modal's
// session receipt to show a line per save without a second round trip.
//
// It carries the duplicate warning too. A duplicate never blocks the record
// (ADR 0052) — two people at the door with the same name on the same night is a
// thing that happens — but the organizer twenty names into a notebook is
// exactly the person who wants to be told they may just have typed one twice,
// and the receipt is where they will see it.
//
// ConfirmationSent is reported rather than assumed. The buyer is ALWAYS mailed,
// with no toggle and no request field, so this is false only when the mail
// itself failed — which does not undo the sale, and which the caller is
// entitled to know about.
type RecordedManualSale struct {
	SaleID            string    `json:"sale_id"`
	ConfirmationRef   string    `json:"confirmation_ref"`
	CustomerEmail     string    `json:"customer_email"`
	CustomerFirstName string    `json:"customer_first_name"`
	CustomerLastName  string    `json:"customer_last_name"`
	TicketTypeID      string    `json:"ticket_type_id"`
	TicketTypeName    string    `json:"ticket_type_name"`
	Quantity          int       `json:"quantity"`
	AmountCents       int       `json:"amount_cents"`
	Currency          string    `json:"currency"`
	SoldAt            time.Time `json:"sold_at"`
	PaymentMethod     string    `json:"payment_method"`
	ConfirmationSent  bool      `json:"confirmation_sent"`
	PossibleDuplicate bool      `json:"possible_duplicate"`
	DuplicateOfDate   string    `json:"duplicate_of_date,omitempty"`
}

// RecordManualSale records ONE Manually Recorded Sale (#368, parent #366,
// ADR 0052): a Sale Import row typed into a form instead of uploaded in a file.
//
// IT IS AN IMPORT ROW IN EVERY RESPECT BUT THE SPREADSHEET. The `import` Sales
// Channel, Sales Source `direct`, one Ticket Type, a Payment Method, a Sale
// Confirmation to the buyer, Tickets minted one per unit and capacity taken —
// and it is judged by the very validator the Sale Import preview and file commit
// use, through the shared typed-row verdict (#367). That sharing is not a
// convenience: two routes now record the same object, and if they validated
// separately the same act would succeed one way and fail the other for reasons
// nobody could see.
//
// IT IS NOT A SALE IMPORT. It belongs to no batch, so it never appears in the
// Import history and no batch undo can reach it — and, the point that decided
// the design, recording one never makes an earlier upload stop being the latest
// batch, so an existing batch stays undoable. Minting a one-row batch per typed
// sale would have quietly disarmed that guard.
//
// THE BUYER IS ALWAYS MAILED, with no toggle and no request field, as a Sale
// Import does and unlike a Sale Correction. ADR 0050 made the correction's
// Confirmation off-by-default because the buyer already holds one from the
// original import; no prior mail exists here, and suppressing it would ship a
// sale whose buyer holds no Confirmation Link and therefore cannot assign a
// Ticket or answer a Ticket Question. A mail that fails does not undo the sale:
// the sale is the record of money that changed hands, and the mail is a
// notification about it.
//
// THERE IS NO IDEMPOTENCY KEY, deliberately — there is no batch to hang one on,
// and Sale Correction, the closest precedent, has none either. The accepted hole
// and its mitigations are in ADR 0052's consequences.
//
// A refused row returns the verdict with a nil result and NOTHING WRITTEN: the
// caller reports one complaint per offending column. The transaction's own
// capacity check under lock remains the last word on a race lost after the
// verdict was reached, and surfaces as IMPORT_BATCH_FAILED.
func (s *Service) RecordManualSale(ctx context.Context, actor ActorContext, eventID string, in ImportRowInput) (*RecordedManualSale, *importfile.ValidateResult, error) {
	// An externally registered Event is refused here, before the body is judged
	// — loadImportContext's own guard, and the reason it runs before the Ticket
	// Types are read (ADR 0028). Such an Event has no Ticket Types, so every
	// body names one that does not exist, and "unknown ticket type" would send
	// the organizer hunting for a data problem that is not there.
	event, types, loc, err := s.loadImportContext(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, nil, err
	}

	validated, err := s.validateManualSale(ctx, actor.OrganizationID, eventID, types, loc, in)
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
	recorded, err := s.repo.RecordBatchlessImportSale(ctx, repository.RecordBatchlessImportSaleInput{
		EventID:        eventID,
		OrganizationID: actor.OrganizationID,
		Now:            s.now(),
		UpsertCustomer: s.customers.UpsertForSale,
		// The buyer holds Ticket 1 (ADR 0055). A typed row is a transcription
		// exactly as a file row is — the Member is recording a sale that already
		// happened — and there is deliberately no checkbox to opt out of it:
		// a Holder differing from the buyer must be written `assigned`, which
		// ADR 0047 shows no name for.
		SelfHeld: s.ticketAssignmentEnabled,
		Sale: repository.CommitSale{
			// No phone and no self-assertion, exactly as a file import records
			// none: an Organization's account of a sale it took off-platform
			// never proves the buyer owns the email. The Customer is upserted
			// unverified for the same reason.
			Customer: platform.SaleCustomer{
				Email:     row.CustomerEmail,
				FirstName: row.CustomerFirstName,
				LastName:  row.CustomerLastName,
				TaxID:     platform.SaleTaxID{Type: row.CustomerTaxIDType, Number: row.CustomerTaxIDNumber},
			},
			PaymentMethod:   row.PaymentMethod,
			SoldAt:          row.SoldAtTime(),
			ConfirmationRef: ref,
			// ONE LINE, ALWAYS. No `import`-channel sale has ever carried more
			// than one Ticket Sale Line and this route does not introduce the
			// first: the family buying two Adult and two Child records two
			// sales, which ADR 0052 acknowledges as a cost rather than denying.
			// A nil UnitPriceCents snapshots the Ticket Type's catalog price,
			// as a blank amount cell does; zero records a comp.
			Lines: []repository.CommitLine{{
				TicketTypeID:   row.TicketTypeID,
				Quantity:       row.Quantity,
				UnitPriceCents: row.AmountCents,
			}},
		},
	})
	if err != nil {
		return nil, nil, mapCommitError(err)
	}

	sent := s.email.SendSaleConfirmation(ctx, s.importedSaleConfirmation(ctx, event, *recorded)) == nil

	return &RecordedManualSale{
		SaleID:            recorded.ID,
		ConfirmationRef:   recorded.ConfirmationRef,
		CustomerEmail:     recorded.CustomerEmail,
		CustomerFirstName: recorded.CustomerFirstName,
		CustomerLastName:  recorded.CustomerLastName,
		TicketTypeID:      row.TicketTypeID,
		TicketTypeName:    row.TicketTypeName,
		Quantity:          row.Quantity,
		AmountCents:       recorded.AmountCents,
		Currency:          event.Currency,
		SoldAt:            row.SoldAtTime(),
		PaymentMethod:     row.PaymentMethod,
		ConfirmationSent:  sent,
		PossibleDuplicate: row.PossibleDuplicate,
		DuplicateOfDate:   row.DuplicateOfDate,
	}, nil, nil
}

// PreviewManualSale is the manual form's live verdict (#368): the row judged
// exactly as RecordManualSale would judge it, in the Sale Import preview's own
// shape, with nothing written.
//
// It exists twice over. It is the surface that tells the organizer what is
// wrong in the field that caused it, as they type — and it is the per-field
// complaint channel whose absence is the JSON commit's stated reason for
// exempting itself from the Purchase Limit. Supplying one is what makes
// enforcing the limit here reasonable rather than merely stricter (ADR 0052).
func (s *Service) PreviewManualSale(ctx context.Context, actor ActorContext, eventID string, in ImportRowInput) (*importfile.ValidateResult, error) {
	_, types, loc, err := s.loadImportContext(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	return s.validateManualSale(ctx, actor.OrganizationID, eventID, types, loc, in)
}

// validateManualSale judges the typed row as the shared verdict judges any
// single one (#367), and is the whole of both the preview and the gate the
// record passes through — so the live verdict and the refusal can never
// disagree.
//
// It supplies the ZERO typedRowContext, and both halves of that are the
// statement: nothing is being reversed, so no Ticket Sale is left out of the
// duplicate signal or the Purchase Limit tally — a sale recorded earlier in the
// same sitting is a real sale and counts against the limit like any other — and
// the capacity complaint is the plain shortfall, because the catalog snapshot it
// is measured against is simply the Event's, with nothing netted back into it.
// Its sibling, a Sale Correction, does the opposite on both counts.
//
// The exclusion for the Purchase Limit is "" for the same reason the file import
// passes "": there is no prior sale in the picture at all.
func (s *Service) validateManualSale(ctx context.Context, orgID, eventID string, types []importfile.TicketTypeRef, loc *time.Location, in ImportRowInput) (*importfile.ValidateResult, error) {
	return s.validateTypedImportRow(ctx, orgID, eventID, types, loc, in, typedRowContext{})
}
