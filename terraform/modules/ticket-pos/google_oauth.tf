# The Google OAuth clients behind Google Sign-In (ADR 0011, PRD #75).
#
# The clients themselves are registered by hand in the Google Cloud Console —
# there is no Terraform resource for an OAuth client, and the consent screen is
# project-wide and reviewed by Google. What lives here is the second half: the
# credentials those registrations produced, and how they reach the workloads.
#
# One client per surface, never a shared one. Google binds an authorization code
# to its issuing client, so a code obtained on the Storefront cannot be redeemed
# for a Staff Session. That is the whole of the cross-surface isolation — it is a
# property of these credentials, not of a check in application code, which is why
# there are four values here and not two.
#
# The split of who holds what is also load-bearing. The two client *secrets* go
# to the API and nowhere else: it is the only thing that exchanges a code with
# Google. The client *IDs* and redirect URIs are public information — they travel
# in the authorization URL the browser follows — and reach the frontends as
# ordinary env vars (cloud_run_frontends.tf). No frontend ever holds a secret.
#
# Like the Resend key, every value is optional and supplied out of band. Applying
# this before the clients exist is a no-op on behaviour: the containers are
# created empty, no versions are written, no env is mounted, and both surfaces
# hide the Google button. Supplying the values is what switches the feature on.
#
# Supply them by sourcing the gitignored .env (which carries the TF_VAR_ names)
# before an apply — `set -a; source .env; set +a` — never a committed tfvars.
# Applying without them REMOVES the secret versions, exactly the trap #73 records
# for the Resend key.

variable "google_staff_client_id" {
  description = "Client ID of the `staff` Google OAuth client. Public information, but it lives beside its secret so the pair is supplied together. Empty hides the Google button on Staff."
  type        = string
  default     = ""
}

variable "google_staff_client_secret" {
  description = "Client secret of the `staff` Google OAuth client. Supplied via TF_VAR_google_staff_client_secret from a sourced .env, never a committed tfvars."
  type        = string
  default     = ""
  sensitive   = true
}

variable "google_storefront_client_id" {
  description = "Client ID of the `storefront` Google OAuth client. Empty hides the Google button on the Storefront."
  type        = string
  default     = ""
}

variable "google_storefront_client_secret" {
  description = "Client secret of the `storefront` Google OAuth client. Supplied via TF_VAR_google_storefront_client_secret from a sourced .env."
  type        = string
  default     = ""
  sensitive   = true
}

# The client IDs are held in Secret Manager alongside the secrets even though
# they are public. The API reads all four from one place, they rotate as a pair,
# and a client ID separated from its secret is the kind of mismatch that surfaces
# only as an opaque `invalid_client` from Google at sign-in time.

resource "google_secret_manager_secret" "google_staff_client_id" {
  project   = var.project_id
  secret_id = "${var.environment}-ticket-pos-google-staff-client-id"

  labels = local.common_labels

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret" "google_staff_client_secret" {
  project   = var.project_id
  secret_id = "${var.environment}-ticket-pos-google-staff-client-secret"

  labels = local.common_labels

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret" "google_storefront_client_id" {
  project   = var.project_id
  secret_id = "${var.environment}-ticket-pos-google-storefront-client-id"

  labels = local.common_labels

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret" "google_storefront_client_secret" {
  project   = var.project_id
  secret_id = "${var.environment}-ticket-pos-google-storefront-client-secret"

  labels = local.common_labels

  replication {
    auto {}
  }
}

# Versions exist only once a value is supplied. Mounting "latest" on a secret
# with no version crashes the service at boot, so the env blocks in cloud_run.tf
# carry the same guard.

resource "google_secret_manager_secret_version" "google_staff_client_id" {
  count = var.google_staff_client_id == "" ? 0 : 1

  secret      = google_secret_manager_secret.google_staff_client_id.id
  secret_data = var.google_staff_client_id
}

resource "google_secret_manager_secret_version" "google_staff_client_secret" {
  count = var.google_staff_client_secret == "" ? 0 : 1

  secret      = google_secret_manager_secret.google_staff_client_secret.id
  secret_data = var.google_staff_client_secret
}

resource "google_secret_manager_secret_version" "google_storefront_client_id" {
  count = var.google_storefront_client_id == "" ? 0 : 1

  secret      = google_secret_manager_secret.google_storefront_client_id.id
  secret_data = var.google_storefront_client_id
}

resource "google_secret_manager_secret_version" "google_storefront_client_secret" {
  count = var.google_storefront_client_secret == "" ? 0 : 1

  secret      = google_secret_manager_secret.google_storefront_client_secret.id
  secret_data = var.google_storefront_client_secret
}

# Granted to the API's runtime identity only, and unconditionally, so the grant
# is not entangled with whether a version exists yet. The two frontend identities
# appear nowhere in this file: they have no business reading a client secret.

resource "google_secret_manager_secret_iam_member" "api_google_staff_client_id" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.google_staff_client_id.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.api.email}"
}

resource "google_secret_manager_secret_iam_member" "api_google_staff_client_secret" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.google_staff_client_secret.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.api.email}"
}

resource "google_secret_manager_secret_iam_member" "api_google_storefront_client_id" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.google_storefront_client_id.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.api.email}"
}

resource "google_secret_manager_secret_iam_member" "api_google_storefront_client_secret" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.google_storefront_client_secret.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.api.email}"
}

# The redirect URI each surface sends to Google, and the one its client is
# registered with. They are the same string in two places by construction — the
# frontend env var below and the Console registration — and a mismatch of scheme,
# host, port or path fails at Google with `redirect_uri_mismatch`. Derived from
# the domain variables so a domain change cannot leave one of them behind.
#
# Without a custom domain there is no stable origin to register (the run.app URL
# changes with the service), so these are empty and the button stays hidden.
locals {
  google_staff_redirect_uri      = var.staff_domain != null ? "https://${var.staff_domain}/api/auth/google/callback" : ""
  google_storefront_redirect_uri = var.storefront_domain != null ? "https://${var.storefront_domain}/api/customer/auth/google/callback" : ""
}
