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
	// DigestEnabled is whether this Customer still wants the Digest (#224).
	//
	// It is read HERE, at send time, and not only at enqueue, because the enqueue
	// filter cannot cover the window it does not span: somebody who unsubscribes
	// between the weekly enqueue and the minute their Digest is drained already
	// has a row in the queue addressed to them. That window is small and it is
	// precisely when an opt-out matters most — the message is sitting there,
	// ready to go — so one press has to be enough to stop it.
	DigestEnabled bool
}

// DigestSections is what one Customer's Digest is about, split into the two
// sections a reader sees (#221) and each cut at the cap (#222).
//
// THE SPLIT IS MADE HERE, in the one query that already knows both the ledger
// and the clock, rather than by the service partitioning a flat list. Novelty is
// the ledger anti-join and the agenda is the ledger semi-join; they are the same
// join read in opposite directions, and computing one of them twice is how the
// two come to disagree about a single Event.
//
// The invariant the rest of the feature rests on: NO EventID APPEARS IN BOTH.
type DigestSections struct {
	// New is what this reader has never been shown, soonest first, at most
	// DigestSectionCap of them.
	New []DigestCandidate
	// Happening is what they were already shown and which starts within seven
	// days, soonest first, at most DigestSectionCap of them.
	Happening []DigestCandidate
	// NewOverflow and HappeningOverflow are what the cap shed from each section,
	// in the same order, and they are the reason the cap is not a truncation
	// (#222).
	//
	// THEY ARE CARRIED RATHER THAN DROPPED, and the mechanism is the caller's
	// discipline about which of these four slices reaches the ledger: only the
	// Events actually printed are recorded as shown, so everything here is still
	// New to You next week and arrives in a later Digest (see
	// service.deliverDigest). Keeping the remainder as CANDIDATES rather than as
	// a bare count is what lets the message say how many more there are and point
	// at a surface that holds them, from the Follows those Events actually
	// matched.
	//
	// The overflow of an agenda is a milder thing than the overflow of the news:
	// its Events are already in the ledger, so nothing about them is at risk of
	// being lost. They simply wait for a week with room, or for their doors to
	// come closer than the ten ahead of them.
	NewOverflow       []DigestCandidate
	HappeningOverflow []DigestCandidate
}

// Empty reports whether there is nothing at all to say, which is the one
// question the drain asks before deciding to send. Both sections empty means no
// message is composed and the Digest is recorded `empty`.
func (s DigestSections) Empty() bool {
	return len(s.New) == 0 && len(s.Happening) == 0
}

// DigestCandidate is one Event a Customer's Follows matched, with the reasons
// they matched it.
type DigestCandidate struct {
	EventID          string
	Name             string
	Slug             string
	StartsAt         sql.NullTime
	Timezone         sql.NullString
	VenueName        sql.NullString
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
	// Attending is whether THIS Customer already holds a live Ticket Sale for
	// this Event (#223) — the fact that turns "get tickets" into "you're going".
	//
	// LIVE means `status = 'active'` and nothing else. A reversed Ticket Sale
	// leaves its row exactly where the live one was, so a test for the row's
	// existence rather than its status would tell somebody who got their money
	// back that they still have a seat.
	//
	// It is FALSE ON AN EXTERNALLY REGISTERED EVENT by construction, not by
	// coincidence. ADR 0028 says such an Event sells no Tickets, so no
	// `ticket_sales` row could exist for it — but the query states the rule
	// anyway, because "we would never know whether they registered" is a fact
	// about the feature rather than an accident of which rows happen to exist.
	Attending bool
	// ExternallyRegistered is whether this Event sends its audience to a
	// third-party site to sign up rather than selling Tickets here (ADR 0028).
	//
	// It decides which call to action the entry carries — Register rather than a
	// purchase — and it is the reason Attending can never be true.
	ExternallyRegistered bool
}

// EnqueueWeek creates one pending Digest for every eligible Customer for the
// given week, and reports how many rows it created and how many were already
// there.
//
// ELIGIBILITY IS THREE FACTS and all are enforced here rather than in the
// service, because all are set-shaped: at least one Follow of either kind, a
// verified Customer, and a Customer who has not unsubscribed.
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
// THE UNSUBSCRIBE FILTER IS HERE, AT THE ENQUEUE, and that is where #224 asks
// for it: a row in this queue is a promise that somebody is owed a Digest, and a
// Customer who unsubscribed is owed nothing. Composing their week and discarding
// it would cost a candidate query and a compose per unsubscribed follower every
// week for a message that could never be sent, and it would leave the queue full
// of rows an operator reading a backlog has to know to ignore. The drain checks
// the same flag again for the one window this predicate cannot span — see
// DigestRecipient.DigestEnabled.
//
// It drops such a Customer out of ELIGIBLE and not merely out of ENQUEUED, which
// is the honest reading of that number: eligible counts the Customers this
// feature is about at all, and somebody who asked us to stop writing to them is
// not one of them. Leaving them eligible would make a week of unsubscribes look
// like the insert failing.
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
			  AND c.digest_enabled
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
		SELECT email, first_name, digest_locale, digest_enabled
		FROM customers
		WHERE id = $1 AND deleted_at IS NULL
	`, customerID).Scan(&out.Email, &out.FirstName, &out.Locale, &out.DigestEnabled)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// digestHappeningWindow is how far ahead "Happening this week" reaches.
//
// Seven days, which is the Digest's own cadence and the only number that makes
// the section complete: a shorter window would let an Event slip between two
// Digests and be reminded about never, and a longer one would remind the same
// reader about the same Event two weeks running, which is precisely what the
// ledger exists to stop.
const digestHappeningWindow = 7 * 24 * time.Hour

// DigestSectionCap is how many Events one section of a Digest may carry (#222).
//
// TEN, because the number is about a person reading an email on a phone rather
// than about a query. A Customer Following a busy Tag matches hundreds of
// Events; a mail that listed them would be a catalogue, and a catalogue is
// deleted unread — which costs that reader every Event in it, not merely the
// ones past the tenth.
//
// THE CAP IS ONLY SAFE BECAUSE THE OVERFLOW IS CARRIED. Soonest-first means what
// the cap sheds is the far-future Events, which are precisely the ones that can
// afford to wait: they resurface as they approach, and until then the ledger has
// never been told they were shown. A cap without the carry would be a silent
// truncation and the worse failure of the two — a reader would never learn that
// Following that Tag had hidden most of it from them.
//
// It is applied HERE rather than as a SQL LIMIT so that the shed Events are
// still in hand: the message has to say how many more there are and point
// somewhere that holds them, and both facts come from the Events themselves.
// The bound on the query is the Follows, which is the same bound the section cut
// already lived under.
const DigestSectionCap = 10

// DigestCandidates is what one Customer's Digest is about, in its two sections
// (#221): the eligible Events their Follows match, split into what they have
// never been shown and what they were shown and which now starts within seven
// days, each soonest first.
//
// This is the composition, and there are six rules in it.
//
// WHAT THIS READER ALREADY BOUGHT IS NOT NEWS (#223). An Event they hold a live
// Ticket Sale for is on the agenda and never in New, whether or not the ledger
// has ever carried it — the ledger answers "have we told them", and a ticket
// answers the stronger "do they already know", which is the question New is
// really asking. It is also what restores the week-before reminder: a purchase
// months ahead stays out of the Digest until its Event enters the seven-day
// window, then arrives once, marked as one they are going to.
//
// ELIGIBILITY MIRRORS THE PUBLIC EXPLORER EXACTLY — published, discoverable, and
// not yet ended (`COALESCE(ends_at, starts_at) >= now()`, which also excludes an
// Event with no date at all). The predicate is copied from
// catalog/repository.ListDiscoverableEvents deliberately and must stay equal to
// it: `discoverable = FALSE` is an Organization deciding an Event is not to be
// advertised, and a Digest reaching further than the explorer would take that
// decision and overturn it in every follower's inbox. Anything looser here is
// not a bug in a listing, it is mail nobody could recall. IT GOVERNS BOTH
// SECTIONS, which is the half easiest to lose: an Event already shown and since
// cancelled or unlisted must drop off the agenda too, and a "we already told
// them" exemption would mail out exactly the Events an Organization most wants
// unmailed.
//
// THE LEDGER IS READ IN BOTH DIRECTIONS, and it is doing four of ADR 0030's
// jobs. Read as an ANTI-join it is the New to You test — an Event already sent
// to THIS Customer is not news to them, however recently it was published — and
// with it the send idempotency, because a retried Digest composes against the
// rows the earlier attempt wrote. Read as a SEMI-join, narrowed to the next
// seven days, it is the agenda: the week-before reminder ADR 0030 folded into
// the Digest rather than sending as a second kind of mail. And the seven-day
// bound on that second reading is the week-to-week anti-repetition, without
// which an Event two months out would be re-listed every week until its doors
// opened.
//
// AN EVENT QUALIFYING FOR BOTH IS NEWS, ONCE. The two readings are complementary
// by construction — `shown` is true or it is not — so the overlap cannot happen
// here at all, which is the reason the sections are cut from ONE query rather
// than from two that could both claim an Event. A reader who meets the same
// Event twice in one email learns that this mail repeats itself.
//
// THE AGENDA IS "STILL FOLLOWED", NOT "EVERYTHING IN THE LEDGER". The match is
// the same join for both sections, so unfollowing an Organization drops its
// Events off the agenda at once. Two reasons: a reader who said they no longer
// want to hear from somebody must stop hearing from them, and every entry has to
// name the Follow that brought it — an entry with no surviving Follow has no
// true answer to give. The ledger row STAYS, which is what stops refollowing
// making an old Event news again.
//
// THE MATCH IS A UNION OF THE TWO FOLLOW KINDS, GROUPED BACK TO ONE ROW PER
// EVENT: cross-Follow dedupe, and it holds within each section. A Customer who
// Follows both an Organization and a Tag one Event carries is matched twice and
// told once. Grouping rather than DISTINCT because both reasons are kept — which
// Follow matched is rendered as the Digest's "because you follow", and ADR 0030
// asks that it be recorded so Tag stuffing can be measured before anything is
// legislated against it.
//
// SOONEST FIRST, tie-broken on the id. The order is what the reader acts on —
// both sections are lists of things with doors, and the one opening first is the
// one a decision has to be made about first — and a total order is what stops
// two identical composes producing two different emails.
func (r *Repository) DigestCandidates(ctx context.Context, customerID string, now time.Time) (DigestSections, error) {
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
		       e.venue_name,
		       o.name,
		       o.slug,
		       m.matched_organization,
		       m.matched_tag_key,
		       EXISTS (
		         SELECT 1 FROM follow_digest_sent_events l
		         WHERE l.customer_id = $1 AND l.event_id = e.id
		       ) AS already_shown,
		       -- Whether this reader is going (#223). status = 'active' and not
		       -- merely a row: ticket_sales keeps a reversed sale in place, and
		       -- somebody who undid their purchase must not be told they have a
		       -- seat. The registration-mode guard restates ADR 0028 rather than
		       -- trusting that no ticket_sales row could exist for an external
		       -- Event: registration happens off-platform and this system would
		       -- never learn it happened, whatever rows appear here later.
		       (
		         e.registration_mode <> 'external'
		         AND EXISTS (
		           SELECT 1 FROM ticket_sales ts
		           WHERE ts.event_id = e.id
		             AND ts.customer_id = $1
		             AND ts.status = 'active'
		         )
		       ) AS attending,
		       (e.registration_mode = 'external') AS externally_registered
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
		  -- The section cut. An Event never shown to this Customer AND not
		  -- already bought by them is news whenever it starts; anything else
		  -- earns a place only while it is inside the agenda's window, and
		  -- otherwise falls out of the Digest entirely rather than being repeated
		  -- week after week.
		  --
		  -- THE TICKET CLAUSE IS #223's SUPPRESSION, and it is here rather than in
		  -- Go because it is a rule about which set an Event belongs to. Something
		  -- this reader already bought is not news to them however new it is to
		  -- everybody else, so a held Event skips the novelty half entirely — and
		  -- with the window still applied, one bought months ahead is silent until
		  -- the week its doors open, when it arrives as the reminder the whole
		  -- feature was asked for. Listing it every week from purchase to doors
		  -- would be the repetition the ledger exists to prevent, wearing a
		  -- different hat.
		  AND (
		    (
		      NOT EXISTS (
		        SELECT 1 FROM follow_digest_sent_events l
		        WHERE l.customer_id = $1 AND l.event_id = e.id
		      )
		      AND NOT EXISTS (
		        SELECT 1 FROM ticket_sales ts
		        WHERE ts.event_id = e.id
		          AND ts.customer_id = $1
		          AND ts.status = 'active'
		      )
		    )
		    OR e.starts_at < $3
		  )
		ORDER BY e.starts_at ASC, e.id ASC
	`, customerID, now, now.Add(digestHappeningWindow))
	if err != nil {
		return DigestSections{}, err
	}
	defer rows.Close()

	// The dedupe itself: several match rows for one Event fold into one entry,
	// keeping every reason. `ordered` preserves the query's order, which is the
	// order each section lists its Events in — splitting a soonest-first list in
	// two leaves both halves soonest-first, so no second sort is needed.
	var ordered []DigestCandidate
	shown := make([]bool, 0, 8)
	index := make(map[string]int)
	for rows.Next() {
		var (
			row          DigestCandidate
			byOrg        bool
			matchedTag   sql.NullString
			alreadyShown bool
		)
		if err := rows.Scan(
			&row.EventID, &row.Name, &row.Slug, &row.StartsAt, &row.Timezone, &row.VenueName,
			&row.OrganizationName, &row.OrganizationSlug,
			&byOrg, &matchedTag, &alreadyShown, &row.Attending, &row.ExternallyRegistered,
		); err != nil {
			return DigestSections{}, err
		}
		at, seen := index[row.EventID]
		if !seen {
			ordered = append(ordered, row)
			shown = append(shown, alreadyShown)
			at = len(ordered) - 1
			index[row.EventID] = at
		}
		if byOrg {
			ordered[at].MatchedOrganization = true
		}
		if matchedTag.Valid && !containsKey(ordered[at].MatchedTagKeys, matchedTag.String) {
			ordered[at].MatchedTagKeys = append(ordered[at].MatchedTagKeys, matchedTag.String)
		}
	}
	if err := rows.Err(); err != nil {
		return DigestSections{}, err
	}

	// The partition, and it reads the same two facts the WHERE clause above cut
	// on. An Event is on the agenda when it was already shown OR when this reader
	// already holds a ticket for it (#223); the window that makes "this week"
	// true of both was applied in SQL, so anything that reached here and is not
	// news is this week's.
	//
	// Attending is checked here as well as there because the two questions are
	// different: the WHERE decides whether the Event is in the Digest at all, and
	// this decides which heading it is printed under. Dropping either leaves a
	// bought Event advertised as news.
	var out DigestSections
	for i, candidate := range ordered {
		if shown[i] || candidate.Attending {
			out.Happening = append(out.Happening, candidate)
			continue
		}
		out.New = append(out.New, candidate)
	}
	// The cap, applied to each section after the dedupe and never before it: an
	// Event matched by both an Organization Follow and two Tag Follows is one
	// Event, and counting its match rows against the cap would let three Follows
	// of one reader's spend a whole section on a single Event.
	out.New, out.NewOverflow = capSection(out.New)
	out.Happening, out.HappeningOverflow = capSection(out.Happening)
	return out, nil
}

// capSection cuts one section at the cap and hands back what did not fit, in
// order.
//
// Both halves stay soonest-first because the input is: cutting an ordered list
// in two leaves both pieces ordered, which is the same property #221 relied on
// when it split one query's rows into two sections.
func capSection(candidates []DigestCandidate) (carried, overflow []DigestCandidate) {
	if len(candidates) <= DigestSectionCap {
		return candidates, nil
	}
	return candidates[:DigestSectionCap], candidates[DigestSectionCap:]
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

// MarkDigestSkipped records that a Digest was not sent because its Customer had
// unsubscribed by the time the drain reached them (#224, ADR 0030).
//
// A FIFTH TERMINAL STATE RATHER THAN A QUIET `empty`, because the two are
// different facts and an operator reading this table has to be able to tell them
// apart. `empty` says "their Follows matched nothing", which is a reason to
// wonder whether the matching is working. `skipped` says "we deliberately did
// not write to this person", which is the feature working. Collapsing them would
// make a surge of unsubscribes look identical to the composition breaking.
//
// It is written BEFORE anything is composed — no candidate query, no Tag
// resolution, no message — which is #224's "skip rather than compose and
// discard" held at the second of the two places it can be held. The first is the
// enqueue, which refuses to create the row at all; this covers only the window
// between the two.
//
// Terminal, like `empty`: the week does not come round again, and a Customer
// turning the Digest back on must not resurrect a Digest addressed to the week
// they wanted to be quiet for.
func (r *Repository) MarkDigestSkipped(ctx context.Context, digestID string) error {
	_, err := r.db.Pool.ExecContext(ctx, `
		UPDATE follow_digests
		SET status = 'skipped', last_error = NULL, updated_at = NOW()
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
