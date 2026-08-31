# The Terms are accepted twice: as attendee on the Storefront, as organizer before a Staff Session

Specified as issue #533.
Extends the consent machinery of [ADR 0036](./0036-the-privacy-policy-is-a-backend-artifact-the-storefront-renders.md)
and [ADR 0038](./0038-a-consent-withdrawal-is-a-capture-act-not-a-new-state.md) to a second document,
and gives the Staff platform its first gate.

## Context

The platform has a first draft of its Términos y Condiciones Generales (Ecuador, FUNDACIÓN
REDPLANETTRIBE) and no way to capture anyone's acceptance of it. The Privacy Policy has full
machinery — Policy Versions, Consent Records, a sign-in gate, a checkout fallback — but Terms
acceptance is a distinct *contractual* act (T&C §2) that must be captured separately, with its own
mandatory, un-premarked checkbox and a link to the full text (§3).

The draft also requires acceptance *in a capacity*: an attendee accepts as an attendee, and an
Organizer must accept expressly "en calidad de organizador" before publishing (§3). The Staff
platform — Organizers, Event Staff, Platform Operators — has no consent machinery at all: any
proven email mints a Staff Session with no acceptance of anything. And §34 requires existing
accounts to accept prospectively before continuing to use the Platform, with evidence retained
per acceptance.

## Decision

**Separate acceptance events per capacity, carried by the platform.** A capture on the Customer
platform *is* the attendee acceptance; a capture on the Staff platform *is* the organizer
acceptance. The same human doing both accepts twice — deliberately (§3). No acceptance carries
role flags; the capacity is implicit in where it was captured.

**Everyone on the Staff platform accepts as "organizer" for now** — org_admin, event_owner,
event_staff and Platform Operators alike. The capacity vocabulary stays open (a CHECK constraint,
not an enum type), so a distinct "staff" capacity can be split out later without schema churn.

**A Terms Versions table parallel to Policy Versions, not a generalization of it.** Label,
effective date, content hash; current = latest effective date ≤ today, ties by creation; no
is-current flag. The two documents version independently — bumping one never re-gates the other —
and the existing Policy Version FK/CHECK chain stays untouched.

**The document is a backend-embedded artifact published in both Locales** (per ADR 0036's
reasoning: this is evidence, not copy): terms markdown plus a checkbox-label markdown per
language, content-hashed together, served by a public endpoint that answers the requested Locale
strictly — a language the Terms are not published in is a 404, exactly as for the policy. *The
first cut of this decision published Spanish only and served it under every Locale; the English
translation was written before the branch merged, and this is the decision as built.* The Spanish
text is the single legally prevailing one (§37) and the English one opens by saying so, so which
language a reader is shown is never which text binds them. Seeded as edition "1", effective on
deploy date, in the same migration that creates the table. No placeholder two-step: text and
machinery ship together, and the seed itself performs the one-time re-gate of the existing
Customer base that §34 requires.

**Both languages are one edition under one fingerprint**, matching Policy Versions. A person
accepts the edition, in whichever language they read it, and the hash covers every published
language at once — so an acceptance beside the English checkbox is evidenced by a fingerprint that
contains both the text that was on screen and the text that legally binds. Per-language hashes
would let the two drift apart under a single label, which is the thing the label exists to
prevent. A correction to the translation alone is therefore a new edition, and re-gates everyone.

**Each capture surface is worded in the language it is being read in**, and links to the terms
page in that language: the Storefront's from the `[locale]` in its own URL, the Staff app's from
the language its login page was rendered in — the same detected language that is already
remembered as the Staff Locale (ADR 0041). A mandatory contractual box with nothing legible beside
it is what §3 forbids; the Staff gate falls back to the prevailing text rather than to an empty
label if a language ever reaches it unpublished.

**Customer capture rides the existing consent machinery.** A nullable Terms answer and Terms
Version reference on Consent Records (null = box not shown), paired accepted-at/version state on
the Customer, a held answer on the payment snapshot, a Terms component in Outstanding. Gate
placement is unchanged: the sign-in consent step and the checkout owed-boxes fallback. No new
screens. This amends ADR 0039's rule that a sign-in consent submission always carries a Policy
Acceptance: a Terms-only re-gate — the step whose only owed required box is the Terms — showed no
policy box, so it carries none; evidencing an answer to a box the person never saw would be worse
than the rule it bends. Every other sign-in submission still carries one.

**The staff gate sits before Staff Session mint, keyed on email, once per person per edition.**
OTP proven but Terms outstanding mints no session; acceptance appends one row to a new append-only
Staff Terms Acceptances table (email, Terms Version, capacity, accepted-at, evidence) and the
session is minted. Outstanding = no row matching the current edition for this email — one indexed
lookup, no state column. A person with several Organizations accepts once. Live sessions are not
revoked; the gate binds at next sign-in. The Staff app hosts no copy of the document — its gate
screen links out to the public Storefront terms page.

**Terms acceptance is contractual and has no withdrawal path.** Withdraw All and every
consent-withdrawal channel leave it untouched, and it never appears as a revocable consent in the
Customer privacy area. The only thing that changes it is a new edition owing re-acceptance —
inserting a later Terms Versions row makes every stored reference stop matching, flipping
Outstanding for Customers and the row lookup for Staff, with no code and no data migration.

## Considered options

**One acceptance with role flags vs separate events per capacity.** Separate events (chosen). The
draft demands an express act "en calidad de organizador"; a flag on an attendee acceptance would
make one act stand for two declarations, and the evidence would not say which capacity was in
front of the person when they ticked.

**Generalizing Policy Versions into a documents table vs a parallel table.** Parallel (chosen).
The two documents version independently, and re-pointing the existing Consent Record FK chain at
a generalized table would churn every consent test to gain a table the schema uses twice.

**A current-state column on staff vs computing outstanding from rows.** Rows (chosen). The Staff
platform has no person table to put state on — email is its person key — and "no row for the
current edition" is one indexed lookup that a new edition flips for free.

**Spanish only vs a published translation.** Published translation (chosen). Spanish-only was
defensible — §37 makes it the operative text and the fingerprint had one language to cover — but it
put an English reader in front of a mandatory contractual checkbox beside a document they may not
be able to read, which is a worse answer to §3 than the extra edition weight is a problem. The
translation is published as a translation: it says so in its first line, it never becomes
operative, and it is fingerprinted with the Spanish so the evidence covers both.

**A "staff" capacity now vs "organizer" for everyone.** One capacity (chosen). The draft names
the organizer's express acceptance; splitting door staff into their own capacity is vocabulary
the CHECK leaves open, and doing it now would force a ruling (which roles are which) that no
requirement yet needs.

## Consequences

- Shipping the seed re-gates the whole Customer base exactly once: everyone owes Terms edition
  "1" at their next sign-in or checkout, exactly as Policy edition "1" did. Live sessions ride
  until then.
- The Staff platform gains its first consent machinery: one append-only table and a
  terms-required outcome in the sign-in contract. Every human on it — including Platform
  Operators — accepts before their next session is minted.
- A publishing Organizer's §3 obligation is discharged at sign-in rather than at first publish;
  the gate is earlier and broader than the clause requires, which satisfies it.
- The Terms Version database id never goes on the wire — labels and hashes only, matching the
  Policy Version rule.
- A wording change to the Terms is a Go commit, a new `terms_versions` row and a migration
  (ADR 0036's weight), and it re-gates Customers *and* Staff at once. That now includes a change to
  the English translation alone, and it means the two languages must be edited together: a
  translation is not a place to fix a typo cheaply.
- The two languages must keep saying the same things in the same order. Section numbering is
  pinned by a test, because both texts cross-reference their own sections (§3, §37) and so does
  this codebase; the rest is a review obligation, not something a test can hold.
- The legal center — public version history, downloadable copia fiel — is §34 machinery this
  decision does not build.
