package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
)

// Browsing the Staff platform by what it owes (#565, parent #556, ADR 0067).
//
// The staff half of the acceptance browser. It is a SEPARATE READ, a separate
// query and a separate screen from the customer half, deliberately: two
// populations with different keys, different column counts and — here — a
// fourth state the other cannot have are not worth forcing through one
// configuration object (#565).
//
// THE POPULATION IS `members ∪ platform_operators`, DEDUPLICATED BY EMAIL, and
// each arm earns its place. `members` is per-Organization, so a person with
// three Organizations holds three rows and must appear once; and a Platform
// Operator may hold NO members row at all — 024 says operator authority is
// orthogonal to Membership and needs no Organization — so without the second
// arm the one person most likely to be reading this screen would be missing
// from it. UNION and not UNION ALL: the dedup is the point.
//
// WHY THE PERSON IS AN EMAIL AND NOT AN ID. There is no table that IS a staff
// person. Migrations 067, 069 and 107 each reached that conclusion
// independently and each was right; this screen is simply the first one that
// has to LINK to a person, which is what the Staff Digest is for
// (identity.StaffDigester). The digest is computed above this layer, on the way
// out, and is written to no row here or anywhere.

// StaffAcceptanceRow is one person on the staff acceptance browser.
//
// EMAIL AND EDITION AND NOTHING ELSE. No optional consent — staff have none to
// have, and a roster column would be a segmentation tool even if they did — no
// role, no Organization, and no digest: the digest is minted from this address
// by the caller and is never a column, a scan target or a stored value.
type StaffAcceptanceRow struct {
	// Email is the person, in the response BODY only. It never reaches a URL,
	// a query string or a referer; that is the whole reason the digest exists
	// and the reason every read of this browser is driven from a POSTed body.
	Email string
	// AcceptedEditionID is which Terms edition this person's acceptance names:
	// the SATISFYING one where they hold one, otherwise their most recent, and
	// "" where they have never accepted at all.
	//
	// PREFERRING THE SATISFYING ROW MATTERS because acceptances are append-only
	// and a person accumulates one per edition (migration 107). Taking merely
	// "the latest" would be right today and wrong the moment somebody accepts a
	// future-dated edition ahead of the floor: they would be reported against a
	// row that clears the gate either way, but the standing computed from it
	// must agree with the SQL predicate that selected them, and this is what
	// makes the two agree by construction.
	AcceptedEditionID string
}

// StaffAcceptanceFilter is one page of the staff browser.
type StaffAcceptanceFilter struct {
	// Standing is the state being asked for, INCLUDING legal.StandingFormer,
	// which the customer browser has no counterpart to.
	Standing legal.Standing
	// Satisfying is the set of Terms edition ids that clear the gate (#560).
	Satisfying []string
	// CursorEmail is the last email of the previous page; "" starts at the
	// beginning. The seek is strictly greater and email is unique within the
	// deduplicated population, so nothing is skipped and nothing repeats.
	CursorEmail string
	// SearchEmail narrows to addresses containing this fragment, from a POSTed
	// body.
	SearchEmail string
	// Limit is the page size plus one, so the caller learns whether another
	// page exists without a COUNT.
	Limit int
}

// staffPopulation is everyone who signs into the Staff platform, one row per
// person. One string, used by every query below, so the four standings cannot
// disagree about who is staff.
const staffPopulation = `
	SELECT email FROM members
	UNION
	SELECT email FROM platform_operators
`

// BrowseStaffAcceptances returns one keyset page of the staff acceptance
// browser, ordered by email ascending.
//
// FOUR STANDINGS, AND *Former* IS COMPUTED RATHER THAN STORED. There is no
// leaver column, no `left_at` and no row anywhere recording that somebody used
// to be staff: a departure is a DELETE from `members` (or from
// `platform_operators`), and Former is read as "holds a Staff Terms Acceptance
// and appears in neither table". That is the only definition that cannot go
// stale, because the membership tables are the only place a departure is ever
// recorded.
//
// AND *Former* IS WHY IT IS A FILTER VALUE AND NOT A HIDDEN STATE. A leaver
// owes nothing — they cannot sign in, and the gate they would meet is one they
// will never reach — so listing them among the outstanding would fill the
// default filter with people nobody can chase. They are listed when asked for
// and absent otherwise, never silently folded into a count.
//
// `capacity = 'organizer'` IS NAMED EXPLICITLY in every predicate here and must
// stay named. Migration 107's CHECK is deliberately open for a later 'staff'
// capacity, and a capacity-agnostic test would read an acceptance made in some
// other capacity as clearance for this gate — which is precisely the mistake
// having capacities at all exists to prevent. It is the same rule
// HasSatisfyingTermsAcceptance enforces at the sign-in gate, so the browser and
// the gate cannot disagree about who is clear.
//
// Keyset and no total, for the customer browser's reasons (ADR 0067): after a
// gating publication the outstanding set is EVERYBODY, and a screen that goes
// quadratic exactly when it matters most is a screen that is not there.
func (r *Repository) BrowseStaffAcceptances(ctx context.Context, filter StaffAcceptanceFilter) ([]StaffAcceptanceRow, error) {
	source, ok := staffStandingSource(filter.Standing)
	if !ok {
		return nil, fmt.Errorf("browse staff acceptances: unsupported standing %q", filter.Standing)
	}

	// $1 satisfying, $2 cursor, $3 search, $4 limit — all bound. The only
	// interpolation is `source`, a literal chosen by the closed switch above.
	//
	// The accepted edition is read as a correlated subquery rather than a JOIN
	// because a person may hold SEVERAL acceptance rows (one per edition,
	// append-only) and a join would multiply them into duplicate people. The
	// satisfying one first, the most recent otherwise, "" where there is none.
	query := `
		SELECT p.email,
		       COALESCE(
		           (SELECT a.terms_version_id FROM staff_terms_acceptances a
		             WHERE a.email = p.email AND a.capacity = 'organizer'
		               AND a.terms_version_id = ANY($1::uuid[])
		             ORDER BY a.accepted_at DESC LIMIT 1),
		           (SELECT a.terms_version_id FROM staff_terms_acceptances a
		             WHERE a.email = p.email AND a.capacity = 'organizer'
		             ORDER BY a.accepted_at DESC LIMIT 1)
		       )
		FROM (` + source + `) AS p
		WHERE ($2 = '' OR p.email > $2)
		  AND ($3 = '' OR p.email LIKE '%' || $3 || '%')
		ORDER BY p.email ASC
		LIMIT $4
	`

	rows, err := r.db.Pool.QueryContext(ctx, query,
		filter.Satisfying, filter.CursorEmail, filter.SearchEmail, filter.Limit)
	if err != nil {
		return nil, fmt.Errorf("browse staff acceptances: %w", err)
	}
	defer rows.Close()

	page := make([]StaffAcceptanceRow, 0, filter.Limit)
	for rows.Next() {
		var (
			row      StaffAcceptanceRow
			accepted sql.NullString
		)
		if err := rows.Scan(&row.Email, &accepted); err != nil {
			return nil, fmt.Errorf("browse staff acceptances: %w", err)
		}
		row.AcceptedEditionID = accepted.String
		page = append(page, row)
	}
	return page, rows.Err()
}

// staffStandingSource is the population one standing draws from: a SELECT
// yielding an `email` column, and nothing a caller can influence.
//
// THE STANDING IS EXPRESSED AS A DIFFERENT POPULATION AND NOT AS A WHERE
// CLAUSE, which is what lets Former exist at all: the first three narrow the
// staff population by what its people have accepted, while Former draws from a
// DIFFERENT SET entirely — people who accepted and are no longer there. A
// design that had tried to express all four as predicates over one population
// would have had to invent a row for the leaver to be predicated on.
func staffStandingSource(standing legal.Standing) (string, bool) {
	switch standing {
	case legal.StandingCurrent:
		// Holds an acceptance of an edition in the satisfying set. MEMBERSHIP
		// AND NOT EQUALITY (#560): this is what makes a correction move nobody
		// out of Current, since a correction joins the set without changing it.
		return `
			SELECT email FROM (` + staffPopulation + `) AS pop
			WHERE EXISTS (
			    SELECT 1 FROM staff_terms_acceptances a
			     WHERE a.email = pop.email AND a.capacity = 'organizer'
			       AND a.terms_version_id = ANY($1::uuid[])
			)
		`, true
	case legal.StandingOutstanding:
		// Accepted SOMETHING, and none of it clears. The `EXISTS ... AND NOT
		// EXISTS` pair is what separates this from Never seen; collapsing the
		// two would bury the handful who owe a re-acceptance among everybody
		// who has never signed in since the gate shipped.
		return `
			SELECT email FROM (` + staffPopulation + `) AS pop
			WHERE EXISTS (
			    SELECT 1 FROM staff_terms_acceptances a
			     WHERE a.email = pop.email AND a.capacity = 'organizer'
			) AND NOT EXISTS (
			    SELECT 1 FROM staff_terms_acceptances a
			     WHERE a.email = pop.email AND a.capacity = 'organizer'
			       AND a.terms_version_id = ANY($1::uuid[])
			)
		`, true
	case legal.StandingNeverSeen:
		// On the Staff platform and has never accepted anything at all: the
		// ordinary state of anybody added since the Terms gate shipped and not
		// yet signed in.
		return `
			SELECT email FROM (` + staffPopulation + `) AS pop
			WHERE NOT EXISTS (
			    SELECT 1 FROM staff_terms_acceptances a
			     WHERE a.email = pop.email AND a.capacity = 'organizer'
			)
		`, true
	case legal.StandingFormer:
		// Accepted at some point and is on neither membership table now.
		//
		// READ FROM THE EVIDENCE AND SUBTRACTED FROM THE POPULATION, which is
		// the only direction available: the acceptance rows are append-only and
		// survive a departure precisely because they are evidence of a
		// contractual act (migration 107), so they are the only lasting trace
		// that somebody was ever here. DISTINCT because a person accumulates
		// one row per edition.
		return `
			SELECT DISTINCT a.email AS email
			  FROM staff_terms_acceptances a
			 WHERE a.capacity = 'organizer'
			   AND a.email NOT IN (` + staffPopulation + `)
		`, true
	}
	return "", false
}
