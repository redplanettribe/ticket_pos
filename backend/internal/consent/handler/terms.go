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
// round. The {locale} in the address is answered strictly, also like the
// policy's: the Terms are published in both Locales the Storefront serves, and
// a third language is a 404 rather than a document nobody asked for. Which
// language a reader is shown is not which text binds them — the Spanish
// prevails over any translation (§37, ADR 0066) and the English artifact says
// so in its own first line.
//
// It is the only place the Terms text exists on the wire: the Storefront terms
// page renders from this, the capture surfaces (#536, #538) render their
// checkbox label from this, and the Staff app links here rather than hosting a
// copy.
//
// @Summary      Current Términos y Condiciones
// @Description  Serves the Terms Version currently in effect, rendered in the requested Locale: the version label, its effective date, the SHA-256 fingerprint of the edition, the full Términos y Condiciones Generales (`body_markdown`) and the mandatory acceptance checkbox label (`acceptance_label`). An edition that asks for an Adulthood Declaration also carries that box's own label (`adulthood_declaration_label`); the field is ABSENT when the edition in effect does not ask, and a client draws the second box only when it arrives. All text is markdown, and all of it in EVERY published language is what the fingerprint covers — one edition, one hash, whichever language a person read before ticking. The Spanish text is the legally prevailing one (§37); the English one is a courtesy translation that says so in its own first line. The Locale is a path parameter and is answered strictly — a language the Terms are not published in is a 404, never a silent fallback. Public and unauthenticated.
// @Tags         public
// @Produce      json
// @Param        locale  path  string  true  "Locale the terms are read in"  Enums(en, es)
// @Success      200  {object}  openapi.EnvelopeTerms
// @Failure      404  {object}  platform.Envelope
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/public/terms/{locale} [get]
func (h *Handler) GetTerms(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	view, err := h.svc.CurrentTerms(r.Context(), r.PathValue("locale"))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	_ = platform.WriteSuccess(w, reqID, http.StatusOK, view)
}
