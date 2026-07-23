output "artifact_registry_host" {
  description = "Registry host for `gcloud auth configure-docker`."
  value       = module.ticket_pos.artifact_registry_host
}

output "image_repository_url" {
  description = "Prefix for production image tags."
  value       = module.ticket_pos.image_repository_url
}

output "database_connection_name" {
  description = "`project:region:instance` for the Cloud SQL connector and proxy."
  value       = module.ticket_pos.database_connection_name
}

output "database_private_ip" {
  description = "Private IP of the production database, reachable only from inside the VPC."
  value       = module.ticket_pos.database_private_ip
}

output "storage_bucket_name" {
  description = "Public media bucket serving covers/ and logos/."
  value       = module.ticket_pos.storage_bucket_name
}

output "api_service_name" {
  description = "Cloud Run service name for the production API."
  value       = module.ticket_pos.api_service_name
}

output "api_service_url" {
  description = "Production API URL. Unauthenticated requests are rejected by Cloud Run; callers need an OIDC ID token (ADR 0008)."
  value       = module.ticket_pos.api_service_url
}

output "api_service_account_email" {
  description = "Least-privilege runtime identity of the production API."
  value       = module.ticket_pos.api_service_account_email
}

output "api_image" {
  description = "Image reference the API service and migrate Job were created with."
  value       = module.ticket_pos.api_image
}

output "migrate_job_name" {
  description = "Cloud Run Job that applies schema migrations. Execute it before deploying a new API revision."
  value       = module.ticket_pos.migrate_job_name
}

# Secret *names*, never values. Read a value with:
#   gcloud secrets versions access latest --secret=prod-ticket-pos-db-password
output "secret_ids" {
  description = "Secret Manager resource names for the credentials the workload needs."
  value = {
    database_url       = module.ticket_pos.database_url_secret_id
    database_password  = module.ticket_pos.database_password_secret_id
    storage_access_key = module.ticket_pos.storage_access_key_secret_id
    storage_secret_key = module.ticket_pos.storage_secret_key_secret_id
  }
}
