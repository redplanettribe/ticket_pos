package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	catalogrepo "github.com/peter/ticket_pos/backend/internal/catalog/repository"
	"github.com/peter/ticket_pos/backend/internal/identity"
	"github.com/peter/ticket_pos/backend/internal/identity/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/storage"
)

const (
	otpLength            = 6
	otpExpiry            = 10 * time.Minute
	otpRateWindow        = 15 * time.Minute
	maxOTPPerEmail       = 3
	maxOTPPerIP          = 10
	maxOTPVerifyAttempts = 5
	sessionDuration      = 14 * 24 * time.Hour
)

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
	email       platform.EmailSender
	logger      platform.Logger
	now         func() time.Time
}

// New returns an identity service.
func New(
	repo *repository.Repository,
	catalogRepo *catalogrepo.Repository,
	objectStorage storage.ObjectStorage,
	email platform.EmailSender,
	logger platform.Logger,
) *Service {
	return &Service{
		repo:        repo,
		catalogRepo: catalogRepo,
		storage:     objectStorage,
		email:       email,
		logger:      logger,
		now:         time.Now,
	}
}

// WithClock overrides the clock (tests).
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// RequestOTP creates and delivers a one-time passcode.
func (s *Service) RequestOTP(ctx context.Context, email, clientIP string) (*OTPRequestResult, error) {
	email = normalizeEmail(email)
	now := s.now()

	since := now.Add(-otpRateWindow)
	emailCount, err := s.repo.CountOTPRequestsByEmail(ctx, email, since)
	if err != nil {
		return nil, err
	}
	if emailCount >= maxOTPPerEmail {
		return nil, identity.ErrOTPRateLimited()
	}

	ipCount, err := s.repo.CountOTPRequestsByIP(ctx, clientIP, since)
	if err != nil {
		return nil, err
	}
	if ipCount >= maxOTPPerIP {
		return nil, identity.ErrOTPRateLimited()
	}

	code, err := generateOTPCode()
	if err != nil {
		return nil, err
	}

	challengeID, err := newUUID()
	if err != nil {
		return nil, err
	}

	if err := s.repo.InvalidateOTPChallengesForEmail(ctx, email); err != nil {
		return nil, err
	}

	challenge := repository.OTPChallenge{
		ID:          challengeID,
		Email:       email,
		CodeHash:    hashOTPCode(challengeID, code),
		RequestIP:   clientIP,
		ExpiresAt:   now.Add(otpExpiry),
		Attempts:    0,
		Invalidated: false,
		CreatedAt:   now,
	}
	if err := s.repo.CreateOTPChallenge(ctx, challenge); err != nil {
		return nil, err
	}

	if err := s.email.SendOTP(ctx, email, code); err != nil {
		s.logger.Error("send otp failed", "email", email, "error", err)
		return nil, fmt.Errorf("send otp: %w", err)
	}

	return &OTPRequestResult{
		Message: "If an account exists for this email, a passcode has been sent.",
	}, nil
}

// VerifyOTP validates a passcode and creates a session.
func (s *Service) VerifyOTP(ctx context.Context, email, code string) (*SessionView, string, error) {
	email = normalizeEmail(email)
	now := s.now()

	challenge, err := s.repo.LatestOTPChallenge(ctx, email)
	if err != nil {
		return nil, "", err
	}
	if challenge == nil {
		return nil, "", identity.ErrOTPInvalid(0)
	}
	if challenge.Invalidated {
		return nil, "", identity.ErrOTPExpired()
	}
	if now.After(challenge.ExpiresAt) {
		_ = s.repo.InvalidateOTPChallenge(ctx, challenge.ID)
		return nil, "", identity.ErrOTPExpired()
	}
	if challenge.Attempts >= maxOTPVerifyAttempts {
		_ = s.repo.InvalidateOTPChallenge(ctx, challenge.ID)
		return nil, "", identity.ErrOTPAttemptsExceeded()
	}

	expected := hashOTPCode(challenge.ID, code)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(challenge.CodeHash)) != 1 {
		attempts, incErr := s.repo.IncrementOTPAttempts(ctx, challenge.ID)
		if incErr != nil {
			return nil, "", incErr
		}
		if attempts >= maxOTPVerifyAttempts {
			_ = s.repo.InvalidateOTPChallenge(ctx, challenge.ID)
			return nil, "", identity.ErrOTPAttemptsExceeded()
		}
		remaining := maxOTPVerifyAttempts - attempts
		return nil, "", identity.ErrOTPInvalid(remaining)
	}

	if err := s.repo.InvalidateOTPChallenge(ctx, challenge.ID); err != nil {
		return nil, "", err
	}

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
	if membership == nil || normalizeEmail(membership.Email) != session.Email {
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

	view := &SessionView{
		Email:       session.Email,
		Memberships: make([]MembershipView, 0, len(memberships)),
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

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func normalizeSlug(slug string) string {
	return strings.ToLower(strings.TrimSpace(slug))
}

func generateOTPCode() (string, error) {
	max := big.NewInt(1000000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func (s *Service) organizationLogoURL(logoImageKey sql.NullString) *string {
	if !logoImageKey.Valid || s.storage == nil {
		return nil
	}
	url := s.storage.PublicURL(logoImageKey.String)
	return &url
}

func hashOTPCode(challengeID, code string) string {
	sum := sha256.Sum256([]byte(challengeID + ":" + code))
	return hex.EncodeToString(sum[:])
}

func newSessionToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
