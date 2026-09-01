package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/consent/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// ONE PERSON'S CONSENT RECORD (#566, parent #556, ADR 0067).
//
// The browsers (#565) answer a question about a POPULATION and are structurally
// incapable of carrying an optional consent. This answers a question about ONE
// HUMAN BEING and carries everything there is, because the two are different
// acts: a roster with a marketing column is a segmentation tool however it is
// labelled, and a record read one person at a time is what a data subject's
// request is answered from.
//
// THE RECORD IS THE EVIDENCE, NOT A SUMMARY OF IT. Nothing here collapses two
// acts into a state, and nothing infers. Where the log says NULL this reports
// NULL, so the surface can spell it in words — "not recorded", "not shown" —
// rather than rendering a blank that reads as a claim something was lost.
//
// TWO READS AND NOT ONE, which is #566's shape and #547's route naming: the
// landing read (CustomerLegalRecord) and the history (CustomerConsentActs) are
// separate, so PAGE 1 AND PAGE 2 HAVE THE SAME SHAPE. Folding the first page
// into the record read would give the screen two payloads to understand for one
// list, and would make "load more" the only request that could not be replayed
// on its own.

// ConsentRecordPageSize is how many acts one page of the history carries.
//
// TWENTY-FIVE and not the house fifty, because these rows are TALL: each is a
// dozen facts about one act, not a name and a status. It is not client-settable
// for the browsers' reason — a page size a caller could name is one they could
// set to 50,000 — and the route's `limit` is a cap this bounds, never a value
// that can exceed it.
const ConsentRecordPageSize = 25

// LegalEditionRef names the exact edition an act was captured against, so it
// can be resolved to the exact bytes.
//
// THE ID AND THE LABEL TRAVEL TOGETHER and neither is derived from the other.
// The id is what the row stores and what an Evidence Pack (#568) resolves to
// artifacts; the label is what a human reads, produced by the ONE function that
// ever produces one (legal.Lineage.Label). A screen that showed only the label
// could not be traced back to a row, and one that showed only the id would be
// unreadable.
type LegalEditionRef struct {
	ID string `json:"id"`
	// Label is "2" for a gating edition or "1.1" for a correction, and "" for
	// an edition whose lineage row could not be read — which the surface spells
	// as "not recorded" rather than showing an empty string.
	Label string `json:"label"`
}

// ConsentActItem is one capture act as the record shows it.
//
// EVERY NULLABLE FACT IS A POINTER, and every one of them means something
// specific: an answer that is null is a box that was NOT SHOWN on that surface,
// which is not a No; a technical field that is null is something the surface
// did not collect, which is not a blank it collected. The surface renders both
// in words.
type ConsentActItem struct {
	ID         string    `json:"id"`
	CapturedAt time.Time `json:"captured_at"`
	// Channel is the surface. It is also what decides whether PresentedLocale
	// is a question worth asking at all — see that field.
	Channel string `json:"channel"`
	// Email is the address AS ASSERTED at the moment of capture, which may
	// differ from the Customer's address today. Reported as it was recorded: a
	// record that substituted the current address would answer a question about
	// the past with a fact about the present.
	Email string `json:"email"`
	// The three Privacy Policy answers plus the edition in force. Null answers
	// are boxes that were not on that surface.
	PolicyEdition     LegalEditionRef `json:"policy_edition"`
	PolicyAcceptance  *bool           `json:"policy_acceptance"`
	MarketingConsent  *bool           `json:"marketing_consent"`
	NetworkingConsent *bool           `json:"networking_consent"`
	// What each optional consent WAS immediately before this act (#266). It is
	// the only way to read whether the act took something away: `denied` looks
	// identical whether somebody gave something up or refused twice.
	PriorMarketingConsent  *string `json:"prior_marketing_consent"`
	PriorNetworkingConsent *string `json:"prior_networking_consent"`
	// The Terms answer and the exact edition that was shown (#536). Both null
	// on every surface that did not show the box.
	TermsAcceptance *bool            `json:"terms_acceptance"`
	TermsEdition    *LegalEditionRef `json:"terms_edition"`
	// EmailProven is whether the address was proven when the act happened —
	// what separates a granted consent from a Pending Confirmation.
	EmailProven bool `json:"email_proven"`
	// The technical proof, null where the surface collected nothing.
	IP        *string `json:"ip"`
	UserAgent *string `json:"user_agent"`
	SessionID *string `json:"session_id"`
	OriginURL *string `json:"origin_url"`
	// PresentedLocale is the language of the legal text this act was captured
	// beside (#567) — of the ARTIFACT RENDERED and never of the page it was
	// rendered on.
	//
	// THREE STATES, AND A **string IS HOW THEY ARE SPELLED ON THE WIRE. The
	// nesting is unusual and it is carrying a rule the ticket states outright:
	//
	//   - ABSENT (outer nil, `omitempty` drops the key): this channel presents
	//     NO DOCUMENT AT ALL — an Customer Area toggle, an unsubscribe, an
	//     operator-recorded withdrawal. Nothing was shown, so there is no
	//     question to answer, and a key rendered here would be a claim that
	//     text was displayed and its language forgotten.
	//   - NULL (outer non-nil, inner nil): this channel DID show a document and
	//     the platform does not know which language — every row written before
	//     migration 115, which has no backfill because the answer is not
	//     recoverable and inventing it would put a guess in an evidence log.
	//     The surface says "not recorded".
	//   - A LOCALE: what was actually rendered.
	//
	// Absent and null are two different sentences and the payload says both,
	// rather than collapsing them into one blank the reader has to interpret.
	PresentedLocale **string `json:"presented_locale,omitempty"`
	// Who recorded this act on the Customer's behalf, and which artefact it
	// answers. Both null on every act the Customer performed themselves.
	RecordedBy       *string `json:"recorded_by"`
	RequestReference *string `json:"request_reference"`
	// The two one-way stamps: that the act was later corroborated from the
	// address, and that its subject was later told about it.
	ConfirmedAt        *time.Time `json:"confirmed_at"`
	ConfirmationSentAt *time.Time `json:"confirmation_sent_at"`
}

// CustomerLegalRecordItem is who the record is about and what is true of them
// now: the landing read.
//
// NOT THE CUSTOMER'S PROFILE. An operator answering a subject's request needs
// to be sure of the human being and to read the four consent facts; a phone
// number, a Tax ID or a purchase history here would be a cross-Organization
// view of somebody's personal data justified by a compliance ticket.
type CustomerLegalRecordItem struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	// The two gates. Each carries when it was last accepted, the exact edition
	// accepted, and the STANDING that edition produces — membership of the
	// satisfying set and never equality with the current edition (#560), so a
	// correction leaves everybody exactly where they were.
	Policy CustomerGateStanding `json:"policy"`
	Terms  CustomerGateStanding `json:"terms"`
	// The two optional consents, null where never answered. Unanswered is a
	// different fact from denied and is published as the different fact it is:
	// an operator must never be shown a refusal somebody did not make.
	MarketingConsent  *string `json:"marketing_consent"`
	NetworkingConsent *string `json:"networking_consent"`
}

// CustomerGateStanding is where this person stands against one document.
type CustomerGateStanding struct {
	// AcceptedAt is null where no acceptance was ever recorded.
	AcceptedAt *time.Time `json:"accepted_at"`
	// Edition is the exact edition accepted, null where none ever was.
	Edition *LegalEditionRef `json:"edition"`
	// Standing is `current`, `outstanding` or `never_seen`. NEVER `former`: a
	// Customer record is never deleted, so there is no departure to observe.
	Standing legal.Standing `json:"standing"`
}

// ConsentActCursor is a keyset position in one person's history: the
// (captured_at, id) of the last act on the page just served.
//
// A TYPE OF THIS MODULE'S OWN, mirroring the repository's, so that a surface
// asking for page two does not have to import a repository package to name a
// position. Two columns and not one because `captured_at` is not unique — see
// repository.ConsentRecordCursor for the argument.
type ConsentActCursor struct {
	CapturedAt time.Time
	ID         string
}

// CustomerConsentActsQuery is one page of one person's history.
type CustomerConsentActsQuery struct {
	CustomerID string
	// After is the previous page's last position, already decoded. The cursor's
	// ENCODING is the surface's business, not this module's — exactly as it is
	// for the browsers.
	After *ConsentActCursor
	// Limit is how many acts to read. The caller asks for one more than the
	// page size so it can tell whether there is another page WITHOUT A COUNT.
	Limit int
}

// CustomerLegalRecord reads who the record is about and where they stand.
//
// IT RESOLVES BOTH SATISFYING SETS ITSELF, on every call, for the browsers'
// reason (#565): which editions clear a gate changes AT MIDNIGHT WITH NOTHING
// FIRING, because the database's own day moves, so a set handed in from a
// surface could be a day stale and would put this screen out of step with the
// gate the person is actually meeting.
//
// A customer id that names nobody is ErrLegalSubjectNotFound — 404 — and not an
// empty record. An operator following a stale link is entitled to be told the
// person is not there rather than shown a blank record they might then act on.
func (s *Service) CustomerLegalRecord(ctx context.Context, customerID string) (*CustomerLegalRecordItem, error) {
	row, err := s.repo.CustomerSubject(ctx, customerID)
	if err != nil {
		if errors.Is(err, repository.ErrConsentSubjectNotFound) {
			return nil, consent.ErrLegalSubjectNotFound()
		}
		return nil, err
	}

	policyEditions, err := s.repo.PolicyLineage(ctx)
	if err != nil {
		return nil, err
	}
	termsEditions, err := s.repo.TermsLineage(ctx)
	if err != nil {
		return nil, err
	}
	policySatisfying, _ := legal.Satisfying(policyEditions)
	termsSatisfying, _ := legal.Satisfying(termsEditions)

	return &CustomerLegalRecordItem{
		ID:        row.ID,
		Email:     row.Email,
		FirstName: row.FirstName,
		LastName:  row.LastName,
		Policy: CustomerGateStanding{
			AcceptedAt: optionalTime(row.PolicyAcceptedAt),
			Edition:    editionRef(row.PolicyVersionID, editionLabels(policyEditions)),
			Standing:   legal.StandingOf(row.PolicyVersionID, policySatisfying),
		},
		Terms: CustomerGateStanding{
			AcceptedAt: optionalTime(row.TermsAcceptedAt),
			Edition:    editionRef(row.TermsVersionID, editionLabels(termsEditions)),
			Standing:   legal.StandingOf(row.TermsVersionID, termsSatisfying),
		},
		MarketingConsent:  optionalWord(row.MarketingConsent),
		NetworkingConsent: optionalWord(row.NetworkingConsent),
	}, nil
}

// CustomerConsentActs reads one page of one person's history, newest first.
//
// THIS IS THE SEAM #568 GENERATES ITS EVIDENCE PACK FROM. The pack must name
// exactly what the screen names — an export telling a different story from the
// record it was exported from would be worse than no export — so it walks this
// read with the same cursor rather than growing a second query beside it.
//
// It does NOT re-check that the Customer exists. The record read above did
// that, and a person with no acts is an empty page rather than a 404: somebody
// created before the evidence log existed has a record and no history, and
// telling an operator they do not exist would be false.
func (s *Service) CustomerConsentActs(ctx context.Context, query CustomerConsentActsQuery) ([]ConsentActItem, error) {
	var after *repository.ConsentRecordCursor
	if query.After != nil {
		after = &repository.ConsentRecordCursor{CapturedAt: query.After.CapturedAt, ID: query.After.ID}
	}
	rows, err := s.repo.CustomerConsentRecords(ctx, repository.ConsentRecordFilter{
		CustomerID: query.CustomerID,
		After:      after,
		Limit:      query.Limit,
	})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []ConsentActItem{}, nil
	}

	// The two lineages are read ONCE for the page rather than once per row, and
	// only because a page has rows at all: a person with no history costs two
	// queries fewer.
	policyEditions, err := s.repo.PolicyLineage(ctx)
	if err != nil {
		return nil, err
	}
	termsEditions, err := s.repo.TermsLineage(ctx)
	if err != nil {
		return nil, err
	}
	policyLabels := editionLabels(policyEditions)
	termsLabels := editionLabels(termsEditions)

	items := make([]ConsentActItem, 0, len(rows))
	for _, row := range rows {
		item := ConsentActItem{
			ID:                     row.ID,
			CapturedAt:             row.CapturedAt,
			Channel:                row.Channel,
			Email:                  row.Email,
			PolicyAcceptance:       optionalBool(row.PolicyAcceptance),
			MarketingConsent:       optionalBool(row.MarketingConsent),
			NetworkingConsent:      optionalBool(row.NetworkingConsent),
			PriorMarketingConsent:  optionalString(row.PriorMarketingConsent),
			PriorNetworkingConsent: optionalString(row.PriorNetworkingConsent),
			TermsAcceptance:        optionalBool(row.TermsAcceptance),
			EmailProven:            row.EmailProven,
			IP:                     optionalString(row.IP),
			UserAgent:              optionalString(row.UserAgent),
			SessionID:              optionalString(row.SessionID),
			OriginURL:              optionalString(row.OriginURL),
			PresentedLocale:        presentedLocale(row.Channel, row.PresentedLocale),
			RecordedBy:             optionalString(row.RecordedBy),
			RequestReference:       optionalString(row.RequestReference),
			ConfirmedAt:            optionalTime(row.ConfirmedAt),
			ConfirmationSentAt:     optionalTime(row.ConfirmationSentAt),
		}
		if ref := editionRef(row.PolicyVersionID, policyLabels); ref != nil {
			item.PolicyEdition = *ref
		}
		item.TermsEdition = editionRef(row.TermsVersionID.String, termsLabels)
		items = append(items, item)
	}
	return items, nil
}

// CustomerIDByEmail resolves an address to a Customer id, "" where nobody holds
// it.
//
// IT EXISTS FOR THE CROSS-LINK AND FOR NOTHING ELSE (#566): where one human
// being is both a Customer and somebody who signs into the Staff platform, the
// staff record offers a link to the Customer record, and the only thing the two
// populations share is the address. The join therefore happens INSIDE THE
// SERVER, from an address the caller already resolved from a digest — it is
// never typed, never in a path, never in a query string.
//
// It is NOT a lookup surface and must not become one. There is no route that
// reaches it with a caller-supplied address; the one that used to exist is
// deleted in this same change.
func (s *Service) CustomerIDByEmail(ctx context.Context, email string) (string, error) {
	return s.repo.CustomerIDByEmail(ctx, platform.NormalizeEmail(email))
}

// TermsEditionLabels indexes every published Terms edition by id, so a caller
// outside this module can name the exact edition an acceptance holds.
//
// IT EXISTS FOR THE STAFF RECORD (#566). Staff acceptances are identity's rows
// — `staff_terms_acceptances` is identity's table and "who is staff" is
// identity's rule — but WHICH EDITION A LABEL BELONGS TO is consent's, and a
// second labeller in identity would be a second thing that could disagree with
// the Legal Center about what an edition is called. So the label crosses the
// boundary and the population does not.
func (s *Service) TermsEditionLabels(ctx context.Context) (map[string]string, error) {
	editions, err := s.repo.TermsLineage(ctx)
	if err != nil {
		return nil, err
	}
	return editionLabels(editions), nil
}

// ChannelPresentsDocument reports whether a capture channel put legal text in
// front of the person (#567, migration 115).
//
// IT IS AN ALLOWLIST OF THE CHANNELS THAT DO, not a denylist of the ones that
// do not, and that direction is the safe one: a channel added later is treated
// as showing nothing until somebody says otherwise, so the worst a new channel
// can do is omit a field. The reverse would have a new channel silently
// claiming to have displayed a notice it never rendered.
//
// The three that do: SIGN-IN and CHECKOUT render the Short Notice and the
// consent boxes beside them, and the checkout also shows the Terms box. The
// rest — the Customer Area toggles, the unsubscribe link, the digest, an
// operator-recorded withdrawal and the passcode withdrawal — show no notice,
// and `email_confirmation` confirms an EARLIER act rather than showing new
// text.
func ChannelPresentsDocument(channel string) bool {
	switch consent.Channel(channel) {
	case consent.ChannelSignIn, consent.ChannelCheckout:
		return true
	}
	return false
}

// presentedLocale spells the three-state answer described on
// ConsentActItem.PresentedLocale: absent, null, or a language.
//
// THE CHANNEL DECIDES ABSENCE AND THE COLUMN DECIDES NULL, in that order. A
// channel that showed nothing has no question to answer whatever the column
// says — and the column is checked SECOND rather than being allowed to
// resurrect the field, because a stray value on a channel that renders no text
// would be a false claim and not a fact worth surfacing.
//
// A channel that showed something and has no locale recorded is a row written
// before migration 115, and the honest answer there is "not recorded".
func presentedLocale(channel string, stored sql.NullString) **string {
	if !ChannelPresentsDocument(channel) {
		return nil
	}
	inner := optionalString(stored)
	return &inner
}

// optionalBool, optionalString and optionalTime carry a SQL NULL up as a nil
// pointer, so that "the box was not shown" and "the surface collected nothing"
// survive to the screen that spells them in words.
func optionalBool(value sql.NullBool) *bool {
	if !value.Valid {
		return nil
	}
	answer := value.Bool
	return &answer
}

func optionalString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	text := value.String
	return &text
}

func optionalTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	at := value.Time
	return &at
}

// optionalWord renders a consent state that the repository already flattened to
// a string, null where it was never answered. The empty state is the ABSENCE of
// an answer rather than a fourth value, so it becomes JSON null and not an
// empty string a client might render as a word.
func optionalWord(state string) *string {
	if state == "" {
		return nil
	}
	return &state
}

// editionLabels indexes a lineage by edition id, so a page of acts resolves
// every edition it names with no query per row.
//
// The LABEL is taken from legal.Lineage.Label — the only place a label is ever
// produced — so the record cannot name an edition differently from the Legal
// Center that published it.
func editionLabels(editions []legal.Edition) map[string]string {
	labels := make(map[string]string, len(editions))
	for _, edition := range editions {
		labels[edition.ID] = edition.Lineage.Label()
	}
	return labels
}

// editionRef names one edition, or nil where the row named none.
//
// AN UNKNOWN ID KEEPS ITS ID AND LOSES ONLY ITS LABEL. A version row that the
// lineage read did not return should be impossible — the FK is RESTRICT — but
// if it ever happened, dropping the reference entirely would erase the one
// thing that ties the act to bytes, so the id survives and the label is empty
// for the surface to spell as "not recorded".
func editionRef(id string, labels map[string]string) *LegalEditionRef {
	if id == "" {
		return nil
	}
	return &LegalEditionRef{ID: id, Label: labels[id]}
}
