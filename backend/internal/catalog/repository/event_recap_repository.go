package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// The Event Recap's candidate query and its record of what was sent (spec
// #884, #891): the Events due their one recap at a run, in the shape
// catalog.EventRecapDue decides about.

// eventRecapDue is catalog.EventRecapDue's clauses in SQL, in the same order,
// parameterised on the go-live instant ($1) and the settle cutoff ($2 = the
// run's 09:00 less six hours), followed by the one clause the Go rule cannot
// state: no recap sent, and none held by a sweep at this moment ($3).
// COALESCE(e.ends_at, e.starts_at) is catalog.EventOverAt, and the expression
// migration 057 indexes.
//
// THE SAME RULE LIVES TWICE, IN SQL AND IN GO, and moves together: the service
// applies the Go rule to every row anyway, and a disagreement resolves as
// silence. The last clause is a filter and not the guard: a sweep that read an
// Event as due still has to claim it (ClaimEventRecap) before it writes.
const eventRecapDue = `
	e.status = '` + catalog.EventStatusPublished + `'
	AND COALESCE(e.ends_at, e.starts_at) IS NOT NULL
	AND COALESCE(e.ends_at, e.starts_at) >= $1
	AND COALESCE(e.ends_at, e.starts_at) <= $2
	AND NOT EXISTS (
		SELECT 1 FROM event_recaps r
		WHERE r.event_id = e.id AND (r.sent_at IS NOT NULL OR r.claimed_until > $3)
	)
`

// ListEventRecapCandidates returns the Events due an Event Recap, the one
// over longest first, up to limit. `e.id` breaks ties so the order is total,
// and an Event a run did not reach is nearer the front of the next.
func (r *Repository) ListEventRecapCandidates(
	ctx context.Context,
	wentLiveAt time.Time,
	settleCutoff time.Time,
	now time.Time,
	limit int,
) ([]catalog.EventRecapCandidate, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT e.id, e.organization_id, e.name, e.status, e.starts_at, e.ends_at,
		       e.registration_mode, e.registration_click_count
		FROM events e
		WHERE `+eventRecapDue+`
		ORDER BY COALESCE(e.ends_at, e.starts_at) ASC, e.id ASC
		LIMIT $4
	`, wentLiveAt, settleCutoff, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	candidates := make([]catalog.EventRecapCandidate, 0)
	for rows.Next() {
		var c catalog.EventRecapCandidate
		var startsAt, endsAt sql.NullTime
		var registrationMode string
		var registrationClicks int64
		if err := rows.Scan(&c.EventID, &c.OrganizationID, &c.EventName, &c.Status, &startsAt, &endsAt,
			&registrationMode, &registrationClicks); err != nil {
			return nil, err
		}
		// The count is carried only for an Event with External Registration:
		// one that sells Ticket Types has no Registration Link to click.
		if catalog.RegistrationModeOrDefault(registrationMode) == catalog.RegistrationModeExternal {
			clicks := int(registrationClicks)
			c.RegistrationLinkClicks = &clicks
		}
		if startsAt.Valid {
			c.StartsAt = startsAt.Time
		}
		if endsAt.Valid {
			c.EndsAt = endsAt.Time
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// CountEventRecapCandidates is how many Events are due an Event Recap, batch
// or no batch: the backlog the sweep reports.
func (r *Repository) CountEventRecapCandidates(ctx context.Context, wentLiveAt, settleCutoff, now time.Time) (int, error) {
	var n int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM events e WHERE `+eventRecapDue,
		wentLiveAt, settleCutoff, now).Scan(&n)
	return n, err
}

// ClaimEventRecap takes one Event for the sweep about to write its recap,
// until leaseUntil, and reports whether it was taken. It is not when the Event
// was recapped, or when another sweep holds it and its lease has not run out.
//
// ONE STATEMENT, so two overlapping sweeps cannot both take one Event: the
// insert of a first claim and the renewal of a lapsed one are the same upsert,
// and the row lock it takes makes the second claimant see the first's lease.
func (r *Repository) ClaimEventRecap(ctx context.Context, eventID string, now, leaseUntil time.Time) (bool, error) {
	var claimed string
	err := r.db.Pool.QueryRowContext(ctx, `
		INSERT INTO event_recaps (event_id, claimed_until) VALUES ($1, $3)
		ON CONFLICT (event_id) DO UPDATE SET claimed_until = EXCLUDED.claimed_until
		WHERE event_recaps.sent_at IS NULL AND event_recaps.claimed_until <= $2
		RETURNING event_id
	`, eventID, now, leaseUntil).Scan(&claimed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// ReleaseEventRecap gives back a claim no mail went out under, so the Event is
// due again at once. A recap already recorded is left alone.
func (r *Repository) ReleaseEventRecap(ctx context.Context, eventID string) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		DELETE FROM event_recaps WHERE event_id = $1 AND sent_at IS NULL
	`, eventID)
	return err
}

// RecordEventRecapSent records that one Event was recapped, on the claim the
// sweep holds. Called AFTER a reader's provider accepted the mail, never
// before, or for an Event with nobody to write to. A second call for the same
// Event keeps the first instant: the Event was recapped once, then.
func (r *Repository) RecordEventRecapSent(ctx context.Context, eventID string, sentAt time.Time) error {
	res, err := r.db.Pool.ExecContext(ctx, `
		UPDATE event_recaps SET sent_at = $2 WHERE event_id = $1 AND sent_at IS NULL
	`, eventID, sentAt)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		var recorded bool
		if err := r.db.Pool.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM event_recaps WHERE event_id = $1 AND sent_at IS NOT NULL)
		`, eventID).Scan(&recorded); err != nil {
			return err
		}
		if !recorded {
			return fmt.Errorf("event recap for %s recorded without a claim", eventID)
		}
	}
	return nil
}
