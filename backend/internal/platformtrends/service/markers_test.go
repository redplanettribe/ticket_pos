package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMonthsBeforeNeverSpillsIntoTheNextMonth(t *testing.T) {
	t.Parallel()

	cases := []struct{ day, want string }{
		{"2027-08-04", "2026-07-04"},
		{"2027-01-15", "2025-12-15"},
		// February 2026 has no 31st, 30th or 29th.
		{"2027-03-31", "2026-02-28"},
		{"2027-03-29", "2026-02-28"},
		{"2027-03-28", "2026-02-28"},
		{"2027-04-01", "2026-03-01"},
		// February 2028 has a 29th.
		{"2029-03-31", "2028-02-29"},
	}
	for _, c := range cases {
		day, err := time.Parse(dayFormat, c.day)
		if err != nil {
			t.Fatal(err)
		}
		if got := monthsBefore(day, markerRetentionMonths).Format(dayFormat); got != c.want {
			t.Errorf("thirteen months before %s = %s, want %s", c.day, got, c.want)
		}
	}
}

// countingMarkerStore counts the marker writes that reach it and holds each
// one open until released, so that every request of a burst arrives while
// the first write is still in flight.
type countingMarkerStore struct {
	OrganizationMarkerStore
	writes  atomic.Int64
	release chan struct{}
}

func (s *countingMarkerStore) MarkOrganizationActive(context.Context, string, string) error {
	s.writes.Add(1)
	<-s.release
	return nil
}

func TestRequestsArrivingTogetherWriteOneMarker(t *testing.T) {
	t.Parallel()

	store := &countingMarkerStore{release: make(chan struct{})}
	svc := New(nil, store, nil, nil, nil, nil, nil, nil, nil)
	at := time.Date(2026, 7, 5, 17, 0, 0, 0, time.UTC)

	var requests sync.WaitGroup
	for i := 0; i < 50; i++ {
		requests.Add(1)
		go func() {
			defer requests.Done()
			svc.MarkOrganizationActive(context.Background(), "organization-1", at)
		}()
	}
	close(store.release)
	requests.Wait()

	if got := store.writes.Load(); got != 1 {
		t.Fatalf("%d marker writes from 50 requests arriving together, want 1", got)
	}
}

// countingCustomerMarkerStore is countingMarkerStore for Customer markers.
type countingCustomerMarkerStore struct {
	CustomerMarkerStore
	writes  atomic.Int64
	release chan struct{}
}

func (s *countingCustomerMarkerStore) MarkCustomerActive(context.Context, string, string) error {
	s.writes.Add(1)
	<-s.release
	return nil
}

func TestCustomerRequestsArrivingTogetherWriteOneMarkerAndNoneWhileRecordingIsOff(t *testing.T) {
	t.Parallel()

	store := &countingCustomerMarkerStore{release: make(chan struct{})}
	close(store.release)
	svc := New(nil, nil, store, nil, nil, nil, nil, nil, nil)
	at := time.Date(2026, 7, 5, 17, 0, 0, 0, time.UTC)

	burst := func() {
		var requests sync.WaitGroup
		for i := 0; i < 50; i++ {
			requests.Add(1)
			go func() {
				defer requests.Done()
				svc.MarkCustomerActive(context.Background(), "customer-1", at)
			}()
		}
		requests.Wait()
	}

	// Off is how the service is built.
	burst()
	if got := store.writes.Load(); got != 0 {
		t.Fatalf("%d marker writes with Customer recording off, want none", got)
	}

	svc.WithCustomerActivityRecording(true)
	burst()
	if got := store.writes.Load(); got != 1 {
		t.Fatalf("%d marker writes from 50 requests arriving together, want 1", got)
	}
}
