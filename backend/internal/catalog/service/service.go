package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
	"github.com/peter/ticket_pos/backend/internal/platform/storage"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

// EventListItem is a summary row for the Events list UI.
type EventListItem struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Slug         string     `json:"slug"`
	Status       string     `json:"status"`
	StartsAt     *time.Time `json:"starts_at"`
	Timezone     *string    `json:"timezone"`
	Discoverable bool       `json:"discoverable"`
	CreatedAt    time.Time  `json:"created_at"`
}

// TicketTypeDetail is a Ticket Type with organization currency for display.
type TicketTypeDetail struct {
	ID          string    `json:"id"`
	EventID     string    `json:"event_id"`
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	PriceCents  int       `json:"price_cents"`
	Currency    string    `json:"currency"`
	Capacity    int       `json:"capacity"`
	SoldCount   int       `json:"sold_count"`
	SortOrder   int       `json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// EventDetail is the full Event record for detail views.
type EventDetail struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Slug          string     `json:"slug"`
	Status        string     `json:"status"`
	StartsAt      *time.Time `json:"starts_at"`
	EndsAt        *time.Time `json:"ends_at"`
	Timezone      *string    `json:"timezone"`
	VenueName     *string    `json:"venue_name"`
	VenueAddress  *string    `json:"venue_address"`
	Description   *string    `json:"description"`
	CoverImageKey *string    `json:"cover_image_key"`
	CoverImageURL *string    `json:"cover_image_url"`
	// CoverVideoKey and CoverVideoURL are the Event's optional Cover Video: the
	// stored object key and the public URL derived from it at read time, never
	// stored (ADR 0020).
	CoverVideoKey *string `json:"cover_video_key"`
	CoverVideoURL *string `json:"cover_video_url"`
	Discoverable  bool    `json:"discoverable"`
	// FeeHandling is the Event's Fee Handling mode, and the two rates are the
	// Platform Fee schedule it is read with. The rates travel with the Event so
	// the staff forms can show an organizer what a price means for the buyer and
	// for their own take-home using the same arithmetic checkout uses (ADR 0014).
	FeeHandling       string    `json:"fee_handling"`
	FeeBasisPoints    int       `json:"fee_basis_points"`
	FeeIVABasisPoints int       `json:"fee_iva_basis_points"`
	CreatedAt         time.Time `json:"created_at"`
}

// ActorContext is the acting member for catalog operations.
type ActorContext struct {
	MemberID       string
	OrganizationID string
}

// CreateEventInput creates a draft Event.
type CreateEventInput struct {
	Name string
	Slug string
}

// CreateTicketTypeInput creates a Ticket Type on an Event.
type CreateTicketTypeInput struct {
	Name        string
	Description *string
	PriceCents  int
	Capacity    int
}

// UpdateTicketTypeInput updates Ticket Type fields.
type UpdateTicketTypeInput struct {
	Name        string
	Description *string
	PriceCents  int
	Capacity    int
	SortOrder   int
}

// UpdateEventInput updates Event fields on the detail form.
type UpdateEventInput struct {
	Name          string
	Slug          string
	StartsAt      *time.Time
	EndsAt        *time.Time
	Timezone      *string
	VenueName     *string
	VenueAddress  *string
	Description   *string
	CoverImageKey *string
	// CoverVideoKey attaches or clears the Cover Video: an empty string clears
	// it, nil leaves it alone, anything else must be a key under this Event's
	// videos prefix.
	CoverVideoKey *string
	// FeeHandling is the submitted Fee Handling mode, or nil when the form said
	// nothing about it — an update that omits it leaves the Event's mode alone.
	FeeHandling *sales.FeeHandling
}

// CreateCoverUploadURLInput requests a presigned cover upload URL.
type CreateCoverUploadURLInput struct {
	ContentType string
	FileName    string
}

// CreateVideoUploadURLInput requests a presigned Cover Video upload URL.
type CreateVideoUploadURLInput struct {
	ContentType string
}

// Service implements catalog business rules.
type Service struct {
	repo    *repository.Repository
	storage storage.ObjectStorage
	fees    sales.FeeRates
	now     func() time.Time
}

// New returns a catalog service. The fee rates are the platform's configured
// Platform Fee schedule, surfaced on Event payloads so the staff forms derive
// buyer and take-home figures with the checkout arithmetic (ADR 0014).
func New(repo *repository.Repository, objectStorage storage.ObjectStorage, fees sales.FeeRates) *Service {
	return &Service{
		repo:    repo,
		storage: objectStorage,
		fees:    fees,
		now:     time.Now,
	}
}

// WithClock overrides the clock (tests).
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// ListEvents returns Events for the active Organization.
func (s *Service) ListEvents(ctx context.Context, actor ActorContext) ([]EventListItem, error) {
	events, err := s.repo.ListEventsByOrganizationID(ctx, actor.OrganizationID)
	if err != nil {
		return nil, err
	}
	items := make([]EventListItem, 0, len(events))
	for _, e := range events {
		items = append(items, toEventListItem(&e))
	}
	return items, nil
}

// CreateEvent creates a draft Event.
func (s *Service) CreateEvent(ctx context.Context, actor ActorContext, input CreateEventInput) (*EventDetail, error) {
	name := strings.TrimSpace(input.Name)
	slug := normalizeSlug(input.Slug)

	exists, err := s.repo.EventSlugExistsInOrganization(ctx, actor.OrganizationID, slug)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, catalog.ErrEventSlugTaken(slug)
	}

	event, err := s.repo.CreateEvent(ctx, actor.OrganizationID, name, slug, s.now())
	if err != nil {
		return nil, err
	}
	detail := s.toEventDetail(event)
	return &detail, nil
}

// GetEvent returns an Event by ID.
func (s *Service) GetEvent(ctx context.Context, actor ActorContext, eventID string) (*EventDetail, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}
	detail := s.toEventDetail(event)
	return &detail, nil
}

// UpdateEvent updates Event fields respecting draft slug rules.
func (s *Service) UpdateEvent(ctx context.Context, actor ActorContext, eventID string, input UpdateEventInput) (*EventDetail, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	name := strings.TrimSpace(input.Name)
	slug := normalizeSlug(input.Slug)

	if slug != event.Slug && event.Status != repository.EventStatusDraft {
		return nil, catalog.ErrEventNotDraft()
	}
	if slug != event.Slug {
		taken, err := s.repo.EventSlugExistsInOrganizationExcluding(ctx, actor.OrganizationID, slug, eventID)
		if err != nil {
			return nil, err
		}
		if taken {
			return nil, catalog.ErrEventSlugTaken(slug)
		}
	}

	params := repository.UpdateEventParams{
		Name: name,
		Slug: slug,
	}
	params.StartsAt = nullTimeFromPtr(input.StartsAt)
	params.EndsAt = nullTimeFromPtr(input.EndsAt)
	params.Timezone = nullStringFromPtr(input.Timezone)
	params.VenueName = nullStringFromPtr(input.VenueName)
	params.VenueAddress = nullStringFromPtr(input.VenueAddress)
	params.Description = nullStringFromPtr(input.Description)
	if input.CoverImageKey != nil {
		key := strings.TrimSpace(*input.CoverImageKey)
		if key == "" {
			params.CoverImageKey = sql.NullString{}
		} else if !storage.CoverKeyBelongsToEvent(key, actor.OrganizationID, eventID) {
			return nil, catalog.ErrInvalidCoverImageKey()
		} else {
			params.CoverImageKey = sql.NullString{String: key, Valid: true}
		}
	} else {
		params.CoverImageKey = event.CoverImageKey
	}
	if input.CoverVideoKey != nil {
		key := strings.TrimSpace(*input.CoverVideoKey)
		if key == "" {
			params.CoverVideoKey = sql.NullString{}
		} else if !storage.VideoKeyBelongsToEvent(key, actor.OrganizationID, eventID) {
			return nil, catalog.ErrInvalidCoverVideoKey()
		} else {
			params.CoverVideoKey = sql.NullString{String: key, Valid: true}
		}
	} else {
		params.CoverVideoKey = event.CoverVideoKey
	}

	params.Discoverable = event.Discoverable

	// Fee Handling takes effect on future checkouts only: pending Payments carry
	// their own snapshot, so a flip never rewrites money already being paid.
	params.FeeHandling = event.FeeHandling
	if input.FeeHandling != nil {
		params.FeeHandling = string(*input.FeeHandling)
	}

	updated, err := s.repo.UpdateEvent(ctx, actor.OrganizationID, eventID, params)
	if err != nil {
		return nil, err
	}
	detail := s.toEventDetail(updated)
	return &detail, nil
}

// SetEventDiscoverable sets whether a published Event is listed in public discovery
// surfaces. Only a published Event may change discoverability; draft and cancelled
// Events are never listed and reject the change.
func (s *Service) SetEventDiscoverable(ctx context.Context, actor ActorContext, eventID string, discoverable bool) (*EventDetail, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}
	if event.Status != repository.EventStatusPublished {
		return nil, catalog.ErrEventNotPublished()
	}

	updated, err := s.repo.SetEventDiscoverable(ctx, actor.OrganizationID, eventID, discoverable)
	if err != nil {
		return nil, err
	}
	detail := s.toEventDetail(updated)
	return &detail, nil
}

// PublishEvent transitions a draft Event to published when requirements are met.
func (s *Service) PublishEvent(ctx context.Context, actor ActorContext, eventID string) (*EventDetail, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	switch event.Status {
	case repository.EventStatusPublished:
		return nil, catalog.ErrEventAlreadyPublished()
	case repository.EventStatusCancelled:
		return nil, catalog.ErrEventAlreadyCancelled()
	case repository.EventStatusDraft:
	default:
		return nil, catalog.ErrEventNotDraft()
	}

	missing := publishMissingFields(event)
	ticketCount, err := s.repo.CountTicketTypesByEventID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if ticketCount == 0 {
		missing = append(missing, "ticket_types")
	}
	if len(missing) > 0 {
		return nil, catalog.ErrEventPublishRequirementsNotMet(missing)
	}

	updated, err := s.repo.UpdateEventStatus(ctx, actor.OrganizationID, eventID, repository.EventStatusPublished)
	if err != nil {
		return nil, err
	}
	detail := s.toEventDetail(updated)
	return &detail, nil
}

// CancelEvent transitions a published Event to cancelled.
func (s *Service) CancelEvent(ctx context.Context, actor ActorContext, eventID string) (*EventDetail, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	switch event.Status {
	case repository.EventStatusCancelled:
		return nil, catalog.ErrEventAlreadyCancelled()
	case repository.EventStatusDraft:
		return nil, catalog.ErrEventNotDraft()
	case repository.EventStatusPublished:
	default:
		return nil, catalog.ErrEventNotDraft()
	}

	updated, err := s.repo.UpdateEventStatus(ctx, actor.OrganizationID, eventID, repository.EventStatusCancelled)
	if err != nil {
		return nil, err
	}
	detail := s.toEventDetail(updated)
	return &detail, nil
}

func publishMissingFields(event *repository.Event) []string {
	var missing []string
	if strings.TrimSpace(event.Name) == "" {
		missing = append(missing, "name")
	}
	if strings.TrimSpace(event.Slug) == "" {
		missing = append(missing, "slug")
	}
	if !event.StartsAt.Valid {
		missing = append(missing, "starts_at")
	}
	if !event.Timezone.Valid || strings.TrimSpace(event.Timezone.String) == "" {
		missing = append(missing, "timezone")
	} else if _, err := time.LoadLocation(strings.TrimSpace(event.Timezone.String)); err != nil {
		missing = append(missing, "timezone")
	}
	return missing
}

// DeleteEvent removes a draft Event.
func (s *Service) DeleteEvent(ctx context.Context, actor ActorContext, eventID string) error {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return err
	}
	if event == nil {
		return catalog.ErrEventNotFound()
	}
	if event.Status != repository.EventStatusDraft {
		return catalog.ErrEventDeleteForbidden()
	}

	if err := s.repo.DeleteEvent(ctx, actor.OrganizationID, eventID); err != nil {
		return err
	}
	return nil
}

// CreateCoverUploadURL returns a presigned PUT URL for an event cover image.
func (s *Service) CreateCoverUploadURL(ctx context.Context, actor ActorContext, eventID string, input CreateCoverUploadURLInput) (*storage.CoverUploadResult, error) {
	if s.storage == nil {
		return nil, catalog.ErrCoverUploadUnavailable()
	}

	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	contentType := strings.ToLower(strings.TrimSpace(input.ContentType))
	if !storage.CoverContentTypeAllowed(contentType) {
		return nil, catalog.ErrInvalidCoverImageKey()
	}

	key, err := storage.BuildCoverObjectKey(actor.OrganizationID, eventID, contentType, input.FileName)
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

// CreateVideoUploadURL returns a presigned PUT URL for an event Cover Video.
// The object is stored verbatim under the Event's videos prefix — no transcoding,
// no processing state: the video is live the moment the PUT returns (ADR 0020).
func (s *Service) CreateVideoUploadURL(ctx context.Context, actor ActorContext, eventID string, input CreateVideoUploadURLInput) (*storage.CoverUploadResult, error) {
	if s.storage == nil {
		return nil, catalog.ErrVideoUploadUnavailable()
	}

	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	contentType := strings.ToLower(strings.TrimSpace(input.ContentType))
	if !storage.VideoContentTypeAllowed(contentType) {
		return nil, catalog.ErrInvalidCoverVideoKey()
	}

	key, err := storage.BuildVideoObjectKey(actor.OrganizationID, eventID, contentType)
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

func normalizeSlug(slug string) string {
	return strings.ToLower(strings.TrimSpace(slug))
}

func nullTimeFromPtr(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func nullStringFromPtr(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: trimmed, Valid: true}
}

func toEventListItem(e *repository.Event) EventListItem {
	item := EventListItem{
		ID:           e.ID,
		Name:         e.Name,
		Slug:         e.Slug,
		Status:       string(e.Status),
		Discoverable: e.Discoverable,
		CreatedAt:    e.CreatedAt,
	}
	if e.StartsAt.Valid {
		t := e.StartsAt.Time
		item.StartsAt = &t
	}
	if e.Timezone.Valid {
		tz := e.Timezone.String
		item.Timezone = &tz
	}
	return item
}

// ListTicketTypes returns Ticket Types for an Event.
func (s *Service) ListTicketTypes(ctx context.Context, actor ActorContext, eventID string) ([]TicketTypeDetail, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	currency, err := s.repo.GetOrganizationCurrency(ctx, actor.OrganizationID)
	if err != nil {
		return nil, err
	}

	types, err := s.repo.ListTicketTypesByEventID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}

	items := make([]TicketTypeDetail, 0, len(types))
	for _, tt := range types {
		items = append(items, toTicketTypeDetail(&tt, currency))
	}
	return items, nil
}

// CreateTicketType adds a Ticket Type to an Event.
func (s *Service) CreateTicketType(ctx context.Context, actor ActorContext, eventID string, input CreateTicketTypeInput) (*TicketTypeDetail, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	currency, err := s.repo.GetOrganizationCurrency(ctx, actor.OrganizationID)
	if err != nil {
		return nil, err
	}

	sortOrder, err := s.repo.NextTicketTypeSortOrder(ctx, eventID)
	if err != nil {
		return nil, err
	}

	created, err := s.repo.CreateTicketType(ctx, actor.OrganizationID, eventID, repository.CreateTicketTypeParams{
		Name:        strings.TrimSpace(input.Name),
		Description: nullStringFromPtr(input.Description),
		PriceCents:  input.PriceCents,
		Capacity:    input.Capacity,
		SortOrder:   sortOrder,
	}, s.now())
	if err != nil {
		return nil, err
	}

	detail := toTicketTypeDetail(created, currency)
	return &detail, nil
}

// UpdateTicketType updates a Ticket Type on an Event.
func (s *Service) UpdateTicketType(ctx context.Context, actor ActorContext, eventID, ticketTypeID string, input UpdateTicketTypeInput) (*TicketTypeDetail, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	existing, err := s.repo.GetTicketTypeByID(ctx, actor.OrganizationID, eventID, ticketTypeID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, catalog.ErrTicketTypeNotFound()
	}

	currency, err := s.repo.GetOrganizationCurrency(ctx, actor.OrganizationID)
	if err != nil {
		return nil, err
	}

	updated, err := s.repo.UpdateTicketType(ctx, actor.OrganizationID, eventID, ticketTypeID, repository.UpdateTicketTypeParams{
		Name:        strings.TrimSpace(input.Name),
		Description: nullStringFromPtr(input.Description),
		PriceCents:  input.PriceCents,
		Capacity:    input.Capacity,
		SortOrder:   input.SortOrder,
	}, s.now())
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, catalog.ErrTicketTypeNotFound()
	}

	detail := toTicketTypeDetail(updated, currency)
	return &detail, nil
}

// DeleteTicketType removes a Ticket Type when the parent Event is draft.
func (s *Service) DeleteTicketType(ctx context.Context, actor ActorContext, eventID, ticketTypeID string) error {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return err
	}
	if event == nil {
		return catalog.ErrEventNotFound()
	}
	if event.Status != repository.EventStatusDraft {
		return catalog.ErrTicketTypeDeleteForbidden()
	}

	existing, err := s.repo.GetTicketTypeByID(ctx, actor.OrganizationID, eventID, ticketTypeID)
	if err != nil {
		return err
	}
	if existing == nil {
		return catalog.ErrTicketTypeNotFound()
	}

	if err := s.repo.DeleteTicketType(ctx, actor.OrganizationID, eventID, ticketTypeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return catalog.ErrTicketTypeNotFound()
		}
		return err
	}
	return nil
}

func toTicketTypeDetail(tt *repository.TicketType, currency string) TicketTypeDetail {
	detail := TicketTypeDetail{
		ID:         tt.ID,
		EventID:    tt.EventID,
		Name:       tt.Name,
		PriceCents: tt.PriceCents,
		Currency:   currency,
		Capacity:   tt.Capacity,
		SoldCount:  tt.SoldCount,
		SortOrder:  tt.SortOrder,
		CreatedAt:  tt.CreatedAt,
		UpdatedAt:  tt.UpdatedAt,
	}
	if tt.Description.Valid {
		s := tt.Description.String
		detail.Description = &s
	}
	return detail
}

func (s *Service) toEventDetail(e *repository.Event) EventDetail {
	detail := EventDetail{
		ID:                e.ID,
		Name:              e.Name,
		Slug:              e.Slug,
		Status:            string(e.Status),
		Discoverable:      e.Discoverable,
		FeeHandling:       e.FeeHandling,
		FeeBasisPoints:    s.fees.FeeBasisPoints,
		FeeIVABasisPoints: s.fees.FeeIVABasisPoints,
		CreatedAt:         e.CreatedAt,
	}
	if e.StartsAt.Valid {
		t := e.StartsAt.Time
		detail.StartsAt = &t
	}
	if e.EndsAt.Valid {
		t := e.EndsAt.Time
		detail.EndsAt = &t
	}
	if e.Timezone.Valid {
		v := e.Timezone.String
		detail.Timezone = &v
	}
	if e.VenueName.Valid {
		v := e.VenueName.String
		detail.VenueName = &v
	}
	if e.VenueAddress.Valid {
		v := e.VenueAddress.String
		detail.VenueAddress = &v
	}
	if e.Description.Valid {
		v := e.Description.String
		detail.Description = &v
	}
	if e.CoverImageKey.Valid {
		key := e.CoverImageKey.String
		detail.CoverImageKey = &key
		if s.storage != nil {
			url := s.storage.PublicURL(key)
			detail.CoverImageURL = &url
		}
	}
	if e.CoverVideoKey.Valid {
		key := e.CoverVideoKey.String
		detail.CoverVideoKey = &key
		if s.storage != nil {
			url := s.storage.PublicURL(key)
			detail.CoverVideoURL = &url
		}
	}
	return detail
}
