package identity

import "github.com/peter/ticket_pos/backend/internal/platform/apperror"

type domainError struct {
	code    string
	message string
	details any
}

func (e *domainError) Error() string   { return e.message }
func (e *domainError) Code() string    { return e.code }
func (e *domainError) Message() string { return e.message }
func (e *domainError) Details() any    { return e.details }

func newDomainError(code, message string, details any) apperror.DomainError {
	return &domainError{code: code, message: message, details: details}
}

// ErrOTPRateLimited is returned when OTP request rate limits are exceeded.
func ErrOTPRateLimited() apperror.DomainError {
	return newDomainError("OTP_RATE_LIMITED", "Too many passcode requests. Please try again later.", nil)
}

// ErrOTPInvalid is returned when the submitted passcode does not match.
func ErrOTPInvalid(remaining int) apperror.DomainError {
	return newDomainError("OTP_INVALID", "Invalid passcode.", map[string]any{
		"remaining_attempts": remaining,
	})
}

// ErrOTPExpired is returned when the passcode has expired or been invalidated.
func ErrOTPExpired() apperror.DomainError {
	return newDomainError("OTP_EXPIRED", "Passcode expired. Request a new one.", nil)
}

// ErrOTPAttemptsExceeded is returned when too many wrong verify attempts were made.
func ErrOTPAttemptsExceeded() apperror.DomainError {
	return newDomainError("OTP_ATTEMPTS_EXCEEDED", "Too many incorrect attempts. Request a new passcode.", nil)
}

// ErrSessionNotFound is returned when no session exists for the token.
func ErrSessionNotFound() apperror.DomainError {
	return newDomainError("SESSION_NOT_FOUND", "Session not found.", nil)
}

// ErrSessionExpired is returned when the session has expired.
func ErrSessionExpired() apperror.DomainError {
	return newDomainError("SESSION_EXPIRED", "Session expired. Please sign in again.", nil)
}

// ErrOrganizationSlugTaken is returned when an organization slug is already in use.
func ErrOrganizationSlugTaken(slug string) apperror.DomainError {
	return newDomainError("ORGANIZATION_SLUG_TAKEN", "Organization slug is already taken.", map[string]any{
		"slug": slug,
	})
}

// ErrMemberNotFound is returned when a member ID does not exist or is not owned by the session email.
func ErrMemberNotFound() apperror.DomainError {
	return newDomainError("MEMBER_NOT_FOUND", "Member not found.", nil)
}

// ErrNoActiveMember is returned when a staff workflow route requires an active member context.
func ErrNoActiveMember() apperror.DomainError {
	return newDomainError("FORBIDDEN", "Select an organization to continue.", nil)
}

// ErrForbidden is returned when the active Member lacks permission for the operation.
func ErrForbidden() apperror.DomainError {
	return newDomainError("FORBIDDEN", "You do not have permission to perform this action.", nil)
}

// ErrOrganizationNotFound is returned when an organization does not exist for the active Member.
func ErrOrganizationNotFound() apperror.DomainError {
	return newDomainError("ORGANIZATION_NOT_FOUND", "Organization not found.", nil)
}

// ErrMemberAlreadyExists is returned when a Member email is already in the Organization.
func ErrMemberAlreadyExists(email string) apperror.DomainError {
	return newDomainError("MEMBER_ALREADY_EXISTS", "A member with this email already exists in the organization.", map[string]any{
		"email": email,
	})
}

// ErrLastOrgAdmin is returned when removing or demoting the sole Org Admin.
func ErrLastOrgAdmin() apperror.DomainError {
	return newDomainError("LAST_ORG_ADMIN", "The organization must have at least one Org Admin.", nil)
}

// ErrCannotRemoveSelf is returned when a Member tries to remove themselves.
func ErrCannotRemoveSelf() apperror.DomainError {
	return newDomainError("CANNOT_REMOVE_SELF", "Add another Org Admin before removing yourself.", nil)
}

// ErrOrganizationDeleteConfirmationMismatch is returned when delete confirmation name does not match.
func ErrOrganizationDeleteConfirmationMismatch() apperror.DomainError {
	return newDomainError("ORGANIZATION_DELETE_CONFIRMATION_MISMATCH", "Confirmation name does not match the organization name.", nil)
}

// ErrAssignmentNotFound is returned when an event assignment does not exist.
func ErrAssignmentNotFound() apperror.DomainError {
	return newDomainError("ASSIGNMENT_NOT_FOUND", "Event assignment not found.", nil)
}

// ErrInvalidMemberRole is returned when a member role value is not allowed.
func ErrInvalidMemberRole(role string) apperror.DomainError {
	return newDomainError("INVALID_MEMBER_ROLE", "Invalid member role.", map[string]any{
		"role": role,
	})
}
