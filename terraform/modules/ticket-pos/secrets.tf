# Every credential the application needs at runtime, held in Secret Manager so
# that nothing is committed and nothing sits in a Cloud Run env var in plaintext.
#
# The secret *values* are set here because Terraform is what generates them.
# They are never exposed as outputs — outputs land in state and print to any
# terminal running `terraform output`. Consumers get the secret's resource name
# and read the value at runtime.
#
# Read access is granted per secret to the two runtime service accounts in
# cloud_run.tf, never project-wide.

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

# The whole connection string, not just the password.
#
# The application reads one variable, DATABASE_URL (internal/platform/config.go),
# and the two halves it would otherwise be assembled from are awkward in
# opposite ways: the password must not appear in a Cloud Run env var, and the
# instance's private IP is not known until after the instance exists. Assembling
# it here means Cloud Run mounts a single secret and the Job mounts the same one,
# with no plaintext credential anywhere in the service definition.
#
# The host is the private IP rather than a DNS name because Cloud SQL private-IP
# instances have no name resolvable from our VPC. If the instance is ever
# recreated the address changes and this secret gets a new version; Cloud Run
# mounts `latest`, so a restart is all that is needed to pick it up.
#
# sslmode=require, matching the instance's ENCRYPTED_ONLY setting: the
# connection is encrypted, and the server certificate is not verified because
# no CA is distributed to the containers. The path is a private VPC address.
resource "google_secret_manager_secret" "database_url" {
  project   = var.project_id
  secret_id = "${var.environment}-ticket-pos-database-url"
  labels    = local.common_labels

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "database_url" {
  secret      = google_secret_manager_secret.database_url.id
  secret_data = local.database_url
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
