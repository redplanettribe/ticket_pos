# PRD: Google Sign-In

## Problem Statement

Every sign-in on this platform costs a trip to a mailbox.

Both surfaces prove identity the same way: type an email, wait for a **One-time Passcode**, switch to a mail client, read six characters, switch back, type them in. It works, it is the reason there are no passwords to leak, and it is not going anywhere. But it is the slowest possible way to answer a question most people's browsers can already answer instantly.

The cost falls hardest where it is least affordable. A **Customer** arriving at the Storefront is a consumer on a phone, months after buying, trying to find out what time the doors open. They are one tap from proving who they are — every Android phone and most desktop browsers hold a live Google session — and instead they are sent to their inbox, which on a phone means leaving the browser and hoping to find their way back. Some fraction simply do not return. The **Customer Area** they were reaching for is the whole point of `prd-customer-login.md`, and the passcode is the toll gate in front of it.

On the Staff app the cost is smaller but constant. A **Member** signs in on a fourteen-day cycle, at work, often on a Google Workspace address that their browser is already authenticated against, and pays the mailbox round-trip anyway.

Nothing about the identity model needs to change to fix this. Signing in already means one thing and one thing only — proving you own an email address — and Google will assert exactly that fact, for free, in a form we can verify.

## Solution

Accept **Google Sign-In** as a second **Proof of Email Ownership**, equal in force to a passcode, on both surfaces.

Google is *not* a second identity. There is no linked account, no provider record, no `sub` stored anywhere, and nothing to unlink. The flow ends by producing a verified email address, and from that point runs the code the passcode path already runs: staff get a **Staff Session** on that email and meet the ordinary auth-fork; customers are created-or-reused on the normalised email and become a **Verified Customer** by the same rule that has always made one. Somebody can sign in by passcode on Monday and by Google on Tuesday and arrive in the same place, holding the same history, because both acts assert the same fact. ADR 0011 records why, and what the alternative would have cost.

This means the feature **adds no tables and no columns**. It is a second front door onto one identity path.

Because the Go API is not publicly invocable (ADR 0008), Google cannot redirect a browser to it. The Staff and Storefront apps therefore own the browser-facing halves of the flow — minting the CSRF `state` and PKCE verifier, bouncing to Google, validating `state` on return — while the API holds the client secrets, exchanges the authorization code with Google itself, and decides who the caller is. The frontends never assert an identity; they relay a code that only Google can turn into one. Each surface has its own Google OAuth client, so a code obtained on the Storefront is not redeemable for a **Staff Session** — the isolation `otp_challenges.purpose` gives the passcode path, expressed here in the credentials themselves.

The passcode stays, fully supported, on both surfaces. Staff are routinely invited at addresses with no Google account behind them, and Customer records are minted wholesale from box-office spreadsheets; removing the passcode would strand both populations. What changes is prominence: on the Storefront, Google leads. On Staff, it sits below the email form — deliberately, because that is the surface where signing in with the wrong address has the worse outcome.

That outcome is the one thing this feature makes more visible, and it is not new. Google returns an account's canonical address; a person whose sale was recorded as `alice+tickets@gmail.com` arrives as `alicesmith@gmail.com` and is, correctly and unavoidably, a different **Customer**. Email is the identity; that is what email-as-identity means, and a passcode sent to the same alias fragments a person's history in exactly the same way today. Rather than rewrite the normalisation rule that the platform-global Customer rests on, this PRD makes the failure legible: the empty states say so and point at signing in with the other address.

## User Stories

### Signing in as a Customer

1. As a Customer with a Google account, I want to sign in to the Storefront with one tap, so that I can see my tickets without leaving the browser for my inbox.
2. As a Customer, I want the Google option to be the first thing I see on the sign-in page, so that I do not scan a form before finding the faster path.
3. As a Customer without a Google account, I want the passcode form still present and fully working on the same page, so that nothing I could do before has been taken away.
4. As a Customer who has never bought anything, I want Google sign-in to work anyway and land me in an empty **Customer Area**, so that the outcome matches what a passcode would have given me.
5. As a Customer whose record was created by a box-office sale, I want Google sign-in to make me a **Verified Customer**, so that a passcode is not a separate step I must also complete.
6. As a Customer signing in from an Event page, I want to be returned to that page afterwards, so that the round-trip through Google does not lose my place.
7. As a Customer with several Google accounts, I want to be asked which one to use, so that I am not silently signed in as the wrong person.
8. As a Customer who arrived by **Confirmation Link** and then signs in with Google, I want my full history rather than the one sale, so that the wider proof gives the wider access.

### Signing in as a Member

9. As a Member with a Google or Workspace account, I want to sign in to the Staff app with one tap, so that the fourteen-day cycle stops costing a mailbox trip.
10. As a Member, I want the email form to remain the first thing on the login page, with Google below it, so that the flow I know is unchanged.
11. As a Member signing in with Google, I want to land exactly where a passcode would have left me — my dashboard, the organization picker, or organization creation — so that the sign-in method never changes where I end up.
12. As a Member of one Organization, I want it selected automatically after Google sign-in, so that the auto-select behaviour is not lost on the new path.

### When the address does not match

13. As a Customer who signs in with Google and sees no purchases, I want to be told that a different email address may hold them and offered a way to sign in with it, so that an empty page is not a dead end.
14. As a Member who signs in with Google and finds no Organization, I want to be told that an invitation may have gone to a different address, so that I do not create a duplicate Organization by mistake.
15. As a person in either position, I want the alternative sign-in to be one click away from the message, so that recovering does not mean understanding the problem first.

### Isolation and safety

16. As a Customer, I want Google sign-in on the Storefront to grant nothing on the Staff app, so that the two identities stay as separate as they are today.
17. As a Member, I want the reverse to hold too, so that a Storefront compromise cannot reach organization management.
18. As a security reviewer, I want the authorization code from one surface to be unusable on the other *by construction* rather than by an application-level check, so that the property survives refactoring.
19. As a security reviewer, I want the frontends to be incapable of naming which email signed in, so that a frontend bug cannot mint a session for an arbitrary address.
20. As a security reviewer, I want a Google account whose email is unverified to be refused, so that the only claim we consume is one Google vouches for.
21. As a security reviewer, I want no browser to address the Go API, so that ADR 0008 continues to hold on both surfaces.
22. As a Customer, I want my session cookie to remain inaccessible to page scripts on this path too, so that the new flow does not weaken what the passcode flow guarantees.
23. As an anonymous visitor, I want a failed or abandoned Google sign-in to tell me nothing about whether my email is known to the platform, so that the anti-enumeration property of the passcode path is not lost.

### When it goes wrong

24. As a person who changes their mind at Google's consent screen, I want to come back to the sign-in page with the passcode form ready, so that cancelling is not a dead end.
25. As a person whose sign-in took too long to complete, I want a plain "please try again" and a working page, so that an expired attempt is recoverable without understanding what expired.
26. As a person hitting any failure at all, I want one generic message rather than a diagnosis, so that error text is not an oracle.
27. As a developer, I want every failure mode to land the user on the ordinary sign-in page rather than an error page, so that there is always a way forward.

### Operations

28. As a platform operator, I want client secrets held only by the API, so that they live where the other secrets live and not in two more services.
29. As a platform operator, I want the Google token endpoint pinned to Google in production and overridable only outside it, so that no environment variable can redirect who the platform trusts.
30. As a platform operator, I want a first-deploy runbook, so that redirect-URI and consent-screen mistakes are caught deliberately rather than by a user clicking a button.
31. As a platform operator, I want Google sign-in to use the standard envelope (`data`, `error`, `request_id`) and to be versioned under `/api/v1/`, so that it is consistent with every other route.
32. As a developer running the stack locally, I want a documented way to exercise the button without production credentials, so that the feature is developable.

## Implementation Decisions

Each decision below was settled in a grilling session; the trade-offs and rejected alternatives for decisions 1, 2, 3 and 5 are recorded in ADR 0011.

### 1. Google proves the email; it is not an identity

No new table, no new column, **no migration**. The flow's only output is a verified email address, which is handed to the same service methods the passcode path uses.

On Staff, that is the existing session-minting step: a `sessions` row on the normalised email, followed by the ordinary auth-fork. On the Storefront, it is `VerifyCustomer(email, now)` — the same call `VerifyOTP` makes, with the same create-or-reuse-and-stamp semantics — so a Google sign-in for an address no sale has reached mints exactly the record a first sale would have, and a Google sign-in for an unverified record is what makes that person a **Verified Customer**.

A token with `email_verified` false, or with no `email` claim, is refused. The claim is the entire value being consumed, so there is no degraded mode.

**Accepted consequence:** the platform trusts Google's verification as equal to its own passcode. That holds for Google; it is not a general licence to add providers to this path. A provider that verifies loosely would need its own decision.

### 2. The frontends redirect; the API exchanges

Under ADR 0008 the API is not publicly invocable, so Google's `redirect_uri` must be a Next route on each app's own origin. The split of responsibility is:

**The Next app** (`/api/auth/google/start` on Staff, `/api/customer/auth/google/start` on the Storefront) generates a random `state` nonce and a PKCE code verifier, writes both — plus the post-sign-in destination — into a short-lived httpOnly cookie, and redirects to Google's authorization endpoint with `prompt=select_account`.

**Google** returns the browser to `/api/auth/google/callback` (respectively `/api/customer/auth/google/callback`), which compares the returned `state` against the cookie, deletes the cookie, and POSTs `{code, code_verifier, redirect_uri}` to the API.

**The API** (`POST /api/v1/auth/google/verify`, `POST /api/v1/customer/auth/google/verify`) exchanges that code at Google's token endpoint using the surface's client secret, reads `email` and `email_verified` from the returned ID token, mints the session, and returns the session token in the standard envelope. The Next route writes it into the existing session cookie — `ticket_pos_session` or `ticket_pos_customer_session`, with the attributes and lifetimes those already have — and redirects the browser to the destination.

The ID token arrives on a direct TLS connection from Google's token endpoint, which under OIDC means its signature need not be independently verified; no JWKS fetching, caching, or key-rotation handling is written.

**Accepted consequence:** CSRF state belongs to Next because only Next can set browser cookies, and identity belongs to Go. That split is deliberate and should not be "tidied up" by moving either half.

### 3. One Google OAuth client per surface

Two registrations, `staff` and `storefront`, each with exactly one authorized redirect URI. The API exchanges a code only against the client that issued it. Google binds an authorization code to its issuing client, so a Storefront code presented at the staff endpoint fails at Google with `invalid_grant` — cross-surface isolation is a property of the credentials, not of a check in the handler.

The OAuth consent screen is project-wide, so this yields **no** per-surface branding. The win is isolation and blast radius only.

### 4. Requested scope is `openid email` — not `profile`

Google can return `given_name` and `family_name`, and it is tempting to use them to fill in a nameless Customer. This PRD does not.

`prd-customer-login.md` decisions 42–44 give profile names a precedence rule of their own: a later sale may improve an unverified Customer's name, and once verified nobody may rename them. Feeding a third source into that rule is a separate decision with its own edge cases, and it is not needed to sign anybody in. Requesting the narrower scope also means a shorter consent screen, which is worth something on the surface where conversion is the point.

**Accepted consequence:** a Customer who first appears via Google Sign-In has no name until a **Ticket Sale** gives them one, exactly as a passcode-first Customer does today.

### 5. Email normalisation is untouched

`NormalizeEmail` stays trim-and-lowercase. Google's canonical address is an ordinary address.

Where it differs from what a box office recorded, the person is a different **Customer**, and on Staff a person with no matching `members` row. This is not a Google problem — a passcode sent to `alice+tickets@gmail.com` fragments a history identically today — and the alternatives (folding Gmail dots and `+suffixes`, or a post-hoc merge) either rewrite the identity key of every existing row or collapse back into a passcode. ADR 0011 records the reasoning.

The mitigation is copy, in two places:

- The empty **Customer Area** (story 28 of `prd-customer-login.md`) currently points at the explorer. It gains a second line: bought with a different address? sign in with that one — linking straight to `/signin`.
- The staff no-memberships fork currently offers only "create an organization". It gains a line noting an invitation may have been sent to a different address, linking back to `/login`.

**Accepted consequence:** some people hit a dead end that looks like a bug. The copy is what makes it recoverable; it is load-bearing, not decoration.

### 6. Google leads on the Storefront, follows on Staff

Both sign-in surfaces are a two-step `AuthCard` (email → code) today.

On the **Storefront**, the Google button goes above the form, with a divider and "or continue with email" below. The population is consumers on phones with a live Google session, and the conversion difference between one tap and a mailbox round-trip is the reason this feature exists.

On **Staff**, the email form stays first and Google sits below it. Members are frequently invited before they ever sign in, and decision 5's failure mode is worse here — a mismatch offers organization *creation*. Leading with Google would maximise how often people meet that.

**Accepted consequence:** Google Sign-In is not subject to the passcode's per-email and per-IP issue limits or the global outbound ceiling, because it sends no email. A `customers` row, or a staff session able to create an **Organization**, can now be obtained without touching those controls. What that yields is an inert record for an address the actor already controls, bounded by the cost of a Google account. If Organization spam becomes real, the control belongs on Organization creation.

### 7. Session semantics are unchanged

Fourteen days sliding on Staff, one hundred and eighty on the Storefront. The same cookies, the same attributes, the same server-side rows. **Nothing records how somebody signed in** — no column, no field on the session view, nothing in the UI. Adding one would imply the two methods differ in force, and decision 1 is that they do not.

Signing out kills the session row and clears the cookie. It does not revoke anything at Google and does not sign the person out of Google.

### 8. The state cookie

One httpOnly, `Secure`-in-production, `SameSite=Lax` cookie, path-scoped to the callback route, holding the `state` nonce, the PKCE verifier, and the post-sign-in destination. Ten-minute lifetime. Deleted on callback, whatever the outcome.

The destination travels **inside** this cookie, never in Google's `state` parameter — it stays on our origin, out of Google's logs, and beyond tampering. The Storefront's existing `safeNext` guard still runs when it is read, so a relative-path check protects the redirect even if the cookie were somehow forged.

### 9. Every failure lands on the sign-in page

`access_denied` from the consent screen, a `state` mismatch, a missing or expired state cookie, a failed exchange, `email_verified` false, a missing `email` claim: all redirect to `/signin` or `/login` with one generic message and the passcode form intact.

No failure distinguishes "this email is unknown" from any other. The passcode request endpoint is carefully non-committal about whether an address is known (`prd-customer-login.md` decision 10); a Google path that answered the question would give back the oracle that endpoint spends effort denying.

### 10. Configuration

The API gains `GOOGLE_STAFF_CLIENT_ID`, `GOOGLE_STAFF_CLIENT_SECRET`, `GOOGLE_STOREFRONT_CLIENT_ID`, `GOOGLE_STOREFRONT_CLIENT_SECRET`, all through Secret Manager alongside `ResendAPIKey` and `ConfirmationLinkSecret`, plus a `GOOGLE_TOKEN_ENDPOINT` defaulting to Google's real endpoint.

**`GOOGLE_TOKEN_ENDPOINT` is refused when `AppEnv` is production** — startup fails loudly if it is set — in the spirit of the existing `StorefrontBaseURL` warning. Anyone who could point it elsewhere could make the API trust an issuer they control, which is unrestricted account takeover; it exists for tests and for nothing else.

Each frontend gets its own client ID and redirect URI as ordinary server-side environment variables. Client IDs are public information, and the `/start` route is a server route handler, so no `NEXT_PUBLIC_` exposure is needed. **No frontend ever holds a client secret.**

### 11. Documentation

Four documents, of which this is one: ADR 0011, CONTEXT.md (the **Signing in** section, plus the **Staff Session**, **Customer Session** and **Verified Customer** entries, which said "passcode" where they meant "proof"), and `manual-verification-google-sign-in.md`.

## Testing Decisions

### Primary seam: no new seams

`docs/testing.md` makes HTTP integration the default, and this feature introduces nothing that changes that. All decisions worth testing — client isolation, `email_verified`, session minting, verification semantics — are observable at `POST /api/v1/auth/google/verify` and `POST /api/v1/customer/auth/google/verify`.

The one piece of test infrastructure is a **stub token endpoint**: an `httptest` server returning canned ID tokens, pointed at through `GOOGLE_TOKEN_ENDPOINT`. This is why that variable exists.

### Scenarios

In `backend/integration/`, extending `auth_test.go` and the customer auth tests:

1. A valid staff exchange mints a **Staff Session** on the token's email.
2. A valid customer exchange mints a **Customer Session** and stamps `verified_at`.
3. A customer exchange for an address with existing sales returns those sales — the record is reused, not duplicated.
4. A customer exchange for an unknown address creates the Customer and returns an empty area.
5. A customer exchange for an address differing only in case reuses the same record.
6. `email_verified: false` is refused; no session, no row written.
7. A token with no `email` claim is refused.
8. An exchange rejected by Google (`invalid_grant`) is refused with the generic error, and the response is indistinguishable from a refusal for a known address.
9. A Google-minted session behaves identically to a passcode-minted one on a subsequent authenticated read, including sliding expiry.
10. A Customer signed in by Google can be signed out, and the session is dead afterwards.

Cross-surface isolation (decision 3) is enforced by Google, not by our code, so it cannot be asserted against a stub. It is verified in the runbook instead, and this is stated rather than papered over with a test that proves only that the stub was configured twice.

### Frontend

The redirect plumbing — `state` generation and comparison, cookie handling, `safeNext` — is unit-tested in each app, in the style of `auth-fork.ts`. `resolveAuthForkPath` is reused unchanged on the Staff callback and needs no new coverage.

### E2E

None. The browser round-trip cannot be driven without either Google or a fake identity provider, and `docs/testing.md` keeps `e2e/` to thin cross-runtime journeys. Both existing sign-in smoke tests must continue to pass, proving the passcode path was not disturbed.

### Existing coverage that must not regress

The whole of `auth_test.go` and the customer auth suite. No existing behaviour changes; if any of it moves, something has been touched that should not have been.

### Manual

`manual-verification-google-sign-in.md`, run at first deploy of each surface. It covers what no automated layer can: the real consent screen, exact redirect-URI matching, and the cross-client rejection.

## Out of Scope

- **Any second provider.** Apple, Microsoft, Facebook. The path is not generalised; a `googleauth` package is written, not a provider abstraction. Decision 1's trust argument is Google-specific.
- **Account linking, and any UI for it.** There is nothing to link.
- **Storing `sub`, or anything else Google returns.** Rejected in ADR 0011.
- **Names from Google.** Decision 4.
- **Alias canonicalisation and Customer merging.** Decision 5.
- **Google Workspace domain restriction (`hd`).** No Organization has asked to restrict staff sign-in to a domain; when one does, it is an Organization setting, not a property of this flow.
- **Signing out of Google, or revoking Google's tokens.** Decision 7.
- **Recording sign-in method anywhere.** Decision 7.
- **Rate limiting the Google path.** Decision 6 states the consequence; no control is added now.
- **A fake identity provider in the Compose stacks.** Considered, and left for later if local-development friction proves real.

## Further Notes

### Sequencing

One initiative, staged: **Storefront first, Staff shortly after.** The shared Go work — the `googleauth` package, the exchange, the config, the Secret Manager wiring — lands with the Storefront and is exercised against real Google traffic before anything touches the staff door, which is the surface where a mistake reaches organization management and sales.

There is **no feature flag.** The staging is "merge and deploy one half first", not runtime configuration; a flag here is machinery that would outlive its purpose.

### Known limitations, accepted knowingly

- A changed email address makes somebody a new person. A linked-identity model would have carried them across; this does not, and neither does today's passcode.
- Alias mismatches produce a dead end that looks like a bug. Decision 5's copy is the whole mitigation.
- Google Sign-In sits outside the passcode rate limits (decision 6).
- Every developer needs Google credentials to click the button locally. This is the accepted cost of not building a fake identity provider, and the strongest argument for revisiting that.

### Local development

A separate Google OAuth client, registered with `http://localhost` redirect URIs for both apps — Google permits plain HTTP on localhost. Credentials go in each developer's `.env`; the stack starts and every non-Google path works without them. The Google button, absent credentials, is hidden rather than broken.

## Related documents

- ADR 0011 — Google Sign-In as proof of email ownership
- ADR 0010 — Customer identity: platform-global and separate from staff
- ADR 0008 — Frontends authenticate to the API with OIDC ID tokens
- `prd-customer-login.md` — the Customer identity this rides on
- `prd-staff-authentication.md` — the Staff Session this rides on
- `manual-verification-google-sign-in.md` — first-deploy runbook
- `CONTEXT.md` — **Signing in**
