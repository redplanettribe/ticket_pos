# Frontends authenticate to the API with OIDC ID tokens

The Go API is deployed with `--no-allow-unauthenticated`, and the Staff and Storefront services call it
with a Google-signed **OIDC ID token** fetched from the Cloud Run metadata server, holding
`roles/run.invoker` on the API service. Cloud Run rejects unauthenticated callers before the container is
reached. The obvious alternative — putting the API on internal ingress so it is unreachable from the
internet — fails on a detail: `*.run.app` resolves to public IPs, so callers would need `ALL_TRAFFIC` VPC
egress, which in turn requires **Cloud NAT** at ~$35/mo, more than the rest of the deployment combined.
ID tokens cost nothing, need no secret, and authenticate the *caller* rather than trusting whatever sits
in the VPC.

This is safe because no browser code calls the Go API directly: every client-side `fetch` in both apps
targets a relative `/api/...` Next route handler. The single exception is the presigned upload URL, which
addresses object storage rather than the API.

## Consequences

- The frontends' API layer attaches an `Authorization: Bearer` header in production and skips it locally,
  where no metadata server exists. This is application code that exists only for deployment reasons.
- The API's own session and role authorization is unchanged. This is defence in depth, not the only lock.
- **Integration Partner endpoints cannot be served by this service.** Cloud Run IAM is per-service, not
  per-path, and a load balancer cannot mint ID tokens. Partner access requires a second Cloud Run service
  from the same image, unauthenticated, behind an external load balancer — with a route profile so it
  mounts only partner routes.
