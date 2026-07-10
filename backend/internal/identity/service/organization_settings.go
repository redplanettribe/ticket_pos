package service

import (
	"context"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/identity"
	"github.com/peter/ticket_pos/backend/internal/identity/repository"
)

// OrganizationView is the public organization profile.
type OrganizationView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
}

// MemberView is a member in the organization roster.
type MemberView struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// EventView is a minimal event summary for settings.
type EventView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
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

// CreateEventInput creates a minimal event for assignment management.
type CreateEventInput struct {
	Name string
	Slug string
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
	return toOrganizationView(org), nil
}

// UpdateOrganizationName updates the organization display name.
func (s *Service) UpdateOrganizationName(ctx context.Context, actor ActiveMemberContext, name string) (*OrganizationView, error) {
	if actor.Role != repository.RoleOrgAdmin {
		return nil, identity.ErrForbidden()
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return nil, identity.ErrOrganizationNotFound()
	}

	org, err := s.repo.UpdateOrganizationName(ctx, actor.OrganizationID, name)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, identity.ErrOrganizationNotFound()
	}
	return toOrganizationView(org), nil
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

	email := normalizeEmail(input.Email)
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

// ListEvents returns events for the active organization.
func (s *Service) ListEvents(ctx context.Context, actor ActiveMemberContext) ([]EventView, error) {
	if actor.Role != repository.RoleOrgAdmin {
		return nil, identity.ErrForbidden()
	}

	events, err := s.repo.ListEventsByOrganizationID(ctx, actor.OrganizationID)
	if err != nil {
		return nil, err
	}
	views := make([]EventView, 0, len(events))
	for _, e := range events {
		views = append(views, toEventView(&e))
	}
	return views, nil
}

// CreateEvent creates a minimal event for the organization.
func (s *Service) CreateEvent(ctx context.Context, actor ActiveMemberContext, input CreateEventInput) (*EventView, error) {
	if actor.Role != repository.RoleOrgAdmin {
		return nil, identity.ErrForbidden()
	}

	name := strings.TrimSpace(input.Name)
	slug := normalizeSlug(input.Slug)
	if name == "" || slug == "" {
		return nil, identity.ErrEventNotFound()
	}

	exists, err := s.repo.EventSlugExistsInOrganization(ctx, actor.OrganizationID, slug)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, identity.ErrEventSlugTaken(slug)
	}

	event, err := s.repo.CreateEvent(ctx, actor.OrganizationID, name, slug, s.now())
	if err != nil {
		return nil, err
	}
	view := toEventView(event)
	return &view, nil
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
		return nil, identity.ErrEventNotFound()
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
		return nil, identity.ErrEventNotFound()
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
		return identity.ErrEventNotFound()
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
	if membership == nil || normalizeEmail(membership.Email) != session.Email {
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
	return &OrganizationView{
		ID:        org.ID,
		Name:      org.Name,
		Slug:      org.Slug,
		CreatedAt: org.CreatedAt,
	}
}

func toMemberView(m *repository.Member) MemberView {
	return MemberView{
		ID:        m.ID,
		Email:     m.Email,
		Role:      string(m.Role),
		CreatedAt: m.CreatedAt,
	}
}

func toEventView(e *repository.Event) EventView {
	return EventView{
		ID:        e.ID,
		Name:      e.Name,
		Slug:      e.Slug,
		CreatedAt: e.CreatedAt,
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
