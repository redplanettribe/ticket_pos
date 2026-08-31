# Proof of Email Ownership alone buys a withdrawal

> Amended by ADR 0066 (#536): the rule that a sign-in consent submission always carries a Policy
> Acceptance gains its one exception — a step whose only owed required box is the Terms (a
> Terms-only re-gate) showed no policy box, so it carries none, and refusing over a box the person
> never saw would evidence an answer to text not on screen. Every other sign-in submission still
> must carry it, and the withdrawal door is unchanged.

## Context

Signing in demands acceptance of the current Policy Version first, and the consent-submission
endpoint enforces it unconditionally: a submission without `policy_acceptance` is refused with
`POLICY_ACCEPTANCE_REQUIRED`. That gate is right for what it was built for — nobody transacts under
a policy they have not accepted.

It produces a genuinely bad sentence the moment a Customer wants to withdraw. Publishing a new
Policy Version re-gates the entire customer base, so a person who wants to take back a consent is
told: *to withdraw your consent, first accept this.* Being made to agree to something in order to
disagree with it is not a defensible thing for a privacy feature to require, and it is worst for
exactly the person the feature exists for.

There is already a credential that proves an address without minting anything: `pending_consents`,
the short-lived, single-use proof of email ownership held in suspension between a verified passcode
and the consent step. Today it can only be spent on a submission that satisfies the gate.

## Decision

`POLICY_ACCEPTANCE_REQUIRED` stops being an unconditional invariant of the consent-submission
endpoint and becomes conditional on the submission's contents:

> Policy Acceptance is required **unless every answer present in the submission is a denial.**

A denials-only submission against a valid pending-consent token succeeds without
`policy_acceptance`, mints **no Customer Session**, and spends the token as it is spent today
whatever the outcome. A submission mixing a denial with any grant, or carrying a policy acceptance
itself, is refused exactly as before unless acceptance is present.

**The rule is enforced in the API**, not in a form. The Storefront's denials-only surface is a
convenience; the guarantee is that the endpoint cannot be talked into anything else.

The reasoning, and the reason it is written down before the code lands: **a submission that grants
nothing needs no acceptance.** Acceptance evidences that somebody was informed before the platform
began doing something on their behalf. A submission that only takes things away begins nothing,
authorizes nothing and opens nothing — there is no processing for the acceptance to have informed
them about. Requiring it would not protect the Customer; it would only stand between them and a
right.

Proof of Email Ownership is the right and sufficient bar. It is the same proof the platform already
accepts for the unsubscribe link and the consent confirmation link, and the act it buys is strictly
weaker than either: it can only move an optional consent to `denied`.

## Consequences

- **No session is minted.** Proving an address in order to withdraw must not leave somebody signed
  in on a shared machine, and the person leaves as signed out as they arrived, with their withdrawal
  recorded.
- **Interception buys nothing.** A stolen passcode on this route can only withdraw consent — never
  grant it, never accept a policy, never open an account — and everything it can do the owner can
  undo from their own Customer Area.
- Without this ADR the conditional gate reads as a hole somebody will "fix". That is the specific
  failure this record exists to prevent: a future reader finding a required check that is sometimes
  skipped, and restoring the unconditional version along with the bad sentence.
- The withdrawal it produces is recorded with the same weight as one made while signed in — same
  write path, same Consent Record, same technical proof — so the easier route is not the weaker one.
- The rule must be tested in both directions. A test that only proves the denials-only submission
  succeeds would pass against an endpoint that had stopped checking acceptance at all.
