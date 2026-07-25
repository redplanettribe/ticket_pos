package handler

import (
	"encoding/json"
	"net/http"
	"net/mail"
	"regexp"
	"strings"

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
}

type googleVerifyBody struct {
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier"`
	RedirectURI  string `json:"redirect_uri"`
}

type selectOrganizationBody struct {
	MemberID string `json:"member_id"`
}

type verifyOTPResponse struct {
	Session   *service.SessionView `json:"session"`
	SessionID string               `json:"session_id"`
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
// @Description  Verifies a one-time passcode and creates a server-side session.
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

	session, sessionID, err := h.svc.VerifyOTP(r.Context(), body.Email, body.Code)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, verifyOTPResponse{
		Session:   session,
		SessionID: sessionID,
	})
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
// @Description  Exchanges an authorization code obtained on the Staff app at Google's token endpoint, using the staff OAuth client, and issues a Staff Session on the email address Google vouches for. The session and the auth-fork that follows are identical to a passcode's. Every failure returns one generic error, so the route reveals nothing about which addresses the platform knows.
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
			fields = append(fields, platform.FieldError{Field: required.name, Message: "is required"})
		}
	}
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	session, sessionID, err := h.svc.VerifyGoogleSignIn(r.Context(), body.Code, body.CodeVerifier, body.RedirectURI)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, verifyOTPResponse{
		Session:   session,
		SessionID: sessionID,
	})
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
// @Description  Returns the active member and organization for the authenticated session.
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

func staffSessionID(r *http.Request) string {
	if session, ok := middleware.SessionFromContext(r.Context()); ok {
		return session.SessionID
	}
	return platform.BearerToken(r)
}

func validateEmail(email string) []platform.FieldError {
	email = strings.TrimSpace(email)
	if email == "" {
		return []platform.FieldError{{Field: "email", Message: "is required"}}
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return []platform.FieldError{{Field: "email", Message: "must be a valid email address"}}
	}
	return nil
}

func validateOTPCode(code string) []platform.FieldError {
	code = strings.TrimSpace(code)
	if code == "" {
		return []platform.FieldError{{Field: "code", Message: "is required"}}
	}
	if len(code) != 6 {
		return []platform.FieldError{{Field: "code", Message: "must be 6 digits"}}
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			return []platform.FieldError{{Field: "code", Message: "must be 6 digits"}}
		}
	}
	return nil
}

func validateCreateOrganization(name, slug string) []platform.FieldError {
	name = strings.TrimSpace(name)
	slug = strings.ToLower(strings.TrimSpace(slug))
	var fields []platform.FieldError
	if name == "" {
		fields = append(fields, platform.FieldError{Field: "name", Message: "is required"})
	}
	if slug == "" {
		fields = append(fields, platform.FieldError{Field: "slug", Message: "is required"})
	} else if !slugPattern.MatchString(slug) {
		fields = append(fields, platform.FieldError{Field: "slug", Message: "must be URL-safe (lowercase letters, numbers, and hyphens)"})
	}
	return fields
}

func validateMemberID(memberID string) []platform.FieldError {
	memberID = strings.TrimSpace(memberID)
	if memberID == "" {
		return []platform.FieldError{{Field: "member_id", Message: "is required"}}
	}
	return nil
}
