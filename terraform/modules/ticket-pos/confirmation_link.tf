# The signing key for Confirmation Links (ADR 0010, PRD #55).
#
# A Confirmation Link is a bearer credential: it opens one Ticket Sale to
# whoever holds it, until that sale's Event has ended plus a grace window.
# Anyone with this key can mint one for any sale, so it lives in Secret Manager
# alongside the Resend key rather than in a plain env var.
#
# Unlike the Resend key it is NOT optional. The API refuses to boot in production
# without it (internal/platform/config.go and internal/server/app.go both check),
# because the alternative — signing with some fallback baked into the binary — is
# indistinguishable from not signing at all. So the value is generated here and a
# version always exists: there is no state in which applying this leaves the API
# unable to start.

resource "random_password" "confirmation_link_secret" {
  length  = 64
  special = false
}

resource "google_secret_manager_secret" "confirmation_link_secret" {
  project   = var.project_id
  secret_id = "${var.environment}-ticket-pos-confirmation-link-secret"

  labels = local.common_labels

  replication {
    auto {}
  }
}

# Rotating this key invalidates every Confirmation Link already sitting in
# somebody's inbox, so it is deliberately not tied to anything that changes on a
# normal apply.
resource "google_secret_manager_secret_version" "confirmation_link_secret" {
  secret      = google_secret_manager_secret.confirmation_link_secret.id
  secret_data = random_password.confirmation_link_secret.result
}

resource "google_secret_manager_secret_iam_member" "api_confirmation_link_secret" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.confirmation_link_secret.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.api.email}"
}
