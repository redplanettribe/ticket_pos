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

// CustomerVerifyOTPData is returned after a successful Customer passcode
// verification — and after a consent submission, which produces the same thing.
//
// It has TWO SHAPES and a client must handle both (#251): a session and its
// token, or `consent_required` with the two of them null. Proof of email
// ownership succeeded either way; what differs is whether the Customer has
// accepted the Policy Version that is current. A client that reads `session_id`
// and treats its absence as a failure will report a consent step as a broken
// sign-in.
type CustomerVerifyOTPData struct {
	Session   *service.CustomerSessionView `json:"session"`
	SessionID string                       `json:"session_id"`
	// ConsentRequired carries the short-lived, single-use pending-consent token
	// and the boxes to show. Null on an ordinary sign-in.
	ConsentRequired *service.ConsentRequiredView `json:"consent_required"`
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

// EnvelopeCustomerPrivacy documents GET /api/v1/customer/privacy success
// responses (#268): what the signed-in Customer has authorized.
//
// The Policy Version pair is read-only and has no write counterpart anywhere,
// because Policy Acceptance is not withdrawable — which is why this envelope
// has no request shape mirroring it.
type EnvelopeCustomerPrivacy struct {
	Data      service.PrivacyView `json:"data"`
	Error     *platform.APIError  `json:"error"`
	RequestID string              `json:"request_id"`
}

// EnvelopeCustomerOptionalConsents documents PUT
// /api/v1/customer/privacy/consents/{purpose} success responses (#268): both
// optional consents as they stand after one control was moved.
//
// It reports the PAIR although one request moves one of them, because the page
// that asked is drawing both and the states are the platform's finding rather
// than the request's echo. It is the same shape the read above embeds, so a
// page and the act it just performed cannot describe the two facts differently.
type EnvelopeCustomerOptionalConsents struct {
	Data      service.OptionalConsentsView `json:"data"`
	Error     *platform.APIError           `json:"error"`
	RequestID string                       `json:"request_id"`
}

// EnvelopeCustomerConsentConfirmation documents POST
// /api/v1/customer/consent/confirm success responses (#255): what one press of
// the confirmation link in a Sale Confirmation actually did.
//
// It is its own shape rather than the digest subscription's, although a
// confirmed Marketing Consent moves that same switch, because the question is
// different: this reports WHICH pending opt-ins this press resolved, and
// `already_resolved` when it resolved none — a second press, or an answer the
// owner has since given themselves. Both are successes.
type EnvelopeCustomerConsentConfirmation struct {
	Data      service.ConsentConfirmationView `json:"data"`
	Error     *platform.APIError              `json:"error"`
	RequestID string                          `json:"request_id"`
}

// CustomerConsentSubmissionData is returned by the consent submission endpoint,
// which spends one pending-consent token on one of two acts (#270, ADR 0039).
//
// It has THREE SHAPES and a client must handle each: the session a finished
// sign-in mints, `consent_required` — which this endpoint does not currently
// return, and which is here because the payload is the verify's — and
// `withdrawal`, the Consent Withdrawal a denials-only submission performed.
//
// `withdrawal` is null on every sign-in submission and every other field is null
// beside it, INCLUDING `session_id`: a withdrawal made on Proof of Email
// Ownership alone mints no Customer Session, and a client that reads a missing
// session as a failed sign-in would report a successful withdrawal as a broken
// one.
type CustomerConsentSubmissionData struct {
	Session         *service.CustomerSessionView   `json:"session"`
	SessionID       string                         `json:"session_id"`
	Follow          *service.FollowView            `json:"follow"`
	ConsentRequired *service.ConsentRequiredView   `json:"consent_required"`
	Withdrawal      *service.ConsentWithdrawalView `json:"withdrawal"`
}

// EnvelopeCustomerConsentSubmission documents POST
// /api/v1/customer/auth/consent success responses.
type EnvelopeCustomerConsentSubmission struct {
	Data      CustomerConsentSubmissionData `json:"data"`
	Error     *platform.APIError            `json:"error"`
	RequestID string                        `json:"request_id"`
}

// EnvelopeCustomerConsentWithdrawalProof documents POST
// /api/v1/customer/consent/withdrawal/passcode/verify success responses: the
// Proof of Email Ownership a Consent Withdrawal runs on, held in suspension.
//
// Its own shape rather than the verify's, because what is NOT in it is the
// point: no session, no session id, and no boxes. The passcode door of the
// withdrawal surface can return nothing else, which is what makes "no Customer
// Session is minted" visible in the contract rather than only in the code.
type EnvelopeCustomerConsentWithdrawalProof struct {
	Data      service.ConsentWithdrawalProofView `json:"data"`
	Error     *platform.APIError                 `json:"error"`
	RequestID string                             `json:"request_id"`
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
