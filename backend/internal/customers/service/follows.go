package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/customers"
	"github.com/peter/ticket_pos/backend/internal/customers/repository"
)

// The Follow (#217, parent #215): a Customer's standing subscription to an
// Organization, entitling them to hear about the Events that fall under it
// (CONTEXT.md). Its whole payload is the Follow Digest (ADR 0030) — nothing here
// sends any mail, and nothing here is a shortlist.
//
// A Follow belongs to the Customer rather than to a browser, which is why every
// function below is scoped by a Customer Session and by nothing else. There is
// no Customer id, email, or any other identifier in any of these signatures that
// a request could supply.

// FollowSubjectOrganization is the `type` discriminator an Organization Follow
// carries on the wire.
//
// The listing endpoint is one list of everything a Customer Follows, not one
// list per kind, and this string is how a client tells the entries apart. Tag
// Follows (#218) join the same list under their own value, so a Storefront that
// switches on this field today keeps working when they arrive; a second endpoint
// would have made every consumer union two calls and reconcile two orderings.
const FollowSubjectOrganization = "organization"

// FollowSubjectTag is the `type` discriminator a Tag Follow carries on the wire
// (#218). The second value of the union the constant above opened.
const FollowSubjectTag = "tag"

// FollowedOrganizationView is the Organization as one of the Customer's Follows
// shows it: exactly the three facts its public profile publishes.
//
// It carries no id on purpose. A Follow is addressed by slug — the Organization
// is named by slug in every Storefront address and on its own public endpoint —
// so an internal identifier here would be a fact this surface does not need and
// a client could come to depend on.
type FollowedOrganizationView struct {
	Name    string  `json:"name"`
	Slug    string  `json:"slug"`
	LogoURL *string `json:"logo_url"`
}

// FollowedTagView is the Tag as one of the Customer's Follows shows it: the same
// three facts catalog's TagView publishes everywhere else a Tag appears.
//
// The canonical key is the Tag's identity here in the way the slug is the
// Organization's — it is what the Follow and Unfollow routes name a Tag by, and
// it is what the Storefront keys a Preset Tag's translated copy on, so a copy
// edit to the display name cannot silently unword a chip (ADR 0027). Name is the
// English display name and is the fallback for every Tag the catalogue does not
// know, which is every Custom Tag.
//
// `curated` is on the wire because a client cannot otherwise tell which of those
// two it is holding: a Preset Tag is worded from the catalogue, a Custom Tag is
// rendered exactly as the Organization coined it. It says nothing about whether
// the Tag may be Followed — every Tag may (ADR 0030).
//
// No id, for the reason FollowedOrganizationView carries none.
type FollowedTagView struct {
	CanonicalKey string `json:"canonical_key"`
	Name         string `json:"name"`
	Curated      bool   `json:"curated"`
}

// FollowView is one entry in the Customer's Follows.
//
// `type` is the discriminator and is always present; the subject then hangs off
// the field named by it, so an Organization Follow carries `organization` and a
// Tag Follow carries `tag`, each absent on the other. That is why each subject is
// a pointer to its own struct rather than an inlined set of columns: the two
// kinds cannot collide, and a third would add a field rather than move one.
//
// `followed_at` is the instant the Customer subscribed and is stable across
// repeats — following something already followed does not move it (see
// repository.FollowOrganization). It is what the list is ordered by.
type FollowView struct {
	Type         string                    `json:"type"`
	FollowedAt   time.Time                 `json:"followed_at"`
	Organization *FollowedOrganizationView `json:"organization,omitempty"`
	Tag          *FollowedTagView          `json:"tag,omitempty"`
}

// FollowsView is the whole of what a Customer Follows.
//
// A wrapping object rather than a bare array, because the bare array is the
// shape that cannot grow: this list wanted a count, a paging cursor, or the
// Customer's Unsubscribe state beside it, and none of those can be added to a
// JSON array without breaking every caller. The last of those is now here.
type FollowsView struct {
	Follows []FollowView `json:"follows"`
	// DigestEnabled is whether the Follow Digest is switched on for this
	// Customer (#224, ADR 0030).
	//
	// IT RIDES BESIDE THE FOLLOWS RATHER THAN ON AN ENDPOINT OF ITS OWN, and that
	// is the point of putting it here. The Customer Area has one sentence to say
	// — "the Digest is off, and everything you Follow still stands" — and it is
	// one screen; two reads that could disagree is how a Following page comes to
	// show an empty list beside a switch that says the mail is on, or a full list
	// beside a switch that has not caught up. One read answers both halves, so
	// they cannot skew.
	//
	// It is also the surface the criterion is written against: unsubscribing must
	// leave every Follow "intact and visible in the Customer Area", and this is
	// the response that proves both at once.
	DigestEnabled bool `json:"digest_enabled"`
}

// OrganizationResolver turns the slug a Customer sees into the Organization id a
// Follow is stored against.
//
// Declared here, on the side that calls it, and implemented by identity — which
// owns Organizations — in the same shape as ReversalRequestResolver above. It is
// deliberately the narrowest statement of the need: one slug in, one id out, no
// Organization view to render and no way to reach anything else about it. The
// customers module owns who is following and when; it does not own what an
// Organization is.
//
// An unknown slug comes back as identity's own ORGANIZATION_NOT_FOUND, which the
// shared HTTP mapping already turns into a 404 — the same answer the public
// Organization profile gives for the same slug, so a Customer cannot learn from
// the Follow endpoint that an Organization exists which the public endpoint
// denies.
type OrganizationResolver interface {
	ResolveOrganizationIDBySlug(ctx context.Context, slug string) (string, error)
}

// WithOrganizations attaches the resolver that turns an Organization slug into
// an id. Same chaining shape as WithReversalRequests, and wired after
// construction for the same reason: the identity service is built alongside this
// one and the dependency is additive.
//
// Unlike the reversal resolver this one is not optional in effect — a service
// without it cannot follow anything, and says so plainly rather than following
// nothing quietly. See requireOrganizations.
func (s *Service) WithOrganizations(resolver OrganizationResolver) *Service {
	s.organizations = resolver
	return s
}

// TagResolver turns the canonical key a Customer sees into the Tag id a Follow
// is stored against (#218).
//
// The same shape as OrganizationResolver above and implemented by catalog, which
// owns the shared Tag pool. Narrow for the same reason: one key in, one id out.
// Everything the key means on the way in — that it is canonicalized exactly as
// every other path into the pool canonicalizes, and that an unknown one is
// TAG_NOT_FOUND rather than a Tag quietly coined — belongs to whoever owns Tags,
// and stating it here in the customers module would be a second copy of a rule
// that has to stay one.
//
// The Tag is addressed by canonical key rather than by display name for the same
// reason the Organization is addressed by slug: it is the stable machine
// identity a Storefront already holds, and it survives a copy edit to the name.
type TagResolver interface {
	ResolveTagIDByCanonicalKey(ctx context.Context, canonicalKey string) (string, error)
}

// WithTags attaches the resolver that turns a Tag's canonical key into an id.
// Same chaining shape and same effect when absent as WithOrganizations: a
// service without it cannot follow a Tag, and says so plainly.
func (s *Service) WithTags(resolver TagResolver) *Service {
	s.tags = resolver
	return s
}

// FollowOrganization records that the signed-in Customer Follows the
// Organization named by slug, and returns the Follow as it now stands.
//
// It is idempotent by construction: following something already followed returns
// the existing Follow, with the instant they first subscribed rather than a new
// one. So the endpoint answers 200 rather than 201 and never 409 — a repeat is
// not a conflict, it is the same request arriving twice with the same meaning.
func (s *Service) FollowOrganization(ctx context.Context, token, slug string) (*FollowView, error) {
	customer, organizationID, err := s.followTarget(ctx, token, slug)
	if err != nil {
		return nil, err
	}

	followedAt, err := s.repo.FollowOrganization(ctx, customer.ID, organizationID, s.now().UTC())
	if err != nil {
		return nil, err
	}

	row, err := s.repo.GetOrganizationFollow(ctx, customer.ID, organizationID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		// The Organization was deleted between the write and the read back; its
		// Follows went with it (the FK cascades). Nothing was orphaned, and the
		// slug the caller named no longer belongs to anything.
		return nil, customers.ErrFollowedOrganizationNotFound()
	}
	row.FollowedAt = followedAt

	view := s.organizationFollowView(*row)
	return &view, nil
}

// UnfollowOrganization removes the signed-in Customer's Follow of the
// Organization named by slug.
//
// Unfollowing something not followed is not an error. The caller asked for a
// state — "I do not Follow this" — and that state already holds; reporting it as
// a failure would make the Storefront's own retry, or a second tap on a control
// that had already been pressed, look like something went wrong.
func (s *Service) UnfollowOrganization(ctx context.Context, token, slug string) error {
	customer, organizationID, err := s.followTarget(ctx, token, slug)
	if err != nil {
		return err
	}
	return s.repo.UnfollowOrganization(ctx, customer.ID, organizationID)
}

// FollowTag records that the signed-in Customer Follows the Tag named by its
// canonical key, and returns the Follow as it now stands.
//
// Idempotent exactly as FollowOrganization is, and answering 200 for the same
// reason. Any Tag may be Followed, Preset or Custom: ADR 0030 settled that the
// weekly Digest's cap — not a narrower pool — is what bounds how much mail a
// Follow can produce.
func (s *Service) FollowTag(ctx context.Context, token, canonicalKey string) (*FollowView, error) {
	customer, tagID, err := s.tagFollowTarget(ctx, token, canonicalKey)
	if err != nil {
		return nil, err
	}

	followedAt, err := s.repo.FollowTag(ctx, customer.ID, tagID, s.now().UTC())
	if err != nil {
		return nil, err
	}

	row, err := s.repo.GetTagFollow(ctx, customer.ID, tagID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		// The Tag was deleted between the write and the read back; its Follows
		// went with it (the FK cascades). Nothing was orphaned, and the key the
		// caller named no longer belongs to anything.
		return nil, catalog.ErrTagNotFound()
	}
	row.FollowedAt = followedAt

	view := tagFollowView(*row)
	return &view, nil
}

// UnfollowTag removes the signed-in Customer's Follow of the Tag named by its
// canonical key. Unfollowing something not followed is not an error, as with
// Organizations.
func (s *Service) UnfollowTag(ctx context.Context, token, canonicalKey string) error {
	customer, tagID, err := s.tagFollowTarget(ctx, token, canonicalKey)
	if err != nil {
		return err
	}
	return s.repo.UnfollowTag(ctx, customer.ID, tagID)
}

// ListFollows returns everything the signed-in Customer Follows, most recent
// first — Organizations and Tags in ONE list rather than one list per kind.
//
// One list because that is what the Customer has: a Following surface reads this
// once and renders it top to bottom, where two endpoints would have made every
// consumer issue two calls, union them, and re-derive an order that has to match
// whatever this one would have been. The kinds are told apart by `type`, and a
// third kind extends this answer rather than adding another endpoint.
//
// The two halves come back already ordered from their own queries and are merged
// here (see mergeFollows) rather than unioned in SQL. A UNION would have had to
// flatten two differently-shaped subjects into one row of nullable columns, so
// the shape the API publishes would have been dictated by the convenience of a
// single query — and it would still have needed the tie-break spelled out.
func (s *Service) ListFollows(ctx context.Context, token string) (*FollowsView, error) {
	customer, err := s.fullSessionCustomer(ctx, token)
	if err != nil {
		return nil, err
	}

	organizationRows, err := s.repo.ListOrganizationFollowsForCustomer(ctx, customer.ID)
	if err != nil {
		return nil, err
	}
	tagRows, err := s.repo.ListTagFollowsForCustomer(ctx, customer.ID)
	if err != nil {
		return nil, err
	}

	views := make([]FollowView, 0, len(organizationRows)+len(tagRows))
	for _, row := range organizationRows {
		views = append(views, s.organizationFollowView(row))
	}
	for _, row := range tagRows {
		views = append(views, tagFollowView(row))
	}
	mergeFollows(views)

	// The Digest switch comes from the Customer already authenticated above rather
	// than from a query of its own, so the list and the switch are read in one
	// pass and cannot disagree (#224).
	return &FollowsView{Follows: views, DigestEnabled: customer.DigestEnabled}, nil
}

// mergeFollows puts the two kinds into one order, in place.
//
// `followed_at` descending is the rule #217 established and it does not change:
// the list is a record of decisions in the order they were made, and the newest
// is what a person who has just made one is looking for.
//
// The rest of the comparison exists because that rule alone is not a total
// order, and two Follows sharing an instant are ordinary rather than exotic —
// two presses in one request-processing second, a fixed clock in tests. Without
// a tie-break the same list could come back in two orders for two identical
// reads, which a Following page re-rendering would show as a shuffle.
//
// Ties break on the identifier each kind is addressed by — the Organization's
// slug, the Tag's canonical key — which is exactly what #217 did within
// Organizations, so an Organization-only list is ordered today as it was before
// Tags existed. Where those are equal across kinds the discriminator settles it,
// and that last step is only reachable when a slug and a canonical key are the
// same string; it is here so that the order is total rather than nearly so.
//
// sort.SliceStable, though the comparison is total: the stability costs nothing
// and means a future kind added without a thought for the tie-break degrades to
// "in the order the queries returned it" rather than to an arbitrary shuffle.
func mergeFollows(views []FollowView) {
	sort.SliceStable(views, func(i, j int) bool {
		a, b := views[i], views[j]
		if !a.FollowedAt.Equal(b.FollowedAt) {
			return a.FollowedAt.After(b.FollowedAt)
		}
		if key := followSortKey(a); key != followSortKey(b) {
			return key < followSortKey(b)
		}
		return a.Type < b.Type
	})
}

// followSortKey is the identifier a Follow is addressed by, whichever kind it
// is: the one string a Customer could point at to unfollow it.
func followSortKey(view FollowView) string {
	switch {
	case view.Organization != nil:
		return view.Organization.Slug
	case view.Tag != nil:
		return view.Tag.CanonicalKey
	default:
		return ""
	}
}

// followTarget authenticates the caller and resolves the slug they named, which
// is the preamble both writes share.
func (s *Service) followTarget(ctx context.Context, token, slug string) (*repository.Customer, string, error) {
	customer, err := s.fullSessionCustomer(ctx, token)
	if err != nil {
		return nil, "", err
	}
	if s.organizations == nil {
		return nil, "", customers.ErrFollowsUnavailable()
	}

	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return nil, "", customers.ErrFollowedOrganizationNotFound()
	}
	organizationID, err := s.organizations.ResolveOrganizationIDBySlug(ctx, slug)
	if err != nil {
		return nil, "", err
	}
	return customer, organizationID, nil
}

// fullSessionCustomer is the gate every Follow route sits behind: a FULL
// Customer Session, never a sale-scoped one.
//
// The distinction is the whole of the check and it is worth stating, because the
// obvious reading — "an active Customer Session implies a verified email, so
// ADR 0010 is satisfied structurally" — is true of a full session and false of
// the other kind. A sale-scoped session is minted by redeeming a Confirmation
// Link (service/confirmationlink.go), which is possession of an email somebody
// was sent and nothing more; it does not mark the Customer verified and it
// proves nothing about who controls the address. A Follow is a request to be
// written to, repeatedly and unbidden, so honouring one from a forwarded receipt
// would let a stranger sign somebody else's inbox up for mail — exactly what
// ADR 0010 forbids and what ADR 0030's Digest would then act on.
//
// This is a session-scope check and not a second email-verification check: a
// full Customer Session can only have been minted by Proof of Email Ownership,
// so `verified_at` is already implied by it. The refusal is the same
// CUSTOMER_SESSION_SCOPE_INSUFFICIENT the profile edit and the undo return, for
// the same reason and with 403 rather than 401 — the credential is genuine,
// re-presenting it will never help, and a wider one is what would.
//
// The reading is gated as well as the writes. What a person Follows is a
// standing statement of their interests, and a forwarded confirmation is not
// authority to read it.
func (s *Service) fullSessionCustomer(ctx context.Context, token string) (*repository.Customer, error) {
	_, customer, err := s.fullSession(ctx, token)
	return customer, err
}

// fullSession is fullSessionCustomer for a caller that also needs the SESSION —
// one caller today, the Customer Area's Digest toggle, whose Consent Record has
// to name the session the act was made under as its evidence (#256).
//
// Split out rather than widened in place so the gate itself stays one piece of
// code: every caller here is refused by the same three lines, and a second copy
// of "reject a Confirmation Link session" is how one surface eventually forgets.
func (s *Service) fullSession(ctx context.Context, token string) (*repository.CustomerSession, *repository.Customer, error) {
	session, customer, err := s.authenticate(ctx, token)
	if err != nil {
		return nil, nil, err
	}
	if session.TicketSaleID.Valid {
		return nil, nil, customers.ErrFollowRequiresFullSession()
	}
	return session, customer, nil
}

// tagFollowTarget authenticates the caller and resolves the canonical key they
// named, which is the preamble both Tag writes share.
//
// It does not canonicalize the key itself. Trimming and lowercasing here would
// be a second copy of a rule the Tag pool already owns, and the two copies would
// only have to agree; the resolver runs the pool's own function, so a Tag
// reached as "  MUSIC " and as "music" is one Follow because it is one Tag.
func (s *Service) tagFollowTarget(ctx context.Context, token, canonicalKey string) (*repository.Customer, string, error) {
	customer, err := s.fullSessionCustomer(ctx, token)
	if err != nil {
		return nil, "", err
	}
	if s.tags == nil {
		return nil, "", customers.ErrFollowsUnavailable()
	}

	tagID, err := s.tags.ResolveTagIDByCanonicalKey(ctx, canonicalKey)
	if err != nil {
		return nil, "", err
	}
	return customer, tagID, nil
}

// tagFollowView is not a method, unlike organizationFollowView beside it: an
// Organization's logo has to be turned into a URL by the object storage the
// service holds, and a Tag has nothing that needs the service at all.
func tagFollowView(row repository.TagFollowRow) FollowView {
	return FollowView{
		Type:       FollowSubjectTag,
		FollowedAt: row.FollowedAt,
		Tag: &FollowedTagView{
			CanonicalKey: row.CanonicalKey,
			Name:         row.DisplayName,
			Curated:      row.Curated,
		},
	}
}

func (s *Service) organizationFollowView(row repository.OrganizationFollowRow) FollowView {
	organization := &FollowedOrganizationView{Name: row.Name, Slug: row.Slug}
	if row.LogoImageKey.Valid && s.storage != nil {
		url := s.storage.PublicURL(row.LogoImageKey.String)
		organization.LogoURL = &url
	}
	return FollowView{
		Type:         FollowSubjectOrganization,
		FollowedAt:   row.FollowedAt,
		Organization: organization,
	}
}
