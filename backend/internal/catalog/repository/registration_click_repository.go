package repository

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// RecordRegistrationClick counts one hand-off to an Event's Registration Link,
// resolved by the two Storefront slugs the Customer's URL carries.
//
// One statement, no read first, exactly as the Affiliate Link counter is
// written: hand-offs arrive concurrently from everyone reading the Event page,
// and a read-modify-write would lose them under any load worth counting.
//
// An address that resolves to nothing updates no row and is not an error — an
// unknown Organization, an unknown Event, or an Event that sells Ticket Types
// here and therefore hands nobody anywhere. The caller is a Customer's
// navigation, and there is nothing to tell them either way.
//
// updated_at is deliberately left alone, unlike the Affiliate Link counter's
// UPDATE. On an Event that column reads as "when an organizer last changed this
// Event", and a stranger's click is not an edit: bumping it would make every
// hand-off look like a change nobody made.
func (r *Repository) RecordRegistrationClick(ctx context.Context, organizationSlug, eventSlug string) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		UPDATE events e
		SET registration_click_count = e.registration_click_count + 1
		FROM organizations o
		WHERE o.id = e.organization_id
		  AND o.slug = $1
		  AND e.slug = $2
		  AND e.registration_mode = $3
	`, organizationSlug, eventSlug, string(catalog.RegistrationModeExternal))
	return err
}
