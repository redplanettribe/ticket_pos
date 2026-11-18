# The Cloud Scheduler tick that drives the daily close (#815, spec #808,
# ADR 0080): each closed platform day's activity, frozen into the totals
# Platform Trends reads.
#
# The shape is answer_purge.tf's, and the argument for a scheduler rather than
# an in-process ticker is the one ADR 0024 records: min_instances = 0 and
# cpu_idle = true mean there is frequently no process alive to tick.
#
# This job sends nothing. It writes one row per closed day and never rewrites
# one. The same run deletes the activity markers older than thirteen months,
# an Organization's and a Customer's alike (#816, #817); the frozen totals
# outlive them and no row of totals is ever deleted. A missed run leaves no
# hole: the endpoint freezes every closed day that has no row yet, oldest
# first, so the next run makes a missed night good. What a missed run does cost is a day: until it
# is made good, Platform Trends states the unfrozen day as absent and its
# 7-day and 30-day figures as unknown, which is why the monitor below opens
# an issue on the first bad run.

# --- Identity -----------------------------------------------------------------

# Its own identity, on the terms every job here has one: the audit log can
# say "the daily close ran" apart from every other job, and revoking one
# never revokes another.
resource "google_service_account" "daily_close" {
  project      = var.project_id
  account_id   = "${var.environment}-ticket-pos-daily-close"
  display_name = "Ticket POS ${var.environment} daily close"
  description  = "Identity Cloud Scheduler presents when driving the ${var.environment} daily close of Platform Trends (ADR 0080)"
}

# run.invoker on the API service and nothing else. This account never
# deploys, never reads a secret and never reaches the database.
resource "google_cloud_run_v2_service_iam_member" "daily_close_invokes_api" {
  project  = var.project_id
  location = google_cloud_run_v2_service.api.location
  name     = google_cloud_run_v2_service.api.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.daily_close.email}"
}

# --- Schedule -----------------------------------------------------------------

resource "google_cloud_scheduler_job" "daily_close" {
  project  = var.project_id
  region   = var.region
  name     = "${var.environment}-ticket-pos-daily-close"
  schedule = var.daily_close_schedule

  # Unlike the purges, this zone is not for the reader's convenience: the day
  # the job closes is the platform's day, which is this clock's. The backend
  # decides which days are closed from its own clock on the same zone, so a
  # run at any hour is correct; this schedule only decides how soon after
  # midnight yesterday is frozen.
  time_zone = "America/Guayaquil"

  description = "Drives the daily close: freezes each closed platform day's activity into the totals Platform Trends reads (ADR 0080)"

  # Paused, not absent, when disabled, and this job SHIPS PAUSED like every
  # scheduled job in this module: the endpoint is run by hand first, and the
  # cron is turned on by a reviewed change once its first run, which freezes
  # every day back to the first recorded Ticket Sale, has been read.
  paused = !var.daily_close_enabled

  retry_config {
    # No retries, as for every job here. A failed run is made good by the
    # next one, which freezes the day this one missed, and an operator who
    # wants it sooner can call the endpoint by hand: it is idempotent.
    retry_count = 0
  }

  http_target {
    http_method = "POST"

    # The pinned contract. Internal-scoped: reachable by neither a Customer
    # Session nor a staff token. Nothing in this URI names a day and nothing
    # may: the days come from the backend's clock, so that possession of this
    # account's token is not the power to freeze a day still running. See
    # platformtrends/handler.CloseDays.
    uri = "${google_cloud_run_v2_service.api.uri}/api/v1/internal/platform-trends/close"

    # Cloud Scheduler puts the ID token in `Authorization`; why that is safe
    # on this route is written down beside the route itself, in
    # backend/internal/server/routes.go. The audience is the service's root
    # URL, not the path: Cloud Run validates `aud` against the service URL.
    oidc_token {
      service_account_email = google_service_account.daily_close.email
      audience              = google_cloud_run_v2_service.api.uri
    }
  }

  # Generous for a nightly run, which reads thirty days of Ticket Sales and
  # writes one row. The first run writes one row per day of the platform's
  # history; if it is cut off, the days it froze stay frozen and the next
  # run carries on. It stays below api_request_timeout_seconds, as every
  # job's deadline does.
  attempt_deadline = "${var.daily_close_attempt_deadline_seconds}s"

  depends_on = [google_cloud_run_v2_service_iam_member.daily_close_invokes_api]
}

# --- Sentry cron monitor (ADR 0079, #778) ---------------------------------------
#
# The job above checks in as "ticket-pos-daily-close". Its monitor exists
# only while the job is unpaused and the API reports, and takes its name,
# schedule and time zone from the job itself; sentry.tf says the rest.
resource "sentry_cron_monitor" "daily_close" {
  count = local.sentry_cron_monitors_enabled && !google_cloud_scheduler_job.daily_close.paused ? 1 : 0

  organization = var.sentry_organization
  project      = sentry_project.deployable["api"].slug
  name         = trimprefix(google_cloud_scheduler_job.daily_close.name, "${var.environment}-")
  description  = google_cloud_scheduler_job.daily_close.description
  owner        = local.sentry_monitor_owner

  schedule = {
    crontab = google_cloud_scheduler_job.daily_close.schedule
  }
  timezone = google_cloud_scheduler_job.daily_close.time_zone

  # Once a day.
  checkin_margin_minutes  = local.sentry_cron_tolerance.daily_or_weekly.checkin_margin_minutes
  failure_issue_threshold = local.sentry_cron_tolerance.daily_or_weekly.failure_issue_threshold
  recovery_threshold      = local.sentry_cron_tolerance.daily_or_weekly.recovery_threshold
  max_runtime_minutes     = ceil(var.daily_close_attempt_deadline_seconds / 60) + 1
}
