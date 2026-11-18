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
// Customer markers: Active Customer (#817). A Customer is active on a
// platform day they acted as themselves, through a full Customer Session or
// by accepting a Ticket Assignment, and what records that is one marker
// holding the Customer and the day: no page, no time of day, no IP, no
// count. Unlike an Organization marker it is personal data, so it is written
// only while Customer recording is switched on, which it is not by default
// (ADR 0045).
//
// Markers are written here and read by the daily close (activity.go). The
// one read of Platform Trends that touches a marker is the breakdown of
// Active Organizations (activitybreakdown.go), which reads Organization
// markers for a bucket still inside the thirteen months. No read touches a
// Customer marker.

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

// CustomerMarkerStore keeps the Customer markers. The day recording began
// is not kept beside them, as it is for Organizations: the frozen totals say
// it themselves (ActivityTotalsStore.FirstActiveCustomersDay).
type CustomerMarkerStore interface {
	MarkCustomerActive(ctx context.Context, customerID, day string) error
	CustomerMarkersBetween(ctx context.Context, firstDay, lastDay string) ([]repository.CustomerMarker, error)
	FirstCustomerMarkerDay(ctx context.Context) (string, bool, error)
	DeleteCustomerMarkersBefore(ctx context.Context, day string) (int, error)
}

// markedSubjects is this process's memory of the last platform day it marked
// each subject on, an Organization or a Customer, which is what keeps every
// request from costing a write: at most one write per subject per day
// reaches the store from a process. It holds one day per subject, so it
// grows with the subjects seen and not with time.
type markedSubjects struct {
	mu      sync.Mutex
	lastDay map[string]string
}

func newMarkedSubjects() *markedSubjects {
	return &markedSubjects{lastDay: map[string]string{}}
}

// claim reports whether this process has yet to mark the subject on the day,
// and remembers it as marked. Of the requests that arrive together only the
// first is told true, so only one of them writes.
func (m *markedSubjects) claim(subjectID, day string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastDay[subjectID] == day {
		return false
	}
	m.lastDay[subjectID] = day
	return true
}

// release forgets a claim whose write did not land, so that the next request
// of the day tries again.
func (m *markedSubjects) release(subjectID, day string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastDay[subjectID] == day {
		delete(m.lastDay, subjectID)
	}
}

// WithOrganizationMarkerStore replaces the marker store and forgets which
// Organizations this process has marked (tests).
func (s *Service) WithOrganizationMarkerStore(store OrganizationMarkerStore) *Service {
	s.markers = store
	s.marked = newMarkedSubjects()
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

// WithCustomerMarkerStore replaces the Customer marker store and forgets
// which Customers this process has marked (tests).
func (s *Service) WithCustomerMarkerStore(store CustomerMarkerStore) *Service {
	s.customerMarkers = store
	s.markedCustomers = newMarkedSubjects()
	return s
}

// WithCustomerActivityRecording switches the recording of Customer markers
// on or off. It is off unless CUSTOMER_ACTIVITY_RECORDING_ENABLED says
// otherwise, and stays off until a Privacy Policy edition describing the
// recording is published (ADR 0045, ADR 0080): deploying this starts no
// collection.
//
// The one switch decides three things. While it is off no marker is
// written, the daily close freezes no Active Customers figure for the days
// it closes, and the read says the figure is not yet recorded for as long as
// no day has one.
func (s *Service) WithCustomerActivityRecording(enabled bool) *Service {
	s.customerRecording = enabled
	return s
}

// MarkCustomerActive records that a Customer acted as themselves at an
// instant: they are active on the platform day that instant falls on. It is
// called where a request is authenticated by a full Customer Session, where
// a sign-in mints one, and where a Ticket Assignment is accepted through an
// Assignment Link, and nowhere else. A Confirmation Link session is never
// passed here, since the link may have been forwarded, and neither is the
// Customer of a sale staff recorded.
//
// While Customer recording is off it does nothing at all. While it is on it
// behaves as MarkOrganizationActive does: the first call of a day for a
// Customer writes their marker, every later one returns without touching the
// store, and it returns nothing, a write that fails or outlasts
// markerWriteTimeout being reported and left to the next request of the day.
//
// What is reported names the day and not the Customer: the report leaves
// the platform, and a Customer beside a day is what ADR 0080 keeps inside.
func (s *Service) MarkCustomerActive(ctx context.Context, customerID string, at time.Time) {
	if !s.customerRecording {
		return
	}
	store, marked := s.customerMarkers, s.markedCustomers
	day := civilDate(at).Format(dayFormat)
	if !marked.claim(customerID, day) {
		return
	}
	writing, cancel := context.WithTimeout(context.WithoutCancel(ctx), markerWriteTimeout)
	defer cancel()
	if err := store.MarkCustomerActive(writing, customerID, day); err != nil {
		marked.release(customerID, day)
		s.logger.Error(ctx, "customer activity marker not written", "day", day, "error", err)
	}
}
