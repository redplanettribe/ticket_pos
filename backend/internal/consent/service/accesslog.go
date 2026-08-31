package service

import (
	"context"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent/repository"
)

// THE CONSENT ACCESS LOG'S MODULE SEAM (#569, parent #556, ADR 0067).
//
// This module owns the table because it owns what the table is about: the
// subject of an entry is a Customer, its FK is `customers`, and it sits beside
// `consent_records` and `consent_evidence_packs`, which it is meaningless
// without. The operator surface composes it through an interface, exactly as it
// composes the per-subject record it audits (ADR 0015) — which also means the
// two cannot drift into two different ideas of what an act is.
//
// THIS LAYER DECIDES NOTHING ABOUT WHICH ACTS ARE LOGGED. That is the operator
// surface's, because that is where the acts happen; what is decided here is
// what an entry is allowed to say. Two rules are enforced on the way in and
// they are the two that would otherwise be enforced nowhere:
//
//   - A SEARCH TERM IS NEVER A STRING. The field is a boolean the whole way
//     down, so there is no signature anywhere on this path that could carry an
//     address into the log by accident.
//   - AN ACTOR IS NORMALISED, so "Ana@Example.com" and "ana@example.com" are one
//     operator on the reader's filter rather than two, and the log agrees with
//     `platform_operators`, whose rows migration 114 holds to the same fold.

// ConsentAccessAct re-exports the four acts at the seam callers use, so nothing
// above this layer imports the repository to name one.
type ConsentAccessAct = repository.ConsentAccessAct

// The four ways somebody's data is touched from the Legal Center.
//
// AND THE ACTS THAT ARE NOT HERE MATTER AS MUCH. A withdrawal writes no entry —
// the Consent Record it produces already evidences it, with the operator, the
// reference and the prior values, and two records of one act are two things that
// can disagree. A preview writes none — #562 made it a platform.Logger line, so
// that every row in this table stays a touch of somebody's data. Publishing,
// correcting, scheduling and cancelling write none — they are provenance columns
// on the version row (#563, #564), so provenance cannot drift from the edition
// it describes.
const (
	ConsentAccessListRead        = repository.ConsentAccessListRead
	ConsentAccessSubjectRead     = repository.ConsentAccessSubjectRead
	ConsentAccessEvidenceExport  = repository.ConsentAccessEvidenceExport
	ConsentAccessAuditRead       = repository.ConsentAccessAuditRead
	consentAccessLogDefaultLimit = ConsentAccessPageSize
)

// ConsentAccessPageSize is how many entries one page of the reader carries.
//
// FIFTY, matching the acceptance browsers (LegalBrowserPageSize) and the house
// default, and NOT settable by a caller — for the browsers' reason and one more:
// a page size a client could name is the export this feature refuses to have,
// spelled as a query parameter.
const ConsentAccessPageSize = 50

// ConsentAccessListQuestion is the question a `list_read` asked: what was
// browsed, how it was narrowed, and how much came back.
//
// IT CARRIES NO ROSTER AND CANNOT BE MADE TO. There is no field for the rows
// served, and adding one would turn the audit log into an unbounded second copy
// of the list it audits, growing by fifty names per "load more".
type ConsentAccessListQuestion struct {
	// Population is `customer` or `staff`; empty on an `audit_read`, which
	// browses the platform's own acts and no population.
	Population string
	// Document is `policy` or `terms`; empty on an `audit_read`.
	Document string
	// StatusFilter is the standing filter on a browser, or the act filter on
	// the audit reader. Empty means unfiltered, which the audit reader records
	// as NULL and a browser never produces — its standing always defaults.
	StatusFilter string
	// Searched is WHETHER a term narrowed the page. A boolean, deliberately and
	// permanently: an operator looking one person up must not thereby write
	// that person's address into an audit log.
	Searched bool
	// ResultCount is how many rows were served. Zero is a real answer.
	ResultCount int
}

// ConsentAccessSubject is who a `subject_read` or an `evidence_export` was
// about.
type ConsentAccessSubject struct {
	// CustomerID is the Customer this act was about, empty where the subject is
	// a staff person with no Customer record — or where the act was reached by
	// the staff route, which is keyed on a person and not on a Customer row.
	CustomerID string
	// Email is the subject BY NAME, and it is required: "somebody's record was
	// opened" with the somebody left out records nothing worth keeping.
	//
	// A PLAIN ADDRESS EVEN FOR A STAFF SUBJECT, never their Staff Digest. The
	// digest is derived from the deployment's link secret, so a rotation would
	// leave every historic row naming nobody — and this log's whole purpose is
	// to still mean something in five years.
	Email string
	// PackSHA256 is the fingerprint of the file handed over, on an
	// `evidence_export` and nothing else.
	PackSHA256 string
}

// RecordConsentListRead logs that a population was browsed.
func (s *Service) RecordConsentListRead(ctx context.Context, actor string, question ConsentAccessListQuestion) error {
	return s.repo.RecordConsentAccess(ctx, repository.ConsentAccessEntry{
		Act:          repository.ConsentAccessListRead,
		ActorEmail:   normalizeActor(actor),
		Population:   accessText(question.Population),
		Document:     accessText(question.Document),
		StatusFilter: accessText(question.StatusFilter),
		SearchTerm:   &question.Searched,
		ResultCount:  &question.ResultCount,
	})
}

// RecordConsentAuditRead logs that this log itself was read.
//
// A TOUCH OF PEOPLE'S DATA IS RECORDED HOWEVER IT IS REACHED, and the reader of
// the log is not exempt from the log. An audit surface that did not audit its
// own reader would have a hole in it shaped exactly like the person most likely
// to use it — and the row is written AFTER the page is served, so a read never
// appears in its own results and the count is the count that was actually shown.
func (s *Service) RecordConsentAuditRead(ctx context.Context, actor string, question ConsentAccessListQuestion) error {
	return s.repo.RecordConsentAccess(ctx, repository.ConsentAccessEntry{
		Act:        repository.ConsentAccessAuditRead,
		ActorEmail: normalizeActor(actor),
		// Population and Document are deliberately dropped: this read browsed
		// no population and asked about no document, and migration 116 refuses
		// a row that claims otherwise.
		StatusFilter: accessText(question.StatusFilter),
		SearchTerm:   &question.Searched,
		ResultCount:  &question.ResultCount,
	})
}

// RecordConsentSubjectRead logs that one person's record was opened.
func (s *Service) RecordConsentSubjectRead(ctx context.Context, actor string, subject ConsentAccessSubject) error {
	return s.repo.RecordConsentAccess(ctx, repository.ConsentAccessEntry{
		Act:               repository.ConsentAccessSubjectRead,
		ActorEmail:        normalizeActor(actor),
		SubjectCustomerID: accessText(subject.CustomerID),
		SubjectEmail:      accessText(subject.Email),
	})
}

// RecordConsentEvidenceExport logs that a Consent Evidence Pack was generated
// and handed over, with the fingerprint of the file.
//
// THE FINGERPRINT IS THE ONE THING THAT MAKES THIS ROW USEFUL: it resolves a
// file somebody is holding to the act that produced it, and it is the same
// value migration 118 stores on the artifact row. 118 says what was in the file;
// this says who produced it and about whom. The actor is here and not there
// because attribution belongs to the act.
func (s *Service) RecordConsentEvidenceExport(ctx context.Context, actor string, subject ConsentAccessSubject) error {
	return s.repo.RecordConsentAccess(ctx, repository.ConsentAccessEntry{
		Act:               repository.ConsentAccessEvidenceExport,
		ActorEmail:        normalizeActor(actor),
		SubjectCustomerID: accessText(subject.CustomerID),
		SubjectEmail:      accessText(subject.Email),
		PackSHA256:        accessText(subject.PackSHA256),
	})
}

// ConsentAccessEntryItem is one logged act on its way to the reader.
//
// Every optional field is a pointer, because on this screen ABSENT MEANS "this
// act has no such thing" rather than "empty": a `subject_read` has no result
// count, and a count of zero rendered in its place would be a claim the row
// does not make.
type ConsentAccessEntryItem struct {
	ID         int64
	Act        string
	ActorEmail string
	OccurredAt time.Time

	Population   *string
	Document     *string
	StatusFilter *string
	Searched     *bool
	ResultCount  *int

	SubjectCustomerID *string
	SubjectEmail      *string

	PackSHA256 *string
}

// ConsentAccessQuery is one page of the reader.
//
// ACTOR, ACT AND DATE. THERE IS NO SUBJECT FILTER and there is no field for one:
// an audit log searchable by data subject would be a second way to look people
// up, keyed on the record of people being looked up, available to the one role
// that already has the first way. The absence is enforced by this type having
// nowhere to put it.
type ConsentAccessQuery struct {
	ActorEmail string
	Act        string
	From       time.Time
	To         time.Time
	After      *ConsentAccessCursor
	// Limit is the page size PLUS ONE, so the caller learns whether another
	// page exists without a COUNT.
	Limit int
}

// ConsentAccessCursor is a keyset position in the log.
type ConsentAccessCursor = repository.ConsentAccessCursor

// ConsentAccessLog reads one page of the log, newest first.
//
// IT DOES NOT LOG ITSELF. The `audit_read` row is written by the caller AFTER
// the page is served (RecordConsentAuditRead), so the count on that row is the
// count that was really shown and a read never appears in its own results —
// which would be a log whose first page was always about itself.
func (s *Service) ConsentAccessLog(ctx context.Context, query ConsentAccessQuery) ([]ConsentAccessEntryItem, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = consentAccessLogDefaultLimit + 1
	}
	rows, err := s.repo.ConsentAccessLog(ctx, repository.ConsentAccessFilter{
		ActorEmail: normalizeActor(query.ActorEmail),
		Act:        strings.TrimSpace(query.Act),
		From:       query.From,
		To:         query.To,
		After:      query.After,
		Limit:      limit,
	})
	if err != nil {
		return nil, err
	}

	items := make([]ConsentAccessEntryItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, ConsentAccessEntryItem{
			ID:                row.ID,
			Act:               row.Act,
			ActorEmail:        row.ActorEmail,
			OccurredAt:        row.OccurredAt,
			Population:        accessTextOf(row.Population),
			Document:          accessTextOf(row.Document),
			StatusFilter:      accessTextOf(row.StatusFilter),
			Searched:          accessBoolOf(row.SearchTerm),
			ResultCount:       accessCountOf(row.ResultCount),
			SubjectCustomerID: accessTextOf(row.SubjectCustomerID),
			SubjectEmail:      accessTextOf(row.SubjectEmail),
			PackSHA256:        accessTextOf(row.PackSHA256),
		})
	}
	return items, nil
}

// normalizeActor folds an operator's address the way every stored staff address
// is folded (platform.NormalizeEmail, and migration 114's CHECK in SQL), so one
// operator is one value on the reader's filter and the log agrees with the
// allowlist it was taken from.
func normalizeActor(actor string) string {
	return strings.ToLower(strings.TrimSpace(actor))
}
