package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/identity"
	"github.com/peter/ticket_pos/backend/internal/identity/repository"
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
	// The House Organization designation (#472, ADR 0060): whether the
	// platform's own entity runs this Organization, and the trail of the act
	// — which operator designated it and when. Both trail fields are null
	// when it is not one; they are never set without the other.
	IsHouseOrganization bool       `json:"is_house_organization"`
	HouseDesignatedBy   *string    `json:"house_designated_by"`
	HouseDesignatedAt   *time.Time `json:"house_designated_at"`
}

// houseOrganizationCurrency is the one currency the platform's Issuer invoices
// in, and so the one a House Organization may trade in: every paid Online
// Sale of a House Event owes a Sale Invoice, and one in another currency could
// never be built (ADR 0060). Stated here, where the designation is made, and
// pointedly not read from the invoicing module: whether an Issuer exists, in
// which environment, or with what certificate is no part of the decision.
const houseOrganizationCurrency = "USD"

// toOperatorOrganization projects a row onto the operator's view of it.
func toOperatorOrganization(o *repository.Organization) OperatorOrganization {
	out := OperatorOrganization{
		ID:        o.ID,
		Name:      o.Name,
		Slug:      o.Slug,
		Currency:  o.Currency,
		CreatedAt: o.CreatedAt,
	}
	if o.HouseDesignatedBy.Valid && o.HouseDesignatedAt.Valid {
		out.IsHouseOrganization = true
		out.HouseDesignatedBy = &o.HouseDesignatedBy.String
		at := o.HouseDesignatedAt.Time
		out.HouseDesignatedAt = &at
	}
	return out
}

// IsPlatformOperator reports whether an email is on the platform operator
// allowlist. It is the single authority check behind the operator namespace and
// the flag the staff session carries, so the middleware and the session view can
// never disagree about who an operator is.
func (s *Service) IsPlatformOperator(ctx context.Context, email string) (bool, error) {
	return s.repo.IsPlatformOperator(ctx, platform.NormalizeEmail(email))
}

// PlatformOperatorEmails returns every address on the operator allowlist, for
// the one thing that needs the list rather than a verdict about one address:
// telling the operators that an Organization has asked to be paid (#179).
//
// It sits beside IsPlatformOperator on purpose. Both answer from the same table,
// so who is notified and who is authorised can never be two different sets —
// which is the whole reason the allowlist is the platform's only operator
// concept (ADR 0015).
func (s *Service) PlatformOperatorEmails(ctx context.Context) ([]string, error) {
	return s.repo.ListPlatformOperatorEmails(ctx)
}

// OrgAdminEmails returns the address of every Org Admin of one Organization,
// for the one notice that has no single asker to answer: a Revocation of a
// Ticket Question is told to everybody accountable for the Organization
// (#410, ADR 0056). Answered from the same table Membership is, so who is told
// and who could have submitted the question can never be two different sets.
func (s *Service) OrgAdminEmails(ctx context.Context, orgID string) ([]string, error) {
	members, err := s.repo.ListMembersByOrganizationID(ctx, orgID)
	if err != nil {
		return nil, err
	}
	emails := make([]string, 0, len(members))
	for _, m := range members {
		if m.Role == repository.RoleOrgAdmin {
			emails = append(emails, m.Email)
		}
	}
	return emails, nil
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
		out = append(out, toOperatorOrganization(&o))
	}
	return out, total, nil
}

// OrganizationsForOperator returns the named Organizations keyed by id, for an
// operator list whose rows came from another module and carry only an
// organization_id — the Payout Request queue (#176).
//
// A map rather than a slice because that is how the caller uses it: it holds the
// rows and needs the Organization for each. Ids that name nothing are simply
// absent, which lets the caller decide whether a gap is possible; for the queue
// it is not, because a request cascades with the Organization it belongs to.
//
// Malformed ids are not parsed and not refused. They can only come from another
// module's foreign key, never from a URL, so the strictness
// GetOrganizationForOperator applies to a path parameter would be answering a
// question nobody asked.
func (s *Service) OrganizationsForOperator(ctx context.Context, orgIDs []string) (map[string]OperatorOrganization, error) {
	rows, err := s.repo.OrganizationsByIDs(ctx, orgIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]OperatorOrganization, len(rows))
	for _, o := range rows {
		out[o.ID] = toOperatorOrganization(&o)
	}
	return out, nil
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
	view := toOperatorOrganization(org)
	return &view, nil
}

// DesignateHouseOrganization marks an Organization as one the platform's own
// entity runs, stamped with the operator's email and the server's clock
// (#472, ADR 0060). Refused when the Organization trades in a currency the
// Issuer does not invoice in, with the currency named; NOT refused for a
// missing, test-environment or expired Issuer, because the platform's
// compliance is the operator's to see and fix rather than the buyer's to wait
// for. Designating an Organization already designated leaves its trail as it
// was. Designation affects future sales only; nothing is invoiced here.
func (s *Service) DesignateHouseOrganization(ctx context.Context, orgID, operator string) (*OperatorOrganization, error) {
	org, err := s.GetOrganizationForOperator(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if org.Currency != houseOrganizationCurrency {
		return nil, identity.ErrHouseOrganizationCurrencyUnsupported(org.Currency)
	}
	updated, err := s.repo.DesignateHouseOrganization(ctx, org.ID, operator, s.now())
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, identity.ErrOrganizationNotFound()
	}
	view := toOperatorOrganization(updated)
	return &view, nil
}

// UndesignateHouseOrganization takes the designation back, emptying who and
// when together. It touches nothing already owed or issued — undesignation
// affects future sales only (ADR 0060) — and clearing an Organization that
// was never designated is an ordinary answer rather than a refusal.
func (s *Service) UndesignateHouseOrganization(ctx context.Context, orgID string) (*OperatorOrganization, error) {
	org, err := s.GetOrganizationForOperator(ctx, orgID)
	if err != nil {
		return nil, err
	}
	updated, err := s.repo.ClearHouseDesignation(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, identity.ErrOrganizationNotFound()
	}
	view := toOperatorOrganization(updated)
	return &view, nil
}
