# Transactional email via Resend (ADR 0009).
#
# The API selects Resend over the logging sender purely on the presence of
# RESEND_API_KEY. This file is written so that applying it BEFORE a key exists is
# a no-op on behaviour: the secret container is created empty, no version is
# written, the env var is not mounted, and the service keeps logging OTP codes to
# Cloud Logging — exactly today's behaviour. Supplying the key (see below) is what
# switches delivery on, with no further Terraform change to the service.

variable "resend_api_key" {
  description = "Resend API key. Leave empty to keep the API on the logging email sender; supply it (via TF_VAR_resend_api_key, never a committed tfvars) to switch on real delivery."
  type        = string
  default     = ""
  sensitive   = true
}

variable "email_from" {
  description = "RFC 5322 From header for outbound mail. The address must live on a domain verified in Resend."
  type        = string
  default     = "Multiticketing <noreply@send.multiticketing.com>"
}

# The secret container always exists, so the accessor grant and the (future)
# version have a stable target. An empty container holds no versions and costs
# nothing.
resource "google_secret_manager_secret" "resend_api_key" {
  project   = var.project_id
  secret_id = "${var.environment}-ticket-pos-resend-api-key"

  labels = local.common_labels

  replication {
    auto {}
  }
}

# The value is written only when a key is supplied. Until then there is no
# version, and the service's secret env below is not mounted.
resource "google_secret_manager_secret_version" "resend_api_key" {
  count = var.resend_api_key == "" ? 0 : 1

  secret      = google_secret_manager_secret.resend_api_key.id
  secret_data = var.resend_api_key
}

# The API's runtime identity may read this one secret. Granted unconditionally so
# the grant is not entangled with whether a version exists yet.
resource "google_secret_manager_secret_iam_member" "api_resend_api_key" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.resend_api_key.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.api.email}"
}
