package main

import (
	"log"
	"net/http"

	httpSwagger "github.com/swaggo/http-swagger"

	"github.com/peter/ticket_pos/backend/internal/platform"

	_ "github.com/peter/ticket_pos/backend/docs"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler)
	mux.Handle("GET /swagger/", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
	))

	addr := ":8080"
	log.Printf("listening on %s (swagger UI at http://localhost%s/swagger/index.html)", addr, addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

type healthResponse struct {
	Status string `json:"status" example:"ok"`
}

// healthHandler returns service health status.
//
// @Summary      Health check
// @Description  Returns service health status.
// @Tags         public
// @Produce      json
// @Success      200  {object}  healthResponse
// @Router       /health [get]
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	if err := platform.WriteJSON(w, http.StatusOK, healthResponse{Status: "ok"}); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}
