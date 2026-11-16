package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Embed the IANA timezone database into the binary so time.LoadLocation
	// works on minimal runtime images (alpine here ships no tzdata package),
	// which event timezone validation relies on.
	_ "time/tzdata"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/reporter"
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
	// The reporter comes first, from configuration of its own, so that every
	// failure after it, LoadConfig's included, is reported and flushed before
	// the process exits (ADR 0079). With no SENTRY_DSN it is the no-op.
	reporterLogger := platform.NewSlogLogger(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	errorReporter := reporter.New(platform.LoadReporterConfig(), reporterLogger)

	// Cloud Run sends SIGTERM and allows ten seconds before SIGKILL.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	if err := run(ctx, errorReporter); err != nil {
		platform.ReportStartupFailure(errorReporter, err)
		log.Fatal(err)
	}
}

// run starts the API and serves until ctx ends, then shuts down gracefully,
// flushing the reporter. An error is a failure to start or to keep serving.
func run(ctx context.Context, errorReporter platform.Reporter) error {
	cfg, err := platform.LoadConfig()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	app, err := server.NewApp(ctx, cfg, server.WithReporter(errorReporter))
	if err != nil {
		return fmt.Errorf("app: %w", err)
	}
	defer app.Close()
	slog.SetDefault(app.Logger)

	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	app.Logger.Info("listening", "addr", cfg.HTTPAddr, "swagger", "http://localhost"+cfg.HTTPAddr+"/swagger/index.html")
	httpServer := &http.Server{
		Handler:           server.NewHandler(app),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return platform.Serve(ctx, httpServer, listener, errorReporter, platform.NewSlogLogger(app.Logger))
}
