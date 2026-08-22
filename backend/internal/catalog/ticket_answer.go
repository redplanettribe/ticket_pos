package catalog

import (
	"strings"
	"time"
	"unicode/utf8"
)

// This file holds everything the Answer knows about itself: what a valid one
// looks like for each of the seven kinds, and when one may be given at all
// (#310, ADR 0043, ADR 0044).
//
// An ANSWER IS A PROPERTY OF THE TICKET and not of the buyer, not of the Ticket
// Sale and not of the Payment. A person buying four tickets is not assumed to
// know four people's sizes, and the Answer stands wherever it came from — the
// buyer at checkout, the holder through an Answer Link, or Event Staff typing it
// in afterwards. Nothing here records WHICH of those it was, deliberately: which
// of four friends ordered the wrong size is not a dispute this product
// adjudicates, so the platform keeps when an Answer last changed and not who
// changed it.

const (
	// MaxShortTextAnswerLength caps a short_text Answer. A one-line input's
	// worth — a size, a name, a company, an address somebody volunteers — and
	// the same 200 the question's own label gets, because a reply longer than
	// the question is a long_text question wearing the wrong kind.
	MaxShortTextAnswerLength = 200

	// MaxLongTextAnswerLength caps a long_text Answer. Ten times the short one:
	// "any dietary requirements we should know about?" gets a paragraph, and a
	// paragraph is where it stops. There is a cap at all because this column
	// travels into a Sales Export cell, and a spreadsheet cell holding an essay
	// helps nobody.
	MaxLongTextAnswerLength = 2000

	// MaxNumberAnswerIntegerDigits and MaxNumberAnswerFractionDigits bound what
	// a number Answer may be.
	//
	// They exist so that the NUMERIC column is never asked to hold something
	// absurd, and they are stated HERE rather than as NUMERIC(p, s) in the
	// schema on purpose: a precision in the column silently ROUNDS a seventh
	// decimal place away, and an Answer that comes back different from what
	// somebody typed is worse than one that was refused.
	MaxNumberAnswerIntegerDigits  = 15
	MaxNumberAnswerFractionDigits = 6

	// AnswerDateLayout is the one shape a date Answer is stored and read in. A
	// date is a CALENDAR DATE — a birthday, a travel day — and not an instant,
	// so it carries no time and no zone and cannot be shifted by one.
	AnswerDateLayout = "2006-01-02"
)

// SubmittedAnswer is one Answer as a caller states it, before its Ticket
// Question's kind has been read against it.
//
// EVERY FIELD IS OPTIONAL AND ABSENCE IS MEANINGFUL, which is why the scalars
// are pointers and OptionIDs is checked for nil rather than for length. `false`
// sent to a checkbox is an Answer — somebody read the question and said no — and
// a nil Checked is the absence of one; a bool alone could not tell those apart.
// Likewise an empty option_ids ARRAY is somebody clearing their choices, which
// is refused as an empty Answer, while an absent one is a caller who said
// nothing about choices at all.
//
// It is one struct with five slots rather than five request shapes because the
// kind — not the caller — decides which slot is the right one, and the kind is
// not known until the question has been loaded. Sending the wrong slot is
// therefore a refusal (AnswerWrongShape) rather than a routing error.
type SubmittedAnswer struct {
	// Text answers short_text and long_text.
	Text *string
	// Number answers number, as the digits were typed. A string and not a
	// float64: this value goes into a NUMERIC column, and round-tripping it
	// through binary floating point is how 0.1 becomes 0.100000001.
	Number *string
	// Date answers date, as a calendar date.
	Date *string
	// Checked answers checkbox.
	Checked *bool
	// OptionIDs answers single_choice (exactly one) and multi_choice (any
	// number). They are OPTION IDENTITIES and never labels — a label could not
	// survive a rename, which is the whole reason an Option has an id.
	OptionIDs []string
}

// AnswerValue is one validated Answer in exactly the shape its kind takes.
//
// Kind says which of the remaining fields is the Answer; the others are zero and
// mean nothing. It is a struct rather than an interface because the set of kinds
// is closed and every reader — the writer, the export, the staff app — branches
// on the kind anyway.
type AnswerValue struct {
	Kind TicketQuestionKind
	// Text is the trimmed reply to short_text or long_text, kept AS WRITTEN
	// otherwise: this is somebody's own words about themselves, and the platform
	// has no more business tidying them than it has tidying an Option's label.
	Text string
	// Number is the reply to number, in canonical decimal digits ready for the
	// NUMERIC column.
	Number string
	// Date is the reply to date, in AnswerDateLayout.
	Date string
	// Checked is the reply to checkbox, false included.
	Checked bool
	// OptionIDs are the Options chosen, in the order they were sent. Whether
	// those Options belong to this question — and what their labels read at this
	// moment, which is what gets snapshotted — is not knowable here; the service
	// resolves them against the question's own Options.
	OptionIDs []string
}

// AnswerProblem names WHY an Answer was refused.
//
// It is an enum rather than an error so that this package states the rule once
// and each layer above gives the refusal its own name: the HTTP layer turns it
// into a field error pointing at the offending input, and the service turns it
// into a domain error. A rule enforced in two places is a rule that will
// eventually be enforced differently in each — the same arrangement
// ClassifyCoinedLabel has for labels.
type AnswerProblem int

const (
	// AnswerOK: the Answer is one this system will keep.
	AnswerOK AnswerProblem = iota
	// AnswerMissing: nothing was supplied for this kind, or what was supplied
	// was empty. Not an error to be papered over — the way to say "this Ticket
	// has no Answer" is to remove the Answer, not to store a blank one that a
	// later reader cannot tell from a real reply.
	AnswerMissing
	// AnswerWrongShape: a slot was filled that this kind does not take, or more
	// than one was.
	AnswerWrongShape
	// AnswerTooLong: over this kind's cap.
	AnswerTooLong
	// AnswerNotANumber: a number question was sent something that is not one.
	AnswerNotANumber
	// AnswerNotADate: a date question was sent something that is not a calendar
	// date, an instant included.
	AnswerNotADate
	// AnswerOneOptionOnly: several Options offered to a single_choice question.
	AnswerOneOptionOnly
	// AnswerDuplicateOption: the same Option chosen twice in one Answer.
	AnswerDuplicateOption
	// AnswerTooManyOptions: more Options chosen than any question can offer.
	AnswerTooManyOptions
)

// ParseAnswer reads a submitted Answer against its Ticket Question's kind,
// returning the value to store or the reason it was refused.
//
// THIS IS THE ONLY PLACE THE PER-KIND RULES LIVE, and every route into an Answer
// goes through it: Event Staff here, and the checkout capture and the Answer Link
// that come later. Each of them is a different surface asking the same question
// — is this a thing this Ticket Question can be answered with — and three
// answers to it would be three ways for a `number` column to end up holding
// something that is not a number.
//
// It knows nothing about WHICH Options the question offers, only how many and of
// what shape. Resolving an id to an Option — and refusing one that belongs to
// somebody else's question — needs the stored Options, so it belongs to the
// service.
func ParseAnswer(kind TicketQuestionKind, submitted SubmittedAnswer) (AnswerValue, AnswerProblem) {
	if problem := submitted.fitsShapeOf(kind); problem != AnswerOK {
		return AnswerValue{}, problem
	}

	value := AnswerValue{Kind: kind}
	switch kind {
	case TicketQuestionKindShortText:
		text, problem := parseTextAnswer(*submitted.Text, MaxShortTextAnswerLength)
		if problem != AnswerOK {
			return AnswerValue{}, problem
		}
		value.Text = text
	case TicketQuestionKindLongText:
		text, problem := parseTextAnswer(*submitted.Text, MaxLongTextAnswerLength)
		if problem != AnswerOK {
			return AnswerValue{}, problem
		}
		value.Text = text
	case TicketQuestionKindNumber:
		number, problem := parseNumberAnswer(*submitted.Number)
		if problem != AnswerOK {
			return AnswerValue{}, problem
		}
		value.Number = number
	case TicketQuestionKindDate:
		date, problem := parseDateAnswer(*submitted.Date)
		if problem != AnswerOK {
			return AnswerValue{}, problem
		}
		value.Date = date
	case TicketQuestionKindCheckbox:
		value.Checked = *submitted.Checked
	case TicketQuestionKindSingleChoice, TicketQuestionKindMultiChoice:
		ids, problem := parseOptionAnswer(kind, submitted.OptionIDs)
		if problem != AnswerOK {
			return AnswerValue{}, problem
		}
		value.OptionIDs = ids
	default:
		// An eighth kind cannot reach here: the column is CHECKed and the wire
		// value is parsed. Refusing rather than storing nothing is the safe way
		// to be wrong if one ever does.
		return AnswerValue{}, AnswerWrongShape
	}
	return value, AnswerOK
}

// fitsShapeOf reports whether the submission filled exactly the one slot this
// kind takes.
//
// Both halves matter. A slot left empty is AnswerMissing — there is nothing to
// store. A slot filled that this kind does not take is AnswerWrongShape and is
// REFUSED rather than ignored, because a caller that sent `text` to a `number`
// question believed something about that question that is not true, and silence
// would let them keep believing it while the Ticket stayed unanswered.
func (s SubmittedAnswer) fitsShapeOf(kind TicketQuestionKind) AnswerProblem {
	var wanted *bool
	stated := 0
	for _, slot := range []struct {
		present bool
		takenBy func(TicketQuestionKind) bool
	}{
		{s.Text != nil, func(k TicketQuestionKind) bool {
			return k == TicketQuestionKindShortText || k == TicketQuestionKindLongText
		}},
		{s.Number != nil, func(k TicketQuestionKind) bool { return k == TicketQuestionKindNumber }},
		{s.Date != nil, func(k TicketQuestionKind) bool { return k == TicketQuestionKindDate }},
		{s.Checked != nil, func(k TicketQuestionKind) bool { return k == TicketQuestionKindCheckbox }},
		{s.OptionIDs != nil, func(k TicketQuestionKind) bool { return k.OffersOptions() }},
	} {
		if !slot.present {
			continue
		}
		stated++
		if slot.takenBy(kind) {
			present := true
			wanted = &present
		}
	}

	if stated > 1 {
		return AnswerWrongShape
	}
	if stated == 0 {
		return AnswerMissing
	}
	if wanted == nil {
		return AnswerWrongShape
	}
	return AnswerOK
}

// parseTextAnswer trims and checks a text Answer against its kind's cap.
//
// Trimming and nothing else, for the reason a coined label is trimmed and
// nothing else: this is somebody's own words about themselves. Surrounding
// whitespace goes because it is invisible and nobody meant it; the casing, the
// punctuation and the double space inside are what they wrote.
//
// The cap counts CHARACTERS and not bytes, so an answer written in Spanish is
// not handed a shorter field than one written in English because its accents
// cost two bytes each.
func parseTextAnswer(raw string, maxLength int) (string, AnswerProblem) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", AnswerMissing
	}
	if utf8.RuneCountInString(trimmed) > maxLength {
		return "", AnswerTooLong
	}
	return trimmed, AnswerOK
}

// parseNumberAnswer checks that a number Answer is a plain decimal this system
// will store as a number.
//
// PLAIN DECIMAL ONLY: an optional minus sign, digits, and at most one fractional
// part. No exponent, no thousands separator, no NaN and no Infinity — all of
// which either Postgres or a spreadsheet would read differently from the person
// who typed them, and the export's whole promise for this kind is that the cell
// is a number a pivot table can sum.
//
// The digits are returned AS TYPED rather than reduced to a canonical form.
// `3.50` stays `3.50`: a number question asking a price or a measurement means
// its trailing zero, and NUMERIC preserves it, so nothing here should throw it
// away on the way past.
func parseNumberAnswer(raw string) (string, AnswerProblem) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", AnswerMissing
	}

	digits := strings.TrimPrefix(trimmed, "-")
	integer, fraction, hasFraction := strings.Cut(digits, ".")
	if !isDigits(integer) {
		return "", AnswerNotANumber
	}
	if hasFraction && !isDigits(fraction) {
		return "", AnswerNotANumber
	}
	if len(integer) > MaxNumberAnswerIntegerDigits || len(fraction) > MaxNumberAnswerFractionDigits {
		return "", AnswerTooLong
	}
	return trimmed, AnswerOK
}

// isDigits reports whether s is one or more ASCII digits and nothing else. Not
// unicode.IsDigit: an Arabic-Indic or Devanagari digit is a digit to Unicode and
// not to Postgres, and a number that parses here and fails on the INSERT is a
// 500 where a refusal belonged.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// parseDateAnswer reads a date Answer as a calendar date and returns it in
// canonical form.
//
// STRICTLY A DATE. An instant — "2026-09-01T18:00:00Z" — is refused rather than
// truncated, because truncating one means choosing a timezone to truncate it in,
// and a birthday that moves by a day depending on where the server is standing
// is exactly the bug a DATE column exists to prevent.
//
// A date that never happened (`2026-02-30`) is refused by time.Parse itself,
// which range-checks the day against the month.
//
// ONE SPELLING AND NO NEAR MISSES: the layout is zero-padded, so `2026-9-1` is
// refused rather than fixed up. Every producer of a date on this platform is a
// date picker or an export and all of them emit the padded form, so accepting a
// second spelling would only widen what the column can be handed. The parsed
// date is re-formatted on the way out anyway, so the stored string is canonical
// by construction rather than by trusting the caller.
func parseDateAnswer(raw string) (string, AnswerProblem) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", AnswerMissing
	}
	parsed, err := time.Parse(AnswerDateLayout, trimmed)
	if err != nil {
		return "", AnswerNotADate
	}
	return parsed.Format(AnswerDateLayout), AnswerOK
}

// parseOptionAnswer checks the Options chosen against what the kind allows: one
// for single_choice, any number for multi_choice, each of them once.
//
// The cap is MaxTicketQuestionOptions because a question cannot offer more than
// that, so an Answer naming more has named something that is not on the list —
// caught here rather than after twenty-one round trips to resolve ids.
func parseOptionAnswer(kind TicketQuestionKind, ids []string) ([]string, AnswerProblem) {
	chosen := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		if seen[trimmed] {
			return nil, AnswerDuplicateOption
		}
		seen[trimmed] = true
		chosen = append(chosen, trimmed)
	}

	if len(chosen) == 0 {
		return nil, AnswerMissing
	}
	if kind == TicketQuestionKindSingleChoice && len(chosen) > 1 {
		return nil, AnswerOneOptionOnly
	}
	if len(chosen) > MaxTicketQuestionOptions {
		return nil, AnswerTooManyOptions
	}
	return chosen, AnswerOK
}

// AnswerRefusal reports whether a Ticket may be answered at all, and why not.
type AnswerRefusal int

const (
	// AnswerWindowOpen: this Ticket may be answered and re-answered.
	AnswerWindowOpen AnswerRefusal = iota
	// AnswerRefusedSaleReversed: the Ticket's Ticket Sale has been reversed.
	AnswerRefusedSaleReversed
	// AnswerRefusedEventStarted: the Event has started.
	AnswerRefusedEventStarted
)

// TicketSaleStatusActive is the ticket_sales.status of a Sale that stands.
//
// A Ticket carries no status of its own (ADR 0043): whether it stands is read
// from its Ticket Sale, because a Sale Reversal is always whole-Sale and a
// second copy of that fact could only ever disagree with the first.
const TicketSaleStatusActive = "active"

// AnswerWindow is the single place the edit window is decided, for every route
// into an Answer — Event Staff, the checkout capture, and the Answer Link.
//
// AN ANSWER IS CHANGEABLE UNTIL THE EVENT STARTS. Not until it ends and not for
// some period after: the questions exist so an Organization can act on the
// replies — order the shirts, count the vegetarians — and the last moment that
// is any use is the moment the doors open. It is a half-open interval, so
// somebody arriving exactly as the doors open is late, exactly as the Reversal
// Window's closing instant works.
//
// "IN THE EVENT'S TIMEZONE" IS ALREADY SETTLED BY THE TIME IT GETS HERE, and
// that is worth saying because the phrase invites a conversion that would be a
// bug. `events.starts_at` is a TIMESTAMPTZ — an instant, fixed when the
// Organization set the Event's start in its own zone — so the Event's timezone
// is baked into it and comparing instants gives the answer the Organization
// meant. Converting either side into the Event's zone first would change
// nothing on a correct implementation and would silently move somebody's
// deadline on an incorrect one. Same reasoning as NewReversalWindow, which takes
// two instants and compares them as instants.
//
// A REVERSED SALE IS REPORTED AHEAD OF THE CLOCK when both are true, because it
// is the more fundamental fact: the Ticket does not stand at all, and telling
// somebody "the Event has started" would send them looking for a deadline they
// never had.
//
// eventStartsAt is nil for an Event that has not said when it starts. That Event
// has not started — a nil read as "started" would freeze every Answer under it,
// which is the wrong way to be wrong.
func AnswerWindow(saleStatus string, eventStartsAt *time.Time, now time.Time) AnswerRefusal {
	if saleStatus != TicketSaleStatusActive {
		return AnswerRefusedSaleReversed
	}
	if eventStartsAt != nil && !now.Before(*eventStartsAt) {
		return AnswerRefusedEventStarted
	}
	return AnswerWindowOpen
}
