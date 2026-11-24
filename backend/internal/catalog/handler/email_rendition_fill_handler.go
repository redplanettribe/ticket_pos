package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// FillEmailRenditions runs the rendition fill (spec #870, #876): every Event
// original whose email rendition is null is rendered.
//
// INTERNAL SERVICE-TO-SERVICE, on the terms every route in the internal
// namespace keeps: Cloud Run IAM authenticates the caller by Google-signed
// OIDC ID token before the container is reached (ADR 0008), and nothing here
// reads an application credential. Unlike its neighbours it is no scheduled
// job: an operator runs it by hand, once after the deploy that introduced
// renditions and again whenever its last answer said it was not done.
//
// There is nothing for the caller to aim: no body, no path parameter. WHICH
// Events are rendered is a property of the database, every original whose
// rendition is null. Safe to call at any time and repeatedly: what is
// rendered is skipped, so a second run after a clean one renders nothing.
//
// @Summary      Fill the missing email renditions
// @Description  Renders every Event original whose email rendition is null, across every Organization: the wide rendition (the Cover Image as a JPEG 1200px wide) and the square one (a 128px JPEG from the Thumbnail, or from the Cover Image cropped to its centre). Each rendition is best-effort: one that cannot be produced is logged, stays null and is counted as failed, and the next run tries it again. A run stops starting Events after four minutes, inside the request timeout, and says so with complete=false; run it again until complete is true and remaining is zero, or remaining holds only originals that keep failing (their causes are in the log). Idempotent: a second run after a clean one renders nothing. Internal service-to-service only: Cloud Run IAM authenticates the caller by Google-signed OIDC ID token before the request reaches the API (ADR 0008). Run by hand, not on a schedule. The response names no Event.
// @Tags         internal
// @Produce      json
// @Success      200  {object}  openapi.EnvelopeEmailRenditionFill
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/internal/email-renditions/fill [post]
func (h *Handler) FillEmailRenditions(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	result, err := h.svc.FillEmailRenditions(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}
