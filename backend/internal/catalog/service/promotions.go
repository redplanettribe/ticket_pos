package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
)

// PromotionView is a Ticket Type's Promotion as the staff API renders it. The
// timestamps are RFC3339 instants; showing them in the Event's timezone is the
// client's job.
type PromotionView struct {
	PromotionalPriceCents int        `json:"promotional_price_cents"`
	StartsAt              *time.Time `json:"starts_at"`
	EndsAt                time.Time  `json:"ends_at"`
}

// SetPromotionInput is the whole Promotion: a Promotional Price and the window
// it holds for. Both writes take all of it — there is no partial edit of a
// Promotion, because a price without its window says nothing.
type SetPromotionInput struct {
	PromotionalPriceCents int
	StartsAt              *time.Time
	EndsAt                time.Time
}

// SetTicketTypePromotion fills the Ticket Type's one Promotion slot. A Ticket
// Type that already has one is refused rather than overwritten: staff edit or
// remove the Promotion they have (ADR 0021).
func (s *Service) SetTicketTypePromotion(
	ctx context.Context,
	actor ActorContext,
	eventID, ticketTypeID string,
	input SetPromotionInput,
) (*TicketTypeDetail, error) {
	ticketType, currency, err := s.loadTicketTypeForPromotion(ctx, actor, eventID, ticketTypeID)
	if err != nil {
		return nil, err
	}

	if input.PromotionalPriceCents >= ticketType.PriceCents {
		return nil, catalog.ErrPromotionalPriceNotBelowListPrice(input.PromotionalPriceCents, ticketType.PriceCents)
	}

	existing, err := s.repo.GetPromotionByTicketTypeID(ctx, ticketTypeID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, catalog.ErrPromotionAlreadyExists()
	}

	promotion, err := s.repo.CreatePromotion(ctx, ticketTypeID, promotionParams(input), s.now())
	if err != nil {
		// The slot's UNIQUE constraint answers the same question the read above
		// did, for the write that lost a race with a concurrent one.
		if repository.IsUniqueViolation(err) {
			return nil, catalog.ErrPromotionAlreadyExists()
		}
		return nil, err
	}

	detail := toTicketTypeDetail(ticketType, currency, promotion)
	return &detail, nil
}

// UpdateTicketTypePromotion replaces the price and window of the Promotion a
// Ticket Type already has.
func (s *Service) UpdateTicketTypePromotion(
	ctx context.Context,
	actor ActorContext,
	eventID, ticketTypeID string,
	input SetPromotionInput,
) (*TicketTypeDetail, error) {
	ticketType, currency, err := s.loadTicketTypeForPromotion(ctx, actor, eventID, ticketTypeID)
	if err != nil {
		return nil, err
	}

	existing, err := s.repo.GetPromotionByTicketTypeID(ctx, ticketTypeID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, catalog.ErrPromotionNotFound()
	}

	if input.PromotionalPriceCents >= ticketType.PriceCents {
		return nil, catalog.ErrPromotionalPriceNotBelowListPrice(input.PromotionalPriceCents, ticketType.PriceCents)
	}

	promotion, err := s.repo.UpdatePromotion(ctx, ticketTypeID, promotionParams(input), s.now())
	if err != nil {
		return nil, err
	}
	if promotion == nil {
		return nil, catalog.ErrPromotionNotFound()
	}

	detail := toTicketTypeDetail(ticketType, currency, promotion)
	return &detail, nil
}

// RemoveTicketTypePromotion empties the Promotion slot, returning the Ticket
// Type back at its List Price.
func (s *Service) RemoveTicketTypePromotion(
	ctx context.Context,
	actor ActorContext,
	eventID, ticketTypeID string,
) (*TicketTypeDetail, error) {
	ticketType, currency, err := s.loadTicketTypeForPromotion(ctx, actor, eventID, ticketTypeID)
	if err != nil {
		return nil, err
	}

	if err := s.repo.DeletePromotion(ctx, ticketTypeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, catalog.ErrPromotionNotFound()
		}
		return nil, err
	}

	detail := toTicketTypeDetail(ticketType, currency, nil)
	return &detail, nil
}

// loadTicketTypeForPromotion resolves the Event, the Ticket Type within it, and
// the Organization's currency — the preamble every Promotion write shares.
func (s *Service) loadTicketTypeForPromotion(
	ctx context.Context,
	actor ActorContext,
	eventID, ticketTypeID string,
) (*repository.TicketType, string, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, "", err
	}
	if event == nil {
		return nil, "", catalog.ErrEventNotFound()
	}

	ticketType, err := s.repo.GetTicketTypeByID(ctx, actor.OrganizationID, eventID, ticketTypeID)
	if err != nil {
		return nil, "", err
	}
	if ticketType == nil {
		return nil, "", catalog.ErrTicketTypeNotFound()
	}

	currency, err := s.repo.GetOrganizationCurrency(ctx, actor.OrganizationID)
	if err != nil {
		return nil, "", err
	}
	return ticketType, currency, nil
}

func promotionParams(input SetPromotionInput) repository.PromotionParams {
	return repository.PromotionParams{
		PromotionalPriceCents: input.PromotionalPriceCents,
		StartsAt:              nullTimeFromPtr(input.StartsAt),
		EndsAt:                input.EndsAt,
	}
}

// toPromotion maps a stored Promotion into the domain shape the effective-price
// rule is expressed over.
func toPromotion(p *repository.TicketTypePromotion) *catalog.Promotion {
	if p == nil {
		return nil
	}
	promotion := &catalog.Promotion{
		PromotionalPriceCents: p.PromotionalPriceCents,
		EndsAt:                p.EndsAt,
	}
	if p.StartsAt.Valid {
		startsAt := p.StartsAt.Time
		promotion.StartsAt = &startsAt
	}
	return promotion
}

func toPromotionView(p *repository.TicketTypePromotion) *PromotionView {
	promotion := toPromotion(p)
	if promotion == nil {
		return nil
	}
	return &PromotionView{
		PromotionalPriceCents: promotion.PromotionalPriceCents,
		StartsAt:              promotion.StartsAt,
		EndsAt:                promotion.EndsAt,
	}
}
