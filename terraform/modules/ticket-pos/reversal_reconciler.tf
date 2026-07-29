# The Cloud Scheduler tick that drives the Reversal Reconciler.
#
# This is the first scheduled execution in the deployment: nothing in the
# backend has ever run outside a request. It is here rather than in the
# application because of this deployment specifically — min_instances = 0 and
# cpu_idle = true mean there is frequently no process alive to tick and no CPU
# outside a request, which is the argument ADR 0024 records for rejecting an
# in-process ticker, along with what a tick a minute costs in return.
#
# The endpoint is a drain, not a schedule-driven decision: what to pursue and
# when to pursue it next lives in sale_reversals.next_attempt_at, so the tick
# only supplies the heartbeat. Requiring cloudscheduler.googleapis.com to be
# enabled on the project is the one prerequisite this file adds.

# --- Identity -----------------------------------------------------------------

# Its own identity rather than reusing the API's or a frontend's. The frontends
# hold run.invoker so they can serve browsers; this account exists so the audit
# log distinguishes "the cron drained reversals" from "a Customer loaded the
# Customer Area", and so revoking one never silently revokes the other.
resource "google_service_account" "reversal_reconciler" {
  project      = var.project_id
  account_id   = "${var.environment}-ticket-pos-reversals"
  display_name = "Ticket POS ${var.environment} Reversal Reconciler"
  description  = "Identity Cloud Scheduler presents when driving the ${var.environment} Reversal Reconciler drain"
}

# run.invoker on the API service and nothing else — not a project-level binding,
# not run.developer. This account never deploys, never reads a secret and never
# reaches the database; everything it is allowed to do is "make one authenticated
# HTTP call to one Cloud Run service", and the drain endpoint decides the rest.
resource "google_cloud_run_v2_service_iam_member" "reversal_reconciler_invokes_api" {
  project  = var.project_id
  location = google_cloud_run_v2_service.api.location
  name     = google_cloud_run_v2_service.api.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.reversal_reconciler.email}"
}

# --- Schedule -----------------------------------------------------------------

resource "google_cloud_scheduler_job" "reversal_reconciler" {
  project  = var.project_id
  region   = var.region
  name     = "${var.environment}-ticket-pos-reversal-reconciler"
  schedule = var.reversal_reconciler_schedule

  # Ecuador, because a human reading "this last ran at 19:58" during an incident
  # is reading it against the Reversal Window's cutoff, which is stated in
  # Ecuador time. The cadence itself is timezone-independent.
  time_zone = "America/Guayaquil"

  description = "Drives the Reversal Reconciler: pursues Reversal Requests the Payment Provider never answered (ADR 0024)"

  # Paused, not absent, when disabled. The rollout in ADR 0024 is to deploy the
  # endpoint and curl it by hand for a day before a cron drives it, and pausing
  # is also the first move in an incident — so the job, its identity and its
  # grant all have to exist and be verifiable while nothing is firing. Deleting
  # the resource instead would make "turn it back on" an apply that recreates
  # three things under pressure.
  paused = !var.reversal_reconciler_enabled

  retry_config {
    # No retries. A failed tick is not worth repeating: the next tick is a minute
    # away and is a strictly better attempt, and per-request backoff already
    # lives in sale_reversals.next_attempt_at where an operator can read it in
    # SQL. Scheduler-level retries would race that schedule with a second,
    # invisible one.
    retry_count = 0
  }

  http_target {
    http_method = "POST"

    # The pinned contract. Internal-scoped: reachable by neither a Customer
    # Session nor a staff token. What the endpoint promises its callers — this
    # job and the operator curling the same URL — is on the handler
    # (sales/handler.DrainReversalRequests).
    uri = "${google_cloud_run_v2_service.api.uri}/api/v1/internal/reversals/drain"

    # Cloud Scheduler puts the ID token in `Authorization` and cannot be told to
    # put it anywhere else, which matters because ADR 0008 keeps that header for
    # the end-user session token. Why that is safe on this route in particular is
    # written down beside the route itself, in backend/internal/server/routes.go
    # (registerInternalRoutes).
    #
    # The audience is the service's root URL, not the drain path: Cloud Run
    # validates the token's `aud` against the service URL, and a path here
    # produces a 401 that looks like a broken IAM grant.
    oidc_token {
      service_account_email = google_service_account.reversal_reconciler.email
      audience              = google_cloud_run_v2_service.api.uri
    }
  }

  # This must never be what stops a run: the backend budgets itself, finishes and
  # returns a summary, and Scheduler is here only to catch a run that never
  # answers at all. The whole chain — what each term is, why this one sits in the
  # middle, and what breaks when one moves alone — is written down once in
  # backend/internal/sales/service/reconciler.go beside reversalDrainBudget, which
  # is also where the number this must stay above lives.
  #
  # It exceeds the one-minute interval, and that is fine rather than something to
  # prevent. Cloud Scheduler promises at-least-once delivery, not that two runs
  # never overlap, so overlap has to be harmless — and it is: every actor takes
  # pg_try_advisory_lock(18, hashtext(saleID)) before probing the Payment
  # Provider, so a second drain either finds nothing due or hands the contended
  # row straight back and the next tick picks it up. Correctness rests on that
  # lock, never on the schedule.
  attempt_deadline = "${var.reversal_reconciler_attempt_deadline_seconds}s"

  depends_on = [google_cloud_run_v2_service_iam_member.reversal_reconciler_invokes_api]
}
