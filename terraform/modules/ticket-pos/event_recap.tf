# The Cloud Scheduler tick that drives the Event Recap sweep (spec #884, #889):
# one mail per Event, the morning after it is over, to the Event Owners of the
# Event, or to the Org Admins of its Organization when it has none, stating how
# it went.
#
# The shape is assignment_reminder.tf's, and through it answer_reminder.tf's:
# read those first. The argument for a scheduler rather than an in-process
# ticker (ADR 0024), the no-retry rule and the un-aimable endpoint are the same
# argument made once. What is written here is only where this job differs.
#
# IT WRITES TO MEMBERS, NOT TO BUYERS. Every reader is an Event Owner or an
# Org Admin answerable for the Event, and the mail names no Customer. That is why it has no dark-ship
# flag in the backend and why production switches it on in terraform.tfvars
# with the deploy, where the Reminders waited for somebody to watch a first
# run.
#
# IT HAS NO CATCH-UP. Only an Event over since the go-live instant
# (platform.EventRecapWentLiveAt in the backend) is ever recapped, so the first
# run is an ordinary morning's and not a backlog.
#
# THE HOUR IS THE PRODUCT'S. The sweep decides for the latest 09:00 the
# platform clock has reached (catalog.EventRecapRunAt), not for the moment it
# is invoked: a run made late or by hand sends what the morning's would have,
# and a missed morning is made good by the next run, which sends late rather
# than dropping an Event. The schedule below is that same 09:00, so the mail
# arrives when the sweep says it is due. A backend test reads this file and
# fails if the two part.
#
# ONCE PER EVENT IS NOT HERE. It is the event_recaps table (migration 146): a
# row is what makes an Event never due again, however often this job fires. A
# schedule is not a rate limit.

# --- Identity -----------------------------------------------------------------

# Its own identity, on the terms every job here has one: the audit log must be
# able to say "the Event Recap sweep ran" as its own sentence, and revoking it
# must never silently revoke the Reminders. The account_id is the job's own
# name. GCP caps a service account id at 30 characters, and at 23 after the
# environment this one is shorter than the ids the Reminders abbreviate to.
resource "google_service_account" "event_recap" {
  project      = var.project_id
  account_id   = "${var.environment}-ticket-pos-event-recap"
  display_name = "Ticket POS ${var.environment} Event Recap sweep"
  description  = "Identity Cloud Scheduler presents when driving the ${var.environment} Event Recap sweep (spec #884)"
}

# run.invoker on the API service and nothing else: one authenticated HTTP call
# to one Cloud Run service. Whom that call writes to is the backend's decision.
resource "google_cloud_run_v2_service_iam_member" "event_recap_invokes_api" {
  project  = var.project_id
  location = google_cloud_run_v2_service.api.location
  name     = google_cloud_run_v2_service.api.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.event_recap.email}"
}

# --- Schedule -----------------------------------------------------------------

resource "google_cloud_scheduler_job" "event_recap" {
  project  = var.project_id
  region   = var.region
  name     = "${var.environment}-ticket-pos-event-recap"
  schedule = var.event_recap_schedule

  # The platform clock, and here the zone is part of the product: 09:00 in
  # America/Guayaquil is the hour the sweep decides for and the hour the
  # glossary says an Event Recap goes.
  time_zone = "America/Guayaquil"

  description = "Drives the Event Recap sweep: mails the Event Owners of every published Event over at least six hours, or its Organization's Org Admins when it has none, how the Event went, once per Event (spec #884)"

  # Paused, not absent, when disabled. The module defaults to paused, as every
  # job here does, and production sets event_recap_enabled true in
  # terraform.tfvars. Pausing is the incident lever if the sweep is ever
  # suspected of writing to the wrong people: nothing is lost but time, since
  # an Event stays due until it is recapped and the runs after a resume send
  # what the paused mornings would have, a batch a run.
  paused = !var.event_recap_enabled

  retry_config {
    # NO RETRIES, for the reason answer_reminder.tf gives: a retry of a run
    # whose record writes were failing would mail the same readers again, and
    # the next morning's run works from the same state.
    retry_count = 0
  }

  http_target {
    http_method = "POST"

    # The pinned contract. Internal-scoped, no body, no parameters, no moment:
    # which Events are due is arithmetic on the backend's own clock.
    uri = "${google_cloud_run_v2_service.api.uri}/api/v1/internal/event-recaps/sweep"

    # Same token placement and audience rule as the Answer Reminder: the
    # audience is the service root, never the sweep path.
    oidc_token {
      service_account_email = google_service_account.event_recap.email
      audience              = google_cloud_run_v2_service.api.uri
    }
  }

  # Middle term of the deadline chain the Reminders keep: the run's own budget
  # first, this second, api_request_timeout_seconds last. The sweep checks its
  # budget once per Event and finishes the readers of an Event it has begun,
  # so the slack between the budget and this deadline is what that last Event
  # spends.
  attempt_deadline = "${var.event_recap_attempt_deadline_seconds}s"

  depends_on = [google_cloud_run_v2_service_iam_member.event_recap_invokes_api]
}

# --- Sentry cron monitor (ADR 0079, #778) ---------------------------------------
#
# The job above checks in as "ticket-pos-event-recap". Its monitor exists only
# while the job is unpaused and the API reports, and takes its name, schedule
# and time zone from the job itself; sentry.tf says the rest.
resource "sentry_cron_monitor" "event_recap" {
  count = local.sentry_cron_monitors_enabled && !google_cloud_scheduler_job.event_recap.paused ? 1 : 0

  organization = var.sentry_organization
  project      = sentry_project.deployable["api"].slug
  name         = trimprefix(google_cloud_scheduler_job.event_recap.name, "${var.environment}-")
  description  = google_cloud_scheduler_job.event_recap.description
  owner        = local.sentry_monitor_owner

  schedule = {
    crontab = google_cloud_scheduler_job.event_recap.schedule
  }
  timezone = google_cloud_scheduler_job.event_recap.time_zone

  # Once a day.
  checkin_margin_minutes  = local.sentry_cron_tolerance.daily_or_weekly.checkin_margin_minutes
  failure_issue_threshold = local.sentry_cron_tolerance.daily_or_weekly.failure_issue_threshold
  recovery_threshold      = local.sentry_cron_tolerance.daily_or_weekly.recovery_threshold
  max_runtime_minutes     = ceil(var.event_recap_attempt_deadline_seconds / 60) + 1
}
