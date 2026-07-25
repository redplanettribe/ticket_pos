# Production is a thin root over the shared module: what is prod-specific lives
# here (project, region, state prefix), everything else lives in the module.
# Adding staging later is a sibling directory, not a refactor.
module "ticket_pos" {
  source = "../../modules/ticket-pos"

  project_id  = var.project_id
  region      = var.region
  environment = "prod"

  # The Staff app's production origin. Browsers PUT cover images and logos
  # straight to the bucket with presigned URLs, so this is the origin the
  # bucket's CORS policy has to accept. It is a prod fact, hence it lives here.
  storage_cors_origins = [var.staff_origin]

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
}
