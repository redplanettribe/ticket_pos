package service

import (
	"context"
	"strings"
	"time"

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

// FollowView is one entry in the Customer's Follows, and its shape is a
// contract with the Follows still to come.
//
// `type` is the discriminator and is always present; the subject then hangs off
// the field named by it, so an Organization Follow carries `organization` and a
// Tag Follow (#218) will carry `tag`, each null on the other. That is why the
// subject is a pointer to a struct rather than an inlined set of columns: adding
// a kind adds a nullable field and changes nothing a client already reads.
//
// `followed_at` is the instant the Customer subscribed and is stable across
// repeats — following something already followed does not move it (see
// repository.FollowOrganization). It is what the list is ordered by.
type FollowView struct {
	Type         string                    `json:"type"`
	FollowedAt   time.Time                 `json:"followed_at"`
	Organization *FollowedOrganizationView `json:"organization,omitempty"`
}

// FollowsView is the whole of what a Customer Follows.
//
// A wrapping object rather than a bare array, because the bare array is the
// shape that cannot grow: this list will one day want a count, a paging cursor,
// or the Customer's Unsubscribe state beside it, and none of those can be added
// to a JSON array without breaking every caller.
type FollowsView struct {
	Follows []FollowView `json:"follows"`
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

// ListFollows returns everything the signed-in Customer Follows, most recent
// first.
//
// Today that is Organizations alone. The return shape is nonetheless the union's
// — a list of discriminated entries — so that Tag Follows (#218) extend this
// answer rather than adding a second one.
func (s *Service) ListFollows(ctx context.Context, token string) (*FollowsView, error) {
	customer, err := s.fullSessionCustomer(ctx, token)
	if err != nil {
		return nil, err
	}

	rows, err := s.repo.ListOrganizationFollowsForCustomer(ctx, customer.ID)
	if err != nil {
		return nil, err
	}

	views := make([]FollowView, 0, len(rows))
	for _, row := range rows {
		views = append(views, s.organizationFollowView(row))
	}
	return &FollowsView{Follows: views}, nil
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
	session, customer, err := s.authenticate(ctx, token)
	if err != nil {
		return nil, err
	}
	if session.TicketSaleID.Valid {
		return nil, customers.ErrFollowRequiresFullSession()
	}
	return customer, nil
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
