# Manual verification: PayPhone Sale Reversal

Run the sandbox passes **before go-live for the Undo feature** (and again whenever the PayPhone
credentials change), and the stub check whenever touching the reversal path locally. They cover
what no automated layer can.

The integration suite drives a full Undo of a paid Online Sale against a *fake* PayPhone server
(`PAYPHONE_API_BASE_URL` override) and proves what we **send** to `POST /api/Reverse/Client` and
what we do with each answer — a literal `true`, a `false`, an `errorCode`, garbage, a dropped
connection. What it cannot prove is what real PayPhone **does**: that our bearer token is
accepted on the reversal endpoint at all, that a reversal keyed on our `clientId` finds the
transaction, that the money actually leaves the merchant account, and — the one this runbook
exists for — **how PayPhone answers a request that arrives after its same-day 20:00 cutoff**,
for which its documentation publishes no error code (ADR 0018).

Money safety while testing: **sandbox credentials move no money**. With production credentials a
reversal moves real money in the correct direction (back to the buyer), so the dangerous
direction here is not a lost charge but a *silent* one — a reversal PayPhone performed that this
platform did not record. Section 5 is how you would find that.

## Prerequisites

- Everything in [manual-verification-payphone.md](manual-verification-payphone.md): a PayPhone
  Business account with a **sandbox** payment-button application, the test origin registered on
  it, and the dev stack (`make dev`) with the seeded catalog
- The reversal endpoint uses the **same token and origin** as Prepare and Confirm, and PayPhone
  requires the reversal to be made by the token that created the transaction. This holds by
  construction while the platform sells through one merchant account (ADR 0012); if that ever
  changes, this assumption is the first thing to re-test
- A Customer able to sign in to the Customer Area at `/tickets` — the passcode arrives by email,
  or in the backend container log when `RESEND_API_KEY` is empty
- **Timing matters in every pass below.** The Reversal Window closes at the earlier of 20:00
  `America/Guayaquil` on the purchase date and the Event's start. Do sections 1–4 in the
  Ecuadorian morning or afternoon; section 4 is the one that needs the evening

## 1. Stub quick check (dev, zero setup)

With `PAYPHONE_API_TOKEN` and `PAYPHONE_STORE_ID` **empty** in `.env` (the default), `make dev`:

1. Buy one **paid** ticket from the seeded Event and approve it on the `/checkout/stub`
   interstitial (steps 1–4 of the stub pass in the payments runbook)
2. Sign in at `http://localhost:64300/tickets` as the buyer. The card must carry
   **"You can undo this purchase until 8:00 PM Ecuador time"** and an **"Undo this purchase"**
   button. It appearing on a *paid* sale is the whole point of the stub supporting reversal — if
   it is missing, the provider selection or the reversal rule is wrong, not the clock
3. Press it. The dialog names the Event, the Sale Confirmation reference, and — because this one
   cost money — that the amount **will be returned**. Confirm
4. Assert: the toast, the card redraws with a **Reversed** badge and no undo offer, and the
   backend log carries the void notice email
5. On the public Event page, assert remaining capacity went back up by the quantity bought
6. In Staff, assert the Sales list still shows the sale — reversed, never deleted — and that the
   Event's Net Proceeds strip dropped by that sale's contribution

The stub agrees to every reversal and contacts nothing. It proves the flow, never the provider.

## 2. Sandbox reversal pass

Configure the **sandbox** credentials and restart (section 2 of the payments runbook; confirm the
log says `payment provider: payphone`). Then:

1. Buy one ticket and complete the payment on PayPhone's hosted card form. Note the
   `client_transaction_id`:

```bash
docker compose exec -T db psql -U ticket_pos -d ticket_pos -c \
  "SELECT client_transaction_id, provider_transaction_id, status, amount_cents FROM payments ORDER BY created_at DESC LIMIT 1"
```

2. In the Customer Area, press **Undo this purchase** and confirm
3. Watch the backend log. A success is silent — there is no "reversed" line — so the evidence is
   the absence of `payphone reverse failed` / `payphone refused the reversal` together with the
   200 on `POST /api/v1/customer/ticket-sales/{id}/reverse`
4. Assert the same six outcomes as the stub pass: reversed badge, no offer, void notice, capacity
   back, Sales list row still present as reversed, Net Proceeds down
5. **In the PayPhone dashboard**, find the transaction by its `clientTransactionId` and assert it
   is shown as reversed there too. This is the assertion the whole runbook is for: the platform's
   record and PayPhone's record must agree
6. Press the browser back button and try the undo again. Assert it is refused as already reversed
   and that PayPhone was **not** asked a second time (no second reversal in the dashboard)

## 3. Issuing-bank refusal pass (`errorCode 42`)

PayPhone documents *"El reverso no se puede ejecutar… contáctese con el banco emisor"* — a
refusal by the card's issuing bank, unrelated to timing. It cannot be provoked on demand; what
this pass verifies is that the platform behaves correctly **if you ever see one**.

If a refusal occurs in any pass (or can be arranged with a test card the sandbox declines
reversals for):

1. Assert the buyer sees **"We couldn't undo this purchase. Nothing has changed — contact the
   organizer with your confirmation reference."** with the `TP-…` reference, and **no** mention of
   an error code, a bank, or a reason
2. Assert the backend log carries both lines, with the code:

```
payphone refused the reversal   (or: payphone reverse failed)
sale reversal refused by the payment provider; the Ticket Sale is untouched
```

3. Assert **nothing moved**: the sale is still active, the undo offer is still on the card,
   capacity is unchanged, no void notice was sent, and the Net Proceeds figure is untouched
4. Assert the PayPhone dashboard shows the transaction still charged

If no refusal can be provoked, record that — the fake-server suite covers the mapping, and this
pass is about the live surfaces.

## 4. After the 20:00 Ecuador cutoff

**This is the pass nothing else can substitute for.** The platform's Reversal Window and
PayPhone's own same-day limit are two rules that happen to end at the same wall-clock hour, and
PayPhone publishes **no error code meaning "too late"** (ADR 0018). What it actually answers to a
late reversal is unknown, and this is where it gets written down.

1. **Before 20:00 Ecuador time**, buy a ticket for an Event that starts at least a day out, and
   assert the Customer Area offers the undo with a deadline of **8:00 PM Ecuador time** today
2. **After 20:00 Ecuador time**, reload `/tickets`. Assert the offer is **gone**: no button, no
   expired countdown, nothing about undoing. The window is the platform's rule and is enforced
   server-side, so this is not a UI decision
3. Re-issue the request anyway, as a client with a stale page would — this is the only step that
   needs a terminal. The session token is the Storefront's own customer-session cookie (copy it
   from the browser's dev tools; the Storefront proxies to the API and never exposes it to page
   scripts):

```bash
curl -i -X POST http://localhost:64080/api/v1/customer/ticket-sales/<sale id>/reverse \
  -H "Authorization: Bearer <customer session token>"
```

Expect **409 `REVERSAL_WINDOW_CLOSED`**. Assert the backend log shows **no PayPhone request at
all** — the platform refuses on its own clock and never asks the provider to reverse something it
has already decided against

4. **The finding worth recording.** Purchase a ticket close to 20:00 and attempt the undo in the
   last minute or two before the cutoff, with the clock in view. If the platform allows it and
   PayPhone refuses, capture the exact `errorCode` and message from the log and **write it in
   this file**: that is the code that means "too late", and it is the one PayPhone does not
   document. Until somebody sees it, the platform is right not to guess at a cause in front of a
   buyer
5. Repeat step 4's purchase for an Event **starting later today** and assert the deadline shown is
   the Event's start rather than 20:00 — the window ends at the doors, because nothing here
   records attendance

## 5. The incident: reversed at PayPhone, not recorded here

The one failure this design accepts (ADR 0018): PayPhone reverses, and the local commit then
fails. The buyer keeps their money **and** their tickets, and the Organization's dashboard shows
revenue that no longer exists. It cannot be provoked from the outside — it needs the database to
fail in the seconds after a successful provider call — so what is verified here is that it would
be **findable**.

The line is greppable and names both ids:

```bash
gcloud logging read \
  'resource.type="cloud_run_revision" AND textPayload:"SALE_REVERSAL_NOT_COMMITTED"' \
  --limit 20 --format=json
```

1. Confirm the query above runs and returns nothing in normal operation
2. Confirm the log line's shape by reading it in
   `backend/internal/sales/service/reversal.go`: it carries `ticket_sale_id`,
   `confirmation_ref` and `client_transaction_id` — the sale to correct here, and the id to find
   the reversal on the PayPhone dashboard
3. Resolution is manual and by hand: verify against the dashboard that the reversal really
   happened, then mark the Ticket Sale reversed and restore its capacity. **Never re-run the
   reversal against PayPhone** — the money has already gone back once

Add `SALE_REVERSAL_NOT_COMMITTED` to whatever alerting exists alongside the checkout's
`PAYMENT_APPROVED_WITHOUT_SALE`; they are the same class of incident and the same manual
resolution.

## 6. Production spot check (at go-live)

After the production credentials are applied, one small real-card purchase, undone by the buyer
from the Customer Area within the window:

- The buyer's card statement (or PayPhone dashboard) shows the reversal
- The Sales list shows the sale reversed, and the Event's Net Proceeds and the Organization's
  Withdrawable Balance both dropped by it — **including into negative territory** if that sale had
  already been paid out, which is by design and says so
- No `SALE_REVERSAL_NOT_COMMITTED` in the logs

## Rollback

The Undo is offered from one place, so withdrawing it is a code change rather than a
configuration one: `PayPhoneProvider.SupportsReverse` returning `false` removes the offer from
every paid sale's card AND makes the endpoint refuse it — the Customer Area and the endpoint read
the same rule, so they cannot disagree (`platform.PaymentReversal`). Free Online Sales stay
undoable, because no provider is involved in them. Sales already reversed keep their status;
nothing to unwind.
