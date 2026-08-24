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
	consenthandler "github.com/peter/ticket_pos/backend/internal/consent/handler"
	consentrepo "github.com/peter/ticket_pos/backend/internal/consent/repository"
	consentsvc "github.com/peter/ticket_pos/backend/internal/consent/service"
	customershandler "github.com/peter/ticket_pos/backend/internal/customers/handler"
	customersrepo "github.com/peter/ticket_pos/backend/internal/customers/repository"
	customerssvc "github.com/peter/ticket_pos/backend/internal/customers/service"
	digesthandler "github.com/peter/ticket_pos/backend/internal/digest/handler"
	digestrepo "github.com/peter/ticket_pos/backend/internal/digest/repository"
	digestsvc "github.com/peter/ticket_pos/backend/internal/digest/service"
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
	// The Follow Digest pipeline (#220, ADR 0030). It owns two tables nobody else
	// could — the pending-Digest queue and the sent-ledger — and reads Follows,
	// Events and Tags that belong to other modules.
	DigestRepo    *digestrepo.Repository
	DigestService *digestsvc.Service
	DigestHandler *digesthandler.Handler
	// Consent (#250, parent #249): the Privacy Policy, its Policy Versions, and
	// — from #251 — the evidence of what each Customer authorized.
	ConsentRepo    *consentrepo.Repository
	ConsentService *consentsvc.Service
	ConsentHandler *consenthandler.Handler
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

	// Consent (#250, #251, parent #249). It depends on nothing but the database
	// and the Privacy Policy text embedded in this binary, so it is built FIRST
	// among the domain modules — before customers, which depends on it. The
	// direction is the whole design: customers may depend on consent, consent may
	// never depend on customers, and the module that decides whether a Customer
	// Session may be minted must not be the one that owns Customer Sessions.
	consentRepo := consentrepo.New(db)
	consentService := consentsvc.New(consentRepo, platformLogger)
	consentHandler := consenthandler.New(consentService)
	if options.clock != nil {
		// Consent Records are stamped with the server clock, so the harness's
		// fixed clock has to reach it like it reaches every other service that
		// writes a timestamp anybody asserts on.
		consentService = consentService.WithClock(options.clock)
	}

	customersRepo := customersrepo.New(db)
	customersService := customerssvc.New(customersRepo, otpService, platformLogger, customerssvc.ConfirmationLinkConfig{
		Secret:            confirmationLinkSecret,
		StorefrontBaseURL: cfg.StorefrontBaseURL,
	}, storefrontGoogle, consentService).
		WithObjectStorage(objectStorage).
		// The Consent Withdrawal confirmation goes out on the TRANSACTIONAL
		// sender (#267): it is sent to somebody who has just withdrawn Marketing
		// Consent, so the one identity it must never travel on is the Digest's.
		// emailSender is the split sender, which routes only the Digest away.
		WithEmailSender(emailSender)
	if options.clock != nil {
		customersService = customersService.WithClock(options.clock)
	}
	// A Follow is stored against an Organization id, and the Customer names one
	// by the slug they see in the address bar (#217). Turning the one into the
	// other is identity's rule — including that an unknown slug is
	// ORGANIZATION_NOT_FOUND rather than an empty answer — so customers declares
	// the narrow interface and identity satisfies it, as sales does for in-flight
	// Reversal Requests below.
	customersService = customersService.WithOrganizations(identityService)
	customersHandler := customershandler.New(customersService)

	salesRepo := salesrepo.New(db)
	paymentProvider := newPaymentProvider(cfg, platformLogger)
	// Whether a Ticket Sale's Payment can actually be undone is a property of
	// whoever settled it, and both sides of Customer-initiated Sale Reversal must
	// read it from the same place: the Customer Area to decide whether to offer
	// Undo, the reversal endpoint to decide whether to go ahead (ADR 0018). One
	// value, handed to both, is what keeps the offer honest.
	customersService = customersService.WithPaymentReversal(platform.NewPaymentReversal(paymentProvider))
	// Consent is a constructor argument and not a knot tied afterwards (#253): an
	// Online Sale may not complete without Policy Acceptance, and the evidence of
	// it is written inside the transaction that records the sale, so a sales
	// service built without one would be a service that sells tickets and keeps no
	// Consent Records. It is built above, and the dependency runs one way only —
	// consent knows nothing of sales.
	salesService := salessvc.New(salesRepo, customersService, emailSender, paymentProvider, cfg.StorefrontBaseURL, feeRates, consentService, platformLogger)
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
	// And each of the five notices is written in its recipient's Staff Locale,
	// which identity stores against the email address the notice is addressed to
	// (#285, ADR 0041). Same seam, same reason: the language belongs to a person,
	// and who a person is, is identity's rule.
	salesService = salesService.WithStaffLocales(identityService)
	// The Sales Export's per-Ticket sheet and the checkout's answer section, dark
	// unless a deployment has deliberately opened the Ticket Question feature
	// (#314, #311, ADR 0045). It reads the SAME config field catalog's service is
	// handed below, so the two modules cannot disagree about whether the feature
	// is on: a deployment where an Organization can author a question the checkout
	// will not ask — or the reverse — is a deployment collecting or discarding
	// personal data by accident.
	salesService = salesService.WithTicketQuestions(cfg.TicketQuestionsEnabled)
	// And the SECOND flag, for the Holder columns on that same sheet (#330,
	// ADR 0047). Sales is handed it for the export and for nothing else: no
	// checkout, no door sale and no Sale Import consults it, because nothing
	// about assignment may block or delay any of them.
	salesService = salesService.WithTicketAssignment(cfg.TicketAssignmentEnabled)
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
	// The Ticket Question authoring surface, dark unless a deployment has
	// deliberately opened it (#309, ADR 0045). Unset, misspelt or absent leaves
	// it closed.
	catalogService = catalogService.WithTicketQuestions(cfg.TicketQuestionsEnabled)
	// Ticket Assignment, dark unless a deployment has deliberately opened it
	// (#324, parent #322). A SEPARATE CONFIG FIELD read from a SEPARATE
	// environment variable, and never cfg.TicketQuestionsEnabled reused: the two
	// features are separable, and the operational property this second flag
	// exists to buy is that assignment can be killed without taking Ticket
	// Questions dark.
	//
	// The catalog service owns the Ticket and every write behind this flag.
	// Sales is handed the same value above for ONE READ — whether the Sales
	// Export's per-Ticket sheet carries its Holder columns (#330) — and for
	// nothing else: nothing in checkout, the door or a Sale Import touches an
	// assignment, and nothing about assignment may block or delay any of them.
	catalogService = catalogService.WithTicketAssignment(cfg.TicketAssignmentEnabled)
	// The Assignment Link's signing key and the origin its links point at (#325,
	// parent #322, ADR 0046).
	//
	// IT IS HANDED THE SAME DEPLOYMENT SECRET THE CONFIRMATION LINK USES, and
	// derives its own key from it under a purpose label rather than signing with
	// it — see catalog.NewAssignmentLinkSigner. That is what keeps CONTEXT.md's
	// promise that the signed links travelling in one mail flow never open what
	// the others do, without asking every deployment to configure a second
	// secret it could forget and thereby ship a linkless feature (ADR 0046).
	catalogService = catalogService.WithAssignmentLinks(confirmationLinkSecret, cfg.StorefrontBaseURL)
	// The one mail an assignment sends. Handed the same split sender everything
	// else uses, through a one-method seam so nothing in catalog can reach the
	// receipt, the passcode or the Digest — and so the Assignment mail is
	// structurally on the transactional identity (ADR 0030), which matters
	// because its recipient has consented to nothing and could not have.
	catalogService = catalogService.WithAssignmentMail(emailSender)
	// The other mail this flow sends: the one an accepted Holder gets when a
	// Ticket stops being theirs (#327, ADR 0046).
	//
	// THE SAME SPLIT SENDER, THROUGH ITS OWN ONE-METHOD SEAM, so this message is
	// structurally on the transactional identity (ADR 0030) — which matters
	// because a Holder consented to nothing by accepting a ticket, and being told
	// the ticket is gone must not be suppressible by a marketing preference.
	//
	// A separate seam from the Assignment mail's, because the two have opposite
	// risk profiles: that one carries a credential capable of minting an identity
	// and goes to a stranger, this one carries no link at all and goes to
	// somebody who proved their address.
	catalogService = catalogService.WithNoLongerHoldingMail(emailSender)
	// The Question Review's submission notice (#406, ADR 0056): the second use
	// of the organizer-facing channel ADR 0026 opened, handed the same three
	// things the Payout Request's is — the split sender through a one-method
	// seam, the operator allowlist, and the Staff Locale reader — so who is
	// told and in what language come from the same place they do there.
	catalogService = catalogService.WithQuestionReviewNotices(emailSender, identityService, identityService)
	// The Revocation notice (#410, ADR 0056): every Org Admin of the
	// Organization, each in their Mail Locale, on the channel ADR 0026 opened.
	// Identity satisfies the recipients seam — who the Org Admins are and what
	// language each reads — so catalog never learns how staff identity works.
	catalogService = catalogService.WithRevocationMail(emailSender, identityService)
	catalogHandler := cataloghandler.New(catalogService)

	// The Sale Confirmation's one conditional sentence (#315, ADR 0044), tied on
	// HERE rather than passed to either constructor.
	//
	// IT IS THE ONE PLACE THE TWO MODULES POINT AT EACH OTHER, and the knot is
	// tied after both exist because it has to be: sales is built first, since the
	// public Event page asks it about a Customer's holdings, so catalog's service
	// does not exist yet at the moment sales is constructed. A constructor
	// argument would therefore mean reordering the two, which would break the
	// dependency that already runs the other way.
	//
	// The seam is one yes-or-no question wide. Sales learns nothing about what a
	// Ticket Question is or when one is owed, and catalog reads its own feature
	// flag on the far side — so a dark deployment answers false and every receipt
	// renders exactly as it did before this feature existed (ADR 0045).
	salesService = salesService.WithOutstandingAnswers(catalogService)

	// The Answer Reminder sweep's seam (#317, ADR 0044), tied on here for the
	// same reason and at the same moment as the one above it.
	//
	// It points the same way — sales asks catalog — and divides the work on the
	// same line: catalog owns the debt, the rationing and the ledger of who has
	// been written to, and sales owns the mail, because the Confirmation Link it
	// points at, the Mail Locale it is written in and the transactional sender it
	// goes out on are all already here for the Sale Confirmation.
	//
	// The far side reads TICKET_QUESTIONS_ENABLED, so a dark deployment sweeps
	// nothing and mails nobody however often the endpoint is called (ADR 0045) —
	// which is the inner of the two switches this job ships behind. The outer one
	// is Terraform's: the Cloud Scheduler job is created paused.
	salesService = salesService.WithAnswerReminders(catalogService)

	// The Assignment Reminder (#362, ADR 0051) runs on the same shape: the
	// catalog says which Sales still have Tickets nobody holds and keeps the
	// ledger; sales composes and sends to the buyer. The far side reads
	// TICKET_ASSIGNMENT_ENABLED, so a dark deployment sweeps nothing, and the
	// Cloud Scheduler job that drives it is created paused.
	salesService = salesService.WithAssignmentReminders(catalogService)

	// A Sale Reversal takes every Holder on the Sale with it, and they are told
	// (#327, parent #322, ADR 0046). Tied on here for the same reason and at the
	// same moment as the two above it, and pointing the same way: sales knows a
	// Sale was reversed, and catalog owns Tickets, Holders and the message.
	//
	// IT COMPLETES ONE RULE THAT HAS TWO CAUSES. The other cause — the buyer
	// reassigning an accepted Ticket — needs no wiring, because it happens inside
	// catalog already. This is the half that has to cross a module boundary, and
	// a build that forgot it would be talkative about a reassignment and silent
	// about a reversal, which is exactly what #327 forbids.
	//
	// The far side reads TICKET_ASSIGNMENT_ENABLED, so a dark deployment reverses
	// sales and mails nobody (ADR 0045).
	salesService = salesService.WithDisplacedHolders(catalogService)

	// A Follow of a Tag is stored against a Tag id, and the Customer names one by
	// the canonical key the Storefront's chips already carry (#218). Turning the
	// one into the other is catalog's rule — including canonicalizing the key the
	// same way every other path into the shared pool does, and answering an
	// unknown key with TAG_NOT_FOUND rather than coining a Tag — so customers
	// declares the narrow interface and catalog satisfies it, exactly as identity
	// does for Organization slugs above. Tied here rather than at construction
	// because catalog is built after customers.
	customersService = customersService.WithTags(catalogService)

	// Accepting a Ticket Assignment mints or matches a Verified Customer (#325,
	// ADR 0046), and a Customer record — `verified_at` above all — is the
	// customers module's authority (ADR 0010).
	//
	// SO THE SEAM POINTS THAT WAY: catalog declares the interface and customers
	// satisfies it, exactly as the Tag one above points the other. Catalog never
	// imports customers/service. Tied here rather than at construction because
	// catalog is built after customers, and because a catalog service nobody
	// wired this into accepts nothing at all — which is the safe way to be
	// unwired, and is what every build before this ticket was.
	catalogService = catalogService.WithHolderCustomers(customersService)

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

	// The Follow Digest pipeline (#220, ADR 0030): the platform's second piece of
	// scheduled work, driven by two internal endpoints.
	//
	// It is built after catalog because it needs catalog's localized Tag names,
	// and it takes the Storefront origin for the reason affiliates does: the link
	// on each Event in a Digest is derived at send time and never stored.
	digestRepo := digestrepo.New(db)
	digestService := digestsvc.New(digestRepo, emailSender, cfg.StorefrontBaseURL, platformLogger)
	if options.clock != nil {
		digestService = digestService.WithClock(options.clock)
	}
	// Naming a Tag in the reader's Mail Locale is catalog's rule and not this
	// module's — a Preset Tag from the catalogue, a Custom Tag exactly as coined
	// (ADR 0027 as amended by ADR 0030). The digest module declares the narrow
	// interface and catalog satisfies it, as customers does for Tag ids.
	digestService = digestService.WithTags(catalogService)
	// The unsubscribe link in every Digest footer (#224, ADR 0030). It REUSES the
	// Confirmation Link signing machinery rather than adding a second scheme:
	// customers owns the key, the Customer, and what unsubscribing does and does
	// not touch, so the digest module declares the narrow minter interface and
	// customers satisfies it — as catalog does for Tag names above.
	//
	// Tied here rather than at construction because customers is built long
	// before this and the dependency is additive. Note which way it runs: the
	// digest module can ask for a link and can do nothing else, so no amount of
	// change in here can reach a Follow.
	digestService = digestService.WithUnsubscribe(customersService)
	digestHandler := digesthandler.New(digestService)

	// The Operator Dashboard is composed from the modules that own its data:
	// identity for Organizations, catalog for Events, sales for money, and
	// customers for the Consent Withdrawal an Operator records on somebody's
	// behalf (#271). It is wired last because it depends on all four and none of
	// them on it.
	operatorService := operatorsvc.New(identityService, catalogService, salesService, customersService)
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
		DigestRepo:        digestRepo,
		DigestService:     digestService,
		DigestHandler:     digestHandler,
		ConsentRepo:       consentRepo,
		ConsentService:    consentService,
		ConsentHandler:    consentHandler,
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
	//
	// A deployment with NO transactional key sends no real mail at all, so there
	// is no domain to protect and nothing to split: the logging sender handles
	// Digests too, which is what local development and the manual verification
	// docs read. The split below is for deployments that actually send.
	if cfg.ResendAPIKey == "" {
		logger.Info("email sender: logging (no RESEND_API_KEY set)")
		return &platform.LoggingEmailSender{Logger: logger}
	}

	logger.Info("email sender: resend", "from", cfg.EmailFrom)
	transactional := platform.NewResendEmailSender(cfg.ResendAPIKey, cfg.EmailFrom, logger)
	return platform.NewSplitEmailSender(transactional, newDigestEmailSender(cfg, logger))
}

// newDigestEmailSender selects the sender for the one non-transactional message
// this platform sends (#225, ADR 0030).
//
// It reads cfg.DigestEmail and nothing else about the transactional identity
// except to check whether the two are on the same domain. Sharing it is refused
// unless the deployment set DIGEST_EMAIL_ALLOW_SHARED_DOMAIN, and the default
// answer is no because marketing mail attracts spam complaints, complaint rates
// degrade domain reputation, and the reputation at stake on the transactional
// domain is the one delivering the One-time Passcodes people sign in with. What
// this function will not do is decide that for itself: the sharing is never
// inferred, only obeyed. An unconfigured Digest sends nothing, loudly.
func newDigestEmailSender(cfg platform.Config, logger platform.Logger) platform.DigestEmailSender {
	if reason := cfg.DigestEmail.UnconfiguredReason(cfg.EmailFrom); reason != "" {
		// Error level, at startup, and again on every refused send. This is a
		// deployment that believes it has a discovery feature and does not.
		logger.Error("digest email sender: not configured; follow digests will not be sent", "reason", reason)
		return platform.NewUnconfiguredDigestSender(logger, reason)
	}
	logger.Info("digest email sender: resend", "from", cfg.DigestEmail.From)
	return platform.NewResendEmailSender(cfg.DigestEmail.ResendAPIKey, cfg.DigestEmail.From, logger)
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
