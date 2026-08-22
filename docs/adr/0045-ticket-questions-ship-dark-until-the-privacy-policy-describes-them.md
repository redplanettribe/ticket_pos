# Ticket Questions ship dark until the Privacy Policy describes them

## Context

A Ticket Question can ask anything an Organization types. The two examples that motivated the
feature are a t-shirt size and dietary requirements — and dietary requirements are, in practice,
allergies, diabetes, coeliac disease and religious observance.

The Privacy Policy artifact does not describe this. §6.1 enumerates the categories processed —
identification, contact, academic and professional, employment, economic and financial, image and
voice — and health is not among them. §6.2 says:

> MULTITICKETING does not deliberately collect or process sensitive personal data of the SUBJECTS.
> In the event that, through exceptional circumstances or voluntary provision by the SUBJECT, data
> of this nature is received, MULTITICKETING will process it with the highest level of protection.

A form field built for the purpose is not "exceptional circumstances". Shipping the feature live
would put the platform in the position of deliberately collecting, through a mechanism it built,
data its own published policy says it does not collect.

Fixing the policy is not free. A Policy Version is "one published edition… preserved exactly as
shown", and publishing a new one "makes every Customer unaccepted again". A clause added for
t-shirt sizes re-gates the entire customer base at their next sign-in or checkout, and the wording
of a legal artifact whose SHA-256 is quoted in audits is not something to draft in passing.

## Decision

**The mechanism merges; collection does not begin.** Schema, minting, question authoring, the
checkout form, the Answer Link page, the export sheet and the tests all land. The question-authoring
surface stays behind a flag, and the Answer Reminder job ships paused, as the Follow Digest's
schedulers did.

**No Answer is collected until the flag flips**, and the flag flips only once a Policy Version
describing this collection is published — batched with any other pending policy edits so the
re-acceptance is paid for once.

**The policy clause is drafted for counsel, not authored here.** It must cover Organization-defined
questions, the Organization's role as the party that chose to ask, and personal data supplied by a
buyer about somebody else.

**An Organization is warned at authoring time** about what it is asking for.

## Considered options

- **Publish the Policy Version first, then ship.** The correct order, and the one this ADR does not
  take. Rejected on sequencing rather than principle: it would hold a finished feature behind a
  legal drafting cycle, and the flag achieves the same protection — no Answers exist to be governed
  until it is flipped.
- **Ship live and amend the policy later.** Rejected: it is the same plan without the property that
  makes it safe. The entire exposure comes from collected Answers, and a flag is the difference
  between a tracked gap and a live one.
- **Forbid sensitive questions in the authoring UI.** Unenforceable. A long-text question accepts
  anything, and "any allergies?" in a free-text box is the motivating case.
- **Restrict Ticket Questions to a curated, non-sensitive set** — sizes, meal choice, and nothing
  else. Rejected because it does not survive contact with real Organizations, who would use "other"
  and free text to ask what they were going to ask anyway.

## Consequences

**This is a tracked gap, deliberately taken.** Between merge and flag-flip, the repository contains
a working collection mechanism the published policy does not describe. Nothing collects, so nothing
is at risk — but anyone flipping the flag before the Policy Version publishes is taking a decision
this ADR was written to prevent.

**Flipping the flag re-gates every Customer.** The Policy Version that unblocks this makes every
Customer unaccepted, and they will re-accept at their next sign-in or checkout. That cost is why the
clause should ride with other policy changes rather than travel alone.

**Answers become another place personal data lives.** Sales Export sheets, Payment rows before the
purge, and Tickets after it. Any deletion or erasure request now has more surface to consider, and
those requests escalate rather than being handled inline.
