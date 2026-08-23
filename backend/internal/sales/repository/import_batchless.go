package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// RecordBatchlessImportSaleInput is ONE Manually Recorded Sale to write: the
// sale itself, already validated and priced by the shared typed-import-row
// verdict, and who is writing it when.
type RecordBatchlessImportSaleInput struct {
	EventID        string
	OrganizationID string
	Sale           CommitSale
	Now            time.Time
	UpsertCustomer UpsertCustomer
	// SelfHeld makes the buyer the Holder of the sale's Ticket 1 (ADR 0055),
	// set from TICKET_ASSIGNMENT_ENABLED by the service.
	SelfHeld bool
}

// RecordBatchlessImportSale records ONE Manually Recorded Sale in a transaction
// of its own (#368, ADR 0052): a Sale Import row that was typed instead of
// uploaded, on the `import` channel with Sales Source `direct` and no batch.
//
// IT OWNS THE TRANSACTION, which is the whole difference between it and its
// sibling below. A Sale Correction has a reversal to do first and therefore
// supplies its own boundary; a Manually Recorded Sale reverses nothing, so the
// sale is the whole of the unit of work and there is nothing for a caller to
// coordinate with. Commit-as-you-go is the decision this expresses: each typed
// sale is complete and final the moment it is saved, and no session, draft or
// accumulated list spans two of them (ADR 0052).
//
// NO BATCH IS THE POINT — see commitBatchlessImportSaleTx, which does the write.
// Recording one never makes an earlier upload stop being the latest batch, so
// batch undo's latest-only guard is left exactly as it was, and a later undo of
// any batch walks straight past this sale.
//
// Refusals are the sale-commit spine's: *CapacityError when the race between the
// service's validation and this lock was lost, *UnknownTicketTypeError when the
// Ticket Type went away underneath the form.
func (r *Repository) RecordBatchlessImportSale(ctx context.Context, in RecordBatchlessImportSaleInput) (*RecordedSale, error) {
	tx, err := r.db.Pool.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	recorded, err := r.commitBatchlessImportSaleTx(ctx, tx, batchlessImportSale{
		EventID:        in.EventID,
		OrganizationID: in.OrganizationID,
		Sale:           in.Sale,
		Now:            in.Now,
		UpsertCustomer: in.UpsertCustomer,
		SelfHeld:       in.SelfHeld,
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return recorded, nil
}

// batchlessImportSale is ONE Direct Sale to record on the `import` channel
// without a Sale Import batch: the sale itself, already validated and priced,
// and who is writing it when.
type batchlessImportSale struct {
	EventID        string
	OrganizationID string
	Sale           CommitSale
	Now            time.Time
	UpsertCustomer UpsertCustomer
	// SelfHeld makes the buyer the Holder of the sale's Ticket 1 (ADR 0055).
	// BOTH CALLERS PASS IT AND NEITHER MAY DEFAULT: a Sale Correction whose
	// replacement forgot it would drop the buyer off the roster in the act of
	// correcting their details, which is the bug ADR 0055 named.
	SelfHeld bool
}

// commitBatchlessImportSaleTx records one imported Ticket Sale that belongs to
// no Sale Import batch, inside a transaction the caller already opened (#367).
//
// IT TAKES A TRANSACTION AND DOES NOT OWN ONE, because its two callers need
// different boundaries and only one of them is about this sale alone. A Sale
// Correction reverses the mistaken sale first and records this one second in the
// SAME transaction, so that the reversal has already given the quantities back
// by the time the spine locks the Ticket Type — capacity comes out net without
// anybody subtracting anything, and a lost race rolls the whole thing back with
// the original sale exactly as active as it was (ADR 0050). A Manually Recorded
// Sale reverses nothing and wraps this in a transaction of its own (ADR 0052).
//
// NO BATCH IS THE POINT. import_batch_id stays NULL, so a later undo of any
// batch walks straight past this sale, and recording one never makes an earlier
// upload stop being the latest batch — the guard batch undo depends on. Channel
// `import`, source `direct`: an imported sale in every respect but the
// spreadsheet.
//
// Everything else is the channel-agnostic sale-commit spine's, unvaried: the
// under-lock capacity check that is the last word on a race, one Ticket minted
// per unit with Ticket 1 held by the buyer and the rest `unassigned` (ADR 0055,
// and only while TICKET_ASSIGNMENT_ENABLED is open), no fee snapshot, and a NULL
// Sale Locale so the buyer's mail falls to their remembered language.
func (r *Repository) commitBatchlessImportSaleTx(ctx context.Context, tx *sql.Tx, in batchlessImportSale) (*RecordedSale, error) {
	recorded, err := r.CommitSales(ctx, tx, CommitSalesInput{
		EventID:        in.EventID,
		OrganizationID: in.OrganizationID,
		Channel:        "import",
		Source:         "direct",
		Sales:          []CommitSale{in.Sale},
		Now:            in.Now,
		UpsertCustomer: in.UpsertCustomer,
		SelfHeld:       in.SelfHeld,
	})
	if err != nil {
		return nil, err
	}
	// The spine returns one RecordedSale per sale handed to it and was handed
	// exactly one. Anything else means the spine changed shape underneath a
	// caller that is about to read recorded[0], so it is refused loudly here
	// rather than panicking a request later.
	if len(recorded) != 1 {
		return nil, errors.New("sales: a batchless import sale recorded no Ticket Sale")
	}
	return &recorded[0], nil
}
