package service

import (
	"context"
	"sync"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platformtrends/repository"
)

// Organization markers: the Member half of Active Organization (#816, ADR
// 0080). An Organization is active on a platform day a Member worked in it
// through a Staff Session, and what records that is one marker holding the
// Organization and the day: no Member, no time of day, no count.
//
// Markers are written here and read by the daily close alone (activity.go).
// No read of Platform Trends touches them.

// OrganizationMarkerStore keeps the Organization markers and the day the
// daily close first froze with them.
type OrganizationMarkerStore interface {
	MarkOrganizationActive(ctx context.Context, organizationID, day string) error
	OrganizationMarkersBetween(ctx context.Context, firstDay, lastDay string) ([]repository.OrganizationMarker, error)
	FirstOrganizationMarkerDay(ctx context.Context) (string, bool, error)
	DeleteOrganizationMarkersBefore(ctx context.Context, day string) (int, error)
	RecordOrganizationMarkersBegan(ctx context.Context, day string) error
	OrganizationMarkersBeganOn(ctx context.Context) (string, bool, error)
}

// markerRetentionMonths is how long a marker is kept: thirteen months, one
// full year-over-year recheck of a frozen figure (ADR 0080).
const markerRetentionMonths = 13

// organizationMarkers is the marker store and this process's memory of the
// last day it marked each Organization on, which is what keeps a Member's
// every request from costing a write: at most one write per Organization per
// day reaches the store from a process. The memory holds one day per
// Organization, so it grows with the Organizations worked in and not with
// time.
type organizationMarkers struct {
	store OrganizationMarkerStore

	mu     sync.Mutex
	marked map[string]string
}

func newOrganizationMarkers(store OrganizationMarkerStore) *organizationMarkers {
	return &organizationMarkers{store: store, marked: map[string]string{}}
}

// claim reports whether this process has yet to mark the Organization on
// the day, and remembers it as marked. Of the requests that arrive together
// only the first is told true, so only one of them writes.
func (m *organizationMarkers) claim(organizationID, day string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.marked[organizationID] == day {
		return false
	}
	m.marked[organizationID] = day
	return true
}

// release forgets a claim whose write did not land, so that the next request
// of the day tries again.
func (m *organizationMarkers) release(organizationID, day string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.marked[organizationID] == day {
		delete(m.marked, organizationID)
	}
}

// WithOrganizationMarkerStore replaces the marker store and forgets which
// Organizations this process has marked (tests).
func (s *Service) WithOrganizationMarkerStore(store OrganizationMarkerStore) *Service {
	s.markers = newOrganizationMarkers(store)
	return s
}

// MarkOrganizationActive records that a Member worked in the Organization at
// an instant: the Organization is active on the platform day that instant
// falls on. It is called where a request's Active Member is loaded, which the
// Operator Dashboard's routes never do, so a Platform Operator reading the
// platform marks nothing.
//
// The first call of a day for an Organization writes its marker and every
// later one returns without touching the store. It returns nothing: a
// marker that cannot be written is reported and the request it rode on goes
// ahead, and the next request of the day writes it instead.
func (s *Service) MarkOrganizationActive(ctx context.Context, organizationID string, at time.Time) {
	markers := s.markers
	day := civilDate(at).Format(dayFormat)
	if !markers.claim(organizationID, day) {
		return
	}
	// The marker outlives the request that happened to carry it: a client
	// that hangs up early must not cost the Organization its day.
	if err := markers.store.MarkOrganizationActive(context.WithoutCancel(ctx), organizationID, day); err != nil {
		markers.release(organizationID, day)
		s.logger.Error(ctx, "organization activity marker not written",
			"organization_id", organizationID, "day", day, "error", err)
	}
}
