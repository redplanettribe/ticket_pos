package platform

// Stable codes for field-level validation failures.
//
// A FieldError has always carried a human message written in English. That
// message is a sentence for a person, and a sentence is the wrong thing for a
// client to branch on: it is reworded whenever the wording improves, and it
// cannot be shown to a buyer reading the Storefront in Spanish. The code is the
// half that does not move — a client keys its own copy on the code and falls
// back to the message when a validation has none yet.
//
// Two rules keep the codes worth keying on:
//
//   - A code names the RULE that failed, never the field it failed on. The field
//     is already on the FieldError, and "REQUIRED" means the same thing on
//     customer_email as on ticket_type_id. Two messages that say the same thing
//     in different words ("must be zero or greater", "must not be negative")
//     share one code; two that say different things get two, even when both
//     start with "is required".
//   - A code, once shipped, means exactly what it meant. Changing what a code
//     covers silently changes the sentence a Spanish-reading buyer is shown, so
//     a changed rule takes a new code rather than a redefined one.
//
// The messages beside each constant are the wording at the time the code was
// assigned, recorded so a later reader can see what the code was derived from.
// They are not a contract: the message may be reworded, the code may not.
const (
	// CodeRequired — "is required". A value was absent or blank where one is
	// always needed. The conditional variants below are deliberately separate:
	// "required because you supplied the other half of a pair" is a different
	// thing to explain to someone than "always required".
	CodeRequired = "REQUIRED"
	// CodeRequiredWithRefundedAmount — "is required when refunded_amount_cents
	// is given" (the Operator Reversal money memo pair, #126).
	CodeRequiredWithRefundedAmount = "REQUIRED_WITH_REFUNDED_AMOUNT"
	// CodeRequiredWithPlatformFeeKept — "is required when platform_fee_kept is
	// given" (the other half of the same pair).
	CodeRequiredWithPlatformFeeKept = "REQUIRED_WITH_PLATFORM_FEE_KEPT"

	// CodeInvalidEmail — "must be a valid email" / "must be a valid email
	// address". One code: the two wordings are the same verdict, reached by the
	// same parse, on surfaces that were simply written months apart.
	CodeInvalidEmail = "INVALID_EMAIL"
	// CodeInvalidPasscodeFormat — "must be 6 digits". The shape of a One-time
	// Passcode, not whether it is the right one; a wrong passcode is OTP_INVALID
	// on the envelope, never a field error.
	CodeInvalidPasscodeFormat = "INVALID_PASSCODE_FORMAT"

	// The Tax ID gradient (ADR 0016). Four codes because the four failures need
	// four different sentences: a buyer told "must be cedula, ruc, or passport"
	// is looking at the wrong dropdown, one told "must be a valid 10-digit
	// cédula" is looking at the wrong digit. See taxid.go, which is where the
	// messages these were derived from live.
	CodeInvalidTaxIDType = "INVALID_TAX_ID_TYPE"
	// CodeInvalidCedula — "must be a valid 10-digit cédula".
	CodeInvalidCedula = "INVALID_CEDULA"
	// CodeInvalidRUC — "must be a valid 13-digit RUC".
	CodeInvalidRUC = "INVALID_RUC"
	// CodeInvalidPassport — "must be 6–20 letters or digits".
	CodeInvalidPassport = "INVALID_PASSPORT"
	// CodeInvalidTaxIDNumber — "is not valid". The fallback for a number whose
	// Tax ID Type is valid but unrecognised by the message table; unreachable
	// today, kept because the message it labels is.
	CodeInvalidTaxIDNumber = "INVALID_TAX_ID_NUMBER"

	// The phone tiers (#103, #105), split for the same reason the Tax ID is:
	// the Ecuadorian message names a shape the buyer can check against the phone
	// in their hand, the generic one cannot. See phone.go.
	CodeInvalidPhoneEC = "INVALID_PHONE_EC"
	// CodeInvalidPhone — "must be 4–15 digits in international format, like
	// +12025550123".
	CodeInvalidPhone = "INVALID_PHONE"

	// CodeInvalidNonNegativeInt — "must be a non-negative integer", "must be
	// zero or greater", "must not be negative". One rule, three wordings.
	CodeInvalidNonNegativeInt = "INVALID_NON_NEGATIVE_INT"
	// CodeInvalidPositiveInt — "must be greater than zero".
	CodeInvalidPositiveInt = "INVALID_POSITIVE_INT"
	// CodeInvalidID — "must be a valid id". A malformed UUID, caught before the
	// database can turn it into a 500.
	CodeInvalidID = "INVALID_ID"
	// CodeInvalidSlug — "must be URL-safe (lowercase letters, numbers, and
	// hyphens)".
	CodeInvalidSlug = "INVALID_SLUG"
	// CodeInvalidTimestamp — "must be a valid RFC3339 timestamp" / "must be an
	// ISO 8601 timestamp".
	CodeInvalidTimestamp = "INVALID_TIMESTAMP"
	// CodeInvalidDate — "must be a date (YYYY-MM-DD)".
	CodeInvalidDate = "INVALID_DATE"
	// CodeInvalidTimezone — "must be a valid IANA timezone".
	CodeInvalidTimezone = "INVALID_TIMEZONE"
	// CodeEndBeforeStart — "must be after start time", reported on the end of a
	// range.
	CodeEndBeforeStart = "INVALID_END_BEFORE_START"
	// CodeStartAfterEnd — "must be before the end time", reported on the start of
	// a range. Separate from CodeEndBeforeStart because the two blame different
	// fields, and the field is what the reader is asked to fix.
	CodeStartAfterEnd = "INVALID_START_AFTER_END"

	// CodeInvalidImageContentType — "must be image/jpeg, image/png, or
	// image/webp".
	CodeInvalidImageContentType = "INVALID_IMAGE_CONTENT_TYPE"
	// CodeInvalidVideoContentType — "must be video/mp4".
	CodeInvalidVideoContentType = "INVALID_VIDEO_CONTENT_TYPE"
	// CodeInvalidUpload — "must be a valid multipart upload".
	CodeInvalidUpload = "INVALID_UPLOAD"

	// CodeInvalidFeeHandling — "must be pass_on or absorb".
	CodeInvalidFeeHandling = "INVALID_FEE_HANDLING"
	// CodeInvalidRegistrationMode — "must be tickets or external". How an Event
	// takes sign-ups (ADR 0028).
	CodeInvalidRegistrationMode = "INVALID_REGISTRATION_MODE"
	// CodeInvalidRegistrationURL — "must be an https URL". The Registration Link's
	// scheme allowlist, which is a security control rather than a formatting
	// preference: it is what keeps javascript: and data: out of a value destined
	// for both an href and a Location header, and what stops a Customer being
	// downgraded to http mid-journey. Its own code rather than a generic
	// INVALID_URL because the sentence a reader needs names the scheme.
	CodeInvalidRegistrationURL = "INVALID_REGISTRATION_URL"
	// CodeInvalidCurrency — "must be a supported ISO 4217 currency code".
	CodeInvalidCurrency = "INVALID_CURRENCY"
	// CodeInvalidRole — "must be org_admin, event_owner, or event_staff" and the
	// narrower assignment set. One code: both say "not a role you may set here",
	// and the message names the set that applies.
	CodeInvalidRole = "INVALID_ROLE"
	// CodeInvalidImportSource — "must be 'direct'".
	CodeInvalidImportSource = "INVALID_IMPORT_SOURCE"
	// CodeInvalidPaymentMethod — "must be 'cash' or 'transfer'".
	CodeInvalidPaymentMethod = "INVALID_PAYMENT_METHOD"
	// CodeInvalidAccountType — "must be ahorros or corriente". The Payout
	// Profile's bank account type, in Spanish because that is the word on the
	// receiving bank's own form (ADR 0026).
	CodeInvalidAccountType = "INVALID_ACCOUNT_TYPE"
	// CodeInvalidAccountNumber — "must contain digits only". A Payout Profile
	// account number after its spaces and dashes have been stripped; separate
	// from CodeTooLong, which is the other way the same field is refused.
	CodeInvalidAccountNumber = "INVALID_ACCOUNT_NUMBER"
	// CodeInvalidEnum — "must be one of …", where the accepted set is built at
	// runtime. The set is in the message because it is not in the code.
	CodeInvalidEnum = "INVALID_ENUM"
	// CodeInvalidRowSelection — "must be a comma-separated list of row numbers".
	CodeInvalidRowSelection = "INVALID_ROW_SELECTION"

	// CodeEmptyCollection — "must contain at least one line" / "at least one
	// row".
	CodeEmptyCollection = "EMPTY_COLLECTION"
	// CodeTooManyItems — "must contain at most N lines".
	CodeTooManyItems = "TOO_MANY_ITEMS"
	// CodeTooLong — "must be at most N characters".
	CodeTooLong = "TOO_LONG"
)
