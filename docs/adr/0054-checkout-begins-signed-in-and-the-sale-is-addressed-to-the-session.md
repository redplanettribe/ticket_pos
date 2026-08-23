# Checkout begins signed in, and the Sale is addressed to the session

**Supersedes [ADR 0010](./0010-customer-identity-platform-global-and-separate-from-staff.md)'s first decision
point — "a Customer is created for the person, not by them" — on the `online` Sales Channel only.**
Everything else in ADR 0010 stands and is still load-bearing: a Customer is still platform-global and still
`UNIQUE(email)`, still shares nothing with a Member, and is still created *for* the person on every
staff-recorded channel.

**Supersedes [ADR 0035](./0035-guest-granted-consent-pends-until-the-email-is-proven.md)'s producer, not its
state.** Nothing in this system creates a Pending Confirmation any more. Every Pending Confirmation already
recorded stays exactly as it is, and every path that resolves one keeps working.

Builds on [ADR 0011](./0011-google-sign-in-as-proof-of-email-ownership.md),
[ADR 0046](./0046-a-ticket-is-assigned-to-a-holder-who-accepts-by-mail.md) and
[ADR 0048](./0048-the-buyer-holds-one-ticket-by-paying.md).

## Context

ADR 0010 weighed three ways to give a buyer an identity and chose the one with no wall: a Customer is
minted *by* their first Ticket Sale, from an email they typed at checkout, and nobody is asked to register.
It considered _Account required to buy_ explicitly and rejected it — "a login wall before payment on a
high-intent, infrequent purchase is a known conversion cost, and it contradicts guest checkout, which we
still want."

That decision named one cost of email-as-identity: "a typo merges strangers." A box office mistyping `jon@`
as `jo@`, where `jo@` is a real person, exposes a sale to somebody else. Accepted knowingly, low rate, no
clean fix.

It did not name the other half, and the other half is worse. **A typo that lands on nobody strands the
buyer completely.** The only credential a guest buyer ever receives is the Confirmation Link, and that link
travels *inside the Sale Confirmation*, to the address that was typed wrong. So does every later mail: the
Assignment Reminder, the Answer Reminder, the Consent Confirmation Link. The success page shows a Sale
Confirmation reference, but a reference is not a credential and nothing accepts it as one. The checkout
context cookie dies with the browser jar. There is no support path, because there is no way for the buyer
to prove the purchase was theirs — and no way to return their money, because a reversal has nobody to
tell.

The buyer has paid, holds tickets, and cannot reach them. This has now happened to real people.

It is worth being precise about which channel has this problem, because it is exactly one:

| Sales Channel | Who types the address | Remedy when it is wrong |
| --- | --- | --- |
| `online` | the buyer | **none** |
| `import` (file) | staff | Sale Correction (ADR 0050) |
| `import` (Manually Recorded Sale) | staff | Sale Correction — it rides the `import` channel (ADR 0052) |
| `in_person` | — | no buyer-facing surface exists |
| Ticket Assignment | the buyer, naming a Holder | none needed: the mail round trip *is* the proof (ADR 0046) |

`online` is the only place in this system where an unverified address is typed by somebody who holds no
other door. Everywhere else, either a staff member is accountable and can correct the row, or the address
proves itself by being clicked from.

Two things have changed since ADR 0010 that make its trade worth reopening rather than merely regretting.
**Google Sign-In exists** (ADR 0011), and it is a second Proof of Email Ownership equal to a passcode in
force — the friction 0010 priced was a mail round trip, and for anyone with a Google account that round
trip is now a redirect. And **Ticket Assignment exists** (ADR 0046, ADR 0047, ADR 0048): "I am buying these
for somebody else" was, in 0010's world, a reason the checkout email had to be free-form. It now has a
first-class home with proof attached, and a reminder to use it (ADR 0051).

## Decision

**Checkout requires a Customer Session, and the wall stands at Buy.** Browsing an Organization, an Event,
its Ticket Types and its prices stays anonymous — the Storefront is a discovery surface (ADR 0002, ADR 0037)
and nothing here gates it. Ticket quantities are chosen anonymously too. Pressing Buy without a session
sends the visitor to sign in and brings them back. One wall, at the moment of intent, before any data
entry — a buyer learns what this costs them before they have filled anything in, not after.

**The Sale is addressed to the session, and the address is not a field.** The checkout dialog no longer
collects an email; it shows the one the session proved, and offers a way to sign in as somebody else. The
API drops `customer_email` from the begin-checkout request entirely, and reads the address from the
session. This is the whole decision: while that field exists, the mistake is expressible, and sooner or
later something will express it. Deleting it is what makes "an online Sale is addressed to a proven
address" a property of the schema rather than a convention of a form.

**Enforcement is in the API, and the route moves to say so.** Begin-checkout leaves `/api/v1/public/…` for
`/api/v1/customer/…` and is wrapped in the same `RequireCustomerSession` as every other session-gated
Customer route. `SelfAsserted` is still computed from the session rather than assumed, so the column keeps
meaning "we checked" for the rows where it varied. Confirm-checkout and the reversal read stay public and
idempotent: they are the Payment Provider's return leg, and they must work for a browser that has lost
everything.

**A Confirmation Link session does not open checkout; a long-lived one does.** The 24-hour, sale-scoped
session minted from a Confirmation Link is not Proof of Email Ownership — it is minted from a token that
travelled in an email and may have been forwarded, and it says only that somebody opened a receipt. It
cannot buy. The ordinary Customer Session can, for its full life and with no re-proof at the till; but the
dialog says whose it is, plainly and where it cannot be missed, because a session that outlives the person
sitting at the machine is the one way a Sale still reaches the wrong inbox.

**The rule is scoped to the `online` channel, and the scope is a rule rather than an omission:** proof is
required where the buyer is the typist and holds no other door. Staff-recorded channels remain
trusted-typist, because a staff member is accountable, present, and has a correction lever. Ticket
Assignment is untouched: it proves by round trip, and demanding proof at typing time would mean a buyer
could only name a Holder who already had an account.

**Policy Acceptance is a fact about the Customer, evidenced once and carried — not re-established per
Sale.** With a wall in front of checkout, a first-time buyer meets the consent boxes at sign-in, where the
form already holds a sign-in that has not accepted the Policy. The checkout dialog then draws no boxes,
because it only ever drew unanswered ones. This is not new behaviour — a returning Customer is not
re-asked today — but it becomes the normal case rather than the repeat case, and ADR 0035's framing of
Policy Acceptance as "a fact about the sale" no longer describes where it is captured.

## Considered options

**Whether to require a session, or merely to prove the address.**

- _An inline passcode inside checkout_ — collect an email as today, then force a passcode or Google round
  trip before the pay button. Solves the stated problem exactly as well: a typo means the code never
  arrives, and the buyer finds out before paying rather than after. Rejected because it keeps every
  guest-shaped branch alive forever — the "is this checkout's email proven?" predicate is written three
  times today, in `selfAssertedCheckout`, in the consent-box computation, and in the Storefront's mirror of
  it — and because it puts a mail round trip *inside* a payment flow, which is the worst place in this
  system for one.
- _Confirm the typo without proving anything_ — type the address twice, or interstitially ask "we will send
  your tickets to `jon@gmial.com`, is that right?". Nearly free, changes no architecture, and catches most
  fat-fingers. Rejected as the primary answer because it catches nothing about an address that is wrong but
  plausible, and buys no identity: the buyer still holds no credential but a link sent to the address in
  question.
- _A session (chosen)_ — the only option under which the failure is structurally impossible rather than
  made unlikely, and the only one that collapses the three predicates to one constant.

**Where the wall stands.**

- _At the Event page_ — sign in to see Ticket Types at all. Rejected outright: it would gate the public
  discovery surface ADR 0002 and ADR 0037 exist to build.
- _Inside the checkout dialog_ — open the dialog as today, make its first step a sign-in. Rejected because
  Google Sign-In is a full-page redirect off this origin, and the state hardest to reconstruct on the way
  back is an open modal's.
- _At Buy (chosen)_ — the redirect crosses a page boundary the app already controls, and the bad news
  arrives before the buyer has typed anything.

**How the chosen quantities survive the round trip.**

- _They do not_ — bounce to sign-in, come back to an empty Event page. Zero machinery, and it makes the
  buyer do the work twice at the exact moment they have already been annoyed once.
- _A short-lived cookie_ — the checkout-context pattern, which exists and is well tested, at the cost of
  another cookie with another expiry policy.
- _In the destination (chosen)_ — the selection rides the `next` query, which the sign-in page already
  reads and which Google Sign-In already carries across its callback in the state cookie, alongside the
  Follow intent it carries for the same reason. No new storage, and debuggable. It is treated as a
  *suggestion* rather than a command: quantities are re-judged against capacity, purchase limits (ADR 0025)
  and Promotions (ADR 0021) exactly as they always were, and a link naming a sold-out Ticket Type restores
  what it can and says what it dropped rather than quietly showing a different total than the one the buyer
  pressed Buy on.

**Whether the address stays editable, defaulting to the session.**

- _Editable_ — preserves buying for somebody else without a second step. Rejected, and this is the decision
  the whole ADR turns on: an editable field can still be typed wrong, so this version pays ADR 0010's
  conversion cost in full and keeps the bug. It would prove only that the buyer owns *some* address, not
  that the Sale is addressed to it.
- _Fixed (chosen)_ — the failure becomes impossible rather than unlikely.

**Whether to enforce in the Storefront alone.**

- _A Storefront-only wall_, leaving the public endpoint as it is — the smallest change, and worthless. The
  BFF is a hop, not a boundary; the API would remain a guest-checkout endpoint accepting any typed address,
  and the property would be a UI convention that the next integration or the next stale client quietly
  drops.

**Whether to extend proof to the staff channels.**

- _Require proof everywhere_ — a door sale could not be recorded without the customer completing a
  passcode, and an import file could not be loaded until every row's owner clicked something. That destroys
  both channels' reason for existing, and both already have a remedy this channel lacked.

## Consequences

- **The friction ADR 0010 priced is now real, and it is a stack.** A first-time buyer presses Buy, waits
  for a passcode mail, types a code, answers the consent boxes, returns to a restored cart, and only then
  gives their name and Tax ID before paying. Google Sign-In collapses the middle of that and is the only
  lever on it worth pulling, which is why its button leads the sign-in form. This is the cost. It is not
  argued away here; it is the price of the property, and if the conversion damage turns out to be worse
  than the stranded buyers it prevents, this ADR is the thing to reopen.
- **Sign-in mints a nameless Customer, so the dialog cannot become a confirmation screen.** Signing in
  proves an address and asks nothing else — no name, no password, no profile. A first purchase must
  therefore still collect first and last name, and since every online checkout is now the buyer speaking
  about themselves, the Tax ID and phone it collects are always written back to the Customer. That is a
  slightly wider write than before, on fields some people consider sensitive, and it is the `SelfAsserted`
  guard ceasing to be conditional rather than a new capability.
- **`SelfAsserted` stops varying on `online`.** It remains meaningful for history and for the staff
  channels, and it is still computed rather than asserted, but a reader who finds it always true on recent
  online rows should find this ADR rather than a bug.
- **Nothing produces a Pending Confirmation any more.** ADR 0035 existed because a guest could tick a
  marketing box for an inbox nobody had proven; `import` and `in_person` collect no consent at all, so with
  the guest gone the state has no producer. Its rows and its resolvers stay: the Consent Confirmation Links
  already sitting in people's inboxes keep resolving, the "proven owner answers at a later capture moment"
  path still supersedes, and the `nil` / `false` / `pending` distinction stays load-bearing (ADR 0034). No
  expiry job is introduced and no pending row is migrated to a decision — inferring an answer from silence
  is precisely what ADR 0035 refused, and tidying up would mean either manufacturing consent or destroying
  evidence. ADR 0035's underlying rule, that a tick from an unproven address is a claim rather than a
  consent, is untouched and still governs Follows and the withdrawal path (ADR 0039).
- **Buying for somebody else becomes Ticket Assignment, deliberately.** A partner, a parent buying for a
  child, an assistant: under this decision the Sale is theirs, addressed to their inbox, and the tickets
  appear in their Customer Area until they name a Holder. That is the flow ADR 0046 and ADR 0048 already
  built and ADR 0051 already reminds people to finish. Somebody who only ever wanted to hand over a ticket
  now has an account and a second step, and that is the trade.
- **This fixes nobody who is already stranded.** The wall is prospective. The Sales that prompted it still
  point at addresses nobody can open, and there is no correction path for an `online` Sale — Sale
  Correction is import-only by construction (ADR 0050), and an online Sale carries a Payment, a platform
  fee and a Reversal Window that replacement cannot honour. Re-addressing an existing Ticket Sale is a
  separate decision, deliberately not taken here: it adds a privileged staff capability to change who a
  financial record belongs to, and that deserves its own argument about who may pull it, what evidence it
  leaves, and what happens when the corrected address is *also* wrong — which is ADR 0010's
  "a typo merges strangers" again, with a staff member's hand on it. What this ADR buys that work is a
  bounded caseload: after it ships, the set of stranded Sales stops growing.
- **A typo on a staff channel still strands somebody, and only staff can fix it.** The boundary drawn here
  is defensible, not airtight. The buyer of a mistyped Manually Recorded Sale is exactly as stuck as the
  online buyer was; the difference is that somebody accountable can reach the row.
- **The Confirmation Link keeps its job, for a new reason.** It stops being the guest buyer's only
  credential and becomes the fallback for a session that did not survive the round trip — cleared cookies,
  a provider webview that drops them, a return in a different browser. That path is not guest-checkout
  residue to be deleted; it is the graceful degradation on the one flow where the buyer has already paid by
  the time it runs, and it stays.
- **A breaking API change, and the tests that stop compiling are the inventory.** Begin-checkout moves and
  loses a required field, so the OpenAPI artifacts regenerate with it. The integration and end-to-end suites
  carry guest checkout as a premise rather than as an assertion — the guest consent-visibility matrix is
  two-thirds unreachable, and every test that posts a `customer_email` no longer compiles. Working through
  them is how the assumptions this decision missed get found.
