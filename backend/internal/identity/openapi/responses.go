package openapi

import (
	"github.com/peter/ticket_pos/backend/internal/identity/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// MessageData is a simple message payload in the success envelope.
type MessageData struct {
	Message string `json:"message"`
}

// VerifyOTPData is returned after a successful OTP verification.
type VerifyOTPData struct {
	Session   *service.SessionView `json:"session"`
	SessionID string               `json:"session_id"`
}

// EnvelopeOTPRequest documents POST /api/v1/auth/otp/request success responses.
type EnvelopeOTPRequest struct {
	Data      service.OTPRequestResult `json:"data"`
	Error     *platform.APIError       `json:"error"`
	RequestID string                   `json:"request_id"`
}

// EnvelopeVerifyOTP documents POST /api/v1/auth/otp/verify success responses.
type EnvelopeVerifyOTP struct {
	Data      VerifyOTPData      `json:"data"`
	Error     *platform.APIError `json:"error"`
	RequestID string             `json:"request_id"`
}

// EnvelopeVerifyGoogle documents POST /api/v1/auth/google/verify success
// responses. It is the VerifyOTP payload verbatim, because the two doors mint
// the same Staff Session and nothing on the wire says which was used.
type EnvelopeVerifyGoogle struct {
	Data      VerifyOTPData      `json:"data"`
	Error     *platform.APIError `json:"error"`
	RequestID string             `json:"request_id"`
}

// EnvelopeSession documents session-shaped success responses.
type EnvelopeSession struct {
	Data      service.SessionView `json:"data"`
	Error     *platform.APIError  `json:"error"`
	RequestID string              `json:"request_id"`
}

// EnvelopeLogout documents POST /api/v1/auth/logout success responses.
type EnvelopeLogout struct {
	Data      MessageData        `json:"data"`
	Error     *platform.APIError `json:"error"`
	RequestID string             `json:"request_id"`
}

// EnvelopeMembershipList documents GET /api/v1/staff/memberships success responses.
type EnvelopeMembershipList struct {
	Data      []service.MembershipView `json:"data"`
	Error     *platform.APIError       `json:"error"`
	RequestID string                   `json:"request_id"`
}

// EnvelopeStaffMe documents GET /api/v1/staff/me success responses.
type EnvelopeStaffMe struct {
	Data      service.ActiveMemberView `json:"data"`
	Error     *platform.APIError       `json:"error"`
	RequestID string                   `json:"request_id"`
}
