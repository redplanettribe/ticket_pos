output "artifact_registry_repository_id" {
  description = "Short name of the Docker repository."
  value       = google_artifact_registry_repository.images.repository_id
}

output "artifact_registry_host" {
  description = "Registry host to authenticate Docker against, e.g. `us-east1-docker.pkg.dev`."
  value       = "${var.region}-docker.pkg.dev"
}

output "image_repository_url" {
  description = "Prefix for image tags: append `/<image>:<tag>` when pushing or deploying."
  value       = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.images.repository_id}"
}

# No secret value is an output. Outputs are stored in state and printed by
# `terraform output` with no redaction beyond a `sensitive` marker, so the
# credentials this module generates are published only through Secret Manager.
# What follows are the names and addresses needed to *find* them.

output "network_id" {
  description = "Self link of the VPC. Cloud Run's direct VPC egress (#47) attaches a subnet to this network."
  value       = google_compute_network.main.id
}

output "database_instance_name" {
  description = "Cloud SQL instance name."
  value       = google_sql_database_instance.postgres.name
}

output "database_connection_name" {
  description = "`project:region:instance`, the address the Cloud SQL connectors and proxy take."
  value       = google_sql_database_instance.postgres.connection_name
}

output "database_private_ip" {
  description = "Private IP of the instance. Reachable only from inside the peered VPC."
  value       = google_sql_database_instance.postgres.private_ip_address
}

output "database_name" {
  description = "Application database on the instance."
  value       = google_sql_database.app.name
}

output "database_user" {
  description = "Application database user. Its password is in the Secret Manager secret named below."
  value       = google_sql_user.app.name
}

output "database_password_secret_id" {
  description = "Full resource name of the Secret Manager secret holding the database password."
  value       = google_secret_manager_secret.database_password.id
}

output "storage_bucket_name" {
  description = "Public media bucket. Value of the API's S3_BUCKET."
  value       = google_storage_bucket.media.name
}

output "storage_endpoint" {
  description = "S3-compatible endpoint for GCS. Value of the API's S3_ENDPOINT and S3_PUBLIC_URL."
  value       = "https://storage.googleapis.com"
}

output "storage_service_account_email" {
  description = "Service account the HMAC key belongs to."
  value       = google_service_account.storage.email
}

output "storage_access_key_secret_id" {
  description = "Full resource name of the Secret Manager secret holding the HMAC access ID."
  value       = google_secret_manager_secret.storage_access_key.id
}

output "storage_secret_key_secret_id" {
  description = "Full resource name of the Secret Manager secret holding the HMAC secret key."
  value       = google_secret_manager_secret.storage_secret_key.id
}
