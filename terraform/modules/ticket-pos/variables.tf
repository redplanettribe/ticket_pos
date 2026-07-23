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

variable "storage_s3_region" {
  description = "Region name the API signs S3-compatible requests with. GCS checks it against the bucket location in the SigV4 credential scope of a presigned URL, so a mismatch fails uploads rather than reads. Defaults to the deployment region, which is where the bucket is."
  type        = string
  default     = null
}

# --- Network ------------------------------------------------------------------

variable "cloud_run_subnet_cidr" {
  description = "Range for the subnet Cloud Run uses for direct VPC egress. Every running instance holds one address. Must not overlap the 10.240.0.0/24 reserved for the Cloud SQL peering, and cannot be narrowed after creation."
  type        = string
  default     = "10.241.0.0/24"

  validation {
    condition     = can(cidrhost(var.cloud_run_subnet_cidr, 0))
    error_message = "cloud_run_subnet_cidr must be a valid CIDR block."
  }
}

# --- API service and migrate Job ----------------------------------------------

variable "api_image_name" {
  description = "Image name within the Artifact Registry repository. The service and the migrate Job run this same image, differing only in entrypoint."
  type        = string
  default     = "api"
}

variable "api_image_tag" {
  description = "Tag used when the service and Job are first created. Terraform does not own the running image afterwards — deploys roll a new revision and the image field is in ignore_changes — so changing this does not deploy anything."
  type        = string
  default     = "latest"
}

variable "api_min_instances" {
  description = "Instances kept warm. Zero means the deployment costs nothing while idle and the first request after idling pays a cold start."
  type        = number
  default     = 0

  validation {
    condition     = var.api_min_instances >= 0
    error_message = "api_min_instances cannot be negative."
  }
}

variable "api_max_instances" {
  description = "Ceiling on concurrent instances. Bounds both the bill and the number of Postgres connection pools opened against a db-g1-small."
  type        = number
  default     = 4

  validation {
    condition     = var.api_max_instances >= 1
    error_message = "api_max_instances must be at least 1."
  }
}

variable "api_concurrency" {
  description = "Requests one instance serves at a time. Raising it trades instance count for pressure on each instance's database pool."
  type        = number
  default     = 80

  validation {
    condition     = var.api_concurrency >= 1 && var.api_concurrency <= 1000
    error_message = "api_concurrency must be between 1 and 1000."
  }
}

variable "api_request_timeout_seconds" {
  description = "How long Cloud Run waits for a response before killing the request. Cloud Run's own default; lowering it would put a new ceiling on the slowest existing endpoint (sale import parses an uploaded file in memory) that nothing else in the system imposes."
  type        = number
  default     = 300

  validation {
    condition     = var.api_request_timeout_seconds >= 1 && var.api_request_timeout_seconds <= 3600
    error_message = "api_request_timeout_seconds must be between 1 and 3600."
  }
}

variable "api_cpu" {
  description = "CPU limit per API instance. Billed only while a request is in flight."
  type        = string
  default     = "1"
}

variable "api_memory" {
  description = "Memory limit per API instance. This is what an idle instance is billed for when min_instances is above zero."
  type        = string
  default     = "512Mi"
}

variable "api_log_level" {
  description = "LOG_LEVEL for the API and the migrate Job. One of debug, info, warn, error."
  type        = string
  default     = "info"

  validation {
    condition     = contains(["debug", "info", "warn", "error"], var.api_log_level)
    error_message = "api_log_level must be one of debug, info, warn, error."
  }
}

variable "migrate_cpu" {
  description = "CPU limit for a migration task."
  type        = string
  default     = "1"
}

variable "migrate_memory" {
  description = "Memory limit for a migration task."
  type        = string
  default     = "512Mi"
}

variable "migrate_timeout_seconds" {
  description = "How long a migration task may run before Cloud Run kills it. Note that cmd/migrate imposes its own 30s context deadline, so this is the outer bound, not the effective one."
  type        = number
  default     = 900

  validation {
    condition     = var.migrate_timeout_seconds >= 1
    error_message = "migrate_timeout_seconds must be at least 1."
  }
}

variable "migrate_max_retries" {
  description = "Retries after a failed migration task. Zero on purpose: a failed migration is a thing to read, not to repeat."
  type        = number
  default     = 0

  validation {
    condition     = var.migrate_max_retries >= 0
    error_message = "migrate_max_retries cannot be negative."
  }
}
