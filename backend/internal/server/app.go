package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	httpSwagger "github.com/swaggo/http-swagger"

	identityhandler "github.com/peter/ticket_pos/backend/internal/identity/handler"
	identityrepo "github.com/peter/ticket_pos/backend/internal/identity/repository"
	identitysvc "github.com/peter/ticket_pos/backend/internal/identity/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/migrate"
)

// App holds wired application dependencies.
type App struct {
	Config          platform.Config
	Logger          *slog.Logger
	DB              *platform.DB
	EmailSender     platform.EmailSender
	IdentityRepo    *identityrepo.Repository
	IdentityService *identitysvc.Service
	IdentityHandler *identityhandler.Handler
}

// Option customizes application wiring (tests and local overrides).
type Option func(*appOptions)

type appOptions struct {
	emailSender platform.EmailSender
	clock       func() time.Time
}

// WithEmailSender overrides the configured email sender.
func WithEmailSender(sender platform.EmailSender) Option {
	return func(o *appOptions) {
		o.emailSender = sender
	}
}

// WithClock overrides the identity service clock (tests).
func WithClock(now func() time.Time) Option {
	return func(o *appOptions) {
		o.clock = now
	}
}

// NewApp constructs all application dependencies from configuration.
func NewApp(ctx context.Context, cfg platform.Config, opts ...Option) (*App, error) {
	var options appOptions
	for _, opt := range opts {
		opt(&options)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	db, err := platform.OpenDB(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}

	if cfg.RunMigrations {
		if err := migrate.Up(ctx, db.Pool); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("migrations: %w", err)
		}
	}

	platformLogger := platform.NewSlogLogger(logger)
	emailSender := options.emailSender
	if emailSender == nil {
		emailSender = newEmailSender(cfg, platformLogger)
	}

	identityRepo := identityrepo.New(db)
	identityService := identitysvc.New(identityRepo, emailSender, platformLogger)
	if options.clock != nil {
		identityService = identityService.WithClock(options.clock)
	}
	identityHandler := identityhandler.New(identityService)

	return &App{
		Config:          cfg,
		Logger:          logger,
		DB:              db,
		EmailSender:     emailSender,
		IdentityRepo:    identityRepo,
		IdentityService: identityService,
		IdentityHandler: identityHandler,
	}, nil
}

// Close releases resources held by the application.
func (a *App) Close() error {
	return a.DB.Close()
}

func newEmailSender(cfg platform.Config, logger platform.Logger) platform.EmailSender {
	// Production email provider is TBD; log OTP codes until a provider is configured.
	_ = cfg
	return &platform.LoggingEmailSender{Logger: logger}
}

// SwaggerHandler returns the Swagger UI handler.
func SwaggerHandler() http.Handler {
	return httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json"))
}
