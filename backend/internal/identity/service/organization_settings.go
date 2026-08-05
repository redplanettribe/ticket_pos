package service

import (
	"context"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/identity"
	"github.com/peter/ticket_pos/backend/internal/identity/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/storage"
)

// OrganizationView is the public organization profile.
type OrganizationView struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Slug           string    `json:"slug"`
	Currency       string    `json:"currency"`
	CurrencyLocked bool      `json:"currency_locked"`
	LogoURL        *string   `json:"logo_url"`
	// SupportWhatsApp is the Organization's Support WhatsApp number in canonical
	// E.164 form, null when it has none. Shown here so an Org Admin can see,
	// correct and withdraw the number the platform publishes on their behalf.
	SupportWhatsApp *string   `json:"support_whatsapp"`
	CreatedAt       time.Time `json:"created_at"`
}

// PublicOrganizationView is the unauthenticated organization profile.
type PublicOrganizationView struct {
	Name    string  `json:"name"`
	Slug    string  `json:"slug"`
	LogoURL *string `json:"logo_url"`
}

// UpdateOrganizationInput updates organization profile fields.
//
// SupportWhatsApp is presence-keyed rather than a plain pointer, because on this
// field an absent key and a null one must mean opposite things: a request that
// never mentions the number leaves it exactly where it is, and one that sends it
// null or blank clears it. SupportWhatsAppSet carries "the request talked about
// it at all"; SupportWhatsApp then carries the verdict, nil meaning clear. The
// same split the Customer profile's phone number runs, for the same reason.
type UpdateOrganizationInput struct {
	Name               string
	Currency           *string
	LogoImageKey       *string
	SupportWhatsAppSet bool
	SupportWhatsApp    *string
}

// CreateLogoUploadURLInput describes a requested organization logo upload.
type CreateLogoUploadURLInput struct {
	ContentType string
	FileName    string
}

// MemberView is a member in the organization roster.
type MemberView struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// EventAssignmentView is an event assignment with member email.
type EventAssignmentView struct {
	ID        string    `json:"id"`
	MemberID  string    `json:"member_id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// ActiveMemberContext is the acting member for org-admin operations.
type ActiveMemberContext struct {
	MemberID       string
	OrganizationID string
	Role           repository.MemberRole
	Email          string
}

// DeleteOrganizationInput confirms organization deletion.
type DeleteOrganizationInput struct {
	ConfirmationName string
}

// AddMemberInput pre-provisions a member by email.
type AddMemberInput struct {
	Email string
	Role  string
}

// UpdateMemberRoleInput changes a member's org-level role.
type UpdateMemberRoleInput struct {
	Role string
}

// UpsertEventAssignmentInput sets a member's per-event role.
type UpsertEventAssignmentInput struct {
	Role string
}

// GetOrganization returns the active member's organization profile.
func (s *Service) GetOrganization(ctx context.Context, actor ActiveMemberContext) (*OrganizationView, error) {
	if actor.Role != repository.RoleOrgAdmin {
		return nil, identity.ErrForbidden()
	}

	org, err := s.repo.GetOrganizationByID(ctx, actor.OrganizationID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, identity.ErrOrganizationNotFound()
	}
	return s.toOrganizationView(ctx, org)
}

// UpdateOrganization updates organization profile fields.
func (s *Service) UpdateOrganization(ctx context.Context, actor ActiveMemberContext, input UpdateOrganizationInput) (*OrganizationView, error) {
	if actor.Role != repository.RoleOrgAdmin {
		return nil, identity.ErrForbidden()
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, identity.ErrOrganizationNotFound()
	}

	org, err := s.repo.GetOrganizationByID(ctx, actor.OrganizationID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, identity.ErrOrganizationNotFound()
	}

	updated := org
	if name != org.Name {
		updated, err = s.repo.UpdateOrganizationName(ctx, actor.OrganizationID, name)
		if err != nil {
			return nil, err
		}
		if updated == nil {
			return nil, identity.ErrOrganizationNotFound()
		}
	}

	if input.Currency != nil {
		currency := strings.ToUpper(strings.TrimSpace(*input.Currency))
		if currency != org.Currency {
			hasTypes, err := s.catalogRepo.OrganizationHasTicketTypes(ctx, actor.OrganizationID)
			if err != nil {
				return nil, err
			}
			if hasTypes {
				return nil, catalog.ErrCurrencyLocked()
			}

			updated, err = s.repo.UpdateOrganizationCurrency(ctx, actor.OrganizationID, currency)
			if err != nil {
				return nil, err
			}
			if updated == nil {
				return nil, identity.ErrOrganizationNotFound()
			}
		}
	}

	if input.LogoImageKey != nil {
		key := strings.TrimSpace(*input.LogoImageKey)
		if key != "" && !storage.LogoKeyBelongsToOrg(key, actor.OrganizationID) {
			return nil, identity.ErrInvalidLogoImageKey()
		}

		updated, err = s.repo.UpdateOrganizationLogoKey(ctx, actor.OrganizationID, input.LogoImageKey)
		if err != nil {
			return nil, err
		}
		if updated == nil {
			return nil, identity.ErrOrganizationNotFound()
		}
	}

	// The Support WhatsApp number. Whether the request is talking about it at all
	// is decided by the key's presence, and only then by its value: a blank or
	// null withdraws the published number, which is a capability the Org Admin is
	// entitled to rather than an empty form nobody filled in. The number itself
	// arrives already canonicalized — the handler runs it through the one shared
	// phone rule, as every surface taking a phone number does.
	//
	// The write is skipped when the number is already what the request asks for,
	// the way the name above is. The Staff form always sends this key, so without
	// the comparison every unrelated profile save — a rename, a currency change —
	// would rewrite this column for nothing.
	if input.SupportWhatsAppSet {
		current := ""
		if org.SupportWhatsApp.Valid {
			current = org.SupportWhatsApp.String
		}
		wanted := ""
		if input.SupportWhatsApp != nil {
			wanted = *input.SupportWhatsApp
		}
		if wanted != current {
			updated, err = s.repo.UpdateOrganizationSupportWhatsApp(ctx, actor.OrganizationID, input.SupportWhatsApp)
			if err != nil {
				return nil, err
			}
			if updated == nil {
				return nil, identity.ErrOrganizationNotFound()
			}
		}
	}

	return s.toOrganizationView(ctx, updated)
}

// CreateLogoUploadURL returns a presigned PUT URL for an organization logo.
func (s *Service) CreateLogoUploadURL(ctx context.Context, actor ActiveMemberContext, input CreateLogoUploadURLInput) (*storage.CoverUploadResult, error) {
	if actor.Role != repository.RoleOrgAdmin {
		return nil, identity.ErrForbidden()
	}
	if s.storage == nil {
		return nil, identity.ErrLogoUploadUnavailable()
	}

	contentType := strings.ToLower(strings.TrimSpace(input.ContentType))
	if !storage.CoverContentTypeAllowed(contentType) {
		return nil, identity.ErrInvalidLogoImageKey()
	}

	key, err := storage.BuildLogoObjectKey(actor.OrganizationID, contentType, input.FileName)
	if err != nil {
		return nil, err
	}

	uploadURL, err := s.storage.PresignPut(ctx, key, contentType, 15*time.Minute)
	if err != nil {
		return nil, err
	}

	return &storage.CoverUploadResult{
		UploadURL: uploadURL,
		ObjectKey: key,
		PublicURL: s.storage.PublicURL(key),
	}, nil
}

// GetPublicOrganization returns the unauthenticated organization profile by slug.
func (s *Service) GetPublicOrganization(ctx context.Context, slug string) (*PublicOrganizationView, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return nil, identity.ErrOrganizationNotFound()
	}

	org, err := s.repo.GetOrganizationBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, identity.ErrOrganizationNotFound()
	}

	return &PublicOrganizationView{
		Name:    org.Name,
		Slug:    org.Slug,
		LogoURL: s.organizationLogoURL(org.LogoImageKey),
	}, nil
}

// ResolveOrganizationIDBySlug turns the public slug of an Organization into its
// id, and is this module's implementation of the customers module's
// OrganizationResolver (#217).
//
// It is a seam rather than an exported repository call because the slug rule —
// lowercased, trimmed, and an unknown one being ORGANIZATION_NOT_FOUND rather
// than an empty answer — belongs to whoever owns Organizations. A Follow of an
// unknown slug therefore fails exactly as GetPublicOrganization above does, with
// the same code and the same 404: the two must agree, or the Follow endpoint
// becomes a way to discover Organizations the public profile will not confirm.
//
// It returns the id and nothing else deliberately. The caller stores a foreign
// key; it has no business with the Organization's name, logo, or settings, and
// this signature is the smallest thing that could serve it.
func (s *Service) ResolveOrganizationIDBySlug(ctx context.Context, slug string) (string, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return "", identity.ErrOrganizationNotFound()
	}

	org, err := s.repo.GetOrganizationBySlug(ctx, slug)
	if err != nil {
		return "", err
	}
	if org == nil {
		return "", identity.ErrOrganizationNotFound()
	}
	return org.ID, nil
}

// UpdateOrganizationName updates the organization display name.
func (s *Service) UpdateOrganizationName(ctx context.Context, actor ActiveMemberContext, name string) (*OrganizationView, error) {
	return s.UpdateOrganization(ctx, actor, UpdateOrganizationInput{Name: name})
}

// DeleteOrganization hard-deletes the organization after confirmation.
func (s *Service) DeleteOrganization(ctx context.Context, actor ActiveMemberContext, input DeleteOrganizationInput) error {
	if actor.Role != repository.RoleOrgAdmin {
		return identity.ErrForbidden()
	}

	org, err := s.repo.GetOrganizationByID(ctx, actor.OrganizationID)
	if err != nil {
		return err
	}
	if org == nil {
		return identity.ErrOrganizationNotFound()
	}
	if strings.TrimSpace(input.ConfirmationName) != org.Name {
		return identity.ErrOrganizationDeleteConfirmationMismatch()
	}

	if err := s.repo.DeleteOrganization(ctx, actor.OrganizationID); err != nil {
		return err
	}
	return nil
}

// ListMembers returns the organization member roster.
func (s *Service) ListMembers(ctx context.Context, actor ActiveMemberContext) ([]MemberView, error) {
	if actor.Role != repository.RoleOrgAdmin {
		return nil, identity.ErrForbidden()
	}

	members, err := s.repo.ListMembersByOrganizationID(ctx, actor.OrganizationID)
	if err != nil {
		return nil, err
	}
	views := make([]MemberView, 0, len(members))
	for _, m := range members {
		views = append(views, toMemberView(&m))
	}
	return views, nil
}

// AddMember pre-provisions a member by email.
func (s *Service) AddMember(ctx context.Context, actor ActiveMemberContext, input AddMemberInput) (*MemberView, error) {
	if actor.Role != repository.RoleOrgAdmin {
		return nil, identity.ErrForbidden()
	}

	email := platform.NormalizeEmail(input.Email)
	if email == "" {
		return nil, identity.ErrMemberNotFound()
	}

	role, err := parseMemberRole(input.Role)
	if err != nil {
		return nil, err
	}

	exists, err := s.repo.MemberEmailExistsInOrganization(ctx, actor.OrganizationID, email)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, identity.ErrMemberAlreadyExists(email)
	}

	member, err := s.repo.CreateMember(ctx, actor.OrganizationID, email, role, s.now())
	if err != nil {
		return nil, err
	}
	view := toMemberView(member)
	return &view, nil
}

// UpdateMemberRole changes a member's org-level role.
func (s *Service) UpdateMemberRole(ctx context.Context, actor ActiveMemberContext, memberID string, input UpdateMemberRoleInput) (*MemberView, error) {
	if actor.Role != repository.RoleOrgAdmin {
		return nil, identity.ErrForbidden()
	}

	memberID = strings.TrimSpace(memberID)
	if memberID == "" {
		return nil, identity.ErrMemberNotFound()
	}

	target, err := s.repo.GetMemberByID(ctx, actor.OrganizationID, memberID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, identity.ErrMemberNotFound()
	}

	newRole, err := parseMemberRole(input.Role)
	if err != nil {
		return nil, err
	}

	if target.Role == repository.RoleOrgAdmin && newRole != repository.RoleOrgAdmin {
		count, err := s.repo.CountOrgAdmins(ctx, actor.OrganizationID)
		if err != nil {
			return nil, err
		}
		if count <= 1 {
			return nil, identity.ErrLastOrgAdmin()
		}
	}

	updated, err := s.repo.UpdateMemberRole(ctx, actor.OrganizationID, memberID, newRole)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, identity.ErrMemberNotFound()
	}
	view := toMemberView(updated)
	return &view, nil
}

// RemoveMember deletes a member from the organization.
func (s *Service) RemoveMember(ctx context.Context, actor ActiveMemberContext, memberID string) error {
	if actor.Role != repository.RoleOrgAdmin {
		return identity.ErrForbidden()
	}

	memberID = strings.TrimSpace(memberID)
	if memberID == "" {
		return identity.ErrMemberNotFound()
	}

	if memberID == actor.MemberID {
		return identity.ErrCannotRemoveSelf()
	}

	target, err := s.repo.GetMemberByID(ctx, actor.OrganizationID, memberID)
	if err != nil {
		return err
	}
	if target == nil {
		return identity.ErrMemberNotFound()
	}

	if target.Role == repository.RoleOrgAdmin {
		count, err := s.repo.CountOrgAdmins(ctx, actor.OrganizationID)
		if err != nil {
			return err
		}
		if count <= 1 {
			return identity.ErrLastOrgAdmin()
		}
	}

	return s.repo.DeleteMember(ctx, actor.OrganizationID, memberID)
}

// ListEventAssignments returns assignments for an event in the organization.
func (s *Service) ListEventAssignments(ctx context.Context, actor ActiveMemberContext, eventID string) ([]EventAssignmentView, error) {
	if actor.Role != repository.RoleOrgAdmin {
		return nil, identity.ErrForbidden()
	}

	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	assignments, err := s.repo.ListEventAssignments(ctx, eventID)
	if err != nil {
		return nil, err
	}
	views := make([]EventAssignmentView, 0, len(assignments))
	for _, a := range assignments {
		views = append(views, toEventAssignmentView(&a))
	}
	return views, nil
}

// UpsertEventAssignment assigns a member to an event with a per-event role.
func (s *Service) UpsertEventAssignment(
	ctx context.Context,
	actor ActiveMemberContext,
	eventID, memberID string,
	input UpsertEventAssignmentInput,
) (*EventAssignmentView, error) {
	if actor.Role != repository.RoleOrgAdmin {
		return nil, identity.ErrForbidden()
	}

	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	member, err := s.repo.GetMemberByID(ctx, actor.OrganizationID, memberID)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, identity.ErrMemberNotFound()
	}
	if member.Role == repository.RoleOrgAdmin {
		return nil, identity.ErrForbidden()
	}

	role, err := parseAssignmentRole(input.Role)
	if err != nil {
		return nil, err
	}

	assignment, err := s.repo.UpsertEventAssignment(ctx, memberID, eventID, role, s.now())
	if err != nil {
		return nil, err
	}
	assignment.Email = member.Email
	view := toEventAssignmentView(assignment)
	return &view, nil
}

// RemoveEventAssignment removes a member's assignment from an event.
func (s *Service) RemoveEventAssignment(ctx context.Context, actor ActiveMemberContext, eventID, memberID string) error {
	if actor.Role != repository.RoleOrgAdmin {
		return identity.ErrForbidden()
	}

	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return err
	}
	if event == nil {
		return catalog.ErrEventNotFound()
	}

	if err := s.repo.DeleteEventAssignment(ctx, eventID, memberID); err != nil {
		if strings.Contains(err.Error(), "assignment not found") {
			return identity.ErrAssignmentNotFound()
		}
		return err
	}
	return nil
}

// HasEventAccess reports whether a member may act on an event.
func (s *Service) HasEventAccess(ctx context.Context, memberID, organizationID string, role repository.MemberRole, eventID string) (bool, error) {
	if role == repository.RoleOrgAdmin {
		event, err := s.repo.GetEventByID(ctx, organizationID, eventID)
		if err != nil {
			return false, err
		}
		return event != nil, nil
	}

	assigned, err := s.repo.MemberHasEventAssignment(ctx, memberID, eventID)
	if err != nil {
		return false, err
	}
	return assigned, nil
}

// LoadActiveMemberContext resolves the acting member for staff routes.
func (s *Service) LoadActiveMemberContext(ctx context.Context, sessionID string) (ActiveMemberContext, error) {
	session, err := s.loadActiveSession(ctx, sessionID, true)
	if err != nil {
		return ActiveMemberContext{}, err
	}
	if !session.ActiveMemberID.Valid {
		return ActiveMemberContext{}, identity.ErrNoActiveMember()
	}

	membership, err := s.repo.GetMembershipByID(ctx, session.ActiveMemberID.String)
	if err != nil {
		return ActiveMemberContext{}, err
	}
	if membership == nil || platform.NormalizeEmail(membership.Email) != session.Email {
		return ActiveMemberContext{}, identity.ErrMemberNotFound()
	}

	return ActiveMemberContext{
		MemberID:       membership.MemberID,
		OrganizationID: membership.OrganizationID,
		Role:           membership.Role,
		Email:          membership.Email,
	}, nil
}

func parseMemberRole(role string) (repository.MemberRole, error) {
	switch repository.MemberRole(strings.TrimSpace(role)) {
	case repository.RoleOrgAdmin, repository.RoleEventOwner, repository.RoleEventStaff:
		return repository.MemberRole(strings.TrimSpace(role)), nil
	default:
		return "", identity.ErrInvalidMemberRole(role)
	}
}

func parseAssignmentRole(role string) (repository.MemberRole, error) {
	switch repository.MemberRole(strings.TrimSpace(role)) {
	case repository.RoleEventOwner, repository.RoleEventStaff:
		return repository.MemberRole(strings.TrimSpace(role)), nil
	default:
		return "", identity.ErrInvalidMemberRole(role)
	}
}

func toOrganizationView(org *repository.Organization) *OrganizationView {
	view := &OrganizationView{
		ID:        org.ID,
		Name:      org.Name,
		Slug:      org.Slug,
		Currency:  org.Currency,
		CreatedAt: org.CreatedAt,
	}
	if org.SupportWhatsApp.Valid {
		view.SupportWhatsApp = &org.SupportWhatsApp.String
	}
	return view
}

func (s *Service) toOrganizationView(ctx context.Context, org *repository.Organization) (*OrganizationView, error) {
	locked, err := s.catalogRepo.OrganizationHasTicketTypes(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	view := toOrganizationView(org)
	view.CurrencyLocked = locked
	view.LogoURL = s.organizationLogoURL(org.LogoImageKey)
	return view, nil
}

func toMemberView(m *repository.Member) MemberView {
	return MemberView{
		ID:        m.ID,
		Email:     m.Email,
		Role:      string(m.Role),
		CreatedAt: m.CreatedAt,
	}
}

func toEventAssignmentView(a *repository.EventAssignment) EventAssignmentView {
	return EventAssignmentView{
		ID:        a.ID,
		MemberID:  a.MemberID,
		Email:     a.Email,
		Role:      string(a.Role),
		CreatedAt: a.CreatedAt,
	}
}
