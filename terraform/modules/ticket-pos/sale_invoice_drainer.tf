# The Cloud Scheduler tick that drives the Sale Invoice Drainer (#474, parent
# #471, ADR 0060): the owed Sale Invoices a House Organization's paid
# checkouts left behind are signed, submitted to the SRI and polled to
# authorized, with nobody waiting.
#
# The shape is reversal_reconciler.tf's, read that file first: the argument
# for a scheduler rather than an in-process ticker (ADR 0024), the no-retry
# rule, the un-aimable endpoint and the deadline chain are the same argument
# made once. What is written here is only where this job differs.
#
# IT IS NOT THE NORMAL PATH. A paid House checkout kicks the invoicing module
# the moment its transaction commits, so in the usual case a buyer's factura
# is authorized seconds after they paid and this tick finds nothing due. What
# the tick is for is everything the kick could not finish: an SRI that was
# down or still "en procesamiento", an instance that died mid-round, an
# Issuer that had no certificate when the sale was made. Every one of those
# is scheduled on the backend's own ladder (invoicing_invoices.next_attempt_at:
# 1 min, 5 min, 15 min, then hourly), and the tick only supplies the
# heartbeat — every five minutes, because the first rung is a minute and the
# rest are longer, and a document is never more than one tick later than its
# rung says.
#
# THE SEQUENCE IS THE THING TO PROTECT. Every signing consumes a secuencial
# the SRI expects to see without holes, and the backend consumes one only
# inside the transaction that signs; nothing about this schedule can consume
# one, and a tick that never fires costs a buyer their factura, not the
# platform a number.

# --- Identity -----------------------------------------------------------------

# Its own identity, on the terms every job here has one (reversal_reconciler.tf
# says why): the audit log must be able to say "the invoice drain ran" as a
# different sentence from "the reversal drain ran", and revoking one must
# never silently revoke the other.
resource "google_service_account" "sale_invoice_drainer" {
  project      = var.project_id
  account_id   = "${var.environment}-ticket-pos-invoices"
  display_name = "Ticket POS ${var.environment} Sale Invoice Drainer"
  description  = "Identity Cloud Scheduler presents when driving the ${var.environment} Sale Invoice Drainer (ADR 0060)"
}

# run.invoker on the API service and nothing else, as reversal_reconciler.tf
# explains: one authenticated HTTP call to one Cloud Run service.
resource "google_cloud_run_v2_service_iam_member" "sale_invoice_drainer_invokes_api" {
  project  = var.project_id
  location = google_cloud_run_v2_service.api.location
  name     = google_cloud_run_v2_service.api.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.sale_invoice_drainer.email}"
}

# --- Schedule -----------------------------------------------------------------

resource "google_cloud_scheduler_job" "sale_invoice_drainer" {
  project  = var.project_id
  region   = var.region
  name     = "${var.environment}-ticket-pos-sale-invoice-drainer"
  schedule = var.sale_invoice_drainer_schedule

  # Ecuador, because the emission date on every factura this signs is the
  # signing date in America/Guayaquil, and a human reading "this last ran at
  # 23:58" is reading it against that date.
  time_zone = "America/Guayaquil"

  description = "Drives the Sale Invoice Drainer: signs, submits and polls the Sale Invoices paid House checkouts owed, on the backend's ladder (ADR 0060)"

  # Paused, not absent, when disabled — and this job SHIPS PAUSED, as every
  # sweep here did. It is enabled once an Issuer in `production` exists and
  # the endpoint has been curled by hand against real House sales; until
  # then the kick after each checkout is the only thing working the queue,
  # which is enough to see the state machine on the invoicing list. Pausing
  # is also the first move in an incident, and the job, its identity and its
  # grant all have to exist while nothing is firing.
  paused = !var.sale_invoice_drainer_enabled

  retry_config {
    # NO RETRIES, for reversal_reconciler.tf's reason: the next tick is five
    # minutes away and is a strictly better attempt, and per-document
    # backoff already lives in invoicing_invoices.next_attempt_at where an
    # operator can read it in SQL.
    retry_count = 0
  }

  http_target {
    http_method = "POST"

    # The pinned contract. Internal-scoped, no body, no parameters: WHICH
    # documents are worked is a property of the queue, never of the caller,
    # so possession of this account's token is not a sign-anything button.
    # What the endpoint promises is on the handler
    # (invoicing/handler.DrainSaleInvoices).
    uri = "${google_cloud_run_v2_service.api.uri}/api/v1/internal/sale-invoices/drain"

    # Same token placement and audience rule as the reconciler: the audience
    # is the service root, never the drain path.
    oidc_token {
      service_account_email = google_service_account.sale_invoice_drainer.email
      audience              = google_cloud_run_v2_service.api.uri
    }
  }

  # Middle term of the same deadline chain the reconciler keeps: the run's
  # own budget first, this second, api_request_timeout_seconds last. The
  # chain and what breaks when one term moves alone is written down once in
  # backend/internal/invoicing/service/drainer.go beside
  # saleInvoiceDrainBudget, and a backend test reads this default against it.
  #
  # Overlap is harmless here for the same reason it is there: every document
  # is claimed under a lease (FOR UPDATE SKIP LOCKED moving next_attempt_at
  # forward in the same statement), so a second run either finds the next
  # document or nothing, and no document is ever signed twice.
  attempt_deadline = "${var.sale_invoice_drainer_attempt_deadline_seconds}s"

  depends_on = [google_cloud_run_v2_service_iam_member.sale_invoice_drainer_invokes_api]
}
