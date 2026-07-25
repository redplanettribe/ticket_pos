// Package openapi holds the typed success-envelope shapes the customers handlers
// reference from their swagger annotations, so the generated spec documents the
// real `data` payload rather than an untyped envelope.
package openapi

import (
	"github.com/peter/ticket_pos/backend/internal/customers/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// MessageData is a simple message payload in the success envelope.
type MessageData struct {
	Message string `json:"message"`
}

// CustomerVerifyOTPData is returned after a successful Customer passcode verification.
type CustomerVerifyOTPData struct {
	Session   *service.CustomerSessionView `json:"session"`
	SessionID string                       `json:"session_id"`
}

// EnvelopeCustomerOTPRequest documents POST /api/v1/customer/auth/otp/request success responses.
type EnvelopeCustomerOTPRequest struct {
	Data      service.CustomerOTPRequestResult `json:"data"`
	Error     *platform.APIError               `json:"error"`
	RequestID string                           `json:"request_id"`
}

// EnvelopeCustomerVerifyOTP documents POST /api/v1/customer/auth/otp/verify success responses.
type EnvelopeCustomerVerifyOTP struct {
	Data      CustomerVerifyOTPData `json:"data"`
	Error     *platform.APIError    `json:"error"`
	RequestID string                `json:"request_id"`
}

// EnvelopeCustomerVerifyGoogle documents POST /api/v1/customer/auth/google/verify
// success responses. Its payload is the passcode path's, unchanged and
// deliberately so: nothing on the wire records how somebody signed in.
type EnvelopeCustomerVerifyGoogle struct {
	Data      CustomerVerifyOTPData `json:"data"`
	Error     *platform.APIError    `json:"error"`
	RequestID string                `json:"request_id"`
}

// EnvelopeCustomerSession documents GET /api/v1/customer/auth/session success responses.
type EnvelopeCustomerSession struct {
	Data      service.CustomerSessionView `json:"data"`
	Error     *platform.APIError          `json:"error"`
	RequestID string                      `json:"request_id"`
}

// EnvelopeCustomerLogout documents POST /api/v1/customer/auth/logout success responses.
type EnvelopeCustomerLogout struct {
	Data      MessageData        `json:"data"`
	Error     *platform.APIError `json:"error"`
	RequestID string             `json:"request_id"`
}

// EnvelopeCustomerArea documents GET /api/v1/customer/ticket-sales success responses.
type EnvelopeCustomerArea struct {
	Data      service.CustomerAreaView `json:"data"`
	Error     *platform.APIError       `json:"error"`
	RequestID string                   `json:"request_id"`
}
