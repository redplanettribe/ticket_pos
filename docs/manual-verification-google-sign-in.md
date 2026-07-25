# Manual verification: Google Sign-In

Run these steps at the **first deploy of each surface** — Storefront first, then Staff (see the sequencing
note in `prd-google-sign-in.md`). They cover what no automated layer can: the real consent screen, exact
redirect-URI matching, and the cross-client rejection that is enforced by Google rather than by our code.

The integration tests exercise the exchange against a stub token endpoint. A stub cannot prove that Google
agrees with our client registration, and it cannot prove that Google refuses a Storefront code at the staff
client — that refusal is the whole of the surface isolation (ADR 0011), so it is verified here by hand.

## Prerequisites

- Two OAuth clients exist in the `multiticketing` Google Cloud project, `staff` and `storefront`, each with
  exactly one authorized redirect URI:
  - staff → `https://pos.multiticketing.com/api/auth/google/callback`
  - storefront → `https://discover.multiticketing.com/api/customer/auth/google/callback`
- `GOOGLE_STAFF_CLIENT_ID` / `_SECRET` and `GOOGLE_STOREFRONT_CLIENT_ID` / `_SECRET` are in Secret Manager
  and attached to the API revision
- Each frontend revision carries its own client ID and redirect URI
- `terraform apply` has run and the revisions are serving
- A Google account whose address has **no** Ticket Sales, and one that **does** — the second is what proves
  the record is reused rather than duplicated

## 1. Confirm the token endpoint is pinned

```bash
gcloud run services describe prod-ticket-pos-api --region us-east1 \
  --format='value(spec.template.spec.containers[0].env)' | grep -i google_token_endpoint
```

Expect **no match**. `GOOGLE_TOKEN_ENDPOINT` set in production is a startup failure by design (PRD decision
10) — if the service is running and this returns a value, that guard is not working, and anyone who can set
an environment variable can redirect who the platform trusts. Stop and fix before continuing.

## 2. Storefront — a Customer who has bought before

1. Open `https://discover.multiticketing.com/signin` in a clean profile
2. Confirm the Google button sits **above** the passcode form (PRD decision 6)
3. Tap it; confirm the consent screen names Multiticketing and requests **email only** — no name, no profile
   (decision 4). A consent screen asking for profile means the scope is wrong
4. Confirm you are offered an account picker rather than being signed in silently (`prompt=select_account`)
5. Choose the account whose address has sales; confirm you land in the **Customer Area** and those sales are
   there

A `redirect_uri_mismatch` at Google is the usual first failure: the registered URI must match what the app
sends **exactly**, including scheme, host, port and trailing path. A custom domain in one place and a
`run.app` URL in the other will fail here.

## 3. Storefront — return-to, and a Customer who has not bought

1. From an Event page, follow the sign-in affordance rather than going to `/signin` directly
2. Complete Google sign-in; confirm you are returned **to that Event page**, not to `/tickets`
3. Sign out, then sign in with the Google account that has no sales
4. Confirm the empty **Customer Area** renders, and that it says a different email address may hold
   purchases and links to sign in with it (decision 5)

Step 4 is the mitigation for the one failure mode this feature makes more visible. If that copy is missing,
the dead end is silent and this is the check that would have caught it.

## 4. Storefront — verification actually happened

```bash
gcloud run services logs read prod-ticket-pos-api --region us-east1 --limit 50 | grep -i google
```

Then confirm in the database that the Customer signed in at step 3 has `verified_at` set and that there is
**one** row for the address, not two:

```sql
SELECT id, email, verified_at FROM customers WHERE email = '<the address>';
```

Two rows means normalisation disagreed somewhere; one row with a null `verified_at` means Google sign-in did
not run the verification step (PRD decision 1) and the person is not a **Verified Customer**.

## 5. Cross-surface isolation

This is the step a stub cannot replace.

1. Begin a Google sign-in on the **Storefront** and stop at the callback, capturing the `code` query
   parameter before the page completes (browser devtools, network tab, the request to
   `/api/customer/auth/google/callback`)
2. POST that code to the **staff** verify endpoint, with a staff-shaped payload:

```bash
API_URL=$(gcloud run services describe prod-ticket-pos-api --region us-east1 --format='value(status.url)')

curl -s -X POST "$API_URL/api/v1/auth/google/verify" \
  -H "Authorization: Bearer $(gcloud auth print-identity-token)" \
  -H 'Content-Type: application/json' \
  -d '{"code":"<storefront code>","code_verifier":"<verifier>","redirect_uri":"https://discover.multiticketing.com/api/customer/auth/google/callback"}'
```

Expect a failure envelope — Google rejects the exchange with `invalid_grant` because the code is bound to
the Storefront client. A **success here is a critical finding**: it means both surfaces are sharing one
OAuth client, and a Storefront sign-in can be turned into a **Staff Session**.

## 6. Staff — the fork is unchanged

Run at the Staff deploy, not before.

1. Open `https://pos.multiticketing.com/login`; confirm the email form is first and Google sits **below** it
   (decision 6)
2. Sign in with Google as a Member of exactly one Organization; confirm that Organization is auto-selected
   and you land on the dashboard — identical to the passcode path (story 11, 12)
3. Sign in with Google as a Member of several; confirm the organization picker
4. Sign in with a Google account holding **no** membership; confirm the create-organization fork, and that it
   mentions an invitation may have gone to a different address (decision 5)
5. Perform one write — create a draft event — and confirm it persists

Step 4 is the staff half of the mismatch mitigation, and the reason Google is not the default button on this
surface.

## 7. Failure modes are generic

1. Start a Google sign-in on either surface and press **Cancel** at the consent screen
2. Confirm you land back on the ordinary sign-in page with the passcode form usable, and a generic message
3. Start a sign-in, wait past ten minutes, then complete it
4. Confirm the same generic outcome — not an error page, not a stack trace, not a message that distinguishes
   an expired attempt from an unknown email (decision 9)

Any message that varies by *why* it failed is an enumeration oracle and undoes what the passcode request
endpoint deliberately protects.

## 8. Passcode sign-in still works

On both surfaces, sign in with a **One-time Passcode** and confirm nothing about it has changed. This path
carries every person without a Google account and must not have been disturbed.

## Rollback

Google Sign-In adds no schema and owns no data, so rollback is removing the entry point:

1. Deploy the previous frontend revision for the affected surface — the button disappears, the passcode form
   is untouched, and every existing session keeps working (nothing records how it was created, PRD decision 7)

```bash
gcloud run services update-traffic prod-ticket-pos-storefront --region us-east1 --to-revisions=<previous>=100
```

2. Leave the API revision in place. Its verify endpoints become unreachable from any browser but harm
   nothing; they cannot be called without a code from a client whose secret only the API holds.

Sessions minted by Google are indistinguishable from passcode sessions and survive the rollback. There is
nothing to clean up and no data to unwind.
