# Google Sign-In is a second proof of email ownership, not a second identity

## Context

Signing in to either surface means exactly one thing: proving you own an email address. A **Staff Session**
is a row holding an email, and authority comes from a `members` row matching it; there is no staff user
table at all. A **Customer** is a row keyed `UNIQUE(email)`, created for the person by their first
**Ticket Sale**, and a completed **One-time Passcode** is the only thing in the system that sets
`verified_at`. Email is not a login field that happens to be unique — it is the identity, in both domains
(ADR 0010).

Adding Google Sign-In therefore raises a question the OTP path never had to answer: does an external
provider introduce a *second* notion of who somebody is, alongside the email? Everything else — where the
OAuth exchange runs, how many client registrations exist, how the surfaces stay isolated — follows from
that answer, and from one hard constraint: under ADR 0008 the Go API is not publicly invocable, so no
browser can reach it and Google cannot redirect to it.

## Decision

**Google proves the email; it does not become the identity.** There is no provider table, no `sub` column,
no account linking, and nothing to unlink. A completed Google Sign-In yields Google's verified `email`
claim and from that point runs the *existing* code path: staff get a `sessions` row on that email and meet
the normal auth-fork; customers go through the same create-or-reuse-and-verify step a passcode triggers,
becoming a **Verified Customer** by the same rule. A person may use a passcode on Monday and Google on
Tuesday and land in the same place, because both assert the same fact. A token whose `email_verified` is
false, or which carries no `email`, is refused outright — the claim is the entire value being consumed.

**The Next apps own the browser redirects; the Go API owns the exchange.** ADR 0008 forces Google's
`redirect_uri` onto the Staff and Storefront origins, so those apps mint the `state` and PKCE verifier into
a short-lived httpOnly cookie, bounce the browser to Google, and validate `state` on return. They then hand
the authorization code to the API, which holds the client secrets, calls Google's token endpoint itself,
and decides who the caller is. The frontends never assert an identity; they relay a code that only Google
can turn into one.

**Each surface has its own Google OAuth client.** Staff and Storefront are registered separately, and the
API exchanges a code only against the client that requested it. An authorization code is bound by Google to
its issuing client, so a code obtained through the Storefront cannot be redeemed for a Staff Session — the
isolation `otp_challenges.purpose` gives the passcode path is here a property of the credentials rather
than of a check in application code.

**Email normalisation is untouched.** `NormalizeEmail` remains trim-and-lowercase. Google's canonical
address is treated as an ordinary address, and where it differs from what a box office recorded, the person
is a different Customer.

## Considered options

**Whether Google is an identity or a proof.**

- _A linked-identity table keyed on Google's `sub`_ — the textbook model, and the only one that survives a
  user changing their email address. But it introduces a second answer to "who is this?" that nothing else
  in the system consumes: every read path resolves by email, so the `sub` would be stored and then ignored.
  It also buys a whole surface of product — linking, unlinking, "this Google account is already attached
  to another customer" — for a platform that has no passwords to link to.
- _Proof only (chosen)_ — costs nothing to add and keeps one definition of identity. Accepts that
  Google's `email_verified` is trusted as equal to our own passcode, which holds for Google specifically
  and would need revisiting for a provider that verifies loosely.

**Where the OAuth exchange runs.**

- _The API hosts the callback_ — impossible without making the API publicly invocable, which ADR 0008
  exists to prevent.
- _The frontend completes the dance and tells the API which email signed in_ — the least code, and the
  worst failure mode: that endpoint mints a session for any email its caller names, and the only thing
  standing between a frontend bug and total account takeover is the service-to-service token. Rejected on
  blast radius.
- _The frontend exchanges the code and forwards the ID token for the API to verify against Google's JWKS_ —
  keeps the trust decision in Go, but puts the client secrets in two more services and requires signature
  verification code.
- _The frontend relays the code; the API exchanges it (chosen)_ — secrets stay with the API's other
  secrets, the identity decision sits beside `VerifyOTP`, and because the ID token arrives directly from
  Google's token endpoint over TLS, OIDC permits skipping signature verification entirely. Costs an extra
  hop on callback and splits responsibility: CSRF state belongs to Next because only Next can set browser
  cookies, identity belongs to Go.

**How many OAuth clients.**

- _One client with both redirect URIs_ — one secret to manage, but cross-surface isolation then rests on
  the API correctly honouring a `surface` value the frontend supplied, which is the same trust this ADR
  rejected above.
- _One per surface (chosen)_ — two secrets to manage, and no separate consent-screen branding (that is
  project-wide), but a leaked Storefront secret cannot reach staff, and the isolation cannot be lost to a
  refactor.

**Whether to canonicalise Gmail aliases.**

- _Fold dots and `+suffixes` in `NormalizeEmail`_ — would reunite the buyer who gave a box office
  `alice+tickets@gmail.com` with the Google account `alice@gmail.com`. Rejected: it changes the identity
  key for every existing row, requires a migration that *merges* Customers with no principled rule for
  which name or `verified_at` survives, and is simply wrong outside Gmail, where dots are significant.
  Rewriting the foundation of Customer identity to smooth one sign-in path is the wrong trade.
- _Leave it (chosen)_ — the fragmentation is a pre-existing property of email-as-identity, not something
  Google introduces; a passcode sent to `alice+tickets@gmail.com` already mints its own Customer today.

## Consequences

- **A changed email address is a new person, on both surfaces.** The linked-identity option would have
  carried someone across a Gmail rename; this does not. It matches today's behaviour, so nothing
  regresses, but it is now a decision rather than an oversight.
- **Some people will hit a dead end that looks like a bug.** A Customer whose sales were recorded under a
  different address sees an empty **Customer Area**; a **Member** invited at one address who signs in with
  another is offered the create-an-organization fork instead of their Organization. Both are correct and
  both look broken, so the empty states carry the explanation and point at signing in with the other
  address. This is the accepted cost of the normalisation decision and the most likely source of support
  questions.
- **The passcode rate limits are no longer the only door.** Per-email and per-IP issue limits and the
  global outbound ceiling all throttle a path that sends email; Google Sign-In sends none, so a
  `customers` row — and on Staff, a session able to create an **Organization** — can now be obtained
  without touching them. What that yields is still an inert record for an address the person controls, so
  it is bounded by the cost of a Google account rather than by our limits. If Organization spam ever
  matters, the control belongs on Organization creation, not on sign-in.
- **The API gains a configurable token endpoint, which is a live footgun.** Integration tests point it at
  a stub; anyone able to set that variable in production could make the API trust an issuer they control,
  which is unrestricted account takeover. It must therefore be refused when `AppEnv` is production, the
  way the dev `StorefrontBaseURL` fallback is already called out.
- **Two more secrets and a Google Cloud project surface to operate.** Client secrets join `ResendAPIKey`
  and `ConfirmationLinkSecret` in Secret Manager, and redirect URIs become deploy-time configuration that
  must match Google's registration exactly — a class of failure invisible until someone clicks the button
  in production, hence a first-deploy runbook.
- **Auth.js/NextAuth is now firmly excluded.** It wants to own the session; sessions here are server-side
  rows minted by the API. Adopting it later would mean two session concepts, so the ~150 lines of redirect
  handling are written by hand deliberately.
