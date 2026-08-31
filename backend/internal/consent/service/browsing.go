package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/consent/repository"
)

// The customer half of the acceptance browser (#565, parent #556, ADR 0067):
// "who has not accepted the current Privacy Policy" and "who has not accepted
// the current Términos y Condiciones", on a screen instead of in psql.
//
// It is a READ AND NOTHING ELSE. Nothing on this path captures a consent,
// records an acceptance, publishes an edition or touches a Consent Record. The
// browser tells an operator who to chase; every act remains per-person, on the
// per-subject record.

// CustomerAcceptanceItem is one person as the browser shows them: who they are,
// and where they stand against BOTH documents.
//
// TWO STATUS COLUMNS ON ONE ROW, which is the shape #565 chose and the reason
// the filter and the display are separate things. A person is one human being;
// listing them once per document would make the operator reconcile two rows by
// eye and would double a screen whose whole job is to be readable.
//
// The filter picks WHICH document's standing the page is about; both standings
// are shown regardless, so the operator can see at a glance that the person
// they are chasing about the Terms is fine on the Policy.
//
// NO OPTIONAL CONSENT, and that is structural (#565): a filterable roster with
// a marketing-consent column IS a segmentation tool, whatever it is called.
// Optional consents live on the per-subject record, read one person at a time.
type CustomerAcceptanceItem struct {
	// ID is the Customer's UUID — how the per-subject record is reached (#566).
	// A Customer needs no digest: identity is UUID-keyed here, so a link out of
	// this row carries an opaque id and no address ever reaches a URL.
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	// PolicyStanding and TermsStanding are `current`, `outstanding` or
	// `never_seen`. NEVER `former`: a Customer record is never deleted (a
	// Ticket Sale is a financial record that must reconcile, migration 016), so
	// there is no departure to observe and inferring one from inactivity would
	// be a guess presented as a fact.
	PolicyStanding legal.Standing `json:"policy_standing"`
	TermsStanding  legal.Standing `json:"terms_standing"`
}

// CustomerAcceptanceQuery is one page of the customer browser, as the operator
// surface asks for it. The cursor is already decoded to an email here — the
// encoding is the surface's business, not this module's.
type CustomerAcceptanceQuery struct {
	// Document is `policy` or `terms`.
	Document string
	// Standing is the state being asked for; the surface defaults it to
	// Outstanding before it gets here.
	Standing legal.Standing
	// CursorEmail is the last email of the previous page, or "".
	CursorEmail string
	// SearchEmail narrows to addresses containing this fragment. It arrives
	// from a POSTED BODY and never from a query string, because a data
	// subject's address must not reach a URL, a log or a referer.
	SearchEmail string
	// Limit is how many rows to read — the surface asks for one more than the
	// page size so it can tell whether there is another page WITHOUT A COUNT.
	Limit int
}

// BrowseCustomerAcceptances answers one page of the customer browser.
//
// IT RESOLVES THE SATISFYING SET ITSELF, on every call, and does not take one
// as a parameter. Which editions clear the gate is the consent module's rule
// (#560) and it changes at MIDNIGHT WITH NOTHING FIRING — a scheduled edition
// becomes current because the database's own day moved — so a set handed in
// from a surface would be a set that could be stale by a day and would put the
// screen out of step with the gate every reader is actually meeting.
//
// MEMBERSHIP OF THAT SET, NEVER EQUALITY WITH THE CURRENT EDITION, both in the
// SQL predicate that selects the page and in legal.StandingOf which labels each
// row. That is what makes A CORRECTION MOVE NOBODY INTO OUTSTANDING: a
// correction is published above the gating floor, so it joins the satisfying
// set without changing it, and everybody who was Current stays Current with no
// backfill, no notification and no column to maintain.
//
// The two documents' sets are read SEPARATELY because the documents version
// independently (ADR 0066) and an edition of one must never re-gate the other.
func (s *Service) BrowseCustomerAcceptances(ctx context.Context, query CustomerAcceptanceQuery) ([]CustomerAcceptanceItem, error) {
	switch query.Document {
	case LegalDocumentPolicy, LegalDocumentTerms:
	default:
		return nil, consent.ErrLegalDocumentNotFound()
	}
	if query.Standing == legal.StandingFormer {
		return nil, consent.ErrLegalStandingNotAvailable(string(legal.StandingFormer))
	}

	policySatisfying, err := s.repo.SatisfyingPolicyEditions(ctx)
	if err != nil {
		return nil, err
	}
	termsSatisfying, err := s.repo.SatisfyingTermsEditions(ctx)
	if err != nil {
		return nil, err
	}

	filtered := policySatisfying
	if query.Document == LegalDocumentTerms {
		filtered = termsSatisfying
	}

	rows, err := s.repo.BrowseCustomerAcceptances(ctx, repository.CustomerAcceptanceFilter{
		Document:    query.Document,
		Standing:    query.Standing,
		Satisfying:  filtered.IDs(),
		CursorEmail: query.CursorEmail,
		SearchEmail: query.SearchEmail,
		Limit:       query.Limit,
	})
	if err != nil {
		return nil, err
	}

	items := make([]CustomerAcceptanceItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, CustomerAcceptanceItem{
			ID:             row.ID,
			Email:          row.Email,
			FirstName:      row.FirstName,
			LastName:       row.LastName,
			PolicyStanding: legal.StandingOf(row.PolicyEditionID, policySatisfying),
			TermsStanding:  legal.StandingOf(row.TermsEditionID, termsSatisfying),
		})
	}
	return items, nil
}
