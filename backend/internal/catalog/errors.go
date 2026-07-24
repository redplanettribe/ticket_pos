package catalog

import "github.com/peter/ticket_pos/backend/internal/platform/apperror"

// ErrEventNotFound is returned when an Event does not exist in the active Organization.
func ErrEventNotFound() apperror.DomainError {
	return apperror.New("EVENT_NOT_FOUND", "Event not found.", nil)
}

// ErrOrganizationNotFound is returned when a public Organization slug does not resolve.
func ErrOrganizationNotFound() apperror.DomainError {
	return apperror.New("ORGANIZATION_NOT_FOUND", "Organization not found.", nil)
}

// ErrEventSlugTaken is returned when an event slug is already in use within the organization.
func ErrEventSlugTaken(slug string) apperror.DomainError {
	return apperror.New("EVENT_SLUG_TAKEN", "Event slug is already taken in this organization.", map[string]any{
		"slug": slug,
	})
}

// ErrEventNotDraft is returned when a slug change is attempted on a non-draft Event.
func ErrEventNotDraft() apperror.DomainError {
	return apperror.New("EVENT_NOT_DRAFT", "This change is only allowed while the event is a draft.", nil)
}

// ErrEventNotPublished is returned when discoverability is changed on a non-published Event.
func ErrEventNotPublished() apperror.DomainError {
	return apperror.New("EVENT_NOT_PUBLISHED", "Only published events can be made discoverable.", nil)
}

// ErrEventDeleteForbidden is returned when delete is attempted on a published or cancelled Event.
func ErrEventDeleteForbidden() apperror.DomainError {
	return apperror.New("EVENT_DELETE_FORBIDDEN", "Only draft events can be deleted.", nil)
}

// ErrEventPublishRequirementsNotMet is returned when publish is attempted without required fields.
func ErrEventPublishRequirementsNotMet(missingFields []string) apperror.DomainError {
	return apperror.New(
		"EVENT_PUBLISH_REQUIREMENTS_NOT_MET",
		"Event cannot be published until all required fields are set.",
		map[string]any{"missing_fields": missingFields},
	)
}

// ErrEventAlreadyPublished is returned when publish is attempted on a published Event.
func ErrEventAlreadyPublished() apperror.DomainError {
	return apperror.New("EVENT_ALREADY_PUBLISHED", "Event is already published.", nil)
}

// ErrEventAlreadyCancelled is returned when cancel is attempted on a cancelled Event.
func ErrEventAlreadyCancelled() apperror.DomainError {
	return apperror.New("EVENT_ALREADY_CANCELLED", "Event is already cancelled.", nil)
}

// ErrTicketTypeNotFound is returned when a Ticket Type does not exist on the Event.
func ErrTicketTypeNotFound() apperror.DomainError {
	return apperror.New("TICKET_TYPE_NOT_FOUND", "Ticket type not found.", nil)
}

// ErrTicketTypeDeleteForbidden is returned when delete is attempted while the parent Event is not draft.
func ErrTicketTypeDeleteForbidden() apperror.DomainError {
	return apperror.New("TICKET_TYPE_DELETE_FORBIDDEN", "Ticket types can only be deleted while the event is a draft.", nil)
}

// ErrCurrencyLocked is returned when Organization currency change is attempted after Ticket Types exist.
func ErrCurrencyLocked() apperror.DomainError {
	return apperror.New("CURRENCY_LOCKED", "Currency cannot be changed after ticket types have been created.", nil)
}

// ErrCoverUploadUnavailable is returned when object storage is not configured.
func ErrCoverUploadUnavailable() apperror.DomainError {
	return apperror.New("COVER_UPLOAD_UNAVAILABLE", "Cover image upload is not available.", nil)
}

// ErrInvalidCoverImageKey is returned when a cover_image_key does not belong to the event.
func ErrInvalidCoverImageKey() apperror.DomainError {
	return apperror.New("INVALID_COVER_IMAGE_KEY", "Cover image key is not valid for this event.", nil)
}

// ErrInvalidTag is returned when one or more Tag names fail normalization or validation.
func ErrInvalidTag(invalid []string) apperror.DomainError {
	return apperror.New(
		"INVALID_TAG",
		"Tags must be 1-30 characters using only letters, numbers, spaces, and hyphens.",
		map[string]any{"invalid": invalid},
	)
}

// ErrTooManyTags is returned when an Event would exceed the maximum number of Tags.
func ErrTooManyTags(max int) apperror.DomainError {
	return apperror.New(
		"TOO_MANY_TAGS",
		"An event has too many tags.",
		map[string]any{"max": max},
	)
}
