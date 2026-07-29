# Storefront error copy is chosen by the API's error code, falling back to the API's message

## Status

accepted

## Context and decision

Three design documents state one rule about failures: **display `error.message` as returned by
the API** ([foundation.md](../design/foundation.md), [README.md](../design/README.md), and
several places in [storefront.md](../design/storefront.md)). It was written when the Storefront
spoke one language, and under that assumption it was exactly right: the API decides which
failure occurred, and a UI that re-words its verdict is a UI that can eventually contradict it.

The Storefront now serves two languages, `/en` and `/es`, both prefixed. The Go API answers in
English and is Locale-unaware: no `Accept-Language`, no locale in any request body, no
per-language message table anywhere in `backend/`. So the rule as literally written now says
that a Customer reading a Spanish page must be refused in English.

We have decided that **the Storefront chooses the sentence, and the API chooses which failure it
is about**:

- Envelope errors resolve on `error.code` against the `errors` namespace of the message
  catalogs. A code the catalog knows renders the catalog's copy in the page's language; a code
  it does not know **falls back to `error.message`, verbatim**.
- Field errors under `details.fields[]` resolve the same way one level down, on the stable
  `code` that each `FieldError` now carries beside its message
  (`backend/internal/platform/validation_codes.go`), falling back to that field's own message.
- Where the API itself gives one code two meanings, the key is **code plus surface**.
  `CUSTOMER_SESSION_SCOPE_INSUFFICIENT` is the only such code today: the same refusal, worded
  for the operation refused — "Sign in with a passcode to undo this purchase" against "…to
  change your details". The catalog holds no default for it, so a surface that has not claimed
  it shows the API's own sentence rather than another surface's.
- **The API does not change.** It stays English, stays Locale-unaware, and learns nothing about
  who is reading. Nothing about language crosses the wire in either direction.
- Surfaces that already intercept a code and substitute their own copy keep it: the Payment
  Provider return leg, Confirmation Link redemption, the Google Sign-In callback, the 401 →
  signed-out redirect, and `fetchData`'s swallow-to-null on public reads. Those decided long ago
  that the API's words were not what a Customer should read there, and this changes none of it.

The resolution lives in `apps/storefront/lib/api-errors.ts` — pure, framework-free, and
unit-tested against the real `en.json`.

## Why this way

- **The rule's purpose survives intact, because the Storefront re-renders rather than
  re-judges.** What the rule protects against is the UI and the API disagreeing about *which*
  failure occurred — a page that reads a 409 and guesses "sold out" while the API said the
  Reversal Window closed. Selecting a sentence by the API's own code cannot produce that
  disagreement: the code *is* the API's answer to "which failure", and nothing else selects
  copy. The rule's letter is contradicted; its reason is not.
- **The fallback is what keeps the coupling one-way.** A closed mapping — every code known, an
  unknown one an error — would mean a backend shipping a new code blanks a Storefront alert
  until a matching frontend release lands, and would put every API release in lockstep with a
  Storefront release. Falling back to `error.message` degrades a new code to *exactly* the
  behaviour this app had before any of this existed, which is a working page in English rather
  than a broken one in Spanish.
- **Localizing on the server was the other real option, and it is worse here.** It would give
  the API an `Accept-Language` on every route, a message table per language in Go, and one
  copy surface shared by the Staff app (English-only, operational) and the Storefront (two
  languages, buyer-facing) — the entanglement ADR 0010 spent a whole decision avoiding between
  those two products. The Storefront already owns every other word on its pages; error copy is
  the last thing that was not its.
- **Code plus surface, rather than asking the backend to split the code.** Splitting is the
  tidier contract, but `validation_codes.go` states the rule that argues against it: a code,
  once shipped, means exactly what it meant. The ambiguity here is genuinely about the
  *operation*, which the client knows for certain and the code does not carry — so the client
  is the right place to disambiguate, and it does so by naming the surface at the call site
  rather than by inferring it.
- **The English copy is the API's own wording**, deviated from only for the four handler-layer
  codes whose messages were written for a developer (`INVALID_JSON` — "Request body must be
  valid JSON"). So the English Storefront reads today exactly as it read yesterday, and the
  Spanish catalog starts from a sentence a translator can work with.

## Consequences

- **The catalog is now a second, softer contract with the backend.** A code renamed on the API
  side stops matching and quietly degrades that failure to the API's English message. That is a
  gap nobody will notice: it is invisible in the tests, invisible in the build, and visible only
  as one English sentence on a Spanish page. Nothing here detects it; the fallback is what makes
  it survivable rather than fatal.
- **The API's message and the catalog's sentence for one code can drift apart in wording.**
  Nothing enforces that they read the same, and after the Spanish pass they deliberately will
  not. What is enforced — by construction, since the code is the only key — is that they name
  the same failure.
- **Only Storefront-reachable codes are cataloged.** Twenty-one envelope codes and nine field
  codes, derived from the handlers the Storefront actually calls; the ones already intercepted
  above are deliberately absent, because copy nothing reads is copy that rots. Staff-only codes
  (`EVENT_SLUG_TAKEN`, `LAST_ORG_ADMIN`, the Operator Reversal money memo) are not here at all,
  and the Staff app keeps the verbatim rule unamended.
- **The client-side mirror validators follow the same rule.** `lib/tax-id.ts`, `lib/phone.ts`
  and `validateProfileDraft` answer before the API does. They used to answer with English
  sentences written to match the API's, which would have complained about a mistyped cédula in
  English before the round trip and in Spanish after it — the same verdict, twice, in two
  languages. They now return the API's own codes and resolve through this same catalog, so
  pre-flight and post-flight produce one sentence. A test asserts that equivalence in both
  Locales, because the failure it guards against is silent: the two paths would still each look
  correct on their own.
- **A field error for a field no form drew is still dropped**, code or no code. Resolution
  produces copy for every field the API named; the filter to a form's own inputs stays with the
  form, which is the only thing that knows what it rendered.

This contradicts a rule stated in three design documents, and a reader who finds the code
choosing its own sentences would reasonably think the rule had simply been forgotten — which is
why it is written down.
