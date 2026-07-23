variable "project_id" {
  description = "GCP project that owns the Terraform state bucket."
  type        = string
}

variable "region" {
  description = "Region the state bucket lives in. Must match the deployment region."
  type        = string
}

variable "state_bucket_name" {
  description = "Globally unique name of the GCS bucket holding Terraform state."
  type        = string
}
