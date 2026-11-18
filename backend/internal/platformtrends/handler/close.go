package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// CloseDays runs the daily close: it freezes every closed platform day that
// has no activity totals yet (#815, ADR 0080), and deletes the Organization
// markers older than thirteen months (#816).
//
// INTERNAL SERVICE-TO-SERVICE, on the terms every scheduled route shares:
// Cloud Run IAM authenticates Cloud Scheduler's OIDC token before the
// container is reached (ADR 0008), so the route carries no application
// middleware and reads no session. It takes no body and no parameter: the
// days it closes come from the service clock, never from the caller.
//
// Safe to call by hand at any time and repeatedly. A day already frozen is
// left as it is, so a second call freezes nothing and says so.
//
// @Summary      Run the daily close
// @Description  Freezes the activity of every closed platform day (America/Guayaquil) that has no totals yet, oldest first, from the first day an Organization was active through yesterday: the Organizations active that day, and the different Organizations active across the 7 and the 30 days ending that day. An Organization is active on a day a Ticket Sale was recorded for it, by the instant the sale reached the platform and not the instant it says it was sold, on any Sales Channel, reversed or not, and on a day a Member worked in it through a Staff Session; one that did both is counted once. A day nobody was active on is frozen as zeros. A frozen day is never rewritten, so the run is idempotent, and a missed night is frozen by the next run. The first day frozen with a Member's work in it is written down as the day exact recording of Active Organizations began; days frozen before it hold the sale half alone. The run then deletes the Organization markers older than thirteen months, which changes no frozen total. Platform Trends reads Active Organizations from these totals alone. Internal service-to-service only: Cloud Run IAM authenticates the caller by Google-signed OIDC ID token before the request reaches the API (ADR 0008), and no Customer Session or staff token reaches it. The days cannot be named by the caller; they are taken from the clock. The response states how many days the run froze, the first and last of them, the day it closed through and how many markers it deleted; it names no Organization.
// @Tags         internal
// @Produce      json
// @Success      200  {object}  openapi.EnvelopeDailyClose
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/internal/platform-trends/close [post]
func (h *Handler) CloseDays(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	result, err := h.svc.CloseDays(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}
