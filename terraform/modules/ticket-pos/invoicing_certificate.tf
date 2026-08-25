# The key the Issuer's signing certificate is kept under (#453, ADR 0059).
#
# The platform's .p12 and its password live in Postgres, each AES-256-GCM
# encrypted under this key; only the certificate's metadata is in clear. A
# leaked database backup exposes ciphertext; this key plus a backup exposes
# the platform's signing key — which is why it lives in Secret Manager beside
# the Confirmation Link key and never in a plain env var.
#
# Unlike the Confirmation Link key it is OPTIONAL to the API: without it the
# service boots and serves everything but certificate upload and signing, which
# answer CERTIFICATE_KEY_NOT_CONFIGURED. Generated here all the same, so that
# a fresh apply leaves invoicing ready rather than dark.
#
# Exactly 32 random bytes, delivered as standard base64: the API refuses to
# start on a key of any other length (internal/platform/config.go), because a
# mistyped secret must never look like a missing one.

resource "random_bytes" "invoicing_certificate_key" {
  length = 32
}

resource "google_secret_manager_secret" "invoicing_certificate_key" {
  project   = var.project_id
  secret_id = "${var.environment}-ticket-pos-invoicing-certificate-key"

  labels = local.common_labels

  replication {
    auto {}
  }
}

# Rotating this key makes the certificate on file unreadable until it is
# uploaded again (CERTIFICATE_UNREADABLE), so it is deliberately not tied to
# anything that changes on a normal apply.
resource "google_secret_manager_secret_version" "invoicing_certificate_key" {
  secret      = google_secret_manager_secret.invoicing_certificate_key.id
  secret_data = random_bytes.invoicing_certificate_key.base64
}

resource "google_secret_manager_secret_iam_member" "api_invoicing_certificate_key" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.invoicing_certificate_key.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.api.email}"
}
