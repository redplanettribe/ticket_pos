package otp

import "github.com/peter/ticket_pos/backend/internal/platform/apperror"

// ErrRateLimited is returned when passcode request rate limits are exceeded.
func ErrRateLimited() apperror.DomainError {
	return apperror.New("OTP_RATE_LIMITED", "Too many passcode requests. Please try again later.", nil)
}

// ErrGlobalCeilingReached is returned when the platform-wide cap on passcode
// emails per window has been reached.
//
// Kept distinct from ErrRateLimited on purpose: both are 429s, but one means
// "you are asking too often" and the other means "the platform has stopped
// sending to everyone". An operator reading logs, and a caller deciding what to
// tell the person, need to tell those apart.
func ErrGlobalCeilingReached() apperror.DomainError {
	return apperror.New("OTP_GLOBAL_CEILING_REACHED", "Passcode sending is temporarily unavailable. Please try again later.", nil)
}

// ErrInvalid is returned when the submitted passcode does not match.
func ErrInvalid(remaining int) apperror.DomainError {
	return apperror.New("OTP_INVALID", "Invalid passcode.", map[string]any{
		"remaining_attempts": remaining,
	})
}

// ErrExpired is returned when the passcode has expired or been invalidated.
func ErrExpired() apperror.DomainError {
	return apperror.New("OTP_EXPIRED", "Passcode expired. Request a new one.", nil)
}

// ErrAttemptsExceeded is returned when too many wrong verify attempts were made.
func ErrAttemptsExceeded() apperror.DomainError {
	return apperror.New("OTP_ATTEMPTS_EXCEEDED", "Too many incorrect attempts. Request a new passcode.", nil)
}
