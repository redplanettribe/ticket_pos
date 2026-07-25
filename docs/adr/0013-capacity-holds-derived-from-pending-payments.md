# Capacity Holds are derived from pending Payments, not stored

A Customer must not be able to pay for tickets that sold out while they were on the provider's
payment page. Instead of a reservation table with explicit release, available capacity is
computed as `capacity − sold_count − quantities on pending Payments younger than the hold
window (~20 minutes)`. A pending Payment *is* the hold; expiry is a `created_at` cutoff in the
query, and committing the sale converts the Payment's own hold into `sold_count` under the
existing row locks.

Chosen because Cloud Run runs no background workers (ADR 0007), so stored holds would need a
cleanup job we cannot host in-process, and because the alternative — charge first, check
capacity after, reverse on conflict — makes "you paid and got nothing" a designed outcome.

## Consequences

- An abandoned checkout blocks its quantities for up to the hold window; storefront "remaining"
  counts are conservative for those minutes. The window comfortably covers PayPhone's 10-minute
  form validity plus its 5-minute confirm window.
- Public remaining/sold-out figures must subtract live holds, so they read the payments table.
- Stale pending Payments are lazily marked `expired`; nothing depends on that transition for
  correctness.
