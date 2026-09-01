package consent

import (
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Channel names the surface a capture act happened on.
//
// The vocabulary is defined here in full although #251 writes only
// ChannelSignIn: the checkout, the Customer Area toggle, the unsubscribe link
// and the confirmation link each land in their own ticket and each needs a name
// nobody has to invent under deadline. The database's CHECK constraint carries
// the same strings — migration 061's five, widened to six by 067 — and the
// vocabulary living in two places is the price of a CHECK over an ENUM: adding
// a seventh means changing both, and changing one alone is either a capture the
// service refuses or a row the database refuses.
type Channel string

const (
	// ChannelSignIn is the consent step reached past Proof of Email Ownership —
	// both doors, passcode and Google alike.
	//
	// It names the surface and not its outcome, which is why the Consent
	// Withdrawal made on Proof of Email Ownership alone is recorded here too
	// (#270): a passcode redeemed for a pending-consent token, spent at the
	// consent submission endpoint, is this surface — it simply mints no Customer
	// Session at the end of it. The two are still told apart from a single row
	// without a seventh channel string: a sign-in consent step always records a
	// Policy Acceptance and the session it minted, and a withdrawal records
	// neither, so `policy_acceptance IS NULL AND session_id IS NULL` on this
	// channel is that surface exactly.
	ChannelSignIn Channel = "signin"
	// ChannelCheckout is the online checkout, where a guest may be answering for
	// an address they have not proven (ADR 0035).
	ChannelCheckout Channel = "checkout"
	// ChannelAccountSettings is a signed-in Customer changing their own answer
	// from the Customer Area.
	ChannelAccountSettings Channel = "account_settings"
	// ChannelUnsubscribeLink is the signed link at the foot of a Follow Digest,
	// which authenticates nobody and must keep working months later (ADR 0030).
	ChannelUnsubscribeLink Channel = "unsubscribe_link"
	// ChannelEmailConfirmation is the confirmation link in a Sale Confirmation
	// that resolves a Pending Confirmation — clicking it from the inbox being
	// itself the proof of ownership (ADR 0035).
	ChannelEmailConfirmation Channel = "email_confirmation"
	// ChannelOperatorRequest is a Consent Withdrawal that arrived off-platform —
	// by email or on paper — and was recorded by a Platform Operator on the
	// Customer's behalf (#266, parent #265).
	//
	// IT IS THE ONE CHANNEL ON WHICH THE ACTOR IS NOT THE CUSTOMER, which is why
	// it is a channel of its own rather than a flag beside another: a compliance
	// report grouping this column must never present a staff action as somebody's
	// own click, and that is only structural if the surface itself is named. The
	// record it writes also carries who recorded it and which artefact it
	// answers, which no other channel has.
	//
	// It can only ever withdraw. An Operator cannot manufacture consent, so the
	// proven-ness question that decides granted-or-pending everywhere else never
	// arises here.
	ChannelOperatorRequest Channel = "operator_request"
	// ChannelPasscodeWithdrawal is the surface where a Consent Withdrawal is made
	// on Proof of Email Ownership alone — a passcode redeemed for a
	// pending-consent token and spent on a denials-only submission, minting no
	// Customer Session and demanding no Policy Acceptance (ADR 0039, #270).
	//
	// It is a channel of its own rather than `signin` because the compliance
	// question the column exists to answer is WHICH SURFACE, and this is a
	// different surface from the sign-in consent step even though it comes
	// through the same door. Telling the two apart by the absence of a Policy
	// Acceptance and a session id worked, but it put a surface's identity into a
	// predicate over other columns' nulls — see migration 068.
	ChannelPasscodeWithdrawal Channel = "passcode_withdrawal"
)

// Valid reports whether the channel is one the storage vocabulary recognises. A
// capture with an unknown channel is refused by the service rather than left to
// the database's CHECK, so the failure names the bug rather than the row.
func (c Channel) Valid() bool {
	switch c {
	case ChannelSignIn, ChannelCheckout, ChannelAccountSettings, ChannelUnsubscribeLink,
		ChannelEmailConfirmation, ChannelOperatorRequest, ChannelPasscodeWithdrawal:
		return true
	}
	return false
}

// State is one optional consent's current state on a Customer.
//
// The zero value — the empty string — is UNANSWERED, and is what the nullable
// column reads back as. It is deliberately not a named constant: "unanswered"
// is the absence of a state rather than a fourth one, and a StateUnanswered
// constant would invite it into the CHECK constraint and into rows.
type State string

const (
	// StateGranted is an affirmative answer given behind Proof of Email
	// Ownership. The only state that authorizes anything.
	StateGranted State = "granted"
	// StateDenied is an explicit No — including the No of leaving an unticked
	// box unticked at a capture moment (ADR 0034).
	StateDenied State = "denied"
	// StatePendingConfirmation is a tick from somebody who had not proven the
	// address: denied for sending, unanswered for prompting, never expiring
	// (ADR 0035).
	//
	// NOTHING PRODUCES ONE ANY MORE (ADR 0054, #386): its only producer was the
	// guest checkout, and that route is deleted. The state is not, and neither is
	// a single row in it. Every resolver stays wired — the Consent Confirmation
	// Links already sitting in people's inboxes, and the proven owner answering at
	// their next capture moment — because the alternative to leaving a Pending
	// Confirmation alone is either manufacturing consent or destroying evidence,
	// and ADR 0035 refuses to read an answer out of silence. No expiry job, no
	// migration.
	StatePendingConfirmation State = "pending_confirmation"
)

// Answered reports whether this state is an answer from the address's owner.
// Pending Confirmation is not: it is somebody else's tick, and the owner is
// asked again at their next capture moment.
func (s State) Answered() bool {
	return s == StateGranted || s == StateDenied
}

// Answers are the three checkboxes as one person left them on one surface.
//
// Each is a *bool, and nil is load-bearing: NIL MEANS THE BOX WAS NOT SHOWN
// HERE, which is a different fact from false. A Customer re-prompted after a
// Policy Version bump is shown the required box alone and their standing
// optional answers are not churned, so their capture carries a non-nil
// PolicyAcceptance and two nils — and reading those nils as refusals would turn
// a re-acceptance into a marketing opt-out.
//
// False is an explicit No: an optional box that was SHOWN and left unticked is
// a refusal and is recorded as one.
type Answers struct {
	PolicyAcceptance  *bool
	MarketingConsent  *bool
	NetworkingConsent *bool
	// TermsAcceptance is the Términos y Condiciones box (#536, ADR 0066): the
	// contractual acceptance, captured beside the privacy answers but never one
	// of them. Nil is load-bearing here exactly as above — the box was not shown
	// on this surface — and it is the value every existing surface passes: a
	// settings toggle, an unsubscribe, a Withdraw All and an operator-recorded
	// withdrawal all say nothing about the Terms, which is what keeps the
	// no-withdrawal ruling structural rather than remembered.
	TermsAcceptance *bool
	// AdulthoodDeclaration is the second, separate box beside the Terms one
	// (#586, ADR 0069): the person's affirmation that they are eighteen or
	// older. Nil is load-bearing exactly as above and is what every surface
	// passes today — the box is drawn only where the edition in effect carries
	// the `label-adulthood-declaration` Artifact.
	//
	// IT IS ONLY EVER TRUE OR NIL. False never reaches a capture, because an
	// untick is refused before any Capture and writes nothing at all
	// (ErrAdulthoodDeclarationRequired): the platform keeps no record of anybody
	// who says they are a minor. The field is a *bool rather than a bool anyway,
	// because "the box was not shown" is a fact worth being able to state, and
	// it is the fact the null in the column records.
	//
	// It is recorded and never derived. That is deliberately redundant with the
	// edition's Artifact set — a refusal writes nothing, so an acceptance of an
	// Artifact-carrying edition necessarily ticked both boxes — and it is stored
	// anyway so that a finished fact never depends on how rows are read later.
	AdulthoodDeclaration *bool
}

// Evidence is the technical proof of one capture act: the circumstances, as the
// platform observed them.
//
// None of it proves anything on its own — an IP arrives through proxies, a user
// agent is whatever a browser claims — and none of it is what makes a consent
// valid; Capture.EmailProven is. It corroborates: it is what lets a compliance
// officer tie a row to a session and a device rather than only to an address.
//
// Every field is optional. A surface that has none records nothing rather than
// empty strings, because "not collected" and "collected as blank" are different
// answers (migration 061).
type Evidence struct {
	// IP is the client address as platform.ClientIP derived it — one agreed
	// value per request, never a handler's own reading of the headers.
	IP string
	// UserAgent is the User-Agent header verbatim.
	UserAgent string
	// SessionID is the session the act happened under. At sign-in it is the
	// Customer Session the capture is about to MINT, which is the identifier
	// that ties the evidence to what the person did next.
	SessionID string
	// OriginURL is the page the capture happened on.
	OriginURL string
	// PresentedLocale is the language of the LEGAL TEXT THAT WAS RENDERED at
	// this capture moment — the Short Notice beside the boxes, the acceptance
	// label on them — and never the language of the page it was rendered on
	// (#567, migration 115).
	//
	// The distinction is the whole point of the field. A surface asks for a
	// language and the platform serves what the edition actually publishes: the
	// staff terms gate floors at the prevailing text when the edition has no
	// artifact in the page's language, so the request's locale can be a lie
	// about what somebody read. What is recorded here is what the renderer
	// returned, which is why identity's termsGateLabels hands back the locale it
	// used alongside the string.
	//
	// A PER-CALLER FIELD, LIKE SessionID, and for the same reason: it is known
	// to the surface that rendered the text and not to the request that carried
	// the answer, so EvidenceFromRequest cannot fill it in and does not try.
	//
	// Empty on every surface that presented NO fresh document — the Customer
	// Area toggles, unsubscribe, the digest link, an operator-recorded
	// withdrawal, the withdrawal passcode door, and the confirmation link, which
	// confirms an earlier act rather than showing new text. Empty is stored as
	// SQL NULL there, which is the truthful answer: nothing was shown.
	PresentedLocale platform.Locale
}

// Capture is one act of asking a person what they authorize, and is the input
// to the platform's single consent-write path (service.Capture).
//
// Every surface in the parent feature builds one of these: the sign-in consent
// step, the checkout, the Customer Area toggle, the unsubscribe link, the
// confirmation link. The value of having one struct is that none of them can
// forget a field — a surface that does not collect an origin URL passes an
// empty one visibly, rather than silently calling a shorter overload.
type Capture struct {
	// CustomerID is who this act was about. Required: the Customer record always
	// exists by the time consent is recorded, because proof of ownership creates
	// it and a checkout writes its record only when the sale commits.
	CustomerID string
	// Email is the address AS ASSERTED on this surface, which is not necessarily
	// the Customer's stored one — a guest may have typed a stranger's.
	Email string
	// Channel is the surface. Required and validated.
	Channel Channel
	// EmailProven is whether this act was behind Proof of Email Ownership, and
	// it is AN EXPLICIT INPUT rather than something inferred from the Channel.
	// Every surface passes true today — since ADR 0054 a checkout cannot be begun
	// for an address nobody proved — but a surface's name is not a security
	// property, and an inference would keep returning the old answer on the day a
	// surface changes. It is what decides whether an optional tick becomes granted
	// or Pending Confirmation, and it is still read from the snapshot a Payment
	// took at begin, so a checkout begun under the old rules settles as what it
	// was.
	EmailProven bool
	// Answers is what the person did with the boxes they were shown.
	Answers Answers
	// TermsVersionID is the Terms edition the surface HELD alongside a Terms
	// answer that had to survive a Payment Provider redirect (#537): resolved
	// server-side at begin-checkout, snapshotted on the Payment (migration 108),
	// and presented here so the record evidences the edition the buyer was
	// actually shown rather than whichever is current by the time the provider
	// answers. Empty on every other surface, where the answer is captured in
	// the same request it was given in and the current edition IS the shown
	// one — an empty value means "resolve the current edition", never "no
	// edition". It is only ever read when Answers.TermsAcceptance is non-nil,
	// and it never comes from a request body: both writers of this field are
	// this platform's own held snapshots.
	TermsVersionID string
	// Evidence is the circumstances.
	Evidence Evidence
	// RecordedBy is the staff member who entered this act on the Customer's
	// behalf, and is EMPTY ON EVERY ACT A CUSTOMER PERFORMED THEMSELVES — which
	// is every act the platform has recorded to date and the overwhelming
	// majority it ever will (#271, migration 067).
	//
	// It is an email, taken from the Staff Session and never from a request body,
	// exactly as every other operator attribution on this platform is. Its
	// emptiness is the assertion that nobody stood between the person and the
	// record; its presence is the assertion that somebody did, which is what
	// stops a staff action from ever being presented as somebody's own click.
	RecordedBy string
	// RequestReference names the inbound artefact this act answers: the dated
	// form, the letter, the email to the data-protection address.
	//
	// IT IS A POINTER TO EVIDENCE HELD ELSEWHERE, NOT EVIDENCE ITSELF. Everything
	// else on a Consent Record is something the platform OBSERVED; this is a
	// human's note saying where the paper is. It is never parsed and nothing is
	// ever decided from its contents — a rule that branched on it would be
	// treating a filing reference as a fact about consent.
	//
	// Empty on every act a Customer performed themselves, which answers no
	// artefact at all.
	RequestReference string
}

// Receipt is what the platform recorded, returned to the surface that captured
// it.
//
// It reports the resulting STATE and not merely the answers, because the two
// differ in exactly the case that matters: a tick from an unproven address is
// recorded faithfully and leaves the state Pending Confirmation, and a surface
// that told the person "you are subscribed" on the strength of their tick would
// be lying. The Policy Version is here for the same reason — which edition was
// recorded is the platform's finding, and the surface learns it rather than
// asserting it.
type Receipt struct {
	// RecordID is the Consent Record this act wrote. There is always exactly one.
	RecordID string
	// CapturedAt is the server clock at the moment of capture.
	CapturedAt time.Time
	// PolicyVersionID is the edition the record points at.
	PolicyVersionID string
	// PolicyVersionLabel is that edition as a human names it.
	PolicyVersionLabel string
	// MarketingConsent and NetworkingConsent are the states AFTER this act, empty
	// when still unanswered.
	MarketingConsent  State
	NetworkingConsent State
	// Withdrawn is what this act TOOK AWAY, and it is the whole of the question
	// "was this a Consent Withdrawal?" answered where it can be answered
	// correctly: inside the transaction that observed the prior state under the
	// Customer row's lock (#266).
	//
	// A caller cannot compute it for itself and must not try. The states above
	// say what is true now, and "denied" is the same value whether the person
	// just gave something up or was declining for the second time — which is
	// exactly the distinction that decides whether anybody is written to (#267).
	Withdrawn Withdrawn
}

// Withdrawn is which optional consents one capture act took away.
//
// It is a different question from the answers and a different question from the
// resulting state, and it is a type of its own for the reason Pending is: the
// three are told apart by name at every call site, and a bool pair called
// "denied" would be indistinguishable from the answers beside it.
//
// The Consent Withdrawal is the whole vocabulary here — never revocation, which
// stays this platform's word for destroying a credential (ADR 0038).
type Withdrawn struct {
	MarketingConsent  bool
	NetworkingConsent bool
}

// Any reports whether this act took anything away at all, which is the
// predicate the confirmation mail is sent on (#267, parent #265). An act that
// moved nothing writes its Consent Record and sends nothing: nobody is told
// about a change that did not happen.
func (w Withdrawn) Any() bool {
	return w.MarketingConsent || w.NetworkingConsent
}

// Withdrew reports whether moving one optional consent from before to after
// took something away.
//
// THE CONDITION IS A MOVE OUT OF GRANTED OR PENDING CONFIRMATION AND INTO
// DENIED, and each half of that is load-bearing:
//
//   - Out of `granted` is the ordinary withdrawal, and out of
//     `pending_confirmation` is one too: somebody's tick was standing against
//     this address, the platform was still holding it as unresolved, and the
//     owner has now settled it as No. Something the person had not asked for
//     stopped being possible, and they are entitled to be told it did.
//   - Into `denied` is what tells a withdrawal from a GRANT. A press of the
//     confirmation link moves a consent out of `pending_confirmation` too — into
//     `granted` — and that is the opposite act. Testing only the origin would
//     confirm a double opt-in as though it were a withdrawal.
//
// Everything else moved nothing worth telling anybody about: denied to denied
// is somebody switching off a switch that was already off, and an unanswered
// consent answered No for the first time is a refusal rather than a withdrawal
// (the guidance's revocation register asks for exactly this distinction).
func Withdrew(before, after State) bool {
	if after != StateDenied {
		return false
	}
	return before == StateGranted || before == StatePendingConfirmation
}

// Outstanding is which boxes a person still has to be shown, and is the one
// predicate every capture surface asks before it renders anything.
//
// It is computed from stored STATE and never from the Consent Record log: the
// log says what happened, the state says what is true now, and this is a
// question about now.
//
// The two rules it encodes, both from the parent spec:
//
//   - PolicyAcceptance is outstanding when there is no recorded acceptance of
//     the CURRENT Policy Version. A new edition therefore re-gates everybody who
//     ever accepted the old one, without a single row changing.
//   - An optional consent is outstanding when its state is unanswered — and
//     Pending Confirmation counts as unanswered here, because somebody else's
//     tick is not the owner's answer (ADR 0035). It is emphatically NOT a
//     reason to pre-tick the box: what is stored decides whether to ASK, never
//     what to show as already agreed.
type Outstanding struct {
	PolicyAcceptance  bool
	MarketingConsent  bool
	NetworkingConsent bool
	// TermsAcceptance is outstanding when there is no recorded acceptance of the
	// CURRENT Terms Version (#536, ADR 0066) — the same version-not-document rule
	// PolicyAcceptance states, over the parallel table, so publishing a Terms
	// edition re-gates everybody without a row changing and without re-gating
	// the Privacy Policy, or vice versa.
	TermsAcceptance bool
	// AdulthoodDeclaration is the 18+ box beside the Terms one (#586, ADR
	// 0069), and it TRACKS TermsAcceptance rather than being computed
	// independently of it: it is owed when the Terms box is owed AND the edition
	// in effect carries the `label-adulthood-declaration` Artifact, and it is
	// owed at no other time.
	//
	// THERE IS NO SECOND GATE HERE, and this field's dependence on the one above
	// is the whole of that. A declaration has no editions, no lineage and no
	// satisfying set of its own — there is nothing about it that can change and
	// therefore nothing to re-ask — so an independent Standing would be a
	// near-copy of Terms standing whose only power would be to disagree with it.
	// Whether to ASK is a fact about the edition in effect, which is why the
	// Artifact decides it; whether the answer is STORED is a different question
	// with a different answer, and Answers.AdulthoodDeclaration is where that
	// one lives.
	//
	// NOTHING ON `customers` FEEDS IT. The current-state block there answers
	// which boxes a person must still be shown, and this box is never decided
	// independently of the Terms — so there is no column for it, and a person
	// Current on an Artifact-carrying edition is never re-asked, for exactly the
	// reason they are not re-asked the acceptance it rode in on.
	AdulthoodDeclaration bool
}

// Any reports whether anything is outstanding at all.
//
// AdulthoodDeclaration is deliberately absent from the disjunction, and its
// absence changes no answer: it is true only where TermsAcceptance already is,
// so naming it would add a term that can never decide the result. Leaving it
// out is how the tracking rule stays legible here — this type is read as the
// list of things that can be owed, and a fifth term in the disjunction invites
// the reading that there are five independent gates, which is exactly what
// there are not.
//
// THE STOREFRONT'S anyConsentBox NAMES IT AND IS ALSO RIGHT (checkout-consent.ts).
// The two are not in disagreement about the fact — neither can be decided by the
// declaration — only about what a wrong reading costs on each side. Here an
// extra term buys nothing and blurs the tracking rule. There, the same
// disjunction decides whether a section of the dialog is rendered at all, so
// omitting a box the API turns out to owe is a submit button disabled forever
// beside a checkbox that is not on the screen. Legibility is worth more where
// the answer cannot change; the box being on the screen is worth more where it
// can.
func (o Outstanding) Any() bool {
	return o.PolicyAcceptance || o.MarketingConsent || o.NetworkingConsent || o.TermsAcceptance
}

// Pending is which optional consents currently sit in Pending Confirmation:
// ticked by somebody who had not proven the address, denied for sending,
// unanswered for prompting, and never expiring (ADR 0035).
//
// It is a different question from Outstanding and deliberately a different
// type, although one implies the other. Outstanding asks "must this person be
// SHOWN this box?", which a Pending Confirmation and a never-answered NULL both
// answer yes to. This asks "is there somebody else's tick here WAITING to be
// resolved?", which only a Pending Confirmation answers yes to — and it is the
// question the Sale Confirmation's confirmation line is decided by (#255).
// Folding the two together would put that line into receipts for people who had
// simply never been asked, offering to confirm an opt-in nobody ever made.
//
// Policy Acceptance is absent, and that is the model rather than an omission: a
// guest's acceptance is recorded unconditionally because it is a fact about the
// sale rather than a claim on an inbox (ADR 0035), so it never pends.
type Pending struct {
	MarketingConsent  bool
	NetworkingConsent bool
}

// Any reports whether anything is waiting to be confirmed at all.
func (p Pending) Any() bool {
	return p.MarketingConsent || p.NetworkingConsent
}

// Intersect narrows this set to the boxes another set also names. It is how a
// confirmation link's SCOPE — what pended when the mail was written — meets the
// state as it stands when somebody finally presses it, months later.
func (p Pending) Intersect(other Pending) Pending {
	return Pending{
		MarketingConsent:  p.MarketingConsent && other.MarketingConsent,
		NetworkingConsent: p.NetworkingConsent && other.NetworkingConsent,
	}
}

// Confirmation is one press of the confirmation link in a Sale Confirmation:
// the double opt-in's resolving half (#255, ADR 0035).
//
// It is deliberately not a Capture, and a caller must not build one into a
// Capture itself: the answers a press produces are not the presser's to state.
// What a press means is "whatever of my scope is still pending, I confirm", and
// only the consent module can see what that is at the moment of the write. A
// surface that composed the Answers from what the mail once offered would
// resurrect an answer the owner has since changed.
type Confirmation struct {
	// CustomerID is who the signed token named.
	CustomerID string
	// Email is the address the confirmation was sent to — the Customer's own
	// stored one, since that is the only address a link this platform mailed
	// could have reached.
	Email string
	// Scope is what pended at the moment the link was minted, carried inside the
	// signed payload. A press confirms nothing outside it: the mail said what it
	// was offering to confirm, and a Pending Confirmation created afterwards by
	// some other checkout was never on the page the person read.
	Scope Pending
	// Evidence is the technical proof of the press.
	Evidence Evidence
}

// ConfirmationResult is what a press actually did.
//
// Confirmed is the set of boxes THIS press flipped, and it is empty on a second
// press or on a link whose pendings the owner has since resolved themselves.
// That is not an error (see service.ConfirmPending); it is the difference the
// confirmation page tells the person, because "confirmed" and "there was
// nothing left to confirm" are both true outcomes and only one of them is news.
type ConfirmationResult struct {
	Confirmed Pending
	// MarketingConsent and NetworkingConsent are the states as they stand AFTER
	// the press, including where the press changed nothing — so the page reports
	// what is true rather than what was asked for.
	MarketingConsent  State
	NetworkingConsent State
}
