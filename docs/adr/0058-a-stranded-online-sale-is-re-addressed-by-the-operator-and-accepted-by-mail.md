# A stranded Online Sale is re-addressed by the Operator, and the corrected address accepts by mail

Specified as issue #419.

Takes the decision [ADR 0054](./0054-checkout-begins-signed-in-and-the-sale-is-addressed-to-the-session.md)
deliberately left open: what to do for the buyers it could not help. Builds on
[ADR 0019](./0019-operator-reversal-recording-an-out-of-band-refund.md)'s Operator authority over Online Sales and on
[ADR 0046](./0046-a-ticket-is-assigned-to-a-holder-who-accepts-by-mail.md)'s accept-by-mail pattern.
Does not touch [ADR 0050](./0050-an-imported-sale-is-corrected-by-replacement-never-edited.md), which
still governs the `import` channel.

## Context

Before ADR 0054, an online buyer typed their own email at checkout and nothing proved it. A typo that
landed on nobody stranded them completely: the Sale Confirmation, the Confirmation Link and every later
mail went to an address nobody can open, and the success page's `TP-` reference is not a credential. The
wall stopped the set growing; it fixed nobody already in it. ADR 0054 named the missing piece — "a
privileged staff capability to change who a financial record belongs to" — and listed what it would
have to answer: who may pull it, what evidence it leaves, and what happens when the corrected address is
*also* wrong.

Today the only remedy is an Operator Reversal after an off-platform refund, followed by the buyer buying
again signed in. It works, costs a refund round trip and a re-purchase, and reopens the Platform Fee
question on every case.

## Decision

A **Sale Re-addressing**: a Platform Operator records, against one active `online` Sale found by its
Sale Confirmation reference, the address the buyer meant. The platform mails that address a
**Re-addressing Link**; nothing moves until it is clicked. The click is Proof of Email Ownership — it
mints or matches a Verified Customer exactly as accepting an Assignment Link does — and at that moment the
Sale's Customer and snapshot email move to the corrected Customer, the Self-held Ticket follows if its
Holder is still the wrong address, and a fresh Sale Confirmation goes to the corrected inbox.

Everything transacted stays: the Sale's id and reference, its Tickets, the Payment and the snapshot the
provider was given, the Platform Fee, the Reversal Window (not restarted), and every other Ticket's
Holder. Consent granted under the wrong address does not follow — a tick from an unproven address was
only ever a claim (ADR 0035). The wrong address is told nothing, and the ghost Customer is left standing,
one Sale poorer, because it is the evidence trail. The Organization sees only the outcome: the Sales
list and export show the corrected address from acceptance on.

## Considered options

**Who may pull it.** Operator only (chosen); Org Admin, gated like Sale Correction; Org Admin asks and
the Operator answers, like a Payout Request. Every stranded Sale is an Online Sale carrying real money,
the caseload is bounded and small, and the Operator already holds the authority to assert what the
platform's money did. Handing an Organization a write on who owns a paid record is a larger trust grant
than the backlog justifies, and a request queue is machinery for a dozen cases.

**What the act is.** Re-address in place (chosen); reverse and replace, as Sale Correction does; leave
the Sale and merely assign the Ticket. Replacement cannot honour the money — the replacement would be a
free Sale against a paid reversal, and every money figure would have to be told to pretend. Assigning
the Ticket alone leaves the party of record a ghost with the money, the window and the other Tickets.
ADR 0050 refused edit-in-place because *what was transacted* must not change; here nothing transacted
changes, only the addressee, which was never a fact the buyer proved. That is a narrower carve-out than
0050 feared, and it is the only one that gives the buyer back everything they paid for.

**Whether the corrected address proves itself.** Mail round trip (chosen); the Operator's word; the
corrected address must already be a signed-in Customer. The round trip neutralises both failure modes
0054 named: a second typo lands on nobody and the Sale stays where it was, and a stranger would have to
click "accept this purchase" for something they never bought. Requiring a prior sign-in would make a
support call depend on the buyer first creating an account that shows them nothing.

**Scope.** Per Sale (chosen) rather than a Customer merge. A merge would move consent records that
deliberately record the address typed, retire a Customer, and be far harder to reason about undoing; a
buyer with two stranded Sales gets two acts and two mails, each naming its Sale.

**Channels.** `online` only (chosen). `import` has Sale Correction and two remedies for one mistake is
how a model rots; `in_person` has no buyer surface waiting to be unlocked. The Operator's two levers on a
Sale — reverse and re-address — therefore share one channel rule.

## Consequences

- One pending re-addressing per Sale. Recording another replaces it and kills the old link; the Operator
  withdraws with the same control; "send again" is recording the same address again.
- It expires at the Event's start, and the unaccepted corrected address is purged with it, on the same
  terms as an unaccepted Ticket Assignment (ADR 0046): it is an address typed by somebody who is not its
  owner. Past Event start the lever is not offered — "give me my tickets" has no meaning after the doors,
  and only the money question remains, which Operator Reversal already answers. Stranded Sales for Events
  that have already started get nothing from this feature, deliberately.
- A Sale Reversal while a re-addressing is pending kills it, and the corrected address is told nothing,
  having never become party to anything.
- Name, Tax ID and phone carry from the ghost only into a nameless Customer; an existing Customer's own
  facts win, and the Sale's snapshot keeps what was typed.
- The Re-addressing Link is a signed token delivered to the corrected address alone, never shown to the
  Operator or to staff, so the Operator cannot complete the acceptance themself — the same property that
  makes the Assignment Link a proof.
- Mail follows the Sale's own locale, as the original Sale Confirmation did.
- No staff or Integration Partner endpoint exists; the Organization's part is forwarding the buyer's
  `TP-` reference to the Operator, which is what it does today.
