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
  value       = local.image_repository_url
}

# No secret value is an output. Outputs are stored in state and printed by
# `terraform output` with no redaction beyond a `sensitive` marker, so the
# credentials this module generates are published only through Secret Manager.
# What follows are the names and addresses needed to *find* them.

output "network_id" {
  description = "Self link of the VPC."
  value       = google_compute_network.main.id
}

output "cloud_run_subnet_id" {
  description = "Self link of the subnet Cloud Run attaches to for direct VPC egress."
  value       = google_compute_subnetwork.cloud_run.id
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

output "database_url_secret_id" {
  description = "Full resource name of the Secret Manager secret holding the whole DATABASE_URL the API and the migrate Job mount."
  value       = google_secret_manager_secret.database_url.id
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
  value       = local.storage_endpoint
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

# --- Cloud Run ----------------------------------------------------------------

output "api_service_name" {
  description = "Cloud Run service name for the API. What `gcloud run deploy` and `gcloud run services describe` take."
  value       = google_cloud_run_v2_service.api.name
}

output "api_service_url" {
  description = "HTTPS URL of the API service. It rejects every request that does not carry a Google-signed OIDC ID token from a principal holding roles/run.invoker on it (ADR 0008); granting that role is #48."
  value       = google_cloud_run_v2_service.api.uri
}

output "api_service_account_email" {
  description = "Dedicated runtime identity of the API service. Holds Cloud SQL client, objectAdmin on the media bucket, and secretAccessor on its own three secrets — nothing else."
  value       = google_service_account.api.email
}

output "api_image" {
  description = "Image reference the service and Job were created with. Push here before the first apply; afterwards Terraform no longer owns which image runs."
  value       = local.api_image
}

output "migrate_job_name" {
  description = "Cloud Run Job name. `gcloud run jobs execute <name> --region <region> --wait` applies the schema."
  value       = google_cloud_run_v2_job.migrate.name
}

output "migrate_service_account_email" {
  description = "Dedicated runtime identity of the migrate Job. Reads only the database URL secret."
  value       = google_service_account.migrate.email
}

output "staff_service_url" {
  description = "Staff's run.app URL. Works immediately; the custom domain does not until DNS resolves."
  value       = google_cloud_run_v2_service.staff.uri
}

output "storefront_service_url" {
  description = "Storefront's run.app URL."
  value       = google_cloud_run_v2_service.storefront.uri
}

output "staff_service_account_email" {
  description = "Staff's runtime identity. This is one of exactly two principals holding run.invoker on the API."
  value       = google_service_account.staff.email
}

output "storefront_service_account_email" {
  description = "Storefront's runtime identity."
  value       = google_service_account.storefront.email
}

# The whole point of not delegating DNS to Cloud DNS: these have to be created by
# hand at Namecheap, so Terraform prints them rather than leaving an operator to
# find them in the console. Empty until the mappings report their resource
# records, which is immediately after create.
output "dns_records" {
  description = "DNS records to create at the registrar for the custom domains. Each entry is the record set Cloud Run expects for that hostname."
  value = {
    for k, m in merge(
      length(google_cloud_run_domain_mapping.staff) > 0 ? { (var.staff_domain) = google_cloud_run_domain_mapping.staff[0] } : {},
      length(google_cloud_run_domain_mapping.storefront) > 0 ? { (var.storefront_domain) = google_cloud_run_domain_mapping.storefront[0] } : {},
      ) : k => [
      for r in m.status[0].resource_records : {
        type   = r.type
        name   = r.name
        rrdata = r.rrdata
      }
    ]
  }
}
