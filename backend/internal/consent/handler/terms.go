package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// GetTerms serves the current Terms Version: the full Términos y Condiciones
// Generales and the acceptance checkbox label.
//
// Public and unauthenticated, like GetPrivacyPolicy and for its reason: a
// contract that could only be read after signing up would be the wrong way
// round. The {locale} in the address keeps the shape of the policy endpoint,
// but the answer is the Spanish document WHATEVER it says — the Terms are
// published in Spanish only and the Spanish text legally prevails (§37, ADR
// 0066), so an unpublished locale gets the one operative text, never a 404.
//
// It is the only place the Terms text exists on the wire: the Storefront terms
// page renders from this, the capture surfaces (#536, #538) render their
// checkbox label from this, and the Staff app links here rather than hosting a
// copy.
//
// @Summary      Current Términos y Condiciones
// @Description  Serves the Terms Version currently in effect: the version label, its effective date, the SHA-256 fingerprint of the edition, the full Términos y Condiciones Generales (`body_markdown`) and the mandatory acceptance checkbox label (`acceptance_label`). All text is markdown and all of it is what the fingerprint covers. The document is published in Spanish only — the single legally prevailing text (§37) — and is served for ANY requested locale rather than falling back or 404ing; the `locale` field names the language of the text ("es"), not the language asked for. Public and unauthenticated.
// @Tags         public
// @Produce      json
// @Param        locale  path  string  true  "Locale the terms are asked for in; the Spanish text is served regardless"
// @Success      200  {object}  openapi.EnvelopeTerms
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/public/terms/{locale} [get]
func (h *Handler) GetTerms(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	view, err := h.svc.CurrentTerms(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, view)
}
