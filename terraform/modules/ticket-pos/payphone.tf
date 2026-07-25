# The PayPhone merchant credentials behind Online Sales (ADR 0012, spec #81).
#
# The platform holds the single PayPhone merchant account: one Bearer token and
# one store id for the whole deployment, settled with Organizations off-system.
# The API selects the real PayPhone Payment Provider purely on the presence of
# BOTH values (internal/platform/config.go, PayPhoneConfig.Configured); with
# either missing it runs the stub provider — fine locally, a dead checkout in
# production, because the Storefront's stub interstitial is a 404 on a
# production build. Supplying the pair is what switches real payments on, with
# no further Terraform change to the service.
#
# Like the Resend key and the Google credentials, both values are optional and
# supplied out of band: source the gitignored .env (which carries the TF_VAR_
# names) before an apply — `set -a; source .env; set +a` — never a committed
# tfvars. Applying without them sourced REMOVES the secret versions and drops
# production back to the stub, the same trap #73 records for the Resend key.
#
# There is deliberately NO payphone_api_base_url variable. The base-URL
# override exists for the integration suite's fake PayPhone server, and the API
# refuses to start in production with PAYPHONE_API_BASE_URL set: anyone able to
# point Prepare/Confirm elsewhere could "approve" payments no one ever made.

variable "payphone_api_token" {
  description = "Bearer token of the platform's PayPhone merchant account (production credentials from the PayPhone Developer portal, never sandbox). Supplied via TF_VAR_payphone_api_token from a sourced .env. Empty leaves the API on the stub Payment Provider."
  type        = string
  default     = ""
  sensitive   = true
}

variable "payphone_store_id" {
  description = "Store id of the platform's PayPhone merchant account. Not itself a credential, but it lives beside its token so the pair is supplied and rotated together. Empty leaves the API on the stub Payment Provider."
  type        = string
  default     = ""
}

# The store id is held in Secret Manager alongside the token even though it is
# not secret on its own, for the same reason the Google client IDs are: the API
# reads the pair from one place, they rotate together, and a token separated
# from its store id surfaces only as an opaque refusal from PayPhone at
# checkout time.

resource "google_secret_manager_secret" "payphone_api_token" {
  project   = var.project_id
  secret_id = "${var.environment}-ticket-pos-payphone-api-token"

  labels = local.common_labels

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret" "payphone_store_id" {
  project   = var.project_id
  secret_id = "${var.environment}-ticket-pos-payphone-store-id"

  labels = local.common_labels

  replication {
    auto {}
  }
}

# Versions exist only once a value is supplied. Mounting "latest" on a secret
# with no version crashes the service at boot, so the env blocks in
# cloud_run.tf carry the same guard.

resource "google_secret_manager_secret_version" "payphone_api_token" {
  count = var.payphone_api_token == "" ? 0 : 1

  secret      = google_secret_manager_secret.payphone_api_token.id
  secret_data = var.payphone_api_token
}

resource "google_secret_manager_secret_version" "payphone_store_id" {
  count = var.payphone_store_id == "" ? 0 : 1

  secret      = google_secret_manager_secret.payphone_store_id.id
  secret_data = var.payphone_store_id
}

# Granted to the API's runtime identity only, and unconditionally, so the grant
# is not entangled with whether a version exists yet. Neither frontend identity
# appears here: the Storefront only ever receives the hosted payment URL the
# API already prepared, never the credentials that prepared it.

resource "google_secret_manager_secret_iam_member" "api_payphone_api_token" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.payphone_api_token.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.api.email}"
}

resource "google_secret_manager_secret_iam_member" "api_payphone_store_id" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.payphone_store_id.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.api.email}"
}
