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
