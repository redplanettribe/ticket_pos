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

// ErrTicketQuestionsUnavailable is returned when the Ticket Question authoring
// surface is asked for while the feature flag is off (ADR 0045).
//
// It maps to 404 and not 403, deliberately. 403 would say "this exists and you
// may not have it", which invites an Organization to ask why and a support agent
// to answer. While the flag is off there is nothing here: these endpoints answer
// exactly as an unrouted path does today, which is the whole point of shipping
// dark — no Storefront surface and no export differs from a build without the
// feature, and neither does the staff API.
func ErrTicketQuestionsUnavailable() apperror.DomainError {
	return apperror.New("TICKET_QUESTIONS_UNAVAILABLE", "Not found.", nil)
}

// ErrInvalidTicketQuestionLabel is returned when a label is blank once trimmed,
// or longer than the cap. It names which label so an editor holding a question
// and twenty Options can point at the offending field rather than the form.
func ErrInvalidTicketQuestionLabel(field string, maxLength int) apperror.DomainError {
	return apperror.New(
		"INVALID_TICKET_QUESTION_LABEL",
		"Label must not be empty and must be within the length limit.",
		map[string]any{"field": field, "max_length": maxLength},
	)
}

// ErrTicketQuestionNotFound is returned when a Ticket Question does not exist on
// the Ticket Type.
func ErrTicketQuestionNotFound() apperror.DomainError {
	return apperror.New("TICKET_QUESTION_NOT_FOUND", "Ticket question not found.", nil)
}

// ErrTicketQuestionOptionNotFound is returned when an Option does not exist on
// the Ticket Question.
func ErrTicketQuestionOptionNotFound() apperror.DomainError {
	return apperror.New("TICKET_QUESTION_OPTION_NOT_FOUND", "Ticket question option not found.", nil)
}

// ErrTicketQuestionKindFrozen is returned when a kind change is attempted on a
// Ticket Question that some Ticket has already answered.
//
// The message names the way out rather than only the refusal, because there is
// one: retire this question and add another. Reachable since #310 landed the
// Answer — see catalog.TicketQuestionKindFrozen.
func ErrTicketQuestionKindFrozen() apperror.DomainError {
	return apperror.New(
		"TICKET_QUESTION_KIND_FROZEN",
		"This question has been answered, so its type can no longer change. Retire it and add a new question instead.",
		nil,
	)
}

// ErrTicketQuestionRetired is returned when an edit reaches a retired Ticket
// Question. A retired thing is kept so that what has already been answered still
// reads; it is not an authoring surface.
func ErrTicketQuestionRetired() apperror.DomainError {
	return apperror.New(
		"TICKET_QUESTION_RETIRED",
		"This question has been retired and can no longer be edited.",
		nil,
	)
}

// ErrTicketQuestionOptionRetired is the same refusal about an Option rather than
// the question it belongs to.
//
// It is a separate error and not a shared one, because the two are told apart by
// the only thing this message does: naming the thing the author just tried to
// edit. A retired Option inside a live question is an ordinary state — the
// author sees it listed, greyed — and answering "this question has been retired"
// would send them looking for a question that is not retired at all.
func ErrTicketQuestionOptionRetired() apperror.DomainError {
	return apperror.New(
		"TICKET_QUESTION_OPTION_RETIRED",
		"This option has been retired and can no longer be edited.",
		nil,
	)
}

// ErrTicketQuestionKindTakesNoOptions is returned when Options are offered to a
// Ticket Question whose kind is not answered by choosing.
func ErrTicketQuestionKindTakesNoOptions() apperror.DomainError {
	return apperror.New(
		"TICKET_QUESTION_KIND_TAKES_NO_OPTIONS",
		"Only single choice and multiple choice questions have options.",
		nil,
	)
}

// ErrTicketQuestionOptionsRequired is returned when a choice Ticket Question
// would be left with no live Option — either created without one, or by retiring
// its last. A choice question nobody can answer is not a question.
func ErrTicketQuestionOptionsRequired() apperror.DomainError {
	return apperror.New(
		"TICKET_QUESTION_OPTIONS_REQUIRED",
		"A choice question needs at least one option.",
		nil,
	)
}

// ErrTooManyTicketQuestionOptions is returned when a Ticket Question would carry
// more live Options than the cap allows.
func ErrTooManyTicketQuestionOptions(max int) apperror.DomainError {
	return apperror.New(
		"TOO_MANY_TICKET_QUESTION_OPTIONS",
		"A question has too many options.",
		map[string]any{"max": max},
	)
}

// ErrTicketNotFound is returned when a Ticket does not exist on the Event, or
// belongs to another Organization's Event.
//
// One error for both, as every scoped lookup on this platform answers: telling
// "no such Ticket" apart from "not yours" would let one Organization confirm
// that another's id exists.
func ErrTicketNotFound() apperror.DomainError {
	return apperror.New("TICKET_NOT_FOUND", "Ticket not found.", nil)
}

// ErrTicketSaleReversed is returned when an Answer is written on a Ticket whose
// Ticket Sale has been reversed (#310).
//
// The Ticket and its Answers are still there and still readable — a Sale
// Reversal voids a sale, it does not unmint or erase anything. What it takes
// away is the point of writing: nobody is coming on a ticket that was refunded,
// so there is nothing left for an Organization to act on.
func ErrTicketSaleReversed() apperror.DomainError {
	return apperror.New(
		"TICKET_SALE_REVERSED",
		"This ticket's sale has been reversed, so its answers can no longer be changed.",
		nil,
	)
}

// ErrEventStartedAnswersClosed is returned when an Answer is written after the
// Event has started (#310).
//
// The window closes at the doors rather than at the Event's end, because the
// questions exist so an Organization can act on the replies — order the shirts,
// count the vegetarians — and the last moment that is any use is the moment the
// doors open. Read as an instant; the Event's timezone is already baked into its
// start. See catalog.AnswerWindow.
func ErrEventStartedAnswersClosed() apperror.DomainError {
	return apperror.New(
		"EVENT_STARTED_ANSWERS_CLOSED",
		"This event has started, so its answers can no longer be changed.",
		nil,
	)
}

// ErrInvalidAnswer is returned when a submitted Answer is not one its Ticket
// Question's kind can take.
//
// It carries the kind and a machine-readable problem token so a form can say
// WHICH question and WHAT about it, rather than leaving the reader to guess
// which of a Ticket's eight fields was refused. The token is catalog's own name
// for the refusal — see catalog.AnswerProblem — and the staff app maps it to a
// sentence in the reader's language, the same arrangement ADR 0023 puts under
// every other error code.
func ErrInvalidAnswer(kind, problem string, extra map[string]any) apperror.DomainError {
	details := map[string]any{"kind": kind, "problem": problem}
	for key, value := range extra {
		details[key] = value
	}
	return apperror.New("INVALID_ANSWER", "That answer does not fit this question.", details)
}

// ErrAnswerOptionNotOffered is returned when a choice Answer names an Option its
// Ticket Question does not offer.
//
// It covers two cases that look different and are the same refusal: an id
// belonging to another question entirely, and a RETIRED Option this Answer had
// not already chosen. A retired Option has left every new list, so choosing one
// afresh is choosing something that is not on offer — while KEEPING one that was
// already chosen is exactly what "kept on the Tickets that chose it" means, and
// that case is allowed through.
func ErrAnswerOptionNotOffered() apperror.DomainError {
	return apperror.New(
		"ANSWER_OPTION_NOT_OFFERED",
		"That option is not one this question offers.",
		nil,
	)
}

// ErrAnswerNotFound is returned when a Ticket has no Answer to this Ticket
// Question to remove.
func ErrAnswerNotFound() apperror.DomainError {
	return apperror.New("ANSWER_NOT_FOUND", "Answer not found.", nil)
}

// ErrAnswerLinkInvalid is returned when an Answer Link does not open (#312,
// ADR 0044).
//
// ONE ERROR COVERING EVERY REASON IT DID NOT, and the breadth is the point. A
// forged token, one truncated by a chat app, one naming a Ticket that no longer
// exists, and one whose Ticket Sale has been REVERSED all answer identically.
//
// The reversed case is the one worth defending. It looks like information the
// holder deserves — "this was refunded" — and it is a fact about somebody else's
// purchase, which is the one category of thing this page exists to disclose
// nothing about. A link forwarded into a group chat that reported a refund would
// be telling six people something about the buyer's money.
//
// The Event having started is told apart from this, and only that, because an
// Event's start is already published on the Storefront and saying so lets the
// page explain a deadline rather than imply a forgery.
func ErrAnswerLinkInvalid() apperror.DomainError {
	return apperror.New("ANSWER_LINK_INVALID", "This link is not valid.", nil)
}

// ErrAnswerLinkExpired is returned when an Answer Link is opened after its Event
// has started.
//
// A SEPARATE OUTCOME FROM INVALID, so the page can say "the event has started"
// rather than "this link is broken" — the first sends nobody looking for a
// replacement that would fail identically. It discloses nothing: the Event's
// start is published on the Storefront, and the page already names the Event.
//
// The window is catalog.AnswerWindow, the same one Event Staff and the checkout
// capture are held to, read against the Event as it stands now.
func ErrAnswerLinkExpired() apperror.DomainError {
	return apperror.New(
		"ANSWER_LINK_EXPIRED",
		"This event has started, so its questions can no longer be answered.",
		nil,
	)
}

// ErrAnswerLinkUnavailable is returned when no link secret is configured.
//
// A DEPLOYMENT FAULT AND NOT THE HOLDER'S, so it is a 500 rather than "your link
// is invalid" — telling somebody their link is broken when it is the server that
// is broken sends them back to the buyer for a replacement that would fail in
// exactly the same way. Its Confirmation Link neighbour answers the same.
func ErrAnswerLinkUnavailable() apperror.DomainError {
	return apperror.New("ANSWER_LINK_UNAVAILABLE", "Answer links are unavailable.", nil)
}

// ErrTicketAssignmentUnavailable is returned when any Ticket Assignment surface
// is asked for while TICKET_ASSIGNMENT_ENABLED is off (#324, parent #322).
//
// ITS OWN FLAG AND ITS OWN REFUSAL, separate from
// ErrTicketQuestionsUnavailable. The two features are genuinely separable —
// Ticket Questions are merged and work without assignment, and a guest list is
// worth having on a Ticket Type that asks nothing — and separate refusals are
// what make "killing assignment does not take questions dark" a testable
// property rather than a claim.
//
// It maps to 404 and not 403, for the reason its Ticket Question neighbour
// does: 403 would say "this exists and you may not have it", and while the flag
// is off there is nothing here. This endpoint answers exactly as an unrouted
// path does on a build without the feature.
func ErrTicketAssignmentUnavailable() apperror.DomainError {
	return apperror.New("TICKET_ASSIGNMENT_UNAVAILABLE", "Not found.", nil)
}

// ErrInvalidHolderEmail is returned when what the buyer typed is not an address
// (#324). See catalog.ParseHolderEmail for what "is not" means and why the
// check is as weak as it is.
//
// 400 AND NOT 409: the body itself is wrong and restating it correctly is
// exactly what fixes it, which is the same line INVALID_ANSWER sits on. It
// carries no detail about the address — there is nothing useful to say beyond
// "that is not an email address", and the buyer can see what they typed.
func ErrInvalidHolderEmail() apperror.DomainError {
	return apperror.New(
		"INVALID_HOLDER_EMAIL",
		"That is not a valid email address.",
		map[string]any{"max_length": MaxHolderEmailLength},
	)
}

// ErrAssignmentChannelUnsupported is returned when a Ticket of an `in_person`
// Ticket Sale is assigned (#324).
//
// A DOOR SALE HAS NO BUYER SURFACE. Assignment happens after purchase, from the
// Confirmation Link page or the Customer Area, and neither exists for a sale
// recorded at the door. The refusal states that rather than pretending the
// feature is off, because the buyer of an `online` sale standing beside them
// can do it.
func ErrAssignmentChannelUnsupported() apperror.DomainError {
	return apperror.New(
		"ASSIGNMENT_CHANNEL_UNSUPPORTED",
		"Tickets sold at the door cannot be assigned to an email address.",
		nil,
	)
}

// ErrAssignmentSaleReversed is returned when a Ticket of a reversed Ticket Sale
// is assigned (#324).
//
// A CODE OF ITS OWN RATHER THAN TICKET_SALE_REVERSED, even though the fact is
// the same one. The two refusals are told apart because the sentence a surface
// must show differs — one is about answers no longer changing, the other about
// there being nobody to hand a refunded ticket to — and a shared code would
// make the Storefront guess which it meant from the route it called.
func ErrAssignmentSaleReversed() apperror.DomainError {
	return apperror.New(
		"ASSIGNMENT_SALE_REVERSED",
		"This ticket's sale has been reversed, so it can no longer be assigned.",
		nil,
	)
}

// ErrAssignmentEventStarted is returned when a Ticket is assigned after its
// Event has started (#324).
//
// The window closes at the doors, exactly as the Answer window does. Told apart
// from its Answer twin for the reason above, and because this deadline is also
// when #322's retention purge takes an unaccepted address — so an assignment
// made after it would be naming somebody the platform is about to forget.
func ErrAssignmentEventStarted() apperror.DomainError {
	return apperror.New(
		"ASSIGNMENT_EVENT_STARTED",
		"This event has started, so its tickets can no longer be assigned.",
		nil,
	)
}

// ErrAssignmentLinkInvalid is returned when an Assignment Link does not open
// (#325, parent #322, ADR 0046).
//
// ONE ERROR COVERING EVERY REASON IT DID NOT, exactly as its Answer Link twin
// does, and for a reason that is if anything stronger here. A forged token, one
// truncated by a mail client, one naming a Ticket that no longer exists, one
// whose Ticket Sale has been REVERSED, and one whose Ticket has been REASSIGNED
// to somebody else all answer identically.
//
// The last two are the ones worth defending. "Your friend cancelled the
// purchase" and "your friend gave your ticket to someone else" are both facts
// about a third party's decisions, and this page must never name the buyer or
// describe what they did — CONTEXT.md is explicit that the disclosure rule holds
// in the error state too. What the Storefront says instead is that the link no
// longer opens, which is the true statement available to the reader.
//
// A SEPARATE CODE FROM ANSWER_LINK_INVALID even though the two read alike,
// because these are two different tokens with two different lifecycles and a
// shared code would let a page draw one link's copy for the other's failure.
func ErrAssignmentLinkInvalid() apperror.DomainError {
	return apperror.New("ASSIGNMENT_LINK_INVALID", "This link is not valid.", nil)
}

// ErrAssignmentLinkExpired is returned when an Assignment Link is opened after
// its Event has started (#325).
//
// TOLD APART FROM INVALID, and the only refusal that is, for the reason its
// Answer Link twin is: an Event's start is already published on the Storefront,
// so saying "the event has started" discloses nothing and lets the page explain
// a deadline rather than imply a forgery. It is also the moment #322's purge
// takes an unaccepted address, so a link that opened past it would be offering
// to mint a Customer from a fact the platform is in the act of forgetting.
func ErrAssignmentLinkExpired() apperror.DomainError {
	return apperror.New(
		"ASSIGNMENT_LINK_EXPIRED",
		"This event has started, so this ticket can no longer be accepted.",
		nil,
	)
}

// ErrAssignmentLinkUnavailable is returned when no link secret is configured.
// A deployment fault and not the Holder's, so it is a 500 beside
// ANSWER_LINK_UNAVAILABLE — and here there is no buyer to go back to for a
// replacement, because the Holder does not know who the buyer is.
func ErrAssignmentLinkUnavailable() apperror.DomainError {
	return apperror.New("ASSIGNMENT_LINK_UNAVAILABLE", "Assignment links are unavailable.", nil)
}

// ErrInvalidHolderName is returned when the name a Holder gave is not one
// (#325). See catalog.ParseHolderName.
//
// 400 beside INVALID_HOLDER_EMAIL: the body is wrong and restating it correctly
// is what fixes it. BOTH HALVES ARE REQUIRED — the name is stored separately
// (ADR 0005) and is written to the Customer as their current asserted name, so
// half a name would be half a person on the Organization's guest list.
func ErrInvalidHolderName() apperror.DomainError {
	return apperror.New(
		"INVALID_HOLDER_NAME",
		"Please give a first name and a last name.",
		map[string]any{"max_length": MaxHolderNameLength},
	)
}
