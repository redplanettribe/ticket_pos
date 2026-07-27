package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform/apperror"
)

type contextKey string

const requestIDKey contextKey = "request_id"

// Envelope is the standard JSON response shape for all API endpoints.
type Envelope struct {
	Data      any       `json:"data"`
	Error     *APIError `json:"error"`
	RequestID string    `json:"request_id"`
}

// APIError is the standard error object inside the envelope.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// FieldError describes a single validation failure.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationErrorDetails is the details payload for VALIDATION_FAILED.
type ValidationErrorDetails struct {
	Fields []FieldError `json:"fields"`
}

// WriteJSON encodes v as JSON and writes it with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

// WriteSuccess writes a success envelope with the given data payload.
func WriteSuccess(w http.ResponseWriter, requestID string, status int, data any) error {
	w.Header().Set("X-Request-ID", requestID)
	return WriteJSON(w, status, Envelope{
		Data:      data,
		Error:     nil,
		RequestID: requestID,
	})
}

// WriteHandlerError writes a handler-layer error envelope.
func WriteHandlerError(w http.ResponseWriter, requestID string, status int, code, message string, details any) error {
	w.Header().Set("X-Request-ID", requestID)
	return WriteJSON(w, status, Envelope{
		Data: nil,
		Error: &APIError{
			Code:    code,
			Message: message,
			Details: details,
		},
		RequestID: requestID,
	})
}

// WriteDomainError maps a domain error to HTTP and writes the envelope.
func WriteDomainError(w http.ResponseWriter, requestID string, err error) error {
	var domainErr apperror.DomainError
	if errors.As(err, &domainErr) {
		status := domainHTTPStatus(domainErr.Code())
		return WriteHandlerError(w, requestID, status, domainErr.Code(), domainErr.Message(), domainErr.Details())
	}
	return WriteHandlerError(w, requestID, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred", nil)
}

func domainHTTPStatus(code string) int {
	switch code {
	// OTP_GLOBAL_CEILING_REACHED shares the status with the per-key limits but
	// never the code: same "come back later" for the caller, a different signal
	// entirely for whoever is reading the logs.
	case "OTP_RATE_LIMITED", "OTP_ATTEMPTS_EXCEEDED", "OTP_GLOBAL_CEILING_REACHED":
		return http.StatusTooManyRequests
	case "OTP_INVALID", "OTP_EXPIRED":
		return http.StatusUnauthorized
	case "SESSION_NOT_FOUND", "SESSION_EXPIRED":
		return http.StatusUnauthorized
	// A failed Google Sign-In is 401 for the same reason a bad passcode is: the
	// caller proved nothing. It is one code covering every cause on purpose —
	// see googleauth.ErrSignInFailed.
	case "GOOGLE_SIGN_IN_FAILED":
		return http.StatusUnauthorized
	// A Customer Session failing is reported with its own codes: it is an
	// unrelated record on an unrelated surface to a Staff Session (ADR 0010).
	case "CUSTOMER_SESSION_NOT_FOUND", "CUSTOMER_SESSION_EXPIRED":
		return http.StatusUnauthorized
	// A genuine Customer Session that is simply too narrow for what it was
	// presented for — a Confirmation Link session asking to edit the profile. It
	// is 403 and not 401 because re-presenting the same credential can never
	// help; a wider one is needed.
	case "CUSTOMER_SESSION_SCOPE_INSUFFICIENT":
		return http.StatusForbidden
	// A Confirmation Link is a credential, so a bad or spent one is 401 for the
	// same reason a bad passcode is: the caller failed to prove anything.
	case "CONFIRMATION_LINK_INVALID", "CONFIRMATION_LINK_EXPIRED":
		return http.StatusUnauthorized
	// No signing key configured is a deployment fault, not the caller's — as is
	// object storage missing when an Avatar upload is asked for.
	case "CONFIRMATION_LINK_UNAVAILABLE", "AVATAR_UPLOAD_UNAVAILABLE":
		return http.StatusInternalServerError
	case "FORBIDDEN":
		return http.StatusForbidden
	case "NOT_FOUND", "ORGANIZATION_NOT_FOUND", "MEMBER_NOT_FOUND", "EVENT_NOT_FOUND":
		return http.StatusNotFound
	case "ORGANIZATION_SLUG_TAKEN", "EVENT_SLUG_TAKEN", "MEMBER_ALREADY_EXISTS", "LAST_ORG_ADMIN", "CANNOT_REMOVE_SELF", "CAPACITY_EXCEEDED", "IMPORT_BATCH_FAILED", "IMPORT_NOT_LATEST_BATCH", "IMPORT_ALREADY_REVERSED", "EVENT_NOT_DRAFT", "EVENT_DELETE_FORBIDDEN", "EVENT_PUBLISH_REQUIREMENTS_NOT_MET", "EVENT_ALREADY_PUBLISHED", "EVENT_ALREADY_CANCELLED", "EVENT_NOT_PUBLISHED", "TICKET_TYPE_DELETE_FORBIDDEN", "CURRENCY_LOCKED":
		return http.StatusConflict
	case "ASSIGNMENT_NOT_FOUND", "TICKET_TYPE_NOT_FOUND", "IMPORT_BATCH_NOT_FOUND", "PAYMENT_NOT_FOUND":
		return http.StatusNotFound
	// The provider took the money but the Ticket Sale could not be recorded: a
	// platform-side failure the caller cannot fix, logged loudly server-side for
	// the operator to resolve by hand.
	case "PAYMENT_SALE_COMMIT_FAILED":
		return http.StatusInternalServerError
	default:
		return http.StatusBadRequest
	}
}

// WriteInvalidJSON writes a malformed JSON error response.
func WriteInvalidJSON(w http.ResponseWriter, requestID string) error {
	return WriteHandlerError(w, requestID, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON", nil)
}

// WriteValidationError writes a validation error response.
func WriteValidationError(w http.ResponseWriter, requestID string, fields []FieldError) error {
	return WriteHandlerError(w, requestID, http.StatusBadRequest, "VALIDATION_FAILED", "Request validation failed", ValidationErrorDetails{
		Fields: fields,
	})
}

// WriteUnauthorized writes an unauthorized error response.
func WriteUnauthorized(w http.ResponseWriter, requestID string, message string) error {
	if message == "" {
		message = "Authentication required"
	}
	return WriteHandlerError(w, requestID, http.StatusUnauthorized, "UNAUTHORIZED", message, nil)
}

// RequestID returns the request ID stored on the context.
func RequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// WithRequestID stores a request ID on the context.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}
