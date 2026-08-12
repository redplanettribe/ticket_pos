# Marketing Consent and the Follow Digest are one switch

## Context

The privacy guidance requires an explicit, un-preselected opt-in for marketing email. The platform
already had one email preference: `digest_enabled`, an opt-out switch defaulting to true, governing
the weekly Follow Digest. Two models were possible: keep them separate — the Digest as a
Follow-solicited service on its own switch, marketing consent as a new opt-in gating only future
campaign mail — or declare them the same thing.

## Decision

They are the same thing. Granting Marketing Consent turns the Digest on; declining it — including
through the existing Customer Area toggle or the signed unsubscribe link, both of which now write
consent state and evidence — turns the Digest off. The consent checkbox copy names the Digest
explicitly, so nobody grants or declines it without being told what it covers.

Because consent must be affirmative, the box is always shown unticked, and submitting a capture
surface with it unticked records an explicit "No" and sets `digest_enabled` false — even for a
Customer who Follows busily and was receiving the Digest under the old default.

## Consequences

- The legacy default (`digest_enabled = true`, set on every Customer ever created) is not consent
  and is never claimed as such. During the transition it stays operative: the Digest keeps going to
  Customers who have not yet answered, on the strength of Follow being "a request to be written to"
  and the unsubscribe link in every issue. The sender's rule is: send when Marketing Consent is
  granted, or when it is unanswered and `digest_enabled` is true; never when it is denied or
  pending confirmation.
- Toggling the digest on from the Customer Area is now a consent-granting act and writes a full
  Consent Record, not just a boolean.
- Future campaign mail checks Marketing Consent and nothing else; `digest_enabled` remains the
  operational switch kept in lockstep, never an independent source of truth.
