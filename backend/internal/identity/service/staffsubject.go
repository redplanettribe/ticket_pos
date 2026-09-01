package service

import (
	"context"
	"database/sql"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/identity"
)

// The staff half of the per-subject record (#566, parent #556, ADR 0067): what
// one person who signs into the Staff platform has accepted, and where they
// stand.
//
// IT LIVES IN IDENTITY BECAUSE THE POPULATION AND THE EVIDENCE BOTH DO, exactly
// as the staff browser does. `members`, `platform_operators` and
// `staff_terms_acceptances` are identity's tables and "who is staff" is
// identity's rule; consent knows nothing of Organizations, Memberships or
// operators and must not learn (ADR 0015). What consent lends is the one thing
// it owns — which editions clear the gate — through the same TermsVersionSource
// the sign-in gate reads it from, so THE RECORD AND THE GATE CANNOT DISAGREE
// about whether this person owes an acceptance.
//
// THE RECORD IS UNPAGED and the count is exact. A person holds at most one row
// per edition per capacity, and this platform will publish editions by the
// handful, so the whole history is a few rows. The Customer side pages because
// a Customer's history grows without bound; this one cannot.

// StaffAcceptanceRecordItem is one Terms Acceptance as the record shows it.
//
// NO DIGEST FIELD, matching StaffAcceptanceItem on the browser and for the same
// reason: the digest is minted by the surface about to put it in a link, and a
// value this module held in a struct is a value it could one day log or
// persist.
type StaffAcceptanceRecordItem struct {
	ID string
	// TermsEditionID is the exact edition accepted. It is NOT labelled here —
	// which edition a label belongs to is consent's rule, and a second labeller
	// in this module would be a second thing that could disagree with the Legal
	// Center about what an edition is called.
	TermsEditionID string
	// Capacity is what the person accepted AS. Carried rather than assumed:
	// migration 107's vocabulary is open, and a record that dropped it would
	// present an acceptance in some future capacity as clearance for this gate.
	Capacity   string
	AcceptedAt time.Time
	// The technical proof, nil where the surface collected nothing — which is
	// a different answer from "collected as blank" and stays different all the
	// way to the screen.
	IP        *string
	UserAgent *string
	SessionID *string
	OriginURL *string
	// PresentedLocale is the language of the acceptance label actually served
	// (#567). Nil on every row written before migration 115, and the surface
	// spells that "not recorded" rather than guessing.
	//
	// It has no three-state dance here, unlike the Customer record's: the staff
	// terms gate is the ONE surface that always presents a document. There is
	// no staff channel that shows nothing, so "the field does not apply" is not
	// a state this record can be in.
	PresentedLocale *string
}

// StaffLegalRecordItem is one staff person's record: who they are, where they
// stand, and everything they have accepted.
type StaffLegalRecordItem struct {
	// Email is the person, IN THE RESPONSE BODY ONLY. The record was reached by
	// digest precisely so this never had to be in the URL; putting it in the
	// payload is not a leak, it is the point — an operator has to know who they
	// are reading about.
	Email string
	// Standing is `current`, `outstanding`, `never_seen` or `former`. Computed
	// against the satisfying set and never against "the current edition", so a
	// correction moves nobody (#560).
	Standing legal.Standing
	// Acceptances is the COMPLETE history, newest first, every capacity
	// included.
	Acceptances []StaffAcceptanceRecordItem
}

// StaffLegalRecord reads one staff person's record by their (already resolved)
// address.
//
// THE ADDRESS ARRIVES FROM A DIGEST THE CALLER MATCHED, never from a request
// line. This method takes an email because at this depth the person IS an email
// — there is no table that is a staff person — and the digest is the surface's
// vocabulary, minted and matched where the link is (#565).
//
// FORMER IS READ HERE AND NOT INFERRED FROM AN ACCEPTANCE. legal.StandingOf
// knows three states; the fourth is "holds an acceptance and is on neither
// membership table", which is a question about the population and is asked of
// it. That is the only definition of a departure that cannot go stale, because
// a DELETE from `members` is the only place one is ever recorded.
//
// A person who is neither on the Staff platform nor holds a single acceptance
// is not a staff person at all, and is reported as STAFF_SUBJECT_NOT_FOUND
// rather than as an empty record: a blank record for a stranger would be a page
// asserting that somebody exists and owes nothing.
func (s *Service) StaffLegalRecord(ctx context.Context, email string) (*StaffLegalRecordItem, error) {
	satisfying, err := s.termsVersions.SatisfyingTermsEditions(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := s.repo.StaffAcceptanceRecords(ctx, email)
	if err != nil {
		return nil, err
	}
	onPlatform, err := s.repo.IsStaffPerson(ctx, email)
	if err != nil {
		return nil, err
	}
	if !onPlatform && len(rows) == 0 {
		return nil, identity.ErrStaffSubjectNotFound()
	}

	record := &StaffLegalRecordItem{
		Email:       email,
		Acceptances: make([]StaffAcceptanceRecordItem, 0, len(rows)),
	}

	// The standing is computed from the acceptance that CLEARS the gate where
	// there is one, and from the most recent otherwise — the browser's rule
	// (repository.StaffAcceptanceRow.AcceptedEditionID), restated here over the
	// rows this record already holds so the two screens cannot disagree about
	// one person. Taking merely "the latest" would be right today and wrong the
	// moment somebody accepts a future-dated edition ahead of the floor.
	//
	// `capacity = 'organizer'` IS NAMED EXPLICITLY, as it is at the sign-in gate
	// and in the browser's every predicate. An acceptance made in some other
	// capacity is real evidence and is listed below, but it is not clearance for
	// THIS gate, and a capacity-agnostic standing is precisely the mistake
	// having capacities at all exists to prevent.
	standingEdition := ""
	for _, row := range rows {
		if row.Capacity != capacityOrganizer {
			continue
		}
		if standingEdition == "" {
			standingEdition = row.TermsVersionID
		}
		if satisfying.Contains(row.TermsVersionID) {
			standingEdition = row.TermsVersionID
			break
		}
	}
	record.Standing = legal.StandingOf(standingEdition, satisfying)
	if !onPlatform {
		// Off both membership tables and holding evidence: a leaver. Former is
		// the one standing no acceptance can express, so it is written over the
		// computed one rather than derived from a row.
		record.Standing = legal.StandingFormer
	}

	for _, row := range rows {
		record.Acceptances = append(record.Acceptances, StaffAcceptanceRecordItem{
			ID:              row.ID,
			TermsEditionID:  row.TermsVersionID,
			Capacity:        row.Capacity,
			AcceptedAt:      row.AcceptedAt,
			IP:              nullableText(row.IP),
			UserAgent:       nullableText(row.UserAgent),
			SessionID:       nullableText(row.SessionID),
			OriginURL:       nullableText(row.OriginURL),
			PresentedLocale: nullableText(row.PresentedLocale),
		})
	}
	return record, nil
}

// StaffPeople is every address that has ever signed into the Staff platform or
// accepted its Terms — the population a Staff Digest is resolved against.
//
// EXPOSED BECAUSE THE DIGESTER LIVES ABOVE THIS MODULE and the digest is
// one-way by design: there is no Parse, so the only way from a digest to a
// person is to try each candidate under StaffDigester.Matches, in constant
// time. Handing the caller the population is what makes that possible without
// this module learning what a digest is.
func (s *Service) StaffPeople(ctx context.Context) ([]string, error) {
	return s.repo.StaffPeople(ctx)
}

// IsStaffPerson reports whether this address signs into the Staff platform
// today — the Customer record's half of the cross-link (#566).
func (s *Service) IsStaffPerson(ctx context.Context, email string) (bool, error) {
	return s.repo.IsStaffPerson(ctx, email)
}

// nullableText carries a SQL NULL up as a nil pointer, so "not collected" and
// "collected as blank" stay different answers to the screen that spells them.
func nullableText(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	text := value.String
	return &text
}
