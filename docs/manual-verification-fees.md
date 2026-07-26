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
