variable "project_id" {
  description = "GCP project the environment is deployed into."
  type        = string
}

variable "region" {
  description = "Region every regional resource is created in. Cloud Run, Cloud SQL and the bucket must share it."
  type        = string
}

variable "environment" {
  description = "Environment name, e.g. `prod`. Used to name and label resources; nothing here assumes a particular value."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,20}$", var.environment))
    error_message = "environment must be lowercase alphanumeric with hyphens, 2-21 characters."
  }
}

variable "artifact_registry_repository_id" {
  description = "Name of the Docker repository container images are pushed to."
  type        = string
  default     = "ticket-pos"
}
