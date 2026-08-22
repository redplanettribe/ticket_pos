package sales

import "time"

// The retention rule for the Answers a buyer typed at checkout onto a Payment
// that never became a sale (#316, ADR 0044).
//
// WHY THIS DEPARTS FROM EVERYTHING ELSE HELD ON A PAYMENT. The Tax ID, the
// phone, the Sale Locale, the affiliate attribution and the consent answers are
// all snapshotted onto a Payment across the Payment Provider redirect too, and
// none of them is ever purged: an abandoned Payment keeps them forever, because
// they are the record of an attempt to transact and the platform is entitled to
// remember one. An Answer is not that. It may be somebody's allergies, their
// accessibility needs or their child's age — and once the checkout is abandoned
// there is no Ticket for it to be about and nothing on this platform will ever
// use it for anything. Keeping it would be holding health data for no purpose,
// which is the one thing a purpose limitation does not permit.
//
// The consent answers are the closest neighbour and the sharpest contrast: those
// are deliberately NEVER promoted and NEVER purged, because an abandoned Payment
// leaves no Consent Record and the snapshot is the only evidence of what was
// asked. Evidence of an ask is not the same kind of thing as the answer to it.

// AbandonedAnswerRetention is how long the Answers held on a Payment survive the
// Payment failing to reach 'approved'.
//
// THIRTY DAYS IS NOT A PRIVACY FIGURE; IT IS A CORRECTNESS ONE, and it is the
// reason this constant may not be shortened casually. The purge cannot key on a
// Payment's status meaning "this is over", because in this codebase no status
// means that: 'pending' is what an abandoned Payment stays as forever on a quiet
// Event (nothing sweeps it), and 'expired' is lazy, opportunistic bookkeeping
// that a late provider confirmation is explicitly allowed to reverse into
// 'approved' (ADR 0013, ApprovePaymentAndCommitSale). The only safe signal that
// a Payment will never commit is AGE — no Payment Provider confirms a month
// after the redirect — so the window is what makes acting on non-approval sound
// at all. Cut it to a day and the purge starts robbing sales that then commit,
// silently: the commit copies nothing, the buyer keeps their Tickets, and the
// Organization is simply told nobody answered.
//
// Expressed in hours rather than days because a Payment's age is arithmetic on a
// timestamp and never a calendar walk; there is no timezone in this rule and no
// daylight saving to survive.
const AbandonedAnswerRetention = 30 * 24 * time.Hour
