# Manual verification: Platform Fee and buyer prices

Run this pass whenever the Storefront's price surfaces, the Fee Handling switch, or the configured
rates change. The backend integration suite already pins the arithmetic, the charged amounts, and
the per-line snapshots (`backend/integration/checkout_fees_test.go`); what cannot be asserted there
is what a Customer's eye actually lands on — one all-in price, one muted note, and no fee
arithmetic anywhere in between (ADR 0014). That is what this checklist covers.

The numbers below assume the launch rates (10% Platform Fee, 15% Fee IVA) and the seeded
`$35.00` General Admission ticket on `demo-venue/midnight-synth-live`: **$39.03** all in
(3500¢ + 350¢ + 53¢).

## Prerequisites

- The dev stack (`make dev`) with the seeded catalog, Storefront at `http://localhost:64300`,
  Staff at `http://localhost:64301`
- `PAYPHONE_*` empty so the stub provider drives the payment legs (see
  [manual-verification-payphone.md](manual-verification-payphone.md))

## 1. Pass-on: one all-in price everywhere

With the Event's Fee Handling left at its default (`pass_on`):

1. Storefront home / organization page — the Event card's "from" price reads **$39.03**, not
   $35.00
2. Event page — the General Admission card reads **$39.03**, and a single muted line
   **"Prices include the service fee."** sits under the running total. No percentage, no tax
   line, no fee amount anywhere
3. Add one ticket, **Get tickets** — the cart dialog lists `1 × General Admission $39.03`, total
   **$39.03**, and repeats the same muted note exactly once
4. **Continue to payment** — the provider's page shows **$39.03**: the price never jumped between
   the picker and the charge
5. Approve — the Sale Confirmation (backend log without `RESEND_API_KEY`) says
   `Total paid: 39.03 USD`
6. Staff → the Event's Sales list — the row's amount is **$39.03**, the amount the Customer paid

## 2. Absorb: clean prices stay clean

In Staff, open the Event's detail form and set Fee Handling to **absorb**, then reload the
Storefront event page:

1. Card and event page both read **$35.00** — exactly the price the organizer set
2. **No** "includes service fee" note appears anywhere: not on the event page, not in the cart
3. Buy one ticket: the provider's page charges **$35.00**, the receipt says `Total paid: 35.00 USD`,
   and the Sales row reads $35.00

## 3. A flip mid-payment does not move a pending Payment

1. With the Event back on `pass_on`, start a checkout and stop at the provider's payment page
   ($39.03)
2. In Staff, flip the Event to **absorb** while that page is still open
3. Approve the payment: the receipt and the Sales row still read **$39.03** — the Payment's
   snapshot wins, and only the next Customer sees $35.00

## 4. The organizer's derived line agrees with what buyers pay

In the Staff ticket-type form, type `3500` as the price:

- Under `pass_on` the derived line reads **Buyers will pay $39.03** — the same number the
  Storefront quotes
- Under `absorb` it reads **You'll receive $30.97 per ticket** (3500¢ − 350¢ − 53¢)

If either of those disagrees with the Storefront by a cent, the two implementations of the
arithmetic (`backend/internal/sales/fees.go` and `apps/staff/lib/fees.ts`) have drifted.

## 5. The Event's Net Proceeds strip

The figures are pinned by `backend/integration/sales_summary_test.go`; what needs an eye is who
sees the strip and that the platform's cut is nowhere on the surface.

1. As the Org Admin, open the Event's **Sales** tab after the `pass_on` purchase above — a strip
   sits above the list showing **Net proceeds $35.00** (the price the organizer set, not the
   $39.03 the buyer paid) and **Sales 1**
2. Nothing on that strip names a fee, a tax, a percentage, or a gross amount to subtract one from
3. Commit a Sale Import of cash sales: the **Sales** count goes up with the list, and **Net
   proceeds** does not move — the platform never held that cash
4. Sign in as an Event Staff member of the same Organization and open the same tab: the Sales list
   renders exactly as before, with **no strip above it** and no import section below it
## 6. Payouts: Withdrawable Balance and history

The balance arithmetic is pinned by `backend/integration/payouts_test.go`; what this checks is
that an Org Admin — and only an Org Admin — finds the figure where they expect it.

1. Staff → **Settings** → the **Payouts** card sits under the organization logo: a Withdrawable
   Balance headline in the organization currency and a payout history under it, reading
   "No payouts recorded yet." on a fresh organization
2. After the pass-on purchase above, the balance reads **$35.00** — the Net Proceeds, not the
   $39.03 the Customer paid. The platform's cut is never displayed as a number
3. Record a Payout the way the platform operator does, straight into the database:
   `INSERT INTO payouts (organization_id, amount_cents, paid_at, note) SELECT id, 2000, '2026-07-01', 'test settlement' FROM organizations WHERE slug = 'demo-venue';`
   Reload Settings: the balance drops to **$15.00** and the history lists the payout with its
   date and note. There is no button to create one — that is the point
4. Sign in as an Event Staff member: **Settings** refuses them entirely, so the Payouts card is
   out of reach along with the rest of it
