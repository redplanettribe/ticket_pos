# Ticket Assignment

Spec synthesized from a domain grilling session.
Canonical vocabulary: [CONTEXT.md](../CONTEXT.md).
Builds on [ADR 0043](./adr/0043-a-ticket-becomes-a-row-and-quantities-stay-the-truth.md) (a Ticket is a row) and
[ADR 0044](./adr/0044-the-holder-answers-by-link-and-the-platform-never-writes-to-them.md), which this
supersedes in part. Two new ADRs ride with it — see **Design records** below.

> **Status note.** Written while the Answer Link still existed. [ADR 0049](./adr/0049-only-the-holder-answers-and-the-answer-link-is-retired.md) has since retired it: there is no
> second door beside the Assignment Link, an `unassigned` or merely `assigned` Ticket is answerable
> only by Event Staff, and the buyer neither reads nor writes Answers on a Ticket they do not hold.
> The "two doors" material below, and **The Answer Link is not retired** under Open Questions, are
> superseded and kept as the record.

## Problem Statement

A Customer who buys four tickets holds four Tickets and no way to say who they are for.

Today the buyer's only route to the other three people is the **Answer Link**: an unauthenticated,
per-Ticket URL they copy off their sale page and forward by hand, through whatever channel they
already use. That link answers Ticket Questions and does nothing else. It carries no name, proves no
identity, and gives the person who holds it nothing — no record of the event, no way back to it, no
standing on the platform at all.

The consequences land on three parties:

- **The buyer** does the platform's distribution work by hand. They copy four links, work out which
  is which — the links disclose nothing, deliberately, so they are indistinguishable — and chase
  four people themselves. If a friend loses the link, the buyer has to find it again.
- **The holder** receives a bare URL from a friend. Nothing tells them the event is theirs, nothing
  reminds them it is coming, and if they answer a Ticket Question their answer can be silently
  overwritten by anyone else the link was forwarded to.
- **The Organization** knows how many tickets it sold and nothing about who is coming. Its
  Outstanding Answers list shows Tickets, not people. Asked "who is in the room", it can answer only
  with the buyer's name repeated four times.

The gap is named in the glossary. [CONTEXT.md](../CONTEXT.md) says of a Ticket: *"What it is not,
yet: it cannot be scanned, checked in, transferred, or handed to a named person."* This spec is the
"handed to a named person" half.

## Solution

The buyer assigns each Ticket to a **Holder** by email address, from their own sale page after the
purchase.

The platform mails that address an **Assignment Link**. Clicking it **accepts** the assignment. The
click is Proof of Email Ownership — the standard [ADR 0035](./adr/0035-guest-granted-consent-pends-until-the-email-is-proven.md)
already sets, *"clicking from the inbox being itself proof of ownership"* — so accepting mints or
matches a **Customer**, and the holder lands on a page where they give their first and last name and
answer that Ticket's Ticket Questions themselves.

What accepting does **not** do is move the Ticket Sale. The Sale, the Sale Confirmation, the money
and the Reversal Window all stay with the buyer, who may reassign any Ticket at any time. A Holder
is the named person a Ticket was handed to, not its owner. This is assignment, never transfer.

For the Organization, every accepted assignment turns a row into a person: the Event's guest list
gains a name and an email address beside each Ticket's Answers, and so does the Sales Export.

Three states per Ticket, and the whole feature is the walk between them:

| State | Meaning | Who may answer |
|---|---|---|
| `unassigned` | No address given. Today's behaviour, unchanged. | Buyer, Event Staff, anyone holding the Answer Link |
| `assigned` | An address has been mailed; nobody has accepted. | Buyer, Event Staff, anyone holding the Answer Link |
| `accepted` | A Holder proved the address and is a Customer. | Buyer, Event Staff, the Holder. **The Answer Link no longer opens.** |

## User Stories

### The buyer assigns

1. As a Customer who bought four tickets, I want to give an email address for each Ticket, so that my friends get their own tickets instead of a link I have to forward by hand.
2. As a Customer, I want to assign Tickets from my sale page after I have paid, so that I am not asked for three email addresses at the moment I am trying to check out.
3. As a Customer, I want to assign Tickets from the Customer Area as well as from a Confirmation Link, so that I can do it whether or not I am signed in.
4. As a Customer, I want to assign only some of my Tickets, so that a sale of four where I know two addresses is not blocked on the other two.
5. As a Customer, I want to assign a Ticket to my own address, so that a parent buying for their children can hold one and assign the rest.
6. As a Customer, I want to see which Ticket I assigned to which address, so that I can tell my four Tickets apart, which the Answer Links never let me do.
7. As a Customer, I want to see whether each assignment has been accepted, so that I know who has actually picked their ticket up.
8. As a Customer, I want to correct an address I mistyped, so that a wrong letter does not cost my friend their ticket.
9. As a Customer, I want to reassign a Ticket whose Holder has already accepted, so that a friend dropping out three weeks before the Event is something I can fix myself.
10. As a Customer, I want reassigning a Ticket to clear its Answers, so that the new Holder is not carrying the old one's dietary requirements.
11. As a Customer, I want to keep answering Ticket Questions on my own Tickets, so that assignment adds a route without taking one away.
12. As a Customer, I want to be told plainly that the address I type will be mailed and shown to the Organization, so that I know what I am doing to my friend before I do it.
13. As a Customer, I want nothing about assignment to block or delay my checkout, so that buying tickets stays as fast as it is today.

### The holder accepts

14. As someone a friend bought a ticket for, I want an email telling me I have a ticket, so that I find out without depending on my friend remembering to forward a link.
15. As a Holder, I want that email in my own language, so that I am not read the platform's default.
16. As a Holder, I want the mail to tell me which Event and which Ticket Type, so that I know what I am accepting.
17. As a Holder, I want to accept by clicking one link, so that I do not have to create an account, choose a password or copy a passcode.
18. As a Holder, I want to give my first and last name when I accept, so that the Organization knows who is coming.
19. As a Holder who is already a Customer, I want my name filled in already, so that I am not typing what the platform already knows.
20. As a Holder who is already a Customer, I want to correct that name, so that a typo captured at some door sale years ago is fixable.
21. As a Holder, I want to answer the Ticket Questions myself, so that my own t-shirt size is the one the Organization orders.
22. As a Holder, I want to change my Answers after accepting, so that I can fix a mistake any time before the Event starts.
23. As a Holder, I want my Answers to stand once I have accepted, so that somebody still holding a forwarded Answer Link cannot overwrite my size as a joke.
24. As a Holder, I want to see the Event in my Customer Area after accepting, so that I have a way back to it that does not depend on the mail.
25. As a Holder, I want to be told that my email address is shared with the Organization, so that I can decide not to accept.
26. As a Holder, I want to be asked for nothing beyond my name and the Ticket Questions, so that accepting a ticket does not turn into a registration form.
27. As a Holder, I want never to be asked for a Tax ID, so that a fact about the buyer's invoice is not demanded of a guest.
28. As a Holder, I want the page to show me the Event, the Ticket Type and the questions and nothing else, so that I am not shown what my friend paid.
29. As a Holder, I want to ignore the mail with no consequence, so that not wanting an account is an option.
30. As a Holder who ignored it, I want the Ticket to keep working the old way, so that my friend can still answer for me or forward me the Answer Link.

### The holder loses a ticket

31. As a Holder, I want to be told when I stop holding a Ticket, so that I do not turn up to an Event I no longer have a ticket to.
32. As a Holder, I want that mail whether the buyer reassigned it or the Sale was reversed, so that the platform is not talkative in one case and silent in the other.
33. As a Holder, I want the Event to leave my Customer Area when that happens, so that what I see reflects what I hold.
34. As a Holder, I want to stay a Customer with the data I gave, so that losing one ticket does not delete my account.
35. As a Holder, I want an Assignment Link for a Ticket I no longer hold to say so, so that a dead link reads as an explanation rather than a bug.
36. As a Holder, I want that page not to name the buyer, so that the platform's disclosure rules hold even in the error state.

### The Organization

37. As an Organizer, I want to see the name of each Ticket's Holder, so that I know who is coming rather than how many.
38. As an Organizer, I want to see each Holder's email address, so that I can reach the people attending my Event.
39. As an Organizer, I want to see each Ticket's assignment state, so that I can tell an unassigned Ticket from one that is waiting to be accepted.
40. As an Organizer, I want the Holder's name and email in the Sales Export beside that Ticket's Answers, so that "who is coming and what size are they" is one sheet.
41. As an Organizer, I want the guest list on the surface I already use for Outstanding Answers, so that there is one place to look at my Tickets and not two.
42. As an Organizer, I want to correct any Answer on any Ticket of my Event, so that a Holder who cannot be reached is not a dead end.
43. As an Organizer, I want assignment to work on imported sales, so that a school buying twenty tickets by transfer can still name twenty children.
44. As an Organizer, I want my Event's Tickets Sold to be unaffected by assignment, so that a published figure does not move because people did or did not accept.

### Chasing answers

45. As a Holder, I want the Answer Reminder to come to me once I have accepted, so that I am asked about my own t-shirt size.
46. As a Customer, I want to stop being reminded about Answers only my friends can give, so that the platform stops nagging me about things I do not know.
47. As a Customer, I want to keep receiving the reminder for Tickets nobody has accepted, so that the Tickets that are still mine to chase are still chased.
48. As a Holder, I want reminders rationed per Ticket, so that a four-ticket sale can chase two of us without mailing either twice.
49. As a Holder, I want the reminder to stop once the Event has started, so that I am not chased about an Event that has happened.
50. As a Holder, I want the reminder regardless of Marketing Consent, so that a transactional message about my own ticket is not gated behind a marketing switch.

### Privacy, retention and abuse

51. As a person whose address a friend typed, I want the platform to hold it only as long as it is useful, so that I am not on a list forever because I ignored one mail.
52. As a Platform Operator, I want an address that is never accepted purged when the Event starts, so that unconsented contact details have a guaranteed end.
53. As a Platform Operator, I want that purge to take the address and leave the Ticket and its Answers, so that the record of the sale survives the deletion of the liability.
54. As a Platform Operator, I want the purge to be pausable, so that a purge suspected of deleting too much can be stopped before more rows are gone.
55. As a Platform Operator, I want no holder addresses collected at checkout, so that an abandoned Payment leaves no third party's contact details behind.
56. As a Platform Operator, I want a hard cap on how many mails one Ticket can send, so that free reassignment is not an unlimited mailer.
57. As a Platform Operator, I want a rate limit per buyer, so that a Free Ticket Type with no Purchase Limit cannot be scripted into a bulk sender.
58. As a Platform Operator, I want assignment behind its own flag, so that killing it does not take Ticket Questions down with it.
59. As a Platform Operator, I want no assignment to be possible until a Policy Version describes this collection, so that the platform never processes data its published policy denies processing.
60. As a Platform Operator, I want that policy clause batched with the one Ticket Questions is waiting on, so that Customers are re-gated once rather than twice.
61. As a Holder, I want the Assignment Link to expire when the Event starts, so that a URL in an old inbox does not stay live forever.
62. As a Customer, I want the Assignment Link never shown to me, so that the proof it carries means something.

## Implementation Decisions

### Vocabulary

Four new glossary terms, and the word **confirm** is deliberately avoided — it already carries four
meanings here (Sale Confirmation, Confirmation Link, Consent Confirmation Link, `confirmation_ref`),
and the new token is the only one that mints an identity.

| Term | Meaning |
|---|---|
| **Ticket Assignment** | The buyer's act of naming an email address for one Ticket, and the record of it. |
| **Holder** | The person a Ticket was assigned to. Capitalised as a role, replacing ADR 0044's lowercase "holder". |
| **Assignment Link** | The signed token mailed to the address, whose click accepts. The fourth signed link, and the only one that proves an identity. |
| **accept** | What the Holder does. States are `unassigned` / `assigned` / `accepted`. |

`CONTEXT.md` edits: new entries for the four terms above; **Ticket** loses "handed to a named person"
from its *what it is not, yet* list and keeps "transferred"; **Answer Link** gains the fact that it
stops opening once a Ticket is accepted; **Answer Reminder** loses "addressed to the buyer because
there is nobody else to address"; **Sales Export** and **Customer** gain the holder columns and the
assignment-minted route to a Customer record.

### Model

- **Assignment is fields on the Ticket, not a new entity.** A Ticket gains a holder address, an
  optional Customer reference, and the timestamps that distinguish the three states. There is no
  assignment history table: like an Answer, the platform keeps the current fact and when it changed,
  not who changed it or what it was before.
- **The Ticket Sale is untouched.** It stays whole with the buyer. A Sale Reversal remains
  whole-Sale; nothing about assignment can reverse, split or partially void a Sale.
- **Tickets Sold is untouched.** It keeps summing Ticket Sale Line quantities, per ADR 0043.
  Assignment changes no figure, no capacity and no Purchase Limit.
- **Accepting writes a Customer.** Match on normalised email, create if absent, and mark Verified —
  the click is Proof of Email Ownership. The Holder's first and last name are written to the
  Customer as their current asserted name, stored separately per ADR 0005. No Tax ID is ever asked
  of a Holder; it is a fact of the sale's buyer.
- **Reassignment clears that Ticket's Answers** back to Outstanding. An Answer is a fact about a
  person, and inheriting one across Holders would attribute the wrong person's dietary requirement.
- **Marketing Consent is not granted by accepting.** A Customer minted this way has consented to
  nothing beyond holding a ticket.

### Where assignment happens

- **After purchase only.** Never on the checkout form. Nothing is held on the Payment, so no new
  purge surface is created for abandoned checkouts and no third party's address is stored for a sale
  that never happened.
- **Buyer surfaces**: the Confirmation Link page and the Customer Area, both of which already list a
  Sale's Tickets with their questions and a per-Ticket copy-link button.
- **Channels**: `online` and `import`, both of which give the buyer a Confirmation Link page.
  `in_person` door sales have no such surface and are excluded.

### The Assignment Link

- **A distinct signed token, never shown to the buyer.** This is the security property of the whole
  feature: the Answer Link is copyable from the buyer's own sale page, so reusing it would mean the
  click proves nothing and the Verified Customer minted from it would be a fiction. The Assignment
  Link is delivered only to the address, and must never appear on a buyer surface or in an API
  response to the buyer.
- **Scope is exactly this Ticket, plus the Holder's name.** It does not open the Ticket Sale, other
  Tickets, other sales, consents or Follows.
- **Discloses**: Event, Ticket Type, the Ticket Questions, and — after the click — the Customer's
  existing name for prefill. **Never**: buyer name or email, price, Tax ID, `confirmation_ref`, or
  the Sale's other Tickets. ADR 0044's disclosure rule is carried over unchanged.
- **Prefill only after the click.** Showing a known Customer's name on a page reachable without the
  click would make the endpoint an oracle for whether an address is registered, which
  [ADR 0035](./adr/0035-guest-granted-consent-pends-until-the-email-is-proven.md) is explicit about
  avoiding.
- **Expires when the Event starts**, read in the Event's timezone, and stops opening when the Ticket
  Sale is reversed or the Ticket is reassigned — the same lifecycle the Answer Link already has.

### The Answer Link, after assignment (superseded by ADR 0049 — the Answer Link is retired)

- **Both doors open while `assigned`.** An Assignment Link that is never accepted degrades to
  exactly today's behaviour, so a mistyped address bricks nothing.
- **Accepting closes the Answer Link** for that Ticket. This is what makes the click mean something.
- **The buyer and Event Staff keep their routes throughout.** ADR 0044's three-party rule holds: a
  wrong Answer stays fixable by the buyer and by Event Staff.

### Mail

Two new mails, both transactional, both written in the recipient's Mail Locale, both sent on the
transactional sending domain.

- **The Assignment mail**: names the Event and the Ticket Type, carries the Assignment Link, states
  that the address was supplied by the person who bought the ticket, and states that accepting
  discloses the address to the Organization. Names no buyer.
- **The No Longer Holding mail**: sent to an **accepted** Holder when the Ticket is reassigned or its
  Sale is reversed. One rule, one mail, no cause given, no buyer named. Never sent to an `assigned`
  Holder who never accepted — they were never told they had it.

### Answer Reminder

The existing swept job changes recipient, per user story 45–49:

- Holder if the Ticket is `accepted`; the buyer otherwise. A Sale with a mix produces one mail to the
  buyer covering the Tickets still his to chase, and one to each accepted Holder about their own.
- **Rationing moves from per-Ticket-Sale to per-Ticket.** The existing cap — at most once per 7 days,
  at most 2, silent once the Event has started — is otherwise preserved. This change lands in this
  work, not as a follow-up.
- Still swept rather than edit-triggered, so an Organization drafting questions cannot mail the same
  people repeatedly.

### Retention

- **An address that is never accepted is purged when the Event starts.** Address only: the Ticket,
  its Answers, and the fact that it was assigned all survive.
- **A new internal purge job**, mirroring the existing Abandoned Answer Purge in shape and in
  operational discipline: it ships paused, and its switch exists so the job can be stopped before
  more rows are gone.
- **An accepted Holder is an ordinary Customer** and falls under ordinary Customer retention. Their
  Answers survive a reversal, consistent with a reversed Ticket Sale never being deleted.

### Abuse

- **A hard per-Ticket cap on assignment mails**, with a small resend allowance for a corrected
  address. The hole is free reassignment, not ticket count: without a cap, one Ticket can be
  re-pointed at fresh addresses indefinitely.
- **A rate limit per buyer.** Purchase Limit is *"a deterrent against taking too many rather than a
  defence against someone minting identities"*, and is unset on most Ticket Types — so a Free Ticket
  Type is otherwise a bulk mailer.

### API surface

All under existing namespaces; no new authentication scheme.

- **Buyer**: assign and reassign one Ticket, under the existing
  `/api/v1/customer/ticket-sales/{ticketSaleId}/tickets/...` namespace, alongside the existing
  per-Ticket answer write. Scoped to the buyer's own Sale; asking about another Sale is answered as
  if it did not exist.
- **Public**: an `assignment-link` pair mirroring the existing `answer-link` pair — one call to open
  and accept, one to write the Holder's name, and question writes scoped to the token. The token
  names the Ticket; no Ticket id appears in a path, which would be a second unsigned way to say
  which Ticket this is.
- **Staff**: the existing `GET /api/v1/staff/events/{id}/outstanding-answers` read is **extended into
  the guest list** rather than duplicated. It already walks every Ticket of an Event, and a second
  near-identical endpoint is the alternative. It gains holder name, holder email and assignment
  state per Ticket.
- **Internal**: one purge job route, in the same family as the existing purge and sweep jobs.

### Shipping

- **`TICKET_ASSIGNMENT_ENABLED`**, separate from `TICKET_QUESTIONS_ENABLED`. The two features are
  genuinely separable — questions are already merged and work without assignment, and a guest list is
  worth having on a Ticket Type that asks nothing — and separate flags mean assignment can be killed
  without taking questions dark.
- **No assignment is possible until the flag opens**, and it opens only once a Policy Version
  describing this collection publishes. The clause is **batched** with the one ADR 0045 is already
  waiting on, so the re-acceptance of every Customer is paid for once.
- **The purge job ships paused**, as the Answer Reminder and Abandoned Answer Purge did.
- **The policy clause is drafted for counsel, not authored in the repo.** It must cover three things
  ADR 0045's clause does not: the platform mailing an address supplied by a third party, a Customer
  record minted from that click, and disclosure of the Holder's address to the Organization as a
  separate controller.

### Design records

Two ADRs ride with this spec. Numbers are assigned at authoring time — `ls docs/adr` first, as
numbering has collided before.

1. **A Ticket is assigned to a Holder, who accepts by mail.** Supersedes
   [ADR 0044](./adr/0044-the-holder-answers-by-link-and-the-platform-never-writes-to-them.md) in part:
   its *"the platform never emails a holder… asks for no holder addresses and stores none"* and its
   Answer Reminder rationale. Records why the option 0044 rejected is now taken — the accept step is
   the capture moment 0044 said the buyer-answers-everything design lacked — and why the Ticket Sale
   nonetheless stays whole with the buyer.
2. **The Organization sees the Holder's address.** Recorded separately because it is separable and
   because it is the contested one: it hands every Organization a list of addresses belonging to
   people who never chose to give it to them, mailable outside anything the platform can govern. The
   decision was taken deliberately, with that cost stated, on the grounds that an Organizer needs to
   reach the people attending its Event.

ADR 0044 gains a superseded-in-part note pointing at the first of these.

## Testing Decisions

**What makes a good test here**: it asserts what a buyer, a Holder, an Organizer or an operator can
observe — an HTTP status and envelope, a mail captured by the harness sender, a row visible through a
later read, a cell in an export sheet. It does not assert which service was called, what SQL ran, or
in what order layers spoke.

### Primary seam: `backend/integration/` HTTP tests

The default layer per [testing.md](./testing.md), and sufficient for this entire feature: the
harness already provides a real Postgres, a capture email sender and a fixed clock, which is exactly
the apparatus assignment needs. Tests run serially; no `t.Parallel()`.

Scenarios, as new files beside their prior art:

- **Assigning** — assign, reassign, self-assign, partial assignment of a multi-Ticket Sale, another
  Customer's Sale answered as if it did not exist, `in_person` refused. Prior art: `tickets_test.go`,
  `buyer_answers_test.go`.
- **Accepting** — the mail is captured and carries the link; the click mints a Verified Customer;
  the click matches an existing Customer and prefills their name; the name writes through; the
  Holder answers; the Ticket Sale, other Tickets, price and `confirmation_ref` are absent from every
  response. Prior art: `answer_link_test.go`, `confirmation_link_test.go`, `customer_profile_test.go`.
- **The two doors** — the Answer Link opens while `assigned` and stops opening once `accepted`; the
  buyer and Event Staff keep writing throughout. Prior art: `answer_link_test.go`,
  `ticket_answers_test.go`.
- **Losing a Ticket** — reassignment clears Answers and mails the displaced accepted Holder;
  reversal mails every accepted Holder; an `assigned`-but-never-accepted address is mailed neither
  time; the link then reports the Ticket as gone without naming the buyer; the Customer record
  survives. Prior art: `customer_sale_reversal_test.go`, `answer_link_test.go`.
- **Reminders** — holder-addressed once accepted, buyer-addressed otherwise, a mixed Sale producing
  both, per-Ticket rationing, silence after Event start. Prior art: `answer_reminder_test.go`,
  which the rationing change also edits.
- **Retention** — the purge takes the address at Event start and leaves the Ticket and its Answers;
  it takes nothing from an accepted Ticket. Prior art: `checkout_answers_purge_test.go`, driven the
  same way through an internal job route with the fixed clock advanced.
- **Abuse** — the per-Ticket send cap and the per-buyer rate limit, both asserted through the
  captured sender.
- **Guest list and export** — the extended staff read carries holder name, address and state; the
  export sheet gains the columns beside the Answers. Prior art: `outstanding_answers_test.go`,
  `sales_export_answers_test.go`.
- **The flag** — with `TICKET_ASSIGNMENT_ENABLED` closed, no assignment is accepted, no mail is
  sent, and Ticket Questions continue to work. This is the test that proves the two flags are
  independent.

### Reused secondary seams

- **Mail content and Mail Locale** — `backend/internal/platform/`, prior art
  `email_answerreminder_test.go` and `email_copy_test.go`, for the two new mails: that they name the
  Event and Ticket Type, that they name no buyer, that they carry the disclosure sentence, and that
  they are written in the recipient's Mail Locale.
- **Storefront pure functions** — unit tests in `apps/storefront/lib/`, prior art
  `answer-link.test.ts` and `buyer-answers.test.ts`, for assignment state derivation and form
  shaping. Note the ICU caveat that already bit this repo: avoid asserting on invisible codepoints
  in formatted dates, since local and CI ICU versions differ.

### Repository-layer tests

None expected. The exception in [testing.md](./testing.md) is for concurrency and locking only, and
nothing here is a race worth a repository test — accepting twice is idempotent and settled by the
same transaction that mints the Customer.

### E2E

None. [testing.md](./testing.md) forbids duplicating integration scenarios in Playwright, and this
feature crosses no runtime boundary that the existing smoke suites do not already cover.

## Out of Scope

- **Transfer.** A Holder never becomes the party of record for a Ticket. The Ticket Sale, the Sale
  Confirmation, the money and the Reversal Window stay with the buyer, and a Sale Reversal stays
  whole-Sale.
- **Check-in, QR codes, scanning.** Assignment names a person; it does not admit them. A Ticket
  still cannot be scanned.
- **Assignment at checkout.** No holder address is ever held on a Payment.
- **Assignment on `in_person` sales.** No buyer surface exists to do it from.
- **The platform collecting a Holder's phone number, Tax ID, or any fact beyond a name and the
  Ticket Questions.**
- **Organizations mailing Holders through the platform.** The address is disclosed; no sending
  surface is built. What an Organization does with an exported address is outside the platform.
- **A Holder declining an assignment explicitly.** Ignoring the mail is the decline, and the address
  is purged at Event start.
- **Assignment history.** The current Holder and when it last changed, not who changed it or what it
  was before — matching how an Answer is kept.
- **Per-Locale authoring of any assignment copy.** Chrome follows the Locale; the Organization's
  Ticket Questions still read as coined.
- **The Privacy Policy clause itself.** Drafted for counsel. It blocks the flag, not the merge.
- **Flipping either flag.** Both stay closed on merge.

## Further Notes

**This reverses a standing decision, deliberately.** ADR 0044 rejected exactly this design —
*"Collect holders' email addresses at checkout and mail them the links"* — on the grounds that it is
*"the same third-party collection problem in a new coat"*. Two things changed. First, the accept step
supplies the capture moment 0044 said was missing: the Holder proves their own address, sees notice,
and answers for themselves, rather than the buyer guessing on their behalf. Second, nothing is
collected at checkout, so no address is held for a sale that never completes. What does not change is
that the first mail goes to a person who never came here, at an address supplied by someone with no
authority to supply it. That cost is real, is priced, and is the reason for the purge, the cap and
the flag.

**The disclosure decision is the contested one.** Withholding the Holder's address from the
Organization was recommended and declined. It is recorded in its own ADR so that a future reader sees
it was a decision rather than an oversight, and so that it can be revisited without unpicking the
rest of the feature.

**The Answer Link is not retired.** *(Reversed by ADR 0049, which retires it.)* It remains the route for every `unassigned` Ticket, which will be
most of them for a long time, and the fallback whenever an assignment is not accepted. Assignment
adds a stronger door beside it; it does not replace it.

**Sequencing.** The policy clause blocking this flag is the same one blocking Ticket Questions, and
it grows here. Counsel is most likely to argue about the disclosure language, which is the clause
Ticket Questions does not need — so if that drafting stalls, splitting the clause and shipping
questions first is the escape hatch, at the cost of re-gating Customers twice.
