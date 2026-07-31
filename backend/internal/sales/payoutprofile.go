package sales

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Payout Profile: where an Organization is paid (ADR 0026, CONTEXT.md
// "Payout Profile").
//
// This file is the whole of what makes a set of bank details acceptable. It sits
// in package `sales` rather than in the handler because the same rules bind the
// Payout Request that snapshots a profile: an organizer correcting an account
// number on a request is correcting their profile, and one validator means the
// two can never disagree about what a valid account number is.
//
// It is a pure function over strings, in the shape of platform/taxid.go, and it
// borrows that file's verdict for the Tax ID rather than restating it. Nothing
// here reaches a database, and nothing here writes to a log: bank details are
// the one payload in this system that must not appear in either a log line or an
// error message, which is why every message below describes the rule and never
// the value that broke it.

// The two account types an Ecuadorian bank offers, in Spanish because that is
// what the receiving bank's form says. These are the wire values and the stored
// ones; the CHECK constraint in migration 042 is the same pair.
const (
	AccountTypeSavings = "ahorros"
	AccountTypeCurrent = "corriente"
)

// Length bounds, mirrored by the CHECK constraints in migration 042. The name
// bounds are generous enough for any real bank or account holder and exist to
// catch a pasted paragraph. The account-number bound is 34 — the longest an IBAN
// can be — because rejecting a real account number is far worse than storing a
// few characters more than Ecuador currently issues.
const (
	MaxBankNameLength          = 120
	MaxAccountHolderNameLength = 120
	MaxAccountNumberLength     = 34
)

// PayoutProfile is where an Organization is paid: the bank, the account and its
// type, the name on it, and the Organization's own Tax ID for the factura.
//
// It carries no organization id and no timestamps. This is the profile as a
// person states it, which is the only form the validation below has an opinion
// about; whose it is and when it was last touched are the repository's business.
type PayoutProfile struct {
	BankName          string
	AccountType       string
	AccountNumber     string
	AccountHolderName string
	TaxIDType         string
	TaxIDNumber       string
}

// Normalize returns the profile in the form it must be stored in, or the
// field-level errors that stop it being stored at all.
//
// Every field is checked, not just the first bad one: an organizer who typed
// three things wrong should be told three times rather than made to submit three
// times. The returned profile is only meaningful when no errors come back with
// it.
//
// A complete profile is required — there is no partial save. A profile missing
// its account number cannot be paid to, so storing one would only mean
// discovering the gap at the moment money was about to move.
func (p PayoutProfile) Normalize() (PayoutProfile, []platform.FieldError) {
	var errs []platform.FieldError

	normalized := PayoutProfile{
		BankName:          strings.TrimSpace(p.BankName),
		AccountType:       strings.TrimSpace(p.AccountType),
		AccountNumber:     NormalizeAccountNumber(p.AccountNumber),
		AccountHolderName: strings.TrimSpace(p.AccountHolderName),
		TaxIDType:         p.TaxIDType,
	}

	errs = appendTextErrors(errs, "bank_name", normalized.BankName, MaxBankNameLength)
	errs = appendTextErrors(errs, "account_holder_name", normalized.AccountHolderName, MaxAccountHolderNameLength)

	switch normalized.AccountType {
	case AccountTypeSavings, AccountTypeCurrent:
	case "":
		errs = append(errs, platform.FieldError{Field: "account_type", Code: platform.CodeRequired, Message: "is required"})
	default:
		errs = append(errs, platform.FieldError{
			Field:   "account_type",
			Code:    platform.CodeInvalidAccountType,
			Message: "must be " + AccountTypeSavings + " or " + AccountTypeCurrent,
		})
	}

	// The account number is judged on what normalisation left, so an organizer
	// who typed only separators is told the field is empty rather than that it
	// is not digits — which is the truer of the two things to say.
	switch {
	case normalized.AccountNumber == "":
		errs = append(errs, platform.FieldError{Field: "account_number", Code: platform.CodeRequired, Message: "is required"})
	case !isDigits(normalized.AccountNumber):
		errs = append(errs, platform.FieldError{
			Field:   "account_number",
			Code:    platform.CodeInvalidAccountNumber,
			Message: "must contain digits only",
		})
	case len(normalized.AccountNumber) > MaxAccountNumberLength:
		errs = append(errs, platform.FieldError{
			Field:   "account_number",
			Code:    platform.CodeTooLong,
			Message: "must be at most " + strconv.Itoa(MaxAccountNumberLength) + " digits",
		})
	}

	taxIDNumber, taxIDErrs := platform.BeneficiaryTaxIDFieldErrors("tax_id_type", "tax_id_number", p.TaxIDType, p.TaxIDNumber)
	errs = append(errs, taxIDErrs...)
	normalized.TaxIDNumber = taxIDNumber

	if len(errs) > 0 {
		return PayoutProfile{}, errs
	}
	return normalized, nil
}

// NormalizeAccountNumber strips the separators an organizer brings with them
// when they copy an account number off a bank statement or a banking app —
// hyphens and whitespace of any kind — and leaves everything else alone.
//
// It deliberately does not drop non-digits generally. A letter in an account
// number is a typo somebody has to be told about, and silently deleting it would
// turn a wrong number into a plausible one, which is the worst outcome this
// field has.
//
// Leading zeros survive untouched, which is the entire reason the column is TEXT
// (ADR 0026). The Staff app mirrors this in apps/staff/lib/payout-profile.ts so
// the field an organizer sees while typing matches what is stored.
func NormalizeAccountNumber(number string) string {
	var b strings.Builder
	b.Grow(len(number))
	for _, r := range number {
		if r == '-' || unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// MaskAccountNumber renders an account number the way it is safe to show beside
// other people's: four dots and the last four digits, "····4821".
//
// It exists in Go — rather than only in the staff app, which has its own
// maskAccountNumber in apps/staff/lib/payout-profile.ts — because the operator's
// queue must not merely DISPLAY a masked number, it must not RECEIVE a whole
// one. That queue is the one screen showing every Organization's details at once
// and the one operators screenshot into support threads (ADR 0026), and a
// payload carrying fifty account numbers to a browser that renders none of them
// is a leak waiting for the first person to open the network tab. Masking at the
// edge of the API is the only version of this rule a test can hold.
//
// The last four are what lets a person recognise an account without the number
// being readable over a shoulder. A number of four digits or fewer is masked
// entirely, because revealing "the last four" of it would reveal all of it.
//
// The separators are stripped first, so a stored number and a hand-typed one
// mask identically; in practice everything stored has already been through
// NormalizeAccountNumber.
func MaskAccountNumber(accountNumber string) string {
	normalized := NormalizeAccountNumber(accountNumber)
	if utf8.RuneCountInString(normalized) <= 4 {
		return accountNumberMask
	}
	runes := []rune(normalized)
	return accountNumberMask + string(runes[len(runes)-4:])
}

// accountNumberMask is the four middle dots, U+00B7, matching the staff app's
// mask character exactly. A different dot in each place would read as two
// different products.
const accountNumberMask = "····"

// appendTextErrors applies the two rules every free-text field on the profile
// shares — present, and within its bound — naming the field and never the value.
// The bound is counted in runes, matching Postgres's char_length so the API and
// the CHECK constraint refuse the same strings.
func appendTextErrors(errs []platform.FieldError, field, value string, max int) []platform.FieldError {
	switch {
	case value == "":
		return append(errs, platform.FieldError{Field: field, Code: platform.CodeRequired, Message: "is required"})
	case utf8.RuneCountInString(value) > max:
		return append(errs, platform.FieldError{
			Field:   field,
			Code:    platform.CodeTooLong,
			Message: "must be at most " + strconv.Itoa(max) + " characters",
		})
	}
	return errs
}

// isDigits reports whether every byte is 0–9. ASCII-only for the same reason
// platform/taxid.go is: an account number typed with Arabic-Indic digits is not
// one any Ecuadorian bank will accept, and taking it here would mean storing
// something no operator could retype.
func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}
