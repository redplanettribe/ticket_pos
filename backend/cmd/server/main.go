package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/server"

	_ "github.com/peter/ticket_pos/backend/docs"
)

// @title           Ticket POS API
// @version         1.0
// @description     Ticket POS backend API.
// @BasePath        /
func main() {
	cfg, err := platform.LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx := context.Background()
	app, err := server.NewApp(ctx, cfg)
	if err != nil {
		log.Fatalf("app: %v", err)
	}
	defer app.Close()
	slog.SetDefault(app.Logger)

	mux := http.NewServeMux()
	server.RegisterRoutes(mux, app)

	handler := platform.RecoverMiddleware(app.Logger,
		platform.LoggingMiddleware(app.Logger,
			platform.RequestIDMiddleware(mux),
		),
	)

	app.Logger.Info("listening", "addr", cfg.HTTPAddr, "swagger", "http://localhost"+cfg.HTTPAddr+"/swagger/index.html")
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
