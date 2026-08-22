# A Ticket is assigned to a Holder, who accepts by mail

## Context

[ADR 0044](./0044-the-holder-answers-by-link-and-the-platform-never-writes-to-them.md) settled how a
Ticket Question reaches the person a ticket is for: the buyer receives a per-Ticket **Answer Link**
and forwards it by hand. That decision rests on one property — *"The platform never emails a holder.
It asks for no holder addresses and stores none."* Collecting nothing was the point, and everything
else in that ADR follows from it: the link proves no identity, discloses nothing, and the Answer
Reminder goes to the buyer *"because there is nobody else to address"*.

A year of the glossary saying so has made the cost legible. A Customer who buys four tickets holds
four Tickets and no way to say who they are for. They copy four links that are deliberately
indistinguishable, work out which is which, and chase four people themselves. The person who ends up
holding one receives a bare URL from a friend: nothing tells them the Event is theirs, nothing
reminds them it is coming, and anything they answer can be silently overwritten by anyone else the
link was forwarded to. The Organization knows how many tickets it sold and nothing about who is
coming; asked "who is in the room", it can answer only with the buyer's name repeated four times.
The glossary names the gap in the Ticket entry itself — *"it cannot be scanned, checked in,
transferred, or handed to a named person"*.

ADR 0044 considered and rejected the obvious fix: *"Collect holders' email addresses at checkout and
mail them the links… the same third-party collection problem in a new coat"*. That rejection was
right about the design it was rejecting. Two things have since changed it. First, an accept step
supplies the capture moment 0044 said the buyer-answers-everything design lacked for three of the
four people involved: the person proves their own address, is shown notice, and answers for
themselves, instead of the buyer guessing on their behalf. Second, nothing need be collected at
checkout at all — assignment can happen after the money has moved, so no third party's address is
ever held against a sale that never completes.

What has not changed is that the first mail goes to somebody who never came here, at an address
supplied by a person with no authority to supply it. That is the price of the feature, and this ADR
is where it is paid.

## Decision

**A buyer assigns a Ticket to a Holder by email address, after the purchase and never at checkout.**
The assigning surfaces are the ones that already list a Sale's Tickets — the Confirmation Link page
and the Customer Area — on the `online` and `import` channels. `in_person` door sales are excluded
because there is no buyer surface to assign from. Keeping it off the checkout form is not a UX
preference: it is what guarantees an abandoned Payment leaves no third party's contact details
behind, and it is the specific respect in which this is not the design ADR 0044 rejected.

**The platform mails that address an Assignment Link, and clicking it accepts.** The click is Proof
of Email Ownership by the standard [ADR 0035](./0035-guest-granted-consent-pends-until-the-email-is-proven.md)
already set — *"clicking from the inbox being itself proof of ownership"* — so accepting mints or
matches a Customer on the normalised email and marks them Verified. The Holder then gives their
first and last name and answers that Ticket's Ticket Questions themselves. Nothing else is asked of
them: no password, no passcode, no phone number, and never a Tax ID, which is a fact about the
sale's buyer.

**The Assignment Link is a fourth signed token, delivered only to the address and never shown to the
buyer.** This is the security property the whole feature rests on. The Answer Link is copyable off
the buyer's own sale page, so reusing it here would mean the click proves nothing and the Verified
Customer minted from it is a fiction. An Assignment Link appearing on a buyer surface or in a
response to the buyer is therefore a defect of the same severity as leaking the token itself. Its
scope is exactly one Ticket plus the Holder's name; ADR 0044's disclosure rule carries over
unchanged — Event, Ticket Type and questions, never the buyer, the price, the Tax ID, the
confirmation reference or the Sale's other Tickets — and a known Customer's name is prefilled only
*after* the click, since showing it before would make the page an oracle for whether an address is
registered, which ADR 0035 is explicit about avoiding. It expires when the Event starts, read in the
Event's timezone, and stops opening when the Sale is reversed or the Ticket is reassigned.

**Assignment is not transfer, and the Ticket Sale stays whole with the buyer.** The Sale, the Sale
Confirmation, the money and the Reversal Window do not move, a Sale Reversal remains whole-Sale, and
the buyer may reassign any Ticket at any time — including one already accepted. A Holder is the
named person a Ticket was handed to, not its owner. Nothing about assignment touches Tickets Sold,
capacity or a Purchase Limit; per [ADR 0043](./0043-a-ticket-becomes-a-row-and-quantities-stay-the-truth.md)
those still read off Ticket Sale Line quantities, and a published figure must not move because
people did or did not accept.

**Both doors stay open until somebody accepts; accepting closes the Answer Link.** While a Ticket is
`assigned` and unaccepted, its Answer Link works exactly as it does today, so a mistyped address
bricks nothing and an ignored mail costs the buyer nothing. Once a Holder has accepted, that
Ticket's Answer Link stops opening — which is what makes the click mean something, and what stops
somebody still holding a forwarded link changing a Holder's size as a joke. ADR 0044's three-party
rule otherwise survives intact: the buyer and Event Staff keep their routes throughout, so a wrong
Answer is still fixable by two parties who can be reached.

**Reassignment clears that Ticket's Answers back to Outstanding.** An Answer is a fact about a
person. Inheriting one across Holders would attribute the previous person's dietary requirement to
the new one, which is worse than a blank cell. Accepting grants no Marketing Consent either: a
Customer minted this way has consented to nothing beyond holding a ticket.

**The Answer Reminder follows the Answer.** It goes to the Holder once a Ticket is `accepted` and to
the buyer otherwise, so a mixed Sale produces one mail to the buyer covering the Tickets still his
to chase and one to each accepted Holder about their own. Rationing moves from per-Ticket-Sale to
per-Ticket; the existing cap, the sweep and the silence after Event start are otherwise preserved.
This retires ADR 0044's rationale for that mail — there is now somebody else to address.

**An address that is never accepted is purged when the Event starts.** The address only: the Ticket,
its Answers and the fact that it was assigned all survive, so the record of the sale outlives the
deletion of the liability. A per-Ticket cap on sends and a per-buyer rate limit keep free
reassignment from being an unlimited mailer, particularly on a Free Ticket Type with no Purchase
Limit. Assignment sits behind its own flag, separate from the Ticket Questions flag, so killing one
does not take the other down; and per
[ADR 0045](./0045-ticket-questions-ship-dark-until-the-privacy-policy-describes-them.md) neither
flips until a Policy Version describes this collection.

**ADR 0044 is superseded in part, not replaced.** What falls is its no-holder-addresses rule and the
Answer Reminder rationale built on it. Everything else in that ADR stands and is load-bearing here:
the Answer belongs to the Ticket, three parties may supply it, "required" means outstanding rather
than blocking, the Answer Link discloses nothing and transfers nothing, and checkout-time answers
are still held on the Payment and purged. The Answer Link is not retired. It remains the route for
every `unassigned` Ticket — which will be most of them for a long time — and the fallback whenever
an assignment is not accepted. Assignment adds a stronger door beside it.

## Considered options

- **Leave it as ADR 0044 left it and do nothing.** Collects the least, and is still the right answer
  for any Organization that only wants a headcount. Rejected because the cost is not really borne by
  the platform: it is borne by a buyer doing manual distribution work, by a holder who is never told
  the Event is theirs, and by an Organizer who cannot answer "who is coming". Collecting nothing is a
  virtue only up to the point where it stops the product doing its job.
- **Collect holder addresses at checkout, as ADR 0044 framed the option.** Still rejected, but now
  for a narrower reason than that ADR gave. Asking for three friends' addresses in the middle of a
  checkout slows the one flow that must stay fast, and — decisively — it puts unconsented third-party
  addresses on Payments that are abandoned, creating a purge surface for data belonging to people
  whose friend never even completed a purchase. Assigning after the sale keeps the collection tied to
  a ticket that actually exists.
- **Transfer the Ticket to the Holder rather than assign it.** The thing users will ask for, and the
  thing this deliberately is not. Rejected because the Ticket Sale is the unit the money, the Sale
  Confirmation, the Tax ID and the Reversal Window all hang off, and a whole-Sale Reversal cannot be
  reconciled with parts of the Sale having walked off to other people. Transfer is a different
  feature with a refund story attached; it is not this one wearing a different word.
- **Require a One-time Passcode instead of a link click.** Stronger proof, and the platform already
  has the apparatus. Rejected because the click already carries the proof this decision needs, by the
  same reasoning ADR 0035 used, and because friction here is not free: the Holder is a person doing
  the platform a favour by answering a t-shirt size, and a passcode step converts a favour into a
  registration.
- **Let the Holder decline explicitly.** Rejected as a surface nobody needs. Ignoring the mail is the
  decline, it costs the Holder nothing, and the address is purged at Event start regardless — so an
  explicit decline would buy only a slightly earlier deletion, at the cost of a page, a token state
  and a mail nobody wants to receive.
- **Keep an assignment history table.** Rejected for the reason an Answer keeps no author: the
  platform records the current fact and when it changed, not who changed it or what it was before.
  Which of four friends was assigned a Ticket first is not a dispute this product adjudicates, and a
  history table would be a permanent store of addresses the purge is meant to end.
- **Retire the Answer Link once assignment ships.** Tempting — one door is simpler than two. Rejected
  because it makes an unassigned Ticket unanswerable and makes a mistyped address fatal. The
  degradation path is the reason a buyer can risk typing an address at all.

## Consequences

**The platform now writes to people who never came here.** The Assignment mail goes to an address
supplied by somebody with no authority to supply it, and no design here removes that. It is why the
mail states plainly who supplied the address and what accepting discloses, why it names no buyer, why
there is a per-Ticket send cap and a per-buyer rate limit, and why an unaccepted address has a
guaranteed end at Event start. Anyone loosening the cap, the rate limit or the purge is spending
something this ADR priced.

**A holder address is a new place personal data lives**, alongside the Answers
[ADR 0045](./0045-ticket-questions-ship-dark-until-the-privacy-policy-describes-them.md) already
counted. Deletion and erasure requests have more surface to consider, and those still escalate rather
than being handled inline. The new purge job ships paused and stays stoppable, in the same
operational discipline as the Abandoned Answer Purge, because a purge suspected of deleting too much
must be halted before more rows are gone.

**Four signed-link concepts now travel in the same mail flow** — Confirmation Link, Consent
Confirmation Link, Answer Link, Assignment Link. ADR 0044 noted three and warned they must stay
separately named; the fourth is the only one that mints an identity, which makes conflating it with
any of the others worse than a naming slip. The word **confirm** is deliberately not used for it: the
glossary already carries four confirmations.

**Reminders get louder.** A four-ticket sale that used to produce at most one mail can now produce
one to the buyer and up to four to Holders, each rationed separately. The per-Ticket rationing keeps
any one person from being mailed twice, but the total volume the feature can emit rose, and that is
visible in the sending domain's reputation before it is visible anywhere else.

**Two flags now guard one mail flow, and they must stay independent.** Ticket Questions and
assignment are separately killable on purpose. A change that makes assignment's flag imply questions'
flag, or vice versa, removes the ability to turn off the newer and riskier half without taking down
the half already in use.
