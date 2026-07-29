package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/identity/repository"
	"github.com/peter/ticket_pos/backend/internal/identity/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/storage"
)

type updateOrganizationBody struct {
	Name         string  `json:"name"`
	Currency     *string `json:"currency"`
	LogoImageKey *string `json:"logo_image_key"`
}

type logoUploadURLBody struct {
	ContentType string  `json:"content_type"`
	FileName    *string `json:"file_name"`
}

var supportedCurrencies = map[string]struct{}{
	"USD": {}, "EUR": {}, "GBP": {}, "CAD": {}, "AUD": {}, "JPY": {}, "CHF": {},
	"MXN": {}, "BRL": {}, "NZD": {}, "SEK": {}, "NOK": {}, "DKK": {},
}

type deleteOrganizationBody struct {
	ConfirmationName string `json:"confirmation_name"`
}

type addMemberBody struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type updateMemberRoleBody struct {
	Role string `json:"role"`
}

type upsertAssignmentBody struct {
	Role string `json:"role"`
}

func actorContextFromRequest(r *http.Request) service.ActiveMemberContext {
	member, _ := middleware.ActiveMemberFromContext(r.Context())
	return service.ActiveMemberContext{
		MemberID:       member.MemberID,
		OrganizationID: member.OrganizationID,
		Role:           repository.MemberRole(member.Role),
		Email:          member.Email,
	}
}

// GetOrganization returns the active organization profile.
func (h *Handler) GetOrganization(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	actor := actorContextFromRequest(r)

	org, err := h.svc.GetOrganization(r.Context(), actor)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, org)
}

// UpdateOrganization updates the organization display name.
func (h *Handler) UpdateOrganization(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body updateOrganizationBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	if fields := validateOrganizationName(body.Name); len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	if fields := validateOrganizationCurrency(body.Currency); len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	actor := actorContextFromRequest(r)

	org, err := h.svc.UpdateOrganization(r.Context(), actor, service.UpdateOrganizationInput{
		Name:         body.Name,
		Currency:     body.Currency,
		LogoImageKey: body.LogoImageKey,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, org)
}

// CreateLogoUploadURL returns a presigned URL for uploading the organization logo.
func (h *Handler) CreateLogoUploadURL(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body logoUploadURLBody
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
	if !storage.CoverContentTypeAllowed(contentType) {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "content_type", Code: platform.CodeInvalidImageContentType, Message: "must be image/jpeg, image/png, or image/webp"},
		})
		return
	}

	fileName := ""
	if body.FileName != nil {
		fileName = strings.TrimSpace(*body.FileName)
	}

	actor := actorContextFromRequest(r)

	result, err := h.svc.CreateLogoUploadURL(r.Context(), actor, service.CreateLogoUploadURLInput{
		ContentType: contentType,
		FileName:    fileName,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}

// DeleteOrganization hard-deletes the organization.
func (h *Handler) DeleteOrganization(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body deleteOrganizationBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	if strings.TrimSpace(body.ConfirmationName) == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "confirmation_name", Code: platform.CodeRequired, Message: "is required"},
		})
		return
	}

	actor := actorContextFromRequest(r)

	if err := h.svc.DeleteOrganization(r.Context(), actor, service.DeleteOrganizationInput{
		ConfirmationName: body.ConfirmationName,
	}); err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, map[string]string{
		"message": "Organization deleted.",
	})
}

// ListMembers returns the organization member roster.
func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	actor := actorContextFromRequest(r)

	members, err := h.svc.ListMembers(r.Context(), actor)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, members)
}

// AddMember pre-provisions a member by email.
func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body addMemberBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	if fields := validateAddMember(body.Email, body.Role); len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	actor := actorContextFromRequest(r)

	member, err := h.svc.AddMember(r.Context(), actor, service.AddMemberInput{
		Email: body.Email,
		Role:  body.Role,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusCreated, member)
}

// UpdateMember updates a member's org-level role.
func (h *Handler) UpdateMember(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	memberID := strings.TrimSpace(r.PathValue("memberID"))
	if memberID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "member_id", Code: platform.CodeRequired, Message: "is required"},
		})
		return
	}

	var body updateMemberRoleBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	if fields := validateMemberRole(body.Role); len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	actor := actorContextFromRequest(r)

	member, err := h.svc.UpdateMemberRole(r.Context(), actor, memberID, service.UpdateMemberRoleInput{
		Role: body.Role,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, member)
}

// RemoveMember deletes a member from the organization.
func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	memberID := strings.TrimSpace(r.PathValue("memberID"))
	if memberID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "member_id", Code: platform.CodeRequired, Message: "is required"},
		})
		return
	}

	actor := actorContextFromRequest(r)

	if err := h.svc.RemoveMember(r.Context(), actor, memberID); err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, map[string]string{
		"message": "Member removed.",
	})
}

// RemoveEventAssignment removes a member's assignment from an event.
func (h *Handler) ListEventAssignments(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID := strings.TrimSpace(r.PathValue("eventID"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "event_id", Code: platform.CodeRequired, Message: "is required"},
		})
		return
	}

	actor := actorContextFromRequest(r)

	assignments, err := h.svc.ListEventAssignments(r.Context(), actor, eventID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, assignments)
}

// UpsertEventAssignment assigns a member to an event.
func (h *Handler) UpsertEventAssignment(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID := strings.TrimSpace(r.PathValue("eventID"))
	memberID := strings.TrimSpace(r.PathValue("memberID"))
	if eventID == "" || memberID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "event_id", Code: platform.CodeRequired, Message: "is required"},
		})
		return
	}

	var body upsertAssignmentBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	if fields := validateAssignmentRole(body.Role); len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	actor := actorContextFromRequest(r)

	assignment, err := h.svc.UpsertEventAssignment(r.Context(), actor, eventID, memberID, service.UpsertEventAssignmentInput{
		Role: body.Role,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, assignment)
}

// RemoveEventAssignment removes a member's event assignment.
func (h *Handler) RemoveEventAssignment(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID := strings.TrimSpace(r.PathValue("eventID"))
	memberID := strings.TrimSpace(r.PathValue("memberID"))
	if eventID == "" || memberID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "event_id", Code: platform.CodeRequired, Message: "is required"},
		})
		return
	}

	actor := actorContextFromRequest(r)

	if err := h.svc.RemoveEventAssignment(r.Context(), actor, eventID, memberID); err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, map[string]string{
		"message": "Event assignment removed.",
	})
}

func validateOrganizationName(name string) []platform.FieldError {
	if strings.TrimSpace(name) == "" {
		return []platform.FieldError{{Field: "name", Code: platform.CodeRequired, Message: "is required"}}
	}
	return nil
}

func validateOrganizationCurrency(currency *string) []platform.FieldError {
	if currency == nil {
		return nil
	}
	code := strings.ToUpper(strings.TrimSpace(*currency))
	if code == "" {
		return nil
	}
	if _, ok := supportedCurrencies[code]; !ok {
		return []platform.FieldError{{Field: "currency", Code: platform.CodeInvalidCurrency, Message: "must be a supported ISO 4217 currency code"}}
	}
	return nil
}

func validateAddMember(email, role string) []platform.FieldError {
	var fields []platform.FieldError
	fields = append(fields, validateEmail(email)...)
	fields = append(fields, validateMemberRole(role)...)
	return fields
}

func validateMemberRole(role string) []platform.FieldError {
	switch strings.TrimSpace(role) {
	case "org_admin", "event_owner", "event_staff":
		return nil
	default:
		return []platform.FieldError{{Field: "role", Code: platform.CodeInvalidRole, Message: "must be org_admin, event_owner, or event_staff"}}
	}
}

func validateAssignmentRole(role string) []platform.FieldError {
	switch strings.TrimSpace(role) {
	case "event_owner", "event_staff":
		return nil
	default:
		return []platform.FieldError{{Field: "role", Code: platform.CodeInvalidRole, Message: "must be event_owner or event_staff"}}
	}
}
