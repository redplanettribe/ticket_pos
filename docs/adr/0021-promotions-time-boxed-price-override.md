# Promotions are time-boxed price overrides, not coupon codes

To discount tickets "until some later date" we chose a Promotion: at most one per Ticket Type, holding an absolute Promotional Price (strictly below the List Price, zero allowed) over a scheduled window read in the Event's timezone. We rejected coupon codes (a distribution and redemption system we don't need yet), percentage-off (forces a rounding rule on integer cents and derives ugly prices), and separate early-bird Ticket Types (fragments one capacity pool into several).

## Consequences

- The Platform Fee stays a percentage of what the Customer actually paid: the Promotional Price replaces the List Price as the base in the existing fee arithmetic, so a Promotion proportionally reduces platform revenue. No fee-model change.
- The Promotional Price is the Ticket Type's effective price on every Sales Channel that prices from the catalog — Online Sales today, In-Person Sales when built. Sale Imports keep carrying their own amounts.
- The invariant Promotional Price < List Price is enforced on both writes: creating a Promotion, and any List Price edit that would break it is rejected until the Promotion is adjusted or removed.
- Prices remain locked at begin-checkout (existing snapshot behavior); a Promotion expiring mid-browse re-prices the cart at begin-checkout, and a pending Payment keeps its snapshotted Promotional Price past expiry.
- One-slot-per-Ticket-Type relaxes cleanly to a non-overlapping queue later if multi-phase pricing is ever needed; nothing in the shape forecloses it.
