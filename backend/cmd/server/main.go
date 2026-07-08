package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	httpSwagger "github.com/swaggo/http-swagger"

	identityhandler "github.com/peter/ticket_pos/backend/internal/identity/handler"
	identitymiddleware "github.com/peter/ticket_pos/backend/internal/identity/middleware"
	identityrepo "github.com/peter/ticket_pos/backend/internal/identity/repository"
	identitysvc "github.com/peter/ticket_pos/backend/internal/identity/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/migrate"

	_ "github.com/peter/ticket_pos/backend/docs"
)

// @title           Ticket POS API
// @version         1.0
// @description     Ticket POS backend API.
// @BasePath        /
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx := context.Background()
	db, err := platform.OpenDBFromEnv(ctx)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	if shouldRunMigrations() {
		if err := migrate.Up(ctx, db.Pool); err != nil {
			log.Fatalf("migrations: %v", err)
		}
	}

	platformLogger := platform.NewSlogLogger(logger)
	emailSender := &platform.LoggingEmailSender{Logger: platformLogger}

	identityRepo := identityrepo.New(db)
	identityService := identitysvc.New(identityRepo, emailSender, platformLogger)
	identityHandler := identityhandler.New(identityService)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler)
	mux.Handle("GET /swagger/", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
	))

	mux.HandleFunc("POST /api/v1/auth/otp/request", identityHandler.RequestOTP)
	mux.HandleFunc("POST /api/v1/auth/otp/verify", identityHandler.VerifyOTP)
	mux.HandleFunc("GET /api/v1/auth/session", identityHandler.GetSession)
	mux.HandleFunc("POST /api/v1/auth/logout", identityHandler.Logout)
	mux.HandleFunc("POST /api/v1/staff/organizations", identityHandler.CreateOrganization)
	mux.HandleFunc("GET /api/v1/staff/memberships", identityHandler.ListMemberships)
	mux.HandleFunc("POST /api/v1/staff/session/organization", identityHandler.SelectOrganization)
	mux.Handle("GET /api/v1/staff/me",
		identitymiddleware.SessionAuth(identityService)(
			identitymiddleware.RequireActiveMember(
				http.HandlerFunc(identityHandler.GetStaffMe),
			),
		),
	)

	handler := platform.RecoverMiddleware(logger,
		platform.LoggingMiddleware(logger,
			platform.RequestIDMiddleware(mux),
		),
	)

	addr := ":8080"
	logger.Info("listening", "addr", addr, "swagger", "http://localhost"+addr+"/swagger/index.html")
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func shouldRunMigrations() bool {
	if os.Getenv("RUN_MIGRATIONS") == "false" {
		return false
	}
	if os.Getenv("APP_ENV") == "production" {
		return os.Getenv("RUN_MIGRATIONS") == "true"
	}
	return true
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
