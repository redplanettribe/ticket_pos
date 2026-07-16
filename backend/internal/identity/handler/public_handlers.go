package handler

import (
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// GetPublicOrganization returns the unauthenticated organization profile by slug.
func (h *Handler) GetPublicOrganization(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	slug := strings.TrimSpace(r.PathValue("slug"))
	if slug == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "slug", Message: "is required"},
		})
		return
	}

	org, err := h.svc.GetPublicOrganization(r.Context(), slug)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, org)
}
