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
//
// Message is the English sentence; Code is the stable machine name of the rule
// that failed (validation_codes.go). Clients key their own copy on Code and fall
// back to Message, which is why Code is omitempty rather than required: a
// validation that has not been given a code yet still renders, in English.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code,omitempty"`
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
	case "ORGANIZATION_SLUG_TAKEN", "EVENT_SLUG_TAKEN", "MEMBER_ALREADY_EXISTS", "LAST_ORG_ADMIN", "CANNOT_REMOVE_SELF", "CAPACITY_EXCEEDED", "PURCHASE_LIMIT_EXCEEDED", "IMPORT_BATCH_FAILED", "IMPORT_NOT_LATEST_BATCH", "IMPORT_ALREADY_REVERSED", "EVENT_NOT_DRAFT", "EVENT_DELETE_FORBIDDEN", "EVENT_PUBLISH_REQUIREMENTS_NOT_MET", "EVENT_ALREADY_PUBLISHED", "EVENT_ALREADY_CANCELLED", "EVENT_NOT_PUBLISHED", "TICKET_TYPE_DELETE_FORBIDDEN", "CURRENCY_LOCKED":
		return http.StatusConflict
	case "ASSIGNMENT_NOT_FOUND", "TICKET_TYPE_NOT_FOUND", "IMPORT_BATCH_NOT_FOUND", "PAYMENT_NOT_FOUND", "TICKET_SALE_NOT_FOUND", "PROMOTION_NOT_FOUND", "AFFILIATE_LINK_NOT_FOUND":
		return http.StatusNotFound
	// The Promotion refusals (ADR 0021). All 409: the request was well formed and
	// the caller was entitled to make it, but the catalog is not in a state that
	// admits it — the one slot is taken, or the price would break the invariant
	// that a Promotional Price sits strictly below its List Price. Each needs a
	// different edit elsewhere before it can succeed, which is why they are three
	// codes and not one.
	case "PROMOTION_ALREADY_EXISTS", "PROMOTIONAL_PRICE_NOT_BELOW_LIST_PRICE", "LIST_PRICE_NOT_ABOVE_PROMOTIONAL_PRICE":
		return http.StatusConflict
	// An Affiliate Link that has been clicked or has attributed a sale cannot be
	// deleted: 409, because the request was well formed and permitted, and the
	// link's history is what stands in the way. Deactivation is the way out, and
	// retrying the delete never becomes the answer.
	case "AFFILIATE_LINK_HAS_HISTORY":
		return http.StatusConflict
	// The Customer-initiated Sale Reversal refusals (ADR 0018). All three are
	// 409: the request was well formed and the caller was entitled to make it,
	// but the Ticket Sale is not in a state that admits an undo — already
	// reversed, never reversible, or past its Reversal Window. Retrying changes
	// nothing, which is what separates them from a 400 the caller could fix.
	case "SALE_ALREADY_REVERSED", "SALE_NOT_REVERSIBLE", "REVERSAL_WINDOW_CLOSED":
		return http.StatusConflict
	// A Customer pressing Undo on a sale whose reversal became an Unresolved
	// Reversal (ADR 0024). A fourth 409 for the same reason as the three above —
	// the request was fine and the sale is not in a state that admits an undo —
	// and pointedly not the 503-with-a-retry the operator's contention gets:
	// retrying is the one thing that must not happen here, because nobody knows
	// whether the money already went back.
	case "REVERSAL_UNRESOLVED":
		return http.StatusConflict
	// An Operator Reversal that contended with a reversal already in flight on the
	// same Ticket Sale (ADR 0024). 503 with a retry rather than 409: unlike the
	// three above, this says nothing about the sale's state — it is the platform
	// asking for a moment while it finds out what the Payment Provider did, and
	// retrying is exactly the right response.
	case "SALE_REVERSAL_IN_PROGRESS":
		return http.StatusServiceUnavailable
	// The three refusals about an Operator Reversal's money memo: a refund larger
	// than the Ticket Sale ever collected (#125), money stated about a sale that
	// collected none, and money left out of a sale that collected some (#126).
	// All are 400 and not 409 because the operator can fix every one of them by
	// restating the memo — the sale is perfectly reversible, the assertion about
	// it is not. Stated rather than left to the default so the choice is visible
	// beside the reversal refusals above.
	case "REFUNDED_AMOUNT_EXCEEDS_COLLECTED", "NOTHING_TO_REFUND", "REFUNDED_AMOUNT_REQUIRED":
		return http.StatusBadRequest
	// The provider was asked and refused. Nothing was changed, and the cause is
	// on the far side of a boundary the buyer cannot act on — a 502 rather than a
	// 409, because this is not a fact about their purchase.
	case "SALE_REVERSAL_FAILED":
		return http.StatusBadGateway
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
