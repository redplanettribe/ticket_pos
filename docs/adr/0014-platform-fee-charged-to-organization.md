# Platform Fee is charged to the Organization, never to the Customer

The platform takes a Platform Fee (10% at launch) on Online Sales, plus Ecuadorian IVA
(15% at launch) on that fee — the fee is the platform's taxable service; the tickets remain
the Organization's own sale and their taxation stays outside the system. The fee and its
Fee IVA are always collected by withholding from the Organization's proceeds. The per-Event
Fee Handling switch (`pass_on`, the default, or `absorb`) never changes who is charged: it
only decides whether the buyer price is raised (by exactly fee + Fee IVA, so the Organization
nets the price it set) or left at the set price (so the Organization nets ~11.5% less).

## Considered Options

- **IVA on the ticket itself** — would make the platform model (and arguably remit) the
  Organization's own tax obligations on every ticket sold. Rejected: the platform is a
  marketplace, not the seller; its taxable service is the commission alone.
- **Fee charged to the Customer under `pass_on`** — the buyer's charge would fiscally contain
  a taxable service component, so PayPhone requests would need `amountWithTax`/`tax`
  populated in one mode but not the other, checkout receipts would need per-mode fiscal
  breakdowns, and the two modes would be structurally different objects. Rejected: under
  organizer-side charging, both modes produce identical charge shapes — the whole amount
  rides in `amountWithoutTax` exactly as before this decision.

## Consequences

- The Customer never sees fee or tax itemization; `pass_on` prices are shown all-in
  everywhere (no drip pricing), with at most a muted "includes service fee" note.
- The platform's fiscal artifact is an IVA-bearing service invoice to the Organization for
  the withheld fee — an accounting concern outside this system.
- Fee math is per-unit in integer cents, round half-up, Fee IVA computed on the already
  rounded fee; every sale line snapshots its fee amounts and the rates used, so recorded
  economics never change when configured rates change (Ecuador moved IVA 12% → 15% in 2024).
- The fee applies only to Online Sales — withheld from money the platform actually holds.
  In-person and imported sales carry no fee, so an Organization's Withdrawable Balance
  (net proceeds minus recorded Payouts) cannot go negative through fee accrual alone.
