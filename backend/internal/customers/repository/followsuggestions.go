package repository

import (
	"context"
	"database/sql"
	"time"
)

// Suggested Follows, ranked on Activity (#231, parent #229, ADR 0031).
//
// ACTIVITY IS A COUNT OF EVENTS AND NEVER OF FOLLOWERS. Neither query below
// touches a Follow table except to subtract what the Customer already has, and
// no follower count is computed here or anywhere else in the platform — ADR 0030
// declined to hold one and ADR 0031 leaves that standing. What a Follow promises
// is the weekly Follow Digest, so the question worth ranking on is whether
// Following a subject would send this Customer anything, which an upcoming-Event
// count answers and an audience size does not.
//
// Both queries run at request time against tables that already exist. There is
// no suggestions table, no materialised ranking and no cache: the catalogue is
// small enough that the join is trivial, and ADR 0031 put suggestions on their
// own endpoint precisely so that adding a cache later is a change in one place.

// discoverableUpcomingEvent is the explorer's own predicate, copied exactly.
//
// COPIED RATHER THAN SHARED, and the duplication is the lesser evil. It lives in
// catalog (ListAvailablePresetTags in catalog/repository/tag_repository.go),
// which owns Events and Tags; reaching it from here would mean either this
// module importing catalog's repository — a module boundary the customers module
// keeps by asking catalog for one id at a time through a resolver interface — or
// catalog growing a query about Follows, which it must not know exist.
//
// What the predicate must never become is "looser than the explorer". A Customer
// offered a subject whose Events they cannot find, or which is only kept alive by
// a draft nobody published or an Event its Organization deliberately unlisted,
// gets a Follow that puts nothing in a Digest. `COALESCE(ends_at, starts_at)`
// treats an Event with no declared end as ending when it starts, and is indexed
// as such by migration 057.
const discoverableUpcomingEvent = `
	e.status = 'published'
	AND e.discoverable = TRUE
	AND COALESCE(e.ends_at, e.starts_at) >= `

// SuggestedTagRow is one Tag worth offering: the same three facts TagFollowRow
// carries — so a suggested Tag and a followed Tag are one shape on the wire —
// and the Activity that earned it its place.
//
// Activity is not published. It is here because the ORDER is computed from it
// and a row has to carry what it was ordered by; putting a number on the wire
// would invite a Storefront to draw "12 events" beside a chip, which is a claim
// about a moment that stops being true the day after it is read.
type SuggestedTagRow struct {
	CanonicalKey string
	DisplayName  string
	Curated      bool
	Activity     int
}

// SuggestedOrganizationRow is one Organization worth offering, in
// OrganizationFollowRow's shape minus the `followed_at` a suggestion by
// definition does not have.
type SuggestedOrganizationRow struct {
	Name         string
	Slug         string
	LogoImageKey sql.NullString
	Activity     int
}

// ListTagSuggestionsByActivity returns the Tags this Customer does not Follow
// that have discoverable upcoming Events behind them, busiest first.
//
// Three rules are folded into the one statement, and each is here rather than in
// Go because each of them changes which rows the LIMIT keeps. Filtering in the
// service would mean fetching an unbounded pool to throw most of it away, and
// would make the cap mean "some of the qualifying Tags" rather than "the best".
//
//  1. NOT EXISTS against customer_tag_follows removes what the Customer already
//     has. The customer id is the only scope on it; nothing in the request can
//     aim this at another person's Follows.
//  2. HAVING lets a Preset Tag through on one Event and requires a Custom Tag to
//     have two. A Preset Tag comes from a curated pool that predates every Event
//     and cannot be an Event's own name; a Custom Tag on exactly one Event
//     usually IS that Event's name, typed into the Tag field by its Organization,
//     and offering a festival's name back as a category is how the panel looks
//     unserious (ADR 0031).
//  3. The INNER JOIN through event_tags is itself the "has something upcoming"
//     rule: a Tag in the pool with no qualifying Event produces no rows at all,
//     so a chip that would dead-end is never reached.
//
// The ordering is TOTAL. Activity descending is the ranking; canonical key
// ascending is the tie-break, and at this catalogue size ties are the common
// case rather than the exotic one — a dozen Preset Tags on one Event each. Without
// it two identical reads could come back in two orders, which a Following page
// re-rendering after a Follow would show as a shuffle of things the Customer did
// not touch.
func (r *Repository) ListTagSuggestionsByActivity(ctx context.Context, customerID string, now time.Time, limit int) ([]SuggestedTagRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT t.canonical_key, t.display_name, t.curated, COUNT(DISTINCT e.id) AS activity
		FROM tags t
		JOIN event_tags et ON et.tag_id = t.id
		JOIN events e ON e.id = et.event_id
		WHERE `+discoverableUpcomingEvent+`$2
		  AND NOT EXISTS (
		    SELECT 1
		    FROM customer_tag_follows f
		    WHERE f.customer_id = $1 AND f.tag_id = t.id
		  )
		GROUP BY t.id, t.canonical_key, t.display_name, t.curated
		HAVING t.curated = TRUE OR COUNT(DISTINCT e.id) > 1
		ORDER BY activity DESC, t.canonical_key ASC
		LIMIT $3
	`, customerID, now, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	suggestions := []SuggestedTagRow{}
	for rows.Next() {
		var row SuggestedTagRow
		if err := rows.Scan(&row.CanonicalKey, &row.DisplayName, &row.Curated, &row.Activity); err != nil {
			return nil, err
		}
		suggestions = append(suggestions, row)
	}
	return suggestions, rows.Err()
}

// ListOrganizationSuggestionsByActivity returns the Organizations this Customer
// does not Follow that are running discoverable upcoming Events, busiest first.
//
// The INNER JOIN to events is the whole of the "no dead ends" rule: an
// Organization with nothing upcoming produces no rows, so it is never offered.
// That is not tidiness — Following an Organization whose Events are all over
// subscribes a person to a Digest that will never mention it, and the Follow they
// made in good faith pays nothing.
//
// No Custom-Tag-style floor applies here and none should. One upcoming Event is a
// real reason to Follow an Organization, because the Organization is the thing
// that will run the next one; the floor on Custom Tags exists because a Tag on
// one Event is often not a category at all, and an Organization is never in
// doubt about being an Organization.
//
// Ordered by Activity then slug, total for the reason the Tag query's order is.
func (r *Repository) ListOrganizationSuggestionsByActivity(ctx context.Context, customerID string, now time.Time, limit int) ([]SuggestedOrganizationRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT o.name, o.slug, o.logo_image_key, COUNT(DISTINCT e.id) AS activity
		FROM organizations o
		JOIN events e ON e.organization_id = o.id
		WHERE `+discoverableUpcomingEvent+`$2
		  AND NOT EXISTS (
		    SELECT 1
		    FROM customer_organization_follows f
		    WHERE f.customer_id = $1 AND f.organization_id = o.id
		  )
		GROUP BY o.id, o.name, o.slug, o.logo_image_key
		ORDER BY activity DESC, o.slug ASC
		LIMIT $3
	`, customerID, now, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	suggestions := []SuggestedOrganizationRow{}
	for rows.Next() {
		var row SuggestedOrganizationRow
		if err := rows.Scan(&row.Name, &row.Slug, &row.LogoImageKey, &row.Activity); err != nil {
			return nil, err
		}
		suggestions = append(suggestions, row)
	}
	return suggestions, rows.Err()
}
