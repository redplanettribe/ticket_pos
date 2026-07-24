// Package handler exposes the Customer sign-in and Customer Area HTTP surface.
//
// Every response uses the standard envelope and every route is versioned under
// /api/v1/. Handlers stay thin: validate the request, call the service, map any
// domain error through the platform mapper.
package handler

import (
	"encoding/json"
	"net/http"
	"net/mail"
	"strings"

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
}

type otpVerifyBody struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type verifyOTPResponse struct {
	Session   *service.CustomerSessionView `json:"session"`
	SessionID string                       `json:"session_id"`
}

// RequestOTP sends a one-time passcode to a Customer's email.
//
// @Summary      Request Customer passcode
// @Description  Sends a one-time passcode for Customer sign-in. The response is identical whether or not the email is known, so it does not reveal who the platform's Customers are.
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
	result, err := h.svc.RequestOTP(r.Context(), body.Email, platform.ClientIP(r))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}

// VerifyOTP validates a passcode and issues a Customer Session.
//
// @Summary      Verify Customer passcode
// @Description  Verifies a Customer one-time passcode, marks the Customer verified, and issues a Customer Session.
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

type confirmationLinkBody struct {
	Token string `json:"token"`
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
// @Description  Exchanges the signed token from a Sale Confirmation for a short-lived Customer Session scoped to that one Ticket Sale. Does not mark the Customer verified. If a full Customer Session is presented in Authorization, it is returned unchanged rather than narrowed.
// @Tags         customer
// @Accept       json
// @Produce      json
// @Param        body  body      confirmationLinkBody  true  "Confirmation Link token"
// @Success      200   {object}  openapi.EnvelopeCustomerVerifyOTP
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Router       /api/v1/customer/auth/confirmation-link [post]
func (h *Handler) RedeemConfirmationLink(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body confirmationLinkBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	if strings.TrimSpace(body.Token) == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "token", Message: "is required"}})
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
// @Description  Returns which email the caller is signed in as, and extends the sliding session window.
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
// @Description  Returns the signed-in Customer's Ticket Sales, upcoming and past, across all Organizations. Always scoped by the Customer Session, never by any identifier in the request.
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
