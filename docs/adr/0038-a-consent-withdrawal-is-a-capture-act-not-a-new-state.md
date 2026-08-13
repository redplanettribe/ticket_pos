# A Consent Withdrawal is a capture act, not a new state

## Context

A Customer must be able to take back an optional consent they granted. Counsel supplied a
withdrawal form (*Formulario de Revocatoria de Consentimiento*) and an evidence template, and the
obvious readings of them both point the wrong way.

The form is a request, which invites a request table: a `consent_revocations` row per withdrawal,
with a status and an operator who actions it. The clarifying email describes the post-withdrawal
condition of the data as "passive", which invites a flag on the Customer. Either would make
withdrawal a mechanism of its own, sitting beside the consent state rather than being it.

The platform already has exactly one way consent changes: a capture act, written through a single
service path, leaving an immutable Consent Record and the current-state columns it makes true, in
one transaction. Both surfaces that can already move Marketing Consent to `denied` — the unsubscribe
link and the `/following` digest toggle — are captures. Nothing distinguishes what they do from what
a withdrawal does except the story told about it.

## Decision

A Consent Withdrawal is a capture act whose answers are No. It writes through the same path, leaves
the same kind of Consent Record with the same technical proof, and moves the same columns. There is
no withdrawal service, no second state machine, no revocation table, and no flag on the Customer.

Three things follow, and are the substance of this decision:

- **No revocation table.** A withdrawal is identifiable from the evidence the platform already
  keeps, because a Consent Record now also records the state each optional consent was in
  immediately before the act. `granted → denied` is legible from the single row that performed it,
  which is what the guidance's revocation register asks for. A second table recording what the
  consent columns already say is a second place for them to disagree.
- **"Passive" is a description, never a stored state.** It means all optional consents stand
  `denied`, and it is read off them. A column would be a summary that can contradict what it
  summarises, and the first bug it produces is a Customer the platform believes is passive while
  still holding a granted consent.
- **`/privacy` moves consent in BOTH directions.** Turning one back on is an affirmative grant
  behind a proven session — exactly what the digest toggle already is — so nothing about the write
  path is special-cased for withdrawal. Rendering the page writes nothing: a settings page reports
  what is true, and one that recorded a refusal because somebody read it would convert "never asked"
  into "denied" for people who did nothing.

**The name is Consent Withdrawal.** `revoke` stays reserved for credentials — sessions, in-flight
pending consents — where it means destroying a thing rather than recording another act. Spanish
user-facing copy says *revocatoria*, which is counsel's word; the ubiquitous language governs the
code and translations render it.

**Withdrawal is immediate, never queued.** It is a write the platform performs correctly at once,
and a queue for it would invent a deadline that can be missed and a human who can miss it.

## Consequences

- Marketing Consent and `digest_enabled` stay in lockstep through a withdrawal automatically,
  because that lockstep lives in the one write path (ADR 0034). No withdrawal code knows about it.
- Every withdrawal carries the technical proof every other capture carries, and can be tied to a
  session and a device, because it is not a different kind of act.
- Policy Acceptance is not withdrawable. It is absent from counsel's form and gates the platform on
  a basis other than consent; clearing it would re-gate the person rather than free them.
- **Any future networking application integration must read this platform's Networking Consent LIVE
  and must not cache it.** There is no integration today, so a withdrawal has nothing to propagate
  to and this platform builds no propagation mechanism — but the guidance's one explicit instruction
  about networking is that a change to private takes effect immediately across all events, and with
  no propagation mechanism the only way to keep that promise is for the reader never to hold a copy.
  This is recorded here so that it is found by the person who needs it, which is whoever builds that
  integration and not whoever built this.
- An operator-recorded withdrawal is distinguishable from a Customer's own without a second table:
  it carries its own channel (`operator_request`) and the staff member who recorded it, so no report
  can present a staff action as somebody's click.
