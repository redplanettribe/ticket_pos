package server

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	httpSwagger "github.com/swaggo/http-swagger"

	affiliateshandler "github.com/peter/ticket_pos/backend/internal/affiliates/handler"
	affiliatesrepo "github.com/peter/ticket_pos/backend/internal/affiliates/repository"
	affiliatessvc "github.com/peter/ticket_pos/backend/internal/affiliates/service"
	cataloghandler "github.com/peter/ticket_pos/backend/internal/catalog/handler"
	catalogrepo "github.com/peter/ticket_pos/backend/internal/catalog/repository"
	catalogsvc "github.com/peter/ticket_pos/backend/internal/catalog/service"
	customershandler "github.com/peter/ticket_pos/backend/internal/customers/handler"
	customersrepo "github.com/peter/ticket_pos/backend/internal/customers/repository"
	customerssvc "github.com/peter/ticket_pos/backend/internal/customers/service"
	identityhandler "github.com/peter/ticket_pos/backend/internal/identity/handler"
	identityrepo "github.com/peter/ticket_pos/backend/internal/identity/repository"
	identitysvc "github.com/peter/ticket_pos/backend/internal/identity/service"
	operatorhandler "github.com/peter/ticket_pos/backend/internal/operator/handler"
	operatorsvc "github.com/peter/ticket_pos/backend/internal/operator/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/googleauth"
	"github.com/peter/ticket_pos/backend/internal/platform/migrate"
	"github.com/peter/ticket_pos/backend/internal/platform/otp"
	"github.com/peter/ticket_pos/backend/internal/platform/storage"
	"github.com/peter/ticket_pos/backend/internal/sales"
	saleshandler "github.com/peter/ticket_pos/backend/internal/sales/handler"
	salesrepo "github.com/peter/ticket_pos/backend/internal/sales/repository"
	salessvc "github.com/peter/ticket_pos/backend/internal/sales/service"
)

// App holds wired application dependencies.
type App struct {
	Config            platform.Config
	Logger            *slog.Logger
	DB                *platform.DB
	EmailSender       platform.EmailSender
	OTPService        *otp.Service
	IdentityRepo      *identityrepo.Repository
	IdentityService   *identitysvc.Service
	IdentityHandler   *identityhandler.Handler
	AffiliatesRepo    *affiliatesrepo.Repository
	AffiliatesService *affiliatessvc.Service
	AffiliatesHandler *affiliateshandler.Handler
	CatalogRepo       *catalogrepo.Repository
	CatalogService    *catalogsvc.Service
	CatalogHandler    *cataloghandler.Handler
	SalesRepo         *salesrepo.Repository
	SalesService      *salessvc.Service
	SalesHandler      *saleshandler.Handler
	CustomersRepo     *customersrepo.Repository
	CustomersService  *customerssvc.Service
	CustomersHandler  *customershandler.Handler
	// The operator surface owns no repository: it composes the three modules
	// that own the data it shows (ADR 0015).
	OperatorService *operatorsvc.Service
	OperatorHandler *operatorhandler.Handler
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

	// One OTP service is shared by every consumer; each names its own purpose
	// when issuing and verifying, so the challenges never mix.
	//
	// It also carries the global outbound ceiling, which spans both purposes:
	// it exists to protect one shared sending domain, not to be fair between
	// surfaces (ADR 0009, PRD decision 12).
	otpService := otp.New(otp.NewRepository(db), emailSender, platformLogger).
		WithGlobalCeiling(cfg.OTPGlobalCeiling)
	platformLogger.Info("otp global ceiling", "sends_per_window", otpService.GlobalCeiling())

	// One Google OAuth client per surface, and they are never interchanged: Google
	// binds an authorization code to the client that requested it, so a code
	// obtained on the Storefront cannot be redeemed with the Staff client's
	// credentials and vice versa. Cross-surface isolation is a property of these
	// two sets of secrets rather than of any check below (ADR 0011).
	googleTokenEndpoint := cfg.Google.TokenEndpoint
	if strings.TrimSpace(googleTokenEndpoint) == "" {
		googleTokenEndpoint = platform.GoogleTokenEndpoint
	}
	storefrontGoogle := googleauth.New(googleauth.Credentials{
		ClientID:     cfg.Google.Storefront.ClientID,
		ClientSecret: cfg.Google.Storefront.ClientSecret,
	}, googleTokenEndpoint, platformLogger)
	staffGoogle := googleauth.New(googleauth.Credentials{
		ClientID:     cfg.Google.Staff.ClientID,
		ClientSecret: cfg.Google.Staff.ClientSecret,
	}, googleTokenEndpoint, platformLogger)
	if googleTokenEndpoint != platform.GoogleTokenEndpoint {
		// Only a non-production deployment can reach this; LoadConfig refuses the
		// override outright in production. Said out loud anyway, because "which
		// issuer does this process trust" is not a thing to have to guess at.
		platformLogger.Warn("google sign-in: token endpoint overridden", "endpoint", googleTokenEndpoint)
	}

	identityRepo := identityrepo.New(db)
	catalogRepo := catalogrepo.New(db)
	identityService := identitysvc.New(identityRepo, catalogRepo, objectStorage, otpService, platformLogger, staffGoogle)
	if options.clock != nil {
		identityService = identityService.WithClock(options.clock)
	}
	identityHandler := identityhandler.New(identityService)

	feeRates := sales.FeeRates{
		FeeBasisPoints:    cfg.Fees.FeeBasisPoints,
		FeeIVABasisPoints: cfg.Fees.FeeIVABasisPoints,
	}

	confirmationLinkSecret, err := confirmationLinkSecret(cfg, platformLogger)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	// A Confirmation Link is only as useful as the origin it points at. Losing
	// the link entirely would be worse than a wrong host, so this warns rather
	// than refuses — but it must not pass silently either.
	if cfg.AppEnv == "production" && cfg.StorefrontBaseURL == platform.DevStorefrontBaseURL {
		platformLogger.Warn("storefront base url: falling back to the development origin (no STOREFRONT_BASE_URL set); Confirmation Links will point at localhost")
	}

	customersRepo := customersrepo.New(db)
	customersService := customerssvc.New(customersRepo, otpService, platformLogger, customerssvc.ConfirmationLinkConfig{
		Secret:            confirmationLinkSecret,
		StorefrontBaseURL: cfg.StorefrontBaseURL,
	}, storefrontGoogle).WithObjectStorage(objectStorage)
	if options.clock != nil {
		customersService = customersService.WithClock(options.clock)
	}
	customersHandler := customershandler.New(customersService)

	salesRepo := salesrepo.New(db)
	paymentProvider := newPaymentProvider(cfg, platformLogger)
	// Whether a Ticket Sale's Payment can actually be undone is a property of
	// whoever settled it, and both sides of Customer-initiated Sale Reversal must
	// read it from the same place: the Customer Area to decide whether to offer
	// Undo, the reversal endpoint to decide whether to go ahead (ADR 0018). One
	// value, handed to both, is what keeps the offer honest.
	customersService = customersService.WithPaymentReversal(platform.NewPaymentReversal(paymentProvider))
	salesService := salessvc.New(salesRepo, customersService, emailSender, paymentProvider, cfg.StorefrontBaseURL, feeRates, platformLogger)
	if options.clock != nil {
		salesService = salesService.WithClock(options.clock)
	}
	// A submitted Payout Request emails the operator allowlist, and the allowlist
	// is identity's to read: presence on it is the whole of operator authority
	// (ADR 0015), so who is notified and who is authorised come from one place
	// (#179, ADR 0026). Identity is built above, so this could be a constructor
	// argument; it is a knot tied afterwards because it serves one notice, and a
	// constructor that grew a parameter per email would stop being readable.
	salesService = salesService.WithPlatformOperators(identityService)
	salesHandler := saleshandler.New(salesService)

	// Catalog is built AFTER sales because the public Event page reports a
	// signed-in Customer their own holdings per Ticket Type, and that count is
	// sales' rule to answer (ADR 0025, #168). The dependency runs one way only —
	// sales knows nothing of catalog's service — so this is an ordinary
	// constructor argument rather than another knot tied afterwards.
	catalogService := catalogsvc.New(catalogRepo, objectStorage, feeRates, salesService, platformLogger)
	if options.clock != nil {
		catalogService = catalogService.WithClock(options.clock)
	}
	catalogHandler := cataloghandler.New(catalogService)

	// The opportunistic drain (ADR 0024): a Customer loading their Area makes the
	// platform ask the Payment Provider again about their own stuck reversal.
	// Wired here rather than at construction because sales is built after
	// customers and depends on it — the two modules point at each other, and this
	// is the direction that has to be tied afterwards.
	customersService = customersService.WithReversalRequests(salesService)

	// Affiliate Links hang off an Event and point at the Storefront, so the
	// module needs the Storefront origin for the same reason Confirmation Links
	// do: the copyable URL is derived at read time, never stored.
	affiliatesRepo := affiliatesrepo.New(db)
	affiliatesService := affiliatessvc.New(affiliatesRepo, cfg.StorefrontBaseURL, platformLogger)
	affiliatesHandler := affiliateshandler.New(affiliatesService)
	// Affiliate Attribution's one crossing between the two modules: sales hands
	// the code a checkout arrived with to affiliates and stores the link id it
	// gets back. Wired here rather than at construction because affiliates is
	// built after sales, and because it is additive — an unwired resolver simply
	// attributes nothing (#146).
	salesService = salesService.WithAffiliateLinks(affiliatesService)

	// The Operator Dashboard is composed from the modules that own its data:
	// identity for Organizations, catalog for Events, sales for money. It is
	// wired last because it depends on all three and none of them on it.
	operatorService := operatorsvc.New(identityService, catalogService, salesService)
	operatorHandler := operatorhandler.New(operatorService)

	return &App{
		Config:            cfg,
		Logger:            logger,
		DB:                db,
		EmailSender:       emailSender,
		OTPService:        otpService,
		IdentityRepo:      identityRepo,
		IdentityService:   identityService,
		IdentityHandler:   identityHandler,
		AffiliatesRepo:    affiliatesRepo,
		AffiliatesService: affiliatesService,
		AffiliatesHandler: affiliatesHandler,
		CatalogRepo:       catalogRepo,
		CatalogService:    catalogService,
		CatalogHandler:    catalogHandler,
		SalesRepo:         salesRepo,
		SalesService:      salesService,
		SalesHandler:      salesHandler,
		CustomersRepo:     customersRepo,
		CustomersService:  customersService,
		CustomersHandler:  customersHandler,
		OperatorService:   operatorService,
		OperatorHandler:   operatorHandler,
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

// newPaymentProvider selects the Payment Provider by credential presence,
// mirroring newEmailSender (ADR 0009, ADR 0012): PayPhone credentials set means
// the real PayPhone client, unset means the stub whose payment page is a
// Storefront interstitial — so the full checkout works locally with zero setup.
func newPaymentProvider(cfg platform.Config, logger platform.Logger) platform.PaymentProvider {
	if cfg.PayPhone.Configured() {
		if cfg.PayPhone.BaseURL != "" && cfg.PayPhone.BaseURL != platform.PayPhoneBaseURL {
			// Only a non-production deployment can reach this; LoadConfig refuses
			// the override outright in production. Said out loud anyway, because
			// "which server do these payments go to" is not a thing to guess at.
			logger.Warn("payment provider: payphone base url overridden", "base_url", cfg.PayPhone.BaseURL)
		}
		logger.Info("payment provider: payphone", "store_id", cfg.PayPhone.StoreID)
		return platform.NewPayPhoneProvider(cfg.PayPhone.APIToken, cfg.PayPhone.StoreID, cfg.PayPhone.BaseURL, logger)
	}
	logger.Info("payment provider: stub (no PAYPHONE_* credentials set)")
	return platform.NewStubPaymentProvider(cfg.StorefrontBaseURL)
}

// confirmationLinkSecret resolves the key every Confirmation Link is signed
// with.
//
// In production a missing key is a startup failure. This is the only place that
// check lives, deliberately: it belongs to the workload that signs links, not to
// LoadConfig, which cmd/migrate also calls with APP_ENV=production and without
// any signing key. The alternative, signing with a constant baked into the binary, would
// mean anyone with a copy of the source could mint a link to any Ticket Sale.
//
// Everywhere else — local development, the parity stack, tests — an ephemeral
// random key is generated per process and the fact is logged. Links then stop
// working across a restart, which is the correct nuisance: it is visible
// immediately and cannot be mistaken for a shared default.
func confirmationLinkSecret(cfg platform.Config, logger platform.Logger) ([]byte, error) {
	if secret := strings.TrimSpace(cfg.ConfirmationLinkSecret); secret != "" {
		return []byte(secret), nil
	}
	if cfg.AppEnv == "production" {
		return nil, fmt.Errorf("CONFIRMATION_LINK_SECRET is required in production")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate development confirmation link secret: %w", err)
	}
	logger.Warn("confirmation link secret: generated for this process (no CONFIRMATION_LINK_SECRET set); links will not survive a restart")
	return secret, nil
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
