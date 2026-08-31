package legal

// Standing is where one person stands against one legal document's gate
// (#565, parent #556, ADR 0067).
//
// FOUR VALUES, AND ONLY FOUR, and the count is a decision rather than an
// accident. Each of them is a different sentence about a different person, and
// each answers a different operator question:
//
//	Current      they have accepted an edition that still satisfies the gate.
//	             Nothing is owed and nobody need be chased.
//	Outstanding  they accepted something, and what they accepted has been
//	             superseded by a gating publication. They owe a RE-acceptance.
//	NeverSeen    they have never accepted anything at all.
//	Former       (staff only) they are not on the Staff platform any more.
//
// WHY *NeverSeen* IS NOT A KIND OF *Outstanding*, which is the collapse this
// vocabulary exists to refuse. On the production copy 511 of 1,569 Customers —
// about a third — have never accepted anything: a box-office sale, a Sale
// Import or a pre-consent checkout created their record and they have never
// themselves acted on a Storefront surface (migration 062 says there is
// deliberately nothing to backfill them with). Folding those 511 into
// "outstanding" would bury the handful who actually owe a fresh acceptance
// under a third of the customer base, and the default filter — the one screen
// this feature exists to make usable — would be the one nobody could read.
//
// WHY *Former* IS NOT A KIND OF *Outstanding* EITHER, and this is the same
// argument from the other end: a leaver owes nothing, because they cannot sign
// in and the gate they would meet is one they will never reach. Listing them as
// outstanding would poison the default filter with people who can never leave
// it — an outstanding list must be a list of people somebody can chase.
//
// WHY THERE IS NO *Withdrawn*. Withdrawal is a state of an OPTIONAL consent
// (marketing, networking) and of neither gate. The Policy gate has no
// withdrawal path — withdrawing marketing does not un-accept a Privacy Policy —
// and the Terms gate has none by construction (migration 106: acceptance is
// contractual, and Withdraw All leaves the columns untouched). A "withdrawn"
// value here would let a withdrawn marketing consent be misread as an
// unaccepted document, which is the confusion the whole four-value vocabulary
// is arranged to prevent.
//
// AND THERE IS NO OPTIONAL-CONSENT COLUMN on either browser, for a reason that
// is about what a screen IS rather than about what it costs: a filterable
// roster with a marketing-consent column is a segmentation tool, whatever it is
// called and whoever built it. Optional consents belong to the per-subject
// record, where they are read one person at a time by somebody acting on that
// person's behalf.
type Standing string

const (
	// StandingCurrent: an acceptance naming an edition in the satisfying set.
	StandingCurrent Standing = "current"
	// StandingOutstanding: an acceptance naming an edition BELOW the gating
	// floor. Something was accepted; it no longer clears.
	StandingOutstanding Standing = "outstanding"
	// StandingNeverSeen: no acceptance of this document, ever.
	StandingNeverSeen Standing = "never_seen"
	// StandingFormer: no longer on the Staff platform.
	//
	// COMPUTED, NEVER STORED. There is no leaver column, no `left_at`, and no
	// row anywhere that says somebody used to be staff: it is read as "holds a
	// Staff Terms Acceptance and appears in neither `members` nor
	// `platform_operators`". That keeps departure a fact about the membership
	// tables — the only place a departure is ever recorded, by a DELETE — and
	// means no second thing can go stale or disagree with them.
	//
	// It is a FILTER VALUE AND NOT A HIDDEN STATE: a Former person is listed
	// when you ask for Former and is absent from every other list, rather than
	// silently dropped from a total or quietly counted somewhere.
	//
	// STAFF ONLY. A Customer never becomes former: Customer records are never
	// deleted (a Ticket Sale is a financial record that must reconcile,
	// migration 016), so there is no departure to observe and inventing one
	// would mean guessing from inactivity.
	StandingFormer Standing = "former"
)

// ParseStanding reads a standing filter off the wire.
//
// Reports false for anything else — including the empty string, so that "no
// filter given" is a decision the CALLER makes (the browsers default to
// Outstanding) rather than a silent widening to everybody that this function
// performs on their behalf.
func ParseStanding(raw string) (Standing, bool) {
	switch Standing(raw) {
	case StandingCurrent, StandingOutstanding, StandingNeverSeen, StandingFormer:
		return Standing(raw), true
	}
	return "", false
}

// StandingOf reads one person's standing against one document from the single
// fact the databases store about it: WHICH EDITION, if any, they accepted.
//
// It returns Current, Outstanding or NeverSeen and NEVER Former, because Former
// is not a fact about an acceptance — it is a fact about a population, decided
// by whether the person still appears in `members ∪ platform_operators`, and a
// function handed one edition id could only guess at it.
//
// MEMBERSHIP OF THE SATISFYING SET, NEVER EQUALITY WITH THE CURRENT EDITION
// (#560). That is what makes a CORRECTION MOVE NOBODY INTO OUTSTANDING: a
// correction sits above the gating floor, so it joins the set without changing
// it, and everybody who was Current stays Current with no backfill and no
// notification. Comparing against "the current edition" instead would re-gate
// the entire population every time a typo was fixed — which is the exact defect
// this vocabulary was built on top of #560 to avoid.
//
// acceptedEditionID is "" for no acceptance, which is how both databases spell
// it: NULL in customers.policy_version_id / customers.terms_version_id, and the
// absence of a staff_terms_acceptances row.
func StandingOf(acceptedEditionID string, satisfying SatisfyingSet) Standing {
	if acceptedEditionID == "" {
		return StandingNeverSeen
	}
	if satisfying.Contains(acceptedEditionID) {
		return StandingCurrent
	}
	return StandingOutstanding
}
