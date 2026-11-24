package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// FillEmailRenditions runs the rendition fill (spec #870, #876, #877): every
// Event original and every Organization Logo whose email rendition is null is
// rendered. The Logos are identity's, asked for through the catalog service.
//
// INTERNAL SERVICE-TO-SERVICE, on the terms every route in the internal
// namespace keeps: Cloud Run IAM authenticates the caller by Google-signed
// OIDC ID token before the container is reached (ADR 0008), and nothing here
// reads an application credential. Unlike its neighbours it is no scheduled
// job: an operator runs it by hand, once after the deploy that introduced
// renditions and again whenever its last answer said it was not done.
//
// There is nothing for the caller to aim: no body, no path parameter. WHICH
// Events and Logos are rendered is a property of the database, every original
// whose rendition is null. Safe to call at any time and repeatedly: what is
// rendered is skipped, so a second run after a clean one renders nothing.
//
// @Summary      Fill the missing email renditions
// @Description  Renders every Event original and every Organization Logo whose email rendition is null, across every Organization: an Event's wide rendition (the Cover Image as a JPEG 1200px wide) and square one (a 128px JPEG from the Thumbnail, or from the Cover Image cropped to its centre), and an Organization's Logo rendition (a PNG fitted within 96px square, its transparency kept). Each rendition is best-effort: one that cannot be produced is logged, stays null and is counted as failed, and the next run tries it again. A run stops starting Events and Logos after four minutes, inside the request timeout, and says so with complete=false; run it again until complete is true and remaining is zero, or remaining holds only originals that keep failing (their causes are in the log). Idempotent: a second run after a clean one renders nothing. Internal service-to-service only: Cloud Run IAM authenticates the caller by Google-signed OIDC ID token before the request reaches the API (ADR 0008). Run by hand, not on a schedule. The response names no Event and no Organization.
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
