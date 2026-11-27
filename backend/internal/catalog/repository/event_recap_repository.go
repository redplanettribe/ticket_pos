package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// The Event Recap's candidate query and its record of what was sent (spec
// #884, #891): the Events due their one recap at a run, in the shape
// catalog.EventRecapDue decides about.

// eventRecapDue is catalog.EventRecapDue's clauses in SQL, in the same order,
// parameterised on the go-live instant ($1) and the settle cutoff ($2 = the
// run's 09:00 less six hours). COALESCE(e.ends_at, e.starts_at) is
// catalog.EventOverAt, and the expression migration 057 indexes.
//
// THE SAME RULE LIVES TWICE, IN SQL AND IN GO, and moves together: the service
// applies the Go rule to every row anyway, and a disagreement resolves as
// silence.
const eventRecapDue = `
	e.status = 'published'
	AND COALESCE(e.ends_at, e.starts_at) IS NOT NULL
	AND COALESCE(e.ends_at, e.starts_at) >= $1
	AND COALESCE(e.ends_at, e.starts_at) <= $2
	AND NOT EXISTS (SELECT 1 FROM event_recaps r WHERE r.event_id = e.id)
`

// ListEventRecapCandidates returns the Events due an Event Recap, the one
// over longest first, up to limit. `e.id` breaks ties so the order is total,
// and an Event a run did not reach is nearer the front of the next.
func (r *Repository) ListEventRecapCandidates(
	ctx context.Context,
	wentLiveAt time.Time,
	settleCutoff time.Time,
	limit int,
) ([]catalog.EventRecapCandidate, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT e.id, e.organization_id, e.name, e.status, e.starts_at, e.ends_at
		FROM events e
		WHERE `+eventRecapDue+`
		ORDER BY COALESCE(e.ends_at, e.starts_at) ASC, e.id ASC
		LIMIT $3
	`, wentLiveAt, settleCutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	candidates := make([]catalog.EventRecapCandidate, 0)
	for rows.Next() {
		var c catalog.EventRecapCandidate
		var startsAt, endsAt sql.NullTime
		if err := rows.Scan(&c.EventID, &c.OrganizationID, &c.EventName, &c.Status, &startsAt, &endsAt); err != nil {
			return nil, err
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

// RecordEventRecapSent records that one Event was recapped. Called AFTER a
// recipient's provider accepted the mail, never before. A second call for the
// same Event keeps the first row: the Event was recapped once, at that instant.
func (r *Repository) RecordEventRecapSent(ctx context.Context, eventID string, sentAt time.Time) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		INSERT INTO event_recaps (event_id, sent_at) VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING
	`, eventID, sentAt)
	return err
}
