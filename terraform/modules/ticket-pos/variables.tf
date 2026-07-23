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

# --- Database -----------------------------------------------------------------

variable "database_version" {
  description = "Cloud SQL Postgres major version. Must match what local development and CI run, or migrations are validated against a different engine."
  type        = string
  default     = "POSTGRES_18"
}

variable "database_tier" {
  description = "Cloud SQL machine type. `db-g1-small` is 1 shared vCPU / 1.7GB, roughly $25/month single-zone."
  type        = string
  default     = "db-g1-small"
}

variable "database_disk_size_gb" {
  description = "Initial data disk size in GB. Autoresize is on, so this is a floor rather than a limit."
  type        = number
  default     = 10

  validation {
    condition     = var.database_disk_size_gb >= 10
    error_message = "database_disk_size_gb must be at least 10; Cloud SQL rejects smaller disks."
  }
}

variable "database_backup_start_time" {
  description = "HH:MM UTC at which the daily automated backup starts."
  type        = string
  default     = "05:00"

  validation {
    condition     = can(regex("^([01][0-9]|2[0-3]):[0-5][0-9]$", var.database_backup_start_time))
    error_message = "database_backup_start_time must be HH:MM in 24-hour UTC."
  }
}

variable "database_retained_backups" {
  description = "How many daily automated backups to keep."
  type        = number
  default     = 7

  validation {
    condition     = var.database_retained_backups >= 1
    error_message = "database_retained_backups must be at least 1."
  }
}

variable "database_transaction_log_retention_days" {
  description = "How far back point-in-time recovery can rewind. Longer retention costs more log storage."
  type        = number
  default     = 7

  validation {
    condition     = var.database_transaction_log_retention_days >= 1 && var.database_transaction_log_retention_days <= 35
    error_message = "database_transaction_log_retention_days must be between 1 and 35."
  }
}

variable "database_maintenance_day" {
  description = "Day of week (1 = Monday, 7 = Sunday) on which Google may apply maintenance."
  type        = number
  default     = 7

  validation {
    condition     = var.database_maintenance_day >= 1 && var.database_maintenance_day <= 7
    error_message = "database_maintenance_day must be between 1 and 7."
  }
}

variable "database_maintenance_hour" {
  description = "Hour of day (UTC) at which maintenance may start."
  type        = number
  default     = 6

  validation {
    condition     = var.database_maintenance_hour >= 0 && var.database_maintenance_hour <= 23
    error_message = "database_maintenance_hour must be between 0 and 23."
  }
}

variable "database_name" {
  description = "Application database created on the instance."
  type        = string
  default     = "ticket_pos"
}

variable "database_user" {
  description = "Application database user. Its password is generated and stored in Secret Manager, never set here."
  type        = string
  default     = "ticket_pos_app"
}

# --- Object storage -----------------------------------------------------------

variable "storage_bucket_name" {
  description = "Globally unique name for the public media bucket. Defaults to `<project>-ticket-pos-<environment>`."
  type        = string
  default     = null
}

variable "storage_cors_origins" {
  description = "Origins allowed to PUT to the bucket. This is the Staff app's origin: browsers upload cover images and logos directly using presigned URLs, so an origin missing here fails uploads in production while working against MinIO locally."
  type        = list(string)

  validation {
    condition     = length(var.storage_cors_origins) > 0
    error_message = "storage_cors_origins must list at least the Staff app origin, or direct browser uploads cannot work."
  }

  validation {
    condition     = alltrue([for o in var.storage_cors_origins : can(regex("^https?://[^/]+$", o))])
    error_message = "Each origin must be scheme://host[:port] with no trailing path; browsers match the Origin header literally."
  }
}

variable "storage_cors_max_age_seconds" {
  description = "How long a browser may cache the CORS preflight response."
  type        = number
  default     = 3600
}
