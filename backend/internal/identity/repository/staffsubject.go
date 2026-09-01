package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ONE STAFF PERSON'S ACCEPTANCE RECORD (#566, parent #556, ADR 0067): the
// staff half of the per-subject record, reached by Staff Digest.
//
// TWO SCREENS AND NOT ONE, which is the ruling this file exists to keep. A
// Customer and a staff person may be the same human being, and where they are
// the two records are cross-linked — but they are never merged. The populations
// have different keys (a UUID and an email), different documents (two and one),
// different tables and a fourth state only one of them can have, and a single
// "person" record spanning both would be an identity this platform does not
// have and cannot evidence.
//
// THERE IS NO DIGEST COLUMN HERE AND THERE NEVER WILL BE. The digest is minted
// on the way out by the surface that is about to put it in a link
// (identity.StaffDigester) and matched on the way in by the same surface. It is
// written to no row, no log, no file and no export — which is what makes
// bumping `legal-staff-digest.v1` to `.v2` cost some bookmarks rather than a
// data migration over append-only evidence.

// StaffAcceptanceRecordRow is one row of `staff_terms_acceptances` as the
// record shows it: the immutable evidence of one contractual act.
//
// EVERY NULLABLE COLUMN STAYS NULLABLE, the way the Customer record's do, for
// the same reason: "not collected" and "collected as blank" are different
// answers, and a zero value here would destroy the distinction several layers
// below the screen whose job is to say "not recorded" out loud.
type StaffAcceptanceRecordRow struct {
	ID string
	// TermsVersionID is the exact edition accepted, so the act resolves to the
	// exact bytes. NOT NULL: an acceptance of nothing is not an acceptance.
	TermsVersionID string
	// Capacity is what the person accepted AS (§3, ADR 0066). It is shown
	// rather than assumed: migration 107's CHECK is deliberately open for a
	// later capacity, and a record that omitted it would read an acceptance
	// made in some other capacity as clearance for this gate.
	Capacity   string
	AcceptedAt time.Time
	// The technical proof, mirroring `consent_records` column for column.
	// `session_id` names the Staff Session this acceptance MINTED — the only
	// identifier tying the row to what the person did next.
	IP        sql.NullString
	UserAgent sql.NullString
	SessionID sql.NullString
	OriginURL sql.NullString
	// PresentedLocale is the language of the acceptance label this person was
	// actually shown (#567, migration 115), pinned when the terms step was
	// issued rather than read from the request that finished it. NULL on every
	// row written before that migration, which the surface spells as "not
	// recorded" rather than guessing.
	PresentedLocale sql.NullString
}

// StaffAcceptanceRecords reads every Terms Acceptance one person holds, newest
// first.
//
// UNPAGED, AND DELIBERATELY SO. A staff person accumulates AT MOST ONE ROW PER
// EDITION per capacity — the UNIQUE index says so — and this platform has
// published a handful of editions in its lifetime and will publish a handful
// more. The whole history is a few rows, so a cursor here would be ceremony
// around a list that cannot grow, and its absence is what lets the screen state
// the complete count as a fact rather than as "at least this many". The
// Customer side pages because a Customer's history grows with every checkout,
// every toggle and every unsubscribe, without bound.
//
// EVERY CAPACITY, not just `organizer`. The gate reads `organizer` alone and
// must keep naming it explicitly, but a RECORD that filtered would be hiding
// evidence of an act the person really performed — and the day a second
// capacity exists, a filtered record would answer "what did this person accept"
// with a subset and say nothing about the omission. The capacity travels on
// each row so the reader can tell them apart.
func (r *Repository) StaffAcceptanceRecords(ctx context.Context, email string) ([]StaffAcceptanceRecordRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT id, terms_version_id, capacity, accepted_at,
		       ip, user_agent, session_id, origin_url, presented_locale
		FROM staff_terms_acceptances
		WHERE email = $1
		ORDER BY accepted_at DESC, id DESC
	`, email)
	if err != nil {
		return nil, fmt.Errorf("staff acceptance records: %w", err)
	}
	defer rows.Close()

	var records []StaffAcceptanceRecordRow
	for rows.Next() {
		var row StaffAcceptanceRecordRow
		if err := rows.Scan(
			&row.ID, &row.TermsVersionID, &row.Capacity, &row.AcceptedAt,
			&row.IP, &row.UserAgent, &row.SessionID, &row.OriginURL, &row.PresentedLocale,
		); err != nil {
			return nil, fmt.Errorf("staff acceptance records: %w", err)
		}
		records = append(records, row)
	}
	return records, rows.Err()
}

// StaffPeople is every address that has ever signed into the Staff platform or
// accepted its Terms: the membership population UNION the people who have left
// it.
//
// IT IS HOW A DIGEST IS RESOLVED TO A PERSON, and it is the only way there is.
// identity.StaffDigester is deliberately one-way — there is no Parse and no
// reverse — so a surface holding a digest matches it across this population in
// constant time (StaffDigester.Matches) until one address answers. A reverse
// function would be a function that turns a URL segment back into somebody's
// email address, which is the exact property the digest exists to deny.
//
// THE THIRD ARM IS THE LEAVERS, and it is why this is not simply
// `staffPopulation`. A Former staff person appears on the browser (#565) and
// their record must therefore be reachable from it: their acceptance rows
// survive a departure precisely because they are evidence of a contractual act,
// and a link that 404'd for exactly the people whose evidence outlives them
// would be the wrong way round.
//
// The list is small — dozens of rows, on a platform whose Organizations number
// in the dozens — so it is read whole. There is no LIMIT and no cursor: this is
// not a screen, it is a resolution step, and a partial answer would resolve a
// valid digest to nobody.
func (r *Repository) StaffPeople(ctx context.Context) ([]string, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT email FROM members
		UNION
		SELECT email FROM platform_operators
		UNION
		SELECT email FROM staff_terms_acceptances
	`)
	if err != nil {
		return nil, fmt.Errorf("staff people: %w", err)
	}
	defer rows.Close()

	var emails []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, fmt.Errorf("staff people: %w", err)
		}
		emails = append(emails, email)
	}
	return emails, rows.Err()
}

// IsStaffPerson reports whether this address signs into the Staff platform
// TODAY: the membership population, without the leavers.
//
// IT IS THE CUSTOMER RECORD'S HALF OF THE CROSS-LINK (#566). A Customer's
// record offers a link to the staff record of the same human being where there
// is one, and "is there one" is this question — asked of the CURRENT
// population, because a link is an invitation to go and read something, and the
// staff record it points at is reachable for leavers through the browser's
// Former filter without needing a Customer's page to advertise it.
func (r *Repository) IsStaffPerson(ctx context.Context, email string) (bool, error) {
	var exists bool
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM (`+staffPopulation+`) AS pop WHERE pop.email = $1)
	`, email).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("is staff person: %w", err)
	}
	return exists, nil
}
