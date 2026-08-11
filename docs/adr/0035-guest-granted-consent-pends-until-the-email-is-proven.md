# Guest-granted consent pends until the email is proven

## Context

Checkout is guest by definition: anyone can type anyone's email. A marketing opt-in ticked there is
a claim on an inbox nobody has proven ownership of — the same problem that makes a Follow require
Proof of Email Ownership, and the reason a guest checkout's Tax ID never overwrites a stored one.
Honouring such ticks at face value would let consent be manufactured by typing a stranger's
address; refusing to show the boxes to guests would lose the capture moment the guidance requires.

## Decision

Policy Acceptance from a guest is recorded unconditionally — it gates that purchase and evidences
that the person transacting was informed; it is a fact about the sale, not a claim on the inbox.
The optional consents (Marketing, Networking) ticked by a guest are recorded but enter Pending
Confirmation: denied for sending, unanswered for prompting. A guest's answer never overwrites a
state written under a proven session.

Pending resolves two ways, and never expires: the proven owner answers the re-shown box at a later
signed-in capture moment, superseding the guest's tick either way; or they click a purpose-scoped
signed confirmation link carried in the Sale Confirmation email — clicking from the inbox being
itself proof of ownership. This is the guidance's double opt-in, built from the unsubscribe token
pattern inverted.

## Consequences

- No expiry job: an unconfirmed pending sends nothing forever, which is functionally the
  guidance's "discard after X days" with better evidence.
- The consent UI never reveals whether an email is known — guests always see all boxes, and
  "already answered" is only ever disclosed after proof, preserving the oracle discipline the
  mail-locale work established.
