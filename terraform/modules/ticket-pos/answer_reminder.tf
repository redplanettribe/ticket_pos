# The Cloud Scheduler tick that drives the Answer Reminder sweep (#317,
# ADR 0044): one mail to each buyer whose Ticket Sale still owes Answers, and
# whose rationing allows it.
#
# The shape is answer_purge.tf's, and the argument for a scheduler rather than an
# in-process ticker is the one ADR 0024 records — min_instances = 0 and
# cpu_idle = true mean there is frequently no process alive to tick.
#
# WHAT MAKES THIS DIFFERENT FROM EVERY OTHER JOB IN THIS MODULE is what a run
# produces. The reversal drain asks a provider a question; the purge deletes
# rows; the Digest jobs send mail to people who asked to be written to. This one
# sends mail to people who did not — buyers, about tickets they bought, which is
# transactional and legitimate and is still an unrequested message in a stranger's
# inbox. Everything below follows from that: the cadence is daily rather than
# per-minute, the endpoint cannot be told WHO to write to or WHEN it is, and the
# job ships paused.
#
# THE RATIONING IS NOT HERE, and must never be moved here. At most one mail per
# Ticket Sale per 7 days, at most two ever, silence once the Event has started:
# all of it lives in catalog.MayRemind and in the SQL beside it, where it holds
# however the endpoint is called. A schedule is not a rate limit — a cron that
# fired hourly would mail nobody twice, because the ledger and not the cadence is
# what decides.
#
# A missed run costs nothing and needs no catch-up: the sweep works from state
# rather than from events, so a day's outage means the same buyers are due
# tomorrow. There is no queue here and nothing to fall behind on.

# --- Identity -----------------------------------------------------------------

# Its own identity, on the terms every job here has one: the audit log has to be
# able to say "the reminder sweep ran" as a different sentence from "the purge
# ran", and revoking one must never silently revoke the other. For this account
# the log is the only record of a run that sent nothing, which is the ordinary
# case and the one worth being able to distinguish from a job that is not firing.
# The account_id is "answer-remind" and not "answer-reminder" because GCP caps a
# service account id at 30 characters and the full word overruns it by two. The
# Scheduler job below keeps the unabbreviated name -- its own limit is far
# higher -- so the pair reads as one thing everywhere except this line.
resource "google_service_account" "answer_reminder" {
  project      = var.project_id
  account_id   = "${var.environment}-ticket-pos-answer-remind"
  display_name = "Ticket POS ${var.environment} Answer Reminder sweep"
  description  = "Identity Cloud Scheduler presents when driving the ${var.environment} Answer Reminder sweep (ADR 0044)"
}

# run.invoker on the API service and nothing else. This account never deploys,
# never reads a secret, never reaches the database and never touches the mail
# provider: everything it may do is "make one authenticated HTTP call to one
# Cloud Run service", and who that call writes to is the backend's decision and
# not this grant's.
resource "google_cloud_run_v2_service_iam_member" "answer_reminder_invokes_api" {
  project  = var.project_id
  location = google_cloud_run_v2_service.api.location
  name     = google_cloud_run_v2_service.api.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.answer_reminder.email}"
}

# --- Schedule -----------------------------------------------------------------

resource "google_cloud_scheduler_job" "answer_reminder" {
  project  = var.project_id
  region   = var.region
  name     = "${var.environment}-ticket-pos-answer-reminder"
  schedule = var.answer_reminder_schedule

  # Ecuador, and here the timezone is part of the product rather than an
  # operator's convenience. This job puts messages in buyers' inboxes, so the
  # hour it fires is the hour they arrive; the purge's 03:20 would mean chasing
  # somebody about a t-shirt size in the middle of the night.
  time_zone = "America/Guayaquil"

  description = "Drives the Answer Reminder sweep: mails the buyers of active Ticket Sales that still owe Answers, rationed per Sale and silent once the Event has started (ADR 0044)"

  # Paused, not absent, when disabled — and this job SHIPS PAUSED, like every
  # scheduled job in this module before it.
  #
  # It ships paused for TWO reasons, and the second outlives the first. The
  # house rollout is to curl an endpoint by hand before a cron drives it. Beyond
  # that: the Ticket Sales this sweep would write to only exist once
  # TICKET_QUESTIONS_ENABLED is open, which waits on a Policy Version describing
  # the collection (ADR 0045) — and even then, the first time this platform
  # writes to buyers who did not ask to be written to should be a decision
  # somebody makes while watching, not a side effect of an apply.
  #
  # The backend refuses independently: catalog's side of the sweep reads the same
  # feature flag and returns no candidates while it is off. Two switches, and
  # both must be thrown deliberately.
  paused = !var.answer_reminder_enabled

  retry_config {
    # NO RETRIES, and for this job that is a stronger rule than for the ones
    # beside it. A retry of a run that failed midway would re-send to nobody it
    # had already mailed — the ledger is written before the response — but a
    # retry of a run whose ledger writes were failing would mail the same buyers
    # again, which is the one failure this feature must not have. The next tick
    # is a day away and works from the same state; there is nothing a retry
    # recovers that waiting does not.
    retry_count = 0
  }

  http_target {
    http_method = "POST"

    # The pinned contract. Internal-scoped: reachable by neither a Customer
    # Session nor a staff token. Note what is NOT in this URI and may never be
    # added — an Event, an Organization, a Ticket Sale, and above all a moment.
    # The window that keeps this mail to one a week is arithmetic on the
    # backend's own clock, precisely so that possession of this account's token
    # is not possession of a mail-everybody-now button; see
    # sales/handler.SweepAnswerReminders.
    uri = "${google_cloud_run_v2_service.api.uri}/api/v1/internal/answer-reminders/sweep"

    # Cloud Scheduler puts the ID token in `Authorization` and cannot be told to
    # put it anywhere else, which matters because ADR 0008 keeps that header for
    # the end-user session token. Why that is safe on this route is written down
    # beside the route itself, in backend/internal/server/routes.go.
    #
    # The audience is the service's root URL, not the sweep path: Cloud Run
    # validates the token's `aud` against the service URL, and a path here
    # produces a 401 that looks like a broken IAM grant.
    oidc_token {
      service_account_email = google_service_account.answer_reminder.email
      audience              = google_cloud_run_v2_service.api.uri
    }
  }

  # The middle term of the deadline chain the backend writes down once, beside
  # answerReminderBudget in backend/internal/sales/service/answerreminder.go:
  # the run's own budget expires first, this second, api_request_timeout_seconds
  # last. Only the innermost term stops a run politely — the other two abandon
  # the request where it stands, and here that means losing the ledger write for
  # a message the provider has already accepted, which costs a buyer a duplicate.
  # A backend test reads this default and fails if the chain stops holding.
  attempt_deadline = "${var.answer_reminder_attempt_deadline_seconds}s"

  depends_on = [google_cloud_run_v2_service_iam_member.answer_reminder_invokes_api]
}
