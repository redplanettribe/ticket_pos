package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	httpSwagger "github.com/swaggo/http-swagger"

	cataloghandler "github.com/peter/ticket_pos/backend/internal/catalog/handler"
	catalogrepo "github.com/peter/ticket_pos/backend/internal/catalog/repository"
	catalogsvc "github.com/peter/ticket_pos/backend/internal/catalog/service"
	identityhandler "github.com/peter/ticket_pos/backend/internal/identity/handler"
	identityrepo "github.com/peter/ticket_pos/backend/internal/identity/repository"
	identitysvc "github.com/peter/ticket_pos/backend/internal/identity/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/migrate"
	"github.com/peter/ticket_pos/backend/internal/platform/storage"
	saleshandler "github.com/peter/ticket_pos/backend/internal/sales/handler"
	salesrepo "github.com/peter/ticket_pos/backend/internal/sales/repository"
	salessvc "github.com/peter/ticket_pos/backend/internal/sales/service"
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
	CatalogRepo     *catalogrepo.Repository
	CatalogService  *catalogsvc.Service
	CatalogHandler  *cataloghandler.Handler
	SalesRepo       *salesrepo.Repository
	SalesService    *salessvc.Service
	SalesHandler    *saleshandler.Handler
}

// Option customizes application wiring (tests and local overrides).
type Option func(*appOptions)

type appOptions struct {
	emailSender   platform.EmailSender
	clock         func() time.Time
	objectStorage storage.ObjectStorage
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

// WithObjectStorage overrides object storage (tests).
func WithObjectStorage(s storage.ObjectStorage) Option {
	return func(o *appOptions) {
		o.objectStorage = s
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
		if err := migrate.Up(ctx, db.Pool, cfg.AppEnv != "production"); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("migrations: %w", err)
		}
	}

	platformLogger := platform.NewSlogLogger(logger)
	emailSender := options.emailSender
	if emailSender == nil {
		emailSender = newEmailSender(cfg, platformLogger)
	}

	objectStorage := options.objectStorage
	if objectStorage == nil {
		objectStorage, err = newObjectStorage(cfg)
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("object storage: %w", err)
		}
	}

	identityRepo := identityrepo.New(db)
	catalogRepo := catalogrepo.New(db)
	identityService := identitysvc.New(identityRepo, catalogRepo, objectStorage, emailSender, platformLogger)
	if options.clock != nil {
		identityService = identityService.WithClock(options.clock)
	}
	identityHandler := identityhandler.New(identityService)

	catalogService := catalogsvc.New(catalogRepo, objectStorage)
	if options.clock != nil {
		catalogService = catalogService.WithClock(options.clock)
	}
	catalogHandler := cataloghandler.New(catalogService)

	salesRepo := salesrepo.New(db)
	salesService := salessvc.New(salesRepo, emailSender)
	if options.clock != nil {
		salesService = salesService.WithClock(options.clock)
	}
	salesHandler := saleshandler.New(salesService)

	return &App{
		Config:          cfg,
		Logger:          logger,
		DB:              db,
		EmailSender:     emailSender,
		IdentityRepo:    identityRepo,
		IdentityService: identityService,
		IdentityHandler: identityHandler,
		CatalogRepo:     catalogRepo,
		CatalogService:  catalogService,
		CatalogHandler:  catalogHandler,
		SalesRepo:       salesRepo,
		SalesService:    salesService,
		SalesHandler:    salesHandler,
	}, nil
}

// Close releases resources held by the application.
func (a *App) Close() error {
	return a.DB.Close()
}

func newEmailSender(cfg platform.Config, logger platform.Logger) platform.EmailSender {
	// The presence of a Resend key is the switch: prod injects it from Secret
	// Manager, local dev and tests set none and keep logging OTP codes to the
	// console. Mirrors how newObjectStorage gates on S3_ENDPOINT. See ADR 0009.
	if cfg.ResendAPIKey != "" {
		logger.Info("email sender: resend", "from", cfg.EmailFrom)
		return platform.NewResendEmailSender(cfg.ResendAPIKey, cfg.EmailFrom, logger)
	}
	logger.Info("email sender: logging (no RESEND_API_KEY set)")
	return &platform.LoggingEmailSender{Logger: logger}
}

func newObjectStorage(cfg platform.Config) (storage.ObjectStorage, error) {
	if strings.TrimSpace(cfg.S3Endpoint) == "" {
		return nil, nil
	}
	return storage.NewS3Storage(storage.S3Config{
		Endpoint:  cfg.S3Endpoint,
		PublicURL: cfg.S3PublicURL,
		AccessKey: cfg.S3AccessKey,
		SecretKey: cfg.S3SecretKey,
		Bucket:    cfg.S3Bucket,
		Region:    cfg.S3Region,
	})
}

// SwaggerHandler returns the Swagger UI handler.
func SwaggerHandler() http.Handler {
	return httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json"))
}
