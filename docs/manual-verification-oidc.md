# Manual verification: frontend → API OIDC ID tokens

Use these steps at the **first deploy** to confirm that Staff and Storefront authenticate themselves to
the Go API with a Google-signed OIDC ID token (ADR 0008). This path cannot be exercised locally: there is
no metadata server outside Google compute, and a mocked one would only test the mock. The parity stack
covers the *other* half — that a missing metadata server is skipped cleanly.

## Prerequisites

- `terraform apply` has run: API deployed with unauthenticated invocation disabled, Staff and Storefront
  service accounts granted `roles/run.invoker` on the API service
- `gcloud` authenticated against project `multiticketing`, region `us-east1`
- Staff and Storefront revisions built from a commit that includes this change

The module currently defines only the `api` and `migrate` services; the Staff and Storefront services,
their runtime service accounts, and their `roles/run.invoker` bindings on the API arrive with the
frontend deployment work. This runbook assumes they are in place. Service names below follow the
module's `${environment}-ticket-pos-<service>` convention with `environment = prod`.

## 1. Confirm the API rejects unauthenticated callers

```bash
API_URL=$(gcloud run services describe prod-ticket-pos-api --region us-east1 --format='value(status.url)')

curl -s -o /dev/null -w '%{http_code}\n' "$API_URL/api/v1/public/events"
# Expect 403 — Cloud Run rejects before the container is reached

curl -s -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer $(gcloud auth print-identity-token)" \
  "$API_URL/api/v1/public/events"
# Expect 200 — your own identity has run.invoker
```

If the first call returns 200, the service is still public: check
`google_cloud_run_v2_service_iam_*` for an `allUsers` binding.

## 2. Confirm the invoker bindings

```bash
gcloud run services get-iam-policy prod-ticket-pos-api --region us-east1
```

Expect exactly two `roles/run.invoker` members, the Staff and Storefront runtime service accounts. No
`allUsers`.

## 3. Storefront — anonymous read path

1. Open `https://discover.multiticketing.com/`
2. Confirm event cards render (server-rendered, so this is a real API call carrying the ID token)
3. Open an organization page and an event detail page; confirm both render

An empty explorer with no error page usually means the API call failed for a *non-auth* reason
(`fetchData` degrades to `null`); a Next error page means token acquisition itself failed — check step 5.

## 4. Staff — session path

The point of this step is that **two** credentials travel on the same request: the ID token in
`X-Serverless-Authorization` (consumed and stripped by Cloud Run) and the user session in
`Authorization`. If the ID token had been put in `Authorization`, browsing would still 403 at the app
layer even though IAM passed.

1. Sign in at `https://pos.multiticketing.com/login` (see `manual-verification-auth.md`; OTP delivery is
   still log-only, so read the code from the API logs)
2. Confirm the dashboard loads with your email and organization in the Session section
3. Open Events, then an event's Ticket types and Sales tabs
4. Perform one write — e.g. create a draft event — and confirm it persists after reload

Any 403 here that says `FORBIDDEN` in the JSON envelope is the *app's* authorization, which is fine and
expected in the no-active-org case. A bare 403 with an HTML/text body is Cloud Run IAM refusing the call.

## 5. Confirm the token is actually being sent

Check the API's request logs — every successful frontend call should appear:

```bash
gcloud run services logs read prod-ticket-pos-api --region us-east1 --limit 50
```

And the frontends' logs should contain **no** `ServiceAuthError`:

```bash
gcloud run services logs read prod-ticket-pos-staff --region us-east1 --limit 100 | grep -i serviceauth
gcloud run services logs read prod-ticket-pos-storefront --region us-east1 --limit 100 | grep -i serviceauth
```

`Failed to obtain an OIDC ID token for <audience>` means the metadata server refused. Usual causes: the
service account was not attached to the revision, or `API_URL` on the frontend does not match the API
service URL (the audience must be the API's own URL, no trailing slash, no custom domain).

## 6. Confirm caching, not per-request minting

Tokens are cached until a minute before expiry, so a busy page should not produce one metadata call per
API call. Load the storefront explorer and page through "Load more" a few times, then confirm the
frontend revision has not logged repeated token fetches (there is no log line per fetch — the check is
simply that latency does not degrade and no `ServiceAuthError` bursts appear).

## Rollback

If the frontends cannot authenticate and the site must be restored immediately:

```bash
gcloud run services add-iam-policy-binding prod-ticket-pos-api --region us-east1 \
  --member=allUsers --role=roles/run.invoker
```

This makes the API publicly reachable — the app's own session authorization still applies, but this is a
temporary measure only. Remove the binding (and re-run `terraform apply` to reconcile state) as soon as
the token path works.
