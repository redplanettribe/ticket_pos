package catalog

import "time"

// The rationing on the Assignment mail: a hard per-Ticket cap and a per-buyer
// rate limit (#332, parent #322, ADR 0046).
//
// WHAT THIS CLOSES. Reassignment is free by design — the buyer may re-point any
// Ticket at any address at any time — and every reassignment to a NEW address
// mails that address. Uncapped, one Ticket is an unlimited mailer, and the
// platform becomes a thing that will email anyone a buyer names. Purchase Limit
// does not cover it: CONTEXT.md calls that "a deterrent against taking too many
// rather than a defence against someone minting identities", and it is unset on
// most Ticket Types, so a Free Ticket Type would otherwise be a bulk sender.
//
// WHAT IS BEING PROTECTED IS ONE SENDING REPUTATION, shared by passcodes, Sale
// Confirmations and this mail. That is the same asset the OTP global ceiling
// exists for, and the reason the Follow Digest was moved to a domain of its own.
// A bounce or a spam complaint from a stranger who never came here costs the
// platform on every other message it sends.
//
// TWO LIMITS BECAUSE THERE ARE TWO ATTACKS, and either alone leaves the other
// open. A per-Ticket cap on its own is defeated by buying fifty free tickets and
// scripting one send each. A per-buyer rate limit on its own leaves a single
// Ticket cycling addresses forever at a slow drip. So: a Ticket has a fixed
// lifetime allowance, and a buyer has an hourly-scale one across all their
// Tickets at once.
//
// BOTH ARE COUNTED FROM SENDS AND NEVER FROM ASSIGNMENTS. An assignment that
// mails nobody — the same address typed twice, a mail the provider refused — is
// not a use of anybody's allowance, because what is rationed is the writing to
// strangers and not the buyer's record-keeping.

const (
	// AssignmentMailsPerTicket is every Assignment mail one Ticket may ever
	// send, for its whole life, across every Holder it is ever pointed at.
	//
	// THREE, WHICH IS ONE SEND PLUS A RESEND ALLOWANCE OF TWO. The first mail is
	// the feature working; the other two are the buyer being human. A mistyped
	// address is the commonest failure this feature has — the buyer is typing
	// somebody else's address from memory and nothing downstream can check it —
	// and a cap of one would mean a single slipped character bricks that Ticket's
	// assignment forever. Two corrections covers a typo, and then the friend who
	// says "that's my old address".
	//
	// IT IS DELIBERATELY NOT GENEROUS. The Answer Reminder's prior art is "at
	// most two ever" for a mail to a person who actually bought something; this
	// one goes to somebody who never came here, so three is already the looser
	// number of the two. What the cap costs, in the rare case it binds honestly,
	// is that the buyer falls back to the Answer Link — which still works for
	// every unaccepted Ticket, and is precisely the degradation path ADR 0046
	// kept the Answer Link alive to provide.
	AssignmentMailsPerTicket = 3

	// AssignmentMailsPerBuyer is every Assignment mail one buyer may send across
	// ALL of their Tickets inside AssignmentMailWindow.
	//
	// TWENTY IN TWENTY-FOUR HOURS. The shape matters more than the number: a
	// ROLLING window, so a buyer who trips it waits rather than being cut off,
	// and a LONG one, because a short window with the same count is just a
	// higher daily rate wearing a stricter face — ten an hour is two hundred and
	// forty a day.
	//
	// Twenty clears the honest case with room to spare. A large group booking is
	// a handful of Tickets assigned in one sitting, and the per-Ticket cap means
	// twenty mails is at least seven Tickets even if every one of them is
	// corrected twice. What it denies is the interesting case: reaching twenty
	// fresh strangers a day is not a mailing list, and the attempt shows up in
	// the ledger before it shows up in the bounce rate.
	AssignmentMailsPerBuyer = 20
)

// AssignmentMailWindow is how far back the per-buyer rate limit counts.
//
// A DAY, ROLLING, AND NOT A CALENDAR ONE. A calendar day would hand out a fresh
// allowance at a fixed moment, in some timezone that would have to be chosen,
// and would let a script send two full allowances back to back across midnight.
// Rolling has no such seam and needs no timezone.
const AssignmentMailWindow = 24 * time.Hour

// AssignmentMailRefusal is why an Assignment mail may not be sent, or that it
// may.
//
// AN INT AND NOT A STRING, unlike TicketAssignmentState beside it, because this
// one never travels on the wire as itself: it becomes a typed error with an API
// code, and the Storefront reads the code.
type AssignmentMailRefusal int

const (
	// AssignmentMailAllowed is room under both limits.
	AssignmentMailAllowed AssignmentMailRefusal = iota
	// AssignmentMailRefusedTicketCap is this Ticket's lifetime allowance spent.
	// PERMANENT: no amount of waiting returns it.
	AssignmentMailRefusedTicketCap
	// AssignmentMailRefusedBuyerRate is this buyer's window full. TEMPORARY: it
	// clears as the window rolls forward.
	AssignmentMailRefusedBuyerRate
)

// AssignmentMailLimits is the pair of numbers in force. It is passed in rather
// than read from the constants so that the integration suite can lower them —
// the same posture the OTP global ceiling takes, and for the same reason: the
// numbers are configuration, and what is under test is the behaviour AT them.
type AssignmentMailLimits struct {
	PerTicket int
	PerBuyer  int
}

// AssignmentMailInputs is everything the decision reads.
type AssignmentMailInputs struct {
	// SentForTicket is how many Assignment mails this Ticket has EVER sent.
	SentForTicket int
	// SentByBuyerInWindow is how many this buyer has sent, across every Ticket
	// they hold, inside AssignmentMailWindow.
	SentByBuyerInWindow int
	Limits              AssignmentMailLimits
}

// MayMailAssignment decides whether the platform may write to one more stranger
// on this buyer's say-so.
//
// THE PER-TICKET CAP IS TESTED FIRST, and the order is load-bearing rather than
// arbitrary. Both refusals are true at once for a buyer who is both over their
// window and out of allowance on this particular Ticket, and the two say
// different things to them: one is "wait", the other is "this Ticket is done,
// use the Answer Link". Reporting the temporary one for a permanent condition
// would send a buyer back in an hour to hear the same refusal forever. This is
// the same discipline the OTP service keeps when it reports a caller's own
// throttling ahead of the platform-wide ceiling.
//
// A ZERO OR NEGATIVE LIMIT MEANS THE DEFAULT AND NEVER "SEND NOTHING", read from
// the constants above. An unset configuration must not be able to silently turn
// the feature off, which is a flag's job and not a limit's.
func MayMailAssignment(in AssignmentMailInputs) AssignmentMailRefusal {
	perTicket := in.Limits.PerTicket
	if perTicket <= 0 {
		perTicket = AssignmentMailsPerTicket
	}
	perBuyer := in.Limits.PerBuyer
	if perBuyer <= 0 {
		perBuyer = AssignmentMailsPerBuyer
	}

	if in.SentForTicket >= perTicket {
		return AssignmentMailRefusedTicketCap
	}
	if in.SentByBuyerInWindow >= perBuyer {
		return AssignmentMailRefusedBuyerRate
	}
	return AssignmentMailAllowed
}
