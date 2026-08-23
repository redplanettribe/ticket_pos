# The buyer is reminded to assign, and the first sweep is the announcement

Takes the decision [ADR 0049](./0049-only-the-holder-answers-and-the-answer-link-is-retired.md)
parked: "an Assignment Reminder is a separate decision if unassigned Tickets prove to pile up."
Builds on [ADR 0046](./0046-a-ticket-is-assigned-to-a-holder-who-accepts-by-mail.md) and
[ADR 0048](./0048-the-buyer-holds-one-ticket-by-paying.md); stays inside
[ADR 0047](./0047-the-organization-sees-the-holders-address.md).

## Context

Ticket Assignment went live on 2026-08-22, after Sales for upcoming Events had already been made.
Those buyers saw a checkout that never asked "who is this one for", and nothing since has told them
the choice now exists; their Sales' other Tickets sit `unassigned`, their Holders are unknown, and
ADR 0049 made an unassigned Ticket something nobody chases. Buyers who check out today can leave
Tickets unassigned just as easily. The Answer Reminder cannot carry this — ADR 0049 said so
explicitly — and ADR 0047 forbids a surface for the Organization to mail anybody.

## Decision

The platform sends an **Assignment Reminder**: a swept, rationed, transactional mail to the buyer of
an `online` Ticket Sale that has more than one Ticket and at least one still `unassigned`, pointing
at the Sale's own page by a fresh Confirmation Link. Rationed per Ticket Sale: not before 24 hours
after the Sale, at most once every 7 days, at most twice ever, and silent once the Event has started
or every Ticket has been assigned. Written in the Sale Locale, not gated by Marketing Consent, and
recorded in its own ledger that holds no address and no body.

The catch-up for Sales made before the feature is **not a separate mechanism**. It is the
Reminder's first sweep, with one extra sentence for Sales created before the go-live moment, which is
a constant in code — a historical fact, not a setting. Once the backlog drains, the constant
selects nothing and can be deleted.

The Customer Area does not change. The page the mail lands on already shows which Tickets have no
address and the field to give one; a banner would need a dismissal store for a message that is stale
the moment the buyer acts.

## Considered options

- **A one-off announcement mail.** Needs the same query, ledger, rationing and copy as the permanent
  Reminder, and is then thrown away. Rejected: the condition ADR 0049 named has been met, and a
  buyer who ignores the first mail two weeks before the Event is exactly who a second one is for.
- **Extending the Answer Reminder.** Rejected by ADR 0049 on its own terms: an assignment nudge is
  not an answer nudge, and the two ledgers ration different recipients about different debts.
- **Including `import` Sales.** Their buyer is a name somebody else typed and never checked out; a
  Reminder to them is cold mail from a platform they may not know, which is why Sale Correction
  (ADR 0050) mails them nothing by default. Rejected.
- **A "New!" banner in the Customer Area.** Rejected as above.

## Consequences

- A second recurring mail to Customers exists, and its policy is the Answer Reminder's policy with
  the buyer as the recipient; the two should be read and changed together.
- The go-live constant hard-codes a deployment timestamp. It is correct only for this deployment,
  which is the only one.
- A buyer of a past Event is never told. That is intended: the mail offers a choice that no longer
  matters.
- Nothing here touches ADR 0047's boundary: the platform, not the Organization, writes, and the
  Organization gains no button.
