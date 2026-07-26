package service

import (
	"context"
	"time"
)

// OperatorEvent is one Event on the Operator Dashboard's Organization
// drill-down: its lifecycle status and schedule, and whether it advertises
// itself in Storefront listings.
type OperatorEvent struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Status       string     `json:"status"`
	StartsAt     *time.Time `json:"starts_at"`
	Discoverable bool       `json:"discoverable"`
}

// ListOrganizationEventsForOperator returns every Event of one Organization,
// all statuses, for the Platform Operator's drill-down. It is not scoped to an
// acting Member: the caller's authority is the operator allowlist, checked in
// the middleware on the operator namespace (ADR 0015).
func (s *Service) ListOrganizationEventsForOperator(ctx context.Context, orgID string) ([]OperatorEvent, error) {
	rows, err := s.repo.ListEventsByOrganizationIDForOperator(ctx, orgID)
	if err != nil {
		return nil, err
	}
	events := make([]OperatorEvent, 0, len(rows))
	for _, e := range rows {
		item := OperatorEvent{
			ID:           e.ID,
			Name:         e.Name,
			Status:       string(e.Status),
			Discoverable: e.Discoverable,
		}
		if e.StartsAt.Valid {
			startsAt := e.StartsAt.Time
			item.StartsAt = &startsAt
		}
		events = append(events, item)
	}
	return events, nil
}

// CountEventsByOrganization returns the Event count of each given Organization,
// for the operator's Organization list. Organizations with no Events are absent
// from the map, and read as zero.
func (s *Service) CountEventsByOrganization(ctx context.Context, orgIDs []string) (map[string]int, error) {
	return s.repo.CountEventsByOrganizationIDs(ctx, orgIDs)
}
