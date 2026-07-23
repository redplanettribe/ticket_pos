# Every credential the application needs at runtime, held in Secret Manager so
# that nothing is committed and nothing sits in a Cloud Run env var in plaintext.
#
# The secret *values* are set here because Terraform is what generates them.
# They are never exposed as outputs — outputs land in state and print to any
# terminal running `terraform output`. Consumers get the secret's resource name
# and read the value at runtime; wiring Cloud Run to these is #47.
#
# Read access for the workload is granted in #48 with the rest of workload IAM.

resource "google_secret_manager_secret" "database_password" {
  project   = var.project_id
  secret_id = "${var.environment}-ticket-pos-db-password"
  labels    = local.common_labels

  replication {
    # Google chooses and manages the replica regions. The alternative pins
    # replicas by hand, which buys data-residency control this project has no
    # requirement for.
    auto {}
  }
}

resource "google_secret_manager_secret_version" "database_password" {
  secret      = google_secret_manager_secret.database_password.id
  secret_data = random_password.database.result
}

resource "google_secret_manager_secret" "storage_access_key" {
  project   = var.project_id
  secret_id = "${var.environment}-ticket-pos-s3-access-key"
  labels    = local.common_labels

  replication {
    auto {}
  }
}

# The HMAC access ID is not itself a secret — it is the public half of the pair,
# and it appears in the signature of every presigned URL the browser receives.
# It lives here anyway so the API reads both halves from one place rather than
# taking one from a Terraform-rendered env var and one from Secret Manager.
resource "google_secret_manager_secret_version" "storage_access_key" {
  secret      = google_secret_manager_secret.storage_access_key.id
  secret_data = google_storage_hmac_key.app.access_id
}

resource "google_secret_manager_secret" "storage_secret_key" {
  project   = var.project_id
  secret_id = "${var.environment}-ticket-pos-s3-secret-key"
  labels    = local.common_labels

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "storage_secret_key" {
  secret      = google_secret_manager_secret.storage_secret_key.id
  secret_data = google_storage_hmac_key.app.secret
}
