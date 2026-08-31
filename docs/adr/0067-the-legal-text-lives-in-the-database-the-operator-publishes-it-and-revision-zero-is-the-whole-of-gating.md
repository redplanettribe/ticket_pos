# The legal text lives in the database, the operator publishes it, and `revision = 0` is the whole of gating

Specified as issue #556, from the wayfinder map #540 (sub-questions #541–#555).
**Reverses** [ADR 0036](./0036-the-privacy-policy-is-a-backend-artifact-the-storefront-renders.md)'s
"Publishing an edition is a backend deploy" while keeping the rest of it, including the ruling that a
Locale the edition does not publish is a 404 and never another language.
**Amends** [ADR 0066](./0066-the-terms-are-accepted-twice-as-attendee-on-the-storefront-as-organizer-before-a-staff-session.md)
on delivery — "the gate binds at next sign-in" becomes "the gate binds on the live session" — and
**confirms** its ruling that Terms acceptance has no withdrawal path.
**Departs** from [ADR 0006](./0006-offset-pagination-house-convention.md) for the acceptance lists,
with the reason recorded below.
Leaves [ADR 0027](./0027-preset-tag-copy-lives-in-the-storefront-catalog.md) untouched.

## Context

The platform's two legal documents — the Privacy Policy and the Términos y Condiciones — are Markdown
files compiled into the Go binary, one directory per Locale, exactly as ADR 0036 decided and ADR 0066
extended. Three consequences of that have come due at once.

**The operator cannot change the text.** A typo fix is a Go commit, a pull request and a manual
production deploy. Since 2026-08-28 GitHub Actions has refused every CI and Deploy run for payment, so
the legal text of a live ticketing platform is presently unchangeable *by anyone*. Even without that,
the one person legally responsible for these documents cannot alter a word without a developer.

**The operator cannot see who owes acceptance.** Of 1,569 Customers, 1,058 hold the current Policy
edition, 511 have never accepted anything and 11 sit on `0-placeholder`. None of those numbers is
visible in the product; they were read with SQL against a copy of production. There is no screen that
answers "who has not accepted the current Terms" for either population.

**The operator cannot answer a subject.** A person asking what they consented to, or a regulator asking
the platform to evidence a consent, is served by an operator reading rows in psql. The one existing
surface, `/operator/consent`, takes an email *in the request line*, shows no history and no editions,
produces no document, and is linked from nowhere.

Underneath all three sits an evidence problem: every acceptance fingerprints an edition by
`content_hash`, and those hashes were rewritten in place as the seed migrations were edited, so the
same edition label carries different fingerprints in different databases. Production's are all
reproducible; CI's and a fresh developer's are not.

ADR 0066 closed by naming the gap this fills: "The legal center — public version history, downloadable
copia fiel — is §34 machinery this decision does not build."

## Decision

An operator-only **Legal Center** at `/operator/legal`, where the legal text lives in the database and
one person authors, publishes, browses and evidences it. Every ruling below was already made in
#541–#555; this record puts them beside the screen rather than in a migration comment.

**The text moves into the database, and publishing stops being a deploy.** Two parallel child tables,
`policy_version_artifacts` and `terms_version_artifacts`, one row per `(version_id, locale, slug)` with
an `ordinal` that turns the hash preimage order from hardcoded Go field order into data. The embedded
`artifacts/` directories and both `seed_test.go` files are deleted in the same commit, so there is
never a second copy of the legal text to drift. One forward-only migration carries the bytes as SQL
literals — not by reading `embed.FS`, because a migration that reads the binary can only ever run
against a binary that still ships the artifacts — and refuses to apply unless `sha256(stored rows)`
equals the `content_hash` the version row already carries. It proves the copy faithful **against the
row, not against the artifacts**, because the row's hash is what every acceptance actually points at,
and it never rewrites a hash a row already holds.

**This reverses ADR 0036's "Publishing an edition is a backend deploy", and only that.** Everything
else that decision ruled survives, for its original reasons:

- The Policy, its Short Notice and the consent checkbox labels remain **one edition, hashed across
  every published Locale at once**, and remain evidence rather than copy — so they stay out of the
  message catalogs. The catalog still holds the page's own chrome.
- **The API never falls back to another language.** `GET /api/v1/public/privacy-policy/{locale}` and
  `GET /api/v1/public/terms/{locale}` keep their shape: "the two languages are two addresses, each
  cacheable and each a 404 in its own right". A Locale the current edition does not publish is a 404,
  and the footer links *across* to Spanish rather than serving Spanish under an English address.
- **The Policy and the checkboxes still cannot come from different editions.** The service cache holds
  one atomic entry per `(document, locale)` — version id, label, effective date, content hash and every
  string of that Locale's text, filled by a single read. That, not the 60s TTL, is the load-bearing
  part: the failure being designed out is a capture surface rendering cached *text* while the
  acceptance path separately re-reads the *current version row*.
- **A Storefront that cannot reach the API still has no Privacy Policy page** — but it now says so
  correctly. A 404 from the API means the Locale is unpublished and stays a `notFound()`; a 5xx,
  unreachable or malformed read **throws**, and the response is a 500. Answering "we publish no privacy
  policy" when the platform does is the one false statement in this area that is expensive to have
  made. ADR 0036's ruling survives; it stops being implemented by a mechanism that lies.

**ADR 0027 is untouched.** The public routes keep their shape, so the API gains no `Accept-Language`,
no locale parameter and no new localized surface; it has exactly the one localized route ADR 0036 gave
it, with the Locale still a path segment. The rest of the API stays as Locale-unaware as it was.

**Status and kind are answered separately from the label, and `revision = 0` is the whole of "gating".**
`generation` and `revision` are integers on both version tables; `label` is their frozen `UNIQUE`
rendering — `2` for a gating edition, `1.1`, `1.2` flat within a generation for a correction, generated
by the system and never typed. The label **carries lineage only**: reading `1.1` tells you what it
descends from and nothing about whether it gates. An edition is gating **iff `revision = 0`**, and its
gating date is its own `effective_date`. There is **no `is_gating` column, no `gating_from` column and
no `published_as` column** — `revision > 0` *is* "published as a correction". #551 introduced
`gating_from` to express a delayed promotion and #553 withdrew both by removing promotion, which
collapsed the column with it. `content_hash` keeps its format CHECK and **must never gain a `UNIQUE`**:
byte-identical editions are legal, and are the mechanism below.

**There is no promotion.** A correction is non-gating permanently and by construction. An operator who
judges a correction material after all publishes a **new gating edition over the same text**, through
the publish path that already exists. The two acts are identical in consequence — both re-gate the same
population, and no gate in the codebase can tell them apart — and differ in exactly one respect:
promotion is the only act in the design that would **mutate a row an acceptance points at**. It was the
sole exemption from the design's own append-only rule, and it is removed. The gating fact is therefore
not merely monotonic but **immutable**, which is the stronger property. The cost is one extra version
row and its artifact children, around 30 KB of Markdown.

**Outstanding is membership in a satisfying set**, not equality against the current edition: the gating
floor is the newest row with `revision = 0 AND effective_date <= CURRENT_DATE`, and the satisfying set
is that edition and every edition at or above it, corrections included. A correction re-gates nobody
for free, with no backfill and no column to maintain. Two people holding two different editions can
both be Current, and each person's evidence still resolves to the exact bytes they accepted.

**The gate binds on the live session — this amends ADR 0066.** That decision ruled "Live sessions are
not revoked; the gate binds at next sign-in", which read as a bounded delay because a Staff Session is
a fortnight. It is not bounded: the session's expiry **slides on every authenticated request**, so a
person who uses the platform daily never signs in again and never meets the gate. The rule was
unbounded, not a fortnight. In its place the staff gate becomes a **live-session check**: a
read-and-accept interstitial on the next page navigation, session untouched, zero passcodes, re-gated
within a minute. `apps/staff/middleware.ts` already short-circuits `/api/`, so the gate binds on
navigation only — an in-flight mutation is never refused and a sale in progress commits. Because the
re-gate lands at a midnight `effective_date` rollover rather than at the operator's click, the
interstitial is met at the start of a shift in the ordinary case.

**A publish revokes no session, in either population, and there is no control to do so even as an
option.** Not for difficulty — both session kinds are server-side rows. It buys nothing from Customers,
who are re-gated on a live session at begin-checkout with the in-flight checkout pinned to the edition
it began under; and revoking the 25 staff would force 25 passcodes against a limit of 10 per IP per 15
minutes, sharing a platform ceiling with Sale Confirmations by design, so a publish mid-event would
lock the door *and* stop buyers' mail.

**Terms acceptance and Policy Acceptance are both non-withdrawable — this confirms ADR 0066 rather than
amending it.** ADR 0066 ruled that "Terms acceptance is contractual and has no withdrawal path", and
the per-subject record offers no *Withdraw Terms* control because that act is a category error: a
contract's basis is performance, not consent. Policy Acceptance is likewise non-withdrawable, exactly
as the existing rules already enforce. **Only Marketing Consent and Networking Consent are ever
withdrawable**, and the existing `/operator/consent` withdrawal folds into the per-subject screen
rather than living on beside it. The screen offers exactly two acts — withdraw an optional consent, and
generate an Evidence Pack. There is no control that manufactures an acceptance, no per-person re-gate,
and **no erasure control**: a deletion request escalates to counsel, and `ON DELETE RESTRICT` from
`consent_records` and `consent_access_log` makes a Customer deletion fail loudly rather than silently
orphan the record.

**The acceptance lists are keyset-paged, which departs from ADR 0006, and the reason is the departure.**
The house convention for staff back-office lists is offset/limit with a
`{data, pagination:{page,page_size,total,total_pages}}` envelope and `COUNT(*) OVER()`, and ADR 0006
explicitly considered and rejected keyset for these surfaces — because it "cannot return a total or
support jump-to-page (both of which *are* the back-office UI)", and because its one real advantage,
deep-offset performance, "only pays off at millions of rows in a single result set, whereas a single
Event's sales are bounded in the low tens of thousands". Neither half of that argument holds here:

- **The result set is the entire Customer base, at exactly the wrong moment.** Immediately after a
  re-gate every Customer is outstanding, so the default filter selects all 1,569 today and everyone the
  platform ever signs up thereafter. Offset paging goes quadratic precisely when the screen matters
  most — the morning after a publish, which is the only morning anyone opens it.
- **The total is the expensive half of the query and the least actionable number on it.** `COUNT(*)
  OVER()` over the whole outstanding set buys a headline that reads "everyone" on the day it is read
  and is chased one person at a time regardless. Jump-to-page over a set nobody paginates by position
  is worth still less.

So: the acceptance lists are keyset on `email ASC`, page size 50, **no total**; the capture history is
keyset on `(captured_at DESC, id)`, page size 25, served by the existing
`(customer_id, captured_at DESC)` index; the staff acceptance history is unpaged. The cursor follows the
public event feed's prior art — base64 RawURL, an unparseable cursor treated as absent. **ADR 0006
stands unchanged for every other staff list**; this is a departure for these surfaces and their stated
reason, not a new house convention. It is written down here because without it the next reader will
"fix" the lists back to the envelope and reintroduce the quadratic.

**A data subject's email never appears in a URL, a query string or a referer.** A Customer is addressed
by UUID; a staff person — who has no id anywhere, because a staff person is an email in migrations 067,
069 and 107 alike — is addressed by an **HMAC digest**: 32 hex chars of
`HMAC-SHA256(key, "staff:" + NormalizeEmail(email))`, under a purpose-derived subkey of
`CONFIRMATION_LINK_SECRET` bound to the string `"legal-staff-digest.v1"`, so there is no new secret and
no `terraform apply`. The screens refuse to serve rather than fall back to an empty key.

**Standing rule: the digest is a URL key and a screen label, and is never written to a row, a log, a
file or an export.** Persisting it would turn a key rotation into a migration over append-only
evidence, and would orphan documents whose whole purpose is to stay meaningful for years. This is why
`consent_access_log` records a staff subject as a plain email, and why the Evidence Pack must not carry
the digest anywhere in its contents. The two existing `{email}` operator routes are **deleted** the day
the Legal Center lands; list search absorbs the one-step lookup, and the address travels in a POSTed
search or a client-side filter, never in a path.

**No act has an approval step, and the overnight delay is the substitute for a second pair of eyes.**
Production holds exactly one `platform_operators` row, with no roles and no second tier, so a
two-person rule deadlocks on every act — and recruiting an approver would also hand them payouts,
reversals and the invoicing backfill, because operator authority is one boolean (ADR 0015). What is
irreversible here is not the text but the **re-gating**, so the delay lands only on acts that move the
gating floor: a **gating publish cannot take effect the day it is made**, must be dated at least
tomorrow, and is cancellable overnight by the same one person, ungated and immediately. A correction
takes effect at once; urgency is served by *correct now, publish a gating edition effective tomorrow*.
A cancelled edition is retained and marked, never deleted.

**The substitute for review is proving the operator was shown the consequence**, not that anyone
agreed. Publishing is refused until the draft is complete in every published language, every artifact
has been previewed, and the diff against the current edition has been seen; the confirm button names
the label it would create and carries the headcount it is about to re-gate; a correction requires a
typed reason and is refused outright over a structural change, over a locale-set change and over an
empty diff. The version row then records who published, when, the diff summary, the typed reason and
**the headcount as it stood on the button**. Provenance lives on the immutable row the evidence already
points at, not in a log that can drift from it.

**One append-only `consent_access_log` records the operator's own reads** — `list_read`, `subject_read`,
`evidence_export`, `audit_read` — because the house pattern hangs attribution off a domain row and a
*read* has no domain row. A list read records the question (filter and result count) and never the
roster; `search_term` is a boolean, so searching for an email does not accumulate emails in a log;
reading the log is itself logged; a withdrawal writes **no** row, because the `consent_records` row that
already evidences it is the better record; and a preview is a `platform.Logger` line rather than a row,
so every row in the table stays a touch of someone's data. It is filterable by actor, act and date and
never by subject, retained unboundedly, and never exported.

**The protected Locale is a constant, never a column.** Dropping it is refused outright in both publish
kinds: `terms.PrevailingLocale` on §37 for the Terms, `policy.MandatoryLocale` on the Ley Orgánica de
Protección de Datos Personales' notice duty for the Policy. A column would be settable, so "unpublish
Spanish" would become "set the column, then unpublish Spanish", through the same editor by the same one
person. It is consulted only at publish, never when validating history.

## Considered options

**Generalizing the two documents into one table vs parallel child tables.** Parallel (chosen), for the
reason ADR 0066 already gave against generalizing the version tables. One artifacts table would need
either a polymorphic `document_kind` + `version_id` pair with no referential integrity, or two nullable
FKs. Real foreign keys are easier to explain to a regulator than a discriminator.

**Keeping `is_gating` (or `gating_from`) beside `revision` vs deriving gating from `revision = 0`.**
Derived (chosen). Two facts that can disagree is one fact plus a bug; kind and gating are the same fact
seen twice, and storing it twice invites a screen to infer one from the other. Once promotion was
removed there was nothing left for either column to say.

**Promotion of a correction to gating vs republishing the text as a new edition.** Republish (chosen).
Promotion would have written `gating_from`, `promoted_by`, `promoted_at` and a second typed reason onto
a row that a thousand acceptances fingerprint — the design's only mutation of an evidenced row — to
save one version row and 30 KB of duplicated Markdown.

**Offset paging with a total, per ADR 0006, vs keyset with none.** Keyset (chosen), for the reason
recorded above. The alternative was seriously weighed as the house convention it is; it lost on the
one workload the screen exists to serve.

**Revoking sessions on publish vs an interstitial on the live session.** Interstitial (chosen). Revoking
is a `DELETE` and would have been easy; it would also have put 25 staff in front of a passcode limit of
10 per IP that is shared with Sale Confirmations, so a publish mid-event locks the door and stops
buyers' mail at the same time.

**A second operator as approver vs the overnight delay.** The delay (chosen). There is no second
operator, and creating one to approve legal text would grant them payouts, reversals and invoicing
backfill in the same boolean.

**A subject-facing self-service Evidence Pack vs operator-only.** Operator-only (chosen). ADR 0039's
precedent cuts against self-service here rather than for it: a proven email buys a **withdrawal**, an
act that only ever takes something away, where a pack **discloses** everything the platform holds. The
accepted cost is that an access request stays manual, at human latency.

**Mass-mailing a new edition vs letting the gate be the notice.** The gate (chosen). Every path into the
platform passes a sign-in gate — ADR 0054 for checkout, ADR 0066 for staff — so the notice reaches
everyone still actually using the platform, and reaches them attached to the act of accepting.

## Consequences

- **A wording change is no longer a deploy, and ADR 0036's "correct weight" argument is answered
  differently.** That decision valued the deploy as real friction in front of an act that re-gates every
  Customer. The friction now comes from the act's own shape — a complete draft, a preview of every
  artifact, a seen diff, a named headcount, and a night to change your mind — rather than from a
  release pipeline that is currently refusing to run at all. Typo-level corrections stop costing a
  deploy, which was ADR 0036's own stated cost.
- **The build-time guarantee is given up deliberately, and replaced by two stronger ones.** ADR 0036's
  Go test recomputed the hash from the embedded artifacts and compared it to the newest seed; because
  `seedMigration` was a single constant, every *older* edition's row was already unguarded — which is
  how the `0-placeholder` hash was rewritten three times without CI noticing. In its place: the text
  migration refuses to apply unless the stored rows reproduce the row's own hash, and a Go test asserts
  the preimage framing reproduces all three hashes production holds.
- **The hash preimage becomes a written rule rather than a coincidence.** Locales are emitted in
  ascending locale code — which reproduces the hand-pinned `[LocaleEN, LocaleES]` only because
  `'en' < 'es'` — and artifacts in `ordinal` order, length-framed on byte length. It has to be a rule
  the moment an operator can add or remove an artifact.
- **A dropped language begins 404ing with no deploy**, because the published set is read per edition
  from its own rows. That reaches the sitemap and `x-default`, which must stop taking their Locales
  from the compile-time constant, and it makes the footer's cross-link the only fallback there is.
- **Nothing has to be notified that a publish happened.** `effective_date <= CURRENT_DATE` makes the
  text current, `revision = 0` plus the same comparison makes the gate bite, and the live-session check
  compares a session against that floor. There is no scheduler, no cache invalidation on publish and no
  broadcast — which is what makes the scheduled edition free, and is the property to preserve.
- **The staff gate now costs a page navigation instead of a sign-in**, so the interstitial is the first
  thing that must never dead-end: the login screen's current behaviour of rendering an ordinary card
  with no acceptance control when a label is missing has to go, not be worked around.
- **`presented_locale` records the Locale of the artifact rendered, not of the page**, because
  `acceptanceLabel` already falls back to the prevailing Locale and the request's Locale can therefore
  be a lie about what was read. It is `NULL` — meaning "not shown", spelled in words on every screen —
  on the channels that present no document, and it is not backfilled.
- **§34's public half is still not built.** ADR 0066 named "public version history, downloadable copia
  fiel"; this decision builds the operator's half and the per-subject Evidence Pack. A public,
  unauthenticated history of editions remains unbuilt, and remains available later at no cost to this
  design, since every edition's bytes are now rows.
- **Nothing here can currently reach production.** CI and Deploy have been refused for payment since
  2026-08-28, so the decision that unblocks changing the legal text is itself blocked behind one manual
  deploy — after which the text never needs a deploy again.
