package platform

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// DevStorefrontBaseURL is where the Storefront answers on the local Compose
// stack. It is a fallback for development only: a Confirmation Link built on it
// is useless anywhere else, which is why NewApp says so out loud if production
// is still using it.
const DevStorefrontBaseURL = "http://localhost:64300"

// GoogleTokenEndpoint is Google's own token endpoint, where every authorization
// code is redeemed unless a non-production deployment says otherwise.
const GoogleTokenEndpoint = "https://oauth2.googleapis.com/token"

// PayPhoneBaseURL is PayPhone's own API origin, where every payment is prepared
// and confirmed unless a non-production deployment says otherwise.
const PayPhoneBaseURL = "https://pay.payphonetodoesposible.com"

// The Platform Fee rates in force at launch, in basis points: a 10% commission
// on the ticket price the Organization set, plus Ecuador's 15% IVA on that
// commission (ADR 0014). They are defaults rather than constants because the
// next IVA change should be an ops action, not a deploy — Ecuador moved from
// 12% to 15% in 2024.
const (
	DefaultPlatformFeeBasisPoints    = 1000
	DefaultPlatformFeeIVABasisPoints = 1500
)

// Config holds application configuration loaded from the environment.
type Config struct {
	AppEnv        string
	DatabaseURL   string
	HTTPAddr      string
	RunMigrations bool
	LogLevel      slog.Level
	S3Endpoint    string
	S3PublicURL   string
	S3AccessKey   string
	S3SecretKey   string
	S3Bucket      string
	S3Region      string
	ResendAPIKey  string
	EmailFrom     string
	// DigestEmail is the Follow Digest's OWN sending identity, configured
	// independently of the transactional one above. See DigestEmailConfig.
	DigestEmail DigestEmailConfig
	// StorefrontBaseURL is the Storefront's own public origin. The API needs it
	// to build the Confirmation Link carried in every Sale Confirmation: the link
	// points at the Storefront, not at this API, because a Customer must land on a
	// page and no browser may address the API directly (ADR 0008).
	StorefrontBaseURL string
	// ConfirmationLinkSecret is the HMAC key every Confirmation Link is signed
	// with. It is the whole of that credential's security: anyone holding it can
	// mint a link to any Ticket Sale, so it is read from the environment like
	// every other secret and never defaulted in production.
	ConfirmationLinkSecret string
	// InvoicingCertificateKey is the AES-256 key the Issuer's signing
	// certificate and its password are kept encrypted under in Postgres (#453,
	// ADR 0059): the 32 bytes INVOICING_CERTIFICATE_KEY decodes to, or nil when
	// it is unset. Nil is a running state and not a startup failure — the app
	// serves everything but certificate upload and signing without it — so
	// that invoicing being unconfigured is never an outage.
	InvoicingCertificateKey []byte
	// OTPGlobalCeiling caps passcode emails sent platform-wide per rate window,
	// across staff and customer sign-in alike. Zero means "use the package
	// default". Tunable without a deploy-time code change because the right
	// number moves with real traffic.
	OTPGlobalCeiling int
	// Google holds what the API needs to complete a Google Sign-In on either
	// surface. See GoogleConfig.
	Google GoogleConfig
	// PayPhone holds the platform's single PayPhone merchant credentials
	// (ADR 0012). Their presence selects the PayPhone Payment Provider; absent,
	// the stub provider serves so checkout works locally with zero setup.
	PayPhone PayPhoneConfig
	// Fees holds the Platform Fee and Fee IVA rates the platform withholds on
	// every Online Sale. See FeeConfig.
	Fees FeeConfig
	// TicketQuestionsEnabled opens the Ticket Question authoring surface, from
	// TICKET_QUESTIONS_ENABLED (#309, ADR 0045).
	//
	// IT STARTS FALSE, AND FLIPPING IT IS A DECISION SOMEBODY MUST TAKE ON
	// PURPOSE. A Ticket Question can ask anything an Organization types, and the
	// two examples that motivated the feature — a t-shirt size and dietary
	// requirements — put health data in reach. The published Privacy Policy does
	// not describe collecting any of it, so the flag flips only once a Policy
	// Version that does has published. Read ADR 0045 before changing this
	// default; the ADR was written to prevent exactly that change being made
	// casually.
	//
	// With it off, no Answer can be authored into existence, no Storefront
	// surface changes, and no export changes — the staff endpoints answer 404 as
	// a build without the feature does.
	TicketQuestionsEnabled bool
	// TicketAssignmentEnabled opens Ticket Assignment — the buyer naming an
	// email address for one of their Tickets — from TICKET_ASSIGNMENT_ENABLED
	// (#324, parent #322).
	//
	// A SEPARATE FLAG FROM TicketQuestionsEnabled ABOVE, AND THAT SEPARATION IS
	// THE POINT. The two features are genuinely separable: Ticket Questions are
	// merged and work with no assignment at all, and a guest list is worth
	// having on a Ticket Type that asks nothing. Two flags mean assignment can
	// be killed without taking questions dark, which is the one operational
	// property this pair exists to give. Never fold them into one.
	//
	// IT STARTS FALSE, AND FLIPPING IT IS A DECISION SOMEBODY MUST TAKE ON
	// PURPOSE — more so than its neighbour, because what it opens is the
	// platform storing, and later mailing, an address supplied by somebody with
	// no authority to supply it. The published Privacy Policy does not describe
	// that collection, so this flips only once a Policy Version that does has
	// published; the clause is drafted for counsel and is batched with the one
	// ADR 0045 is already waiting on, so Customers are re-gated once and not
	// twice.
	//
	// With it off, no address can be stored, no Ticket leaves the `unassigned`
	// state, no mail is sent, and the buyer's surfaces carry no assignment
	// fields at all — a build with the flag closed answers exactly as a build
	// without the feature.
	TicketAssignmentEnabled bool
}

// FeeConfig is the platform-wide fee schedule: the Platform Fee rate and the
// Fee IVA rate levied on it, both in basis points (1000 = 10%). Rates are
// platform-level and identical for every Organization at launch; per-sale rate
// snapshots are what make a negotiated rate additive later (ADR 0014).
type FeeConfig struct {
	FeeBasisPoints    int
	FeeIVABasisPoints int
}

// PayPhoneConfig is the platform's registration with PayPhone: one merchant
// account for the whole platform, settled with Organizations off-system
// (ADR 0012). Both values are secrets in production, injected like every other
// secret; neither is required — a deployment without them runs on the stub
// Payment Provider.
type PayPhoneConfig struct {
	APIToken string
	StoreID  string
	// BaseURL is where payments are prepared and confirmed. It defaults to
	// PayPhone's own origin and exists to be overridden by exactly one caller:
	// the integration suite, which points it at a fake PayPhone server.
	// LoadConfig refuses an override in production — see loadPayPhoneConfig.
	BaseURL string
}

// Configured reports whether PayPhone credentials are present — the switch that
// selects the real Payment Provider over the stub.
func (c PayPhoneConfig) Configured() bool {
	return c.APIToken != "" && c.StoreID != ""
}

// DigestEmailConfig is the Follow Digest's sending identity: its own Resend API
// key and its own From address, on its own subdomain (#225, ADR 0030).
//
// It exists because the Digest is the platform's first NON-TRANSACTIONAL mail.
// Marketing mail attracts spam complaints in a way transactional mail never
// does, and complaint rates degrade domain reputation — so a Digest sharing
// `send.multiticketing.com` could impair delivery of the One-time Passcodes
// people need in order to sign in. A discovery feature would be taking down
// authentication.
//
// It is a SIBLING of ResendAPIKey/EmailFrom and never a derivative of them.
// Neither field has a default, deliberately: a default here would be the silent
// fallback this whole feature exists to remove, and "unset" has to read as
// unset rather than as "use the one next to it". A deployment that has not
// configured this sends no Digests at all — see NewUnconfiguredDigestSender.
//
// AllowSharedSendingDomain is the one way to put the Digest back on the
// transactional domain, and it is a setting rather than a deletion because the
// reason to keep them apart did not stop being true — the deployment simply
// cannot act on it. Resend's free tier verifies ONE domain, so a second
// subdomain is not a configuration step there but a paid plan. Faced with that,
// the honest options are to send Digests from the transactional domain or to
// send none at all, and the second is worse: the feature is dark, while the
// risk it avoids is a reputation effect that only appears at a complaint volume
// a free tier's sending limits cannot reach. So the flag says yes, out loud, in
// a place an operator sets deliberately and can unset the day the plan changes.
// Production set it on 2026-08-05 and unset it on 2026-08-20, having moved to a
// plan that verifies the second domain; the flag remains for deployments that
// cannot. It is never inferred from the domains matching — that shape is also
// what a typo in DIGEST_EMAIL_FROM looks like, and that one should still be
// refused.
type DigestEmailConfig struct {
	ResendAPIKey             string
	From                     string
	AllowSharedSendingDomain bool
}

// Configured reports whether a usable Digest identity is present — the switch
// that selects a real Digest sender over the refusing one. BOTH halves are
// required: a key with no From has nothing to send as, and a From with no key
// cannot authenticate.
func (c DigestEmailConfig) Configured() bool {
	return c.ResendAPIKey != "" && c.From != ""
}

// PartiallyConfigured reports whether exactly one half was supplied. It is not
// the negation of Configured: it distinguishes "this deployment never switched
// Digest mail on" from "somebody tried and stopped halfway", which are the same
// outcome and very different bugs. An operator in the second case is told which
// half is missing at startup rather than watching Digests silently not arrive.
func (c DigestEmailConfig) PartiallyConfigured() bool {
	return (c.ResendAPIKey == "") != (c.From == "")
}

// SharesSendingDomainWith reports whether this Digest identity sends from the
// same domain as the given transactional From header.
//
// The preferred second identity is on a DIFFERENT sending domain. A Digest From
// pointing back at the transactional domain is otherwise exactly the coupling
// ADR 0030 removes, wearing a different variable name, so it is detected rather
// than trusted — and newDigestEmailSender refuses to send on it unless
// AllowSharedSendingDomain says the sharing was meant. This method reports the
// fact and not the verdict; UnconfiguredReason decides what to do about it.
// Comparison is case-insensitive because DNS is; an empty From on either side
// shares nothing, having no domain to share.
func (c DigestEmailConfig) SharesSendingDomainWith(transactionalFrom string) bool {
	digest := sendingDomain(c.From)
	transactional := sendingDomain(transactionalFrom)
	if digest == "" || transactional == "" {
		return false
	}
	return digest == transactional
}

// UnconfiguredReason states, in one line an operator can act on, why this
// deployment cannot send Digests — or "" when it can.
//
// It is a method rather than a branch at the call site because every answer
// here is a refusal to send marketing mail, and the three ways to earn one
// (nothing set, half set, set to the transactional domain without saying so)
// deserve to be listed together and phrased as instructions.
func (c DigestEmailConfig) UnconfiguredReason(transactionalFrom string) string {
	switch {
	case c.ResendAPIKey == "" && c.From == "":
		return "neither DIGEST_RESEND_API_KEY nor DIGEST_EMAIL_FROM is set"
	case c.ResendAPIKey == "":
		return "DIGEST_EMAIL_FROM is set but DIGEST_RESEND_API_KEY is not"
	case c.From == "":
		return "DIGEST_RESEND_API_KEY is set but DIGEST_EMAIL_FROM is not"
	case c.SharesSendingDomainWith(transactionalFrom) && !c.AllowSharedSendingDomain:
		// Named as a choice with two exits, because both are legitimate and the
		// operator hitting this cannot tell from the symptom which one they are
		// in: a typo pointing the Digest at the transactional domain looks
		// identical to a deliberate single-domain deployment.
		return fmt.Sprintf("DIGEST_EMAIL_FROM (%s) is on the same sending domain as EMAIL_FROM; give the Digest its own subdomain (ADR 0030), or set DIGEST_EMAIL_ALLOW_SHARED_DOMAIN=true to accept sharing the transactional domain's reputation", c.From)
	}
	return ""
}

// loadDigestEmailConfig reads the Digest's sending identity from the
// environment. Every field is read with no fallback, with ONE exception it
// takes the transactional key as an argument in order to make.
//
// When the operator has said the two identities share a sending domain, they
// share the Resend domain object, and a Resend key is scoped to an account
// rather than to a domain — so requiring DIGEST_RESEND_API_KEY there would be
// requiring the same secret to be stored twice. Two copies of one credential is
// not extra isolation; it is a rotation hazard, where the transactional half is
// rotated, the Digest half is forgotten, and Digests fail with a stale key on
// some later Thursday. The fallback is therefore gated on the same explicit
// flag as the domain sharing itself and is unreachable without it: with no flag
// set, an unset DIGEST_RESEND_API_KEY still means no Digests, exactly as before.
// Supplying the key outright always wins, so a separate restricted key remains
// possible for anyone who wants one.
func loadDigestEmailConfig(transactionalAPIKey string) DigestEmailConfig {
	cfg := DigestEmailConfig{
		ResendAPIKey:             strings.TrimSpace(os.Getenv("DIGEST_RESEND_API_KEY")),
		From:                     strings.TrimSpace(os.Getenv("DIGEST_EMAIL_FROM")),
		AllowSharedSendingDomain: envIsTrue("DIGEST_EMAIL_ALLOW_SHARED_DOMAIN"),
	}
	if cfg.AllowSharedSendingDomain && cfg.ResendAPIKey == "" {
		cfg.ResendAPIKey = strings.TrimSpace(transactionalAPIKey)
	}
	return cfg
}

// envIsTrue reads a boolean setting the permissive way round: anything Go does
// not parse as true — including unset, empty and misspelt — is false. Every
// caller here guards something that should stay on unless deliberately turned
// off, so an unreadable value must never be the one that opens the gate.
func envIsTrue(key string) bool {
	value, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(key)))
	return err == nil && value
}

// sendingDomain pulls the domain out of an RFC 5322 From header, lowercased.
// It accepts both `Name <user@host>` and a bare `user@host`, which are the two
// shapes these settings are ever written in. Anything it cannot parse yields
// "", which every caller reads as "no domain to compare".
func sendingDomain(from string) string {
	address := strings.TrimSpace(from)
	if open := strings.LastIndex(address, "<"); open >= 0 {
		rest := address[open+1:]
		close := strings.Index(rest, ">")
		if close < 0 {
			return ""
		}
		address = strings.TrimSpace(rest[:close])
	}
	at := strings.LastIndex(address, "@")
	if at < 0 || at == len(address)-1 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(address[at+1:]))
}

// GoogleOAuthClient is one surface's registration with Google.
//
// Each surface has its own (ADR 0011), because Google binds an authorization
// code to the client that requested it: a code obtained on the Storefront
// cannot be redeemed with the Staff client's credentials, so cross-surface
// isolation is a property of these values rather than of a check in application
// code. A leaked Storefront secret therefore reaches no Organization.
type GoogleOAuthClient struct {
	ClientID     string
	ClientSecret string
}

// GoogleConfig is the Google Sign-In configuration, one entry per surface plus
// the endpoint they share.
//
// The two clients are siblings and stay siblings: neither is a default for the
// other and there is no "the Google client". A surface that cannot name which
// one it means has a bug, because naming the wrong one is exactly the mistake
// ADR 0011's isolation exists to make impossible.
type GoogleConfig struct {
	// Storefront is the client the Customer surface signs in with.
	Storefront GoogleOAuthClient
	// Staff is the client the Staff surface signs in with. A code obtained on
	// the Storefront is not redeemable with these credentials, which is where
	// cross-surface isolation lives.
	Staff GoogleOAuthClient
	// TokenEndpoint is where authorization codes are redeemed. It defaults to
	// Google's own endpoint and exists to be overridden by exactly one caller:
	// the integration suite, which points it at a stub. LoadConfig refuses an
	// override in production — see loadGoogleConfig.
	TokenEndpoint string
}

// LoadConfig reads configuration from environment variables.
func LoadConfig() (Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	otpCeiling, err := envIntOrZero("OTP_GLOBAL_CEILING")
	if err != nil {
		return Config{}, err
	}

	appEnv := envOrDefault("APP_ENV", "development")

	// A Confirmation Link is a bearer credential for a Ticket Sale, so production
	// must never sign one with a fallback key. That requirement is enforced in
	// server.NewApp, at the point the key is actually used, and not here: this
	// same LoadConfig runs in cmd/migrate, whose Cloud Run Job identity reads
	// only the connection string and has no business holding a signing key.
	// Requiring it here failed the migrate Job on every production deploy.
	confirmationLinkSecret := strings.TrimSpace(os.Getenv("CONFIRMATION_LINK_SECRET"))

	invoicingCertificateKey, err := loadInvoicingCertificateKey()
	if err != nil {
		return Config{}, err
	}

	resendAPIKey := os.Getenv("RESEND_API_KEY")
	digestEmail := loadDigestEmailConfig(resendAPIKey)

	google, err := loadGoogleConfig(appEnv)
	if err != nil {
		return Config{}, err
	}

	payPhone, err := loadPayPhoneConfig(appEnv)
	if err != nil {
		return Config{}, err
	}

	fees, err := loadFeeConfig()
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		AppEnv:        appEnv,
		DatabaseURL:   databaseURL,
		HTTPAddr:      envOrDefault("HTTP_ADDR", ":8080"),
		RunMigrations: shouldRunMigrations(),
		LogLevel:      parseLogLevel(os.Getenv("LOG_LEVEL")),
		S3Endpoint:    os.Getenv("S3_ENDPOINT"),
		S3PublicURL:   os.Getenv("S3_PUBLIC_URL"),
		S3AccessKey:   os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:   os.Getenv("S3_SECRET_KEY"),
		S3Bucket:      os.Getenv("S3_BUCKET"),
		S3Region:      envOrDefault("S3_REGION", "us-east-1"),
		ResendAPIKey:  resendAPIKey,
		EmailFrom:     envOrDefault("EMAIL_FROM", "Multiticketing <noreply@send.multiticketing.com>"),

		DigestEmail: digestEmail,

		// The dev-stack Storefront origin. Production injects the real one, as it
		// already does for the Storefront container itself; until a custom domain
		// is mapped it has none to inject, which NewApp warns about at startup.
		StorefrontBaseURL:      strings.TrimRight(envOrDefault("STOREFRONT_BASE_URL", DevStorefrontBaseURL), "/"),
		ConfirmationLinkSecret: confirmationLinkSecret,

		InvoicingCertificateKey: invoicingCertificateKey,

		OTPGlobalCeiling: otpCeiling,
		Google:           google,
		PayPhone:         payPhone,
		Fees:             fees,

		// envIsTrue is read the permissive way round here for once in the right
		// direction: anything it cannot parse as true — unset, empty, misspelt —
		// leaves Ticket Question authoring closed. A typo in this setting must
		// never be what opens it (ADR 0045).
		TicketQuestionsEnabled: envIsTrue("TICKET_QUESTIONS_ENABLED"),

		// Read the same permissive way round as its neighbour, and read from
		// its OWN variable: a deployment that opened Ticket Questions has said
		// nothing about assignment, and inheriting the answer would be the
		// separation of the two flags quietly ending at configuration.
		TicketAssignmentEnabled: envIsTrue("TICKET_ASSIGNMENT_ENABLED"),
	}
	return cfg, nil
}

// loadGoogleConfig reads the Google Sign-In settings and enforces the one guard
// this feature ships with.
//
// GOOGLE_TOKEN_ENDPOINT decides which issuer the platform trusts to say who
// somebody is. Anyone able to set it could point the exchange at a server they
// control and mint a session for any address on either surface, which is
// unrestricted account takeover — and it would leave no trace, because a
// sign-in through a hostile issuer looks exactly like a real one. It exists for
// the integration suite's stub token endpoint and for nothing else, so in
// production it is not warned about like the Storefront origin fallback, but
// refused: the process does not start.
//
// The check lives here rather than in server.NewApp because it is a refusal
// rather than a requirement. Nothing is asked of a deployment that has not set
// it, so cmd/migrate — which shares this loader and holds none of the API's
// secrets — is unaffected, and every workload built from this config is covered.
//
// The client credentials are not required. A deployment without them starts and
// serves every other path; Google Sign-In then refuses, which is what local
// development without Google credentials looks like.
func loadGoogleConfig(appEnv string) (GoogleConfig, error) {
	tokenEndpoint := strings.TrimSpace(os.Getenv("GOOGLE_TOKEN_ENDPOINT"))
	if tokenEndpoint != "" && appEnv == "production" {
		return GoogleConfig{}, fmt.Errorf("GOOGLE_TOKEN_ENDPOINT must not be set when APP_ENV is production: it decides which issuer the platform trusts and exists only for tests")
	}
	if tokenEndpoint == "" {
		tokenEndpoint = GoogleTokenEndpoint
	}

	return GoogleConfig{
		Storefront: GoogleOAuthClient{
			ClientID:     strings.TrimSpace(os.Getenv("GOOGLE_STOREFRONT_CLIENT_ID")),
			ClientSecret: strings.TrimSpace(os.Getenv("GOOGLE_STOREFRONT_CLIENT_SECRET")),
		},
		Staff: GoogleOAuthClient{
			ClientID:     strings.TrimSpace(os.Getenv("GOOGLE_STAFF_CLIENT_ID")),
			ClientSecret: strings.TrimSpace(os.Getenv("GOOGLE_STAFF_CLIENT_SECRET")),
		},
		TokenEndpoint: tokenEndpoint,
	}, nil
}

// loadPayPhoneConfig reads the PayPhone settings and enforces the same
// base-URL rule loadGoogleConfig does for the token endpoint.
//
// PAYPHONE_API_BASE_URL decides which server the platform believes collected a
// Customer's money. Anyone able to set it could point Prepare and Confirm at a
// server they control and "approve" payments no one ever made — free tickets,
// and nothing in the logs to distinguish them from real sales. It exists for
// the integration suite's fake PayPhone server and for nothing else, so in
// production it is refused: the process does not start.
//
// The check lives here rather than in server.NewApp for the same reason the
// Google one does: it is a refusal, not a requirement, so cmd/migrate — which
// shares this loader and holds no payment credentials — is unaffected.
//
// The credentials themselves are not required. A deployment without them
// starts on the stub Payment Provider, which is what local development looks
// like (ADR 0012).
func loadPayPhoneConfig(appEnv string) (PayPhoneConfig, error) {
	baseURL := strings.TrimSpace(os.Getenv("PAYPHONE_API_BASE_URL"))
	if baseURL != "" && appEnv == "production" {
		return PayPhoneConfig{}, fmt.Errorf("PAYPHONE_API_BASE_URL must not be set when APP_ENV is production: it decides which server the platform believes collected the money and exists only for tests")
	}
	if baseURL == "" {
		baseURL = PayPhoneBaseURL
	}

	return PayPhoneConfig{
		APIToken: strings.TrimSpace(os.Getenv("PAYPHONE_API_TOKEN")),
		StoreID:  strings.TrimSpace(os.Getenv("PAYPHONE_STORE_ID")),
		BaseURL:  baseURL,
	}, nil
}

// InvoicingCertificateKeyLength is the key size AES-256-GCM takes, and so
// what INVOICING_CERTIFICATE_KEY must decode to.
const InvoicingCertificateKeyLength = 32

// loadInvoicingCertificateKey reads INVOICING_CERTIFICATE_KEY: standard base64
// of exactly 32 bytes, or unset.
//
// Unset is allowed everywhere, production included, and is NOT the Confirmation
// Link rule: a deployment without the key boots and serves every other feature,
// and only certificate upload and signing answer with a specific error (#453).
// A key that IS set but is not 32 decoded bytes is a startup failure, because
// the alternative — silently running keyless, or padding it — would let a
// mistyped secret look like a missing one.
func loadInvoicingCertificateKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv("INVOICING_CERTIFICATE_KEY"))
	if raw == "" {
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("INVOICING_CERTIFICATE_KEY must be standard base64 of %d bytes: %w", InvoicingCertificateKeyLength, err)
	}
	if len(key) != InvoicingCertificateKeyLength {
		return nil, fmt.Errorf("INVOICING_CERTIFICATE_KEY must decode to exactly %d bytes, got %d", InvoicingCertificateKeyLength, len(key))
	}
	return key, nil
}

// loadFeeConfig reads the Platform Fee schedule, falling back to the launch
// rates. Both settings are ops knobs rather than secrets: a rate change (an IVA
// reform, a promotional fee) is an environment edit, and every sale already
// snapshots the rates it was charged under, so past economics do not move.
//
// A malformed or out-of-range rate is a startup error rather than a silent
// fallback: quietly reading "10" (meant as 10%) as 0.1% would bill wrong for as
// long as nobody checked.
func loadFeeConfig() (FeeConfig, error) {
	feeBP, err := envBasisPointsOrDefault("PLATFORM_FEE_BASIS_POINTS", DefaultPlatformFeeBasisPoints)
	if err != nil {
		return FeeConfig{}, err
	}
	ivaBP, err := envBasisPointsOrDefault("PLATFORM_FEE_IVA_BASIS_POINTS", DefaultPlatformFeeIVABasisPoints)
	if err != nil {
		return FeeConfig{}, err
	}
	return FeeConfig{FeeBasisPoints: feeBP, FeeIVABasisPoints: ivaBP}, nil
}

// envBasisPointsOrDefault reads an optional rate in basis points. Zero is a
// meaningful value ("charge nothing"), so the fallback applies only to an unset
// variable; anything above 10000 (100%) is a typo, not a rate.
func envBasisPointsOrDefault(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 || value > 10000 {
		return 0, fmt.Errorf("%s must be a rate in basis points between 0 and 10000, got %q", key, raw)
	}
	return value, nil
}

// envIntOrZero reads an optional positive integer setting. An unset variable
// means zero ("use the default"); a malformed one is a startup error rather
// than a silent fallback, because misreading an abuse limit as "unset" is
// exactly the mistake that stays invisible until it matters.
func envIntOrZero(key string) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", key, raw)
	}
	return value, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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

func parseLogLevel(raw string) slog.Level {
	switch raw {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
