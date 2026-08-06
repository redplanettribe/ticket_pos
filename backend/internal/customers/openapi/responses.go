// Package openapi holds the typed success-envelope shapes the customers handlers
// reference from their swagger annotations, so the generated spec documents the
// real `data` payload rather than an untyped envelope.
package openapi

import (
	"github.com/peter/ticket_pos/backend/internal/customers/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/storage"
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

// EnvelopeCustomerProfile documents PATCH /api/v1/customer/profile success
// responses: the Customer's "My info" as it stands after the edit.
type EnvelopeCustomerProfile struct {
	Data      service.CustomerProfileView `json:"data"`
	Error     *platform.APIError          `json:"error"`
	RequestID string                      `json:"request_id"`
}

// EnvelopeCustomerAvatarUpload documents POST
// /api/v1/customer/profile/avatar-upload-url success responses: the presigned
// upload URL, the object key to attach afterwards, and the public URL the
// Avatar will be served from.
type EnvelopeCustomerAvatarUpload struct {
	Data      storage.CoverUploadResult `json:"data"`
	Error     *platform.APIError        `json:"error"`
	RequestID string                    `json:"request_id"`
}

// EnvelopeCheckoutReversal documents GET
// /api/v1/public/checkout/{clientTransactionId}/reversal success responses: the
// undo offer a guest is told about on the checkout success page — whether it
// stands, until when, and which sale it is about — and nothing about the buyer.
type EnvelopeCheckoutReversal struct {
	Data      service.ReversalOffer `json:"data"`
	Error     *platform.APIError    `json:"error"`
	RequestID string                `json:"request_id"`
}

// EnvelopeCustomerFollows documents GET /api/v1/customer/follows success
// responses: everything the Customer Follows, as one discriminated list.
type EnvelopeCustomerFollows struct {
	Data      service.FollowsView `json:"data"`
	Error     *platform.APIError  `json:"error"`
	RequestID string              `json:"request_id"`
}

// EnvelopeCustomerFollowSuggestions documents GET
// /api/v1/customer/follow-suggestions success responses: the Suggested Follows
// panel, as two groups rather than one interleaved list (#231, ADR 0031).
//
// A SECOND ENVELOPE BESIDE EnvelopeCustomerFollows RATHER THAN A WIDER ONE. The
// two reads answer different questions and are called by different callers —
// the listing by every public page with a Follow control on it, this only by the
// Following page — so a shared shape would document a payload most of its
// callers never receive.
type EnvelopeCustomerFollowSuggestions struct {
	Data      service.FollowSuggestionsView `json:"data"`
	Error     *platform.APIError            `json:"error"`
	RequestID string                        `json:"request_id"`
}

// EnvelopeCustomerDigestSubscription documents the success responses of both
// routes that write the Follow Digest switch (#224): POST
// /api/v1/customer/unsubscribe and PUT /api/v1/customer/digest.
//
// ONE SHAPE FOR BOTH, because both answer the same question — is the Digest on
// for this person now — and the two entry points differ only in what authorises
// them. The same fact also rides on the Follows listing, so nothing that reads
// it has to reconcile two spellings.
type EnvelopeCustomerDigestSubscription struct {
	Data      service.DigestSubscriptionView `json:"data"`
	Error     *platform.APIError             `json:"error"`
	RequestID string                         `json:"request_id"`
}

// EnvelopeCustomerFollow documents POST
// /api/v1/customer/follows/organizations/{slug} success responses: the one
// Follow, in exactly the shape it takes inside the list above.
type EnvelopeCustomerFollow struct {
	Data      service.FollowView `json:"data"`
	Error     *platform.APIError `json:"error"`
	RequestID string             `json:"request_id"`
}

// EnvelopeCustomerArea documents GET /api/v1/customer/ticket-sales success responses.
type EnvelopeCustomerArea struct {
	Data      service.CustomerAreaView `json:"data"`
	Error     *platform.APIError       `json:"error"`
	RequestID string                   `json:"request_id"`
}
