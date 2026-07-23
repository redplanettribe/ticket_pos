# Where CI pushes the api, staff, storefront and migrate images. One Docker
# repository holds all of them, distinguished by image name.
resource "google_artifact_registry_repository" "images" {
  project       = var.project_id
  location      = var.region
  repository_id = var.artifact_registry_repository_id
  format        = "DOCKER"
  description   = "Ticket POS container images (${var.environment})"

  labels = local.common_labels
}
