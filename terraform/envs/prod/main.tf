# Production is a thin root over the shared module: what is prod-specific lives
# here (project, region, state prefix), everything else lives in the module.
# Adding staging later is a sibling directory, not a refactor.
module "ticket_pos" {
  source = "../../modules/ticket-pos"

  project_id  = var.project_id
  region      = var.region
  environment = "prod"

  # Both browser-facing origins. Browsers PUT straight to the bucket with
  # presigned URLs — cover images and logos from Staff, Customer Avatars from the
  # Storefront — so both origins have to be in the bucket's CORS policy or the
  # preflight fails and the upload dies as an opaque network error. Prod facts,
  # hence they live here.
  storage_cors_origins = [var.staff_origin, var.storefront_origin]

  # The hostnames the two browser-facing surfaces answer on. Prod facts, so they
  # live here; the module treats them as optional and skips the domain mappings
  # when they are null. DNS for these is served by Namecheap, so creating the
  # mappings is only half the job — see `terraform output dns_records`.
  staff_domain      = var.staff_domain
  storefront_domain = var.storefront_domain

  # The one repository GitHub Actions may impersonate the deploy service account
  # from (#49). Pinned here explicitly rather than left to the module default so
  # the single security-critical string is visible in the prod root itself: the
  # WIF attribute condition and the impersonation principalSet are both scoped to
  # exactly this repo, so a fork or any other repo under the same owner cannot
  # mint deploy tokens.
  github_repository = var.github_repository

  # Resend API key, supplied via TF_VAR_resend_api_key (from a sourced, gitignored
  # .env — never a committed tfvars). Empty by default, which keeps the API on the
  # logging email sender; a non-empty value creates the secret version and mounts
  # it on the service. TF_VAR_ populates root variables only, so it must be
  # declared here and threaded through — not just on the module.
  resend_api_key = var.resend_api_key
  email_from     = var.email_from

  # The Follow Digest's sending identity (#225, ADR 0030). Its own variables,
  # though this deployment points them at the transactional domain because the
  # Resend plan verifies only one — see digest_email_allow_shared_domain. With
  # the domain shared no separate key is sourced, so unlike the pair above there
  # is no apply that silently removes a secret version and stops Digests.
  digest_email_domain              = var.digest_email_domain
  digest_email_allow_shared_domain = var.digest_email_allow_shared_domain
  digest_resend_api_key            = var.digest_resend_api_key
  digest_email_from                = var.digest_email_from

  # Google Sign-In credentials, one client per surface (ADR 0011). Supplied the
  # same way as the Resend key — sourced .env, never a committed tfvars — and
  # subject to the same trap: applying without sourcing it removes the secret
  # versions and turns the feature off. The redirect URIs are not passed; the
  # module derives them from staff_domain and storefront_domain above, so the
  # registered URI and the sent one cannot drift apart.
  google_staff_client_id          = var.google_staff_client_id
  google_staff_client_secret      = var.google_staff_client_secret
  google_storefront_client_id     = var.google_storefront_client_id
  google_storefront_client_secret = var.google_storefront_client_secret

  # PayPhone merchant credentials (ADR 0012), supplied the same way — sourced
  # .env, never a committed tfvars — and subject to the same trap: applying
  # without them sourced removes the secret versions and drops the API back to
  # the stub Payment Provider, which in production is a dead checkout. There is
  # deliberately no base-URL input: PAYPHONE_API_BASE_URL is refused by the API
  # in production (see payphone.tf in the module).
  payphone_api_token = var.payphone_api_token
  payphone_store_id  = var.payphone_store_id

  # The Reversal Reconciler tick (ADR 0024). Threaded through the root rather
  # than left to the module default because turning it off is an incident move:
  # declared here, pausing is `terraform apply -var reversal_reconciler_enabled=false`
  # from this directory, and the state then agrees with reality instead of a
  # console pause that the next apply silently undoes.
  reversal_reconciler_enabled                  = var.reversal_reconciler_enabled
  reversal_reconciler_schedule                 = var.reversal_reconciler_schedule
  reversal_reconciler_attempt_deadline_seconds = var.reversal_reconciler_attempt_deadline_seconds

  # The Follow Digest jobs (#226, ADR 0030), threaded through for the same reason
  # the reconciler's are: turning the weekly send off is an incident move, and
  # `terraform apply -var follow_digest_enqueue_enabled=false` from this directory
  # leaves the state agreeing with reality.
  follow_digest_enqueue_enabled                  = var.follow_digest_enqueue_enabled
  follow_digest_enqueue_schedule                 = var.follow_digest_enqueue_schedule
  follow_digest_enqueue_attempt_deadline_seconds = var.follow_digest_enqueue_attempt_deadline_seconds
  follow_digest_drain_enabled                    = var.follow_digest_drain_enabled
  follow_digest_drain_schedule                   = var.follow_digest_drain_schedule
  follow_digest_drain_attempt_deadline_seconds   = var.follow_digest_drain_attempt_deadline_seconds
}
