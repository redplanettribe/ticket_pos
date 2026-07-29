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
// The shape is additive by design: attributed sales and Net Proceeds join these
// fields in later tickets, so clients read fields by name rather than expecting
// this exact set.
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
	// Clicks is how many times the Event page was reached through this link —
	// raw visits, counted again every time, which is what makes a bad link
	// readable next to a bad audience.
	Clicks int64 `json:"clicks"`
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

// UpdateAffiliateLinkInput is a partial edit of an Affiliate Link: a rename, a
// change of circulation, or both. Both fields are optional pointers, so leaving
// one out leaves it alone — a rename never disturbs whether the link is live,
// and taking a link out of circulation never disturbs its label.
type UpdateAffiliateLinkInput struct {
	Name   *string
	Active *bool
}

// UpdateAffiliateLink renames an Affiliate Link, deactivates or reactivates it,
// or both at once.
//
// The code is untouchable here, and that is the point of the whole lifecycle: a
// deactivated link's code stops counting clicks (repository.RecordClick) and
// stops attributing at checkout (repository.FindActiveLinkIDByCode) while its
// history stays on the staff list marked inactive, and reactivating resumes both
// under the same code — the URLs a promoter published never stop being the same
// URLs.
func (s *Service) UpdateAffiliateLink(ctx context.Context, actor ActorContext, eventID, linkID string, input UpdateAffiliateLinkInput) (*AffiliateLinkView, error) {
	target, err := s.repo.GetLinkTarget(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, affiliates.ErrEventNotFound()
	}

	current, err := s.repo.GetByIDForEvent(ctx, eventID, linkID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, affiliates.ErrAffiliateLinkNotFound()
	}

	name, active := current.Name, current.Active
	if input.Name != nil {
		name = *input.Name
	}
	if input.Active != nil {
		active = *input.Active
	}

	updated, err := s.repo.UpdateNameAndActive(ctx, eventID, linkID, name, active)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, affiliates.ErrAffiliateLinkNotFound()
	}
	// The attribution figures come from the read above: this update moves neither
	// of them, and re-deriving them would only put a second, slower answer to the
	// same question in the response.
	updated.SalesCount = current.SalesCount
	updated.NetProceedsCents = current.NetProceedsCents

	view := s.toView(*updated, *target)
	return &view, nil
}

// DeleteAffiliateLink removes an Affiliate Link that has done nothing.
//
// A link is deletable only while it has zero clicks, no attributed Ticket Sale
// of any status, and no pending checkout that could still become one: a mistyped
// link created a minute ago can be taken back, and anything that actually
// happened is kept. A reversed attributed sale counts as history like any other
// — it still names the link that drove it — so the way to retire a link that
// worked is to deactivate it, which is what the refusal says.
//
// A checkout that was abandoned or declined is not history: nobody bought
// anything, and the Payment stops naming the link when it goes
// (repository.DeleteIfNoHistory).
func (s *Service) DeleteAffiliateLink(ctx context.Context, actor ActorContext, eventID, linkID string) error {
	target, err := s.repo.GetLinkTarget(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return err
	}
	if target == nil {
		return affiliates.ErrEventNotFound()
	}

	link, err := s.repo.GetByIDForEvent(ctx, eventID, linkID)
	if err != nil {
		return err
	}
	if link == nil {
		return affiliates.ErrAffiliateLinkNotFound()
	}

	deleted, err := s.repo.DeleteIfNoHistory(ctx, eventID, linkID)
	if err != nil {
		return err
	}
	if !deleted {
		// The link was there a statement ago, so what stopped the delete is its
		// history — the condition the delete carries with it.
		return affiliates.ErrAffiliateLinkHasHistory()
	}
	return nil
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

// RecordClick counts one visit to an Event page reached through an Affiliate
// Link's code. Repeat visits count again: this is a raw click counter, with no
// dedup and no visitor identification.
//
// A code that matches nothing live is a no-op, not an error. The caller is a
// buyer's page load, and there is nothing a buyer could do about a dead code in
// a URL somebody else published — the page renders and nothing is counted. A
// database failure is returned so it can be logged, never shown.
//
// The code is normalized exactly as ResolveLiveCode normalizes it, and for the
// same reason: one visit through one ref must count a click on the link it
// later attributes the sale to. A ref retyped in lower case is a real visit
// through a real link, and the two verdicts may never disagree about it.
func (s *Service) RecordClick(ctx context.Context, organizationSlug, eventSlug, code string) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	if organizationSlug == "" || eventSlug == "" || code == "" {
		return nil
	}
	if err := s.repo.RecordClick(ctx, organizationSlug, eventSlug, code); err != nil {
		s.logger.Error("affiliate link click not recorded", "code", code, "error", err)
		return err
	}
	return nil
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
		Clicks:           link.ClickCount,
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
