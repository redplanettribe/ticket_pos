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

// ErrEventIsExternalRegistration is returned when something that would only make
// sense on a ticketed Event is attempted on one that registers externally: here,
// creating a Ticket Type on it.
//
// The dedicated code exists because the alternative is worse than unhelpful.
// Every one of these paths would fail anyway — each resolves a Ticket Type that
// does not exist — but it would fail as a not-found, sending a staff member or an
// Integration Partner's program hunting for a data problem that is not there. The
// refusal names the actual reason: the two modes are exclusive (ADR 0028).
func ErrEventIsExternalRegistration() apperror.DomainError {
	return apperror.New(
		"EVENT_IS_EXTERNAL_REGISTRATION",
		"This event registers externally and does not sell tickets. Switch it back to selling tickets first.",
		nil,
	)
}

// ErrEventHasTicketTypes is returned when an Event would be switched to External
// Registration while Ticket Types still exist on it — the other side of the
// exclusivity invariant, which spans two tables and so cannot be a CHECK
// constraint. The fix is in the organizer's hands: delete the Ticket Types first.
func ErrEventHasTicketTypes() apperror.DomainError {
	return apperror.New(
		"EVENT_HAS_TICKET_TYPES",
		"This event cannot register externally while it still has ticket types. Delete them first.",
		nil,
	)
}

// ErrEventRegistrationModeLocked is returned when the mode of an Event that is no
// longer a draft would change, in either direction (ADR 0028, issue #208).
//
// Publishing is the moment the mode sets. Ticketed → external on a published Event
// would orphan real Ticket Sales, leaving Customers holding Sale Confirmations for
// an Event whose page no longer mentions tickets while capacity, Net Proceeds and
// the Withdrawable Balance still count those sales. External → ticketed risks
// nothing in itself, but passes through the exact state the publish gate exists to
// forbid: published with zero Ticket Types, which renders as an empty ticket
// selector on a listing card that shows no price and is not marked sold out.
//
// The Event status machine has no unpublish transition, so the escape hatch for a
// genuine change of mind is a new Event — which is what the message says, because
// an organizer told only "no" would go looking for a setting that does not exist.
func ErrEventRegistrationModeLocked() apperror.DomainError {
	return apperror.New(
		"EVENT_REGISTRATION_MODE_LOCKED",
		"How an event takes sign-ups is settled when it is published and cannot change afterwards. Create a new event instead.",
		nil,
	)
}

// ErrEventRegistrationURLRequired is returned when an update would leave a
// published externally registered Event with no Registration Link.
//
// The link may be corrected — a typo, a rescheduled registration page — but never
// emptied, because on such an Event it is the only way in: there are no Ticket
// Types behind it, so an Event without it is a published page its audience cannot
// act on. Distinct from the format refusal (INVALID_REGISTRATION_URL), which is
// about a value this platform will not store at all.
func ErrEventRegistrationURLRequired() apperror.DomainError {
	return apperror.New(
		"EVENT_REGISTRATION_URL_REQUIRED",
		"A published event that registers externally must keep its registration link: it is the only way in. Replace it rather than removing it.",
		nil,
	)
}

// ErrTicketTypeDeleteForbidden is returned when delete is attempted while the parent Event is not draft.
func ErrTicketTypeDeleteForbidden() apperror.DomainError {
	return apperror.New("TICKET_TYPE_DELETE_FORBIDDEN", "Ticket types can only be deleted while the event is a draft.", nil)
}

// ErrPromotionNotFound is returned when a Ticket Type has no Promotion to update or remove.
func ErrPromotionNotFound() apperror.DomainError {
	return apperror.New("PROMOTION_NOT_FOUND", "This ticket type has no promotion.", nil)
}

// ErrPromotionAlreadyExists is returned when a Promotion is set on a Ticket Type
// that already has one: there is one slot per Ticket Type (ADR 0021), so the
// existing Promotion is edited or removed rather than joined by a second.
func ErrPromotionAlreadyExists() apperror.DomainError {
	return apperror.New(
		"PROMOTION_ALREADY_EXISTS",
		"This ticket type already has a promotion. Edit or remove it first.",
		nil,
	)
}

// ErrPromotionalPriceNotBelowListPrice is returned when a Promotional Price is
// not strictly below the Ticket Type's List Price. Zero is allowed; equal is not
// — a Promotion that changes nothing is a Promotion in name only.
func ErrPromotionalPriceNotBelowListPrice(promotionalPriceCents, listPriceCents int) apperror.DomainError {
	return apperror.New(
		"PROMOTIONAL_PRICE_NOT_BELOW_LIST_PRICE",
		"The promotional price must be lower than the ticket type's price.",
		map[string]any{
			"promotional_price_cents": promotionalPriceCents,
			"list_price_cents":        listPriceCents,
		},
	)
}

// ErrListPriceNotAbovePromotionalPrice is returned when a List Price edit would
// leave an existing Promotion at or above it. The same invariant as
// ErrPromotionalPriceNotBelowListPrice seen from the other write, and a distinct
// code because the fix is a different one: adjust or remove the Promotion.
func ErrListPriceNotAbovePromotionalPrice(listPriceCents, promotionalPriceCents int) apperror.DomainError {
	return apperror.New(
		"LIST_PRICE_NOT_ABOVE_PROMOTIONAL_PRICE",
		"This price is not above the ticket type's promotional price. Adjust or remove the promotion first.",
		map[string]any{
			"list_price_cents":        listPriceCents,
			"promotional_price_cents": promotionalPriceCents,
		},
	)
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

// ErrVideoUploadUnavailable is returned when object storage is not configured.
func ErrVideoUploadUnavailable() apperror.DomainError {
	return apperror.New("VIDEO_UPLOAD_UNAVAILABLE", "Cover video upload is not available.", nil)
}

// ErrInvalidCoverVideoKey is returned when a cover_video_key does not belong to the event.
// A key under the covers prefix is invalid here too: the two prefixes are disjoint by
// design so an image key can never attach to the video slot (ADR 0020).
func ErrInvalidCoverVideoKey() apperror.DomainError {
	return apperror.New("INVALID_COVER_VIDEO_KEY", "Cover video key is not valid for this event.", nil)
}

// ErrCoverVideoRequiresCoverImage is returned when an Event update would leave a Cover
// Video without the Cover Image that serves as its poster — whether by attaching a video
// to an imageless Event or by clearing the image out from under an existing video. The
// invariant is evaluated against the final state of the update, so a single request that
// sets both is fine and one that clears both is fine.
func ErrCoverVideoRequiresCoverImage() apperror.DomainError {
	return apperror.New(
		"COVER_VIDEO_REQUIRES_COVER_IMAGE",
		"A cover video requires a cover image as its poster: add a cover image first, and remove the video before removing the image.",
		nil,
	)
}

// ErrInvalidTag is returned when one or more Tag names fail normalization or validation.
func ErrInvalidTag(invalid []string) apperror.DomainError {
	return apperror.New(
		"INVALID_TAG",
		"Tags must be 1-30 characters using only letters, numbers, spaces, and hyphens.",
		map[string]any{"invalid": invalid},
	)
}

// ErrTagNotFound is returned when a canonical key names no Tag in the shared
// pool (#218).
//
// It exists because a Follow of a Tag has to fail when the Tag is not there,
// rather than coin one. Every other way a Tag enters the pool is an Organization
// naming it on an Event (ADR 0004) — a considered act, by somebody who will see
// it rendered — and a Customer following a word is neither. Coining on Follow
// would fill the shared pool with typos nothing displays, and leave Follows that
// no Event can ever match feeding the Follow Digest.
func ErrTagNotFound() apperror.DomainError {
	return apperror.New("TAG_NOT_FOUND", "Tag not found.", nil)
}

// ErrTooManyTags is returned when an Event would exceed the maximum number of Tags.
func ErrTooManyTags(max int) apperror.DomainError {
	return apperror.New(
		"TOO_MANY_TAGS",
		"An event has too many tags.",
		map[string]any{"max": max},
	)
}
