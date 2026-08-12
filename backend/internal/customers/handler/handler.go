// Package handler exposes the Customer sign-in and Customer Area HTTP surface.
//
// Every response uses the standard envelope and every route is versioned under
// /api/v1/. Handlers stay thin: validate the request, call the service, map any
// domain error through the platform mapper.
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/mail"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/customers"
	"github.com/peter/ticket_pos/backend/internal/customers/middleware"
	"github.com/peter/ticket_pos/backend/internal/customers/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Handler exposes HTTP endpoints for Customer identity.
type Handler struct {
	svc *service.Service
}

// New returns a customers HTTP handler.
func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

type otpRequestBody struct {
	Email string `json:"email"`
	// Locale is the language of the Storefront page this passcode was asked
	// from, and it words THAT ONE EMAIL AND NOTHING ELSE (ADR 0033).
	//
	// It is emphatically not the Mail Locale being set: this route is anonymous,
	// so anybody could name anybody's address here, and letting that rewrite a
	// stored property of a stranger's record would be a way to change what
	// language their receipts arrive in. Only a completed sign-in remembers a
	// language — see otpVerifyBody.
	//
	// Optional, and never a reason to refuse: a caller with no page to name one
	// omits it, and a language this platform does not serve is dropped. The
	// passcode goes out in English either way, because a person locked out of
	// their tickets must not be kept there by a spelling.
	Locale string `json:"locale"`
}

type otpVerifyBody struct {
	Email string `json:"email"`
	Code  string `json:"code"`
	// Locale is the language of the Storefront page this sign-in happened on,
	// and it is the one field here that is not part of proving anything. It is
	// remembered as the Customer's Mail Locale (ADR 0030), because a Locale is
	// a property of a page's address and the Follow Digest is mail. Optional and
	// never validated into a refusal: a caller with no page to name — anything
	// but the Storefront — omits it and leaves what was remembered standing, and
	// a language this platform does not serve is dropped rather than made a
	// reason a person cannot sign in.
	Locale string `json:"locale"`
	// Follow is the Follow somebody asked for before they could be asked who
	// they are (#219): one string, "organization:<slug>", carried explicitly
	// from the sign-in address rather than stashed in browser storage so that it
	// is server-visible and can be validated at all.
	//
	// It names a subject and never a subscriber. Whose Follow it becomes is
	// decided by the session this verification mints and by nothing in this
	// body — the Email field above proves who is signing in, and is never read
	// as who is being subscribed. Optional; see service.ParseFollowIntent.
	Follow string `json:"follow"`
}

// verifyOTPResponse is what both doors answer with: the session, its token, and
// what became of any Follow intent that rode along.
//
// Follow is null on every ordinary sign-in, and null too when an intent named a
// subject that no longer exists — verification is what was asked for and does
// not fail over the other half. It is present rather than implied so a client
// can render the control in its true state without a second round trip, and so
// the round trip is assertable at this seam.
type verifyOTPResponse struct {
	Session   *service.CustomerSessionView `json:"session"`
	SessionID string                       `json:"session_id"`
	Follow    *service.FollowView          `json:"follow"`
	// ConsentRequired is the other shape this response has (#251): proof of email
	// ownership succeeded and NO session was minted, because the Customer has not
	// accepted the Policy Version that is current. It carries a short-lived,
	// single-use pending-consent token and the boxes to show; `session`,
	// `session_id` and `follow` are all null beside it, and the sign-in is
	// finished — or abandoned — at the consent submission endpoint.
	//
	// Null on every ordinary sign-in, so an existing client reading `session_id`
	// sees the same field it always did. What it must NOT do is treat a missing
	// `session_id` as a transport failure; the BFF checks this field.
	ConsentRequired *service.ConsentRequiredView `json:"consent_required"`
}

// signInResponse renders whichever of the two outcomes a proven email produced,
// and applies the Follow intent to the session — when there is one.
//
// A held sign-in applies no Follow: there is no session to write it against,
// and the intent's whole security property is that it is written against the
// session verification produced and against no identifier a request could name.
// The Storefront relays the intent again on the consent submission, which is
// where a session finally exists (#219, #251).
func (h *Handler) signInResponse(ctx context.Context, outcome *service.SignInOutcome, intent *service.FollowIntent) verifyOTPResponse {
	response := verifyOTPResponse{
		Session:         outcome.Session,
		SessionID:       outcome.SessionID,
		ConsentRequired: outcome.ConsentRequired,
	}
	if outcome.SessionID != "" {
		response.Follow = h.svc.ApplyFollowIntent(ctx, outcome.SessionID, intent)
	}
	return response
}

// RequestOTP sends a one-time passcode to a Customer's email.
//
// @Summary      Request Customer passcode
// @Description  Sends a one-time passcode for Customer sign-in. The response is identical whether or not the email is known, so it does not reveal who the platform's Customers are. An optional `locale` names the language of the Storefront page the passcode was asked from and words that one email only: it does not update the Customer's stored Mail Locale, which only a completed sign-in writes. A language the platform does not serve is ignored rather than refused, and the passcode is sent in English.
// @Tags         customer
// @Accept       json
// @Produce      json
// @Param        body  body      otpRequestBody  true  "Email address"
// @Success      200   {object}  openapi.EnvelopeCustomerOTPRequest
// @Failure      400   {object}  platform.Envelope
// @Failure      429   {object}  platform.Envelope
// @Router       /api/v1/customer/auth/otp/request [post]
func (h *Handler) RequestOTP(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body otpRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	if fields := validateEmail(body.Email); len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	// The client IP is derived by the platform, never taken from this handler's
	// own reading of the request, so per-IP rate limiting counts one agreed value.
	result, err := h.svc.RequestOTP(r.Context(), body.Email, platform.ClientIP(r), body.Locale)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}

// VerifyOTP validates a passcode and issues a Customer Session.
//
// @Summary      Verify Customer passcode
// @Description  Verifies a Customer one-time passcode, marks the Customer verified, and issues a Customer Session. An optional `locale` names the language of the Storefront the sign-in happened on and is remembered as the Customer's Mail Locale; a language the platform does not serve is ignored rather than refused. An optional `follow` carries a Follow the visitor pressed before signing in, as `organization:<slug>`. It is applied against the Customer Session this call mints and against nothing else, so an email in this request can never become the address that gets subscribed; the Follow that was made comes back in `follow`, or null. A malformed intent — an unknown kind, or a subject that is not a well-formed slug — is refused with 400 before the passcode is checked, so it does not spend it. A subject that resolves to nothing does not fail the sign-in: the session is issued and `follow` is null.
// @Tags         customer
// @Accept       json
// @Produce      json
// @Param        body  body      otpVerifyBody  true  "Email and passcode"
// @Success      200   {object}  openapi.EnvelopeCustomerVerifyOTP
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      429   {object}  platform.Envelope
// @Router       /api/v1/customer/auth/otp/verify [post]
func (h *Handler) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body otpVerifyBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	fields := validateEmail(body.Email)
	fields = append(fields, validateOTPCode(body.Code)...)
	// The intent is parsed BEFORE anything is proved, and a malformed one is
	// refused here rather than after verification. A passcode is single-use: were
	// the intent read afterwards, a mangled one would spend the code and leave
	// somebody staring at a form asking for a passcode that is now worthless.
	intent, intentFields := service.ParseFollowIntent(body.Follow)
	fields = append(fields, intentFields...)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	outcome, err := h.svc.VerifyOTP(r.Context(), body.Email, body.Code, body.Locale)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	// Applied against the token that verification just returned, and against no
	// other identifier in this request. That is the whole security property of
	// the feature, and it is a property of this line: nothing else here could
	// name a Customer even if it wanted to.
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, h.signInResponse(r.Context(), outcome, intent))
}

type googleVerifyBody struct {
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier"`
	RedirectURI  string `json:"redirect_uri"`
	// Locale is read exactly as it is on the passcode door; see otpVerifyBody.
	Locale string `json:"locale"`
	// Follow is read exactly as it is on the passcode door too, and deliberately
	// so: both doors are Proof of Email Ownership and neither is worth more than
	// the other (ADR 0011), so a visitor who pressed Follow and then chose Google
	// must not silently lose it. This body cannot name an email at all, which
	// makes the rule that the intent never chooses a subscriber structural here.
	Follow string `json:"follow"`
}

// VerifyGoogle completes a Google Sign-In and issues a Customer Session.
//
// The three fields below are the whole of the request, and what is missing from
// them is the point: the Storefront cannot name an email address here. It relays
// an authorization code that only Google can turn into one, so a bug in the
// Storefront cannot mint a session for an address of its choosing (ADR 0011).
//
// The route is unauthenticated because, like passcode verification, it mints the
// credential rather than consuming one.
//
// @Summary      Verify a Google Sign-In
// @Description  Exchanges an authorization code obtained on the Storefront at Google's token endpoint, and issues a Customer Session on the email address Google vouches for. Marks the Customer verified by the same rule a passcode does. An optional `locale` is remembered as the Customer's Mail Locale, exactly as on the passcode route. An optional `follow` carries a Follow intent and is honoured exactly as on the passcode route, because both doors are equal Proof of Email Ownership. Every failure returns one generic error, so the route reveals nothing about which addresses the platform knows.
// @Tags         customer
// @Accept       json
// @Produce      json
// @Param        body  body      googleVerifyBody  true  "Authorization code, PKCE verifier and redirect URI"
// @Success      200   {object}  openapi.EnvelopeCustomerVerifyGoogle
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Router       /api/v1/customer/auth/google/verify [post]
func (h *Handler) VerifyGoogle(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body googleVerifyBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	var fields []platform.FieldError
	for _, required := range []struct {
		name  string
		value string
	}{
		{"code", body.Code},
		{"code_verifier", body.CodeVerifier},
		{"redirect_uri", body.RedirectURI},
	} {
		if strings.TrimSpace(required.value) == "" {
			fields = append(fields, platform.FieldError{Field: required.name, Code: platform.CodeRequired, Message: "is required"})
		}
	}
	intent, intentFields := service.ParseFollowIntent(body.Follow)
	fields = append(fields, intentFields...)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	outcome, err := h.svc.VerifyGoogleSignIn(r.Context(), body.Code, body.CodeVerifier, body.RedirectURI, body.Locale)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, h.signInResponse(r.Context(), outcome, intent))
}

type confirmationLinkBody struct {
	Token string `json:"token"`
	// Follow exists on this body only so that it can be refused, and refused
	// loudly (#219).
	//
	// A Confirmation Link mints a sale-scoped session, which is possession of an
	// email somebody was SENT and may well have been forwarded. #217 already
	// refuses that session the three Follow routes; an intent riding the
	// redemption would be the same subscription by another road — whoever a Sale
	// Confirmation reached could sign the ticket-holder's address up for a
	// weekly email without ever proving they own it (ADR 0010, CONTEXT.md
	// "Follow"). Leaving the field off the struct would have refused it too, by
	// silently dropping it, and silence is the wrong answer to a request that
	// must never work.
	Follow string `json:"follow"`
}

// RedeemConfirmationLink exchanges a Confirmation Link token for a Customer
// Session scoped to the one Ticket Sale the link names.
//
// The route is unauthenticated because the token IS the credential — the same
// reason passcode verification is unauthenticated. An Authorization header, when
// present, is not a requirement but a claim to something wider: a caller already
// holding a full Customer Session keeps it, and this call returns that session
// rather than the narrower one the link would have minted.
//
// @Summary      Redeem a Confirmation Link
// @Description  Exchanges the signed token from a Sale Confirmation for a short-lived Customer Session scoped to that one Ticket Sale. Does not mark the Customer verified. If a full Customer Session is presented in Authorization, it is returned unchanged rather than narrowed. A `follow` intent is refused outright with CUSTOMER_SESSION_SCOPE_INSUFFICIENT: this door mints a sale-scoped session, and subscribing an address to mail takes the same proof signing in does.
// @Tags         customer
// @Accept       json
// @Produce      json
// @Param        body  body      confirmationLinkBody  true  "Confirmation Link token"
// @Success      200   {object}  openapi.EnvelopeCustomerVerifyOTP
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      403   {object}  platform.Envelope
// @Router       /api/v1/customer/auth/confirmation-link [post]
func (h *Handler) RedeemConfirmationLink(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body confirmationLinkBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	if strings.TrimSpace(body.Token) == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "token", Code: platform.CodeRequired, Message: "is required"}})
		return
	}
	// Refused before the link is redeemed, so the answer is the same whether or
	// not the token was any good: this door does not subscribe anybody, and
	// whether it could have opened is not part of that answer. The same 403 the
	// Follow routes give a sale-scoped session, for the same reason.
	if strings.TrimSpace(body.Follow) != "" {
		_ = platform.WriteDomainError(w, reqID, customers.ErrFollowRequiresFullSession())
		return
	}

	session, sessionID, err := h.svc.RedeemConfirmationLink(r.Context(), body.Token, platform.BearerToken(r))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, verifyOTPResponse{
		Session:   session,
		SessionID: sessionID,
	})
}

// GetSession returns the current Customer Session.
//
// @Summary      Get Customer Session
// @Description  Returns which email the caller is signed in as, and extends the sliding session window. consent_boxes reports which consent checkboxes a capture surface must still show this Customer — the checkout dialog reads it here so that who is buying and what may still be asked of them come from one snapshot of one session (#254). A box is true when its answer is outstanding: policy_acceptance when there is no acceptance of the CURRENT Policy Version, and each optional consent when its state is unanswered, with a Pending Confirmation counting as unanswered because somebody else's tick is not the owner's answer (ADR 0035). It never says what to pre-tick; boxes are always drawn unticked. A Confirmation Link session (ticket_sale_id set) reports all three true whatever the stored state says: it is minted from a forwarded email rather than from Proof of Email Ownership, so a capture under it is treated as a guest's on the write side too.
// @Tags         customer
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeCustomerSession
// @Failure      401  {object}  platform.Envelope
// @Router       /api/v1/customer/auth/session [get]
func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	session, err := h.svc.GetSession(r.Context(), customerSessionToken(r))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, session)
}

// Logout destroys the current Customer Session.
//
// @Summary      Customer sign-out
// @Description  Destroys the Customer Session. The token is worthless immediately; a Staff Session is unaffected.
// @Tags         customer
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeCustomerLogout
// @Failure      401  {object}  platform.Envelope
// @Router       /api/v1/customer/auth/logout [post]
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	if err := h.svc.Logout(r.Context(), customerSessionToken(r)); err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, map[string]string{
		"message": "Signed out",
	})
}

// ListTicketSales returns the Customer Area: the signed-in Customer's Ticket
// Sales, upcoming and past, across every Organization.
//
// The only identifier this handler passes to the service is the session token the
// caller presented. It reads nothing from the path, query, or body — a Customer
// id, email, or Organization supplied in the request has nowhere to go.
//
// @Summary      List the Customer's Ticket Sales
// @Description  Returns the signed-in Customer's Ticket Sales, upcoming and past, across all Organizations. Always scoped by the Customer Session, never by any identifier in the request. Each sale reports whether the Customer could undo it right now (`reversible`) and, when they could, until when (`reversible_until`, RFC3339 UTC). That deadline is the offer's rather than a publication of the Reversal Window: while an undo is on offer it is the earlier of 20:00 Ecuador time on the day of purchase or the Event's start (ADR 0018), and it is null whenever no undo is on offer even if that Window is still open. Only an active Online Sale can be reversible, and its Payment must be one this deployment can actually undo: a free claim always is, since nothing was collected, and a paid one is when the Payment Provider that collected it supports reversal (ADR 0012). A sale settled by some other provider therefore reports false inside its window rather than offering an undo that would fail. `reversible_until` is null whenever `reversible` is false. This endpoint reports the window and nothing more: there is no reversal action here.
// @Tags         customer
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeCustomerArea
// @Failure      401  {object}  platform.Envelope
// @Router       /api/v1/customer/ticket-sales [get]
func (h *Handler) ListTicketSales(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	area, err := h.svc.GetCustomerArea(r.Context(), customerSessionToken(r))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, area)
}

// updateProfileBody is the editable half of a Customer's record, and its shape
// is the contract: there is no email field here, so no request can name one.
//
// The two Tax ID halves are pointers because null is meaningful on this
// endpoint and only on this one — it is how "clear my Tax ID" is spelled. Sent
// together they assert a Tax ID, absent together they clear it, and one without
// the other is a validation failure: the pair is one fact.
//
// The phone needs one more state than a pointer can hold, hence
// platform.OptionalString: this field was added to an endpoint that already
// existed (#108), so a request that never mentions the phone must leave it
// exactly where it is rather than be read as a clear. A Storefront running the
// previous build sends no phone key at all, and a deploy window is no reason for
// a buyer to lose their number.
type updateProfileBody struct {
	FirstName   string  `json:"first_name"`
	LastName    string  `json:"last_name"`
	TaxIDType   *string `json:"tax_id_type"`
	TaxIDNumber *string `json:"tax_id_number"`
	// The phone number in canonical E.164 form, e.g. +593987654321. Null or blank
	// clears the stored number; omitting the field leaves it untouched.
	Phone platform.OptionalString `json:"phone" swaggertype:"string"`
}

// UpdateProfile edits the signed-in Customer's "My info": their name, their one
// current Tax ID assertion, and their phone number.
//
// The request carries no identifier of who is being edited. The Customer is the
// one on the session and can be no other, exactly as for the Customer Area read.
//
// @Summary      Update the Customer's profile
// @Description  Edits the signed-in Customer's name, Tax ID, and phone number — the "My info" section of the Customer Area. The name must be non-blank on both halves; the Tax ID is validated by the same rules as checkout, and sending both halves null clears it. The phone is validated by the same rule as checkout and stored in canonical E.164 form; sending it null or blank clears it, and omitting the field entirely leaves the stored number untouched. The email is the Customer's identity and is not editable here. Requires a full Customer Session: a Confirmation Link session is refused with CUSTOMER_SESSION_SCOPE_INSUFFICIENT. The edit moves the Customer's current assertion only — every Ticket Sale keeps the name and Tax ID it was transacted under.
// @Tags         customer
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      updateProfileBody  true  "Name, Tax ID, and phone"
// @Success      200   {object}  openapi.EnvelopeCustomerProfile
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      403   {object}  platform.Envelope
// @Router       /api/v1/customer/profile [patch]
func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body updateProfileBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	fields, input := validateUpdateProfile(body)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	profile, err := h.svc.UpdateProfile(r.Context(), customerSessionToken(r), input)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, profile)
}

// validateUpdateProfile applies the handler-layer rules: non-blank names, a Tax
// ID that is either wholly absent or wholly valid, and a phone that is either
// being cleared or passes the shared rule.
//
// The blank-name rejection is the one rule here that is load-bearing beyond this
// endpoint. repository.Upsert treats a Customer whose name is currently blank as
// one who was never named, so that a first Ticket Sale can name someone who
// signed in before ever buying; this editor is the only write path that could
// blank a name that was set, and it refuses to (#102).
func validateUpdateProfile(body updateProfileBody) ([]platform.FieldError, service.UpdateProfileInput) {
	var fields []platform.FieldError

	firstName := strings.TrimSpace(body.FirstName)
	lastName := strings.TrimSpace(body.LastName)
	if firstName == "" {
		fields = append(fields, platform.FieldError{Field: "first_name", Code: platform.CodeRequired, Message: "is required"})
	}
	if lastName == "" {
		fields = append(fields, platform.FieldError{Field: "last_name", Code: platform.CodeRequired, Message: "is required"})
	}

	input := service.UpdateProfileInput{FirstName: firstName, LastName: lastName}

	taxIDType := trimmedOrEmpty(body.TaxIDType)
	taxIDNumber := trimmedOrEmpty(body.TaxIDNumber)
	switch {
	case taxIDType == "" && taxIDNumber == "":
		// Both absent: the Customer is clearing their Tax ID, which they are
		// entitled to do. Nothing prefills at their next checkout until they
		// supply one again — where a sale may fill the blank they left
		// (ADR 0016).
	case taxIDType == "":
		fields = append(fields, platform.FieldError{Field: "tax_id_type", Code: platform.CodeRequired, Message: "is required"})
	case taxIDNumber == "":
		fields = append(fields, platform.FieldError{Field: "tax_id_number", Code: platform.CodeRequired, Message: "is required"})
	default:
		normalized, taxIDFields := platform.TaxIDFieldErrors("tax_id_type", "tax_id_number", taxIDType, taxIDNumber)
		if len(taxIDFields) > 0 {
			fields = append(fields, taxIDFields...)
		} else {
			input.TaxIDType, input.TaxIDNumber = &taxIDType, &normalized
		}
	}

	// The phone. Whether the request is talking about it at all is decided by the
	// key's presence, and only then by its value: a blank or null clears the
	// stored number, which is a capability the Customer is entitled to (#103,
	// user story 10) rather than an empty form nobody filled in. Anything else
	// goes through the one shared rule, under the field name it travels under
	// here — `phone`, where checkout says `customer_phone` — so the wording a
	// person reads is identical on both surfaces (#105).
	if body.Phone.Present {
		input.PhoneSet = true
		if phone := trimmedOrEmpty(body.Phone.Value); phone != "" {
			normalized, phoneFields := platform.PhoneFieldErrors("phone", phone)
			if len(phoneFields) > 0 {
				fields = append(fields, phoneFields...)
			} else {
				input.Phone = &normalized
			}
		}
	}

	return fields, input
}

// trimmedOrEmpty flattens "absent", "null", and "blank" into one value, because
// on this endpoint they mean the same thing: the Customer supplied nothing here.
func trimmedOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

// avatarUploadURLBody names the image about to be uploaded as an Avatar. The
// content type decides the object key's extension and must be on the image
// allowlist; the file name is advisory.
type avatarUploadURLBody struct {
	ContentType string  `json:"content_type"`
	FileName    *string `json:"file_name"`
}

// CreateAvatarUploadURL returns a presigned URL for uploading the signed-in
// Customer's Avatar.
//
// @Summary      Create an Avatar upload URL
// @Description  Returns a presigned PUT URL for uploading the signed-in Customer's Avatar image (JPEG, PNG, or WebP), keyed under that Customer alone. The upload itself goes straight to object storage; attaching the uploaded image is a separate PUT to /customer/profile/avatar with the object key. Requires a full Customer Session.
// @Tags         customer
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      avatarUploadURLBody  true  "Image content type and optional file name"
// @Success      200   {object}  openapi.EnvelopeCustomerAvatarUpload
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      403   {object}  platform.Envelope
// @Router       /api/v1/customer/profile/avatar-upload-url [post]
func (h *Handler) CreateAvatarUploadURL(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body avatarUploadURLBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	contentType := strings.ToLower(strings.TrimSpace(body.ContentType))
	if contentType == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "content_type", Code: platform.CodeRequired, Message: "is required"},
		})
		return
	}

	fileName := ""
	if body.FileName != nil {
		fileName = strings.TrimSpace(*body.FileName)
	}

	result, err := h.svc.CreateAvatarUploadURL(r.Context(), customerSessionToken(r), contentType, fileName)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}

// updateAvatarBody carries the object key of an uploaded Avatar image. There is
// no URL here: the key must sit under the Customer's own prefix, and the service
// refuses any that does not.
type updateAvatarBody struct {
	ImageKey string `json:"image_key"`
}

// UpdateAvatar attaches an uploaded image as the signed-in Customer's Avatar.
//
// @Summary      Set the Customer's Avatar
// @Description  Attaches a previously uploaded image as the signed-in Customer's Avatar. The image key must be one minted by the upload-URL endpoint for this Customer; any other key is refused. Requires a full Customer Session.
// @Tags         customer
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      updateAvatarBody  true  "Uploaded image object key"
// @Success      200   {object}  openapi.EnvelopeCustomerProfile
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      403   {object}  platform.Envelope
// @Router       /api/v1/customer/profile/avatar [put]
func (h *Handler) UpdateAvatar(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body updateAvatarBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	imageKey := strings.TrimSpace(body.ImageKey)
	if imageKey == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "image_key", Code: platform.CodeRequired, Message: "is required"},
		})
		return
	}

	profile, err := h.svc.UpdateAvatar(r.Context(), customerSessionToken(r), &imageKey)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, profile)
}

// DeleteAvatar removes the signed-in Customer's Avatar.
//
// @Summary      Remove the Customer's Avatar
// @Description  Removes the signed-in Customer's Avatar, reverting them to the initials fallback. Always allowed, whether the Avatar was uploaded or seeded from Google Sign-In. Requires a full Customer Session.
// @Tags         customer
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeCustomerProfile
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/customer/profile/avatar [delete]
func (h *Handler) DeleteAvatar(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	profile, err := h.svc.UpdateAvatar(r.Context(), customerSessionToken(r), nil)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, profile)
}

// customerSessionToken returns the Customer Session token for the request,
// preferring the one the middleware already validated.
func customerSessionToken(r *http.Request) string {
	if session, ok := middleware.SessionFromContext(r.Context()); ok {
		return session.SessionID
	}
	return platform.BearerToken(r)
}

func validateEmail(email string) []platform.FieldError {
	email = strings.TrimSpace(email)
	if email == "" {
		return []platform.FieldError{{Field: "email", Code: platform.CodeRequired, Message: "is required"}}
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return []platform.FieldError{{Field: "email", Code: platform.CodeInvalidEmail, Message: "must be a valid email address"}}
	}
	return nil
}

func validateOTPCode(code string) []platform.FieldError {
	code = strings.TrimSpace(code)
	if code == "" {
		return []platform.FieldError{{Field: "code", Code: platform.CodeRequired, Message: "is required"}}
	}
	if len(code) != 6 {
		return []platform.FieldError{{Field: "code", Code: platform.CodeInvalidPasscodeFormat, Message: "must be 6 digits"}}
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			return []platform.FieldError{{Field: "code", Code: platform.CodeInvalidPasscodeFormat, Message: "must be 6 digits"}}
		}
	}
	return nil
}
