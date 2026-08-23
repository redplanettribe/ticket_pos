# Customer identity is invisible, platform-global, and separate from staff identity

**Superseded in part by [ADR 0054](./0054-checkout-begins-signed-in-and-the-sale-is-addressed-to-the-session.md).**
One thing below no longer holds. On the `online` Sales Channel a Customer is no longer created *for* the
person by their first Ticket Sale: online checkout requires a Customer Session, the Sale is addressed to
the address that session proved, and the buyer types no address at all. The option this ADR rejected —
_Account required to buy_ — is the option now taken, for a reason this ADR did not weigh: a mistyped
address strands the buyer completely, because the only credential a guest ever receives travels inside the
mail sent to it. The conversion cost named below is real and was accepted knowingly.

Everything else here stands and is still load-bearing: a Customer is still platform-global and
`UNIQUE(email)`, still shares nothing with a Member but the OTP method, and is still created *for* the
person on every staff-recorded channel — a Sale Import, a Manually Recorded Sale, and later an In-Person
Sale all still mint a Customer from an address nobody has proven. `customer_id` is still `NOT NULL` with no
claim path, verification is still a public path that creates a persisted record, and "a typo merges
strangers" is still true of every channel a staff member types on.

## Context

Customers were deliberately not modeled as accounts. `CONTEXT.md` defined a **Customer** as identified by
email and name on each **Ticket Sale** and "not otherwise modeled as an account at launch"; `roadmap.md`
listed **Customer accounts** under _Out of scope_ with "guest checkout only";
`technical-design.md` recorded "Customer auth: guest checkout at launch". A buyer was three denormalized
strings on `ticket_sales`, repeated per purchase, with nothing tying those purchases to a person.

That forecloses everything buyer-facing: upcoming-event reminders, purchase history, saved preferences,
and eventually a real ticket wallet all need a durable identity to hang from. We decided to build one.
Three questions had to be answered together, because the answer to each constrains the others: does an
account gate the purchase, does it span **Organizations**, and does it share anything with a **Member**?

The system already has one identity: a Member, holding an email, an **Organization**, and a role, signed
in by email OTP into a **Staff Session** whose active Member gates that Organization's entire catalog and
sales.

## Decision

**A Customer is created for the person, not by them.** Every **Ticket Sale** on every **Sales Channel**
— **Sale Import**, **In-Person Sale**, and later **Online Sale** — creates or reuses a Customer keyed on a
normalised email, so `ticket_sales.customer_id` is `NOT NULL`. That is where essentially every record comes
from. A completed OTP creates one too, on the same normalised email, when someone proves an address no sale
has reached yet: a person can sign in before ever buying, and the read-only **Customer Area** owes them an
empty result rather than an error that would leak whether we hold them. Both paths converge on one row —
the record verification mints is the one a first sale would have — and neither asks anybody to register:
no signup form, no password, no profile to complete. Signing in means proving ownership of an email
address, nothing more. A record created on someone's behalf is inert: until an OTP sets `verified_at`, it
cannot be signed into and receives no email beyond the **Sale Confirmation** its own sale triggered.

**A Customer is platform-global**, `UNIQUE(email)`, spanning every Organization they have bought from.

**A Customer shares nothing with a Member** but the OTP method. Separate tables, separate session records,
separate cookies on separate origins. Even where the email matches, nothing links them. The OTP primitive
is extracted to a platform package that both depend on, with a `purpose` column on `otp_challenges` so a
code minted for one surface cannot be redeemed on the other.

## Considered options

**Whether the account gates the purchase.**

- _Account required to buy_ — the cleanest model and the best data, but a login wall before payment on a
  high-intent, infrequent purchase is a known conversion cost, and it contradicts guest checkout, which
  we still want.
- _Guest-first, account optional, claim past sales at sign-in_ — no friction, but leaves `customer_id`
  nullable and requires a "claim orphaned sales by email" backfill path that exists forever.
- _Created for the buyer, not by them (chosen)_ — indistinguishable from guest checkout to the buyer, but
  yields the required-account schema: a non-null FK from day one and no claim path ever written. The
  buyer who signs in before their first purchase is served by minting the record at verification rather
  than by a claim path, because both origins key on the same normalised email and land on the same row —
  there is nothing orphaned left to reconcile later.

**Whether a Customer spans Organizations.**

- _Org-scoped_, mirroring `members` `UNIQUE(organization_id, email)` — each Organization owns its buyer
  list cleanly, but the buyer signs in per promoter and "my upcoming events" covers one at a time.
- _Global identity with per-org profiles_ — the hedge; two tables and a join before a single per-org
  preference exists to justify it.
- _Platform-global (chosen)_ — the Storefront already has a global explorer listing **Discoverable**
  Events across all Organizations, so the product is a marketplace surface, not a set of white-label
  shops. A buyer who found a second promoter through our own explorer will not understand why their
  sign-in does not carry over.

**Whether Customer and Member are one identity.**

- _Reuse the `sessions` table_ with a nullable customer reference beside `active_member_id` — one OTP
  pipeline, but `active_member_id IS NULL` currently means "authenticated, no Organization context,
  onboarding only" and would silently also start meaning "this is a shopper". That is an overloaded null
  in the code that gates tenancy.
- _A shared `people` table_ that both Members and Customers point at — the more correct model in the
  abstract, but staff and buyers share an email string and a code generator, nothing else. It buys a join
  traversed on every request to express what comparing two emails already tells us.
- _Fully separate (chosen)_ — they are different authorization domains that happen to share an
  authentication method.

## Consequences

- **Organizations do not own their buyer lists.** An Organization sees only the buyers who bought its
  tickets, derived by joining through `ticket_sales`. The first promoter asking to "export my customers"
  forces product rules — scope, consent, what an export may contain — that do not exist yet. This is the
  main cost of going global and it is deferred, not avoided.
- **We hold identities for people who never asked.** A door sale or a promoter's spreadsheet creates a
  record for someone who never visited the site. `verified_at` is the entire defence, and it must not be
  bypassed when notifications are built — which is precisely the feature this identity exists to enable.
- **Verification is a public, unauthenticated path that creates a persisted record.** This is the honest
  cost of the second origin and the most interesting thing in this decision, so it is stated rather than
  left implied: anyone who can reach the Storefront can cause a `customers` row to exist, without being
  authenticated as anything. It is bounded, not prevented. The caller must **prove control of the address**
  by completing a passcode delivered to it, so a row can only be created for a mailbox its creator can
  read — an address they could equally have used to buy a ticket. The **per-email (3 / 15 min) and per-IP
  (10 / 15 min) issue limits**, scoped per purpose, and the five-attempt verify cap make the passcode
  expensive to obtain at volume. The **global outbound ceiling** caps how many passcodes the platform will
  send per window at all, and is the control that still holds when the attacker's per-key identity is
  unreliable; it is also why that ceiling is not merely an email-reputation defence but the backstop on
  this write path. What the effort buys an attacker is a row holding an address they already control, with
  no name, no sales, and access to nothing — the same row their first purchase would have created.
- **Email is the join key, so a typo merges strangers.** A box office mistyping `jon@` as `jo@`, where
  `jo@` is a real person who later signs in, exposes a stranger's sale. Low rate, no clean fix while email
  is the key. Accepted knowingly, and the reason the **Customer Area** stays limited to sales rather than
  widening to anything more sensitive.
- **Erasure collides with an Organization's records.** A **Ticket Sale** is a financial record a promoter
  needs to reconcile, and `customer_id` is `NOT NULL`, so a Customer cannot simply be deleted. Erasure
  tooling is deferred and handled manually; a `deleted_at` column ships unused so soft delete is later a
  small change rather than a backfill-and-audit exercise.
- **Two auth paths to maintain**, and shipped staff auth gets refactored before the new feature is
  written: the OTP primitive moves to a platform package and `otp_challenges` gains `purpose`. The
  existing staff auth integration tests passing unchanged is the evidence that extraction was
  behaviour-preserving.
- **Three documents were wrong the moment this was decided.** `CONTEXT.md` (updated — `Session` renamed to
  **Staff Session**, **Customer** redefined, a **Customer identity** section added), `roadmap.md`, and
  `technical-design.md`.
