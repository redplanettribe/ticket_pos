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

output "staff_service_url" {
  description = "Staff's run.app URL."
  value       = module.ticket_pos.staff_service_url
}

output "storefront_service_url" {
  description = "Storefront's run.app URL."
  value       = module.ticket_pos.storefront_service_url
}

output "dns_records" {
  description = "Records to create at Namecheap for the custom domains."
  value       = module.ticket_pos.dns_records
}

# --- CI deploy (Workload Identity Federation) ---------------------------------
# These two are the values the deploy workflow's google-github-actions/auth step
# needs. Neither is a secret: the provider name is a resource path and the SA
# email is public. There is no key to output because none is ever created.

output "deploy_service_account_email" {
  description = "Identity GitHub Actions impersonates. Set as `service_account` in the deploy workflow's auth step."
  value       = module.ticket_pos.deploy_service_account_email
}

output "workload_identity_provider" {
  description = "Full resource name of the GitHub OIDC provider. Set as `workload_identity_provider` in the deploy workflow's auth step."
  value       = module.ticket_pos.workload_identity_provider
}

output "digest_email_dns_setup" {
  description = "Operator checklist for the Follow Digest's separate sending domain (#225, ADR 0030). Terraform creates none of it: the Resend domain and its DKIM/SPF/DMARC records are added by hand, as the transactional domain's were."
  value       = module.ticket_pos.digest_email_dns_setup
}

output "reversal_reconciler_job_name" {
  description = "Cloud Scheduler job driving the reversal drain. `gcloud scheduler jobs pause <name> --location us-east1` stops the tick immediately; follow it with the matching Terraform change so the next apply does not resume it."
  value       = module.ticket_pos.reversal_reconciler_job_name
}

output "answer_purge_job_name" {
  description = "Cloud Scheduler job driving the Abandoned Answer Purge. `gcloud scheduler jobs pause <name> --location us-east1` stops it immediately; follow it with the matching Terraform change so the next apply does not resume it. Reach for this before investigating anything about the purge — it is the one job whose runs cannot be undone."
  value       = module.ticket_pos.answer_purge_job_name
}

output "follow_digest_job_names" {
  description = "The two Cloud Scheduler jobs driving the Follow Digest. `gcloud scheduler jobs pause <name> --location us-east1` stops either immediately; follow it with the matching Terraform change so the next apply does not resume it. Pausing the enqueue stops next week's send; pausing the drain holds this week's Digests in the queue."
  value = {
    enqueue = module.ticket_pos.follow_digest_enqueue_job_name
    drain   = module.ticket_pos.follow_digest_drain_job_name
  }
}
