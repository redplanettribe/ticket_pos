package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"

	catalogrepo "github.com/peter/ticket_pos/backend/internal/catalog/repository"
	"github.com/peter/ticket_pos/backend/internal/identity"
	"github.com/peter/ticket_pos/backend/internal/identity/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/googleauth"
	"github.com/peter/ticket_pos/backend/internal/platform/otp"
	"github.com/peter/ticket_pos/backend/internal/platform/storage"
)

const sessionDuration = 14 * 24 * time.Hour

// otpPurpose scopes every passcode this service issues and verifies to staff
// sign-in. A passcode minted for any other surface must never open a Staff
// Session, so this constant is the only purpose identity ever names.
const otpPurpose = otp.PurposeStaff

// MembershipView is returned in session responses.
type MembershipView struct {
	MemberID            string  `json:"member_id"`
	OrganizationID      string  `json:"organization_id"`
	OrganizationName    string  `json:"organization_name"`
	OrganizationSlug    string  `json:"organization_slug"`
	OrganizationLogoURL *string `json:"organization_logo_url"`
	Role                string  `json:"role"`
}

// ActiveMemberView is the active organization context on a session.
type ActiveMemberView struct {
	MemberID            string  `json:"member_id"`
	OrganizationID      string  `json:"organization_id"`
	OrganizationName    string  `json:"organization_name"`
	OrganizationSlug    string  `json:"organization_slug"`
	OrganizationLogoURL *string `json:"organization_logo_url"`
	Role                string  `json:"role"`
}

// StaffMeView is what /api/v1/staff/me answers: the Active Member context the
// staff app resolves on every render, and the Staff Locale of the person doing
// the reading.
//
// The two are joined here rather than in two calls because the app already makes
// this one on every render, so the language arrives on a request that was
// happening anyway. They are joined here and NOT on ActiveMemberView because the
// Active Member is per-Organization and the Staff Locale is emphatically not:
// putting a language on the membership is the shape this feature exists to
// avoid, and this view is where the two facts meet without either claiming the
// other's scope.
type StaffMeView struct {
	MemberID            string  `json:"member_id"`
	OrganizationID      string  `json:"organization_id"`
	OrganizationName    string  `json:"organization_name"`
	OrganizationSlug    string  `json:"organization_slug"`
	OrganizationLogoURL *string `json:"organization_logo_url"`
	Role                string  `json:"role"`
	// Locale is the person's Staff Locale, or null when they have stated none.
	// Null is not "English": it says nobody has chosen, which is what lets the
	// next sign-in record a detected language instead of finding one already
	// there. A reader with null renders in English all the same.
	Locale *string `json:"locale"`
}

// StaffLocaleView is the Staff Locale of the signed-in person, as the write
// endpoint reports it back. Never null: a write always states one.
type StaffLocaleView struct {
	Locale string `json:"locale"`
}

// SessionView is the public session representation.
type SessionView struct {
	Email        string            `json:"email"`
	ActiveMember *ActiveMemberView `json:"active_member"`
	Memberships  []MembershipView  `json:"memberships"`
	// Locale is the Staff Locale of the person this session belongs to, or null
	// when they have stated none. It sits beside Email and IsPlatformOperator
	// because it is a fact about the PERSON, not about the Organization they are
	// currently looking at — which is also why it is reported here and not on
	// ActiveMember: a Platform Operator who is a Member of nothing has no Active
	// Member to hang it on, and still has a language.
	Locale *string `json:"locale"`
	// IsPlatformOperator says whether this session's email is on the platform
	// operator allowlist, so the staff app knows whether to render the Operator
	// Dashboard navigation. It is a hint for the UI, never the gate: the
	// operator middleware re-checks the allowlist on every request (ADR 0015).
	// It is orthogonal to Memberships — an operator may have none.
	IsPlatformOperator bool `json:"is_platform_operator"`
}

// OTPRequestResult is returned after requesting an OTP.
type OTPRequestResult struct {
	Message string `json:"message"`
}

// Service implements identity business rules.
type Service struct {
	repo        *repository.Repository
	catalogRepo *catalogrepo.Repository
	storage     storage.ObjectStorage
	otp         *otp.Service
	logger      platform.Logger
	now         func() time.Time
	// google redeems authorization codes against the STAFF OAuth client, and
	// only that one. It sits beside otp as the second Proof of Email Ownership
	// this service accepts, and like otp it establishes an email and nothing
	// more (ADR 0011).
	google *googleauth.Client
	// termsVersions is the sign-in gate's one read of the consent module: which
	// Terms edition is current (#538, ADR 0066). See TermsVersionSource on why
	// it is an interface and why it is this narrow.
	termsVersions TermsVersionSource
}

// New returns an identity service.
func New(
	repo *repository.Repository,
	catalogRepo *catalogrepo.Repository,
	objectStorage storage.ObjectStorage,
	otpService *otp.Service,
	logger platform.Logger,
	google *googleauth.Client,
	termsVersions TermsVersionSource,
) *Service {
	return &Service{
		repo:          repo,
		catalogRepo:   catalogRepo,
		storage:       objectStorage,
		otp:           otpService,
		logger:        logger,
		now:           time.Now,
		google:        google,
		termsVersions: termsVersions,
	}
}

// WithClock overrides the clock (tests). Passcode expiry is measured by the OTP
// service's own clock, so both move together.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	s.otp.WithClock(now)
	return s
}

// RequestOTP delivers a staff-purpose one-time passcode.
//
// The language is named here, explicitly, and it is THE RECIPIENT'S (#285,
// ADR 0041). It used to be English by decision — no Member had a locale, and
// apps/staff had no i18n at all, so Spanish mail linking into an English
// application would have been worse than consistency (ADR 0033). Both supports
// are gone, and the address a passcode is being sent to is the key the Staff
// Locale is stored under, so the one message somebody needs in order to reach
// the app at all is written in the language the app will then be in.
//
// It resolves against the ADDRESS and nothing else, which is the only thing
// available: a passcode request is anonymous by design, and the response body is
// identical for a known address and an unknown one. That is not weakened here.
// The Customer passcode deliberately does NOT read a stored language for this
// reason (see platform.ResolveMailLocale) — but the staff door differs on the
// fact that matters: its stored value is the language of the application this
// code opens, so writing the passcode in anything else would land somebody in a
// Spanish app holding an English email.
func (s *Service) RequestOTP(ctx context.Context, email, clientIP string) (*OTPRequestResult, error) {
	if err := s.otp.Issue(ctx, otpPurpose, email, clientIP, s.staffMailLocale(ctx, email)); err != nil {
		return nil, err
	}

	return &OTPRequestResult{
		Message: "If an account exists for this email, a passcode has been sent.",
	}, nil
}

// staffMailLocale is the language mail to a staff address is written in: the
// Staff Locale stored against it, and English underneath.
//
// A read that FAILS is English rather than an error, and that is the whole
// reason this is a method and not two lines at the call site. The passcode is
// the one email in this system that is not best-effort — a sign-in that could
// not deliver one is a sign-in that failed — and a database hiccup reading a
// preference must never be the thing that stops somebody signing in. It is
// logged and the floor applies.
func (s *Service) staffMailLocale(ctx context.Context, email string) platform.Locale {
	stored, err := s.StaffLocale(ctx, email)
	if err != nil {
		s.logger.Error("read staff locale for mail", "error", err)
		return platform.DefaultLocale
	}
	return platform.ResolveStaffLocale(stored)
}

// VerifyOTP validates a staff-purpose passcode and completes the sign-in: a
// Staff Session, or — for an email owing a Terms Acceptance of the current
// edition — a terms-required outcome instead (#538). A passcode issued for any
// other purpose is not accepted here.
//
// detectedLocale is the language the login page was rendered in, as the caller
// detected it. It is remembered as the person's Staff Locale if they have none,
// and ignored otherwise — see signInProvenEmail.
func (s *Service) VerifyOTP(ctx context.Context, email, code, detectedLocale string) (*SignInOutcome, error) {
	email = platform.NormalizeEmail(email)
	now := s.now()

	if err := s.otp.Verify(ctx, otpPurpose, email, code); err != nil {
		return nil, err
	}

	return s.signInProvenEmail(ctx, email, detectedLocale, now)
}

// signInProvenEmail is what every Proof of Email Ownership converges on: the
// Staff Session a proven email earns, and the auto-selection of a lone
// membership that decides where the app lands the person next.
//
// Both doors end here — a One-time Passcode and a Google Sign-In — because both
// assert the same fact and neither is worth more than the other (ADR 0011).
// Nothing recorded here says which one was used: no column, no field on the
// session view. That is also why the auth-fork cannot vary by sign-in method —
// by the time anything decides where to land somebody, the method is gone.
//
// detectedLocale is what the caller detected before anyone was signed in — a
// cookie, then an Accept-Language, then English — and this is the one moment it
// is worth writing down. A sign-in carries evidence that a box office sale does
// not (ADR 0033 left the Sale Locale absent precisely because there was none):
// the browser stated a preference, a login page was rendered in it, and the
// person read that page and proceeded. Weak evidence, and it is treated as weak
// — it is written only when the person has NO Staff Locale, and never over one
// (repository.RememberStaffLocale). That is what makes somebody's app and
// somebody's mail agree without them ever finding a setting.
//
// A language this platform does not serve, or none at all, records nothing: a
// missing or malformed locale must never fail a sign-in, so it is dropped
// through platform.ParseLocale rather than refused. Nor is failing to write it
// worth refusing a session over — the person is proven and the session is what
// they came for, so a write error is logged and the sign-in continues.
//
// THE TERMS GATE LIVES HERE, at the convergence, for the reason the customer
// consent gate lives at its own (#538, ADR 0066): both doors prove the same
// fact, so both owe the same question afterwards — has this email a Staff
// Terms Acceptance of the current Terms Version? A gate written into VerifyOTP
// would leave Google Sign-In an unguarded way past it. The check runs AFTER
// the proof and never before it, so the passcode request endpoint stays the
// non-oracle it is. A person gated here is minted NO SESSION: the locale is
// still remembered — that write costs them nothing and must survive an
// abandoned terms step — but the credential is withheld, and live sessions
// minted before the gate shipped are deliberately untouched.
//
// The email must already be normalised and proven by the caller.
func (s *Service) signInProvenEmail(ctx context.Context, email, detectedLocale string, now time.Time) (*SignInOutcome, error) {
	if parsed, ok := platform.ParseLocale(detectedLocale); ok {
		if err := s.repo.RememberStaffLocale(ctx, email, string(parsed), now); err != nil {
			s.logger.Error("remember staff locale", "error", err)
		}
	}

	required, err := s.gateOnTerms(ctx, email, now)
	if err != nil {
		return nil, err
	}
	if required != nil {
		return &SignInOutcome{TermsRequired: required}, nil
	}

	session, view, err := s.mintStaffSession(ctx, email, now)
	if err != nil {
		return nil, err
	}
	return &SignInOutcome{Session: view, SessionID: session.ID}, nil
}

// mintStaffSession issues the Staff Session a proven email earns, and is the
// one place that does: both sign-in doors reach it through signInProvenEmail,
// and the terms step reaches it directly when an acceptance finishes a sign-in
// that was held. The session an acceptance produces must be THE SAME session
// the sign-in would have produced — same window, same lone-membership
// auto-selection, same view on the wire — and one statement minting it is the
// only way to guarantee that (the customer door's mintSession, for its
// reasons).
func (s *Service) mintStaffSession(ctx context.Context, email string, now time.Time) (repository.Session, *SessionView, error) {
	sessionID, err := newSessionToken()
	if err != nil {
		return repository.Session{}, nil, err
	}

	session := repository.Session{
		ID:             sessionID,
		Email:          email,
		ActiveMemberID: sql.NullString{},
		ExpiresAt:      now.Add(sessionDuration),
		CreatedAt:      now,
	}

	memberships, err := s.repo.ListMemberships(ctx, email)
	if err != nil {
		return repository.Session{}, nil, err
	}
	if len(memberships) == 1 {
		session.ActiveMemberID = sql.NullString{String: memberships[0].MemberID, Valid: true}
	}

	if err := s.repo.CreateSession(ctx, session); err != nil {
		return repository.Session{}, nil, err
	}

	view, err := s.buildSessionView(ctx, &session)
	if err != nil {
		return repository.Session{}, nil, err
	}
	return session, view, nil
}

// GetSession loads and optionally extends a session.
func (s *Service) GetSession(ctx context.Context, sessionID string) (*SessionView, error) {
	session, err := s.loadActiveSession(ctx, sessionID, true)
	if err != nil {
		return nil, err
	}
	return s.buildSessionView(ctx, session)
}

// CreateOrganizationInput is the service input for organization creation.
type CreateOrganizationInput struct {
	Name string
	Slug string
}

// CreateOrganization creates an organization, org_admin member, and sets the session active member.
func (s *Service) CreateOrganization(ctx context.Context, sessionID string, input CreateOrganizationInput) (*SessionView, error) {
	session, err := s.loadActiveSession(ctx, sessionID, true)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(input.Name)
	slug := normalizeSlug(input.Slug)

	exists, err := s.repo.OrganizationSlugExists(ctx, slug)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, identity.ErrOrganizationSlugTaken(slug)
	}

	now := s.now()
	membership, err := s.repo.CreateOrganizationWithMember(ctx, session.ID, session.Email, name, slug, now)
	if err != nil {
		return nil, err
	}

	session.ActiveMemberID = sql.NullString{String: membership.MemberID, Valid: true}
	return s.buildSessionView(ctx, session)
}

// ListMemberships returns memberships for the authenticated session email.
func (s *Service) ListMemberships(ctx context.Context, sessionID string) ([]MembershipView, error) {
	session, err := s.loadActiveSession(ctx, sessionID, true)
	if err != nil {
		return nil, err
	}

	memberships, err := s.repo.ListMemberships(ctx, session.Email)
	if err != nil {
		return nil, err
	}

	views := make([]MembershipView, 0, len(memberships))
	for _, m := range memberships {
		views = append(views, MembershipView{
			MemberID:            m.MemberID,
			OrganizationID:      m.OrganizationID,
			OrganizationName:    m.OrganizationName,
			OrganizationSlug:    m.OrganizationSlug,
			OrganizationLogoURL: s.organizationLogoURL(m.OrganizationLogoKey),
			Role:                string(m.Role),
		})
	}
	return views, nil
}

// SelectOrganization sets the active member on the session.
func (s *Service) SelectOrganization(ctx context.Context, sessionID, memberID string) (*SessionView, error) {
	session, err := s.loadActiveSession(ctx, sessionID, true)
	if err != nil {
		return nil, err
	}

	memberID = strings.TrimSpace(memberID)
	if memberID == "" {
		return nil, identity.ErrMemberNotFound()
	}

	membership, err := s.repo.GetMembershipByID(ctx, memberID)
	if err != nil {
		return nil, err
	}
	if membership == nil || platform.NormalizeEmail(membership.Email) != session.Email {
		return nil, identity.ErrMemberNotFound()
	}

	if err := s.repo.SetActiveMember(ctx, sessionID, memberID); err != nil {
		return nil, err
	}

	session.ActiveMemberID = sql.NullString{String: memberID, Valid: true}
	return s.buildSessionView(ctx, session)
}

// GetStaffMe returns the active member context for staff workflow routes, and
// the Staff Locale of the person reading them.
func (s *Service) GetStaffMe(ctx context.Context, sessionID string) (*StaffMeView, error) {
	session, err := s.loadActiveSession(ctx, sessionID, true)
	if err != nil {
		return nil, err
	}
	if !session.ActiveMemberID.Valid {
		return nil, identity.ErrNoActiveMember()
	}

	view, err := s.buildSessionView(ctx, session)
	if err != nil {
		return nil, err
	}
	if view.ActiveMember == nil {
		return nil, identity.ErrNoActiveMember()
	}
	return &StaffMeView{
		MemberID:            view.ActiveMember.MemberID,
		OrganizationID:      view.ActiveMember.OrganizationID,
		OrganizationName:    view.ActiveMember.OrganizationName,
		OrganizationSlug:    view.ActiveMember.OrganizationSlug,
		OrganizationLogoURL: view.ActiveMember.OrganizationLogoURL,
		Role:                view.ActiveMember.Role,
		Locale:              view.Locale,
	}, nil
}

// SetStaffLocale records the language the signed-in person chose.
//
// It is keyed on the session's email and on nothing else, which is what makes
// this endpoint reachable by everybody who needs it: no Active Member is
// consulted, so a Platform Operator belonging to no Organization can set one,
// and an Event Staff member can set their own without an Org Admin's permission.
// A personal preference is not an Organization's setting.
//
// The locale must already be one this platform serves; the handler refuses
// anything else before this is called. That strictness is the opposite of the
// sign-in path's, and deliberately: a detected language is a guess worth
// dropping silently, a chosen one is a request that must not fail quietly.
func (s *Service) SetStaffLocale(ctx context.Context, sessionID string, locale platform.Locale) (*StaffLocaleView, error) {
	session, err := s.loadActiveSession(ctx, sessionID, true)
	if err != nil {
		return nil, err
	}

	if err := s.repo.SetStaffLocale(ctx, session.Email, string(locale), s.now()); err != nil {
		return nil, err
	}
	return &StaffLocaleView{Locale: string(locale)}, nil
}

// StaffLocale is the Staff Locale stored for an email address, or the empty
// string when nobody at that address has stated one.
//
// It takes an ADDRESS and not a session on purpose. Its callers outside this
// package compose mail, and the recipient of a Payout Request notice is a
// recorded email string attached to no Member id and no session — which is the
// whole reason this preference is keyed the way it is. The empty string is
// absence, not English: the caller applies the English floor itself.
func (s *Service) StaffLocale(ctx context.Context, email string) (string, error) {
	locale, found, err := s.repo.GetStaffLocale(ctx, platform.NormalizeEmail(email))
	if err != nil || !found {
		return "", err
	}
	return locale, nil
}

// Logout destroys a session.
func (s *Service) Logout(ctx context.Context, sessionID string) error {
	session, err := s.repo.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return identity.ErrSessionNotFound()
	}
	return s.repo.DeleteSession(ctx, sessionID)
}

func (s *Service) loadActiveSession(ctx context.Context, sessionID string, extend bool) (*repository.Session, error) {
	session, err := s.repo.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, identity.ErrSessionNotFound()
	}
	now := s.now()
	if now.After(session.ExpiresAt) {
		_ = s.repo.DeleteSession(ctx, sessionID)
		return nil, identity.ErrSessionExpired()
	}
	if extend {
		newExpiry := now.Add(sessionDuration)
		if err := s.repo.ExtendSession(ctx, sessionID, newExpiry); err != nil {
			return nil, err
		}
		session.ExpiresAt = newExpiry
	}
	return session, nil
}

func (s *Service) buildSessionView(ctx context.Context, session *repository.Session) (*SessionView, error) {
	memberships, err := s.repo.ListMemberships(ctx, session.Email)
	if err != nil {
		return nil, err
	}

	isOperator, err := s.repo.IsPlatformOperator(ctx, session.Email)
	if err != nil {
		return nil, err
	}

	locale, hasLocale, err := s.repo.GetStaffLocale(ctx, session.Email)
	if err != nil {
		return nil, err
	}

	view := &SessionView{
		Email:              session.Email,
		Memberships:        make([]MembershipView, 0, len(memberships)),
		IsPlatformOperator: isOperator,
	}
	if hasLocale {
		view.Locale = &locale
	}
	for _, m := range memberships {
		view.Memberships = append(view.Memberships, MembershipView{
			MemberID:            m.MemberID,
			OrganizationID:      m.OrganizationID,
			OrganizationName:    m.OrganizationName,
			OrganizationSlug:    m.OrganizationSlug,
			OrganizationLogoURL: s.organizationLogoURL(m.OrganizationLogoKey),
			Role:                string(m.Role),
		})
	}

	if session.ActiveMemberID.Valid {
		for _, m := range memberships {
			if m.MemberID == session.ActiveMemberID.String {
				view.ActiveMember = &ActiveMemberView{
					MemberID:            m.MemberID,
					OrganizationID:      m.OrganizationID,
					OrganizationName:    m.OrganizationName,
					OrganizationSlug:    m.OrganizationSlug,
					OrganizationLogoURL: s.organizationLogoURL(m.OrganizationLogoKey),
					Role:                string(m.Role),
				}
				break
			}
		}
	}

	return view, nil
}

func normalizeSlug(slug string) string {
	return strings.ToLower(strings.TrimSpace(slug))
}

func (s *Service) organizationLogoURL(logoImageKey sql.NullString) *string {
	if !logoImageKey.Valid || s.storage == nil {
		return nil
	}
	url := s.storage.PublicURL(logoImageKey.String)
	return &url
}

func newSessionToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
