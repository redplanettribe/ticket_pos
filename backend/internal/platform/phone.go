package platform

import (
	"errors"
	"strings"
)

// The phone number rule (#103, #105, #200).
//
// A phone number exists in this system for two reasons, and the first is what
// shaped everything below:
//
//   - The Customer's phone number is handed to PayPhone's Prepare call so the
//     hosted card form arrives with the cardholder's number already filled,
//     leaving the buyer nothing to type but their card.
//   - The Organization's Support WhatsApp number is published on its public Event
//     pages, where a Customer taps it to open a WhatsApp conversation (ADR 0029).
//
// The second arrived later and reuses this rule unchanged rather than growing a
// second one, because the number of definitions of "a valid phone number" a
// system can hold without them drifting is one.
//
// The strict Ecuadorian tier earns its keep under the second reason even though
// its stated justification — PayPhone wants a cardholder's MOBILE — does not
// transfer. A support line has its own reason to be a mobile: an organizer who
// types their landline by mistake publishes a number that never answers a
// message, and a Customer discovering that mid-purchase is a worse failure than
// a rejection at the settings form. The knowing cost is that WhatsApp Business
// can be verified on a landline by voice call, so an Ecuadorian Organization
// running support from an 02… number cannot enter it here. ADR 0029 records that
// exclusion as chosen rather than overlooked.
//
// This file is the source of truth for what counts as a valid phone number. The
// Storefront mirrors it in apps/storefront/lib/phone.ts so a buyer who
// fat-fingers a digit hears about it without a round trip, but the server's
// verdict is the one that decides whether a Sale is recorded — and where the two
// could ever disagree, the mirror must be the more permissive side. A number the
// mirror rejects but this file would have accepted is a buyer locked out of a
// purchase the platform would have taken. This is the same split the Tax ID
// already runs (taxid.go / lib/tax-id.ts), for the same reasons.
//
// The Staff app deliberately does NOT mirror this rule, and should not start.
// The Storefront's copy earns its keep because a buyer mid-purchase must not
// spend a round trip on a typo; an Org Admin editing Settings once is nowhere
// near that bar, and a second mirror would double the drift surface the
// paragraph above warns about. Where the Support WhatsApp number is concerned,
// this file's verdict is the only verdict.
//
// Like ValidateTaxID this is a pure function over strings: no database, no
// context, no domain error types. Whether a phone number is required at all is
// the caller's rule — the checkout field is deliberately optional (#103), so an
// absent number is not this file's business; an ill-formed one is.
//
// No phone-number library is used, and none should be introduced. Per-country
// national formats are a moving target maintained by people who do nothing else,
// and a rule stricter than PayPhone's own would lock out a legitimate buyer for
// the sake of a field that is only ever a convenience. See the two tiers on
// ValidatePhone.

// EcuadorDiallingCode is the one dialling code with a rule of its own. It is
// exported because the Storefront's country table defaults to Ecuador and the
// message helper below keys off it; nothing else about a country's dialling code
// carries validation duty.
const EcuadorDiallingCode = "+593"

// ecuadorDigits is EcuadorDiallingCode without the leading plus, which is the
// form the scanner below produces.
const ecuadorDigits = "593"

// ErrPhoneInvalid means the supplied phone number is not a well-formed
// international number under the rules on ValidatePhone. There is only one way
// to fail, unlike the Tax ID's type/number split, because a phone number is one
// field on the wire and one column in the database.
var ErrPhoneInvalid = errors.New("invalid phone number")

// The two field-level messages, one per tier. They are constants rather than
// strings retyped at each surface for the same reason TaxIDTypeMessage is: the
// wording a person reads must be identical wherever a number is rejected, and it
// has to move when the rule above it moves.
const (
	// PhoneEcuadorMessage explains the strict tier. It names the shape of the
	// number on the buyer's own phone — nine digits beginning with 9 — rather
	// than the canonical form, because that is what they are looking at.
	PhoneEcuadorMessage = "must be an Ecuadorian mobile: 9 digits starting with 9"
	// PhoneGenericMessage explains the permissive tier, and gives the example
	// because "international format" means nothing to most people until they see
	// one.
	PhoneGenericMessage = "must be 4–15 digits in international format, like +12025550123"
)

// ValidatePhone checks a phone number and returns it in the single canonical
// form it is stored and sent in: E.164, a leading plus and nothing but digits
// after it, e.g. "+593987654321". That is exactly the form PayPhone's
// phoneNumber parameter wants, which is why the value is never split into a
// dialling code and a national part anywhere below the Storefront — see #103.
//
// Validation is two-tier, and the unevenness is the point:
//
//   - Ecuador (+593) is strict: the national part must be a MOBILE — nine digits
//     beginning with 9. An Ecuadorian landline (an 02… number, say) is rejected,
//     because PayPhone's form wants a cardholder's mobile and a landline there
//     is a field the buyer will have to correct on the very screen this feature
//     exists to clear. Ecuador is the one country whose numbering plan this
//     platform can afford to know: it is the home market, the default selection,
//     and the overwhelming majority of buyers.
//   - Every other country gets generic E.164 and nothing more: 4 to 15 digits in
//     TOTAL, dialling code included. Deliberately permissive. Fifteen is E.164's
//     own ceiling and four is barely more than a plausible dialling code, so this
//     catches a slip of the hand or a pasted paragraph and nothing else. Guessing
//     at 200 national numbering plans without a library would reject real buyers,
//     and PayPhone itself documents no restriction beyond "(+) + country code +
//     number".
//
// Normalisation accepts what a human types. Spaces, dashes, parentheses, dots
// and slashes are how people write phone numbers down and are simply dropped;
// so is a single leading zero on an Ecuadorian national part, because an
// Ecuadorian reading their own number off a screen reads "0987654321" — the
// trunk prefix they dial domestically, which E.164 has no room for. What is not
// accepted is a missing plus: without a dialling code there is no canonical form
// to normalise to, only a guess about which country the buyer is in.
func ValidatePhone(phone string) (string, error) {
	digits, ok := scanPhone(phone)
	if !ok {
		return "", ErrPhoneInvalid
	}

	if national, isEcuador := strings.CutPrefix(digits, ecuadorDigits); isEcuador {
		// The domestic trunk prefix, dropped rather than rejected: "0987654321"
		// is how the number is printed on the buyer's own bank statement.
		national = strings.TrimPrefix(national, "0")
		if len(national) != 9 || national[0] != '9' {
			return "", ErrPhoneInvalid
		}
		return EcuadorDiallingCode + national, nil
	}

	if len(digits) < 4 || len(digits) > 15 {
		return "", ErrPhoneInvalid
	}
	return "+" + digits, nil
}

// PhoneNumberMessage explains a rejected number in the terms of the tier it was
// judged under, because "is not valid" tells an Ecuadorian staring at a landline
// nothing about why. It is the message half of ValidatePhone's verdict and moves
// with the rules above.
//
// The tier is chosen from what the buyer typed rather than from what parsed,
// so a number mangled badly enough to fail the scanner still gets the Ecuadorian
// message when it starts with 593 — that is the tier they were aiming at.
func PhoneNumberMessage(phone string) string {
	if strings.HasPrefix(digitsOnly(phone), ecuadorDigits) {
		return PhoneEcuadorMessage
	}
	return PhoneGenericMessage
}

// PhoneNumberCode is the stable code half of PhoneNumberMessage's verdict, and
// picks its tier the same way and from the same input, so a client rendering its
// own wording splits Ecuadorian from generic exactly where the English does.
func PhoneNumberCode(phone string) string {
	if strings.HasPrefix(digitsOnly(phone), ecuadorDigits) {
		return CodeInvalidPhoneEC
	}
	return CodeInvalidPhone
}

// PhoneFieldErrors validates a supplied phone number and, on failure, returns
// the field-level error under the name the field travels under on this surface —
// `customer_phone` at checkout, `phone` on the profile — while the verdict and
// the wording a person reads stay identical everywhere. Every endpoint that
// takes a phone number maps it through here rather than writing its own check,
// exactly as TaxIDFieldErrors does for the Tax ID.
//
// The number must already be non-empty: the checkout field is optional (#103),
// and whether an absent phone is a validation failure or an ordinary clear is
// the caller's rule, not this function's.
func PhoneFieldErrors(field, phone string) (string, []FieldError) {
	normalized, err := ValidatePhone(phone)
	if err != nil {
		return "", []FieldError{{Field: field, Code: PhoneNumberCode(phone), Message: PhoneNumberMessage(phone)}}
	}
	return normalized, nil
}

// scanPhone strips the punctuation people write phone numbers with and returns
// the bare digits, without the leading plus. It reports false for anything that
// is not a plausible international number: a missing plus, a plus that is not
// first, any character that is neither a digit nor recognised punctuation, or no
// digits at all.
//
// Deliberately ASCII-only, for the same reason isASCIIDigits is: Go's
// unicode.IsDigit would accept Arabic-Indic or fullwidth digits, and a typographic
// en-dash is not a separator this needs to understand. Anything exotic enough to
// arrive that way is better rejected with a message than silently reinterpreted
// into a different phone number.
func scanPhone(phone string) (string, bool) {
	var digits strings.Builder
	plus := false
	for i := 0; i < len(phone); i++ {
		c := phone[i]
		switch {
		case c >= '0' && c <= '9':
			digits.WriteByte(c)
		case c == '+':
			// E.164 has exactly one plus and it is the first thing in the
			// string; a second one, or one after a digit, is a typo rather than
			// a formatting habit.
			if plus || digits.Len() > 0 {
				return "", false
			}
			plus = true
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '-' || c == '(' || c == ')' || c == '.' || c == '/':
			// How humans write phone numbers: "+593 (0)98-765.4321".
		default:
			return "", false
		}
	}
	if !plus || digits.Len() == 0 {
		return "", false
	}
	return digits.String(), true
}

// digitsOnly collects the ASCII digits from a string and discards everything
// else. It exists only so PhoneNumberMessage can tell which tier a rejected
// number was aiming at, which is a question about intent rather than validity —
// hence no verdict, unlike scanPhone.
func digitsOnly(s string) string {
	var digits strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			digits.WriteByte(s[i])
		}
	}
	return digits.String()
}
