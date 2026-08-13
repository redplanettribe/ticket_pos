package service

import "github.com/peter/ticket_pos/backend/internal/consent"

// refuseGrantOnOperatorRequest enforces the one rule that makes the Operator's
// withdrawal surface safe to hand anybody: IT CAN ONLY WITHDRAW, NEVER GRANT
// (#271, parent #265).
//
// WHY IT LIVES HERE AND NOT IN THE HANDLER. A form without a button is a way of
// telling somebody what is offered and no way at all of guaranteeing it — the
// API is reachable with curl, and the Operator namespace is reachable by
// everybody on the allowlist. Putting the refusal in the one consent-write path
// makes "an Operator cannot manufacture consent" a property of the platform
// rather than of a screen: every future caller of Capture that names this
// channel gets it, including the one somebody writes in a hurry two years from
// now. It is the same reasoning that puts the Policy Acceptance gate in the API
// rather than in the checkbox.
//
// WHAT COUNTS AS A GRANT. Any answer that is not a denial:
//
//   - A ticked optional box, which is the obvious case. On any other channel a
//     tick becomes `granted` or Pending Confirmation depending on proof; here it
//     is refused outright, which is why the proven-ness question never arises on
//     this channel at all.
//   - A Policy Acceptance, which is not withdrawable (ADR 0038) and is therefore
//     never something this surface has business writing. An operator-recorded
//     acceptance would also be the worst kind of evidence: a staff member
//     asserting that somebody agreed to a document.
//
// An absent answer (nil) is not refused. Nil means the box was not shown on this
// surface, and a form that asks for one consent takes one consent away.
//
// It reads the CAPTURE and not the surface's intent, which is the point: a
// caller that assembled a grant by mistake, or by having its body forwarded
// unfiltered from a request, is refused by the same check that refuses a
// deliberate one.
func refuseGrantOnOperatorRequest(capture consent.Capture) error {
	if capture.Channel != consent.ChannelOperatorRequest {
		return nil
	}
	if capture.Answers.PolicyAcceptance != nil {
		return consent.ErrConsentGrantNotPermitted()
	}
	if granted(capture.Answers.MarketingConsent) || granted(capture.Answers.NetworkingConsent) {
		return consent.ErrConsentGrantNotPermitted()
	}
	return nil
}

// granted reports whether an answer is an affirmative one. A nil answer is not:
// the box was not shown, and nothing was authorized by nobody being asked.
func granted(answer *bool) bool {
	return answer != nil && *answer
}
