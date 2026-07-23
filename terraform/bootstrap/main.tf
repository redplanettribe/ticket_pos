# The Terraform state bucket.
#
# Versioning is the point of this file. A corrupted or truncated state write is
# recoverable by restoring the previous object generation; without versioning it
# is recoverable only by rebuilding state by hand from the live project.
resource "google_storage_bucket" "terraform_state" {
  name     = var.state_bucket_name
  project  = var.project_id
  location = var.region

  # State is small, frequently read, and never worth a storage-class transition.
  storage_class = "STANDARD"

  versioning {
    enabled = true
  }

  # State files contain secrets in plaintext. Neither ACLs nor public access.
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  # Refuse `terraform destroy` on the bucket that holds every environment's state.
  force_destroy = false

  # Keep a bounded history of superseded versions rather than every write ever made.
  # Twenty generations is far more than any realistic rollback needs.
  lifecycle_rule {
    condition {
      with_state         = "ARCHIVED"
      num_newer_versions = 20
    }
    action {
      type = "Delete"
    }
  }

  lifecycle {
    prevent_destroy = true
  }
}
