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

// markerWriteTimeout bounds the one write a day a request may carry, so a
// store that hangs holds a Member's request up for no longer than this.
const markerWriteTimeout = 2 * time.Second

// monthsBefore is the same day of the month the given number of calendar
// months earlier, or that month's last day where it has no such day: one
// month before the 31st of March is the last day of February. It never
// spills into the following month, as time.AddDate does.
func monthsBefore(day time.Time, months int) time.Time {
	firstOfMonth := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -months, 0)
	lastOfMonth := firstOfMonth.AddDate(0, 1, -1)
	if day.Day() > lastOfMonth.Day() {
		return lastOfMonth
	}
	return firstOfMonth.AddDate(0, 0, day.Day()-1)
}

// markedOrganizations is this process's memory of the last platform day it
// marked each Organization on, which is what keeps a Member's every request
// from costing a write: at most one write per Organization per day reaches
// the store from a process. It holds one day per Organization, so it grows
// with the Organizations worked in and not with time.
type markedOrganizations struct {
	mu      sync.Mutex
	lastDay map[string]string
}

func newMarkedOrganizations() *markedOrganizations {
	return &markedOrganizations{lastDay: map[string]string{}}
}

// claim reports whether this process has yet to mark the Organization on
// the day, and remembers it as marked. Of the requests that arrive together
// only the first is told true, so only one of them writes.
func (m *markedOrganizations) claim(organizationID, day string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastDay[organizationID] == day {
		return false
	}
	m.lastDay[organizationID] = day
	return true
}

// release forgets a claim whose write did not land, so that the next request
// of the day tries again.
func (m *markedOrganizations) release(organizationID, day string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastDay[organizationID] == day {
		delete(m.lastDay, organizationID)
	}
}

// WithOrganizationMarkerStore replaces the marker store and forgets which
// Organizations this process has marked (tests).
func (s *Service) WithOrganizationMarkerStore(store OrganizationMarkerStore) *Service {
	s.markers = store
	s.marked = newMarkedOrganizations()
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
// marker that cannot be written, or not within markerWriteTimeout, is
// reported and the request it rode on goes ahead, and the next request of
// the day writes it instead.
//
// Requests stamped either side of midnight that arrive out of order may
// each write their day more than once. The store takes a marker that exists
// as a no-op, so that costs a write and nothing else.
func (s *Service) MarkOrganizationActive(ctx context.Context, organizationID string, at time.Time) {
	store, marked := s.markers, s.marked
	day := civilDate(at).Format(dayFormat)
	if !marked.claim(organizationID, day) {
		return
	}
	// The marker outlives the request that happened to carry it: a client
	// that hangs up early must not cost the Organization its day.
	writing, cancel := context.WithTimeout(context.WithoutCancel(ctx), markerWriteTimeout)
	defer cancel()
	if err := store.MarkOrganizationActive(writing, organizationID, day); err != nil {
		marked.release(organizationID, day)
		s.logger.Error(ctx, "organization activity marker not written",
			"organization_id", organizationID, "day", day, "error", err)
	}
}
