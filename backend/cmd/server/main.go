package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"time"

	// Embed the IANA timezone database into the binary so time.LoadLocation
	// works on minimal runtime images (alpine here ships no tzdata package),
	// which event timezone validation relies on.
	_ "time/tzdata"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/server"

	_ "github.com/peter/ticket_pos/backend/docs"
)

// @title           Ticket POS API
// @version         1.0
// @description     Ticket POS backend API.
// @description     JSON endpoints use the standard envelope with data, error, and request_id.
// @description     /health is the only endpoint that returns a bare JSON object.
// @servers         url=http://localhost:64080 description=Local development
// @tag.name        public
// @tag.description Public routes for the Storefront (no auth)
// @tag.name        auth
// @tag.description Staff email OTP authentication and session management
// @tag.name        staff
// @tag.description Staff routes for catalog, sales, and organization management
// @tag.name        customer
// @tag.description Customer email OTP sign-in, Customer Session, and the Customer Area (Storefront)
// @securitydefinitions.apikey BearerAuth
// @in              header
// @name            Authorization
// @description     Staff session token as Bearer <session_id>. The Staff BFF also accepts an httpOnly cookie.
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
