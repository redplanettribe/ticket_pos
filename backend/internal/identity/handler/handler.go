package handler

import (
	"encoding/json"
	"net/http"
	"net/mail"
	"regexp"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/identity/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Handler exposes HTTP endpoints for identity authentication.
type Handler struct {
	svc *service.Service
}

// New returns an identity HTTP handler.
func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type createOrganizationBody struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type otpRequestBody struct {
	Email string `json:"email"`
}

type otpVerifyBody struct {
	Email string `json:"email"`
	Code  string `json:"code"`
	// Locale is the language the login page was rendered in, as the caller
	// detected it. Optional, and remembered as the Staff Locale only when the
	// person has none. It is the one locale field on a read-adjacent route in
	// this API and it names a fact about a person, not a language for the API to
	// answer in (ADR 0027).
	Locale string `json:"locale"`
}

type googleVerifyBody struct {
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier"`
	RedirectURI  string `json:"redirect_uri"`
	// Locale, exactly as on the passcode door and for the same reason.
	Locale string `json:"locale"`
}

type staffLocaleBody struct {
	Locale string `json:"locale"`
}

type selectOrganizationBody struct {
	MemberID string `json:"member_id"`
}

type acceptTermsBody struct {
	// PendingTermsToken is the single-use token a gated verify returned.
	PendingTermsToken string `json:"pending_terms_token"`
	// TermsAcceptance is the one required box. Absent is false and false is
	// refused — the API's guarantee, not the form's.
	TermsAcceptance bool `json:"terms_acceptance"`
}

// RequestOTP sends a one-time passcode to the given email.
//
// @Summary      Request OTP
// @Description  Sends a one-time passcode to the given email address.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      otpRequestBody  true  "Email address"
// @Success      200   {object}  openapi.EnvelopeOTPRequest
// @Failure      400   {object}  platform.Envelope
// @Failure      429   {object}  platform.Envelope
// @Router       /api/v1/auth/otp/request [post]
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

	result, err := h.svc.RequestOTP(r.Context(), body.Email, platform.ClientIP(r))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}

// VerifyOTP validates a passcode and creates a session.
//
// @Summary      Verify OTP
// @Description  Verifies a one-time passcode and completes the sign-in. The outcome has exactly two shapes: `session` and `session_id`, or — for an email with no Terms Acceptance of the current Terms Version — `terms_required` with a single-use `pending_terms_token`, the edition label and the checkbox label, and NO session (#538, ADR 0066). The terms step is finished at /api/v1/auth/terms/accept. An optional `locale` names the language the login page was rendered in, as the caller detected it, and is remembered as the person's Staff Locale — but only if they have none. It never overwrites a stored one, because a detected language must not overrule a chosen one. A language the platform does not serve is ignored rather than refused, and never fails the sign-in.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      otpVerifyBody  true  "Email and passcode"
// @Success      200   {object}  openapi.EnvelopeVerifyOTP
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      429   {object}  platform.Envelope
// @Router       /api/v1/auth/otp/verify [post]
func (h *Handler) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body otpVerifyBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	fields := validateEmail(body.Email)
	if codeFields := validateOTPCode(body.Code); len(codeFields) > 0 {
		fields = append(fields, codeFields...)
	}
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	outcome, err := h.svc.VerifyOTP(r.Context(), body.Email, body.Code, body.Locale)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, outcome)
}

// VerifyGoogle completes a Google Sign-In and issues a Staff Session.
//
// The three fields below are the whole of the request, and what is missing from
// them is the point: the Staff app cannot name an email address here. It relays
// an authorization code that only Google can turn into one, so a bug in the
// Staff app cannot mint a session for an address of its choosing (ADR 0011).
//
// The route is unauthenticated because, like passcode verification, it mints the
// credential rather than consuming one.
//
// @Summary      Verify a Google Sign-In
// @Description  Exchanges an authorization code obtained on the Staff app at Google's token endpoint, using the staff OAuth client, and issues a Staff Session on the email address Google vouches for. The session and the auth-fork that follows are identical to a passcode's. An optional `locale` is remembered as the person's Staff Locale exactly as on the passcode route: only when they have none. Every failure returns one generic error, so the route reveals nothing about which addresses the platform knows.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      googleVerifyBody  true  "Authorization code, PKCE verifier and redirect URI"
// @Success      200   {object}  openapi.EnvelopeVerifyGoogle
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Router       /api/v1/auth/google/verify [post]
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
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	outcome, err := h.svc.VerifyGoogleSignIn(r.Context(), body.Code, body.CodeVerifier, body.RedirectURI, body.Locale)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, outcome)
}

// AcceptTerms finishes a sign-in the terms gate held: it spends the
// pending-terms token, records one Staff Terms Acceptance, and mints the Staff
// Session the verify withheld (#538, ADR 0066).
//
// Unauthenticated, because the token IS the credential — the same reason the
// customer consent submission is. It reveals nothing to anybody who does not
// already hold one: an unknown, spent or expired token gets one
// indistinguishable refusal.
//
// @Summary      Accept the Terms and finish a staff sign-in
// @Description  Spends the short-lived, single-use `pending_terms_token` from a verify response whose outcome was `terms_required`, records one append-only Staff Terms Acceptance — the current Terms edition, the capacity "organizer", the timestamp, and the technical proof (IP, user agent, session, origin URL) — and mints the Staff Session the sign-in withheld; the response carries `session` and `session_id` exactly as a verify does (#538, ADR 0066). `terms_acceptance` must be true: an unticked box is refused with TERMS_ACCEPTANCE_REQUIRED, and the refusal lives in the API, not only in the form. The token is spent whatever the outcome, so a refused submission is restarted by signing in again; abandoning the step records nothing and leaves no session. One acceptance per email per edition: subsequent sign-ins pass with no extra step until a later Terms Version is published.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      acceptTermsBody  true  "Pending terms token and the ticked box"
// @Success      200   {object}  openapi.EnvelopeAcceptTerms
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Router       /api/v1/auth/terms/accept [post]
func (h *Handler) AcceptTerms(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body acceptTermsBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	if strings.TrimSpace(body.PendingTermsToken) == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "pending_terms_token", Code: platform.CodeRequired, Message: "is required"},
		})
		return
	}

	outcome, err := h.svc.AcceptTerms(r.Context(), service.TermsAcceptanceSubmission{
		Token:           body.PendingTermsToken,
		TermsAcceptance: body.TermsAcceptance,
		// The technical proof is derived from the REQUEST and never from the
		// body — the one spelling every capture surface shares.
		Evidence: consent.EvidenceFromRequest(r),
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, outcome)
}

// GetSession returns the current authenticated session.
//
// @Summary      Get session
// @Description  Returns the authenticated session and membership summary.
// @Tags         auth
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeSession
// @Failure      401  {object}  platform.Envelope
// @Router       /api/v1/auth/session [get]
func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	token := platform.BearerToken(r)
	if token == "" {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	session, err := h.svc.GetSession(r.Context(), token)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, session)
}

// Logout destroys the current session.
//
// @Summary      Logout
// @Description  Destroys the authenticated session.
// @Tags         auth
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeLogout
// @Failure      401  {object}  platform.Envelope
// @Router       /api/v1/auth/logout [post]
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	token := platform.BearerToken(r)
	if token == "" {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	if err := h.svc.Logout(r.Context(), token); err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, map[string]string{
		"message": "Signed out",
	})
}

// CreateOrganization creates an organization and org_admin membership for the session.
//
// @Summary      Create organization
// @Description  Creates an organization, org_admin member, and sets the session active member.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      createOrganizationBody  true  "Organization details"
// @Success      201   {object}  openapi.EnvelopeSession
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      409   {object}  platform.Envelope
// @Router       /api/v1/staff/organizations [post]
func (h *Handler) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	token := platform.BearerToken(r)
	if token == "" {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	var body createOrganizationBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	if fields := validateCreateOrganization(body.Name, body.Slug); len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	session, err := h.svc.CreateOrganization(r.Context(), token, service.CreateOrganizationInput{
		Name: body.Name,
		Slug: body.Slug,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusCreated, session)
}

// ListMemberships returns memberships for the authenticated session email.
//
// @Summary      List memberships
// @Description  Lists organization memberships for the authenticated session email.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeMembershipList
// @Failure      401  {object}  platform.Envelope
// @Router       /api/v1/staff/memberships [get]
func (h *Handler) ListMemberships(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	token := platform.BearerToken(r)
	if token == "" {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	memberships, err := h.svc.ListMemberships(r.Context(), token)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, memberships)
}

// SelectOrganization sets the active member on the session.
//
// @Summary      Select organization
// @Description  Sets the active member on the authenticated session.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      selectOrganizationBody  true  "Member to activate"
// @Success      200   {object}  openapi.EnvelopeSession
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      404   {object}  platform.Envelope
// @Router       /api/v1/staff/session/organization [post]
func (h *Handler) SelectOrganization(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	token := platform.BearerToken(r)
	if token == "" {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	var body selectOrganizationBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	if fields := validateMemberID(body.MemberID); len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	session, err := h.svc.SelectOrganization(r.Context(), token, body.MemberID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, session)
}

// GetStaffMe returns the active member context for staff workflow routes.
//
// @Summary      Get staff context
// @Description  Returns the active member and organization for the authenticated session, together with the signed-in person's Staff Locale. `locale` is null when they have stated none, which is not the same as English: it says nobody has chosen, and a reader with null is written to in English all the same. The API reports which language a person prefers; it does not answer in one, and no read path here consults Accept-Language (ADR 0027).
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeStaffMe
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/staff/me [get]
func (h *Handler) GetStaffMe(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	sessionID := staffSessionID(r)
	if sessionID == "" {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	member, err := h.svc.GetStaffMe(r.Context(), sessionID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, member)
}

// SetStaffLocale records the language the signed-in person chose.
//
// It takes a bearer token and nothing else — no Active Member is loaded and no
// role is checked. That is the point: the Staff Locale belongs to a person, so a
// Platform Operator who is a Member of no Organization can set one, and an Event
// Staff member can set their own without asking an Org Admin.
//
// @Summary      Set staff locale
// @Description  Records the Staff Locale of the signed-in person: the language the staff app and staff mail are written in for them. Keyed on the session's email address, so it follows them across devices and across Organizations, and switching Organization does not change it. Requires only a Staff Session — no Active Member and no role — because a personal preference is not an Organization's setting. A language the platform does not serve is refused with a field error rather than ignored, unlike the detected locale on the sign-in routes: a stated choice that quietly did nothing would be worse than one that failed.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      staffLocaleBody  true  "Language to record"
// @Success      200   {object}  openapi.EnvelopeStaffLocale
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Router       /api/v1/staff/me/locale [put]
func (h *Handler) SetStaffLocale(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	token := platform.BearerToken(r)
	if token == "" {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	var body staffLocaleBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	locale, fields := validateLocale(body.Locale)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	result, err := h.svc.SetStaffLocale(r.Context(), token, locale)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}

func staffSessionID(r *http.Request) string {
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

func validateCreateOrganization(name, slug string) []platform.FieldError {
	name = strings.TrimSpace(name)
	slug = strings.ToLower(strings.TrimSpace(slug))
	var fields []platform.FieldError
	if name == "" {
		fields = append(fields, platform.FieldError{Field: "name", Code: platform.CodeRequired, Message: "is required"})
	}
	if slug == "" {
		fields = append(fields, platform.FieldError{Field: "slug", Code: platform.CodeRequired, Message: "is required"})
	} else if !slugPattern.MatchString(slug) {
		fields = append(fields, platform.FieldError{Field: "slug", Code: platform.CodeInvalidSlug, Message: "must be URL-safe (lowercase letters, numbers, and hyphens)"})
	}
	return fields
}

// validateLocale accepts one of the languages this platform is written in, and
// refuses everything else. It is deliberately strict where the sign-in routes
// are permissive: they are dropping a browser's guess, this is recording a
// person's choice.
func validateLocale(raw string) (platform.Locale, []platform.FieldError) {
	if strings.TrimSpace(raw) == "" {
		return "", []platform.FieldError{{Field: "locale", Code: platform.CodeRequired, Message: "is required"}}
	}
	locale, ok := platform.ParseLocale(raw)
	if !ok {
		return "", []platform.FieldError{{Field: "locale", Code: platform.CodeInvalidLocale, Message: "must be en or es"}}
	}
	return locale, nil
}

func validateMemberID(memberID string) []platform.FieldError {
	memberID = strings.TrimSpace(memberID)
	if memberID == "" {
		return []platform.FieldError{{Field: "member_id", Code: platform.CodeRequired, Message: "is required"}}
	}
	return nil
}
