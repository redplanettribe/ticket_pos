package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/identity/repository"
)

// The staff half of the acceptance browser (#565, parent #556, ADR 0067):
// "which of the people who sign into the Staff platform owe a Terms
// Acceptance", on a screen.
//
// IT LIVES IN IDENTITY BECAUSE THE POPULATION DOES. `members`,
// `platform_operators` and `staff_terms_acceptances` are identity's tables and
// "who is staff" is identity's rule; consent knows nothing of Organizations,
// Memberships or operators and must not learn (ADR 0015). What consent lends is
// the one thing it owns — WHICH EDITIONS CLEAR THE GATE — through the same
// TermsVersionSource the sign-in gate already reads it from, so the screen and
// the gate cannot disagree about who is clear.
//
// THE STAFF SCREEN IS ABOUT THE TERMS AND ONLY THE TERMS, and that is not an
// omission. There is exactly one staff gate: everyone who signs in accepts the
// Términos y Condiciones "en calidad de organizador" (§3, ADR 0066). Staff
// accept no Privacy Policy — the Policy gate is a Customer gate, met on
// Storefront surfaces — so a document filter offering `policy` here would offer
// a question about a gate that does not exist. The document is named in the API
// anyway, and refused for anything but `terms`, so the path stays total and a
// later staff-facing document is an added case rather than a redesign.

// StaffAcceptanceItem is one person on the staff browser: their address, and
// where they stand against the Terms.
//
// NO DIGEST FIELD, and its absence is deliberate. The Staff Digest is minted by
// the surface that is about to put it in a link (identity.StaffDigester), on
// the way out, from the address below. Carrying it here would make it a value
// this module produces, stores in a struct, and could one day log or persist —
// and the standing rule is that the digest is a URL key and a screen label,
// written to no row, no log, no file and no export.
type StaffAcceptanceItem struct {
	// Email is the person. In a response BODY only, never in a URL.
	Email string
	// Standing is `current`, `outstanding`, `never_seen` or `former`.
	Standing legal.Standing
}

// StaffAcceptanceQuery is one page of the staff browser, as the operator
// surface asks for it.
type StaffAcceptanceQuery struct {
	// Standing is the state being asked for; the surface defaults it to
	// Outstanding.
	Standing legal.Standing
	// CursorEmail is the last email of the previous page, or "".
	CursorEmail string
	// SearchEmail narrows to addresses containing this fragment, from a POSTed
	// body and never from a query string.
	SearchEmail string
	// Limit is one more than the page size, so the caller learns whether
	// another page exists without a COUNT.
	Limit int
}

// BrowseStaffAcceptances answers one page of the staff browser.
//
// THE SATISFYING SET IS READ ON EVERY CALL and never handed in, for the reason
// the customer browser reads its own: which editions clear the gate changes at
// MIDNIGHT WITH NOTHING FIRING, because the database's day moves on its own
// (legal.Edition.Arrived), so a set carried in from a surface could be a day
// stale and would put the screen out of step with the gate every person on it
// is actually meeting.
//
// A CORRECTION MOVES NOBODY INTO OUTSTANDING here either: a correction joins
// the satisfying set above the gating floor without changing it, so both the
// SQL predicate that selects the page and legal.StandingOf that labels it leave
// everybody exactly where they were.
//
// FORMER IS NOT COMPUTED FROM AN ACCEPTANCE — it is the standing of everybody
// the Former query returned, by construction, because that query draws from a
// different population (people who accepted and are on neither membership
// table). legal.StandingOf could not have produced it and is not asked to.
func (s *Service) BrowseStaffAcceptances(ctx context.Context, query StaffAcceptanceQuery) ([]StaffAcceptanceItem, error) {
	satisfying, err := s.termsVersions.SatisfyingTermsEditions(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := s.repo.BrowseStaffAcceptances(ctx, repository.StaffAcceptanceFilter{
		Standing:    query.Standing,
		Satisfying:  satisfying.IDs(),
		CursorEmail: query.CursorEmail,
		SearchEmail: query.SearchEmail,
		Limit:       query.Limit,
	})
	if err != nil {
		return nil, err
	}

	items := make([]StaffAcceptanceItem, 0, len(rows))
	for _, row := range rows {
		standing := legal.StandingFormer
		if query.Standing != legal.StandingFormer {
			// Labelled from the person's own acceptance rather than copied
			// from the filter, so that the SQL predicate and the Go rule must
			// agree — a disagreement between them shows up as a row whose
			// column contradicts the page it is on, which is a defect a test
			// can catch. Former is the one standing no acceptance can express.
			standing = legal.StandingOf(row.AcceptedEditionID, satisfying)
		}
		items = append(items, StaffAcceptanceItem{Email: row.Email, Standing: standing})
	}
	return items, nil
}
