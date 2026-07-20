// Package repository provides hand-written SQL data access for the catalog domain.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// EventStatus is the lifecycle state of a catalog Event.
type EventStatus string

const (
	EventStatusDraft     EventStatus = "draft"
	EventStatusPublished EventStatus = "published"
	EventStatusCancelled EventStatus = "cancelled"
)

// Event is a catalog record belonging to an Organization.
type Event struct {
	ID             string
	OrganizationID string
	Name           string
	Slug           string
	Status         EventStatus
	StartsAt       sql.NullTime
	EndsAt         sql.NullTime
	Timezone       sql.NullString
	VenueName      sql.NullString
	VenueAddress   sql.NullString
	Description    sql.NullString
	CoverImageKey  sql.NullString
	Discoverable   bool
	CreatedAt      time.Time
}

// UpdateEventParams holds mutable Event fields for PATCH.
type UpdateEventParams struct {
	Name          string
	Slug          string
	StartsAt      sql.NullTime
	EndsAt        sql.NullTime
	Timezone      sql.NullString
	VenueName     sql.NullString
	VenueAddress  sql.NullString
	Description   sql.NullString
	CoverImageKey sql.NullString
	Discoverable  bool
}

// Repository provides SQL access for catalog data.
type Repository struct {
	db *platform.DB
}

// New returns a repository backed by the given database pool.
func New(db *platform.DB) *Repository {
	return &Repository{db: db}
}

const eventColumns = `
	id, organization_id, name, slug, status,
	starts_at, ends_at, timezone, venue_name, venue_address,
	description, cover_image_key, discoverable, created_at
`

func scanEvent(row interface {
	Scan(dest ...any) error
}) (*Event, error) {
	var e Event
	var status string
	if err := row.Scan(
		&e.ID, &e.OrganizationID, &e.Name, &e.Slug, &status,
		&e.StartsAt, &e.EndsAt, &e.Timezone, &e.VenueName, &e.VenueAddress,
		&e.Description, &e.CoverImageKey, &e.Discoverable, &e.CreatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	e.Status = EventStatus(status)
	return &e, nil
}

// ListEventsByOrganizationID returns events for an organization.
func (r *Repository) ListEventsByOrganizationID(ctx context.Context, orgID string) ([]Event, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+eventColumns+`
		FROM events
		WHERE organization_id = $1
		ORDER BY created_at DESC, name ASC
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var status string
		if err := rows.Scan(
			&e.ID, &e.OrganizationID, &e.Name, &e.Slug, &status,
			&e.StartsAt, &e.EndsAt, &e.Timezone, &e.VenueName, &e.VenueAddress,
			&e.Description, &e.CoverImageKey, &e.Discoverable, &e.CreatedAt,
		); err != nil {
			return nil, err
		}
		e.Status = EventStatus(status)
		events = append(events, e)
	}
	return events, rows.Err()
}

// EventSlugExistsInOrganization reports whether a slug is taken within the org.
func (r *Repository) EventSlugExistsInOrganization(ctx context.Context, orgID, slug string) (bool, error) {
	var exists bool
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM events WHERE organization_id = $1 AND slug = $2
		)
	`, orgID, slug).Scan(&exists)
	return exists, err
}

// EventSlugExistsInOrganizationExcluding reports slug collision excluding one event.
func (r *Repository) EventSlugExistsInOrganizationExcluding(ctx context.Context, orgID, slug, eventID string) (bool, error) {
	var exists bool
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM events
			WHERE organization_id = $1 AND slug = $2 AND id <> $3
		)
	`, orgID, slug, eventID).Scan(&exists)
	return exists, err
}

// CreateEvent inserts a draft event for an organization.
func (r *Repository) CreateEvent(ctx context.Context, orgID, name, slug string, createdAt time.Time) (*Event, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		INSERT INTO events (organization_id, name, slug, status, created_at)
		VALUES ($1, $2, $3, 'draft', $4)
		RETURNING `+eventColumns+`
	`, orgID, name, slug, createdAt)
	return scanEvent(row)
}

// GetEventByID loads an event scoped to an organization.
func (r *Repository) GetEventByID(ctx context.Context, orgID, eventID string) (*Event, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT `+eventColumns+`
		FROM events
		WHERE id = $1 AND organization_id = $2
	`, eventID, orgID)
	return scanEvent(row)
}

// UpdateEvent updates mutable fields on an event.
func (r *Repository) UpdateEvent(ctx context.Context, orgID, eventID string, params UpdateEventParams) (*Event, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		UPDATE events
		SET
			name = $3,
			slug = $4,
			starts_at = $5,
			ends_at = $6,
			timezone = $7,
			venue_name = $8,
			venue_address = $9,
			description = $10,
			cover_image_key = $11,
			discoverable = $12
		WHERE id = $1 AND organization_id = $2
		RETURNING `+eventColumns+`
	`, eventID, orgID,
		params.Name, params.Slug,
		nullTime(params.StartsAt), nullTime(params.EndsAt),
		nullString(params.Timezone), nullString(params.VenueName),
		nullString(params.VenueAddress), nullString(params.Description),
		nullString(params.CoverImageKey), params.Discoverable,
	)
	return scanEvent(row)
}

// CountTicketTypesByEventID returns how many Ticket Types belong to an Event.
func (r *Repository) CountTicketTypesByEventID(ctx context.Context, orgID, eventID string) (int, error) {
	var count int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ticket_types
		WHERE event_id = $1 AND organization_id = $2
	`, eventID, orgID).Scan(&count)
	return count, err
}

// UpdateEventStatus sets the lifecycle status on an Event.
func (r *Repository) UpdateEventStatus(ctx context.Context, orgID, eventID string, status EventStatus) (*Event, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		UPDATE events
		SET status = $3
		WHERE id = $1 AND organization_id = $2
		RETURNING `+eventColumns+`
	`, eventID, orgID, string(status))
	return scanEvent(row)
}

// SetEventDiscoverable sets the discoverable flag on an Event.
func (r *Repository) SetEventDiscoverable(ctx context.Context, orgID, eventID string, discoverable bool) (*Event, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		UPDATE events
		SET discoverable = $3
		WHERE id = $1 AND organization_id = $2
		RETURNING `+eventColumns+`
	`, eventID, orgID, discoverable)
	return scanEvent(row)
}

// DeleteEvent removes an event by ID scoped to an organization.
func (r *Repository) DeleteEvent(ctx context.Context, orgID, eventID string) error {
	result, err := r.db.Pool.ExecContext(ctx, `
		DELETE FROM events WHERE id = $1 AND organization_id = $2
	`, eventID, orgID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func nullTime(v sql.NullTime) any {
	if v.Valid {
		return v.Time
	}
	return nil
}

func nullString(v sql.NullString) any {
	if v.Valid {
		return v.String
	}
	return nil
}

// TicketType is a purchasable ticket category belonging to an Event.
type TicketType struct {
	ID             string
	EventID        string
	OrganizationID string
	Name           string
	Description    sql.NullString
	PriceCents     int
	Capacity       int
	SoldCount      int
	SortOrder      int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CreateTicketTypeParams holds values for a new Ticket Type.
type CreateTicketTypeParams struct {
	Name        string
	Description sql.NullString
	PriceCents  int
	Capacity    int
	SortOrder   int
}

// UpdateTicketTypeParams holds mutable Ticket Type fields.
type UpdateTicketTypeParams struct {
	Name        string
	Description sql.NullString
	PriceCents  int
	Capacity    int
	SortOrder   int
}

const ticketTypeColumns = `
	id, event_id, organization_id, name, description,
	price_cents, capacity, sold_count, sort_order, created_at, updated_at
`

func scanTicketType(row interface {
	Scan(dest ...any) error
}) (*TicketType, error) {
	var tt TicketType
	if err := row.Scan(
		&tt.ID, &tt.EventID, &tt.OrganizationID, &tt.Name, &tt.Description,
		&tt.PriceCents, &tt.Capacity, &tt.SoldCount, &tt.SortOrder, &tt.CreatedAt, &tt.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &tt, nil
}

// GetOrganizationCurrency returns the ISO 4217 currency code for an organization.
func (r *Repository) GetOrganizationCurrency(ctx context.Context, orgID string) (string, error) {
	var currency string
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT currency FROM organizations WHERE id = $1
	`, orgID).Scan(&currency)
	return currency, err
}

// OrganizationHasTicketTypes reports whether any Ticket Type exists in the organization.
func (r *Repository) OrganizationHasTicketTypes(ctx context.Context, orgID string) (bool, error) {
	var exists bool
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM ticket_types WHERE organization_id = $1)
	`, orgID).Scan(&exists)
	return exists, err
}

// NextTicketTypeSortOrder returns the next sort_order for a new Ticket Type on an Event.
func (r *Repository) NextTicketTypeSortOrder(ctx context.Context, eventID string) (int, error) {
	var maxOrder sql.NullInt64
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT MAX(sort_order) FROM ticket_types WHERE event_id = $1
	`, eventID).Scan(&maxOrder)
	if err != nil {
		return 0, err
	}
	if !maxOrder.Valid {
		return 0, nil
	}
	return int(maxOrder.Int64) + 1, nil
}

// ListTicketTypesByEventID returns Ticket Types for an Event ordered by sort_order.
func (r *Repository) ListTicketTypesByEventID(ctx context.Context, orgID, eventID string) ([]TicketType, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+ticketTypeColumns+`
		FROM ticket_types
		WHERE event_id = $1 AND organization_id = $2
		ORDER BY sort_order ASC, created_at ASC
	`, eventID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var types []TicketType
	for rows.Next() {
		var tt TicketType
		if err := rows.Scan(
			&tt.ID, &tt.EventID, &tt.OrganizationID, &tt.Name, &tt.Description,
			&tt.PriceCents, &tt.Capacity, &tt.SoldCount, &tt.SortOrder, &tt.CreatedAt, &tt.UpdatedAt,
		); err != nil {
			return nil, err
		}
		types = append(types, tt)
	}
	return types, rows.Err()
}

// CreateTicketType inserts a Ticket Type for an Event.
func (r *Repository) CreateTicketType(
	ctx context.Context,
	orgID, eventID string,
	params CreateTicketTypeParams,
	now time.Time,
) (*TicketType, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		INSERT INTO ticket_types (
			event_id, organization_id, name, description,
			price_cents, capacity, sort_order, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
		RETURNING `+ticketTypeColumns+`
	`, eventID, orgID, params.Name, nullString(params.Description),
		params.PriceCents, params.Capacity, params.SortOrder, now)
	return scanTicketType(row)
}

// GetTicketTypeByID loads a Ticket Type scoped to an Event and organization.
func (r *Repository) GetTicketTypeByID(ctx context.Context, orgID, eventID, ticketTypeID string) (*TicketType, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT `+ticketTypeColumns+`
		FROM ticket_types
		WHERE id = $1 AND event_id = $2 AND organization_id = $3
	`, ticketTypeID, eventID, orgID)
	return scanTicketType(row)
}

// UpdateTicketType updates mutable Ticket Type fields.
func (r *Repository) UpdateTicketType(
	ctx context.Context,
	orgID, eventID, ticketTypeID string,
	params UpdateTicketTypeParams,
	now time.Time,
) (*TicketType, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		UPDATE ticket_types
		SET
			name = $4,
			description = $5,
			price_cents = $6,
			capacity = $7,
			sort_order = $8,
			updated_at = $9
		WHERE id = $1 AND event_id = $2 AND organization_id = $3
		RETURNING `+ticketTypeColumns+`
	`, ticketTypeID, eventID, orgID,
		params.Name, nullString(params.Description),
		params.PriceCents, params.Capacity, params.SortOrder, now)
	return scanTicketType(row)
}

// DeleteTicketType removes a Ticket Type scoped to an Event and organization.
func (r *Repository) DeleteTicketType(ctx context.Context, orgID, eventID, ticketTypeID string) error {
	result, err := r.db.Pool.ExecContext(ctx, `
		DELETE FROM ticket_types
		WHERE id = $1 AND event_id = $2 AND organization_id = $3
	`, ticketTypeID, eventID, orgID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
