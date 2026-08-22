# The Cloud Scheduler tick that drives the Abandoned Answer Purge (#316,
# ADR 0044): the Answers a buyer typed into a checkout that never became a sale,
# deleted 30 days on.
#
# The shape is reversal_reconciler.tf's, and the argument for a scheduler rather
# than an in-process ticker is the same one ADR 0024 records — min_instances = 0
# and cpu_idle = true mean there is frequently no process alive to tick. What is
# NOT the same is the cadence and what a missed run costs, and both differences
# are the reason this is its own file rather than a fourth block in that one.
#
# THIS IS THE ONLY SCHEDULED JOB IN THE DEPLOYMENT THAT DELETES ANYTHING. The
# reversal drain asks a provider a question; the Digest jobs send mail. This one
# removes rows, and they are the rows the platform treats as potentially health
# data. That asymmetry is why the tick is daily rather than per-minute (a
# retention promise measured in days gains nothing from being measured in
# minutes, and a bug in a job that runs 1,440 times a day empties the table
# before anybody reads the first alert) and why the endpoint it calls cannot be
# told WHICH Answers to delete.
#
# A missed run costs nothing and needs no catch-up: the endpoint deletes by
# predicate, so a day's outage means the next tick deletes two days' worth. There
# is no queue here, no cursor and nothing to fall behind on.

# --- Identity -----------------------------------------------------------------

# Its own identity, on the terms every job here has one: the audit log has to be
# able to say "the purge ran" as a different sentence from "the cron drained
# reversals", and revoking one must never silently revoke the other. It matters
# more for this account than the others, because this is the one whose calls
# destroy data — an unexplained purge in the log is an incident, and it can only
# be traced to a caller if the caller is distinguishable.
resource "google_service_account" "answer_purge" {
  project      = var.project_id
  account_id   = "${var.environment}-ticket-pos-answer-purge"
  display_name = "Ticket POS ${var.environment} Abandoned Answer Purge"
  description  = "Identity Cloud Scheduler presents when driving the ${var.environment} Abandoned Answer Purge (ADR 0044)"
}

# run.invoker on the API service and nothing else. This account never deploys,
# never reads a secret and never reaches the database: everything it may do is
# "make one authenticated HTTP call to one Cloud Run service", and what that call
# is allowed to delete is the endpoint's decision and not this grant's.
resource "google_cloud_run_v2_service_iam_member" "answer_purge_invokes_api" {
  project  = var.project_id
  location = google_cloud_run_v2_service.api.location
  name     = google_cloud_run_v2_service.api.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.answer_purge.email}"
}

# --- Schedule -----------------------------------------------------------------

resource "google_cloud_scheduler_job" "answer_purge" {
  project  = var.project_id
  region   = var.region
  name     = "${var.environment}-ticket-pos-answer-purge"
  schedule = var.answer_purge_schedule

  # Ecuador, so "this last ran at 03:20" reads against the working day of the
  # people who would be looking. The cadence itself is timezone-independent: the
  # 30-day window is arithmetic on a timestamp and has no calendar in it.
  time_zone = "America/Guayaquil"

  description = "Drives the Abandoned Answer Purge: deletes the Answers held on Payments that never reached approved, 30 days on (ADR 0044)"

  # Paused, not absent, when disabled — and this job SHIPS PAUSED, like every
  # scheduled job in this module before it.
  #
  # It ships paused for a reason of its own, beyond the house rollout of curling
  # an endpoint by hand before a cron drives it. The feature that fills these
  # rows is behind TICKET_QUESTIONS_ENABLED, which is off until a Policy Version
  # describing the collection publishes (ADR 0045), so until that flag flips
  # there is nothing here to purge and a firing job would be a daily DELETE
  # against an empty table. The purge is turned on when there is something to
  # purge — which is a different decision, made later, by a human who can see
  # that the answers_held figure has started moving.
  paused = !var.answer_purge_enabled

  retry_config {
    # No retries, and here the reason is stronger than the reversal drain's. A
    # failed tick is not worth repeating because the next one is a day away and
    # deletes exactly what this one would have: the predicate is age-based, so
    # nothing is lost by waiting and nothing accumulates that a catch-up would
    # have to work through. Retrying a DELETE that may have partially succeeded
    # buys nothing and makes the log harder to read during the one kind of
    # incident this job can have.
    retry_count = 0
  }

  http_target {
    http_method = "POST"

    # The pinned contract. Internal-scoped: reachable by neither a Customer
    # Session nor a staff token. Note what is NOT in this URI and may never be
    # added — a cutoff, a Payment, an Organization. The window comes from the
    # backend's clock precisely so that possession of this account's token is not
    # possession of a delete-every-Answer-on-the-platform button; see
    # sales/handler.PurgeAbandonedAnswers.
    uri = "${google_cloud_run_v2_service.api.uri}/api/v1/internal/checkout-answers/purge"

    # Cloud Scheduler puts the ID token in `Authorization` and cannot be told to
    # put it anywhere else, which matters because ADR 0008 keeps that header for
    # the end-user session token. Why that is safe on this route is written down
    # beside the route itself, in backend/internal/server/routes.go.
    #
    # The audience is the service's root URL, not the purge path: Cloud Run
    # validates the token's `aud` against the service URL, and a path here
    # produces a 401 that looks like a broken IAM grant.
    oidc_token {
      service_account_email = google_service_account.answer_purge.email
      audience              = google_cloud_run_v2_service.api.uri
    }
  }

  # Generous, because unlike the reversal drain this run has no budget of its own
  # to expire first. It is a single DELETE against a predicate — there is no loop
  # to bound and no per-item work to abandon halfway — so the only thing this
  # deadline can catch is a statement wedged behind a lock, and the honest
  # response to that is to give up and let tomorrow's tick try again.
  #
  # It stays below api_request_timeout_seconds for the reason the reversal chain
  # gives: a deadline above the request timeout means Scheduler is waiting on a
  # request the platform has already abandoned, and the run is recorded as a
  # timeout rather than as the failure it was.
  attempt_deadline = "${var.answer_purge_attempt_deadline_seconds}s"

  depends_on = [google_cloud_run_v2_service_iam_member.answer_purge_invokes_api]
}
