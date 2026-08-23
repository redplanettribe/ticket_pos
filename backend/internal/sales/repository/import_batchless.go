package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// batchlessImportSale is ONE Direct Sale to record on the `import` channel
// without a Sale Import batch: the sale itself, already validated and priced,
// and who is writing it when.
type batchlessImportSale struct {
	EventID        string
	OrganizationID string
	Sale           CommitSale
	Now            time.Time
	UpsertCustomer UpsertCustomer
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
// under-lock capacity check that is the last word on a race, Tickets minted one
// per unit and all `unassigned` with none self-held, no fee snapshot, and a NULL
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
