package repository

import (
	"context"
	"database/sql"
	"time"
)

// Suggested Follows, ranked on Co-occurrence and on Activity (#231, #232 and
// #233, parent #229, ADR 0031).
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
// relation, and the only Follow rows any query here reads are this Customer's
// own, to learn what to start from and what to subtract. That stays true of the
// derived Tags #233 seeds from their followed Organizations: the derivation runs
// through Events and their Tags, which is the catalogue, and never through
// anybody else's Follows. Every WHERE below is scoped to $1 alone. There is no
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
// The Reason* fields are the followed Tag that produced this suggestion, and are
// all NULL together on everything the Activity ranking returns — there is no
// producing Tag when the subject was chosen for being busy.
//
// THE SAME THREE FACTS THE SUBJECT CARRIES, and for the same reason (#234). The
// producing Tag is a Tag, so it needs exactly what any Tag needs to be worded:
// the canonical key the Storefront looks a Preset Tag's copy up under (ADR
// 0027), the English display name that is the fallback for every Custom Tag, and
// `curated` to say which of the two it is holding. The key ALONE cannot be
// worded, and the missing half used to be fetched from the Follows listing —
// which works only while every producing Tag is one the Customer Follows
// DIRECTLY. #233 made a producer a DERIVED Tag, which by ADR 0031 is never in
// that listing, so the reason has to be self-sufficient.
//
// Still nothing in a Locale on the wire: `DisplayName` here is the same English
// the listing publishes, and it is the fallback rather than the copy.
type SuggestedTagRow struct {
	CanonicalKey string
	DisplayName  string
	Curated      bool
	Activity     int
	ReasonTag    SuggestedByTagRow
}

// SuggestedByTagRow is the producing Tag, nullable as a unit.
//
// One struct rather than three loose columns because the three are one fact: a
// reason either names a Tag with everything needed to word it or names nothing
// at all, and three independently nullable fields is the shape in which a row
// comes back carrying a key and no name.
type SuggestedByTagRow struct {
	CanonicalKey sql.NullString
	DisplayName  sql.NullString
	Curated      sql.NullBool
}

// SuggestedOrganizationRow is one Organization worth offering, in
// OrganizationFollowRow's shape minus the `followed_at` a suggestion by
// definition does not have.
type SuggestedOrganizationRow struct {
	Name         string
	Slug         string
	LogoImageKey sql.NullString
	Activity     int
	ReasonTag    SuggestedByTagRow
}

// derivedTagWeight is what a Tag reached through a followed Organization counts
// for beside a Tag the Customer chose, which counts for 1.
//
// A QUARTER, AND THE NUMBER IS THE SENTENCE "you Followed the Organization, not
// necessarily its genre" WRITTEN DOWN. Following an Organization is a statement
// about who is putting the Event on; the Tags on its programme are a by-product
// of that statement, often several genres wide, and one of them is frequently
// the house style rather than anything the Customer came for. At a quarter, a
// derived Tag needs FOUR shared Events to weigh what one shared Event under a
// chosen Tag weighs — which is the claim being made: derivation is evidence, and
// it takes several times as much of it to say what a Follow says once.
//
// Neither extreme was available. At 1 the inference stops being an inference and
// a Customer who Followed a venue for one band is ranked as though they had
// declared its whole programme; near 0 the derived Tags stop changing any order
// and #233 is a query that runs for nothing, leaving the Organization-only
// Customer with the cold-start panel this ticket exists to take away from them.
//
// It is a constant and not a column because it is a judgement about the domain,
// not a property of any row, and because the day it wants tuning the tuning is
// one number in one place with every test in this feature standing over it. It
// is spelled as the SQL literal it is inlined as rather than bound as a
// parameter, so that every query here reads the same weight from the same
// declaration and no call site can pass a different one — it is a compile-time
// constant and never anything from a request.
const derivedTagWeight = "0.25"

// followedTags is everything this Customer has told the platform, weighted, and
// the starting point of every Co-occurrence query below.
//
// TWO KINDS OF SEED IN ONE SET (#233). The Tags the Customer CHOSE, at full
// weight, and the DERIVED Tags carried by the upcoming Events of the
// Organizations they Follow, at derivedTagWeight. Union rather than two CTEs
// because every query downstream wants the same two things from a seed — what
// it co-occurs with, and how much that counts — and a second set would mean
// every join below written twice and drifting.
//
// MAX RATHER THAN SUM ON THE OVERLAP, which is where a union quietly goes wrong.
// A Tag can be both chosen and carried by a followed Organization's Events, and
// it can be carried by several of them; adding those up would make a Customer's
// strongest interest a function of how many of their Organizations happen to
// stamp it on a programme. A seed is a statement, not a tally, and the strongest
// statement the Customer made about a Tag is what it weighs — so a chosen Tag
// weighs 1 whatever else derives it, and a Tag derived five times weighs what a
// Tag derived once does.
//
// The derived half is confined to the discoverable upcoming subset, the same one
// every ranking here counts over. A Tag reachable only through a followed
// Organization's finished or unlisted Events is not something that Organization
// is telling anybody about now, and seeding from it would rank on a programme
// nobody can go to.
//
// THIS IS ALSO THE EXCLUSION SET, and the two uses are deliberately the same
// rows. `followed` is what the Customer already stands in front of, so every
// query below subtracts it from its candidates — which is how ADR 0031's rule
// that a derived Tag is NEVER offered back to that Customer is enforced: not as
// a filter somebody remembered to add, but as the same NOT EXISTS that keeps a
// chosen Tag out.
//
// IT CARRIES THE DISPLAY NAME AND `curated` BESIDE THE KEY (#234), because a
// seed is also what a reason names, and a key alone cannot be worded — the
// Storefront's `tags` catalogue holds copy for Preset Tags only, so a Custom
// Tag's key would render as the lowercased string somebody typed. They ride here
// rather than being joined back to `tags` in each query below for the reason
// every other column does: one declaration, and no query that can drift from it.
// The GROUP BY widens with them harmlessly — a Tag's id already determines both.
const followedTags = `
	followed AS (
	    SELECT id, canonical_key, display_name, curated, MAX(weight) AS weight
	    FROM (
	        SELECT t.id, t.canonical_key, t.display_name, t.curated, 1::float8 AS weight
	        FROM customer_tag_follows f
	        JOIN tags t ON t.id = f.tag_id
	        WHERE f.customer_id = $1
	        UNION ALL
	        SELECT t.id, t.canonical_key, t.display_name, t.curated, ` + derivedTagWeight + `::float8
	        FROM customer_organization_follows f
	        JOIN events e ON e.organization_id = f.organization_id
	        JOIN event_tags et ON et.event_id = e.id
	        JOIN tags t ON t.id = et.tag_id
	        WHERE f.customer_id = $1
	          AND ` + discoverableUpcomingEvent + `$2
	    ) seed
	    GROUP BY id, canonical_key, display_name, curated
	)`

// ListTagSuggestionsByActivity returns the Tags this Customer does not Follow
// that have discoverable upcoming Events behind them, busiest first.
//
// STILL HERE, AND STILL THE ONLY RANKING FOR A CUSTOMER WHO FOLLOWS NOTHING AT
// ALL (#232, narrowed by #233). Co-occurrence has nothing to start from for that
// person, and the cold-start answer to "what should I Follow" is "whatever has
// the most coming up". It also runs BEHIND Co-occurrence for a Customer who does
// Follow something, filling the slots Co-occurrence left empty — which is why it takes
// `excluding`: the Tags already offered above it, subtracted here so the LIMIT
// keeps the best of what is left rather than repeating what is already on the
// page. An empty `excluding` excludes nothing, so the cold-start path passes nil.
//
// Three rules are folded into the one statement, and each is here rather than in
// Go because each of them changes which rows the LIMIT keeps. Filtering in the
// service would mean fetching an unbounded pool to throw most of it away, and
// would make the cap mean "some of the qualifying Tags" rather than "the best".
//
//  1. NOT EXISTS against `followed` removes what the Customer already stands in
//     front of — the Tags they chose AND the Tags derived from the Organizations
//     they Follow (#233). THE BACKFILL IS WHERE THE "never offer a derived Tag
//     back" RULE IS EASIEST TO LOSE: Co-occurrence excludes derived Tags because
//     it excludes everything it seeds from, but this query runs behind it over
//     the whole pool and would happily offer a Customer the Tag just inferred
//     from their own Follow — with a top-of-the-panel Activity, since a Tag
//     stamped across an Organization's programme is by construction a busy one.
//     One exclusion set serves both rankings so the rule cannot hold in one and
//     not the other. The customer id is the only scope on it; nothing in the
//     request can aim this at another person's Follows.
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
		WITH `+followedTags+`
		SELECT t.canonical_key, t.display_name, t.curated, COUNT(DISTINCT e.id) AS activity
		FROM tags t
		JOIN event_tags et ON et.tag_id = t.id
		JOIN events e ON e.id = et.event_id
		WHERE `+discoverableUpcomingEvent+`$2
		  AND NOT t.canonical_key = ANY($4)
		  AND NOT EXISTS (SELECT 1 FROM followed WHERE followed.id = t.id)
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
// above one matching a single interest twice as often. Each term is WEIGHTED by
// its seed (#233): a shared Event under a chosen Tag counts for one, the same
// Event under a Tag derived from a followed Organization counts for
// derivedTagWeight. The multiplication is the whole of the weighting and it is
// inside the sum, because the two kinds of seed have to compete term by term —
// applied outside it would scale a Customer's every candidate equally and change
// no order at all.
//
// The REASON is then the strongest single contributor — the followed Tag whose
// WEIGHTED share is largest, ties broken on canonical key ascending so a reason
// is a fact about the catalogue and not about which row Postgres reached first.
// Weighted and not raw, so a candidate reached by a chosen Tag and a derived Tag
// on the same evidence names the one the Customer actually chose.
//
// `best` picks that contributor whole, with DISTINCT ON rather than the
// ARRAY_AGG the reason was a single key long enough to fit in (#234). The
// producing Tag now travels as THREE facts — key, English display name, curated
// — and three parallel ARRAY_AGGs sorted three times is the shape in which one
// of them eventually names a different Tag from the other two. DISTINCT ON keeps
// one ROW, so the three cannot disagree by construction, and the ORDER BY is the
// same one the score's tie-break uses.
//
// A DERIVED TAG NAMES ITSELF AS THE REASON, NOT THE ORGANIZATION IT CAME FROM,
// and the choice is smaller than it looks because of what the reason says. The
// Storefront words it as "Goes with X" — a claim about the CATALOGUE, that this
// candidate rides alongside X on real Events, which is exactly as true of a
// derived Tag as of a chosen one and is the relation this query actually
// computed. Naming the Organization instead would publish a relation nobody
// measured: the candidate does not go with the Organization, it goes with a Tag
// the Organization happens to programme, and the sentence would have to become
// "because you follow that Organization" — a claim about the READER, which is a
// different sentence, a second shape on the wire, and new copy in both message
// catalogues. It would also expose the inference itself, telling a Customer that
// their one Organization Follow has been read as a statement about genre; the
// Tag names something they can check against the Event they land on. The
// asymmetry it costs is real and accepted: a Customer cannot tell a chosen seed
// from a derived one in the panel, which is right, because the panel's claim is
// about the Events and not about them. A key and never a sentence either way:
// the Storefront words it (ADR 0027).
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
		           followed.display_name AS reason_name,
		           followed.curated AS reason_curated,
		           followed.weight AS weight,
		           COUNT(DISTINCT e.id) AS shared
		    FROM events e
		    JOIN event_tags fet ON fet.event_id = e.id
		    JOIN followed ON followed.id = fet.tag_id
		    JOIN event_tags cet ON cet.event_id = e.id
		    JOIN candidate ON candidate.id = cet.tag_id
		    WHERE `+discoverableUpcomingEvent+`$2
		    GROUP BY candidate.id, followed.canonical_key, followed.display_name, followed.curated, followed.weight
		),
		scored AS (
		    SELECT together.candidate_id,
		           SUM(together.shared * together.weight) / sqrt(candidate.activity::float8) AS score
		    FROM together
		    JOIN candidate ON candidate.id = together.candidate_id
		    GROUP BY together.candidate_id, candidate.activity
		),
		best AS (
		    SELECT DISTINCT ON (candidate_id)
		           candidate_id, reason_key, reason_name, reason_curated
		    FROM together
		    ORDER BY candidate_id, shared * weight DESC, reason_key ASC
		)
		SELECT candidate.canonical_key, candidate.display_name, candidate.curated, candidate.activity,
		       best.reason_key, best.reason_name, best.reason_curated
		FROM scored
		JOIN candidate ON candidate.id = scored.candidate_id
		JOIN best ON best.candidate_id = scored.candidate_id
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
		if err := rows.Scan(&row.CanonicalKey, &row.DisplayName, &row.Curated, &row.Activity,
			&row.ReasonTag.CanonicalKey, &row.ReasonTag.DisplayName, &row.ReasonTag.Curated); err != nil {
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
// Organization's Events by WEIGHTED share, ties broken on canonical key.
//
// THE WEIGHT IS TAKEN PER EVENT AND NEVER PER TAG-MATCH (#233), which is the one
// place a weighted sum here could quietly undo the distinct-Event rule above. An
// Event carrying a chosen Tag and two derived ones is still one Event's worth of
// programme, and it is worth what its STRONGEST seed says — so `weighted` maxes
// the weight within each Event first and sums across Events after. Summing the
// pairs instead would rank an Organization by how many of a Customer's seeds it
// managed to stack onto one night.
//
// Ordered by that weighted score then slug, total for the reason every order
// here is. `matches` still travels as the row's Activity: a count of Events is
// what that field has always meant, and a weighted score is not a count.
func (r *Repository) ListOrganizationSuggestionsByCoOccurrence(ctx context.Context, customerID string, now time.Time, limit int) ([]SuggestedOrganizationRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		WITH `+followedTags+`,
		matching AS (
		    SELECT o.id, o.name, o.slug, o.logo_image_key, e.id AS event_id,
		           followed.canonical_key AS reason_key,
		           followed.display_name AS reason_name,
		           followed.curated AS reason_curated,
		           followed.weight AS weight
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
		    SELECT id, reason_key, reason_name, reason_curated,
		           COUNT(DISTINCT event_id) * MAX(weight) AS shared
		    FROM matching
		    GROUP BY id, reason_key, reason_name, reason_curated
		),
		weighted AS (
		    SELECT id, event_id, MAX(weight) AS weight
		    FROM matching
		    GROUP BY id, event_id
		),
		matched AS (
		    SELECT matching.id, matching.name, matching.slug, matching.logo_image_key,
		           COUNT(DISTINCT matching.event_id) AS matches,
		           (SELECT SUM(weighted.weight) FROM weighted WHERE weighted.id = matching.id) AS score
		    FROM matching
		    GROUP BY matching.id, matching.name, matching.slug, matching.logo_image_key
		)
		SELECT matched.name, matched.slug, matched.logo_image_key, matched.matches,
		       best.reason_key, best.reason_name, best.reason_curated
		FROM matched
		-- LATERAL rather than three correlated subqueries, for the reason the Tag
		-- query's best is one row: the producing Tag travels as three facts now
		-- (#234) and they must all come from the SAME pairs row. Three subqueries
		-- each re-running the same ORDER BY is a reason that can name one Tag's
		-- key beside another's name the day the sort is touched in only two.
		LEFT JOIN LATERAL (
		    SELECT pairs.reason_key, pairs.reason_name, pairs.reason_curated
		    FROM pairs
		    WHERE pairs.id = matched.id
		    ORDER BY pairs.shared DESC, pairs.reason_key ASC
		    LIMIT 1
		) best ON TRUE
		ORDER BY matched.score DESC, matched.slug ASC
		LIMIT $3
	`, customerID, now, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	suggestions := []SuggestedOrganizationRow{}
	for rows.Next() {
		var row SuggestedOrganizationRow
		if err := rows.Scan(&row.Name, &row.Slug, &row.LogoImageKey, &row.Activity,
			&row.ReasonTag.CanonicalKey, &row.ReasonTag.DisplayName, &row.ReasonTag.Curated); err != nil {
			return nil, err
		}
		suggestions = append(suggestions, row)
	}
	return suggestions, rows.Err()
}
