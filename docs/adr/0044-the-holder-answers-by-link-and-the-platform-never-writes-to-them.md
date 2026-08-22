# The holder answers by link, and the platform never writes to them

**The Answer Link described here is retired by [ADR 0049](./0049-only-the-holder-answers-and-the-answer-link-is-retired.md), along with the rule that three parties may supply an Answer; its disclosure rule survives in the Assignment Link.**

**Superseded in part by [ADR 0046](./0046-a-ticket-is-assigned-to-a-holder-who-accepts-by-mail.md).**
Two things below no longer hold. The rule that *"the platform never emails a holder… asks for no
holder addresses and stores none"* falls: a buyer may now assign a Ticket to a Holder by email
address after the purchase, and the platform mails that address an Assignment Link whose click
accepts. With it falls the Answer Reminder's rationale — that mail is addressed to the buyer *"because
there is nobody else to address"*, and once a Holder has accepted there is. The option this ADR
rejected, collecting holder addresses at checkout, is still rejected; what changed is that assignment
happens after the money has moved and that the accept click supplies the capture moment this ADR said
the design lacked. Everything else here stands and is still load-bearing: the Answer belongs to the
Ticket, three parties may supply it, "required" means outstanding rather than blocking, the Answer
Link discloses nothing and transfers nothing, and checkout-time Answers are still held on the Payment
and purged. See also
[ADR 0047](./0047-the-organization-sees-the-holders-address.md) for what the Organization is shown.

## Context

Ticket Questions ask for facts about the person who will hold a ticket. The obvious design — put the
questions on the checkout form and let the buyer fill them in — assumes the buyer knows the answers.
Often they do not. Somebody buying four tickets for friends does not know three of those t-shirt
sizes, and a form with three blanks is not a way to find out.

It also puts the platform in an awkward position. Every answer the buyer types about somebody else
is third-party personal data, supplied by a person with no authority to supply it, about a person who
has been shown no notice. In the dietary case that data is health data. The Privacy Policy's
consent apparatus is built around a person answering for themselves at a capture moment, and the
buyer-answers-everything design has no such moment for three of the four people involved.

## Decision

**An Answer belongs to the Ticket, and three parties may supply it**: whoever opens the Ticket's
**Answer Link**, the buyer, and Event Staff. Not exclusive — a mother buying for her children
answers all four herself.

**The Answer Link answers; it does not transfer.** It opens one Ticket's questions and nothing else.
The Ticket Sale, the Sale Confirmation, the Customer Area entry and the Reversal Window all stay with
the buyer, and holding the link makes nobody a Customer.

**It discloses nothing about the purchase**: the Event, the Ticket Type and the questions, never the
buyer's name or email, the price, the Tax ID, the confirmation reference, or the Sale's other
Tickets. Expires when the Event starts; stops opening when the Sale is reversed.

**It requires no proof of identity.** Anyone holding the link may answer, and may overwrite what the
buyer guessed.

**The platform never emails a holder.** It asks for no holder addresses and stores none. The buyer
receives one link to their sale's page and distributes the per-Ticket links themselves, by whatever
channel they already use.

**"Required" therefore means outstanding, not blocking.** No checkout, door sale or Sale Import is
ever refused for want of an Answer. What a required Ticket Question produces is an **Outstanding
Answer** the Organization can see and chase.

**Answers are still collected at checkout**, as a skippable form: held on the Payment keyed by
`(payment_line, index)` exactly as `payment_lines` holds the price snapshot, and written onto the
minted Tickets in order when the sale commits. The buyer fills in what they know; the links cover the
rest.

## Considered options

- **The buyer answers everything, at checkout, and that is the whole feature.** Much smaller. Rejected
  because it produces wrong data by design — the buyer guesses, or leaves blanks — and because it
  makes the platform a collector of third-party health data with no notice given to the third party.
- **Collect holders' email addresses at checkout and mail them the links.** The tidier product,
  and the reason it was rejected is the reason the whole design exists: asking a buyer for three
  friends' addresses so the platform can write to them is the same third-party collection problem in
  a new coat, and it would put unconsented addresses in the database. Handing the buyer links to
  forward collects nothing.
- **Require a One-time Passcode before answering.** Would stop a prankster in a group chat setting
  somebody's size to XS. Rejected because it is heavy friction for a t-shirt size and because
  proving the email means collecting the email, which is the thing being avoided. A wrong Answer is
  cheap and both the buyer and Event Staff can fix it.
- **Skip the checkout form entirely** and start all answering on the confirmation page. Genuinely
  simpler — no answers on the Payment, no purge job, no form between the buyer and the pay button.
  Rejected by product judgment: the buyer who does know the answers should be able to give them while
  they are thinking about it. The timing is kept as a property of the Ticket Question so it can
  become an Organization's choice later without migrating any Answer.
- **Block checkout until required questions are answered.** The conventional meaning of required.
  Impossible here once the buyer is not assumed to know, and never enforceable on `in_person` or
  `import` anyway — where a blocked sale at a door with a queue behind it is a lost sale.

## Consequences

**A late-added Ticket Question reaches a holder only if the buyer forwards it.** The platform can
write to the buyer and no one else. This is the accepted cost of collecting no holder addresses, and
it is why the Answer Reminder is addressed to buyers.

> Superseded in part by [ADR 0046](./0046-a-ticket-is-assigned-to-a-holder-who-accepts-by-mail.md).
> This still holds for a Ticket that is `unassigned` or `assigned` — most of them, for a long time —
> and it is exactly where it stops holding. Once a Holder has **accepted**, the platform holds an
> address that its owner proved from their own inbox, and the Answer Reminder is addressed to them
> about their own Ticket; a Sale with a mix produces one mail to the buyer covering the Tickets still
> theirs to chase and one to each Holder. The rationing moved from the Ticket Sale to the **Ticket**
> at the same time, because a per-Sale allowance cannot ration several Holders at all.

**Answer Links are unauthenticated URLs that answer for a person.** Their safety rests entirely on
disclosing nothing and on expiring at Event start. Anyone widening what the page shows — adding the
buyer's name "for context", or the confirmation reference "to help support" — is reversing this
decision.

**Answers exist on Payments that never become Sales.** Unlike the Tax ID, phone and consent answers
already held there, these may be health data with no purpose once the checkout is abandoned, so they
are purged 30 days after a Payment that is not approved. Never on `expired` alone, which can revive
into an approved sale.

**Three signed-link concepts now travel in the same mail flow** — Confirmation Link, Consent
Confirmation Link, Answer Link. They are separately named on purpose and none opens what the others
do.
