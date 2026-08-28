// Package service holds the invoicing module's business rules: the Ecuador
// Issuer as an operator records it, its signing certificate in custody
// (#453), and from #454 what may no longer change once documents exist and
// the issue flow through the TaxAuthority seam.
package service

import (
	"context"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
	"github.com/peter/ticket_pos/backend/internal/invoicing/sri"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Service is the invoicing module's business logic.
type Service struct {
	repo    *repository.Repository
	custody *invoicing.Custody
	logger  platform.Logger
	clock   func() time.Time
	// authority builds the country adapter for the environment an invoice is
	// issued under; the SRI's real hosts unless overridden (#454).
	authority  func(invoicing.Environment) invoicing.TaxAuthority
	pollDelays []time.Duration
	pollBudget time.Duration
	// kick says whether a committed Sale Invoice is worked at once in the
	// background (#474); drainBatch narrows one drain's bound (tests).
	kick       bool
	drainBatch int
	// email is the transactional sender the Drainer hands an authorized
	// document to the buyer through (#475); storefrontBaseURL is where the
	// Customer Area lives, for the link in that mail. A deployment that
	// wires neither delivers nothing and says so in its log.
	email             platform.EmailSender
	storefrontBaseURL string
	// operators, staffLocales and staffBaseURL serve the Certificate Expiry
	// Warning (#503, ADR 0063): the allowlist the Drainer's tick warns, the
	// language each address reads, and the origin the Issuer-page link is
	// built on. A deployment that wires no reader warns nobody and says so.
	operators    PlatformOperators
	staffLocales StaffLocales
	staffBaseURL string
	// saleInvoicingEnabled is the SALE_INVOICING_ENABLED flag (#471, ADR
	// 0060). Closed, the Drainer's endpoint answers 404 and works nothing;
	// the manual Tax Invoices, the Issuer and every read serve as before.
	saleInvoicingEnabled bool
}

// New builds the invoicing Service. custody may be unconfigured (built over a
// nil key): the Service then serves everything but certificate upload and
// signing, which answer ErrCertificateKeyNotConfigured.
func New(repo *repository.Repository, custody *invoicing.Custody, logger platform.Logger) *Service {
	if custody == nil {
		custody, _ = invoicing.NewCustody(nil)
	}
	return &Service{
		repo:       repo,
		custody:    custody,
		logger:     logger,
		clock:      time.Now,
		authority:  sri.AuthorityFactory(),
		pollDelays: DefaultPollDelays,
		pollBudget: DefaultPollBudget,
		kick:       true,
		email:      platform.NoopEmailSender{},
	}
}

// WithEmailSender replaces the sender the Tax Document delivery goes out
// through: the transactional identity, never the Digest's (#475).
func (s *Service) WithEmailSender(sender platform.EmailSender) *Service {
	if sender != nil {
		s.email = sender
	}
	return s
}

// WithStorefrontBaseURL sets the Storefront's public origin, which the
// delivery mail's Customer Area link is built on.
func (s *Service) WithStorefrontBaseURL(baseURL string) *Service {
	s.storefrontBaseURL = baseURL
	return s
}

// PlatformOperators is what invoicing needs from identity in order to warn
// the Platform Operators that the signing certificate is about to lapse
// (#503, ADR 0063 §4): the allowlist, read as a list of addresses. The
// sales module's seam of the same name, for the same reason: presence on
// the allowlist is the whole of operator authority (ADR 0015), and asking
// identity rather than reading `platform_operators` here keeps who is
// WARNED and who is AUTHORISED the same set, decided in one module.
type PlatformOperators interface {
	PlatformOperatorEmails(ctx context.Context) ([]string, error)
}

// StaffLocales is what invoicing needs from identity in order to write the
// Certificate Expiry Warning in the language its reader uses (ADR 0041): the
// Staff Locale stored against one email address. "" is absence rather than
// English; platform.ResolveStaffLocale owns the floor.
type StaffLocales interface {
	StaffLocale(ctx context.Context, email string) (string, error)
}

// WithPlatformOperators supplies the operator allowlist reader the
// Certificate Expiry Warning fans out to. Tied on after construction, as
// the sales module ties its own, because it serves one notice.
func (s *Service) WithPlatformOperators(operators PlatformOperators) *Service {
	s.operators = operators
	return s
}

// WithStaffLocales supplies the Staff Locale reader, so each Certificate
// Expiry Warning is written in the language its recipient reads.
func (s *Service) WithStaffLocales(locales StaffLocales) *Service {
	s.staffLocales = locales
	return s
}

// WithStaffBaseURL sets the staff application's public origin, which the
// Certificate Expiry Warning's Issuer-page link is built on.
func (s *Service) WithStaffBaseURL(baseURL string) *Service {
	s.staffBaseURL = baseURL
	return s
}

// EcuadorIssuer is the Ecuador Issuer as the operator surface reads it: the
// core row and the SRI details flattened into one payload, because there is
// exactly one of each and the page shows them as one form.
//
// The certificate metadata (#453) and, later, the freeze flags the form greys
// fields on (#454) are on this shape rather than served beside it, so the
// page keeps loading one thing.
type EcuadorIssuer struct {
	ID                       string  `json:"id"`
	Country                  string  `json:"country"`
	Environment              string  `json:"environment"`
	RUC                      string  `json:"ruc"`
	RazonSocial              string  `json:"razon_social"`
	NombreComercial          string  `json:"nombre_comercial"`
	DireccionMatriz          string  `json:"direccion_matriz"`
	DireccionEstablecimiento string  `json:"direccion_establecimiento"`
	Establecimiento          string  `json:"establecimiento"`
	PuntoEmision             string  `json:"punto_emision"`
	ObligadoContabilidad     bool    `json:"obligado_contabilidad"`
	Regimen                  string  `json:"regimen"`
	AgenteRetencion          *string `json:"agente_retencion"`
	// Certificate is the signing certificate's metadata, null until one is
	// uploaded. Never the bytes and never the password, under any name.
	Certificate *EcuadorIssuerCertificate `json:"certificate"`
	// CertificateRUCMismatch is true only when the certificate carries a RUC
	// and it is not the Issuer's. A warning for the page, never a refusal: the
	// SRI's own check is the final word.
	CertificateRUCMismatch bool `json:"certificate_ruc_mismatch"`
	// CertificateExpiry is the Certificate Expiry Warning's state (#500, ADR
	// 0063 §5), derived on the server by the rule the Drainer's ladder uses,
	// so the banners never count days from a date. Served whether or not
	// SALE_INVOICING_ENABLED is open.
	CertificateExpiry EcuadorIssuerCertificateExpiry `json:"certificate_expiry"`
	// FrozenFields names the details that may no longer change (#455): "ruc"
	// once any Tax Invoice exists in either environment, "establecimiento"
	// and "punto_emision" once a sequence has started under them. Empty
	// until then. The page renders these read-only and says why; a save
	// that changes one is refused with ISSUER_FIELD_FROZEN.
	FrozenFields []string  `json:"frozen_fields"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// The frozen field names, as they appear in frozen_fields and in the
// refusal's details.field. They are the JSON names of the Issuer payload.
const (
	frozenFieldRUC             = "ruc"
	frozenFieldEstablecimiento = "establecimiento"
	frozenFieldPuntoEmision    = "punto_emision"
)

// frozenField is one Issuer detail that may stop changing: its JSON name and
// how to read it off the details, so the three are listed once and both the
// frozen_fields view and the save-time refusal walk the same table.
type frozenField struct {
	name string
	get  func(invoicing.EcuadorIssuerDetails) string
}

var (
	frozenRUC             = frozenField{frozenFieldRUC, func(d invoicing.EcuadorIssuerDetails) string { return d.RUC }}
	frozenEstablecimiento = frozenField{frozenFieldEstablecimiento, func(d invoicing.EcuadorIssuerDetails) string { return d.Establecimiento }}
	frozenPuntoEmision    = frozenField{frozenFieldPuntoEmision, func(d invoicing.EcuadorIssuerDetails) string { return d.PuntoEmision }}
)

// EcuadorIssuerCertificate is the certificate in custody as the page shows
// it: metadata only.
type EcuadorIssuerCertificate struct {
	Subject string `json:"subject"`
	// RUC is the RUC found inside the certificate, "" when none was.
	RUC       string    `json:"ruc"`
	NotBefore time.Time `json:"not_before"`
	NotAfter  time.Time `json:"not_after"`
	// FingerprintSHA256 is lowercase hex.
	FingerprintSHA256 string    `json:"fingerprint_sha256"`
	UploadedAt        time.Time `json:"uploaded_at"`
}

// EcuadorIssuerCertificateExpiry is invoicing.CertificateExpiry on the wire:
// `state` is `none`, `valid`, `expiring` or `expired`; `not_after` and
// `days_before` are null exactly under `none`.
type EcuadorIssuerCertificateExpiry struct {
	State string `json:"state" enums:"none,valid,expiring,expired"`
	// NotAfter is the certificate's own end of validity, UTC.
	NotAfter *time.Time `json:"not_after"`
	// DaysBefore counts Ecuadorian calendar days from today to the date
	// NotAfter falls on: zero on the day of expiry, negative once past.
	DaysBefore *int `json:"days_before"`
}

func certificateExpiryView(expiry invoicing.CertificateExpiry) EcuadorIssuerCertificateExpiry {
	view := EcuadorIssuerCertificateExpiry{State: string(expiry.State), DaysBefore: expiry.DaysBefore}
	if expiry.NotAfter != nil {
		notAfter := expiry.NotAfter.UTC()
		view.NotAfter = &notAfter
	}
	return view
}

// SaveEcuadorIssuerInput is what a save carries: the environment on the core
// Issuer and the SRI details, already normalised by the handler.
type SaveEcuadorIssuerInput struct {
	Environment invoicing.Environment
	Details     invoicing.EcuadorIssuerDetails
}

// GetEcuadorIssuer returns the Ecuador Issuer, or nil when none has been
// recorded yet — an ordinary state, not an error.
func (s *Service) GetEcuadorIssuer(ctx context.Context) (*EcuadorIssuer, error) {
	row, err := s.repo.GetEcuadorIssuer(ctx)
	if err != nil || row == nil {
		return nil, err
	}
	return s.ecuadorIssuerViewWithFreezes(ctx, row)
}

// SaveEcuadorIssuer records the Ecuador Issuer, creating it on the first save
// and updating it after, and returns it as stored.
//
// The input arrives validated: well-formedness is
// invoicing.EcuadorIssuerDetails.Normalize's verdict and the handler asks for
// it. What only the service can answer is whether a detail MAY change (#455):
// the RUC is refused once any Tax Invoice exists for the Issuer in either
// environment, establecimiento and punto de emisión once a sequence has
// started under them. Sent unchanged, a frozen field is no obstacle; every
// other detail saves freely.
func (s *Service) SaveEcuadorIssuer(ctx context.Context, input SaveEcuadorIssuerInput) (*EcuadorIssuer, error) {
	current, err := s.repo.GetEcuadorIssuer(ctx)
	if err != nil {
		return nil, err
	}
	if current != nil {
		if err := s.refuseFrozenChanges(ctx, current, input.Details); err != nil {
			return nil, err
		}
	}
	row, err := s.repo.SaveEcuadorIssuer(ctx, input.Environment, input.Details)
	if err != nil {
		return nil, err
	}
	return s.ecuadorIssuerViewWithFreezes(ctx, row)
}

// frozenFields names the Issuer details that may no longer change, from
// what has been issued: never from the Issuer row itself. The order is the
// order the page shows them.
func (s *Service) frozenFields(ctx context.Context, row *repository.EcuadorIssuerRow) ([]frozenField, error) {
	frozen := []frozenField{}
	hasInvoices, err := s.repo.IssuerHasInvoices(ctx, row.Issuer.ID)
	if err != nil {
		return nil, err
	}
	if hasInvoices {
		frozen = append(frozen, frozenRUC)
	}
	sequenceStarted, err := s.repo.SequenceExists(ctx, row.Issuer.ID, row.Details.Establecimiento, row.Details.PuntoEmision)
	if err != nil {
		return nil, err
	}
	if sequenceStarted {
		frozen = append(frozen, frozenEstablecimiento, frozenPuntoEmision)
	}
	return frozen, nil
}

// refuseFrozenChanges answers ErrIssuerFieldFrozen for the first frozen
// detail the save would change, in the order the page shows them.
func (s *Service) refuseFrozenChanges(ctx context.Context, current *repository.EcuadorIssuerRow, next invoicing.EcuadorIssuerDetails) error {
	frozen, err := s.frozenFields(ctx, current)
	if err != nil {
		return err
	}
	for _, field := range frozen {
		if field.get(next) != field.get(current.Details) {
			return invoicing.ErrIssuerFieldFrozen(field.name)
		}
	}
	return nil
}

func (s *Service) ecuadorIssuerViewWithFreezes(ctx context.Context, row *repository.EcuadorIssuerRow) (*EcuadorIssuer, error) {
	frozen, err := s.frozenFields(ctx, row)
	if err != nil {
		return nil, err
	}
	view := ecuadorIssuerView(s.clock(), row)
	view.FrozenFields = make([]string, 0, len(frozen))
	for _, field := range frozen {
		view.FrozenFields = append(view.FrozenFields, field.name)
	}
	return view, nil
}

// UploadEcuadorIssuerCertificate puts a .p12 and its password in custody on
// the Ecuador Issuer, replacing any certificate already there, and returns
// the Issuer as it now reads.
//
// THE FILE IS OPENED BEFORE ANYTHING IS STORED. A wrong password, a file with
// no RSA key and a file that is not a .p12 each come back as their own error
// and leave the row exactly as it was, so an operator never discovers a bad
// certificate at signing time (#450 story 8). Without a certificate key the
// upload fails before the file is even looked at, with the one code the page
// shows as "the server has no key" rather than "bad file".
func (s *Service) UploadEcuadorIssuerCertificate(ctx context.Context, p12 []byte, password string) (*EcuadorIssuer, error) {
	if !s.custody.Configured() {
		return nil, invoicing.ErrCertificateKeyNotConfigured()
	}
	row, err := s.repo.GetEcuadorIssuer(ctx)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, invoicing.ErrIssuerNotFound()
	}

	cert, err := sri.OpenCertificate(p12, password)
	if err != nil {
		switch {
		case errors.Is(err, sri.ErrCertificatePassword):
			return nil, invoicing.ErrCertificatePasswordIncorrect()
		case errors.Is(err, sri.ErrCertificateNoRSAKey):
			return nil, invoicing.ErrCertificateNoRSAKey()
		default:
			return nil, invoicing.ErrCertificateFileInvalid()
		}
	}

	sealedP12, err := s.custody.Seal(p12)
	if err != nil {
		return nil, err
	}
	sealedPassword, err := s.custody.Seal([]byte(password))
	if err != nil {
		return nil, err
	}
	meta := invoicing.CertificateMetadata{
		Subject:           cert.Metadata.Subject,
		RUC:               cert.Metadata.RUC,
		NotBefore:         cert.Metadata.NotBefore,
		NotAfter:          cert.Metadata.NotAfter,
		FingerprintSHA256: cert.Metadata.FingerprintSHA256,
	}
	if err := s.repo.SaveCertificate(ctx, row.Issuer.ID, repository.SealedCertificate{P12: sealedP12, Password: sealedPassword}, meta); err != nil {
		return nil, err
	}
	s.logger.Info("invoicing: ecuador issuer certificate replaced",
		"issuer_id", row.Issuer.ID,
		"fingerprint_sha256", meta.FingerprintSHA256,
		"not_after", meta.NotAfter,
	)
	return s.GetEcuadorIssuer(ctx)
}

// OpenEcuadorSigningKey opens the Ecuador Issuer's signing certificate into
// memory: the private key and leaf certificate the XAdES-BES signer takes
// (#454). This is the only place the custody columns are decrypted, and the
// result is the caller's to hold for one signing and drop.
//
// Absent key, absent Issuer, absent certificate and a certificate that will
// not open under the current key are each their own error, so the issue flow
// can say which of them stops it.
func (s *Service) OpenEcuadorSigningKey(ctx context.Context) (*sri.Certificate, error) {
	if !s.custody.Configured() {
		return nil, invoicing.ErrCertificateKeyNotConfigured()
	}
	row, err := s.repo.GetEcuadorIssuer(ctx)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, invoicing.ErrIssuerNotFound()
	}
	sealed, err := s.repo.GetSealedCertificate(ctx, row.Issuer.ID)
	if err != nil {
		return nil, err
	}
	if sealed == nil {
		return nil, invoicing.ErrCertificateNotUploaded()
	}
	p12, err := s.custody.Open(sealed.P12)
	if err != nil {
		return nil, s.unreadable(row.Issuer.ID, err)
	}
	password, err := s.custody.Open(sealed.Password)
	if err != nil {
		return nil, s.unreadable(row.Issuer.ID, err)
	}
	cert, err := sri.OpenCertificate(p12, string(password))
	if err != nil {
		// It opened at upload, so this is the row or the key, not the file.
		return nil, s.unreadable(row.Issuer.ID, err)
	}
	return cert, nil
}

func (s *Service) unreadable(issuerID string, cause error) error {
	if errors.Is(cause, invoicing.ErrCustodyNotConfigured) {
		return invoicing.ErrCertificateKeyNotConfigured()
	}
	s.logger.Error("invoicing: stored certificate will not open under the current key", "issuer_id", issuerID, "error", cause)
	return invoicing.ErrCertificateUnreadable()
}

func ecuadorIssuerView(now time.Time, row *repository.EcuadorIssuerRow) *EcuadorIssuer {
	updatedAt := row.Issuer.UpdatedAt
	if row.DetailsUpdatedAt.After(updatedAt) {
		updatedAt = row.DetailsUpdatedAt
	}
	var certificate *EcuadorIssuerCertificate
	mismatch := false
	if c := row.Issuer.Certificate; c != nil {
		// UTC on the wire: a validity window is a fact about the certificate,
		// not about where the process happens to run.
		certificate = &EcuadorIssuerCertificate{
			Subject:           c.Subject,
			RUC:               c.RUC,
			NotBefore:         c.NotBefore.UTC(),
			NotAfter:          c.NotAfter.UTC(),
			FingerprintSHA256: c.FingerprintSHA256,
			UploadedAt:        c.UploadedAt.UTC(),
		}
		mismatch = c.RUC != "" && c.RUC != row.Details.RUC
	}
	return &EcuadorIssuer{
		ID:                       row.Issuer.ID,
		Country:                  string(row.Issuer.Country),
		Environment:              string(row.Issuer.Environment),
		RUC:                      row.Details.RUC,
		RazonSocial:              row.Details.RazonSocial,
		NombreComercial:          row.Details.NombreComercial,
		DireccionMatriz:          row.Details.DireccionMatriz,
		DireccionEstablecimiento: row.Details.DireccionEstablecimiento,
		Establecimiento:          row.Details.Establecimiento,
		PuntoEmision:             row.Details.PuntoEmision,
		ObligadoContabilidad:     row.Details.ObligadoContabilidad,
		Regimen:                  row.Details.Regimen,
		AgenteRetencion:          row.Details.AgenteRetencion,
		Certificate:              certificate,
		CertificateRUCMismatch:   mismatch,
		CertificateExpiry:        certificateExpiryView(invoicing.CertificateExpiryAt(now, row.Issuer.Certificate)),
		CreatedAt:                row.Issuer.CreatedAt,
		UpdatedAt:                updatedAt,
	}
}
