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

**The document is a Spanish-only backend-embedded artifact** (per ADR 0036's reasoning: this is
evidence, not copy): terms markdown plus a checkbox-label markdown, content-hashed, served by a
public endpoint under every Locale — the Spanish text is the single legally prevailing one (§37),
so the English Storefront shows Spanish rather than 404ing. Seeded as edition "1", effective on
deploy date, in the same migration that creates the table. No placeholder two-step: text and
machinery ship together, and the seed itself performs the one-time re-gate of the existing
Customer base that §34 requires.

**Customer capture rides the existing consent machinery.** A nullable Terms answer and Terms
Version reference on Consent Records (null = box not shown), paired accepted-at/version state on
the Customer, a held answer on the payment snapshot, a Terms component in Outstanding. Gate
placement is unchanged: the sign-in consent step and the checkout owed-boxes fallback. No new
screens.

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
  (ADR 0036's weight), and it re-gates Customers *and* Staff at once.
- The legal center — public version history, downloadable copia fiel — is §34 machinery this
  decision does not build.
