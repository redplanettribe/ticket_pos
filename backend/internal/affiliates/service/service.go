// Package service holds the Affiliate Links business rules.
package service

import (
	"context"
	"crypto/rand"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/affiliates"
	"github.com/peter/ticket_pos/backend/internal/affiliates/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

const (
	// maxNameLength caps an Affiliate Link's display name (runes).
	maxNameLength = 80
	// codeLength is how many characters a generated code carries. Eight over the
	// 32-character alphabet below is ~1.1e12 codes per Event: short enough to
	// read out loud, long enough that guessing one is pointless.
	codeLength = 8
	// codeAttempts bounds the retry loop when a generated code collides with an
	// existing one on the same Event.
	codeAttempts = 5
)

// codeAlphabet is Crockford's base32: digits and uppercase letters with I, L, O
// and U removed, so a code survives being read off a phone screen and retyped —
// no 1/I, 0/O confusion — and stays URL-safe without escaping.
const codeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ActorContext is the acting Member for Affiliate Link operations.
type ActorContext struct {
	MemberID       string
	OrganizationID string
}

// AffiliateLinkView is one Affiliate Link as the staff section shows it.
//
// The shape is additive by design: clicks, attributed sales and Net Proceeds
// join these fields in later tickets, so clients read fields by name rather than
// expecting this exact set.
type AffiliateLinkView struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Code   string `json:"code"`
	Active bool   `json:"active"`
	// URL is the whole thing an organizer copies: the Storefront Event page with
	// the code as its ref. Derived at read time from the Storefront origin this
	// process is configured with, never stored, so moving the Storefront moves
	// every link with it.
	URL string `json:"url"`
	// SalesCount and NetProceedsCents are the link's Affiliate Attribution
	// figures: how many ACTIVE Ticket Sales it drove, and what they left the
	// Organization after the Platform Fee and its Fee IVA. Display-only — no
	// commission is computed from either — and a reversed sale drops out of both
	// however it was reversed. A link that drove only free claims shows its count
	// with zero beside it.
	SalesCount       int       `json:"sales_count"`
	NetProceedsCents int       `json:"net_proceeds_cents"`
	CreatedAt        time.Time `json:"created_at"`
}

// Service implements Affiliate Link operations.
type Service struct {
	repo *repository.Repository
	// storefrontBaseURL is the Storefront's own public origin (STOREFRONT_BASE_URL),
	// the same value Confirmation Links are built on.
	storefrontBaseURL string
	logger            platform.Logger
}

// New returns an Affiliate Links service.
func New(repo *repository.Repository, storefrontBaseURL string, logger platform.Logger) *Service {
	return &Service{
		repo:              repo,
		storefrontBaseURL: strings.TrimRight(storefrontBaseURL, "/"),
		logger:            logger,
	}
}

// CreateAffiliateLink adds a named Affiliate Link to an Event and generates its
// immutable code. The caller never chooses the code.
func (s *Service) CreateAffiliateLink(ctx context.Context, actor ActorContext, eventID, name string) (*AffiliateLinkView, error) {
	target, err := s.repo.GetLinkTarget(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, affiliates.ErrEventNotFound()
	}

	for attempt := 0; attempt < codeAttempts; attempt++ {
		code, err := generateCode()
		if err != nil {
			return nil, err
		}
		created, err := s.repo.Create(ctx, repository.AffiliateLink{
			EventID:        eventID,
			OrganizationID: actor.OrganizationID,
			Name:           name,
			Code:           code,
		})
		if errors.Is(err, repository.ErrCodeTaken) {
			continue
		}
		if err != nil {
			return nil, err
		}
		view := s.toView(*created, *target)
		return &view, nil
	}

	s.logger.Error("affiliate link code generation exhausted", "event_id", eventID)
	return nil, affiliates.ErrAffiliateLinkCodeExhausted()
}

// ListAffiliateLinks returns an Event's Affiliate Links, newest first.
func (s *Service) ListAffiliateLinks(ctx context.Context, actor ActorContext, eventID string) ([]AffiliateLinkView, error) {
	target, err := s.repo.GetLinkTarget(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, affiliates.ErrEventNotFound()
	}

	links, err := s.repo.ListByEventID(ctx, eventID)
	if err != nil {
		return nil, err
	}
	views := make([]AffiliateLinkView, 0, len(links))
	for _, link := range links {
		views = append(views, s.toView(link, *target))
	}
	return views, nil
}

// ResolveLiveCode answers who, if anybody, a checkout begun with this code
// should be attributed to: the id of the Event's ACTIVE Affiliate Link carrying
// it, or "" when no live link does.
//
// It is the whole of the backend's view of Affiliate Attribution's front half.
// Where the code came from — a cookie the Storefront kept for the Attribution
// Window, a hand-typed URL, a bookmark — is none of this module's business, and
// an unknown, mistyped or deactivated code is not an error: it means the sale is
// unattributed, and the buyer must never learn the difference (#146).
//
// Codes are drawn from an uppercase alphabet, so a code typed in lower case
// still finds its link.
func (s *Service) ResolveLiveCode(ctx context.Context, eventID, code string) (string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return "", nil
	}
	return s.repo.FindActiveLinkIDByCode(ctx, eventID, code)
}

// NormalizeName trims an Affiliate Link's display name and reports whether what
// is left is usable. Exported so the handler can refuse a blank or overlong name
// with a field-level validation error before the service is entered.
func NormalizeName(raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if name == "" || len([]rune(name)) > maxNameLength {
		return "", false
	}
	return name, true
}

// MaxNameLength is the display name cap, for the handler's error message.
const MaxNameLength = maxNameLength

func (s *Service) toView(link repository.AffiliateLink, target repository.LinkTarget) AffiliateLinkView {
	return AffiliateLinkView{
		ID:               link.ID,
		Name:             link.Name,
		Code:             link.Code,
		Active:           link.Active,
		URL:              s.storefrontURL(target, link.Code),
		SalesCount:       link.SalesCount,
		NetProceedsCents: link.NetProceedsCents,
		CreatedAt:        link.CreatedAt,
	}
}

// storefrontURL builds {storefrontBase}/{orgSlug}/events/{eventSlug}?ref=CODE.
func (s *Service) storefrontURL(target repository.LinkTarget, code string) string {
	path := "/" + url.PathEscape(target.OrganizationSlug) + "/events/" + url.PathEscape(target.EventSlug)
	return s.storefrontBaseURL + path + "?ref=" + url.QueryEscape(code)
}

// generateCode draws a code from the unambiguous alphabet using crypto/rand, so
// codes are neither sequential nor guessable from one another.
func generateCode() (string, error) {
	buf := make([]byte, codeLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, codeLength)
	for i, b := range buf {
		out[i] = codeAlphabet[int(b)%len(codeAlphabet)]
	}
	return string(out), nil
}
