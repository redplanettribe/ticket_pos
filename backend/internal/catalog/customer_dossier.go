package catalog

import "github.com/peter/ticket_pos/backend/internal/platform/apperror"

// ErrCustomerNotFoundAtEvent is returned when a Customer Dossier is asked for a
// Customer with nothing at the Event (#638, spec #635). It is exactly the answer
// an id naming no Customer at all gets — the platform's CUSTOMER_NOT_FOUND, a
// 404 — so the Dossier never tells a caller whether somebody bought elsewhere.
func ErrCustomerNotFoundAtEvent() apperror.DomainError {
	return apperror.New("CUSTOMER_NOT_FOUND", "Customer not found.", nil)
}
