# The Cloud Scheduler tick that drives the Owed Assignment Mail sweep (#671,
# parent #665, ADR 0076): the Assignment mails a Named Tickets checkout owes,
# one per Ticket the buyer named for somebody else, sent paced and retried after
# the commit that recorded the Ticket Sale.
#
# The shape is assignment_reminder.tf's, and through it answer_reminder.tf's:
# read those first. What is written here is only where this job differs.
#
# IT RUNS EVERY MINUTE, NOT DAILY. A reminder can wait a day; this cannot. The
# Holder was named at checkout a minute ago, and the buyer has just been told
# their friends will hear from the platform. The cadence is the latency of that
# promise, and a minute is the shortest Cloud Scheduler offers.
#
# THE QUEUE IS THE DATABASE'S, NOT THIS JOB'S. A row in owed_assignment_mails
# (migration 127) is a mail owed; the sweep deletes it when the mail is sent or
# when it is owed no longer (the Ticket reassigned, the Sale reversed, the Event
# started), and backs it off when the provider refuses it. So a missed tick, a
# paused job or an outage loses nothing: the mails wait, and the Event starting
# is what eventually drops them.
#
# THE PACING IS NOT HERE either. The gap between sends is the backend's, shared
# with the Reminder sweeps, and the run budget is shorter than this cadence so
# two scheduled runs never overlap. A schedule is not a rate limit.

# --- Identity -----------------------------------------------------------------

# Its own identity, on the terms every job here has one: the audit log must be
# able to say "the owed mail sweep ran" as its own sentence, and revoking it must
# never silently revoke the Reminders. The account_id is abbreviated because GCP
# caps a service account id at 30 characters.
resource "google_service_account" "owed_assignment_mail" {
  project      = var.project_id
  account_id   = "${var.environment}-ticket-pos-owed-mail"
  display_name = "Ticket POS ${var.environment} Owed Assignment Mail sweep"
  description  = "Identity Cloud Scheduler presents when driving the ${var.environment} Owed Assignment Mail sweep (ADR 0076)"
}

# run.invoker on the API service and nothing else.
resource "google_cloud_run_v2_service_iam_member" "owed_assignment_mail_invokes_api" {
  project  = var.project_id
  location = google_cloud_run_v2_service.api.location
  name     = google_cloud_run_v2_service.api.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.owed_assignment_mail.email}"
}

# --- Schedule -----------------------------------------------------------------

resource "google_cloud_scheduler_job" "owed_assignment_mail" {
  project  = var.project_id
  region   = var.region
  name     = "${var.environment}-ticket-pos-owed-assignment-mail"
  schedule = var.owed_assignment_mail_schedule

  # Ecuador, as every job here, though a per-minute cadence is
  # timezone-independent: the zone only makes an incident log readable.
  time_zone = "America/Guayaquil"

  description = "Drives the Owed Assignment Mail sweep: sends, paced and retried, the Assignment mails a Named Tickets checkout owes, and drops those owed no longer (ADR 0076)"

  # Paused, not absent, when disabled. Pausing is the incident lever if the
  # sweep is ever suspected of writing to the wrong people: the owed mails wait
  # in the database, and nothing is lost but time.
  #
  # The backend holds independently: the sweep sends nothing while
  # TICKET_ASSIGNMENT_ENABLED is off.
  paused = !var.owed_assignment_mail_enabled

  retry_config {
    # NO RETRIES. A failed run is retried by the next tick a minute later, from
    # the same state; a retry of a run whose ledger writes were failing could
    # only send the same mails again.
    retry_count = 0
  }

  http_target {
    http_method = "POST"

    # The pinned contract. Internal-scoped, no body, no parameters, no moment:
    # whom it writes to is the database's owed mails and the backend's clock.
    uri = "${google_cloud_run_v2_service.api.uri}/api/v1/internal/owed-assignment-mails/sweep"

    # The audience is the service root, never the sweep path.
    oidc_token {
      service_account_email = google_service_account.owed_assignment_mail.email
      audience              = google_cloud_run_v2_service.api.uri
    }
  }

  # Middle term of the deadline chain the Reminders keep: the run's own budget
  # first, this second, api_request_timeout_seconds last.
  attempt_deadline = "${var.owed_assignment_mail_attempt_deadline_seconds}s"

  depends_on = [google_cloud_run_v2_service_iam_member.owed_assignment_mail_invokes_api]
}
