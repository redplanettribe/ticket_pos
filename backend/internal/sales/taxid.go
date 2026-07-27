package sales

import (
	"fmt"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// RequireTaxID is the one statement of ADR 0016's Tax ID requirement: a Ticket
// Sale on a native Sales Channel (`online`, `in_person`) must carry the Tax ID
// it is transacted under, while the `import` channel accepts its absence —
// those sales happened elsewhere and the ID may never have been collected.
// Entry points validate user input against it and the sale-commit spine asserts
// it, so a future channel cannot skip the requirement by accident.
//
// Handlers reject a missing Tax ID with field-level validation errors long
// before this runs; a failure here is an invariant breach — some code path
// reached the spine without validating — so the error is a plain one, mirroring
// the spine's UpsertCustomer guard, not a user-facing DomainError.
func RequireTaxID(channel string, taxID platform.SaleTaxID) error {
	set := taxID.Type != "" && taxID.Number != ""
	if channel != "import" && !set {
		return fmt.Errorf("sales: a Tax ID is required to record a sale on the %q channel (ADR 0016)", channel)
	}
	return nil
}
