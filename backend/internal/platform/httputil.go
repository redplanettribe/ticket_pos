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
	case "OTP_RATE_LIMITED", "OTP_ATTEMPTS_EXCEEDED":
		return http.StatusTooManyRequests
	case "OTP_INVALID", "OTP_EXPIRED":
		return http.StatusUnauthorized
	case "SESSION_NOT_FOUND", "SESSION_EXPIRED":
		return http.StatusUnauthorized
	case "FORBIDDEN":
		return http.StatusForbidden
	case "NOT_FOUND", "ORGANIZATION_NOT_FOUND", "MEMBER_NOT_FOUND", "EVENT_NOT_FOUND":
		return http.StatusNotFound
	case "ORGANIZATION_SLUG_TAKEN", "EVENT_SLUG_TAKEN", "MEMBER_ALREADY_EXISTS", "LAST_ORG_ADMIN", "CANNOT_REMOVE_SELF", "CAPACITY_EXCEEDED", "EVENT_NOT_DRAFT", "EVENT_DELETE_FORBIDDEN", "EVENT_PUBLISH_REQUIREMENTS_NOT_MET", "EVENT_ALREADY_PUBLISHED", "EVENT_ALREADY_CANCELLED", "EVENT_NOT_PUBLISHED", "TICKET_TYPE_DELETE_FORBIDDEN", "CURRENCY_LOCKED":
		return http.StatusConflict
	case "ASSIGNMENT_NOT_FOUND", "TICKET_TYPE_NOT_FOUND":
		return http.StatusNotFound
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
