locals {
  # Labels every resource in the environment carries, so a stray resource can be
  # traced back to this module and environment from the console.
  common_labels = {
    managed-by  = "terraform"
    application = "ticket-pos"
    environment = var.environment
  }

  # Bucket names are globally unique across all of GCS, so the project id is the
  # cheapest available disambiguator.
  storage_bucket_name = coalesce(var.storage_bucket_name, "${var.project_id}-ticket-pos-${var.environment}")
}
