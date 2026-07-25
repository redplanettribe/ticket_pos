# Online payments: platform is merchant of record, via a redirect Payment Provider boundary

Online Sales are paid through a Payment Provider (PayPhone first) behind a provider-agnostic
boundary in the Go API. The platform holds the single merchant account — credentials are
platform configuration, and Organizations are settled outside the system. Integration is by
full-page redirect: the API initiates a payment server-side and hands the Storefront a hosted
payment URL; the provider's return redirect lands on a Storefront route handler (per ADR 0008,
nothing external may call the Go API), which asks the API to confirm.

## Considered Options

- **Per-Organization merchant accounts** — organizers would receive money directly, but it adds
  credential storage/UI per Organization and every account would need the storefront domain
  registered with the provider. Rejected for now; the boundary passes credentials through the
  provider implementation (not domain code) so this remains an additive change.
- **Embedded widget (PayPhone "Cajita")** — keeps the customer on our domain but puts vendor
  JavaScript in the Storefront and cannot be hidden behind a provider-agnostic interface, since
  every provider's widget mounts differently. A redirect URL is a shape nearly every provider
  can satisfy.

## Consequences

- Refunds/reversals are manual (platform operator, provider dashboard) until a reversal flow is
  built; the provider interface reserves a method for it.
- PayPhone has no webhooks: the customer's return redirect is the only confirm trigger, and an
  unconfirmed payment is auto-reversed by PayPhone after 5 minutes. A lost redirect therefore
  means an automatically refunded customer and a lost sale — money-safe by construction. A
  rescue reconciler (Cloud Scheduler) is a possible follow-up, not part of this decision.
- Only an explicit provider decline marks a Payment `failed`. A provider error with an unknown
  outcome (HTTP failure, malformed response, undocumented status) leaves the Payment `pending`
  and the confirm retryable: marking it failed would idempotently replay "failed" to a customer
  who may in fact have paid, while an unconfirmed charge is safely auto-reversed anyway.
- Online Ticket Sales record Payment Method `payphone` even when the dev/test stub collected
  the payment: the stub is a stand-in for the real provider, and the recorded value set stays
  production-truthful rather than admitting a `stub` value.
