package consent

import "time"

// Channel names the surface a capture act happened on.
//
// The vocabulary is defined here in full although #251 writes only
// ChannelSignIn: the checkout, the Customer Area toggle, the unsubscribe link
// and the confirmation link each land in their own ticket and each needs a name
// nobody has to invent under deadline. Migration 061's CHECK constraint carries
// the same five strings; adding a sixth means changing both.
type Channel string

const (
	// ChannelSignIn is the consent step between Proof of Email Ownership and a
	// Customer Session — both doors, passcode and Google alike.
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
)

// Valid reports whether the channel is one the storage vocabulary recognises. A
// capture with an unknown channel is refused by the service rather than left to
// the database's CHECK, so the failure names the bug rather than the row.
func (c Channel) Valid() bool {
	switch c {
	case ChannelSignIn, ChannelCheckout, ChannelAccountSettings, ChannelUnsubscribeLink, ChannelEmailConfirmation:
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
	// Today `signin` is always proven and `checkout` may be either, but a
	// surface's name is not a security property: an inference would keep
	// returning the old answer on the day a surface changes. It is what decides
	// whether an optional tick becomes granted or Pending Confirmation.
	EmailProven bool
	// Answers is what the person did with the boxes they were shown.
	Answers Answers
	// Evidence is the circumstances.
	Evidence Evidence
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
}

// Any reports whether anything is outstanding at all.
func (o Outstanding) Any() bool {
	return o.PolicyAcceptance || o.MarketingConsent || o.NetworkingConsent
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
