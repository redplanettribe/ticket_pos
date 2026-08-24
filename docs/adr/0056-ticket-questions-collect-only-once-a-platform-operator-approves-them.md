# Ticket Questions collect only once a Platform Operator approves them

**Stacks on [ADR 0045](./0045-ticket-questions-ship-dark-until-the-privacy-policy-describes-them.md)
and does not supersede it**: the Policy Version describing this collection still publishes first,
and `TICKET_QUESTIONS_ENABLED` still flips only after it. Widens the organizer-facing mail channel
[ADR 0026](./0026-payout-requests-the-organization-asks-the-operator-answers.md) opened to a second
use, which 0026 named as a decision not yet taken.

## Context

A Ticket Question can ask anything an Organization types. ADR 0045 dealt with the platform's own
exposure — the published policy says the platform does not deliberately collect sensitive data — by
keeping the feature dark until the policy describes the collection. It does not deal with the next
exposure: once the policy covers "Organization-defined questions", nothing stops an Organization
asking "any medical conditions?" in a free-text box on a Tuesday, and the platform being the
processor that collected the answers by Wednesday.

The Platform Operator is the party answerable for what the platform collects, and today they cannot
see a question until an Answer exists. ADR 0045 rejected forbidding sensitive questions in the UI
(unenforceable) and a curated set (Organizations use "other"). What is left is a human reading each
question before it is asked.

The platform already has a shape for "the Organization asks, the Operator answers": the Payout
Request — an ask that moves nothing until answered, one outstanding per Organization, a snapshot of
what was asked, a reason on a decline, a queue on the Operator Dashboard, and mail both ways.

## Decision

**Every Ticket Question is asked of nobody until a Platform Operator has approved it.** A question
is a draft when authored, under review while a Question Review carries it, and approved thereafter;
approved is the only state in which it is asked at checkout or in the Customer Area, chased by the
Answer Reminder, counted as an Outstanding Answer, shown on the Holder List or given a column in the
Sales Export.

**The unit of review is the question, the unit of the ask is the Event.** An Org Admin or Event
Owner submits an Event's drafts as one Question Review, one outstanding per Event, with an optional
note and a recorded acknowledgement of what the Organization is choosing to collect. The Operator
answers it as one act but per question — approve, or refuse with a reason — so a refusal over one
question of six names the one.

**An approved question is immutable.** Wording, kind and required-ness are what the Operator read.
Changing any of them is a retirement and a fresh draft; the only moves allowed without review are
the ones that collect less — making it optional, retiring an Option, retiring the question. An
Option added to an approved question is itself a draft until reviewed, and the question keeps
collecting in its approved shape meanwhile. Label corrections on approved Options are done by
retire-and-add, because a correction and a rewording are the same operation.

**Asking permission costs nothing the Organization already had.** Already-approved questions keep
collecting while a Review is outstanding.

**The Operator can take an approval back.** A Revocation retires the question with a reason the
Organization is told; Answers already given stay.

**A Review lapses when the Event starts**, and none is accepted after that, in the Event's
timezone: the moment Answers stop being changeable is the moment there is nothing left to approve.

**The six questions in production are grandfathered** by a migration that records an explicit
approval attributed to the migration. A question can be born approved by that migration and by
nothing else: approval is a record of a review, never a column default.

**Mail both ways** — Operator on submission, submitter on verdict and on Revocation — on the
channel ADR 0026 opened.

## Considered options

- **Make per-Event approval replace ADR 0045's global flag.** Rejected: the Operator's diligence
  does not change what the published policy says the platform collects. Two locks, not one.
- **An Event-level licence to ask, then free authoring.** Rejected: the Operator's concern is what
  is asked, and a licence cannot see the fourth question added after it was granted.
- **Approve the whole set, refuse the whole set.** Rejected: the Operator would write "it's number
  four" in the note anyway.
- **Go dark while a resubmission is pending.** Rejected: it punishes the Organization for asking,
  and teaches them not to add questions.
- **Allow edits to approved questions, with the old wording live until the new is approved.**
  Rejected: two texts per question for the sake of avoiding a retire-and-re-ask the Options
  identity rule already made cheap.
- **Automatic submission on every save.** Rejected: five reviews for five questions typed in ten
  minutes, and the Operator guessing whether the Organization is done.
- **A required purpose per question.** Rejected as friction that would read "we need it"; the
  Operator refuses with "say why" when a question warrants it, and the answer rides in the
  resubmission's note.
- **Start the existing production questions as drafts.** Considered right in principle and
  overruled by the user: the one real Event's questions are known and safe, and nothing collects
  before the ADR 0045 flag flips either way.

## Consequences

**A question set is now a state machine on a live table**, and every surface that lists an Event's
questions must filter on review state. The test that the buyer, Holder, reminder, export and roster
surfaces agree on which questions exist becomes an invariant to assert, not a query to trust.

**The Operator becomes a bottleneck by design.** An Organization that adds a question the day
before an Event may not get it approved in time; that is the feature, and the lapse rule makes the
outcome explicit rather than a stale queue.

**The acknowledgement on a Question Review is a record**, kept with who affirmed it and when: it is
where ADR 0045's "warned at authoring time" stops being a banner.

**The staff-to-organizer mail channel now has two senders of intent.** A third use should be a
decision, not a precedent.
