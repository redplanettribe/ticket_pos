# PRD: Customer Login

## Problem Statement

A person who buys a ticket has no way back in.

Once a **Ticket Sale** is recorded, the only artifact the buyer holds is the **Sale Confirmation** email.
If they lose it, they cannot recover what they bought.
They cannot see which of their upcoming Events is next, cannot review what they bought last season, and have nowhere to express a preference about anything.
The platform, for its part, cannot recognise a returning buyer: it holds their email as an inert string on a sales row, repeated once per purchase, with nothing tying those purchases to a person.

This blocks a whole class of buyer-facing capability — upcoming-event reminders, purchase history, saved preferences, and later a real ticket wallet — none of which can be built until there is a durable, verified identity to attach them to.

Today the only identity the system knows is a **Member**: staff acting inside an **Organization**.
Buyers were deliberately left as guests (`CONTEXT.md`: _"Not otherwise modeled as an account at launch"_; `roadmap.md` lists **Customer accounts** under _Out of scope_).
This PRD reverses that decision and is the reason for ADR 0010.

## Solution

Give every **Customer** a real, platform-global identity — created for them, not by them — and a way to prove they own it.

The account is **invisible**. A Customer record is created or reused by *any* **Ticket Sale**, on *any* **Sales Channel**: a **Sale Import**, an **In-Person Sale** at the door, and later an **Online Sale**. It is also created, on the same normalised email, by a completed passcode verification for an address no sale has reached yet — the person who arrives before their first purchase. Nobody registers. Nobody fills in a signup form. By the time the **Sale Confirmation** email lands, the record already exists.

"Logging in" therefore means only one thing: **proving you own an email address**. Almost always the record is already there and the proof simply opens it; where it is not, the proof mints exactly the record a first sale would have. Either way nothing is asked of the person but their address. There are two ways to prove it.

1. **A one-time passcode**, exactly as staff sign in today — reusing the existing OTP pipeline and its Resend delivery. This yields a full **Customer Session** spanning every **Ticket Sale** the Customer owns, across every **Organization**.
2. **A Confirmation Link** carried in the **Sale Confirmation** email. One tap, no typing, no form. It grants access to *that one Ticket Sale* only. This is the path most buyers will ever take.

Signed in, the Customer lands in the **Customer Area**: their upcoming Events and their past **Ticket Sales**, read-only.

A Customer is **platform-global**. One email, one record, one login — spanning every **Organization** they have ever bought from. This follows the Storefront the product already has: the global explorer at `/` lists **Discoverable** Events across all Organizations, so a buyer who found one promoter through another will reasonably expect one identity to cover both.

A Customer record created on someone's behalf is **inert until verified**. It receives no email beyond the **Sale Confirmation** its own sale triggered, and cannot be signed into, until the person completes an OTP and becomes a **Verified Customer**.

This PRD ships **identity only**: the record, the two credentials, the session, and a read-only **Customer Area**. Preferences, notifications, and any change to checkout are deliberately excluded — but every one of them is a column or a table hanging off a stable Customer id, and nothing here forecloses them.

## User Stories

### Getting an identity without asking for one

1. As a person who bought a ticket at the door, I want a Customer record to exist for me already, so that I can see my purchase without ever having registered.
2. As a person whose sale arrived through a **Sale Import**, I want a Customer record created for me too, so that off-platform buyers are not second-class.
3. As a person who buys from a second **Organization**, I want the same Customer record reused, so that I do not accumulate an identity per promoter.
4. As a person who has never bought anything, I want to be able to request a passcode without being told whether my email is known, so that the system does not leak who its customers are.
5. As a person whose email was entered with different capitalisation at two different box offices, I want both sales on one record, so that letter case does not fragment my history.
6. As a person for whom a record was created without my involvement, I want to receive no marketing or notification email until I have verified my address myself, so that I am not contacted because a promoter uploaded a spreadsheet.

### Proving ownership with a passcode

7. As a Customer, I want to sign in from the Storefront with just my email address, so that I do not need a password.
8. As a Customer, I want to receive a one-time passcode by email, so that I can prove I own the address.
9. As a Customer, I want to enter the passcode on the same page I entered my email, so that signing in does not scatter across routes.
10. As a Customer who mistypes the code, I want a limited number of attempts before it is invalidated, so that typos are tolerated but guessing is not.
11. As a Customer who requests codes repeatedly, I want to be rate-limited, so that my inbox is not flooded.
12. As a Customer, I want a code minted for the Storefront to be useless on the Staff app, so that one surface's passcode cannot open another.
13. As a Customer whose code has expired, I want a clear message and an obvious way to request another, so that I am not stuck.
14. As a Customer who completes a passcode, I want to become a **Verified Customer**, so that I own my record from that point on.
15. As a developer running the stack locally, I want Customer OTP codes logged to the terminal like staff codes, so that I can test without a real mailbox.

### Proving ownership with a Confirmation Link

16. As a buyer, I want a "view your tickets" link in my **Sale Confirmation** email, so that I can see my purchase without typing anything.
17. As a buyer, I want that link to still work on the day of the Event months later, so that it is useful at the gate rather than expired.
18. As a buyer following that link, I want to see the **Ticket Sale** it belongs to, so that the link goes somewhere useful.
19. As a buyer who forwards my confirmation email to a friend, I want the link to expose only that one sale, so that my full purchase history is not forwarded with it.
20. As a buyer arriving by Confirmation Link, I want to be offered a passcode sign-in to see everything else, so that there is an obvious path from one sale to my whole history.
21. As a buyer, I want a Confirmation Link for a **Ticket Sale** that has been reversed to say so plainly, so that I am not misled about what I hold.
22. As a Customer already signed in who taps a Confirmation Link, I want to keep my full access rather than be narrowed to one sale, so that the link never downgrades me.

### The Customer Area

23. As a signed-in Customer, I want to see my upcoming Events, so that I know what is next.
24. As a signed-in Customer, I want to see my past **Ticket Sales**, so that I have a purchase history.
25. As a signed-in Customer, I want each entry to show the Event, its date, the **Organization**, and what I bought, so that the list is meaningful at a glance.
26. As a signed-in Customer, I want my purchases across every **Organization** in one list, so that I do not check several places.
27. As a signed-in Customer, I want to see my **Sale Confirmation** reference for each sale, so that I can quote it to a promoter.
28. As a signed-in Customer with no purchases, I want an empty state that points me at the explorer, so that the page is not a dead end.
29. As a Customer arriving by Confirmation Link, I want the Area scoped to that one sale, so that the narrow credential and the narrow view agree.
30. As a signed-in Customer, I want to see which email I am signed in as, so that I know which identity I am using.
31. As a signed-in Customer, I want it to be impossible to see another Customer's sales, so that my purchases stay private.

### Sessions

32. As a Customer, I want my session to last months and extend on use, so that a ticket bought in March still recognises me in June.
33. As a Customer, I want to sign out, so that I can leave a shared device safely.
34. As a Customer, I want signing out of the Storefront to have no effect on any Staff app session, so that the two identities stay independent.
35. As a **Member** who also buys tickets, I want my staff sign-in and my buyer sign-in to be separate, so that acting for my **Organization** and buying a ticket never blur.
36. As a Customer, I want my session cookie to be inaccessible to page scripts, so that a script injection cannot lift it.
37. As a Customer whose session has expired, I want to be sent to sign-in rather than shown an error, so that recovery is obvious.

### Sign-in state on the Storefront

38. As an anonymous visitor, I want the Storefront to look and behave exactly as it does today, so that browsing Events needs no account.
39. As an anonymous visitor, I want a visible way to sign in, so that I can reach my purchases when I want them.
40. As a signed-in Customer, I want the Storefront to show that I am signed in, so that my state is never ambiguous.
41. As a visitor, I want Event and **Storefront listing** pages to stay as fast as they are now, so that adding accounts does not slow down browsing.

### Names and profile

42. As a Customer, I want my name taken from my purchase, so that I am greeted correctly without filling anything in.
43. As a Customer entered under a nickname at a box office, I want a later, better-spelled purchase to improve my profile name while I am unverified, so that the record self-corrects.
44. As a **Verified Customer**, I want a promoter's later spreadsheet to be unable to rename me, so that I own my own name once I have claimed it.
45. As an **Org Admin**, I want the buyer name on my **Sales list** to remain exactly what was recorded at the time of sale, so that my records stay stable for reconciliation.

### Operations and abuse

46. As a platform operator, I want a hard ceiling on OTP emails sent per window, so that an attacker cannot burn the sending domain's reputation.
47. As a platform operator, I want a loud signal when that ceiling is hit, so that I learn of an attack from my own alerting and not from a suspended email provider.
48. As a platform operator, I want per-IP rate limits to count the real client IP, so that the limit cannot be defeated by a forged header.
49. As a platform operator, I want Customer auth to use the standard envelope (`data`, `error`, `request_id`), so that errors stay consistent across the API.
50. As a platform operator, I want Customer auth versioned under `/api/v1/`, so that breaking changes can be introduced cleanly.
51. As a platform operator, I want to erase a Customer on request without destroying an **Organization**'s sales records, so that I can honour the request without breaking reconciliation.
52. As a security reviewer, I want no browser to call the Go API directly, so that ADR 0008 continues to hold on the Storefront.

## Implementation Decisions

Each numbered decision below was settled in a grilling session; the trade-offs and rejected alternatives for the first three are recorded in ADR 0010.

### 1. Invisible account

A Customer record has **two origins**, and both key on the same normalised email:

1. **A Ticket Sale**, on any **Sales Channel** — **Sale Import**, **In-Person Sale**, and later **Online Sale**. This is where essentially every record comes from: `ticket_sales.customer_id` is **`NOT NULL`**, so no sale can be recorded without one.
2. **A completed passcode verification**, when no record exists for the proven email yet. Signing in creates the record and stamps `verified_at` in the same step.

The second origin exists because the state it produces has to be reachable. This PRD asks for an empty **Customer Area** that points at the explorer rather than being a dead end (story 28, scenario 16) — which describes a signed-in person who has bought nothing, and people plausibly arrive there: they were told about the platform and are looking around, they are checking whether a sale they were promised was actually recorded, or a friend bought on their behalf against a different address. The alternative is to fail verification for an unknown email, which reintroduces at the verify step precisely the oracle decision 10 removes from the request step, or to hand back a session anchored to nothing. Creating or reusing by normalised email is what that person's first **Ticket Sale** would have done anyway, so the record is the same shape either way; it simply arrives already verified and carrying no name yet, since an email is all it was built from.

Neither origin weakens the invisible account, because the thing being avoided is enrolment, not record creation. **Nobody fills in a registration form on either path.** There is no signup, no password, no profile to complete, and nobody is ever asked whether they would like an account: on one path a purchase mints the record, on the other a proof of address does. "Login" still means only proving you control an email address.

**Accepted consequence:** verification is an unauthenticated, publicly reachable path that writes a persisted row. What bounds it — proof of control of the address, the per-key passcode limits of decision 12, and the global outbound ceiling — is recorded in full in ADR 0010.

### 2. Platform-global Customer

`UNIQUE(email)` across the platform. One record spans every **Organization**.

Emails are normalised to lowercase and trimmed before lookup, insert, and uniqueness comparison. Normalisation happens at one point in the service layer so that every entry path — import, POS, and later online checkout — agrees.

**Accepted consequence:** an **Organization** does not own its buyer list. It sees only the buyers who bought its tickets, derived by joining through `ticket_sales`. Any future "export my customers" capability is a product decision requiring its own rules.

### 3. Separate from staff identity

`customers` is independent of `members`. Customer Sessions and **Staff Sessions** are unrelated records with separate cookies on separate origins; signing out of one does nothing to the other. Nothing links a Customer to a Member even when the email matches.

The two share one thing: the OTP mechanism.

### 4. OTP mechanics extracted to platform

The OTP primitive moves from `identity` into a **platform package**: code generation, hashing, rate limiting by email and IP, verify-attempt capping, expiry, and invalidation. None of it knows what kind of session results.

Both `identity` and the new `customers` module depend on it. Neither depends on the other.

`otp_challenges` gains a **`purpose`** column (`staff` | `customer`). Issuing records the purpose; verifying requires a match. A code minted for one surface **must not** be redeemable on the other. Existing rows migrate to `staff`.

Rate-limit counters are scoped per purpose so Storefront traffic cannot exhaust a staff operator's allowance.

### 5. Two credentials, two scopes

| | Full Customer Session | Confirmation Link session |
|---|---|---|
| Earned by | Completing an OTP | Following a signed link in a **Sale Confirmation** |
| Grants | Every **Ticket Sale** the Customer owns | Exactly one **Ticket Sale** |
| Lifetime | 180 days, sliding on use | ~24 hours, **does not slide**, re-mintable by following the link again |
| Sets `verified_at` | Yes | **No** |

The two lifetimes are one bargain, not two independent settings. The **Confirmation Link token itself** is deliberately durable — it must work at the gate months after purchase — and that durability is only safe because each redemption mints a session that is both narrow and short. So the sale-scoped session's ~24 hours is measured from the moment it was minted and is **never extended by use**: reading the **Customer Area** behind a link, however often, does not buy more time. Getting more access means following the link again, which mints a fresh short session. Extending a sale-scoped session on use would silently turn a forwardable email into a six-month credential to that **Ticket Sale**, which is the one thing the sale-scoping exists to prevent.

Only the full Customer Session slides (decision 6).

**Confirmation Link token lifetime, in full.** The token's expiry is signed into the token itself, so redemption needs no Event lookup and no stored token table. It is decided once, at issue time:

| Case | Token valid until |
|---|---|
| Event has a schedule | Event end + **30 days** grace |
| Event has no date at all | issue time + **365 days** |
| Either of the above landing sooner | never less than issue time + **30 days** (floor) |

- "Event end" is the Event's `ends_at`, falling back to `starts_at` when only a start is known.
- The **grace window** is 30 days so a dispute, a late question, or a misremembered date after the Event still resolves.
- The **floor** exists for back-dated sales: a **Sale Import** of an Event that already happened would otherwise mint a link that was dead before the email left the building. Measuring from issue time guarantees every buyer gets a usable link.
- The **undated horizon** covers an Event with no schedule to hang an expiry on: a year is long enough to stay useful once the date is announced, short enough to still be a bound.

None of these lengthen the session a redemption mints; they only govern how long the link keeps being redeemable.

A link redemption **does not** mark the Customer verified: possession of a forwarded email is not proof of ownership of the address. Only an OTP sets `verified_at`.

Following a Confirmation Link while already holding a full session **must not** narrow access — the wider session wins.

**Accepted consequence:** a forwarded confirmation email lets the recipient see that one sale. Bounded deliberately by the sale-scoping, which is why the **Customer Area** stays limited to sales and must not widen to anything more sensitive without revisiting this.

### 6. Sliding 180-day sessions

Buyers purchase months ahead and return at the door. A staff-length window would make the account a login form met at every visit, defeating the invisible-account model.

Sessions are server-side rows, so revocation stays available.

**Accepted consequence:** long-lived sessions persist on shared and family devices. Mitigated by a visible sign-out; a "sign out everywhere" is a later addition.

### 7. New `customers` domain module

A new domain module under the backend, following the repo's existing handler / service / repository layering.

It owns: the Customer record, Customer Sessions, Confirmation Link issuing and redemption, and the **Customer Area** read model.

The **sales** module calls the `customers` **service** (not its repository) to upsert a Customer, inside the same transaction that records the sale — consistent with the technical design's rule that cross-module calls go through services.

Rationale for a separate module over extending `identity`: staff authorization is the highest-consequence code in the system, since the active **Member** gates an entire **Organization**'s catalog and sales. Buyer features should not require edits adjacent to it.

### 8. Account holds assertions; sales hold transactions

| | Source of truth for | Mutability |
|---|---|---|
| `ticket_sales.customer_first_name` / `customer_last_name` | What was recorded at that transaction | **Immutable.** Never rewritten. |
| `customers` profile name | What the person currently asserts | Refreshed by each new sale **while `verified_at IS NULL`**; person-owned and sale-immune once verified |

This is the general rule for everything that follows: **the account holds what the person asserts about themselves; sales hold what was transacted.** Preferences, notification settings, and locale belong on the account and never leak onto a sale.

**The verified Customer who has no name.** Decision 1's second origin creates one: a completed passcode verification builds the record from an email alone, so a person who signs in before ever buying is verified and carries no name. Sale-immunity taken literally would freeze them nameless forever, since there is no profile editor and nothing else writes the name. So the rule is stated one notch more precisely, and this is the form the implementation must express:

> **A sale may fill in a profile name that has never been set, and may never overwrite one that has.** While the Customer is unverified, every new sale refreshes the name — an unverified record is an assertion made on someone's behalf, and the newest sale is the best guess available. Once verified, the person owns whatever name is there and no sale may rewrite it; if there is no name there at all, the first sale supplies one, and it is protected from that moment on.

The distinction that matters is **never set** versus **set to something** — not "currently looks empty". These coincide today, and only because nothing can blank a name that was set: a name reaches `customers` only from a Ticket Sale, every sale entry path rejects a blank first or last name, verification writes empty names on insert alone, and no profile editor exists. A blank name therefore means precisely "no sale has ever supplied one", which is why the implementation may test the stored value directly. **Any future write path that can blank a name that was set — a profile editor, an erasure, an import correction — breaks that equivalence and requires the rule to be enforced by recording that a name was once set, rather than by inspecting the current value.**

None of this touches verification: a sale filling in a blank name neither sets nor clears `verified_at`, which only a completed passcode ever writes (decision 5). And it never touches the sale, whose own recorded name stays immutable in every case above.

### 9. Schema changes

**New `customers` table**

| Column | Notes |
|---|---|
| id | Primary key |
| email | Normalised lowercase, **globally unique** |
| first name, last name | Profile name per decision 8 |
| `verified_at` | Nullable. Null = record exists, nobody has proven ownership. Gates sign-in and all non-Sale-Confirmation email. |
| `deleted_at` | Nullable. **Ships unused** — see decision 12. |
| created_at | |

**New Customer Session table** — opaque token as primary key, customer reference, sliding expiry, and the scoping that distinguishes a full session from a Confirmation Link session (null = full; a **Ticket Sale** reference = scoped to that sale).

**`ticket_sales`** — gains `customer_id`, backfilled from existing `customer_email` values, then set `NOT NULL` with an index supporting the **Customer Area** query. (Migration 011 records the database as greenfield, so the backfill is expected to be a no-op in practice; it is written for correctness regardless.)

**`otp_challenges`** — gains `purpose`, existing rows set to `staff`.

### 10. API surface

Under `/api/v1/`, using the standard envelope:

| Purpose | Auth |
|---|---|
| Request a Customer OTP | None |
| Verify a Customer OTP, create a Customer Session | None |
| Read current Customer Session | Session token |
| Sign out | Session token |
| List the Customer's **Ticket Sales** (upcoming and past) | Session token |
| Redeem a Confirmation Link token | None (the token is the credential) |

Authorization rule, and the single most important property in this PRD: **a Customer Area read is always scoped by the session's Customer id, never by an identifier supplied in the request.** A sale-scoped session additionally restricts to its one **Ticket Sale**.

Requesting an OTP returns the same response whether or not the email is known.

### 11. Storefront BFF and UI

ADR 0008 forbids browser-to-Go-API calls, so the Storefront gains its own auth **route handlers**, mirroring the Staff app's BFF: the browser talks only to the Storefront's own routes, which hold the httpOnly cookie and forward the session token to the Go API.

Cookie: `httpOnly`, `Secure` in production, `SameSite=Lax`, path `/`.

New surfaces: a sign-in page (two-step, email then code, on one page — matching the Staff pattern), the **Customer Area**, and sign-in state in the Storefront header.

**Caching:** every Storefront page is already `force-dynamic` with `no-store` fetches, so reading a session cookie costs nothing — there is no static or ISR output to opt out of. This was verified rather than assumed.

### 12. Abuse defence

Three parts, the first of which is a **pre-existing bug fixed as a prerequisite**:

1. **Client IP trust is broken today.** The platform middleware takes the leftmost `X-Forwarded-For` entry, and the Staff BFF forwards the browser's own header verbatim. A client can therefore forge the IP the per-IP OTP limit counts against, and defeat it by rotating values. **This affects shipped staff auth now**, independently of this feature. Fix: the Go API stops trusting client-supplied forwarding headers; each BFF derives the true client IP and passes it in a distinct header, trustworthy precisely because ADR 0008 guarantees only our own services can reach the API.
2. **A global outbound OTP ceiling** — a platform-wide cap per window, independent of per-email and per-IP keys. It is the only control that holds when the attacker's per-key identity is unreliable. It fails closed on sending-domain reputation, which is slow and painful to repair, rather than open.
3. **A loud operational signal** when the ceiling trips.

The per-email (3 / 15 min), per-IP (10 / 15 min), and verify-attempt (5) limits already implemented for staff move to the platform package and apply to Customers, scoped per purpose.

CAPTCHA is deliberately deferred (see Out of Scope).

**Accepted consequence:** the global ceiling is a shared-fate control — a determined attacker can trip it and deny legitimate buyers their sign-in emails. A temporary sign-in outage is recoverable; a burned sending domain is not.

### 13. Sale Confirmation email

The existing **Sale Confirmation** gains a **Confirmation Link**. Because a Customer record provably exists by the time the email is sent (decision 1), the link always resolves.

### 14. Documentation

- `CONTEXT.md` — **already updated**: `Session` renamed to **Staff Session**, **Customer** redefined, and a **Customer identity** section added (**Verified Customer**, **Customer Session**, **Confirmation Link**, **Customer Area**).
- **ADR 0010** — records decisions 1, 2 and 3 as one coherent stance, with the buyer-list-ownership consequence stated plainly.
- `roadmap.md` — **Customer accounts** moves out of _Out of scope_; B13 ("OTP rate limiting and attempt lockout") is currently marked _Partial_ but is implemented, and should be corrected.
- `technical-design.md` — "Customer auth: guest checkout at launch" is now wrong.

## Testing Decisions

### What makes a good test

Test **external behavior at the highest stable seam**: HTTP request in, HTTP response out — status, envelope shape, `error.code`, and the resulting database state. Do not assert on internal call order, private helpers, or SQL text.

### Primary seam: no new seams

Per `docs/testing.md`, HTTP integration is the default and repository-level tests are a narrow exception for concurrency and locking only. Nothing here has a race worth that exception — the Customer upsert happens inside the sale's existing transaction.

So: **one new file in the existing `backend/integration/` package**, alongside `auth_test.go`, reusing the existing harness, its capture email sender, and its fixed clock. No new harness, no new package, no new test type.

Prior art: `auth_test.go` is named in `docs/testing.md` as the reference pattern for identity and session scenarios, and is the direct model for this file.

### Scenarios

**Identity creation**
1. Recording a sale for an unknown email creates a Customer; the sale references it.
2. Recording a second sale for the same email reuses the record — one Customer, two sales.
3. Emails differing only in case or surrounding whitespace resolve to one Customer.
4. A sale for a buyer in a second **Organization** reuses the same Customer.
5. A newly created Customer has `verified_at` null.

**OTP and verification**
6. Request, verify, session issued; the Customer becomes verified.
7. A wrong code is rejected; the attempt cap invalidates the challenge; the envelope carries the right `error.code`.
8. An expired code is rejected.
9. Per-email and per-IP rate limits return the rate-limited error.
10. Requesting for an unknown email returns the same response as for a known one.
11. **A code minted with purpose `staff` cannot be verified on the Customer endpoint, and vice versa.**
12. Staff and Customer rate-limit counters do not consume each other's allowance.
13. The global outbound ceiling blocks sends once tripped.

**Customer Area authorization — the properties that matter most**
14. A signed-in Customer sees their own sales, upcoming and past, across Organizations.
15. **A Customer never sees another Customer's sales, including when an identifier is supplied in the request.**
16. A Customer with no sales gets a well-formed empty result.
17. An expired session is rejected as unauthenticated.
18. Sign-out destroys the session; the next call is rejected.

**Confirmation Link**
19. Redeeming a valid token yields a session scoped to exactly that **Ticket Sale**.
20. **A sale-scoped session cannot read any other sale belonging to the same Customer.**
21. Redemption does **not** set `verified_at`.
22. A tampered or unsigned token is rejected.
23. A token past the event-end grace window is rejected.
24. Redeeming while holding a full session does not narrow access.

**Profile name**
25. An unverified Customer's profile name is refreshed by a later sale.
26. A verified Customer's profile name is **not** overwritten by a later sale.
27. A **Ticket Sale**'s recorded name is never rewritten by any of the above.

**Session lifetime**
28. Use extends the sliding window; a session past expiry is rejected. (Via the harness's fixed clock — not by waiting.)

### Existing coverage that must not regress

Extracting OTP into the platform package changes shipped staff auth. `auth_test.go` must pass **unchanged** — it is the regression suite for that refactor, and its passing unmodified is the evidence the extraction was behaviour-preserving.

No unit tests are proposed for the extracted package: it is a move rather than a rewrite, already covered through HTTP, and adding unit tests would fix its internals at the moment they should stay free.

### E2E

Deferred. The cookie flow across the Storefront BFF is genuinely E2E-shaped, but B16 (staff auth E2E) is still not started, so there is no auth E2E pattern to follow and this is the wrong feature in which to invent the first one. Noted as a follow-up.

## Out of Scope

- **Any change to checkout.** V5 (Online Sale, guest checkout) does not exist. Nothing here creates, blocks, or modifies a purchase flow.
- **Requiring an account to buy.** Guest purchase remains; the sale *mints* the account, and never demands one first.
- **Customer preferences**, saved payment details, and notification settings — the first thing to build on this foundation, but they need product decisions not yet made (which preferences; global or per-**Organization**; marketing consent and its lawful basis).
- **Upcoming-event reminders and any outbound notification.** The **Customer Area** shows upcoming Events; it does not email about them. When this lands, the `verified_at` gate must not be bypassed.
- **A profile editor.** The name is captured and refreshed per decision 8; there is no UI to change it.
- **Real admission tickets** — per-attendee artifacts, QR codes, and check-in. `Sale Confirmation` remains explicitly not an admission ticket.
- **Deletion and erasure tooling.** Handled manually by operators (decision 12 below). `deleted_at` ships unused.
- **Merging or splitting Customers**, and correcting a mistyped email onto the right record.
- **Linking a Customer to a Member** even when emails match.
- **Any Staff app awareness of Customers** — no customer list, no CRM, no export.
- **CAPTCHA / Turnstile.** The durable answer for a public form, but a third-party script on the highest-intent page, a conversion cost, and a data processor to document — all to defend an endpoint with no users yet. The global ceiling buys time to add it deliberately.
- **Passwords, OAuth, or social sign-in.**
- **"Sign out everywhere"** and session management UI.
- **Per-**Organization** customer profiles.** Considered and deferred; revisit when a genuinely per-org preference (most likely marketing consent) exists.

## Further Notes

### Sequencing

This feature is designed *ahead of* the online purchase it will eventually attach to. That is deliberate, and decision 1 is what makes it viable: because **any** sale mints a Customer, the login is useful the day it ships — a buyer whose sale arrived by **Sale Import** or at the door can sign in and see it. Had Customer creation been tied to online checkout, this would have shipped as dark code until V5.

### Known limitations, accepted knowingly

- **Email is the join key, so a typo merges strangers.** A box office mistyping `jon@` as `jo@`, where `jo@` is a real person who later signs in, exposes a stranger's sale. Inherent to keying on email, low rate, no clean fix. Bounded by keeping the **Customer Area** to sales only, and it is the main reason not to widen it.
- **Organizations do not own their buyer lists** (decision 2). The first promoter asking to export their customers forces product rules that do not exist yet.
- **Unverified records exist for people who never asked.** The `verified_at` gate is the whole defence and must hold as features are added on top.
- **A forwarded Sale Confirmation exposes that one sale** (decision 5).
- **The global OTP ceiling is shared-fate** (decision 12).

### Prerequisite bug

The `X-Forwarded-For` weakness in decision 12 is **live in shipped staff auth** and independent of this feature. It could reasonably ship as its own small change first.

### Roadmap accuracy

`roadmap.md` describes the repo as "between V0 and V2" with catalog "Not started"; catalog and the sales spine have in fact landed. Statuses are stale beyond the two corrections listed in decision 14, but correcting them wholesale is not this PRD's job.

## Related documents

- [CONTEXT.md](../CONTEXT.md) — canonical domain glossary (already updated for this feature)
- [ADR 0010](./adr/0010-customer-identity-platform-global-and-separate-from-staff.md) — the identity-model decision
- [ADR 0008](./adr/0008-frontends-authenticate-to-api-with-oidc-id-tokens.md) — why the Storefront needs a BFF
- [ADR 0009](./adr/0009-transactional-email-via-resend.md) — email delivery
- [ADR 0005](./adr/0005-customer-name-split-first-last.md) — first/last name on **Ticket Sale**
- [prd-staff-authentication.md](./prd-staff-authentication.md) — the OTP and session pattern being reused
- [technical-design.md](./technical-design.md) — architecture and engineering agreements
- [testing.md](./testing.md) — integration testing guide
