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

# Secret *names*, never values. Read a value with:
#   gcloud secrets versions access latest --secret=prod-ticket-pos-db-password
output "secret_ids" {
  description = "Secret Manager resource names for the credentials the workload needs."
  value = {
    database_password  = module.ticket_pos.database_password_secret_id
    storage_access_key = module.ticket_pos.storage_access_key_secret_id
    storage_secret_key = module.ticket_pos.storage_secret_key_secret_id
  }
}
