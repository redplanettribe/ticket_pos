package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/identity"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// OperatorOrganization is one Organization as the Platform Operator sees it:
// who they are, and the currency every money figure about them is quoted in.
// Deliberately thin — the operator surface answers "who is on the platform",
// not "what is inside this Organization" (ADR 0015).
type OperatorOrganization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Currency  string    `json:"currency"`
	CreatedAt time.Time `json:"created_at"`
}

// IsPlatformOperator reports whether an email is on the platform operator
// allowlist. It is the single authority check behind the operator namespace and
// the flag the staff session carries, so the middleware and the session view can
// never disagree about who an operator is.
func (s *Service) IsPlatformOperator(ctx context.Context, email string) (bool, error) {
	return s.repo.IsPlatformOperator(ctx, platform.NormalizeEmail(email))
}

// ListOrganizationsForOperator returns one page of every Organization on the
// platform, name-ascending, plus the unpaginated total (ADR 0006). Page and size
// arrive already floored and clamped by the handler.
func (s *Service) ListOrganizationsForOperator(ctx context.Context, page, pageSize int) ([]OperatorOrganization, int, error) {
	rows, total, err := s.repo.ListAllOrganizations(ctx, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	out := make([]OperatorOrganization, 0, len(rows))
	for _, o := range rows {
		out = append(out, OperatorOrganization{
			ID:        o.ID,
			Name:      o.Name,
			Slug:      o.Slug,
			Currency:  o.Currency,
			CreatedAt: o.CreatedAt,
		})
	}
	return out, total, nil
}

// GetOrganizationForOperator returns one Organization by id, whoever the
// operator is a Member of. A malformed id is answered as a missing Organization
// rather than as a database failure: to the caller, both mean "no such
// Organization at this path".
func (s *Service) GetOrganizationForOperator(ctx context.Context, orgID string) (*OperatorOrganization, error) {
	if _, err := uuid.Parse(orgID); err != nil {
		return nil, identity.ErrOrganizationNotFound()
	}
	org, err := s.repo.GetOrganizationByID(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, identity.ErrOrganizationNotFound()
	}
	return &OperatorOrganization{
		ID:        org.ID,
		Name:      org.Name,
		Slug:      org.Slug,
		Currency:  org.Currency,
		CreatedAt: org.CreatedAt,
	}, nil
}
