// Package sales holds sales domain errors and shared types.
package sales

import (
	"fmt"

	"github.com/peter/ticket_pos/backend/internal/platform/apperror"
)

// ErrEventNotFound is returned when the target Event does not exist in the active Organization.
func ErrEventNotFound() apperror.DomainError {
	return apperror.New("EVENT_NOT_FOUND", "Event not found.", nil)
}

// ErrTicketTypeNotFound is returned when a row references a Ticket Type not on the Event.
func ErrTicketTypeNotFound(ticketTypeID string) apperror.DomainError {
	return apperror.New("TICKET_TYPE_NOT_FOUND", "Ticket Type not found on this Event.", map[string]any{
		"ticket_type_id": ticketTypeID,
	})
}

// ErrCapacityExceeded is returned when a checkout requests more tickets of a
// Ticket Type than remain. Available is capacity minus sold count at the moment
// of the check.
func ErrCapacityExceeded(ticketTypeID string, requested, available int) apperror.DomainError {
	return apperror.New("CAPACITY_EXCEEDED", "Not enough tickets remaining.", map[string]any{
		"ticket_type_id": ticketTypeID,
		"requested":      requested,
		"available":      available,
	})
}

// ErrPaymentNotFound is returned when no Payment carries the given client
// transaction id.
func ErrPaymentNotFound() apperror.DomainError {
	return apperror.New("PAYMENT_NOT_FOUND", "Payment not found.", nil)
}

// ErrPaymentSaleCommitFailed is returned for the one loudly-logged incident
// case: the Payment Provider approved the charge but recording the Ticket Sale
// failed, leaving an approved Payment without a sale for the platform operator
// to resolve by hand.
func ErrPaymentSaleCommitFailed() apperror.DomainError {
	return apperror.New("PAYMENT_SALE_COMMIT_FAILED", "Your payment was approved but your tickets could not be recorded. Please contact support.", nil)
}

// ErrImportFileUnreadable is returned when an uploaded Sale Import file cannot be
// parsed (wrong format, missing columns, missing Sales sheet, corrupt or empty
// contents). The reason is a human-readable sentence produced by the importfile
// parser; it becomes the message the organizer sees so both the preview UI and
// any API consumer get the specific dead-end without translating error codes.
func ErrImportFileUnreadable(reason string) apperror.DomainError {
	return apperror.New("IMPORT_FILE_INVALID", reason, map[string]any{
		"reason": reason,
	})
}

// ErrImportFileTooLarge is returned when an uploaded Sale Import file exceeds the
// per-batch row limit. The message states the limit; details carries it too.
func ErrImportFileTooLarge(limit int) apperror.DomainError {
	return apperror.New("IMPORT_FILE_TOO_LARGE", fmt.Sprintf("Import file has too many rows. The maximum is %d rows per import.", limit), map[string]any{
		"limit": limit,
	})
}

// ErrImportBatchNotFound is returned when the target Sale Import batch does not
// exist on the Event.
func ErrImportBatchNotFound(batchID string) apperror.DomainError {
	return apperror.New("IMPORT_BATCH_NOT_FOUND", "Sale Import batch not found on this Event.", map[string]any{
		"batch_id": batchID,
	})
}

// ErrImportNotLatestBatch is returned when an undo targets a batch that is not
// the most recent one on the Event. Only the latest batch is reversible.
func ErrImportNotLatestBatch(batchID string) apperror.DomainError {
	return apperror.New("IMPORT_NOT_LATEST_BATCH", "Only the most recent Sale Import can be undone.", map[string]any{
		"batch_id": batchID,
	})
}

// ErrImportAlreadyReversed is returned when an undo targets a batch that has
// already been reversed. Reversal is not repeatable.
func ErrImportAlreadyReversed(batchID string) apperror.DomainError {
	return apperror.New("IMPORT_ALREADY_REVERSED", "This Sale Import has already been undone.", map[string]any{
		"batch_id": batchID,
	})
}

// ErrImportBatchFailed is returned when a Sale Import cannot be committed as a
// whole. The batch is all-or-nothing; details identify the first offending row
// and the reason (e.g. CAPACITY_EXCEEDED).
func ErrImportBatchFailed(row int, reason string, details map[string]any) apperror.DomainError {
	d := map[string]any{"row": row, "reason": reason}
	for k, v := range details {
		d[k] = v
	}
	return apperror.New("IMPORT_BATCH_FAILED", "Import could not be completed.", d)
}
