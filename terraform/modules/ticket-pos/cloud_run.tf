# The Go API on Cloud Run, and the migrate Job that prepares the schema for it.
#
# Both run the same image from the same digest and differ only in entrypoint
# (backend/Dockerfile builds /server and /migrate side by side). They do not
# share an identity: the service can read object storage credentials, the Job
# cannot, and the Job is the only thing that ever writes DDL.
#
# Migrations run as a Job rather than at service boot because Cloud Run starts
# instances concurrently and migrate.Up takes no advisory lock — two instances
# booting together would race the same DDL. A failed migration at boot is also a
# crash-looping revision rather than a failed job with a log. ADR 0007, decision 5.

# --- Identity -----------------------------------------------------------------

# Never the default compute service account. That account is Editor on the whole
# project by default, so a compromise of the API container would be a compromise
# of everything in it. These two hold exactly the three grants each workload
# needs and nothing else.
resource "google_service_account" "api" {
  project      = var.project_id
  account_id   = "${var.environment}-ticket-pos-api"
  display_name = "Ticket POS ${var.environment} API"
  description  = "Runtime identity of the ${var.environment} API Cloud Run service"
}

resource "google_service_account" "migrate" {
  project      = var.project_id
  account_id   = "${var.environment}-ticket-pos-migrate"
  display_name = "Ticket POS ${var.environment} migrate"
  description  = "Runtime identity of the ${var.environment} schema migration Cloud Run Job"
}

# Cloud SQL client. The application connects over the private IP with pgx rather
# than through the connector, so this is not what makes the connection work —
# the VPC egress below is. It is here so that the Cloud SQL Auth Proxy path
# (`gcloud sql connect`, a debugging sidecar, a future switch away from raw
# private IP) is available to these identities and to no others.
resource "google_project_iam_member" "api_cloudsql_client" {
  project = var.project_id
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${google_service_account.api.email}"
}

resource "google_project_iam_member" "migrate_cloudsql_client" {
  project = var.project_id
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${google_service_account.migrate.email}"
}

# Object access, scoped to this bucket rather than the project. objectAdmin
# covers what platform/storage does: presign puts, read, delete, list a prefix.
#
# The API does not currently authenticate to GCS with this identity — it uses the
# HMAC key from storage.tf, per ADR 0007 — so this grant is what makes the native
# path available without a further IAM change, and what a presigned URL minted by
# the service is allowed to do if signing ever moves to the runtime account.
resource "google_storage_bucket_iam_member" "api_object_admin" {
  bucket = google_storage_bucket.media.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${google_service_account.api.email}"
}

# Secret access, granted on each individual secret. A project-level
# secretAccessor would let either workload read every secret the project ever
# holds, including ones added for unrelated reasons years from now.
#
# The API reads three; the Job reads only the connection string. It has no
# business with object storage credentials.
resource "google_secret_manager_secret_iam_member" "api_database_url" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.database_url.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.api.email}"
}

resource "google_secret_manager_secret_iam_member" "api_storage_access_key" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.storage_access_key.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.api.email}"
}

resource "google_secret_manager_secret_iam_member" "api_storage_secret_key" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.storage_secret_key.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.api.email}"
}

resource "google_secret_manager_secret_iam_member" "migrate_database_url" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.database_url.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.migrate.email}"
}

# --- API service --------------------------------------------------------------

resource "google_cloud_run_v2_service" "api" {
  project  = var.project_id
  name     = "${var.environment}-ticket-pos-api"
  location = var.region

  # Unauthenticated invocation is *disabled*, and in Cloud Run v2 that is not a
  # flag — it is the absence of an `allUsers` binding on roles/run.invoker. There
  # is deliberately no google_cloud_run_v2_service_iam_member granting allUsers
  # anywhere in this module. Cloud Run then rejects any request without a
  # Google-signed OIDC ID token before the container is reached (ADR 0008).
  #
  # Ingress stays open to the internet on purpose. Internal-only ingress would
  # force the calling frontends to route ALL_TRAFFIC through the VPC, which needs
  # Cloud NAT at ~$35/month — more than the rest of this deployment. IAM
  # authenticates the caller instead of trusting the network, and costs nothing.
  ingress = "INGRESS_TRAFFIC_ALL"

  # Cloud Run services carry no state and are recreated by a deploy. The guard
  # that matters is on the database and the bucket.
  deletion_protection = false

  labels = local.common_labels

  template {
    service_account = google_service_account.api.email

    # Direct VPC egress: the instance gets an address in the Cloud Run subnet and
    # can open a socket to the database's private IP. PRIVATE_RANGES_ONLY sends
    # only RFC1918 destinations that way, so calls to Secret Manager, Artifact
    # Registry and storage.googleapis.com still leave over Google's own path and
    # no Cloud NAT is needed.
    vpc_access {
      network_interfaces {
        # Names, not self links: the Cloud Run API stores and returns the short
        # name, so a self link here reads back as a change on every plan.
        network    = google_compute_network.main.name
        subnetwork = google_compute_subnetwork.cloud_run.name
      }
      egress = "PRIVATE_RANGES_ONLY"
    }

    scaling {
      # Scale to zero. The first request after an idle period pays a cold start;
      # the alternative is paying for an always-warm instance continuously, which
      # ADR 0007 chose Cloud Run specifically to avoid.
      min_instance_count = var.api_min_instances
      # A ceiling on both runaway concurrency and runaway bill. Every instance
      # also holds an address in the Cloud Run subnet.
      max_instance_count = var.api_max_instances
    }

    # Postgres connections are the scarce resource behind this service, and each
    # instance opens its own pool. Raising max_instance_count without checking
    # the instance's connection limit is how this falls over.
    max_instance_request_concurrency = var.api_concurrency

    timeout = "${var.api_request_timeout_seconds}s"

    containers {
      image = local.api_image

      ports {
        container_port = 8080
      }

      resources {
        limits = {
          cpu    = var.api_cpu
          memory = var.api_memory
        }
        # CPU only while a request is in flight: an idle instance is billed for
        # memory alone. Background work would break under this, and there is
        # none — ADR 0007 records that Cloud Run has no background worker model.
        cpu_idle          = true
        startup_cpu_boost = true
      }

      # APP_ENV=production and no RUN_MIGRATIONS=true means shouldRunMigrations()
      # in internal/platform/config.go returns false: this service never touches
      # the schema. RUN_MIGRATIONS is left unset rather than set to "false"
      # because "false" and unset behave identically here and an unset variable
      # cannot be flipped by an accidental edit that reads as harmless.
      env {
        name  = "APP_ENV"
        value = "production"
      }

      env {
        name  = "HTTP_ADDR"
        value = ":8080"
      }

      env {
        name  = "LOG_LEVEL"
        value = var.api_log_level
      }

      # Credentials come from Secret Manager, never from a value here. A plain
      # `value` is visible to anyone with run.viewer on the project and is
      # printed by `gcloud run services describe`.
      env {
        name = "DATABASE_URL"
        value_source {
          secret_key_ref {
            secret = google_secret_manager_secret.database_url.secret_id
            # `latest`, not a pinned version: rotating a credential is then a
            # new secret version plus a restart, with no Terraform change.
            version = "latest"
          }
        }
      }

      env {
        name = "S3_ACCESS_KEY"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.storage_access_key.secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "S3_SECRET_KEY"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.storage_secret_key.secret_id
            version = "latest"
          }
        }
      }

      # Not secrets: the bucket name and endpoint are in every image URL the
      # browser already receives.
      env {
        name  = "S3_ENDPOINT"
        value = local.storage_endpoint
      }

      env {
        name  = "S3_PUBLIC_URL"
        value = local.storage_endpoint
      }

      env {
        name  = "S3_BUCKET"
        value = google_storage_bucket.media.name
      }

      # The bucket's location, in S3 naming. GCS validates the region in the
      # SigV4 credential scope of a presigned URL, so a mismatch here surfaces
      # only at upload time as a signature error.
      env {
        name  = "S3_REGION"
        value = local.storage_s3_region
      }

      # Not a secret: the From header is public on every email sent.
      env {
        name  = "EMAIL_FROM"
        value = var.email_from
      }

      # The Storefront origin every Confirmation Link points at. Same source as
      # the Storefront's own metadataBase, and set here rather than derived from
      # the Storefront service's URI, which would make the two services depend on
      # each other. Empty until a custom domain is mapped.
      env {
        name  = "STOREFRONT_BASE_URL"
        value = var.storefront_domain != null ? "https://${var.storefront_domain}" : ""
      }

      # The platform-wide cap on passcode emails per window — the control that
      # still holds when an attacker's per-key identity is unreliable. Wired here
      # so it can be retuned mid-incident by editing a variable, rather than
      # needing a code deploy at exactly the wrong moment. Empty means "use the
      # API's own default"; the API treats a blank value as unset and only
      # rejects a malformed or non-positive one.
      env {
        name  = "OTP_GLOBAL_CEILING"
        value = var.otp_global_ceiling != null ? tostring(var.otp_global_ceiling) : ""
      }

      # Required, not optional: without it the API refuses to start in production
      # rather than sign Confirmation Links with a default (confirmation_link.tf).
      env {
        name = "CONFIRMATION_LINK_SECRET"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.confirmation_link_secret.secret_id
            version = "latest"
          }
        }
      }

      # Mounted only when a Resend key version exists (email.tf). Absent, the API
      # falls back to the logging sender — so this env appearing is precisely what
      # switches production email on. Referencing "latest" when no version existed
      # would crash the service at boot, hence the guard.
      dynamic "env" {
        for_each = var.resend_api_key == "" ? [] : [1]
        content {
          name = "RESEND_API_KEY"
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.resend_api_key.secret_id
              version = "latest"
            }
          }
        }
      }

      # The Follow Digest's own sending identity (#225, ADR 0030, email.tf).
      #
      # Two env vars, both independent of the transactional pair above. The API
      # requires BOTH before it sends a single Digest and defaults NEITHER from
      # EMAIL_FROM or RESEND_API_KEY, so a deployment that has only got halfway
      # sends no marketing mail rather than sending it from the domain the
      # One-time Passcodes depend on.
      env {
        name  = "DIGEST_EMAIL_FROM"
        value = local.digest_email_from
      }

      # Guarded exactly like the transactional key: no version exists until a
      # value is supplied, and "latest" against an empty secret is a boot crash.
      # Absent, the API logs a reason at startup and refuses every Digest.
      dynamic "env" {
        for_each = var.digest_resend_api_key == "" ? [] : [1]
        content {
          name = "DIGEST_RESEND_API_KEY"
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.digest_resend_api_key.secret_id
              version = "latest"
            }
          }
        }
      }

      # The Google OAuth credentials, one client per surface (google_oauth.tf).
      # Guarded the same way as the Resend key and for the same reason: no
      # version exists until a value is supplied, and "latest" against an empty
      # secret is a boot crash. Absent, the API simply has no Google client to
      # exchange a code against and the surfaces hide the button.
      #
      # There is deliberately NO GOOGLE_TOKEN_ENDPOINT here. It exists so tests
      # can point the exchange at a stub, and the API refuses to start in
      # production if it is set (PRD decision 10) — anyone who could set it could
      # make the platform trust an issuer they control, which is unrestricted
      # account takeover. Adding it to this file is the mistake that guard exists
      # to catch.
      dynamic "env" {
        for_each = var.google_staff_client_id == "" ? [] : [1]
        content {
          name = "GOOGLE_STAFF_CLIENT_ID"
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.google_staff_client_id.secret_id
              version = "latest"
            }
          }
        }
      }

      dynamic "env" {
        for_each = var.google_staff_client_secret == "" ? [] : [1]
        content {
          name = "GOOGLE_STAFF_CLIENT_SECRET"
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.google_staff_client_secret.secret_id
              version = "latest"
            }
          }
        }
      }

      dynamic "env" {
        for_each = var.google_storefront_client_id == "" ? [] : [1]
        content {
          name = "GOOGLE_STOREFRONT_CLIENT_ID"
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.google_storefront_client_id.secret_id
              version = "latest"
            }
          }
        }
      }

      dynamic "env" {
        for_each = var.google_storefront_client_secret == "" ? [] : [1]
        content {
          name = "GOOGLE_STOREFRONT_CLIENT_SECRET"
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.google_storefront_client_secret.secret_id
              version = "latest"
            }
          }
        }
      }

      # The PayPhone merchant credentials (payphone.tf), guarded the same way
      # and for the same reason: no secret version exists until a value is
      # supplied, and "latest" against an empty secret is a boot crash. The API
      # selects the real PayPhone Payment Provider only when BOTH are present
      # (ADR 0012); with either absent it runs the stub provider, which in
      # production is a dead checkout — never acceptable for real sales.
      #
      # There is deliberately NO PAYPHONE_API_BASE_URL here. It exists so the
      # integration suite can point Prepare/Confirm at a fake PayPhone server,
      # and the API refuses to start in production if it is set — anyone who
      # could point it elsewhere could "approve" payments no one ever made,
      # which is free tickets with nothing in the logs to tell them from real
      # sales. Adding it to this file is the mistake that guard exists to catch.
      dynamic "env" {
        for_each = var.payphone_api_token == "" ? [] : [1]
        content {
          name = "PAYPHONE_API_TOKEN"
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.payphone_api_token.secret_id
              version = "latest"
            }
          }
        }
      }

      dynamic "env" {
        for_each = var.payphone_store_id == "" ? [] : [1]
        content {
          name = "PAYPHONE_STORE_ID"
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.payphone_store_id.secret_id
              version = "latest"
            }
          }
        }
      }

      # /health is the one endpoint that answers without auth or a session, and
      # it is what tells Cloud Run the instance is ready to take traffic. Without
      # a startup probe Cloud Run only waits for the port to open, which happens
      # before the database pool is proven.
      startup_probe {
        http_get {
          path = "/health"
        }
        # The Go binary opens :8080 and answers /health within a second of boot,
        # so a 5s delay before the first probe was pure dead time on every cold
        # start. One second keeps a small cushion; failure_threshold * period_seconds
        # still gives the pool 30s to come up before the revision is failed.
        initial_delay_seconds = 1
        period_seconds        = 5
        timeout_seconds       = 3
        failure_threshold     = 6
      }
    }
  }

  # The secret versions and the grants that read them must exist before the
  # first revision boots; Cloud Run resolves secret env vars at instance start
  # and a missing grant is a revision that never becomes ready.
  depends_on = [
    google_secret_manager_secret_version.database_url,
    google_secret_manager_secret_iam_member.api_database_url,
    google_secret_manager_secret_iam_member.api_storage_access_key,
    google_secret_manager_secret_iam_member.api_storage_secret_key,
    google_secret_manager_secret_version.confirmation_link_secret,
    google_secret_manager_secret_iam_member.api_confirmation_link_secret,
  ]

  lifecycle {
    # Terraform creates this service; it does not own which build is running on
    # it. Deploys — by hand today (terraform/README.md), by GitHub Actions once
    # #49 lands — push a new image and roll a revision. Without this, the next
    # `terraform apply` would quietly roll production back to whatever tag was
    # last written here, and `terraform plan` would never be clean after a
    # deploy. Changing var.api_image_tag therefore does *not* deploy; see the
    # README.
    #
    # client/client_version are stamped by gcloud on every deploy and would drift
    # for the same reason.
    #
    # `scaling` here is the *service-level* block, not template[0].scaling which
    # this module does declare. The Cloud Run v2 API synthesises a service-level
    # scaling block (zeroed) on every service whether or not one was requested,
    # so leaving it undeclared makes every plan propose removing it — a diff that
    # never converges and would mask real drift. Confirmed against the first live
    # apply, not anticipated: plan reported `manual_instance_count = 0 -> null`
    # and `min_instance_count = 0 -> null` immediately after a successful apply.
    ignore_changes = [
      template[0].containers[0].image,
      client,
      client_version,
      scaling,
    ]
  }
}

# --- Migrate Job --------------------------------------------------------------

resource "google_cloud_run_v2_job" "migrate" {
  project  = var.project_id
  name     = "${var.environment}-ticket-pos-migrate"
  location = var.region

  deletion_protection = false

  labels = local.common_labels

  template {
    # Exactly one task, never in parallel. The whole reason migrations are a Job
    # is that migrate.Up takes no advisory lock; running two tasks of this Job at
    # once would recreate precisely the race the Job exists to avoid.
    task_count  = 1
    parallelism = 1

    template {
      service_account = google_service_account.migrate.email

      # A migration that fails should stop and be read, not be retried against a
      # database left wherever the first attempt stopped. Retrying is at best a
      # slower way to reach the same failure and at worst hides it.
      max_retries = var.migrate_max_retries
      timeout     = "${var.migrate_timeout_seconds}s"

      vpc_access {
        network_interfaces {
          network    = google_compute_network.main.name
          subnetwork = google_compute_subnetwork.cloud_run.name
        }
        egress = "PRIVATE_RANGES_ONLY"
      }

      containers {
        # The same image as the service, by the same tag. The only difference is
        # the entrypoint: /migrate instead of the Dockerfile's /server. Deploying
        # the Job and the service from one digest is what makes "run migrations,
        # then deploy" a coherent sequence rather than two independent builds.
        image   = local.api_image
        command = ["/migrate"]

        resources {
          limits = {
            cpu    = var.migrate_cpu
            memory = var.migrate_memory
          }
        }

        env {
          name = "DATABASE_URL"
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.database_url.secret_id
              version = "latest"
            }
          }
        }

        # cmd/migrate calls the same LoadConfig as the server, which requires
        # DATABASE_URL and nothing else. APP_ENV is set for log parity; the
        # migrate binary runs migrate.Up unconditionally and does not consult
        # RUN_MIGRATIONS.
        env {
          name  = "APP_ENV"
          value = "production"
        }

        env {
          name  = "LOG_LEVEL"
          value = var.api_log_level
        }
      }
    }
  }

  depends_on = [
    google_secret_manager_secret_version.database_url,
    google_secret_manager_secret_iam_member.migrate_database_url,
  ]

  lifecycle {
    ignore_changes = [
      template[0].template[0].containers[0].image,
      client,
      client_version,
    ]
  }
}
