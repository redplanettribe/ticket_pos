# The application database.
#
# Private IP only. Nothing outside the VPC peering can open a socket to it, so
# the blast radius of a leaked password is bounded by network reachability as
# well as by the password itself.
resource "google_sql_database_instance" "postgres" {
  project = var.project_id
  name    = "${var.environment}-ticket-pos"
  region  = var.region

  database_version = var.database_version

  # Terraform-level guard: `terraform destroy` refuses to touch this instance
  # while it is true. `settings.deletion_protection_enabled` below is the
  # separate API-level guard, which also blocks deletion from gcloud and the
  # console. Both are needed; neither implies the other.
  deletion_protection = true

  settings {
    tier              = var.database_tier
    edition           = "ENTERPRISE"
    availability_type = "ZONAL"
    # ZONAL is deliberate (ADR 0007): a zonal outage takes the system down, and
    # that is an accepted risk until real money flows through it. The upgrade
    # path is this one line -> "REGIONAL", which provisions a synchronous
    # standby in a second zone and roughly doubles the database bill. Changing
    # it is an in-place update, not a recreate.

    disk_type = "PD_SSD"
    # A floor, not a ceiling: autoresize grows the disk when it fills, and a
    # Cloud SQL disk can never shrink. If a future plan ever proposes reducing
    # disk_size back to this value, raise the variable rather than applying it.
    disk_size       = var.database_disk_size_gb
    disk_autoresize = true
    user_labels     = local.common_labels

    backup_configuration {
      enabled = true
      # UTC. Early morning here is the middle of the night in us-east1.
      start_time                     = var.database_backup_start_time
      point_in_time_recovery_enabled = true
      # PITR replays write-ahead logs on top of the nightly backup, so recovery
      # granularity is a moment rather than a day. The log retention below is
      # what bounds how far back "a moment" can be.
      transaction_log_retention_days = var.database_transaction_log_retention_days

      backup_retention_settings {
        retained_backups = var.database_retained_backups
        retention_unit   = "COUNT"
      }
    }

    ip_configuration {
      # No public IP. This is the reason the VPC and the peering exist.
      ipv4_enabled                                  = false
      private_network                               = google_compute_network.main.id
      enable_private_path_for_google_cloud_services = false
      ssl_mode                                      = "ENCRYPTED_ONLY"
    }

    maintenance_window {
      day          = var.database_maintenance_day
      hour         = var.database_maintenance_hour
      update_track = "stable"
    }

    # API-level guard, independent of Terraform's own.
    deletion_protection_enabled = true
  }

  # The instance cannot be given a private IP until the peering it borrows that
  # IP from exists. Terraform cannot infer this from the network reference alone.
  depends_on = [google_service_networking_connection.private_services]

  lifecycle {
    prevent_destroy = true
  }
}

resource "google_sql_database" "app" {
  project  = var.project_id
  instance = google_sql_database_instance.postgres.name
  name     = var.database_name

  # Postgres 18 defaults to the ICU provider; these match what the local
  # docker-compose Postgres and CI run, so collation-dependent ordering in
  # queries behaves the same in both places.
  charset   = "UTF8"
  collation = "en_US.UTF8"
}

# The password is generated, never chosen. It exists in three places: Terraform
# state (in the private, versioned state bucket), the Cloud SQL user, and the
# Secret Manager version in secrets.tf. It is never in a .tf file, a .tfvars
# file, or a Terraform output.
resource "random_password" "database" {
  length = 32

  # Restricted to URL-unreserved punctuation so the password can be dropped into
  # a DATABASE_URL without percent-encoding. Cloud Run env vars and connection
  # strings are where this value gets used, and escaping bugs there fail at
  # runtime with an authentication error that reads like a wrong password.
  special          = true
  override_special = "-_.~"
}

resource "google_sql_user" "app" {
  project  = var.project_id
  instance = google_sql_database_instance.postgres.name
  name     = var.database_user
  password = random_password.database.result
}
