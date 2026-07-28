# Manual verification: PayPhone online payments

Run the sandbox passes **before go-live** (and again whenever the PayPhone credentials or the
Storefront domain change), and the stub check whenever touching checkout locally. They cover what
no automated layer can: the integration tests exercise Prepare/Confirm against a *fake* PayPhone
server (`PAYPHONE_API_BASE_URL` override), and the Playwright journeys drive the *stub* provider's
interstitial — neither proves that real PayPhone accepts our credentials, honors the registered
domain, renders its hosted card form, and drives our return leg with the params it actually sends.
That agreement is verified here by hand.

Money safety while testing: **sandbox credentials move no money** (payments auto-approve), and even
with production credentials an unconfirmed charge is auto-reversed by PayPhone after 5 minutes.
The dangerous direction is the opposite one — a paid Customer without tickets — which is exactly
what the approve pass proves cannot happen silently.

## Prerequisites

- A PayPhone Business account with the Developer portal open ([docs.payphone.app](https://docs.payphone.app/boton-de-pago))
  and a **sandbox** payment-button application on it — the sandbox pair is what these passes use;
  the production pair goes only to Secret Manager (see the go-live checklist in
  [gcp-deployment.md](gcp-deployment.md))
- The domain the test runs from registered on that sandbox application — the button is
  **domain-bound**, so an unregistered origin fails at the payment page, not before
- The dev stack (`make dev`) with the seeded catalog: the passes buy from
  `http://localhost:64300/demo-venue/events/midnight-synth-live` (capacity 200, so repeated runs
  never sell it out)
- `RESEND_API_KEY` may stay empty: Sale Confirmations then print to the backend container log,
  which is enough to assert on

## 1. Stub quick check (dev, zero setup)

With `PAYPHONE_API_TOKEN` and `PAYPHONE_STORE_ID` **empty** in `.env` (the default), `make dev`:

1. Confirm the backend log says the stub was selected:
   `payment provider: stub (no PAYPHONE_* credentials set)`
2. On the seeded Event page, add one General Admission ticket, **Get tickets**, fill email +
   first/last name, **Continue to payment**
3. Confirm the interstitial at `/checkout/stub` shows the badge **"Test payment — no money
   moves"** and the exact amount ($39.03 — the seeded $35.00 ticket all in under `pass_on`
   Fee Handling; see [manual-verification-fees.md](manual-verification-fees.md))
4. **Approve payment** → the success page shows a Sale Confirmation reference (`TP-…`), and the
   backend log carries the Sale Confirmation email
5. Repeat and **Decline payment** → the failure page offers a retry, and retrying starts a fresh
   attempt (a new reference on the interstitial)
6. In Staff (`http://localhost:64301`), open the Event's Sales list: the approved purchase is one
   row, channel `online`, Payment Method **PayPhone**; the declined attempt is **absent**

The stub records Payment Method `payphone` on purpose — an Online Sale names the provider that
would have collected the money, and the Sales list must look the same in dev as in production.

## 2. Configure the sandbox credentials

1. Put the **sandbox** token and store id in the gitignored `.env`:

```bash
PAYPHONE_API_TOKEN=<sandbox bearer token>
PAYPHONE_STORE_ID=<sandbox store id>
```

2. Restart the dev stack (`make down && make dev`; Compose passes both through to the backend)
3. Confirm selection in the backend log: `payment provider: payphone` with the store id. If it
   still says `stub`, one of the pair is empty — half a pair selects the stub by design
4. Do **not** set `PAYPHONE_API_BASE_URL`. It exists for the integration suite's fake server; the
   dev stack must talk to real sandbox PayPhone here, and production refuses to boot with it set

## 3. Sandbox approve pass

1. Buy one ticket exactly as in step 1; on **Continue to payment** the browser must land on
   PayPhone's **hosted card form** (a `pay.payphonetodoesposible.com` page, top-level — never an
   iframe), showing the amount and the Event name as reference
2. Complete the payment the sandbox way (sandbox transactions auto-approve; no money moves).
   Do not dawdle once paid: confirmation must happen within PayPhone's **5-minute** window
3. PayPhone redirects to `/checkout/return`, which confirms server-side and lands you on
   `/checkout/success` — assert the `TP-…` Sale Confirmation reference is shown
4. **Refresh the success page and re-open the return URL from history**: same outcome, same
   reference — confirm is idempotent and never double-commits
5. Assert the Sale Confirmation email (real, or in the backend log) carries the same reference and
   a working Confirmation Link
6. In Staff, assert the Sales list gained **one** row: channel `online`, Payment Method
   **PayPhone**, correct amount — and that filtering by Payment Method **PayPhone** isolates it
7. On the public Event page, assert remaining capacity dropped by exactly the quantity bought
8. In the PayPhone dashboard (sandbox), find the transaction and cross-check the
   `clientTransactionId` against the payment row (`payments.client_transaction_id`) — this id is
   the support cross-reference every incident procedure leans on

Landing anywhere other than success/failed after paying — the explorer home page, an error page —
means the return handler did not recognize the params PayPhone sent. **Stop and treat it as a
release blocker**: the customer's charge will auto-reverse (money-safe), but every real sale would
be lost the same way.

## 4. Sandbox prefill pass

This is the pass that closes the one assumption the prefill feature (#103) ships on. PayPhone's
Prepare call accepts `email`, `documentId` and `phoneNumber` as optional prefills — its docs say of
each *"se solicitará si no se proporciona"* (it will be asked for if not provided) — and the
integration suite proves what we **send**. Only a real hosted form proves what PayPhone **does**
with it. In particular, PayPhone's documentation never states whether a non-Ecuadorian dialling
code is accepted, and never denies it either.

Watch the backend log throughout. The line

```
payphone prepare rejected our request; retrying once without the prefills
```

is the fallback firing: PayPhone refused the payload, and the checkout completed on a retry
carrying no prefills at all. It reports `had_email` / `had_document_id` / `had_phone_number`
booleans — never the values — so the field to suspect is named without any PII reaching the log.
**A checkout that succeeds is not evidence of nothing wrong**; that line is where a systematic
rejection shows up.

1. **Ecuadorian pass.** Buy one ticket with Tax ID Type **Cédula** and a valid cédula, and a phone
   with Ecuador (+593) selected and a real mobile. On PayPhone's hosted form assert the email, the
   identification number and the phone all arrive **already filled**, and that the card is the only
   thing left to enter. Complete the payment and assert the sale lands as in the approve pass
2. **Foreign-number pass.** Repeat with a non-Ecuadorian country selected and a valid number for
   that country. Assert whether the phone arrives prefilled, **and check the log for the retry
   line**. This is the assumption under test: record the outcome either way
3. **Passport pass.** Repeat with Tax ID Type **Pasaporte** and a passport number. Assert the
   identification field on PayPhone's form is **empty and asked for** — a passport is deliberately
   withheld, because `documentId` is built around Ecuadorian identifiers and a refused Prepare is a
   failed checkout. Assert the email and phone still arrive filled, and that the checkout succeeds
4. **Omitted-phone pass.** Repeat leaving the phone field blank. Assert PayPhone asks for the phone
   as it always did and the checkout is otherwise unchanged — the field is optional, and a blank one
   must send no `phoneNumber` at all rather than an empty value
5. **Record the findings** in this file or the issue: specifically, whether foreign dialling codes
   are accepted. If they are refused, narrow the country table in `apps/storefront/lib/phone.ts` to
   what PayPhone takes and note why — the fallback means this is a tidy-up, not an incident

What this pass cannot settle: PayPhone prohibits static or hardcoded cardholder data (*"datos
quemados o estáticos"*) and enforces it through fraud monitoring, not through the API. A Prepare
returning 200 therefore proves nothing about long-term account safety. The structural protection is
that every value sent is buyer-entered or omitted, with no defaults anywhere in the path — if a
future change ever introduces a fallback value for any of these three fields, that protection is
gone regardless of what this pass showed.

## 5. Sandbox decline pass

1. Start another purchase, and on PayPhone's payment page **cancel** instead of paying
2. Assert you land on `/checkout/failed` with a retry action, and that retrying opens a fresh
   PayPhone page (a new attempt under a new client transaction id)
3. In Staff, assert **no** new Sales list row exists; on the Event page, assert remaining capacity
   is unchanged
4. Abandon one more attempt entirely (close the PayPhone tab). Assert the Event's remaining
   capacity recovers within the ~20-minute hold window (ADR 0013) — the pending Payment's Capacity
   Hold lapses on its own; nothing needs cleaning up

## 6. Production spot checks (at go-live)

After the production credentials are applied per [gcp-deployment.md](gcp-deployment.md):

```bash
gcloud run services describe prod-ticket-pos-api --region us-east1 \
  --format='value(spec.template.spec.containers[0].env)' | grep -i payphone
```

Expect `PAYPHONE_API_TOKEN` and `PAYPHONE_STORE_ID` present **as secret refs** (never plaintext),
and **no** `PAYPHONE_API_BASE_URL` — that variable set in production is a startup refusal by
design; a running service showing it means the guard is not working. Likewise confirm
`STOREFRONT_STUB_PAYMENTS` appears nowhere on the Storefront service, and that
`https://discover.multiticketing.com/checkout/stub` is a 404.

Then one small real-card purchase end to end (success page, email, Sales list row, dashboard
transaction), refunded manually from the PayPhone dashboard — refunds are a manual operator action
by design (ADR 0012).

## Rollback

Online payments add no coupling to the other Sales Channels, so rollback is removing the entry
point: unset the PayPhone credentials (locally: blank them in `.env` and restart; production:
apply without the `TF_VAR_payphone_*` values sourced, which removes the secret versions). The API
falls back to the stub provider — in production that makes checkout a dead end rather than a fake
sale, and POS, imports, the Sales list, and every recorded sale keep working. Payments already
recorded keep their rows; nothing to unwind.
