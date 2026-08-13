package consent

import "time"

// The Privacy page's read (#268, parent #265): what a Customer has authorized,
// as the platform currently holds it.
//
// IT IS A READ AND NOTHING ELSE. The whole of this file is a question, and the
// service method answering it touches no row. A settings page that recorded
// something because somebody looked at it would convert "never asked" into
// "denied" for every person who opened the page out of curiosity and closed it
// again, which is the one failure this surface must be structurally incapable
// of. Nothing here builds a Capture, and nothing here can.
//
// It is deliberately NOT Outstanding. Outstanding asks "which boxes must this
// person be SHOWN?" and is the predicate every capture surface renders from;
// this asks "what is true about this person?" and is the report a settings
// surface renders. Folding the two together is how a settings page becomes a
// capture surface by accident — and the line #265 draws is that `/privacy` must
// never be the surface that first ASKS a Customer a question.

// PrivacyState is one Customer's privacy settings as the platform holds them:
// the Policy Version they accepted and when, and where each optional consent
// stands right now.
//
// The Policy Version here is THE ONE THEY ACCEPTED and not the one in effect.
// Those differ for anybody who accepted an edition that has since been
// superseded, and the difference is the whole value of showing it: a page
// naming the current edition would tell a Customer they had agreed to a text
// they have never been shown.
type PrivacyState struct {
	// PolicyVersionLabel is the accepted edition as a human names it, empty
	// where nothing was ever recorded. Empty is reachable: a session minted
	// before consent capture existed outlives the deploy that introduced it, and
	// a Customer holding one has no acceptance on their row.
	PolicyVersionLabel string
	// PolicyAcceptedAt is when that acceptance was recorded, zero where there is
	// none. It moves with the label or not at all — both come off the same
	// Customer row, written by the same statement.
	PolicyAcceptedAt time.Time
	// MarketingConsent and NetworkingConsent are the two optional consents'
	// states, EMPTY MEANING NEVER ANSWERED.
	//
	// All four values a surface must be able to tell apart survive this type
	// unflattened — granted, denied, Pending Confirmation, and the empty string
	// — because each means something different to the person reading the page.
	// "Never answered" is not a refusal and must never be drawn as one; a
	// Pending Confirmation is somebody else's tick still standing unresolved,
	// which is not an answer either (ADR 0035). A bool pair here would collapse
	// three of the four into "off" and put words in the Customer's mouth.
	MarketingConsent  State
	NetworkingConsent State
}
