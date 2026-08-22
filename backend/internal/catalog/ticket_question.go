package catalog

import (
	"strings"
	"unicode/utf8"
)

// TicketQuestionKind is one of the seven shapes a Ticket Question can take.
//
// The list is closed and short on purpose. There is no file upload, and no
// purpose-built email or phone kind: `short_text` covers an address or a number
// somebody volunteers without the platform building itself a second contact
// channel for a person who never consented to one.
type TicketQuestionKind string

const (
	// TicketQuestionKindShortText is a one-line input.
	TicketQuestionKindShortText TicketQuestionKind = "short_text"
	// TicketQuestionKindLongText is a textarea.
	TicketQuestionKindLongText TicketQuestionKind = "long_text"
	// TicketQuestionKindSingleChoice offers Options and takes one of them.
	TicketQuestionKindSingleChoice TicketQuestionKind = "single_choice"
	// TicketQuestionKindMultiChoice offers Options and takes any number of them.
	TicketQuestionKindMultiChoice TicketQuestionKind = "multi_choice"
	// TicketQuestionKindNumber takes a number, and exports as one.
	TicketQuestionKindNumber TicketQuestionKind = "number"
	// TicketQuestionKindDate takes a date, and exports as one.
	TicketQuestionKindDate TicketQuestionKind = "date"
	// TicketQuestionKindCheckbox is a single tick box.
	TicketQuestionKindCheckbox TicketQuestionKind = "checkbox"
)

// ParseTicketQuestionKind converts a submitted value into a TicketQuestionKind,
// reporting whether it is one this system offers. Exact match only: the wire
// value and the stored value are the same string, and a kind is never guessed
// from a near miss.
func ParseTicketQuestionKind(raw string) (TicketQuestionKind, bool) {
	switch TicketQuestionKind(raw) {
	case TicketQuestionKindShortText,
		TicketQuestionKindLongText,
		TicketQuestionKindSingleChoice,
		TicketQuestionKindMultiChoice,
		TicketQuestionKindNumber,
		TicketQuestionKindDate,
		TicketQuestionKindCheckbox:
		return TicketQuestionKind(raw), true
	}
	return "", false
}

// OffersOptions reports whether this kind is answered by choosing from Options.
// Two of the seven are; the other five are answered by typing or ticking, and
// carry no Options at all.
func (k TicketQuestionKind) OffersOptions() bool {
	return k == TicketQuestionKindSingleChoice || k == TicketQuestionKindMultiChoice
}

// TicketQuestionTiming is when a Ticket Question is put to somebody.
//
// v1 writes at_checkout and always honours it. after_purchase is stored and
// parsed but never chosen yet: the field exists now so that the Organization's
// choice can be honoured later WITHOUT migrating Answers, which is the whole
// reason it is here before anything reads it.
type TicketQuestionTiming string

const (
	// TicketQuestionTimingAtCheckout asks on the skippable checkout form.
	TicketQuestionTimingAtCheckout TicketQuestionTiming = "at_checkout"
	// TicketQuestionTimingAfterPurchase asks only by Answer Link, afterwards.
	TicketQuestionTimingAfterPurchase TicketQuestionTiming = "after_purchase"
)

// ParseTicketQuestionTiming converts a submitted value into a
// TicketQuestionTiming, reporting whether it is one this system knows.
func ParseTicketQuestionTiming(raw string) (TicketQuestionTiming, bool) {
	switch TicketQuestionTiming(raw) {
	case TicketQuestionTimingAtCheckout, TicketQuestionTimingAfterPurchase:
		return TicketQuestionTiming(raw), true
	}
	return "", false
}

const (
	// MaxTicketQuestionOptions is the cap CONTEXT.md sets: at most twenty
	// Options per choice Ticket Question.
	//
	// It counts LIVE Options. A retired one has left every new list and is kept
	// only so the Tickets that chose it still read, so counting them here would
	// mean an Organization that corrected its sizes twice over three seasons
	// eventually could not add another — and the answer would be to un-retire
	// something, which is exactly what "retired, never deleted" forbids.
	MaxTicketQuestionOptions = 20

	// MaxTicketQuestionLabelLength caps a Ticket Question's label. Generous
	// enough for a whole sentence — "Do you have any dietary requirements we
	// should know about?" — and short of the point where a label is really a
	// description.
	MaxTicketQuestionLabelLength = 200

	// MaxTicketQuestionOptionLabelLength caps one Option's label. Shorter than
	// the question's: an Option is a value in a list — `S`, `M`, `Vegetarian` —
	// and a paragraph in a radio button is a question wearing the wrong kind.
	MaxTicketQuestionOptionLabelLength = 100
)

// NormalizeTicketQuestionLabel prepares a Ticket Question label for storage,
// reporting whether it is one this system will keep.
//
// It trims and does NOTHING ELSE, and that is the decision worth naming. A label
// is the Organization's own words, read AS COINED in every Locale exactly as a
// Custom Tag is (ADR 0027): casing, punctuation, accents and even a double space
// are what somebody typed, and the platform has no business tidying them because
// there is no second, per-Locale version for the tidying to be checked against.
// Contrast Tag names, which ARE folded — because a Tag is a shared pool key and
// two spellings of one Tag are a discovery bug, whereas two Ticket Questions
// that read alike are simply two questions.
//
// Surrounding whitespace goes because it is invisible: nobody means to coin a
// label with a leading space, and one that has it sorts and exports oddly for a
// reason its author cannot see.
func NormalizeTicketQuestionLabel(raw string) (string, bool) {
	return normalizeCoinedLabel(raw, MaxTicketQuestionLabelLength)
}

// NormalizeTicketQuestionOptionLabel is the same rule for one Option's label,
// against the shorter cap. An Option is read as coined for the same reason the
// question is, and renaming one never forks the Answers already given under it —
// that is what the Option's own identity is for.
func NormalizeTicketQuestionOptionLabel(raw string) (string, bool) {
	return normalizeCoinedLabel(raw, MaxTicketQuestionOptionLabelLength)
}

// normalizeCoinedLabel holds the one rule both labels follow, so the two can
// never drift into normalizing differently.
//
// The cap counts CHARACTERS, not bytes: an Organization writing Spanish must not
// be handed a shorter field than one writing English because its accents cost
// two bytes each.
func normalizeCoinedLabel(raw string, maxLength int) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	if utf8.RuneCountInString(trimmed) > maxLength {
		return "", false
	}
	return trimmed, true
}

// TicketQuestionKindFrozen reports whether a kind change must be refused.
//
// A Ticket Question's kind is frozen once ANY Answer exists (PRD, "Editing rules
// once any Answer exists"). Everything else about the question stays editable —
// the label, whether it is required, its timing, its place in the list — because
// those describe how the question reads, and the kind describes what the stored
// Answers ARE. A `single_choice` whose Answers are Option identities does not
// become a `date` by relabelling; changing it means retiring this question and
// adding another, which is what the authoring surface offers instead.
//
// Restating the same kind is not a change and is always allowed, so an editor
// that PATCHes the whole question back — which is what a form does — is never
// refused for sending the kind it was shown.
//
// answersExist is a fact this package cannot establish for itself; the service
// asks whatever can answer it. Today nothing can answer at all, so it is always
// false in production and this guard has never yet refused anything. It is
// written and tested now so that the ticket which lands Answers only has to wire
// the fact in, rather than discover the rule.
func TicketQuestionKindFrozen(current, requested TicketQuestionKind, answersExist bool) bool {
	return answersExist && current != requested
}

// TicketQuestionOptionCapReached reports whether a question that already has
// liveOptionCount live Options must refuse another.
func TicketQuestionOptionCapReached(liveOptionCount int) bool {
	return liveOptionCount >= MaxTicketQuestionOptions
}
