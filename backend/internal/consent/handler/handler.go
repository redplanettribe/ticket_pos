// Package handler exposes the consent module over HTTP.
package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/consent/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Handler serves the consent module's HTTP surface.
type Handler struct {
	svc *service.Service
}

// New builds the consent Handler.
func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// GetPrivacyPolicy serves the current Policy Version in one language: the full
// Privacy Policy, the Short Notice, and the three consent checkbox labels.
//
// Public and unauthenticated, because a privacy notice that could only be read
// by somebody who had already signed up would be the wrong way round — the
// whole point of it is to be readable BEFORE anyone hands over anything. It
// reveals nothing about anybody: the response is the same bytes for every
// caller in a given language, which is exactly the property the fingerprint
// beside it attests to.
//
// It is the only place the policy text exists on the wire. The Storefront
// renders the page from this, and the capture surfaces in #251 render their
// notice and labels from this, so a deploy can never put one edition on the
// page and another beside the checkbox (service.PolicyView).
//
// @Summary      Current Privacy Policy
// @Description  Serves the Policy Version currently in effect, rendered in the requested Locale: the version label, its effective date, the SHA-256 fingerprint of the edition, the full Privacy Policy (`body_markdown`), the Short Notice shown inline at consent capture moments (`short_notice`), and the three consent checkbox labels (`consent_labels`) — the required Policy Acceptance, the optional Marketing Consent which names the weekly Follow Digest, and the optional Networking Consent which names both audiences. All text is markdown, and all of it is what the fingerprint covers: the hash on the Policy Version is computed over exactly these strings in every published Locale, so a client can prove that what it rendered is what the platform recorded as accepted. Public and unauthenticated. The Locale is a path parameter and is answered strictly — a language the policy is not published in is a 404, never a silent fallback to English.
// @Tags         public
// @Produce      json
// @Param        locale  path  string  true  "Locale the policy is read in"  Enums(en, es)
// @Success      200  {object}  openapi.EnvelopePrivacyPolicy
// @Failure      404  {object}  platform.Envelope
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/public/privacy-policy/{locale} [get]
func (h *Handler) GetPrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	view, err := h.svc.CurrentPolicy(r.Context(), r.PathValue("locale"))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, view)
}
