package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

type healthResponse struct {
	Status string `json:"status" example:"ok"`
}

// Health returns service health status.
//
// @Summary      Health check
// @Description  Returns service health status.
// @Tags         public
// @Produce      json
// @Success      200  {object}  healthResponse
// @Router       /health [get]
func Health(w http.ResponseWriter, _ *http.Request) {
	if err := platform.WriteJSON(w, http.StatusOK, healthResponse{Status: "ok"}); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}
