package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/consent"
	customerssvc "github.com/peter/ticket_pos/backend/internal/customers/service"
	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// requestReferenceMaxLength bounds the Operator's naming of the inbound
// artefact. A reference is a sentence — "Formulario de Revocatoria, signed
// 2026-08-01, received by post" — and the bound is stated here so a caller is
// told which field is wrong rather than shown a database error. It matches the
// reversal note's bound for the same reason: both are a human's line of prose
// about something that happened off the platform.
const requestReferenceMaxLength = 500

// LookUpCustomerConsent returns one Customer's consent state, found by email.
//
// @Summary      Look up a Customer's consent state by email address
// @Description  Returns the Customer at an email address and their CURRENT consent state, across every Organization on the platform — Customer identity is global and separate from staff (ADR 0010), so a Customer's consents are the platform's relationship with them and no Organization may inspect them. The payload identifies the person before anybody acts on their behalf (id, email, name) and reports what a withdrawal would actually change: marketing_consent and networking_consent as granted, denied or pending_confirmation, and NULL where the Customer has never been asked — null is UNANSWERED and is a different fact from denied, published as the different fact it is so that nobody is shown a refusal they never made. pending_confirmation is a tick from somebody who never proved the address: standing against it, denied for sending, never expiring. policy_accepted_at is when they last accepted a Policy Version, null if never; it is shown and is NOT actionable, because Policy Acceptance is not withdrawable — it is absent from the withdrawal form, it gates the platform on a basis other than consent, and clearing it would re-gate the person rather than free them. withdrew is null here: a lookup takes nothing away. The address is matched on its normalised form, so case does not matter. READING WRITES NOTHING — no consent is captured and no Consent Record appears, because a lookup that recorded something would put an act in the evidence log that nobody performed. An address no Customer holds is 404 CUSTOMER_NOT_FOUND: an Operator is entitled to know, and that candour is bought by the door rather than granted by the answer — every caller the operator allowlist has not admitted is refused identically for an address that exists and one that does not. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        email  path  string  true  "Customer email address (matched case-insensitively)"
// @Success      200  {object}  openapi.EnvelopeOperatorCustomerConsent
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/customers/{email}/consent [get]
func (h *Handler) LookUpCustomerConsent(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	view, err := h.svc.LookUpCustomerConsent(r.Context(), strings.TrimSpace(r.PathValue("email")))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, view)
}

// recordConsentWithdrawalBody is a Consent Withdrawal that arrived off the
// platform, as the Operator states it.
//
// THE TWO CONSENTS ARE POINTERS BECAUSE ABSENT, FALSE AND TRUE ARE THREE
// DIFFERENT THINGS, and the third is a refusal rather than a value:
//
//   - Absent is "this form did not name this consent", and nothing is written
//     for it. A form asking for one thing takes one thing away; reading its
//     silence as a refusal would turn a marketing withdrawal into a Withdraw All.
//   - False is the withdrawal.
//   - TRUE IS REFUSED. An Operator cannot manufacture consent, and the shape of
//     the body is deliberately capable of expressing the grant so that the API
//     can be seen to refuse it: a field that simply did not exist would leave
//     "can this surface grant?" answered by a JSON decoder rather than by a rule.
//     Nothing here filters it — it travels to the consent module's single write
//     path, which refuses it for every caller.
//
// There is no policy_acceptance field and there never will be. It is not
// withdrawable, and an operator-recorded acceptance would be a staff member
// asserting that somebody agreed to a document.
type recordConsentWithdrawalBody struct {
	MarketingConsent  *bool  `json:"marketing_consent"`
	NetworkingConsent *bool  `json:"networking_consent"`
	RequestReference  string `json:"request_reference"`
}

// RecordCustomerConsentWithdrawal records a Consent Withdrawal on a Customer's
// behalf.
//
// @Summary      Record a Consent Withdrawal that arrived off the platform
// @Description  Records that a Customer withdrew an optional consent by a route other than the platform — counsel's printed form, or an email to the data-protection address — so that a request which arrived on paper can be honoured within the legal deadline without anybody editing the database by hand. THIS SURFACE CAN ONLY WITHDRAW, NEVER GRANT: marketing_consent and networking_consent accept false (withdraw) or absence (this form did not name this consent, and nothing is written for it), and a value of TRUE is refused with 400 CONSENT_GRANT_NOT_PERMITTED. The refusal is the API's rather than the form's — an Operator who could grant could manufacture the very consent they exist to honour the withdrawal of — and it is enforced in the platform's single consent-write path, so no caller escapes it. Because it can only withdraw, the proven-ness question that decides granted-or-pending on every other channel never arises here. At least one of the two consents must be named, and request_reference is REQUIRED: it names the inbound artefact (the dated form, the letter, the email) and is at most 500 characters. It is a POINTER TO EVIDENCE HELD ELSEWHERE rather than evidence itself — it is never parsed and nothing is ever decided from its contents. The act writes exactly ONE Consent Record on channel operator_request, carrying the state each consent was in immediately before it, the acting operator's email (taken from the Staff Session, NEVER from the body) and the reference — the pair that stops a staff action from ever being presented as somebody's own click. Marketing Consent and the Follow Digest move in lockstep in the same transaction (ADR 0034). Policy Acceptance is untouched: it is not withdrawable. The response reports the state AFTER the act and `withdrew`, which is what the act TOOK AWAY — not the same question as what it answered, since `denied` reads the same whether somebody just gave something up or was already refusing. The Customer receives the same withdrawal confirmation email as any other channel, at their own stored address and in their Mail Locale, ONLY when something actually moved; an act that changed nothing is still recorded and mails nobody. A failure to send never fails the withdrawal. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        email  path  string                       true  "Customer email address (matched case-insensitively)"
// @Param        body   body  recordConsentWithdrawalBody  true  "Which consents the artefact withdrew, and which artefact it was"
// @Success      200  {object}  openapi.EnvelopeOperatorCustomerConsent
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/customers/{email}/consent/withdrawal [post]
func (h *Handler) RecordCustomerConsentWithdrawal(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body recordConsentWithdrawalBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	input, fields := validateRecordConsentWithdrawal(body)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	// Who recorded this comes from the session and nowhere else. The one act no
	// Customer performed must not also be the one act nobody is accountable for,
	// and an attribution a caller could name would be no attribution at all.
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}
	input.RecordedBy = session.Email
	// The circumstances of the RECORDING — the Operator's own browser and
	// session. On a channel where the act itself happened on paper, that is the
	// only technical fact there is, and it is recorded as what it is rather than
	// dressed up as the Customer's.
	input.Evidence = consent.EvidenceFromRequest(r)
	input.Evidence.SessionID = session.SessionID

	view, err := h.svc.RecordCustomerConsentWithdrawal(r.Context(), strings.TrimSpace(r.PathValue("email")), input)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, view)
}

// validateRecordConsentWithdrawal checks the request's SHAPE and pointedly not
// its meaning.
//
// It refuses a body that names no consent — there is nothing to record — and one
// with no artefact reference, because the reference is what makes the row
// evidence rather than an assertion. Blank is the same as absent: "recorded
// against nothing" has one spelling.
//
// WHAT IT DELIBERATELY DOES NOT CHECK is the grant. A `true` passes through here
// untouched and is refused by the consent module's write path, which is the only
// place the refusal is worth anything: a check here would make "an Operator
// cannot grant" a property of this handler rather than of the platform, and
// would leave the next caller of that write path to remember it for themselves.
// This is the same split validateReverseSale makes — shape here, meaning where
// the facts are.
func validateRecordConsentWithdrawal(body recordConsentWithdrawalBody) (customerssvc.OperatorWithdrawalInput, []platform.FieldError) {
	var fields []platform.FieldError

	if body.MarketingConsent == nil && body.NetworkingConsent == nil {
		fields = append(fields, platform.FieldError{
			Field:   "marketing_consent",
			Code:    platform.CodeRequired,
			Message: "at least one consent must be named",
		})
	}

	reference := strings.TrimSpace(body.RequestReference)
	if reference == "" {
		fields = append(fields, platform.FieldError{
			Field:   "request_reference",
			Code:    platform.CodeRequired,
			Message: "is required: name the form, letter or email this withdrawal answers",
		})
	} else if len([]rune(reference)) > requestReferenceMaxLength {
		fields = append(fields, platform.FieldError{
			Field:   "request_reference",
			Code:    platform.CodeTooLong,
			Message: "must be at most " + strconv.Itoa(requestReferenceMaxLength) + " characters",
		})
	}

	return customerssvc.OperatorWithdrawalInput{
		MarketingConsent:  body.MarketingConsent,
		NetworkingConsent: body.NetworkingConsent,
		RequestReference:  reference,
	}, fields
}
