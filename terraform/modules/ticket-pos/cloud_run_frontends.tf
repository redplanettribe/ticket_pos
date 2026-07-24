# Staff and Storefront on Cloud Run, and the domain mappings that put them on
# real hostnames.
#
# These two are the only things in this deployment the public internet talks to
# directly. They are *unauthenticated* at the Cloud Run layer — a browser has no
# Google identity to present — and each is a Next server that calls the Go API
# server-side, attaching an OIDC ID token minted from its own service account
# (ADR 0008). That is why the API can refuse everyone else.
#
# Neither gets VPC egress: they never touch the database, and every call they
# make (the API, over its public run.app URL) leaves over Google's own path.

# --- Identity -----------------------------------------------------------------

# One identity per frontend rather than one shared "frontend" account. The two
# surfaces have different audiences — Storefront serves Customers, Staff serves
# Members holding a Session — and a shared identity would make a compromise of
# the public storefront indistinguishable from a compromise of the staff console
# in the API's eyes.
resource "google_service_account" "staff" {
  project      = var.project_id
  account_id   = "${var.environment}-ticket-pos-staff"
  display_name = "Ticket POS ${var.environment} Staff"
  description  = "Runtime identity of the ${var.environment} Staff Cloud Run service"
}

resource "google_service_account" "storefront" {
  project      = var.project_id
  account_id   = "${var.environment}-ticket-pos-storefront"
  display_name = "Ticket POS ${var.environment} Storefront"
  description  = "Runtime identity of the ${var.environment} Storefront Cloud Run service"
}

# The grants that make ADR 0008 work. Without these two bindings the frontends
# present a valid ID token and Cloud Run still returns 403 — the token proves
# who they are, this says who is allowed in.
#
# These are the ONLY invoker bindings on the API. There is deliberately no
# allUsers member here; adding one silently converts the API into a public
# service and every other control in this module into decoration.
resource "google_cloud_run_v2_service_iam_member" "staff_invokes_api" {
  project  = var.project_id
  location = google_cloud_run_v2_service.api.location
  name     = google_cloud_run_v2_service.api.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.staff.email}"
}

resource "google_cloud_run_v2_service_iam_member" "storefront_invokes_api" {
  project  = var.project_id
  location = google_cloud_run_v2_service.api.location
  name     = google_cloud_run_v2_service.api.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.storefront.email}"
}

# --- Services -------------------------------------------------------------------

resource "google_cloud_run_v2_service" "storefront" {
  project  = var.project_id
  name     = "${var.environment}-ticket-pos-storefront"
  location = var.region

  ingress             = "INGRESS_TRAFFIC_ALL"
  deletion_protection = false
  labels              = local.common_labels

  template {
    service_account = google_service_account.storefront.email

    scaling {
      min_instance_count = var.storefront_min_instances
      max_instance_count = var.storefront_max_instances
    }

    max_instance_request_concurrency = var.frontend_concurrency

    containers {
      image = local.storefront_image

      ports {
        container_port = 3000
      }

      resources {
        limits = {
          cpu    = var.frontend_cpu
          memory = var.frontend_memory
        }
        cpu_idle          = true
        startup_cpu_boost = true
      }

      # Server-side only. The browser never calls the Go API directly — every
      # client fetch in this app targets a relative /api/... route on this
      # server — so this value is never inlined into the bundle and can be a
      # runtime variable. NEXT_PUBLIC_* would have to be a build argument.
      env {
        name  = "API_URL"
        value = google_cloud_run_v2_service.api.uri
      }

      # The Storefront's own public origin, used to set Next's metadataBase so
      # canonical and og:url resolve to absolute URLs on shared event links.
      # Runtime-only (never NEXT_PUBLIC), so the same image serves every env.
      # Empty until a custom domain is mapped, where Next falls back to relative
      # URLs — og:image is already absolute, so link previews still work.
      env {
        name  = "STOREFRONT_BASE_URL"
        value = var.storefront_domain != null ? "https://${var.storefront_domain}" : ""
      }

      env {
        name  = "NODE_ENV"
        value = "production"
      }
    }
  }

  lifecycle {
    # Same reasoning as the API service: Terraform creates the service, deploys
    # roll revisions. See cloud_run.tf.
    ignore_changes = [
      template[0].containers[0].image,
      client,
      client_version,
      scaling,
    ]
  }
}

resource "google_cloud_run_v2_service" "staff" {
  project  = var.project_id
  name     = "${var.environment}-ticket-pos-staff"
  location = var.region

  ingress             = "INGRESS_TRAFFIC_ALL"
  deletion_protection = false
  labels              = local.common_labels

  template {
    service_account = google_service_account.staff.email

    scaling {
      min_instance_count = var.staff_min_instances
      max_instance_count = var.staff_max_instances
    }

    max_instance_request_concurrency = var.frontend_concurrency

    containers {
      image = local.staff_image

      ports {
        container_port = 3001
      }

      resources {
        limits = {
          cpu    = var.frontend_cpu
          memory = var.frontend_memory
        }
        cpu_idle          = true
        startup_cpu_boost = true
      }

      env {
        name  = "API_URL"
        value = google_cloud_run_v2_service.api.uri
      }

      # Staff sets the Session cookie `secure` when NODE_ENV is production
      # (apps/staff/lib/session.ts). Behind a managed certificate that is what
      # is wanted; it also means Staff is unusable over plain HTTP on a
      # non-localhost host, which is intended rather than incidental.
      env {
        name  = "NODE_ENV"
        value = "production"
      }
    }
  }

  lifecycle {
    ignore_changes = [
      template[0].containers[0].image,
      client,
      client_version,
      scaling,
    ]
  }
}

# Browsers carry no Google identity, so these two are open at the Cloud Run
# layer. Authorization for Staff is the application's own Session and Member
# role checks, which is where it has always lived.
resource "google_cloud_run_v2_service_iam_member" "storefront_public" {
  project  = var.project_id
  location = google_cloud_run_v2_service.storefront.location
  name     = google_cloud_run_v2_service.storefront.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

resource "google_cloud_run_v2_service_iam_member" "staff_public" {
  project  = var.project_id
  location = google_cloud_run_v2_service.staff.location
  name     = google_cloud_run_v2_service.staff.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

# --- Domain mappings ------------------------------------------------------------
#
# Managed certificates, no load balancer. us-east1 supports domain mappings,
# which is what lets this design skip an external HTTPS load balancer and its
# ~$18/month (ADR 0007, decision 7).
#
# These resources are created immediately but stay PENDING until the DNS records
# they emit exist at the registrar — DNS for multiticketing.com is served by
# Namecheap, deliberately: the apex is unused and there is no email to migrate,
# so two static CNAMEs did not justify delegating the zone. `terraform output
# dns_records` prints exactly what to create.
#
# google_cloud_run_domain_mapping is a v1 resource; there is no v2 equivalent.
# It maps onto the v2 service by name, which is why it takes a plain string.
resource "google_cloud_run_domain_mapping" "storefront" {
  count = var.storefront_domain == null ? 0 : 1

  project  = var.project_id
  location = var.region
  name     = var.storefront_domain

  metadata {
    namespace = var.project_id
    labels    = local.common_labels
  }

  spec {
    route_name = google_cloud_run_v2_service.storefront.name
  }
}

resource "google_cloud_run_domain_mapping" "staff" {
  count = var.staff_domain == null ? 0 : 1

  project  = var.project_id
  location = var.region
  name     = var.staff_domain

  metadata {
    namespace = var.project_id
    labels    = local.common_labels
  }

  spec {
    route_name = google_cloud_run_v2_service.staff.name
  }
}
