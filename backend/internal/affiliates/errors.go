// Package affiliates owns Affiliate Links: named, trackable links to an Event's
// page that attribute Online Sales to whoever is promoting the Event.
package affiliates

import "github.com/peter/ticket_pos/backend/internal/platform/apperror"

// ErrEventNotFound is returned when the Event does not exist in the active
// Organization. It reuses the catalog code deliberately: from the caller's side
// this is the same "no such Event here" the rest of the staff Event family
// answers with, and an Affiliate Link surface must not tell a stranger that an
// Event exists elsewhere.
func ErrEventNotFound() apperror.DomainError {
	return apperror.New("EVENT_NOT_FOUND", "Event not found.", nil)
}

// ErrAffiliateLinkNotFound is returned when the Event has no Affiliate Link with
// the given id. Scoped to the Event on purpose: a link id from another Event is
// the same "no such link here" as one that never existed.
func ErrAffiliateLinkNotFound() apperror.DomainError {
	return apperror.New("AFFILIATE_LINK_NOT_FOUND", "Affiliate link not found.", nil)
}

// ErrAffiliateLinkHasHistory refuses to delete an Affiliate Link that has done
// something. Its own code, distinct from every other conflict, because the fix
// is specific: a link with clicks or attributed sales is deactivated, not
// deleted — deleting it would rewrite what happened, and an attributed Ticket
// Sale would lose the link it names.
func ErrAffiliateLinkHasHistory() apperror.DomainError {
	return apperror.New(
		"AFFILIATE_LINK_HAS_HISTORY",
		"This affiliate link has clicks or attributed sales and can no longer be deleted. Deactivate it instead.",
		nil,
	)
}

// ErrAffiliateLinkCodeExhausted is returned when the generator could not find an
// unused code for the Event after several attempts. Practically unreachable at
// the code length in use; loud rather than silent if the assumption ever breaks.
func ErrAffiliateLinkCodeExhausted() apperror.DomainError {
	return apperror.New(
		"AFFILIATE_LINK_CODE_EXHAUSTED",
		"Could not generate an affiliate link code. Please try again.",
		nil,
	)
}
