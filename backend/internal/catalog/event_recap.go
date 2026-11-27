package catalog

import (
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// This file holds the DUE RULE of the Event Recap: whether one Event is to be
// recapped by one run of the daily sweep (spec #884, #891).
//
// THE SAME RULE LIVES TWICE, IN GO AND IN SQL, for the reason
// assignment_reminder.go states: the sweep must be able to skip inside the
// database what it may not send. repository.eventRecapDue is these clauses in
// SQL, this is the statement of them, and the service applies THIS one to every
// candidate the SQL returned before anything is composed. A disagreement
// resolves as silence.

// EventStatusPublished is the status of an Event the public can see, as the
// events table spells it. Named here so the due rule and the SQL beside it
// (repository.eventRecapDue's `e.status = 'published'`) say the same word.
const EventStatusPublished = "published"

const (
	// EventRecapHour is the hour of the platform clock the Event Recap goes at:
	// 09:00 in America/Guayaquil, whatever the Event's own timezone.
	EventRecapHour = 9

	// EventRecapSettleTime is how long an Event must have been over before the
	// run's 09:00: six hours, so the night's door sales are in. An Event over
	// at 05:00 has not settled by 09:00 that morning and waits for the next.
	EventRecapSettleTime = 6 * time.Hour
)

// EventRecapRunAt is the 09:00 of the platform clock a run made at now is the
// run of: today's once the clock has reached it, yesterday's before.
//
// THE RUN IS NAMED BY ITS 09:00 AND NOT BY THE MOMENT IT WAS INVOKED, so a run
// a few seconds late, a retry and a run made by hand in the afternoon all
// decide exactly what the morning's run decided, and none of them recaps an
// Event that had not settled by 09:00.
func EventRecapRunAt(now time.Time) time.Time {
	return platform.LatestEcuadorHour(now, EventRecapHour)
}

// EventOverAt is the instant an Event is over, as the glossary defines it: its
// end, or its start where it has no end. The zero value for a dateless Event,
// which is never over.
func EventOverAt(startsAt, endsAt time.Time) time.Time {
	if !endsAt.IsZero() {
		return endsAt
	}
	return startsAt
}

// EventRecapInputs are the facts, and the only facts, that decide whether one
// Event is due its Event Recap.
type EventRecapInputs struct {
	// Status is the Event's status. Only a `published` Event is recapped:
	// never a draft, never a `cancelled` one.
	Status string
	// StartsAt and EndsAt are the Event's dates, each the zero value where the
	// Event names none.
	StartsAt time.Time
	EndsAt   time.Time
	// RunAt is the run's 09:00 (EventRecapRunAt), never the caller's choice of
	// moment.
	RunAt time.Time
	// Recapped is whether the Event was ever recapped. Once it is, nothing
	// makes it due again.
	Recapped bool
	// WentLiveAt is the go-live instant, platform.EventRecapWentLiveAt in
	// every deployment. An Event over before it is never recapped.
	WentLiveAt time.Time
}

// EventRecapDue reports whether one Event is due its Event Recap at one run.
//
// FIVE CLAUSES, ALL CONJUNCTIVE, and repository.eventRecapDue is the same
// clauses in the same order. Neither may be changed alone. The fourth clause
// implies the Event is over at the run, since the run's 09:00 is never after
// the moment the run is made.
func EventRecapDue(in EventRecapInputs) bool {
	overAt := EventOverAt(in.StartsAt, in.EndsAt)
	return in.Status == EventStatusPublished &&
		!overAt.IsZero() &&
		!overAt.Before(in.WentLiveAt) &&
		!overAt.After(in.RunAt.Add(-EventRecapSettleTime)) &&
		!in.Recapped
}

// EventRecapCandidate is one Event the sweep found due: the row EventRecapDue
// decides about, and what the mail is written from.
//
// IT CARRIES NO PERSON. Who is written to is resolved when the mail is sent,
// from the Event's assignments as they stand then.
type EventRecapCandidate struct {
	EventID        string
	OrganizationID string
	// EventName is what the mail's subject names.
	EventName string
	// Status, StartsAt and EndsAt are the facts EventRecapDue is applied to.
	// They travel with the candidate rather than being re-read, so the decision
	// is about the row the query returned.
	Status   string
	StartsAt time.Time
	EndsAt   time.Time
	// RegistrationLinkClicks is the Event's count of hand-offs to its
	// Registration Link as the candidate query read it (#888), for an Event
	// with External Registration, and nil for one that sells Ticket Types.
	// Clicks, never registrations or people. It is the Event's lifetime
	// counter, the one the staff app states: it has no hourly buckets, so it
	// cannot be cut at the instant the Event was over.
	RegistrationLinkClicks *int
}

// ExternalRegistration is whether the candidate is an Event with External
// Registration, which is what carrying Registration Link clicks means. Fixed
// once the Event is published, so it cannot change under a run.
func (c EventRecapCandidate) ExternalRegistration() bool {
	return c.RegistrationLinkClicks != nil
}

// Inputs is the candidate as EventRecapDue reads it, at one run. A candidate
// is by construction an Event with no recap recorded.
func (c EventRecapCandidate) Inputs(runAt time.Time) EventRecapInputs {
	return EventRecapInputs{
		Status:     c.Status,
		StartsAt:   c.StartsAt,
		EndsAt:     c.EndsAt,
		RunAt:      runAt,
		Recapped:   false,
		WentLiveAt: platform.EventRecapWentLiveAt,
	}
}
