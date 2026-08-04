// Package repository provides hand-written SQL data access for the Follow
// Digest pipeline (#220, parent #215, ADR 0030): the pending-Digest queue, the
// sent-ledger, and the one query that decides what a Digest is about.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Repository is the Digest pipeline's data access.
type Repository struct {
	db *platform.DB
}

// New builds a Repository over the shared connection pool.
func New(db *platform.DB) *Repository {
	return &Repository{db: db}
}

// PendingDigest is one claimed row of the queue: whose Digest it is, which week,
// and how many times delivery has already been tried.
//
// It carries no Events, and it never will. What a Digest is ABOUT is read at
// send time by DigestCandidates below, against the world as it is when the
// message actually goes out — see the comment on migration 055.
type PendingDigest struct {
	ID           string
	CustomerID   string
	WeekStart    time.Time
	AttemptCount int
}

// DigestRecipient is the Customer a claimed Digest is addressed to, as the
// composition needs them.
type DigestRecipient struct {
	Email     string
	FirstName string
	// Locale is the Customer's remembered Digest Locale (#216). Never empty: the
	// column is NOT NULL DEFAULT 'en'.
	Locale string
}

// DigestCandidate is one Event a Customer's Follows matched, with the reasons
// they matched it.
type DigestCandidate struct {
	EventID          string
	Name             string
	Slug             string
	StartsAt         sql.NullTime
	Timezone         sql.NullString
	OrganizationName string
	OrganizationSlug string
	// MatchedOrganization is true when the Customer Follows the Organization
	// putting this Event on.
	MatchedOrganization bool
	// MatchedTagKeys are the canonical keys of the Tags the Customer Follows that
	// this Event carries. Canonical KEYS rather than display names, because the
	// name a reader sees depends on their Digest Locale and resolving that is
	// catalog's rule, not this query's (catalog/service.LocalizedTagNames).
	MatchedTagKeys []string
}

// EnqueueWeek creates one pending Digest for every eligible Customer for the
// given week, and reports how many rows it created and how many were already
// there.
//
// ELIGIBILITY IS TWO FACTS and both are enforced here rather than in the
// service, because both are set-shaped: at least one Follow of either kind, and
// a verified Customer.
//
// The Follow requirement is why this query starts from the Follow tables and
// unions them rather than starting from `customers` and filtering. A Customer
// who has never pressed Follow has never asked to be written to, and mailing
// them is the difference between this feature and spam; starting from
// `customers` makes "has no Follows" a condition somebody can forget, and
// starting from the Follows makes it the shape of the query.
//
// The verification requirement is ADR 0010 held at the send. A Customer record
// created by a box office sale or an import is an address somebody typed at a
// counter, and nobody has proven they control it. Follows can only be pressed
// from a full Customer Session today, so the two conditions overlap completely
// in practice — this is the guard for the write path that does not exist yet,
// and it costs one predicate.
//
// ON CONFLICT DO NOTHING is what makes running this twice harmless: the unique
// constraint on (customer_id, week_start) refuses the second row, and the count
// of what was NOT inserted is reported rather than swallowed, so an operator who
// curls the endpoint after the cron already fired sees "already enqueued" rather
// than a suspicious zero.
func (r *Repository) EnqueueWeek(ctx context.Context, weekStart, now time.Time) (eligible, enqueued int, err error) {
	err = r.db.Pool.QueryRowContext(ctx, `
		WITH following AS (
			SELECT DISTINCT customer_id FROM (
				SELECT customer_id FROM customer_organization_follows
				UNION
				SELECT customer_id FROM customer_tag_follows
			) f
		),
		eligible AS (
			SELECT c.id
			FROM customers c
			JOIN following f ON f.customer_id = c.id
			WHERE c.verified_at IS NOT NULL
			  AND c.deleted_at IS NULL
		),
		inserted AS (
			INSERT INTO follow_digests (customer_id, week_start, status, next_attempt_at)
			SELECT e.id, $1::date, 'pending', $2
			FROM eligible e
			ON CONFLICT (customer_id, week_start) DO NOTHING
			RETURNING 1
		)
		SELECT (SELECT count(*) FROM eligible), (SELECT count(*) FROM inserted)
	`, weekStart, now).Scan(&eligible, &enqueued)
	if err != nil {
		return 0, 0, err
	}
	return eligible, enqueued, nil
}

// ClaimDueDigest takes the oldest due pending Digest out of the queue and hides
// it from other claimants until leaseUntil, returning nil when nothing is due.
//
// Written as ClaimDueReversalRequest is, and for the same reasons. FOR UPDATE
// SKIP LOCKED means two overlapping drain ticks never fight over one row and
// never wait on each other. Moving `next_attempt_at` forward to the lease is
// what takes the row out of the queue for everybody, so a claim survives the
// claiming instance dying: nothing here needs a lock table or a worker
// identity, and a row whose worker vanished simply comes due again.
//
// The attempt count is incremented BY THE CLAIM rather than by the failure that
// follows it. A drain that dies mid-send has still used an attempt — the message
// may well have been delivered — and an attempt counted only on a clean failure
// is an attempt bound that a crash loop never reaches.
func (r *Repository) ClaimDueDigest(ctx context.Context, now, leaseUntil time.Time) (*PendingDigest, error) {
	var out PendingDigest
	err := r.db.Pool.QueryRowContext(ctx, `
		UPDATE follow_digests
		SET next_attempt_at = $2,
		    attempt_count = attempt_count + 1,
		    updated_at = NOW()
		WHERE id = (
			SELECT d.id
			FROM follow_digests d
			WHERE d.status = 'pending'
			  AND d.next_attempt_at <= $1
			ORDER BY d.next_attempt_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, customer_id, week_start, attempt_count
	`, now, leaseUntil).Scan(&out.ID, &out.CustomerID, &out.WeekStart, &out.AttemptCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// LoadRecipient reads the Customer a claimed Digest is addressed to.
//
// A Customer deleted between the claim and this read comes back nil, and the
// caller finishes the Digest without sending anything. The cascade would have
// taken the queue row with them, so this is the narrow window between the two
// statements rather than an ordinary case.
func (r *Repository) LoadRecipient(ctx context.Context, customerID string) (*DigestRecipient, error) {
	var out DigestRecipient
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT email, first_name, digest_locale
		FROM customers
		WHERE id = $1 AND deleted_at IS NULL
	`, customerID).Scan(&out.Email, &out.FirstName, &out.Locale)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DigestCandidates is what one Customer's Digest is about: the eligible Events
// their Follows match and that they have not already been shown, soonest first.
//
// This is the composition, and there are four rules in it.
//
// ELIGIBILITY MIRRORS THE PUBLIC EXPLORER EXACTLY — published, discoverable, and
// not yet ended (`COALESCE(ends_at, starts_at) >= now()`, which also excludes an
// Event with no date at all). The predicate is copied from
// catalog/repository.ListDiscoverableEvents deliberately and must stay equal to
// it: `discoverable = FALSE` is an Organization deciding an Event is not to be
// advertised, and a Digest reaching further than the explorer would take that
// decision and overturn it in every follower's inbox. Anything looser here is
// not a bug in a listing, it is mail nobody could recall.
//
// THE LEDGER IS AN ANTI-JOIN, and it is doing three of ADR 0030's four jobs at
// once. It is the New to You test — an Event already sent to THIS Customer is
// not news to them, however recently it was published. It is the week-to-week
// anti-repetition, without which a standing Event would be mailed every week for
// as long as it stayed upcoming. And it is the send idempotency, because a
// retried Digest composes against the rows the earlier attempt wrote.
//
// THE MATCH IS A UNION OF THE TWO FOLLOW KINDS, GROUPED BACK TO ONE ROW PER
// EVENT, which is the fourth job: cross-Follow dedupe. A Customer who Follows
// both an Organization and a Tag one Event carries is matched twice and told
// once. Grouping rather than DISTINCT because both reasons are kept — which
// Follow matched is rendered as the Digest's "because you follow", and ADR 0030
// asks that it be recorded so Tag stuffing can be measured before anything is
// legislated against it.
//
// SOONEST FIRST, tie-broken on the id. The order is what the reader acts on, and
// a total order is what stops two identical composes producing two different
// emails.
func (r *Repository) DigestCandidates(ctx context.Context, customerID string, now time.Time) ([]DigestCandidate, error) {
	// ONE ROW PER MATCH, grouped into one entry per Event in Go below rather than
	// aggregated into an array in SQL. The array would have been shorter to write
	// and it would have had to cross the database/sql boundary as a Postgres
	// text[], which this driver pairing does not scan into a Go slice without a
	// type-specific helper. Grouping in Go costs a loop and keeps the query
	// something a person can run by hand during an incident.
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT e.id,
		       e.name,
		       e.slug,
		       e.starts_at,
		       e.timezone,
		       o.name,
		       o.slug,
		       m.matched_organization,
		       m.matched_tag_key
		FROM events e
		JOIN organizations o ON o.id = e.organization_id
		JOIN (
			-- The Organization half of the match: every Event of an Organization
			-- this Customer Follows.
			SELECT e2.id AS event_id, TRUE AS matched_organization, NULL::text AS matched_tag_key
			FROM customer_organization_follows f
			JOIN events e2 ON e2.organization_id = f.organization_id
			WHERE f.customer_id = $1
			UNION ALL
			-- The Tag half: every Event carrying a Tag this Customer Follows. One
			-- row per matching Tag, so an Event carrying two Followed Tags names
			-- both of them.
			SELECT et.event_id, FALSE, t.canonical_key
			FROM customer_tag_follows f
			JOIN tags t ON t.id = f.tag_id
			JOIN event_tags et ON et.tag_id = f.tag_id
			WHERE f.customer_id = $1
		) m ON m.event_id = e.id
		WHERE e.status = 'published'
		  AND e.discoverable = TRUE
		  AND COALESCE(e.ends_at, e.starts_at) >= $2
		  AND NOT EXISTS (
		    SELECT 1 FROM follow_digest_sent_events l
		    WHERE l.customer_id = $1 AND l.event_id = e.id
		  )
		ORDER BY e.starts_at ASC, e.id ASC
	`, customerID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// The dedupe itself: several match rows for one Event fold into one entry,
	// keeping every reason. `out` preserves the query's order, which is the order
	// the Digest lists Events in.
	var out []DigestCandidate
	index := make(map[string]int)
	for rows.Next() {
		var (
			row        DigestCandidate
			byOrg      bool
			matchedTag sql.NullString
		)
		if err := rows.Scan(
			&row.EventID, &row.Name, &row.Slug, &row.StartsAt, &row.Timezone,
			&row.OrganizationName, &row.OrganizationSlug,
			&byOrg, &matchedTag,
		); err != nil {
			return nil, err
		}
		at, seen := index[row.EventID]
		if !seen {
			out = append(out, row)
			at = len(out) - 1
			index[row.EventID] = at
		}
		if byOrg {
			out[at].MatchedOrganization = true
		}
		if matchedTag.Valid && !containsKey(out[at].MatchedTagKeys, matchedTag.String) {
			out[at].MatchedTagKeys = append(out[at].MatchedTagKeys, matchedTag.String)
		}
	}
	return out, rows.Err()
}

func containsKey(keys []string, key string) bool {
	for _, existing := range keys {
		if existing == key {
			return true
		}
	}
	return false
}

// MarkDigestSent records a delivered Digest and everything it carried, in ONE
// transaction.
//
// The two writes are inseparable and that is the whole point of this method. A
// `sent` row without its ledger rows would let the next week re-advertise
// everything this Digest just advertised; ledger rows without the `sent` row
// would leave a Digest to be retried with nothing left to say, and the Customer
// would get silence where a message was owed. Neither half is meaningful alone.
//
// It runs AFTER the provider has accepted the message, never before. The order
// costs a known and bounded risk — an instance dying between the accepted send
// and this commit leaves the Digest pending, and the retry sends a second copy —
// and buys the invariant migration 054 rests on: a ledger row means somebody was
// actually shown that Event. The other order trades a duplicate email nobody
// minds for an Event permanently invisible to a reader, which nothing would ever
// notice.
func (r *Repository) MarkDigestSent(ctx context.Context, digestID, customerID string, eventIDs []string, sentAt time.Time) error {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE follow_digests
		SET status = 'sent', sent_at = $2, last_error = NULL, updated_at = NOW()
		WHERE id = $1
	`, digestID, sentAt); err != nil {
		return err
	}

	// ON CONFLICT DO NOTHING because a retry that already wrote some of these
	// rows must not fail on them. The pair is the fact; writing it twice is the
	// same fact.
	//
	// The rows are spelled out as placeholders rather than passed as an array
	// parameter, for the reason DigestCandidates does its grouping in Go: an
	// array crossing this driver pairing needs a type-specific helper, and a
	// Digest carries few enough Events that the explicit form costs nothing.
	if len(eventIDs) > 0 {
		values := make([]string, 0, len(eventIDs))
		args := make([]any, 0, len(eventIDs)+2)
		args = append(args, customerID, sentAt)
		for i, eventID := range eventIDs {
			values = append(values, fmt.Sprintf("($1, $%d, $2)", i+3))
			args = append(args, eventID)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO follow_digest_sent_events (customer_id, event_id, sent_at)
			VALUES `+strings.Join(values, ", ")+`
			ON CONFLICT (customer_id, event_id) DO NOTHING
		`, args...); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// MarkDigestEmpty records that a Digest was composed and had nothing to say, so
// no email was sent.
//
// A terminal state of its own rather than a quiet `sent`, because the two are
// different facts and only one of them means a person was written to. It is also
// what stops the week being retried: nothing changed, and composing it again
// next tick would find the same nothing.
func (r *Repository) MarkDigestEmpty(ctx context.Context, digestID string) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		UPDATE follow_digests
		SET status = 'empty', last_error = NULL, updated_at = NOW()
		WHERE id = $1
	`, digestID)
	return err
}

// RescheduleDigest hands a failed Digest back to the queue, due again at
// nextAttemptAt and carrying what went wrong.
//
// The row stays `pending`, which is what makes a delivery failure cost a
// Customer minutes rather than their week: the queue is a table precisely so
// that a provider being down for a minute is survivable by a process that has
// since been redeployed.
func (r *Repository) RescheduleDigest(ctx context.Context, digestID string, nextAttemptAt time.Time, lastError string) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		UPDATE follow_digests
		SET next_attempt_at = $2, last_error = left($3, 500), updated_at = NOW()
		WHERE id = $1
	`, digestID, nextAttemptAt, lastError)
	return err
}

// AbandonDigest records that the platform has stopped trying to deliver this
// Digest.
//
// Giving up is the right ending here, unlike on a Reversal Request where nobody
// may ever stop caring what happened to somebody's money. A Digest is about the
// week it names; one that could not be delivered within that week has nothing
// left to be. Retrying it into the next week would deliver a stale email AND
// consume the Events it carried out of the ledger, so the following week's real
// Digest would be the poorer for it.
func (r *Repository) AbandonDigest(ctx context.Context, digestID, lastError string) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		UPDATE follow_digests
		SET status = 'failed', last_error = left($2, 500), updated_at = NOW()
		WHERE id = $1
	`, digestID, lastError)
	return err
}

// CountPendingDigests reports how many Digests are still waiting once a run has
// finished, and the week the oldest of them belongs to.
//
// It is the drain response's answer to "is this getting better or worse", which
// is the question an operator has during the one hour a week this pipeline does
// any work. Two curls a minute apart answer it without a database session.
func (r *Repository) CountPendingDigests(ctx context.Context) (int, sql.NullTime, error) {
	var total int
	var oldestWeek sql.NullTime
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT count(*), min(week_start)
		FROM follow_digests
		WHERE status = 'pending'
	`).Scan(&total, &oldestWeek)
	if err != nil {
		return 0, sql.NullTime{}, err
	}
	return total, oldestWeek, nil
}
