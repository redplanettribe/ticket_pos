package service

import (
	"context"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	consentrepo "github.com/peter/ticket_pos/backend/internal/consent/repository"
	"github.com/peter/ticket_pos/backend/internal/consent/terms"
	"github.com/peter/ticket_pos/backend/internal/identity"
	"github.com/peter/ticket_pos/backend/internal/identity/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// pendingTermsDuration is how long a held staff sign-in is worth finishing:
// long enough to read the checkbox label and follow the link to the full text,
// short enough that this is not a second credential lying about. The customer
// side's pending consent window, for its reasons (pending_consents,
// migration 063).
const pendingTermsDuration = 15 * time.Minute

// capacityOrganizer is the capacity every acceptance on the Staff platform is
// recorded in today — org_admin, event_owner, event_staff and Platform
// Operators alike accept "en calidad de organizador" (§3, ADR 0066). The
// vocabulary is held open by the CHECK on migration 107, not by anything here.
const capacityOrganizer = "organizer"

// TermsVersionSource is what the sign-in gate needs to know about the Terms:
// which edition is current, AND the words on the box it is about to show. It is
// the narrowest possible slice of the consent module — one read, no way to
// record anything — declared HERE, on the side that calls it, the house pattern
// for a cross-module dependency (ConsentGate, OrganizationResolver). identity
// may depend on consent; consent must never depend on identity, and an
// interface this thin keeps the gate from ever asking the consent module to do
// the gating.
//
// ONE READ FOR BOTH, and that is the whole reason this is CurrentTermsEdition
// rather than a version read plus a label read (#558). The acceptance this gate
// is about to sell is pinned to the edition id it reads here, and the label it
// shows must be that same edition's words — two reads could straddle a
// publication and produce a Staff Terms Acceptance evidencing text that was not
// on screen.
//
// The satisfying set is a SECOND read and not a widening of the first (#560),
// because it answers a different question: CurrentTermsEdition says what to
// SHOW and what an acceptance will be pinned to, and the set says what already
// CLEARS. The two cannot straddle a publication harmfully — the current edition
// is always a member of its own satisfying set, so somebody shown an edition
// and pinned to it is cleared by accepting it, whichever read went first.
//
// consentrepo.Repository satisfies it as-is.
//
// TermsEditionByID is the THIRD read, and it answers about the past (#587). The
// two above are questions about now — what to show, and what clears — while a
// submission arriving fifteen minutes later carries only a token, and the one
// thing that must be asked of the edition IT pinned is whether that edition
// drew the Adulthood Declaration box. Answering that from the current edition
// would judge a person against words they were never shown, which is the exact
// failure the pinning exists to prevent.
type TermsVersionSource interface {
	CurrentTermsEdition(ctx context.Context) (consentrepo.TermsEdition, error)
	SatisfyingTermsEditions(ctx context.Context) (legal.SatisfyingSet, error)
	TermsEditionByID(ctx context.Context, id string) (consentrepo.TermsEdition, error)
}

// SignInOutcome is what a completed Proof of Email Ownership produces on the
// Staff platform, and it has exactly two shapes: a Staff Session, or a terms
// step to be finished first (#538).
//
// One type with a nil in it rather than two return paths, for the customer
// door's reason (customers/service.SignInOutcome): every caller must handle
// both, and a shape that lets one be forgotten is a shape that lets a session
// be minted past the gate. Session and SessionID are set together, or
// TermsRequired is set and both are empty; there is no outcome with neither
// and none with both.
type SignInOutcome struct {
	Session   *SessionView `json:"session"`
	SessionID string       `json:"session_id"`
	// TermsRequired is non-nil when no session was minted because this email has
	// no Terms Acceptance of the current Terms Version.
	TermsRequired *TermsRequiredView `json:"terms_required"`
}

// TermsRequiredView is the terms-required outcome on the wire: the token that
// can finish this sign-in, when it stops being worth anything, and the one box
// to show.
//
// IT IS ONLY EVER RETURNED AFTER PROOF OF EMAIL OWNERSHIP — the oracle
// discipline the customer consent gate set (ADR 0035): whether an address owes
// a Terms Acceptance is a fact about a known person, and it must not be
// visible until a passcode or a Google token has established who is asking.
//
// The Terms Version's database id is deliberately absent, matching the Policy
// Version rule: the label is how a human names the edition, and which edition
// an acceptance records is the platform's finding — pinned server-side on the
// token at the moment the box was shown (#537's held-answer rule) — never the
// client's assertion.
type TermsRequiredView struct {
	// PendingTermsToken is the single-use, short-lived credential that exchanges
	// an acceptance for the session this sign-in did not mint. A server-side row
	// (migration 107), so spending it destroys it.
	PendingTermsToken string `json:"pending_terms_token"`
	// ExpiresAt is when that token stops working, RFC 3339.
	ExpiresAt string `json:"expires_at"`
	// Version is the edition label being accepted ("1").
	Version string `json:"version"`
	// AcceptanceLabel is the mandatory, un-premarked checkbox's label, markdown,
	// verbatim from the embedded artifact (§3) in the language the login page is
	// rendered in. The UI renders it beside a link to the public Storefront terms
	// page and may not reword or pre-tick it — the Staff app hosts no copy of the
	// document.
	AcceptanceLabel string `json:"acceptance_label"`
	// AdulthoodDeclarationLabel is the SECOND mandatory, un-premarked checkbox's
	// label, markdown, verbatim from the `label-adulthood-declaration` artifact
	// in the same language the acceptance label above was served in (#587,
	// ADR 0069) — ABSENT FROM THE PAYLOAD when the edition being shown does not
	// carry that Artifact.
	//
	// THE FIELD'S PRESENCE IS THE ANSWER TO "DOES THIS EDITION ASK?", which is
	// why it is omitempty rather than an empty string: "this edition does not
	// ask" and "this edition asks with nothing written beside the box" must not
	// look the same on the wire, and the second of those is refused outright
	// before it can be served (termsGateLabels). A surface draws the box iff the
	// field arrives, so introducing the declaration is a publish and not a
	// deploy.
	//
	// It is a SEPARATE box and never a rewording of the one above. A combined
	// tick would evidence only that somebody accepted a document containing an
	// age sentence, which is the inference ADR 0069 exists to replace; and the
	// two refusals mean different things — declining the Terms is "I do not
	// agree", declining this is "I am a child", and one control cannot say both.
	AdulthoodDeclarationLabel string `json:"adulthood_declaration_label,omitempty"`
	// LabelLocale is the language the labels above were ACTUALLY served in,
	// which is the requested one in the ordinary case and the prevailing one
	// when this edition publishes no artifact in it (termsGateLabels' floor).
	// ONE value for both boxes, because both are read from one document: a card
	// wording one box in Spanish and its neighbour in English would be a person
	// shown two texts and told they were shown one.
	//
	// It is on the wire so the link beside the box can open the document the
	// words came from: a reader floored at the prevailing text needs the Spanish
	// page, and sending them to a translation this edition does not publish is a
	// link into a not-found page (#559's rule, told rather than guessed). The
	// same value is pinned server-side as the acceptance's presented locale
	// (#567); this field is the renderer's copy of it, never the source of it.
	LabelLocale string `json:"label_locale"`
}

// TermsAcceptanceSubmission is a terms step being finished: the token, the one
// answer, and the circumstances to be recorded as evidence.
type TermsAcceptanceSubmission struct {
	// Token is the pending-terms token from the terms-required outcome.
	Token string
	// TermsAcceptance is the required box. False is refused by the API and not
	// merely by a disabled button.
	TermsAcceptance bool
	// AdulthoodDeclaration is the second required box, judged EXACTLY WHERE IT
	// WAS OWED — where the edition this token pinned carries the
	// `label-adulthood-declaration` Artifact, and nowhere else (#587, ADR 0069).
	// Absent is false there too, and false where owed is refused before the
	// session is minted and before anything is written.
	AdulthoodDeclaration bool
	// Evidence is the technical proof of this act, derived by the handler from
	// the request itself and never from the body (consent.EvidenceFromRequest —
	// one spelling for every capture surface in every module).
	Evidence consent.Evidence
}

// gateOnTerms decides what a proven staff email earns: a Staff Session, or a
// terms step (#538, ADR 0066).
//
// The predicate is one thing only — no Staff Terms Acceptance, in the
// organizer capacity, of any edition that still satisfies the gate — and it has
// no special cases in it. An Org Admin of three Organizations, an Event Staff
// member scanning at a door, and a Platform Operator who is a Member of nothing
// are all gated by the same test for the same reason: every human on the Staff
// platform accepts, once per person per edition, and nobody is grandfathered.
// Inserting a later GATING terms_versions row lifts the floor above every
// stored reference, which re-gates everyone with no code and no data migration
// — while a correction leaves the floor where it is and stops nobody (#560).
//
// A read that fails FAILS THE SIGN-IN rather than waving it through: unlike
// the Staff Locale beside it, this gate is the thing the sign-in owes, and a
// database hiccup must not be the way past a contractual gate. Migration 105
// seeds edition 1, so "no current version" is unreachable in any migrated
// environment and is reported as the plain error it is.
// pageLocale is the language the login page is rendered in; the checkbox label
// is served in it. Which language the box is WORDED in is not which text binds:
// both published translations are one edition under one fingerprint, the
// Spanish prevails (§37), and the link beside the box goes to the prevailing
// text.
func (s *Service) gateOnTerms(ctx context.Context, email string, pageLocale platform.Locale, now time.Time) (*TermsRequiredView, error) {
	// The predicate first, and the current edition only for somebody who owes
	// one (#570). The two reads may go in either order — the current edition is
	// always a member of its own satisfying set, so nobody shown an edition is
	// refused their acceptance of it — and asking "who owes" first is what lets
	// the live-session gate share this exact function: the navigation check runs
	// on every page and must not read text for the ninety-nine people out of a
	// hundred who owe nothing.
	outstanding, err := s.TermsOutstanding(ctx, email)
	if err != nil {
		return nil, err
	}
	if !outstanding {
		return nil, nil
	}

	edition, err := s.termsVersions.CurrentTermsEdition(ctx)
	if err != nil {
		return nil, err
	}
	version := edition.Version

	token, err := newSessionToken()
	if err != nil {
		return nil, err
	}
	pending := repository.PendingStaffTerms{
		ID:    token,
		Email: email,
		// Pinned here, where the box is issued: the acceptance this token buys
		// evidences the edition that was shown, not a later one (#537's rule).
		TermsVersionID: version.ID,
		ExpiresAt:      now.Add(pendingTermsDuration),
		CreatedAt:      now,
	}
	labels, err := termsGateLabels(edition, pageLocale)
	if err != nil {
		return nil, err
	}
	// Pinned beside the edition, and for the same reason (#567): the acceptance
	// this token buys is written by a LATER request, which knows what was ticked
	// but not what was shown.
	pending.LabelLocale = string(labels.locale)

	if err := s.repo.CreatePendingStaffTerms(ctx, pending); err != nil {
		return nil, err
	}

	return &TermsRequiredView{
		PendingTermsToken: token,
		ExpiresAt:         pending.ExpiresAt.UTC().Format(time.RFC3339),
		Version:           version.Label,
		AcceptanceLabel:   labels.acceptance,
		// Empty when this edition does not ask, which drops the field from the
		// payload and is how a surface knows not to draw the box (#587).
		AdulthoodDeclarationLabel: labels.adulthood,
		LabelLocale:               string(labels.locale),
	}, nil
}

// AcceptTerms exchanges a pending-terms token and a ticked box for the Staff
// Session the sign-in withheld, appending exactly one Staff Terms Acceptance
// row on the way (#538).
//
// The token is spent whatever happens next — consumed before the answer is
// judged, so a submission refused for an unticked box cannot be retried
// against the same proof; the person signs in again, which costs a passcode.
// Abandoning instead leaves no session and no row: refusing the Terms costs a
// person nothing they already had, because what they had was being signed out.
//
// NOBODY IS EVER SIGNED IN WITHOUT THE EVIDENCE HAVING COMMITTED. The session
// is minted before the acceptance row is written, because the row names the
// session this act produces — but it is not RETURNED unless the insert
// succeeds, so a failure to record the evidence is a failed sign-in and the
// orphaned session row is never handed to anybody (the customer consent step's
// trade, taken identically).
//
// The edition recorded is the one PINNED TO THE TOKEN — the edition whose
// checkbox the person was actually shown — never a re-read of whatever is
// current at the write. This is the checkout's held-answer rule (#537) kept
// identically: evidence must name the text that was on screen (§34). An
// edition bump inside the token window therefore records an acceptance of the
// superseded text, and the person is re-gated at their next sign-in, exactly
// as a re-gate mid-provider-redirect works on the customer side.
func (s *Service) AcceptTerms(ctx context.Context, submission TermsAcceptanceSubmission) (*SignInOutcome, error) {
	pending, err := s.repo.ConsumePendingStaffTerms(ctx, submission.Token)
	if err != nil {
		return nil, err
	}
	now := s.now()
	// Unknown, already spent, or expired: one refusal, deliberately
	// indistinguishable, exactly as the customer side's token gets.
	if pending == nil || now.After(pending.ExpiresAt) {
		return nil, identity.ErrPendingTermsInvalid()
	}

	// The required box, refused by the API. The Staff app disables its button
	// too, and that is a courtesy; this is the guarantee.
	if !submission.TermsAcceptance {
		return nil, identity.ErrTermsAcceptanceRequired()
	}

	// And the Adulthood Declaration beside it, owed iff THE PINNED EDITION asks
	// (#587, ADR 0069). The read fails the submission rather than waving it
	// through, for gateOnTerms' reason: a database hiccup must not be the way
	// past a contractual box.
	asks, err := s.editionAsksAdulthoodDeclaration(ctx, pending.TermsVersionID)
	if err != nil {
		return nil, err
	}
	// REFUSED ABOVE EVERY WRITE, which is the feature and not an ordering
	// preference. No session has been minted and no acceptance row exists, so a
	// person who says they are not eighteen leaves this platform holding
	// NOTHING about that answer. The token is spent, as it is for every outcome
	// on this path, and starting again costs a passcode — which is what refusing
	// the Terms costs too, and is the whole of the cost.
	if asks && !submission.AdulthoodDeclaration {
		return nil, consent.ErrAdulthoodDeclarationRequired()
	}

	session, view, err := s.mintStaffSession(ctx, pending.Email, now)
	if err != nil {
		return nil, err
	}

	if err := s.repo.InsertTermsAcceptance(ctx, repository.StaffTermsAcceptance{
		Email:          pending.Email,
		TermsVersionID: pending.TermsVersionID,
		Capacity:       capacityOrganizer,
		AcceptedAt:     now,
		IP:             submission.Evidence.IP,
		UserAgent:      submission.Evidence.UserAgent,
		SessionID:      session.ID,
		OriginURL:      submission.Evidence.OriginURL,
		// The language the acceptance label was served in, from the token that
		// showed it (#567). Not re-derived from this request: it renders no text,
		// and the language it is made in says nothing about the language the box
		// was worded in minutes ago.
		PresentedLocale: pending.LabelLocale,
		// True or nil, never false (#587): the only submission that reaches this
		// line with the box owed is one that ticked it, and an edition that does
		// not ask records the null that means "this act did not ask".
		AdulthoodDeclaration: declaredAdulthood(asks),
	}); err != nil {
		return nil, err
	}

	return &SignInOutcome{Session: view, SessionID: session.ID}, nil
}

// gateLabels is everything a staff terms surface must word: the two checkbox
// labels, and the one language they were both taken from.
//
// TOGETHER, because they come from one document. The floor below can substitute
// the prevailing text for a language this edition does not publish, and a card
// that applied that floor to one box and not the other would show a person two
// texts while recording that it showed them one.
type gateLabels struct {
	// acceptance is the Terms box's words. Never empty: an edition that cannot
	// word it is refused rather than served.
	acceptance string
	// adulthood is the Adulthood Declaration box's words, EMPTY when this
	// edition does not carry the Artifact — which is the whole of "this edition
	// does not ask" (#587, ADR 0069).
	adulthood string
	// locale is the language both labels were actually taken from, which is what
	// the acceptance row records as the text presented (#567).
	locale platform.Locale
}

// termsGateLabels is the checkbox words in one language, taken from the
// edition that was just read, floored at the prevailing text.
//
// The floor is not defensive tidiness: a language this edition does not publish
// reaching here would otherwise serve a person an EMPTY label — a mandatory
// contractual box with nothing written beside it, which is the one thing §3
// forbids outright. Falling back to the text that legally binds them is the
// only safe answer.
//
// IT RETURNS THE LOCALE IT ACTUALLY USED, and that is not a convenience (#567,
// migration 115). The fallback above is exactly the case where the request's
// locale is a LIE about what was read: the login page is in English, the
// edition publishes only Spanish, and the person is shown Spanish. The
// acceptance row records which language somebody was shown, so it must record
// what this function chose and not what it was asked for — which is only
// knowable here, at the one point where the choice is made.
//
// And if even the prevailing text is missing, THE SIGN-IN FAILS. That is now
// reachable in a way it was not when the words were compiled into this binary:
// the text is data, and data can be absent. A gate that cannot state the
// contract must not sell a session past itself, so this refuses rather than
// showing an empty box — the same trade the gate makes when the read itself
// fails.
// THE ADULTHOOD DECLARATION RIDES THE SAME DOCUMENT AND THE SAME FLOOR (#587),
// with one asymmetry that is deliberate: whether the edition ASKS is decided
// from the prevailing text, while what the box SAYS is served in the reader's
// language. "Does this edition ask?" is a fact about the edition and not about
// the reader — the Spanish text is the contract (§37) and the English one is
// its courtesy translation — so answering it per-Locale would be a required box
// owed to a Spanish reader and not to an English one, which is a gate that
// varies by language.
//
// The corollary is the failure mode, and it fails LOUDLY: an edition published
// carrying the Artifact in Spanish but not in English owes the box to everybody
// and can word it for only half of them. That reader is refused the gate
// entirely rather than shown a card missing a box the API is about to require —
// the same trade the acceptance label's own floor makes, for the same reason.
// It is an incomplete publish, the Legal Draft authors both languages in
// parallel columns precisely so it cannot happen by accident, and a person
// unable to finish a sign-in is a far better outcome than a person signed in
// with a declaration nobody asked for.
func termsGateLabels(edition consentrepo.TermsEdition, locale platform.Locale) (gateLabels, error) {
	served, ok := terms.DocumentFrom(locale, edition.Artifacts)
	servedLocale := locale
	if !ok {
		if served, ok = terms.DocumentFrom(terms.PrevailingLocale, edition.Artifacts); !ok {
			return gateLabels{}, fmt.Errorf("terms edition %q publishes no acceptance label", edition.Version.Label)
		}
		servedLocale = terms.PrevailingLocale
	}

	// The edition-level question, asked of the operative text. When this edition
	// publishes no prevailing document at all — which the publish path refuses
	// and no migrated environment can produce — the document actually served
	// answers for itself, because it is the only text there is and a box written
	// right beside the reader must not be silently dropped.
	asks := served.AdulthoodDeclarationLabel != ""
	if prevailing, ok := terms.DocumentFrom(terms.PrevailingLocale, edition.Artifacts); ok {
		asks = prevailing.AdulthoodDeclarationLabel != ""
	}
	if asks && served.AdulthoodDeclarationLabel == "" {
		return gateLabels{}, fmt.Errorf(
			"terms edition %q asks the adulthood declaration but publishes no label for it in %q",
			edition.Version.Label, servedLocale)
	}

	return gateLabels{
		acceptance: served.AcceptanceLabel,
		adulthood:  served.AdulthoodDeclarationLabel,
		locale:     servedLocale,
	}, nil
}

// editionAsksAdulthoodDeclaration reports whether ONE NAMED edition — the one a
// pending token pinned — draws the Adulthood Declaration box (#587, ADR 0069).
//
// It is the accept path's half of termsGateLabels' question, and it is asked
// again rather than carried on the token because there is nowhere on the token
// to carry it: `pending_staff_terms` pins the edition and the label locale, and
// the edition is the thing that knows. Asking the edition twice cannot
// disagree with itself — an edition's artifact set never changes, a later
// publish being a new row.
//
// THE PREVAILING TEXT ANSWERS, exactly as above and for the same reason: this
// is a fact about the edition, and the request that ticks a box renders no text
// and has no language of its own to ask in.
func (s *Service) editionAsksAdulthoodDeclaration(ctx context.Context, versionID string) (bool, error) {
	edition, err := s.termsVersions.TermsEditionByID(ctx, versionID)
	if err != nil {
		return false, err
	}
	document, ok := terms.DocumentFrom(terms.PrevailingLocale, edition.Artifacts)
	return ok && document.AdulthoodDeclarationLabel != "", nil
}

// declaredAdulthood turns "this edition asked" into the answer to store: TRUE
// when it did, and NIL when it did not (#587, ADR 0069).
//
// NEVER FALSE, and there is no branch here that could produce one. An untick is
// refused above every write, so no acceptance row can exist for somebody who
// said no — the platform keeps no record of anyone who says they are a minor —
// and the null means "this act did not ask", which is a fact worth being able
// to state and the only other thing the column ever holds.
func declaredAdulthood(asks bool) *bool {
	if !asks {
		return nil
	}
	declared := true
	return &declared
}
