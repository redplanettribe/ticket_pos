package service

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	consentsvc "github.com/peter/ticket_pos/backend/internal/consent/service"
	"github.com/peter/ticket_pos/backend/internal/identity"
	identitysvc "github.com/peter/ticket_pos/backend/internal/identity/service"
)

// ONE PERSON'S CONSENT RECORD (#566, parent #556, ADR 0067): the screen an
// operator answering a data subject's request actually works from, and the only
// place the Consent Withdrawal now lives.
//
// TWO SCREENS, NOT ONE, AND CROSS-LINKED SERVER-SIDE. A Customer is reached by
// UUID and a staff person by Staff Digest, because the two populations have
// different keys and different evidence. Where one human being is both, each
// record offers a link to the other — resolved HERE, inside the server, from
// the one thing the two share — so an operator never has to type an address to
// cross over and no address ever reaches a URL. What the platform must never do
// is MERGE them: there is no row anywhere that says these two are one person,
// only an address that matches, and a single "person" record would be an
// identity the platform cannot evidence.
//
// EXACTLY TWO ACTS ARE AVAILABLE FROM THIS SCREEN, and the absences are as
// ruled as the presences:
//
//   - WITHDRAW AN OPTIONAL CONSENT. The operator withdrawal surface (#271) folds
//     in here rather than living on separately, and it is keyed on the Customer
//     id like everything else on this path — the address it used to be keyed on
//     is gone from the request line entirely.
//   - GENERATE A CONSENT EVIDENCE PACK, which is #568 and is not built here.
//
// And deliberately NOT:
//
//   - NO CONTROL THAT MANUFACTURES AN ACCEPTANCE. There is no route on this
//     path that can write a grant, and the guarantee is not the absence of a
//     button: the platform's single consent-write path refuses an affirmative
//     answer for every caller (consent/service.refuseGrantOnOperatorRequest).
//   - NO PER-PERSON RE-GATE. "Publish an edition for an audience of one" is not
//     a thing this or any surface can express — a gating floor is a property of
//     a lineage, not of a person.
//   - NO ERASURE. A deletion request escalates to counsel; automating it behind
//     a button is how an irreversible act gets performed by somebody who thought
//     they were tidying up.
//   - NO WITHDRAW TERMS, AND NO WITHDRAW POLICY ACCEPTANCE. A contract's basis
//     is performance and not consent, so offering the act would be a category
//     error, and clearing a Policy Acceptance would RE-GATE the person rather
//     than free them. Only Marketing Consent and Networking Consent are ever
//     withdrawable, exactly as the existing write path already enforces.

// LegalRecords is what this surface needs from the consent module: one
// Customer's record, their history, and the labels that name an edition.
//
// NOTHING HERE CAN TOUCH A CONSENT. The one WRITE this screen offers goes
// through Consents.RecordOperatorWithdrawal, which is the customers module's
// method and therefore the platform's single consent-write path — so nothing on
// this interface can capture, record or alter a consent. The one write that IS
// here, RecordEvidencePack (#568), records that a file was handed over and
// touches no consent, no Customer and no acceptance: its whole content is a
// hash, a size and a list of ids.
type LegalRecords interface {
	// CustomerLegalRecord answers LEGAL_SUBJECT_NOT_FOUND for an id nobody
	// holds, which the handler maps to 404.
	CustomerLegalRecord(ctx context.Context, customerID string) (*consentsvc.CustomerLegalRecordItem, error)
	// CustomerConsentActs returns one keyset page of the history, newest first.
	CustomerConsentActs(ctx context.Context, query consentsvc.CustomerConsentActsQuery) ([]consentsvc.ConsentActItem, error)
	// TermsEditionLabels names every published Terms edition, so the STAFF
	// record can label an acceptance without identity learning what a label is.
	TermsEditionLabels(ctx context.Context) (map[string]string, error)
	// CustomerIDByEmail is the staff record's half of the cross-link: "" where
	// the address belongs to no Customer, which is the ordinary case.
	CustomerIDByEmail(ctx context.Context, email string) (string, error)
	// The Evidence Pack's two edition reads (#568). They resolve an edition ID
	// an act names to its exact bytes, with NO date and NO cancellation
	// predicate: a superseded, scheduled or withdrawn edition an act names is
	// still the edition somebody was shown. An id naming no row is absent from
	// the result rather than an error.
	PolicyEditionTexts(ctx context.Context, ids []string) ([]consentsvc.LegalEditionTextItem, error)
	TermsEditionTexts(ctx context.Context, ids []string) ([]consentsvc.LegalEditionTextItem, error)
	// RecordEvidencePack writes that a pack was generated — its SHA-256, its
	// size and the ids it covered (migration 118) — AND NOT THE PACK. It is the
	// only write on this interface, and it writes nothing about the subject.
	RecordEvidencePack(ctx context.Context, handover consentsvc.EvidencePackHandover) (string, error)
}

// LegalStaffRecords is the staff population's half, from identity.
type LegalStaffRecords interface {
	// StaffLegalRecord answers STAFF_SUBJECT_NOT_FOUND for an address that is
	// on neither membership table and holds no acceptance.
	StaffLegalRecord(ctx context.Context, email string) (*identitysvc.StaffLegalRecordItem, error)
	// StaffPeople is the population a digest is resolved against.
	StaffPeople(ctx context.Context) ([]string, error)
	// IsStaffPerson is the customer record's half of the cross-link.
	IsStaffPerson(ctx context.Context, email string) (bool, error)
}

// WithLegalRecords gives this service the per-subject reads (#566). Tied on
// after construction beside WithLegalAcceptanceBrowsers, and for its reason:
// the Staff Digester is derived from the deployment's link secret, which
// server.NewApp resolves.
func (s *Service) WithLegalRecords(records LegalRecords, staff LegalStaffRecords) *Service {
	s.legalRecords = records
	s.legalStaffRecords = staff
	return s
}

// CustomerLegalRecordView is one Customer's record on the wire: who they are,
// where they stand against both gates, and the link across to their staff
// record where there is one.
//
// THE HISTORY IS NOT ON IT. It arrives from its own endpoint, so page 1 and
// page 2 have the same shape and neither the landing request nor "load more"
// has a payload the other does not — see ConsentActPage.
type CustomerLegalRecordView struct {
	Customer CustomerSubjectView `json:"customer"`
	// StaffDigest is the cross-link: 32 hex characters naming this same human
	// being on the Staff platform, or null where they are not on it.
	//
	// RESOLVED SERVER-SIDE FROM THE ADDRESS, and the address never leaves the
	// server to make it happen. It is null, too, on a deployment with no link
	// secret — the digest cannot be minted, so there is no honest link to
	// offer, and refusing the WHOLE RECORD over a missing cross-link would take
	// away the evidence to protect a convenience. The staff BROWSER refuses in
	// that situation because every row it serves would be unnamed; here exactly
	// one optional field is.
	StaffDigest *string `json:"staff_digest"`
}

// CustomerSubjectView identifies the person and states what is true of them
// now. It is NOT their profile: no phone number, no Tax ID, no avatar, no
// purchases — an operator answering a consent request needs to be sure of the
// human being and to read the consent facts, and anything more would be a
// cross-Organization view of personal data justified by a compliance ticket.
type CustomerSubjectView struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	// The two gates: when each was last accepted, the exact edition accepted,
	// and the standing that edition produces.
	Policy consentsvc.CustomerGateStanding `json:"policy"`
	Terms  consentsvc.CustomerGateStanding `json:"terms"`
	// The two optional consents, NULL WHERE NEVER ANSWERED. Unanswered is a
	// different fact from denied and is published as the different fact it is:
	// an operator must never be shown a refusal somebody did not make.
	MarketingConsent  *string `json:"marketing_consent"`
	NetworkingConsent *string `json:"networking_consent"`
}

// ConsentActPage is one page of one person's history.
//
// NOT the ADR 0006 envelope, following the browsers and for ADR 0067's reason.
// There is no `page`, no `total_pages` and no `total` — but there IS a count,
// and it is a different number:
type ConsentActPage struct {
	Acts []consentsvc.ConsentActItem `json:"acts"`
	// VisibleCount is HOW MANY ACTS ARE ON THIS PAGE, and it is here so that a
	// TRUNCATED PAGE IS DISTINGUISHABLE FROM A COMPLETE HISTORY (#566). Read
	// with NextCursor it answers the only question that matters about a record
	// presented as evidence: "is this all of it?" — 25 acts and a cursor means
	// no; 25 acts and no cursor means yes.
	//
	// IT IS NOT A TOTAL and must never become one. A total over the whole
	// history is the expensive half of the query, and the screen accumulates
	// pages as the operator walks, so the running sum of these counts is the
	// number they actually need.
	VisibleCount int `json:"visible_count"`
	// NextCursor is the opaque token for the following page, or null when this
	// is the last. Its presence IS the "is there more?" signal.
	NextCursor *string `json:"next_cursor"`
}

// StaffLegalRecordView is one staff person's record on the wire.
type StaffLegalRecordView struct {
	// Digest is the key this record was reached by, echoed so the screen can
	// label the person by something other than their address if it chooses.
	Digest string `json:"digest"`
	// Email is the person, IN THE BODY ONLY. Reaching this record put no
	// address in a URL; showing one on the screen is the point of the screen.
	Email string `json:"email"`
	// Standing is `current`, `outstanding`, `never_seen` or `former`.
	Standing string `json:"standing"`
	// Acceptances is the COMPLETE history, unpaged, newest first.
	Acceptances []StaffAcceptanceRecordView `json:"acceptances"`
	// VisibleCount is how many acceptances are listed. Here it is the WHOLE
	// count, because the staff history is unpaged — and it is reported anyway,
	// under the same name as the customer side's, so a reader does not have to
	// learn two words for the same idea.
	VisibleCount int `json:"visible_count"`
	// CustomerID is the cross-link: the same human being's Customer record, or
	// null where this address belongs to no Customer. Resolved server-side from
	// the address, which never leaves the server to do it.
	CustomerID *string `json:"customer_id"`
}

// StaffAcceptanceRecordView is one Terms Acceptance on the wire.
type StaffAcceptanceRecordView struct {
	ID string `json:"id"`
	// TermsEdition names the exact edition accepted, id and label together, so
	// the act resolves to the exact bytes and reads as something a human can
	// say out loud.
	TermsEdition consentsvc.LegalEditionRef `json:"terms_edition"`
	// Capacity is what the person accepted AS (§3, ADR 0066).
	Capacity   string    `json:"capacity"`
	AcceptedAt time.Time `json:"accepted_at"`
	// The technical proof, null where the surface collected nothing.
	IP        *string `json:"ip"`
	UserAgent *string `json:"user_agent"`
	SessionID *string `json:"session_id"`
	OriginURL *string `json:"origin_url"`
	// PresentedLocale is the language of the acceptance label actually served
	// (#567), null on rows written before migration 115.
	//
	// A PLAIN POINTER AND NOT THE CUSTOMER RECORD'S **string, because the staff
	// gate is the one surface that ALWAYS presents a document: there is no
	// staff channel that shows nothing, so "this field does not apply" is not a
	// state this record can be in and there is no third case to spell.
	PresentedLocale *string `json:"presented_locale"`
}

// CustomerLegalRecord reads one Customer's record, cross-linked.
func (s *Service) CustomerLegalRecord(ctx context.Context, customerID string) (*CustomerLegalRecordView, error) {
	record, err := s.legalRecords.CustomerLegalRecord(ctx, strings.TrimSpace(customerID))
	if err != nil {
		return nil, err
	}

	view := &CustomerLegalRecordView{
		Customer: CustomerSubjectView{
			ID:                record.ID,
			Email:             record.Email,
			FirstName:         record.FirstName,
			LastName:          record.LastName,
			Policy:            record.Policy,
			Terms:             record.Terms,
			MarketingConsent:  record.MarketingConsent,
			NetworkingConsent: record.NetworkingConsent,
		},
	}

	// THE CROSS-LINK IS OFFERED ONLY WHERE THERE IS SOMETHING TO LINK TO. A
	// digest minted for an address nobody on the Staff platform holds would be
	// a link to a 404 — and worse, a link whose existence asserted that this
	// Customer is also staff, which is exactly the false identity the two-screen
	// rule exists to prevent.
	onStaff, err := s.legalStaffRecords.IsStaffPerson(ctx, record.Email)
	if err != nil {
		return nil, err
	}
	if onStaff && s.staffDigester.Configured() {
		if digest, ok := s.staffDigester.Digest(record.Email); ok {
			view.StaffDigest = pointerTo(digest)
		}
	}
	return view, nil
}

// CustomerConsentActsInput is one page request for the history.
//
// THE CURSOR TRAVELS IN A QUERY STRING HERE, unlike the browsers' — and the
// difference is not an inconsistency but the same rule applied to a different
// cursor. The browsers page by `email ASC`, so their cursor IS an address and
// base64 is an encoding rather than a disguise. This one pages by
// `(captured_at, id)`: a timestamp and a row id, neither of which names anybody.
// Nothing on this path puts an address in a request line, which is the rule —
// not "no query strings".
type CustomerConsentActsInput struct {
	CustomerID string
	// Cursor is the previous page's next_cursor. AN UNPARSEABLE CURSOR IS
	// TREATED AS ABSENT and serves the first page, following the browsers and
	// the public event feed: a bad cursor is a stale bookmark, and the useful
	// answer is the top of the list.
	Cursor string
	// Limit is what the caller asked for, "" or 0 meaning the page size. It is
	// CAPPED at ConsentRecordPageSize and never trusted: a limit a client could
	// name is one it could set to 50,000, and an evidence log downloaded in one
	// request is an export by another name — the export is #568's, and it is a
	// deliberate, named act rather than a query parameter.
	Limit int
}

// CustomerConsentActs answers one page of one person's history.
func (s *Service) CustomerConsentActs(ctx context.Context, input CustomerConsentActsInput) (*ConsentActPage, error) {
	// The record read is performed first and its refusal is the one that
	// travels: an id naming nobody is LEGAL_SUBJECT_NOT_FOUND on BOTH endpoints,
	// so a stale link is answered the same way whichever of the two the screen
	// happens to fire first.
	if _, err := s.legalRecords.CustomerLegalRecord(ctx, strings.TrimSpace(input.CustomerID)); err != nil {
		return nil, err
	}

	limit := consentsvc.ConsentRecordPageSize
	if input.Limit > 0 && input.Limit < limit {
		limit = input.Limit
	}

	acts, err := s.legalRecords.CustomerConsentActs(ctx, consentsvc.CustomerConsentActsQuery{
		CustomerID: strings.TrimSpace(input.CustomerID),
		After:      decodeConsentActCursor(input.Cursor),
		// ONE MORE THAN THE PAGE SIZE, the browsers' trick: read 26, show 25,
		// and the 26th's existence is how "is there another page?" is answered
		// without a COUNT. The extra row is never rendered and never leaves
		// this function.
		Limit: limit + 1,
	})
	if err != nil {
		return nil, err
	}

	page := &ConsentActPage{Acts: make([]consentsvc.ConsentActItem, 0, limit)}
	for i, act := range acts {
		if i == limit {
			page.NextCursor = pointerTo(encodeConsentActCursor(acts[i-1]))
			break
		}
		page.Acts = append(page.Acts, act)
	}
	page.VisibleCount = len(page.Acts)
	return page, nil
}

// StaffLegalRecord reads one staff person's record, found by Staff Digest.
//
// THE DIGEST IS RESOLVED BY MATCHING, NOT BY REVERSING. identity.StaffDigester
// is deliberately one-way — there is no Parse — so the caller walks the staff
// population and asks the digester whether each address produces this digest,
// in constant time (StaffDigester.Matches). A reverse function would be a
// function that turns a URL segment back into somebody's email address, which
// is the exact property the digest exists to deny.
//
// IT REFUSES TO SERVE WITHOUT A KEY, like the staff browser: on a deployment
// with no CONFIRMATION_LINK_SECRET every address would produce the same digest
// under a constant anybody with a copy of this source could reproduce, so the
// match would resolve an arbitrary digest to the first person in the list. That
// is not a degraded answer, it is the wrong person's record, and it is refused
// with 503 rather than served.
func (s *Service) StaffLegalRecord(ctx context.Context, digest string) (*StaffLegalRecordView, error) {
	email, err := s.resolveStaffDigest(ctx, digest)
	if err != nil {
		return nil, err
	}
	trimmed := strings.ToLower(strings.TrimSpace(digest))

	record, err := s.legalStaffRecords.StaffLegalRecord(ctx, email)
	if err != nil {
		return nil, err
	}
	labels, err := s.legalRecords.TermsEditionLabels(ctx)
	if err != nil {
		return nil, err
	}

	view := &StaffLegalRecordView{
		Digest:       trimmed,
		Email:        record.Email,
		Standing:     string(record.Standing),
		Acceptances:  make([]StaffAcceptanceRecordView, 0, len(record.Acceptances)),
		VisibleCount: len(record.Acceptances),
	}
	for _, acceptance := range record.Acceptances {
		view.Acceptances = append(view.Acceptances, StaffAcceptanceRecordView{
			ID: acceptance.ID,
			TermsEdition: consentsvc.LegalEditionRef{
				ID:    acceptance.TermsEditionID,
				Label: labels[acceptance.TermsEditionID],
			},
			Capacity:        acceptance.Capacity,
			AcceptedAt:      acceptance.AcceptedAt,
			IP:              acceptance.IP,
			UserAgent:       acceptance.UserAgent,
			SessionID:       acceptance.SessionID,
			OriginURL:       acceptance.OriginURL,
			PresentedLocale: acceptance.PresentedLocale,
		})
	}

	// The cross-link the other way. "" is the ordinary answer — most staff have
	// never bought a ticket — and it is an absent link rather than an error.
	customerID, err := s.legalRecords.CustomerIDByEmail(ctx, record.Email)
	if err != nil {
		return nil, err
	}
	if customerID != "" {
		view.CustomerID = pointerTo(customerID)
	}
	return view, nil
}

// resolveStaffDigest turns a Staff Digest back into an address, by MATCHING and
// never by reversing.
//
// EXTRACTED SO THE RECORD AND THE EVIDENCE PACK RESOLVE ONE DIGEST ONE WAY
// (#568). Two copies of this walk would be two chances for a link and the file
// it generates to name two different people.
//
// identity.StaffDigester is deliberately one-way — there is no Parse — so the
// digest is compared in constant time against every address in the staff
// population until one answers. A reverse function would be a function that
// turns a URL segment back into somebody's email address, which is the exact
// property the digest exists to deny.
//
// It refuses to serve without a key. On a deployment with no
// CONFIRMATION_LINK_SECRET every address produces the same digest under a
// constant anybody with a copy of this source could reproduce, so the match
// would resolve an arbitrary digest to the first person in the list — not a
// degraded answer but the wrong person's evidence.
func (s *Service) resolveStaffDigest(ctx context.Context, digest string) (string, error) {
	if !s.staffDigester.Configured() {
		return "", identity.ErrStaffDigestUnavailable()
	}
	trimmed := strings.ToLower(strings.TrimSpace(digest))

	people, err := s.legalStaffRecords.StaffPeople(ctx)
	if err != nil {
		return "", err
	}
	for _, candidate := range people {
		if s.staffDigester.Matches(trimmed, candidate) {
			return candidate, nil
		}
	}
	return "", identity.ErrStaffSubjectNotFound()
}

// consentActCursorSeparator divides the two halves of the keyset position. A
// space cannot occur in either half — RFC 3339 with no space, and a UUID — so
// no escaping is needed and a malformed token cannot be mistaken for a valid
// one with an odd id.
const consentActCursorSeparator = " "

// encodeConsentActCursor renders the keyset position — the last act of the page
// just served — as an opaque token.
//
// Base64 RawURL, following the browsers (service.encodeAcceptanceCursor) and
// the public event feed, so there is ONE cursor encoding in this codebase. It
// is an ENCODING AND NOT A SECRET: anybody can decode it, and unlike the
// browsers' cursor there is nothing inside worth hiding — a capture timestamp
// and a row id name nobody.
//
// BOTH HALVES, because `captured_at` is not unique: two acts stamped from one
// clock reading would either skip a row or repeat one under a timestamp-only
// cursor, and on an evidence log either is a defect somebody has to explain.
// The timestamp is RFC 3339 with nanoseconds, so it survives the round trip at
// the resolution Postgres stores.
func encodeConsentActCursor(act consentsvc.ConsentActItem) string {
	raw := act.CapturedAt.UTC().Format(time.RFC3339Nano) + consentActCursorSeparator + act.ID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeConsentActCursor reads a cursor back, or nil for the first page.
//
// AN UNPARSEABLE CURSOR IS TREATED AS ABSENT rather than as an error — the
// browsers' rule and the public event feed's — because a bad cursor means a
// stale bookmark or a truncated copy-paste, and the useful answer is the top of
// the list rather than an error page over somebody's evidence.
func decodeConsentActCursor(cursor string) *consentsvc.ConsentActCursor {
	trimmed := strings.TrimSpace(cursor)
	if trimmed == "" {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		return nil
	}
	at, id, found := strings.Cut(string(raw), consentActCursorSeparator)
	if !found || id == "" {
		return nil
	}
	capturedAt, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return nil
	}
	return &consentsvc.ConsentActCursor{CapturedAt: capturedAt, ID: id}
}

// ParseConsentActLimit reads the `limit` query parameter, which is a CAP the
// caller may lower and never one it can raise.
//
// Anything unreadable is the page size, not an error: `?limit=lots` is a
// client bug or a hand-typed URL, and the useful answer is the ordinary page.
func ParseConsentActLimit(raw string) int {
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || limit <= 0 {
		return consentsvc.ConsentRecordPageSize
	}
	return min(limit, consentsvc.ConsentRecordPageSize)
}
