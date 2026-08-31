package identity

import "github.com/peter/ticket_pos/backend/internal/platform/apperror"

// ErrSessionNotFound is returned when no session exists for the token.
func ErrSessionNotFound() apperror.DomainError {
	return apperror.New("SESSION_NOT_FOUND", "Session not found.", nil)
}

// ErrSessionExpired is returned when the session has expired.
func ErrSessionExpired() apperror.DomainError {
	return apperror.New("SESSION_EXPIRED", "Session expired. Please sign in again.", nil)
}

// ErrOrganizationSlugTaken is returned when an organization slug is already in use.
func ErrOrganizationSlugTaken(slug string) apperror.DomainError {
	return apperror.New("ORGANIZATION_SLUG_TAKEN", "Organization slug is already taken.", map[string]any{
		"slug": slug,
	})
}

// ErrMemberNotFound is returned when a member ID does not exist or is not owned by the session email.
func ErrMemberNotFound() apperror.DomainError {
	return apperror.New("MEMBER_NOT_FOUND", "Member not found.", nil)
}

// ErrNoActiveMember is returned when a staff workflow route requires an active member context.
func ErrNoActiveMember() apperror.DomainError {
	return apperror.New("FORBIDDEN", "Select an organization to continue.", nil)
}

// ErrForbidden is returned when the active Member lacks permission for the operation.
func ErrForbidden() apperror.DomainError {
	return apperror.New("FORBIDDEN", "You do not have permission to perform this action.", nil)
}

// ErrOrganizationNotFound is returned when an organization does not exist for the active Member.
func ErrOrganizationNotFound() apperror.DomainError {
	return apperror.New("ORGANIZATION_NOT_FOUND", "Organization not found.", nil)
}

// ErrMemberAlreadyExists is returned when a Member email is already in the Organization.
func ErrMemberAlreadyExists(email string) apperror.DomainError {
	return apperror.New("MEMBER_ALREADY_EXISTS", "A member with this email already exists in the organization.", map[string]any{
		"email": email,
	})
}

// ErrLastOrgAdmin is returned when removing or demoting the sole Org Admin.
func ErrLastOrgAdmin() apperror.DomainError {
	return apperror.New("LAST_ORG_ADMIN", "The organization must have at least one Org Admin.", nil)
}

// ErrCannotRemoveSelf is returned when a Member tries to remove themselves.
func ErrCannotRemoveSelf() apperror.DomainError {
	return apperror.New("CANNOT_REMOVE_SELF", "Add another Org Admin before removing yourself.", nil)
}

// ErrOrganizationDeleteConfirmationMismatch is returned when delete confirmation name does not match.
func ErrOrganizationDeleteConfirmationMismatch() apperror.DomainError {
	return apperror.New("ORGANIZATION_DELETE_CONFIRMATION_MISMATCH", "Confirmation name does not match the organization name.", nil)
}

// ErrAssignmentNotFound is returned when an event assignment does not exist.
func ErrAssignmentNotFound() apperror.DomainError {
	return apperror.New("ASSIGNMENT_NOT_FOUND", "Event assignment not found.", nil)
}

// ErrInvalidMemberRole is returned when a member role value is not allowed.
func ErrInvalidMemberRole(role string) apperror.DomainError {
	return apperror.New("INVALID_MEMBER_ROLE", "Invalid member role.", map[string]any{
		"role": role,
	})
}

// ErrLogoUploadUnavailable is returned when object storage is not configured.
func ErrLogoUploadUnavailable() apperror.DomainError {
	return apperror.New("LOGO_UPLOAD_UNAVAILABLE", "Logo upload is not available.", nil)
}

// ErrInvalidLogoImageKey is returned when a logo_image_key is not valid for the organization.
func ErrInvalidLogoImageKey() apperror.DomainError {
	return apperror.New("INVALID_LOGO_IMAGE_KEY", "Logo image key is not valid for this organization.", nil)
}

// ErrPendingTermsInvalid is returned for a pending-terms token that is
// unknown, spent or expired (#538). One refusal for all three, deliberately
// indistinguishable, exactly as the customer side's pending-consent token
// gets: a token cannot be probed for a different answer, and the recovery is
// to sign in again.
func ErrPendingTermsInvalid() apperror.DomainError {
	return apperror.New("PENDING_TERMS_INVALID", "This sign-in can no longer be finished. Please sign in again.", nil)
}

// ErrTermsAcceptanceRequired is returned when a terms submission arrives with
// the required box unticked (#538). The refusal lives in the API and not only
// in the form's disabled button.
func ErrTermsAcceptanceRequired() apperror.DomainError {
	return apperror.New("TERMS_ACCEPTANCE_REQUIRED", "The Términos y Condiciones must be accepted to continue.", nil)
}

// ErrHouseOrganizationCurrencyUnsupported is returned when an Organization is
// designated a House Organization while trading in a currency the platform's
// Issuer does not issue Sale Invoices in (#472, ADR 0060). The message names
// the currency, because that is the whole of what the operator can do about
// it.
func ErrHouseOrganizationCurrencyUnsupported(currency string) apperror.DomainError {
	return apperror.New("HOUSE_ORGANIZATION_CURRENCY_UNSUPPORTED",
		"This organization trades in "+currency+", and the platform issues Sale Invoices in USD only. A House Organization must trade in USD.",
		map[string]any{"currency": currency})
}

// ErrStaffSubjectNotFound is returned when a per-subject staff record is
// addressed to somebody who is not on the Staff platform and has never accepted
// its Terms (#566, spec #556, ADR 0067).
//
// It is what a Staff Digest matching NOBODY resolves to. The digest is one-way
// by design — there is no reverse, so a caller holding one is matched against
// the staff population in constant time until an address answers — and a digest
// minted under a rotated key, or simply mistyped, matches none of them.
//
// 404 and NOT an empty record, for the reason its Customer counterpart
// LEGAL_SUBJECT_NOT_FOUND is: an operator following a stale bookmark must be
// told the person is not there rather than shown a blank record asserting that
// somebody exists and owes nothing.
//
// IT CARRIES NO DETAILS, and specifically not the digest it was asked for.
// Echoing the URL segment back into an error body would put it in one more
// place a log ships, and it names a human being.
func ErrStaffSubjectNotFound() apperror.DomainError {
	return apperror.New(
		"STAFF_SUBJECT_NOT_FOUND",
		"There is no acceptance record for that person.",
		nil,
	)
}
