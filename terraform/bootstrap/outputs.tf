output "state_bucket_name" {
  description = "Name to put in the `backend \"gcs\"` block of every root configuration."
  value       = google_storage_bucket.terraform_state.name
}

output "state_bucket_url" {
  description = "gs:// URL of the state bucket."
  value       = google_storage_bucket.terraform_state.url
}
