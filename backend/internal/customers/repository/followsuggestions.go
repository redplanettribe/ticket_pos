package repository

import (
	"context"
	"database/sql"
	"time"
)

// Suggested Follows, ranked on Co-occurrence and on Activity (#231 and #232,
// parent #229, ADR 0031).
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
//
// CO-OCCURRENCE IS A FACT ABOUT THE CATALOGUE AND NEVER ABOUT OTHER CUSTOMERS.
// Two Tags co-occur when ONE EVENT CARRIES BOTH — that is the whole of the
// relation, and the only Follow table either Co-occurrence query reads is this
// Customer's own, to learn what to start from and what to subtract. There is no
// "Customers who Follow this also Follow that" here in any disguise, and none is
// to be added (ADR 0031, CONTEXT.md). A query that joined one Customer's Follows
// to another's would be a different feature wearing this one's name.

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
//
// ReasonTagCanonicalKey is the followed Tag that produced this suggestion, and
// is NULL on everything the Activity ranking returns — there is no producing Tag
// when the subject was chosen for being busy. A canonical key and never a name:
// the Storefront words a Tag from its own message catalogues (ADR 0027), so
// nothing here may travel in a language.
type SuggestedTagRow struct {
	CanonicalKey          string
	DisplayName           string
	Curated               bool
	Activity              int
	ReasonTagCanonicalKey sql.NullString
}

// SuggestedOrganizationRow is one Organization worth offering, in
// OrganizationFollowRow's shape minus the `followed_at` a suggestion by
// definition does not have.
type SuggestedOrganizationRow struct {
	Name                  string
	Slug                  string
	LogoImageKey          sql.NullString
	Activity              int
	ReasonTagCanonicalKey sql.NullString
}

// followedTags is the Customer's own Tag Follows, and the starting point of
// every Co-occurrence query below.
//
// DIRECTLY FOLLOWED TAGS ONLY. The Tags carried by the Events of an
// Organization the Customer Follows are NOT in here: treating those as weakly
// followed is #233's work, and folding it in early would mean a Customer being
// offered a Tag inferred from their own Follow with no weighting to hold it
// below the Tags they actually chose — the panel's most obvious way of looking
// foolish (ADR 0031). Until then a Customer who Follows only Organizations has
// an empty set here and falls through to the Activity ranking, which is #231's
// and is unchanged.
const followedTags = `
	followed AS (
	    SELECT t.id, t.canonical_key
	    FROM customer_tag_follows f
	    JOIN tags t ON t.id = f.tag_id
	    WHERE f.customer_id = $1
	)`

// ListTagSuggestionsByActivity returns the Tags this Customer does not Follow
// that have discoverable upcoming Events behind them, busiest first.
//
// STILL HERE, AND STILL THE ONLY RANKING FOR A CUSTOMER WHO FOLLOWS NO TAG
// (#232). Co-occurrence has nothing to start from for that person, and the
// cold-start answer to "what should I Follow" is "whatever has the most coming
// up". It also runs BEHIND Co-occurrence for a Customer who does Follow a Tag,
// filling the slots Co-occurrence left empty — which is why it takes
// `excluding`: the Tags already offered above it, subtracted here so the LIMIT
// keeps the best of what is left rather than repeating what is already on the
// page. An empty `excluding` excludes nothing, so the cold-start path passes nil.
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
func (r *Repository) ListTagSuggestionsByActivity(ctx context.Context, customerID string, now time.Time, limit int, excluding []string) ([]SuggestedTagRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT t.canonical_key, t.display_name, t.curated, COUNT(DISTINCT e.id) AS activity
		FROM tags t
		JOIN event_tags et ON et.tag_id = t.id
		JOIN events e ON e.id = et.event_id
		WHERE `+discoverableUpcomingEvent+`$2
		  AND NOT t.canonical_key = ANY($4)
		  AND NOT EXISTS (
		    SELECT 1
		    FROM customer_tag_follows f
		    WHERE f.customer_id = $1 AND f.tag_id = t.id
		  )
		GROUP BY t.id, t.canonical_key, t.display_name, t.curated
		HAVING t.curated = TRUE OR COUNT(DISTINCT e.id) > 1
		ORDER BY activity DESC, t.canonical_key ASC
		LIMIT $3
	`, customerID, now, limit, keys(excluding))
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

// keys makes a nil exclusion list an EMPTY ARRAY rather than a NULL one, and the
// difference is the whole behaviour of the query above.
//
// `x = ANY(NULL)` is NULL, so `NOT x = ANY(NULL)` is NULL, so the row is dropped
// — a nil slice would silently return no suggestions at all rather than all of
// them. `= ANY('{}')` is false and the negation lets every row through, which is
// what "exclude nothing" has to mean.
func keys(excluding []string) []string {
	if excluding == nil {
		return []string{}
	}
	return excluding
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
//
// `excluding` is the Organizations already offered by Co-occurrence above it,
// for the reason the Tag query takes one (#232).
func (r *Repository) ListOrganizationSuggestionsByActivity(ctx context.Context, customerID string, now time.Time, limit int, excluding []string) ([]SuggestedOrganizationRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT o.name, o.slug, o.logo_image_key, COUNT(DISTINCT e.id) AS activity
		FROM organizations o
		JOIN events e ON e.organization_id = o.id
		WHERE `+discoverableUpcomingEvent+`$2
		  AND NOT o.slug = ANY($4)
		  AND NOT EXISTS (
		    SELECT 1
		    FROM customer_organization_follows f
		    WHERE f.customer_id = $1 AND f.organization_id = o.id
		  )
		GROUP BY o.id, o.name, o.slug, o.logo_image_key
		ORDER BY activity DESC, o.slug ASC
		LIMIT $3
	`, customerID, now, limit, keys(excluding))
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

// ListTagSuggestionsByCoOccurrence returns the Tags that ride alongside the ones
// this Customer Follows, best match first, each naming the followed Tag that
// produced it (#232).
//
// CO-OCCURRENCE IS ONE EVENT CARRYING BOTH TAGS. `together` is that sentence as
// SQL: the same Event joined to event_tags twice, once for a Tag the Customer
// Follows and once for a Tag they do not, counted over the discoverable upcoming
// subset. Nobody else's Follows are read, because relatedness here is a property
// of the catalogue (ADR 0031).
//
// NORMALISATION IS THE POINT OF THIS QUERY AND NOT A REFINEMENT OF IT. The score
// is the shared-Event count divided by the SQUARE ROOT of the candidate's own
// Activity, and both halves of that are load-bearing:
//
//   - Dividing at all is what stops the panel being useless. A Tag carried by
//     nearly every Event co-occurs with nearly every Tag, so on raw counts the
//     broadest Tags in the shared pool would win every comparison and the same
//     handful of generic Tags would be offered to every Customer on the
//     platform — a personalised ranking returning the identical list to everybody.
//   - Damping rather than dividing outright is what stops the opposite failure.
//     Plain division is a ratio, and a ratio is won by the smallest denominator:
//     a Tag on ONE Event that happened to co-occur once scores a perfect 1.0 and
//     takes the top of the list from a Tag with real supply behind it. The square
//     root keeps the ratio's judgement — ubiquity is still punished — while
//     letting real supply still win.
//
// Do not "simplify" either half back out. Each one alone produces a panel that
// is wrong in the opposite direction, and both failures look plausible in a
// query and absurd on the page.
//
// The score SUMS across the Customer's followed Tags rather than counting
// distinct Events, so a candidate matching two of their interests is ranked
// above one matching a single interest twice as often. The REASON is then the
// strongest single contributor — the followed Tag sharing the most Events with
// this candidate, ties broken on canonical key ascending so a reason is a fact
// about the catalogue and not about which row Postgres reached first. A key and
// never a sentence: the Storefront words it (ADR 0027).
//
// Every rule the Activity ranking keeps is kept here, because `candidate` is
// where they live: existing Follows subtracted, the Custom Tag floor of a second
// Event, and the discoverable-upcoming subset. The order is TOTAL — score
// descending, canonical key ascending.
func (r *Repository) ListTagSuggestionsByCoOccurrence(ctx context.Context, customerID string, now time.Time, limit int) ([]SuggestedTagRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		WITH `+followedTags+`,
		candidate AS (
		    SELECT t.id, t.canonical_key, t.display_name, t.curated, COUNT(DISTINCT e.id) AS activity
		    FROM tags t
		    JOIN event_tags et ON et.tag_id = t.id
		    JOIN events e ON e.id = et.event_id
		    WHERE `+discoverableUpcomingEvent+`$2
		      AND NOT EXISTS (SELECT 1 FROM followed WHERE followed.id = t.id)
		    GROUP BY t.id, t.canonical_key, t.display_name, t.curated
		    HAVING t.curated = TRUE OR COUNT(DISTINCT e.id) > 1
		),
		together AS (
		    SELECT candidate.id AS candidate_id,
		           followed.canonical_key AS reason_key,
		           COUNT(DISTINCT e.id) AS shared
		    FROM events e
		    JOIN event_tags fet ON fet.event_id = e.id
		    JOIN followed ON followed.id = fet.tag_id
		    JOIN event_tags cet ON cet.event_id = e.id
		    JOIN candidate ON candidate.id = cet.tag_id
		    WHERE `+discoverableUpcomingEvent+`$2
		    GROUP BY candidate.id, followed.canonical_key
		),
		scored AS (
		    SELECT together.candidate_id,
		           SUM(together.shared)::float8 / sqrt(candidate.activity::float8) AS score,
		           (ARRAY_AGG(together.reason_key ORDER BY together.shared DESC, together.reason_key ASC))[1] AS reason_key
		    FROM together
		    JOIN candidate ON candidate.id = together.candidate_id
		    GROUP BY together.candidate_id, candidate.activity
		)
		SELECT candidate.canonical_key, candidate.display_name, candidate.curated, candidate.activity, scored.reason_key
		FROM scored
		JOIN candidate ON candidate.id = scored.candidate_id
		ORDER BY scored.score DESC, candidate.canonical_key ASC
		LIMIT $3
	`, customerID, now, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	suggestions := []SuggestedTagRow{}
	for rows.Next() {
		var row SuggestedTagRow
		if err := rows.Scan(&row.CanonicalKey, &row.DisplayName, &row.Curated, &row.Activity, &row.ReasonTagCanonicalKey); err != nil {
			return nil, err
		}
		suggestions = append(suggestions, row)
	}
	return suggestions, rows.Err()
}

// ListOrganizationSuggestionsByCoOccurrence returns the Organizations whose
// upcoming Events carry the Tags this Customer Follows, most of their programme
// first, each naming the Tag that produced it (#232).
//
// THE SAME JOIN REACHES THE OTHER FOLLOWABLE KIND, and that is the reason
// Co-occurrence was chosen for both. Tags are worn by Events and never by
// Organizations, so an Organization is related to a Tag exactly when its
// upcoming Events carry that Tag. There is no Organization–Tag table, this
// feature adds none, and one mechanism serves both kinds rather than two
// mechanisms drifting apart (ADR 0031).
//
// ORGANIZATIONS ARE NOT NORMALISED, and the asymmetry with the Tag query above
// is deliberate rather than an oversight — do not "fix" it. Normalisation exists
// because a Tag lives in one shared pool spanning the whole catalogue, so a
// broad Tag can genuinely be attached to nearly everything and its raw counts
// say nothing about any one Customer. An Organization spans only its own
// programme: it cannot be ubiquitous, and two of its Events carrying a followed
// Tag is two real reasons to Follow it. Dividing by programme size would do the
// opposite of what it does for Tags — it would rank a one-Event Organization
// with a perfect ratio above a busy one running several matching Events, which
// is exactly the Follow that pays less in the weekly Digest.
//
// `matched` counts DISTINCT matching Events rather than summing per-Tag counts,
// so an Event carrying two of the Customer's followed Tags is one Event's worth
// of programme and not two. `pairs` keeps the per-Tag breakdown, and it is only
// there to name the reason: the followed Tag matching the most of this
// Organization's Events, ties broken on canonical key.
//
// Ordered by match count then slug, total for the reason every order here is.
func (r *Repository) ListOrganizationSuggestionsByCoOccurrence(ctx context.Context, customerID string, now time.Time, limit int) ([]SuggestedOrganizationRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		WITH `+followedTags+`,
		matching AS (
		    SELECT o.id, o.name, o.slug, o.logo_image_key, e.id AS event_id, followed.canonical_key AS reason_key
		    FROM organizations o
		    JOIN events e ON e.organization_id = o.id
		    JOIN event_tags et ON et.event_id = e.id
		    JOIN followed ON followed.id = et.tag_id
		    WHERE `+discoverableUpcomingEvent+`$2
		      AND NOT EXISTS (
		        SELECT 1
		        FROM customer_organization_follows f
		        WHERE f.customer_id = $1 AND f.organization_id = o.id
		      )
		),
		pairs AS (
		    SELECT id, reason_key, COUNT(DISTINCT event_id) AS shared
		    FROM matching
		    GROUP BY id, reason_key
		),
		matched AS (
		    SELECT id, name, slug, logo_image_key, COUNT(DISTINCT event_id) AS matches
		    FROM matching
		    GROUP BY id, name, slug, logo_image_key
		)
		SELECT matched.name, matched.slug, matched.logo_image_key, matched.matches,
		       (SELECT pairs.reason_key
		        FROM pairs
		        WHERE pairs.id = matched.id
		        ORDER BY pairs.shared DESC, pairs.reason_key ASC
		        LIMIT 1) AS reason_key
		FROM matched
		ORDER BY matched.matches DESC, matched.slug ASC
		LIMIT $3
	`, customerID, now, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	suggestions := []SuggestedOrganizationRow{}
	for rows.Next() {
		var row SuggestedOrganizationRow
		if err := rows.Scan(&row.Name, &row.Slug, &row.LogoImageKey, &row.Activity, &row.ReasonTagCanonicalKey); err != nil {
			return nil, err
		}
		suggestions = append(suggestions, row)
	}
	return suggestions, rows.Err()
}
