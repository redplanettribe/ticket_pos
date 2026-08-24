package catalog

// The review state of a Ticket Question and of an Option (#405, parent #404,
// ADR 0056): born a draft, asked of nobody until a Platform Operator approves.

// TicketQuestionReviewStatus is where a question or Option stands with the
// Platform Operator. The glossary words, exactly: a verdict against is
// `refused`, never rejected, and taking an approval back is a Revocation,
// which retires the row and leaves this `approved` — the approval was real.
type TicketQuestionReviewStatus string

const (
	TicketQuestionReviewDraft       TicketQuestionReviewStatus = "draft"
	TicketQuestionReviewUnderReview TicketQuestionReviewStatus = "under_review"
	TicketQuestionReviewApproved    TicketQuestionReviewStatus = "approved"
	TicketQuestionReviewRefused     TicketQuestionReviewStatus = "refused"
)

// THE ONE DEFINITION OF "ASKED", IN SQL, and the reason this file exists.
//
// Five read paths put a question in front of somebody or count it as owed: the
// checkout form and the public Event page (CheckoutQuestionsSQL), the staff,
// Customer Area and Assignment Link answer views, the Holder List with its
// Outstanding Answer counts, the Sales Export's answers sheet with its "has any
// question" gate, and the Answer Reminder's candidates. ADR 0056 made every one
// of them an invariant to assert rather than a query to trust: they must agree
// on which questions exist. So the predicate is written ONCE here and each of
// them composes it in, and a later ticket that widens or narrows the rule
// touches one string rather than five queries.
//
// Every fragment names the question as `q` and the Option as `o`, which is the
// alias every one of those queries already uses. A caller that aliased
// differently would fail to prepare, loudly, which is the right failure.

// ApprovedQuestionSQL is the questions a Platform Operator has approved,
// RETIRED ONES INCLUDED: what was ever asked. This is the filter for the
// surfaces that read Answers back — the answer views and the export — where a
// retired-but-approved question stays visible exactly where it always was,
// because the Answers under it are still on Tickets and its column is still
// owed to the file. A draft, under-review or refused question was never asked
// and has nothing to read back.
const ApprovedQuestionSQL = `q.review_status = 'approved'`

// AskedQuestionSQL is the questions being put to somebody NOW: approved and
// not retired. The checkout, the Outstanding Answer derivation and the Answer
// Reminder read this one. A Revocation sets retired_at, so it drops a question
// from here without touching its approval.
const AskedQuestionSQL = ApprovedQuestionSQL + ` AND q.retired_at IS NULL`

// ApprovedOptionSQL and OfferedOptionSQL are the same two statements about an
// Option. An Option added to an approved question is a draft until a Question
// Review approves it, so an approved question can carry Options it does not
// yet offer; the question keeps collecting in its approved shape meanwhile.
const ApprovedOptionSQL = `o.review_status = 'approved'`

// OfferedOptionSQL is the Options a choice question offers to somebody now.
const OfferedOptionSQL = ApprovedOptionSQL + ` AND o.retired_at IS NULL`
