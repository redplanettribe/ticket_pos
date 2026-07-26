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

// SessionView is the public session representation.
type SessionView struct {
	Email        string            `json:"email"`
	ActiveMember *ActiveMemberView `json:"active_member"`
	Memberships  []MembershipView  `json:"memberships"`
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
}

// New returns an identity service.
func New(
	repo *repository.Repository,
	catalogRepo *catalogrepo.Repository,
	objectStorage storage.ObjectStorage,
	otpService *otp.Service,
	logger platform.Logger,
	google *googleauth.Client,
) *Service {
	return &Service{
		repo:        repo,
		catalogRepo: catalogRepo,
		storage:     objectStorage,
		otp:         otpService,
		logger:      logger,
		now:         time.Now,
		google:      google,
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
func (s *Service) RequestOTP(ctx context.Context, email, clientIP string) (*OTPRequestResult, error) {
	if err := s.otp.Issue(ctx, otpPurpose, email, clientIP); err != nil {
		return nil, err
	}

	return &OTPRequestResult{
		Message: "If an account exists for this email, a passcode has been sent.",
	}, nil
}

// VerifyOTP validates a staff-purpose passcode and creates a Staff Session.
// A passcode issued for any other purpose is not accepted here.
func (s *Service) VerifyOTP(ctx context.Context, email, code string) (*SessionView, string, error) {
	email = platform.NormalizeEmail(email)
	now := s.now()

	if err := s.otp.Verify(ctx, otpPurpose, email, code); err != nil {
		return nil, "", err
	}

	return s.signInProvenEmail(ctx, email, now)
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
// The email must already be normalised and proven by the caller.
func (s *Service) signInProvenEmail(ctx context.Context, email string, now time.Time) (*SessionView, string, error) {
	sessionID, err := newSessionToken()
	if err != nil {
		return nil, "", err
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
		return nil, "", err
	}
	if len(memberships) == 1 {
		session.ActiveMemberID = sql.NullString{String: memberships[0].MemberID, Valid: true}
	}

	if err := s.repo.CreateSession(ctx, session); err != nil {
		return nil, "", err
	}

	view, err := s.buildSessionView(ctx, &session)
	if err != nil {
		return nil, "", err
	}
	return view, sessionID, nil
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

// GetStaffMe returns the active member context for staff workflow routes.
func (s *Service) GetStaffMe(ctx context.Context, sessionID string) (*ActiveMemberView, error) {
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
	return view.ActiveMember, nil
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

	view := &SessionView{
		Email:              session.Email,
		Memberships:        make([]MembershipView, 0, len(memberships)),
		IsPlatformOperator: isOperator,
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
