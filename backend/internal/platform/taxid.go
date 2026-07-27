package platform

import (
	"errors"
	"strings"
)

// The Tax ID validation gradient (ADR 0016, CONTEXT.md "Tax ID", "Tax ID Type").
//
// A Tax ID is a Tax ID Type plus its number, supplied by a Customer so an
// Organization can declare the sale to Ecuador's tax authority. A number that
// fails the check digit is worse than no number at all: it is declared, it is
// wrong, and the Organization carries the liability. So this file is the single
// source of truth for what counts as a valid Tax ID — the checkout endpoint, the
// Sale Import row validator and the future in-person POS all call ValidateTaxID
// rather than re-deriving the rules, and the clients mirror it only to give
// instant feedback while typing.
//
// It is deliberately a pure function over strings: no database, no context, no
// domain error types. The service layer decides *when* a Tax ID is required
// (native Sales Channels yes, `import` no — ADR 0016); this file only decides
// whether a supplied one is well formed.

// Tax ID Type values. These are the wire values as well as the stored ones; the
// glossary term is Tax ID Type and these three are the whole of it.
const (
	TaxIDTypeCedula   = "cedula"
	TaxIDTypeRUC      = "ruc"
	TaxIDTypePassport = "passport"
)

// The two ways a Tax ID can be rejected, kept separate because the API surfaces
// field-level errors: an unknown type is a fault of the type field, an invalid
// number a fault of the number field. Callers match with errors.Is and map to
// their own domain error codes.
var (
	// ErrTaxIDTypeUnknown means the type is not one of the three Tax ID Types.
	ErrTaxIDTypeUnknown = errors.New("unknown tax id type")
	// ErrTaxIDNumberInvalid means the number does not satisfy the rules for its
	// (valid) type: wrong length, non-digit characters, an impossible province
	// prefix, or a failed check digit.
	ErrTaxIDNumberInvalid = errors.New("invalid tax id number")
)

// SaleTaxID is the Tax ID one Ticket Sale was transacted under, as it travels
// from the checkout form to the Customer upsert. Both halves are empty when the
// sale carries no Tax ID at all, which only the `import` channel may do
// (ADR 0016).
//
// It is defined here, beside the validator, because it crosses a module
// boundary: sales hands it to customers through the upsert seam, and neither
// module may import the other's packages for a data type.
type SaleTaxID struct {
	// Type is a Tax ID Type; Number is already normalised by ValidateTaxID.
	Type   string
	Number string
	// SelfAsserted reports that the person supplying this Tax ID had proven they
	// own the email it is recorded against — the checkout ran under that
	// Customer's own full Customer Session.
	//
	// It is the single fact that lets a sale overwrite a *Verified* Customer's
	// stored Tax ID (ADR 0016). Someone editing their own prefilled value is
	// correcting themselves and their override becomes the new stored
	// assertion; an anonymous visitor typing a known email is not, and the
	// stored value stands however the sale is recorded. It says nothing about
	// the name, whose rule is unchanged and unaffected.
	SelfAsserted bool
}

// Set reports whether a Tax ID was supplied at all.
func (t SaleTaxID) Set() bool { return t.Type != "" && t.Number != "" }

// Display renders a Tax ID the way the buyer's own paper trail shows it —
// "Cédula: 1712345675" — or the empty string when the sale carries none
// (#99).
//
// Unmasked, deliberately: this is the line a buyer copies into their expense
// records, and a receipt that hid half the number would be useless for the one
// job it exists to do. It is safe here because the surfaces that render it —
// the Sale Confirmation email and the Customer Area — are the buyer's own.
//
// The empty string is the whole of the "no Tax ID" rendering. Callers append a
// line only when there is one, so a legacy or imported sale gets no line at all
// rather than a label with nothing after it.
func (t SaleTaxID) Display() string {
	if !t.Set() {
		return ""
	}
	return TaxIDTypeLabel(t.Type) + ": " + t.Number
}

// TaxIDTypeLabel is the human name of a Tax ID Type, as printed on the document
// itself: Spanish, because an Ecuadorian buyer looks for the word "Cédula" on
// the card in their hand rather than a translation of it. The Storefront mirrors
// these in apps/storefront/lib/tax-id.ts.
//
// An unrecognised type falls back to the raw value. Nothing can currently store
// one — the columns carry a CHECK constraint and ValidateTaxID is the only way
// in — so this is a rendering that will never be reached; showing the stored
// value beats showing an empty label if it ever is.
func TaxIDTypeLabel(taxIDType string) string {
	switch taxIDType {
	case TaxIDTypeCedula:
		return "Cédula"
	case TaxIDTypeRUC:
		return "RUC"
	case TaxIDTypePassport:
		return "Pasaporte"
	default:
		return taxIDType
	}
}

// ValidateTaxID checks a Tax ID Type and number and returns the number in the
// form it must be stored in: trimmed always, and uppercased for passports so
// "ab123456" and "AB123456" are the same passport rather than two.
//
// The gradient is uneven on purpose. Cédula and RUC are algorithmically
// verifiable Ecuadorian identifiers, so they are checked to the digit — a typo
// is caught before it reaches a declaration. A passport is issued by any country
// on earth under any scheme, so there is nothing to verify against; it is
// accepted on shape alone (non-empty alphanumeric, 6–20 characters) rather than
// pretending to a rigour that would lock out legitimate foreign buyers.
//
// The type is matched exactly: it arrives from a closed enum on the wire, and
// silently accepting "Cedula" here would put a second normalisation rule in the
// system for a value that has exactly one spelling.
func ValidateTaxID(taxIDType, number string) (string, error) {
	trimmed := strings.TrimSpace(number)

	switch taxIDType {
	case TaxIDTypeCedula:
		if !isValidCedula(trimmed) {
			return "", ErrTaxIDNumberInvalid
		}
		return trimmed, nil
	case TaxIDTypeRUC:
		if !isValidRUC(trimmed) {
			return "", ErrTaxIDNumberInvalid
		}
		return trimmed, nil
	case TaxIDTypePassport:
		if !isValidPassport(trimmed) {
			return "", ErrTaxIDNumberInvalid
		}
		return strings.ToUpper(trimmed), nil
	default:
		return "", ErrTaxIDTypeUnknown
	}
}

// TaxIDFieldErrors validates a supplied Tax ID and, on failure, returns the
// field-level errors naming the half of the pair actually at fault: an unknown
// type is the type field's problem, a failed check digit the number's. On
// success it returns the normalised number and no errors.
//
// The two field names are parameters because the same pair travels under
// different names on different surfaces — `customer_tax_id_type` at checkout,
// `tax_id_type` on the profile — while the verdict and the wording a person
// reads must be identical everywhere. Every endpoint that takes a Tax ID maps
// it through here rather than writing its own switch, so no surface can drift
// into telling a buyer their cédula is fine when another says it is not.
//
// Both halves must already be non-empty: whether an absent Tax ID is a
// validation failure (native Sales Channels) or an ordinary clear (the profile
// editor) is the caller's rule, not this function's.
func TaxIDFieldErrors(typeField, numberField, taxIDType, number string) (string, []FieldError) {
	normalized, err := ValidateTaxID(taxIDType, number)
	switch {
	case errors.Is(err, ErrTaxIDTypeUnknown):
		return "", []FieldError{{Field: typeField, Message: "must be cedula, ruc, or passport"}}
	case errors.Is(err, ErrTaxIDNumberInvalid):
		return "", []FieldError{{Field: numberField, Message: taxIDNumberMessage(taxIDType)}}
	case err != nil:
		return "", []FieldError{{Field: numberField, Message: "is not valid"}}
	default:
		return normalized, nil
	}
}

// taxIDNumberMessage explains a rejected number in the terms of its own Tax ID
// Type, because "is not valid" tells a buyer staring at their ID card nothing
// about which digit to look at.
func taxIDNumberMessage(taxIDType string) string {
	switch taxIDType {
	case TaxIDTypeCedula:
		return "must be a valid 10-digit cédula"
	case TaxIDTypeRUC:
		return "must be a valid 13-digit RUC"
	case TaxIDTypePassport:
		return "must be 6–20 letters or digits"
	default:
		return "is not valid"
	}
}

// isValidCedula applies the full cédula rule: ten digits, a real province
// prefix, and the modulo-10 check digit.
func isValidCedula(number string) bool {
	if len(number) != 10 || !isASCIIDigits(number) {
		return false
	}
	if !hasValidProvincePrefix(number) {
		return false
	}
	return number[9] == cedulaCheckDigit(number)
}

// isValidRUC applies the RUC rule: thirteen digits, a real province prefix, and
// a third digit that names one of the three RUC forms.
//
// Only the natural-person form (third digit 0–5) can be verified further: it is
// a cédula with a three-digit establishment suffix appended, so the first ten
// digits must pass the cédula check. The company form (9) and the public-entity
// form (6) each use a different modulus and a check digit in a different
// position; those algorithms are not published as stably as the cédula one and
// getting them subtly wrong would reject real taxpayers, so both are accepted on
// structure alone. Third digits 7 and 8 name no RUC form and are rejected.
func isValidRUC(number string) bool {
	if len(number) != 13 || !isASCIIDigits(number) {
		return false
	}
	if !hasValidProvincePrefix(number) {
		return false
	}
	switch third := number[2]; {
	case third >= '0' && third <= '5':
		// Natural-person RUC: cédula + establishment suffix.
		return number[9] == cedulaCheckDigit(number)
	case third == '6' || third == '9':
		// Public entity / company: structure only.
		return true
	default:
		return false
	}
}

// isValidPassport accepts any non-empty ASCII-alphanumeric string of 6–20
// characters. The bounds exist only to catch a slip of the hand or a pasted
// paragraph, not to model any country's passport format.
func isValidPassport(number string) bool {
	if len(number) < 6 || len(number) > 20 {
		return false
	}
	for i := 0; i < len(number); i++ {
		c := number[i]
		isDigit := c >= '0' && c <= '9'
		isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !isDigit && !isLetter {
			return false
		}
	}
	return true
}

// hasValidProvincePrefix checks the leading two digits against Ecuador's
// province codes: 01–24 for the provinces themselves, plus 30 for citizens
// registered abroad. Anything else (00, 25–29, 31+) was never issued.
func hasValidProvincePrefix(number string) bool {
	province := int(number[0]-'0')*10 + int(number[1]-'0')
	return (province >= 1 && province <= 24) || province == 30
}

// cedulaCheckDigit computes the modulo-10 check digit over the first nine digits
// of a cédula: coefficients 2,1,2,1,2,1,2,1,2, any product above 9 reduced by 9,
// and the check digit is whatever completes the sum to the next multiple of ten.
// It returns the digit as its ASCII byte so callers compare against the string
// directly. The caller has already established that number holds at least ten
// ASCII digits.
func cedulaCheckDigit(number string) byte {
	sum := 0
	for i := 0; i < 9; i++ {
		digit := int(number[i] - '0')
		if i%2 == 0 {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}
	return byte('0' + (10-sum%10)%10)
}

// isASCIIDigits reports whether every byte is 0–9. Deliberately ASCII-only:
// Go's unicode.IsDigit would accept Arabic-Indic or fullwidth digits, which are
// not what a cédula is made of and would break the arithmetic below.
func isASCIIDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}
