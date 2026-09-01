package openapi

import (
	"github.com/peter/ticket_pos/backend/internal/identity/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// MessageData is a simple message payload in the success envelope.
type MessageData struct {
	Message string `json:"message"`
}

// VerifyOTPData is returned after a successful OTP verification: a Staff
// Session, or a terms-required outcome instead of one (#538, ADR 0066).
type VerifyOTPData struct {
	Session   *service.SessionView `json:"session"`
	SessionID string               `json:"session_id"`
	// TermsRequired is non-nil when no session was minted because the email owes
	// a Terms Acceptance of the current Terms Version; Session and SessionID are
	// then empty. Finished at /api/v1/auth/terms/accept.
	TermsRequired *service.TermsRequiredView `json:"terms_required"`
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

// EnvelopeAcceptTerms documents POST /api/v1/auth/terms/accept success
// responses. It is the VerifyOTP payload verbatim, because a finished terms
// step mints exactly the session the verify would have — `terms_required` is
// null here, the way `session` was null on the response that led here.
type EnvelopeAcceptTerms struct {
	Data      VerifyOTPData      `json:"data"`
	Error     *platform.APIError `json:"error"`
	RequestID string             `json:"request_id"`
}

// StaffTermsGateData is what a signed-in staff member owes the Terms gate
// (#570, ADR 0067): nothing, or the box to show before they see another page.
type StaffTermsGateData struct {
	Outstanding bool `json:"outstanding"`
	// TermsRequired is the interstitial's box — the sign-in door's terms step
	// verbatim, asked of somebody who is already inside — and null when nothing
	// is owed.
	TermsRequired *service.TermsRequiredView `json:"terms_required"`
}

// StaffTermsAcceptedData documents an acceptance recorded from a live session.
// It carries no session, because none was minted, re-minted or revoked.
type StaffTermsAcceptedData struct {
	Accepted bool `json:"accepted"`
}

// EnvelopeStaffTermsGate documents GET /api/v1/staff/terms/gate success
// responses.
type EnvelopeStaffTermsGate struct {
	Data      StaffTermsGateData `json:"data"`
	Error     *platform.APIError `json:"error"`
	RequestID string             `json:"request_id"`
}

// EnvelopeStaffTermsAccepted documents POST /api/v1/staff/terms/accept success
// responses.
type EnvelopeStaffTermsAccepted struct {
	Data      StaffTermsAcceptedData `json:"data"`
	Error     *platform.APIError     `json:"error"`
	RequestID string                 `json:"request_id"`
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
	Data      service.StaffMeView `json:"data"`
	Error     *platform.APIError  `json:"error"`
	RequestID string              `json:"request_id"`
}

// EnvelopeStaffLocale documents PUT /api/v1/staff/me/locale success responses.
type EnvelopeStaffLocale struct {
	Data      service.StaffLocaleView `json:"data"`
	Error     *platform.APIError      `json:"error"`
	RequestID string                  `json:"request_id"`
}
