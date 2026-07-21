// Package sales holds sales domain errors and shared types.
package sales

import "github.com/peter/ticket_pos/backend/internal/platform/apperror"

type domainError struct {
	code    string
	message string
	details any
}

func (e *domainError) Error() string   { return e.message }
func (e *domainError) Code() string    { return e.code }
func (e *domainError) Message() string { return e.message }
func (e *domainError) Details() any    { return e.details }

func newDomainError(code, message string, details any) apperror.DomainError {
	return &domainError{code: code, message: message, details: details}
}

// ErrEventNotFound is returned when the target Event does not exist in the active Organization.
func ErrEventNotFound() apperror.DomainError {
	return newDomainError("EVENT_NOT_FOUND", "Event not found.", nil)
}

// ErrTicketTypeNotFound is returned when a row references a Ticket Type not on the Event.
func ErrTicketTypeNotFound(ticketTypeID string) apperror.DomainError {
	return newDomainError("TICKET_TYPE_NOT_FOUND", "Ticket Type not found on this Event.", map[string]any{
		"ticket_type_id": ticketTypeID,
	})
}

// ErrImportFileUnreadable is returned when an uploaded Sale Import file cannot be
// parsed (wrong format, missing columns, corrupt contents).
func ErrImportFileUnreadable(reason string) apperror.DomainError {
	return newDomainError("IMPORT_FILE_INVALID", "Import file could not be read.", map[string]any{
		"reason": reason,
	})
}

// ErrImportFileTooLarge is returned when an uploaded Sale Import file exceeds the
// per-batch row limit.
func ErrImportFileTooLarge(limit int) apperror.DomainError {
	return newDomainError("IMPORT_FILE_TOO_LARGE", "Import file has too many rows.", map[string]any{
		"limit": limit,
	})
}

// ErrImportBatchNotFound is returned when the target Sale Import batch does not
// exist on the Event.
func ErrImportBatchNotFound(batchID string) apperror.DomainError {
	return newDomainError("IMPORT_BATCH_NOT_FOUND", "Sale Import batch not found on this Event.", map[string]any{
		"batch_id": batchID,
	})
}

// ErrImportNotLatestBatch is returned when an undo targets a batch that is not
// the most recent one on the Event. Only the latest batch is reversible.
func ErrImportNotLatestBatch(batchID string) apperror.DomainError {
	return newDomainError("IMPORT_NOT_LATEST_BATCH", "Only the most recent Sale Import can be undone.", map[string]any{
		"batch_id": batchID,
	})
}

// ErrImportAlreadyReversed is returned when an undo targets a batch that has
// already been reversed. Reversal is not repeatable.
func ErrImportAlreadyReversed(batchID string) apperror.DomainError {
	return newDomainError("IMPORT_ALREADY_REVERSED", "This Sale Import has already been undone.", map[string]any{
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
	return newDomainError("IMPORT_BATCH_FAILED", "Import could not be completed.", d)
}
