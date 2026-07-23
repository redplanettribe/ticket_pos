output "artifact_registry_host" {
  description = "Registry host for `gcloud auth configure-docker`."
  value       = module.ticket_pos.artifact_registry_host
}

output "image_repository_url" {
  description = "Prefix for production image tags."
  value       = module.ticket_pos.image_repository_url
}
