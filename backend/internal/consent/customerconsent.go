package consent

import "time"

// CustomerConsent is one Customer's consent state as it stands NOW — what is
// true, as against the log of what happened.
//
// It is a read, and rendering it writes nothing. That is worth stating in the
// type rather than only in the endpoint that serves it: the never-pre-tick rule
// governs CAPTURE surfaces, moments where the platform asks, and this is a
// report. A surface that recorded a refusal because somebody looked at it would
// convert "never asked" into "denied" for people who did nothing.
//
// The optional consents are consent.State, whose zero value — the empty string
// — is UNANSWERED and is a different fact from denied. Anything reporting this
// to a human must keep the two apart, because the whole point of showing it to
// an Operator before they act is to say what a withdrawal would actually change
// (#271, parent #265).
//
// Policy Acceptance is here as a timestamp and is deliberately NOT withdrawable
// (ADR 0038): it is absent from counsel's form, it gates the platform on a basis
// other than consent, and clearing it would re-gate the person rather than free
// them. It is reported so that whoever is looking can see the whole of what the
// platform holds, and for no other reason.
type CustomerConsent struct {
	// MarketingConsent and NetworkingConsent are the states as they stand, empty
	// where the Customer has never been asked.
	MarketingConsent  State
	NetworkingConsent State
	// PolicyAcceptedAt is when this Customer last accepted a Policy Version, zero
	// where no acceptance has ever been recorded — which is every Customer a box
	// office sale created and everybody who has not signed in since the gate
	// existed.
	PolicyAcceptedAt time.Time
}
