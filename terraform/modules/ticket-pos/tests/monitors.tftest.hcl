# The Sentry cron and uptime monitors (#778), checked by planning the module
# against mocked providers, as sentry.tftest.hcl does. Nothing is applied and
# nothing reaches Google, AWS or Sentry:
#
#   cd terraform/modules/ticket-pos
#   terraform init
#   terraform test
#
# A cron monitor exists beside a scheduler job only while the job is unpaused
# and the API reports; its slug is the name the backend checks in with
# (backend/internal/server/jobs.go, read here), and its schedule and time zone
# are the scheduler job's own. An uptime monitor exists only while the
# deployable it watches reports and has its production hostname.

mock_provider "google" {}
mock_provider "random" {}

mock_provider "aws" {
  mock_data "aws_iam_policy_document" {
    defaults = {
      json = "{}"
    }
  }
}

mock_provider "sentry" {
  mock_resource "sentry_team" {
    override_during = plan
    defaults = {
      internal_id = "1001"
    }
  }
}

# Distinct, known monitor ids at plan time, so an alert's monitor_ids can be
# compared.
override_resource {
  target          = sentry_cron_monitor.reversal_reconciler
  override_during = plan
  values          = { id = "cron-reversal-reconciler" }
}

override_resource {
  target          = sentry_cron_monitor.sale_invoice_drainer
  override_during = plan
  values          = { id = "cron-sale-invoice-drainer" }
}

override_resource {
  target          = sentry_cron_monitor.follow_digest_enqueue
  override_during = plan
  values          = { id = "cron-follow-digest-enqueue" }
}

override_resource {
  target          = sentry_cron_monitor.follow_digest_drain
  override_during = plan
  values          = { id = "cron-follow-digest-drain" }
}

override_resource {
  target          = sentry_cron_monitor.answer_purge
  override_during = plan
  values          = { id = "cron-answer-purge" }
}

override_resource {
  target          = sentry_cron_monitor.answer_reminder
  override_during = plan
  values          = { id = "cron-answer-reminder" }
}

override_resource {
  target          = sentry_cron_monitor.assignment_reminder
  override_during = plan
  values          = { id = "cron-assignment-reminder" }
}

override_resource {
  target          = sentry_cron_monitor.owed_assignment_mail
  override_during = plan
  values          = { id = "cron-owed-assignment-mail" }
}

override_resource {
  target          = sentry_cron_monitor.event_recap
  override_during = plan
  values          = { id = "cron-event-recap" }
}

override_resource {
  target          = sentry_cron_monitor.holder_address_purge
  override_during = plan
  values          = { id = "cron-holder-address-purge" }
}

override_resource {
  target          = sentry_cron_monitor.daily_close
  override_during = plan
  values          = { id = "cron-daily-close" }
}

override_resource {
  target          = sentry_uptime_monitor.storefront
  override_during = plan
  values          = { id = "uptime-storefront" }
}

override_resource {
  target          = sentry_uptime_monitor.staff
  override_during = plan
  values          = { id = "uptime-staff" }
}

variables {
  project_id             = "test-project"
  region                 = "us-east1"
  environment            = "prod"
  storage_cors_origins   = ["https://shop.example.test"]
  aws_budget_alert_email = "budget@example.test"
  staff_domain           = "pos.example.test"
  storefront_domain      = "discover.example.test"
}

# Every job ships paused, so even with the API reporting there is no cron
# monitor, and no alert with nothing to watch.
run "every_job_paused_declares_no_cron_monitor" {
  command = plan

  variables {
    sentry_api_enabled = true
  }

  assert {
    condition = length(concat(
      sentry_cron_monitor.reversal_reconciler,
      sentry_cron_monitor.sale_invoice_drainer,
      sentry_cron_monitor.follow_digest_enqueue,
      sentry_cron_monitor.follow_digest_drain,
      sentry_cron_monitor.answer_purge,
      sentry_cron_monitor.answer_reminder,
      sentry_cron_monitor.assignment_reminder,
      sentry_cron_monitor.owed_assignment_mail,
      sentry_cron_monitor.event_recap,
      sentry_cron_monitor.holder_address_purge,
      sentry_cron_monitor.daily_close,
    )) == 0
    error_message = "A paused job must have no cron monitor, or it would alert forever."
  }

  assert {
    condition     = length(sentry_alert.job_check_in) == 0
    error_message = "With no cron monitor there must be no check-in alert."
  }

  # The daily close ships paused too, on the platform clock its day is a
  # day of.
  assert {
    condition     = google_cloud_scheduler_job.daily_close.paused && google_cloud_scheduler_job.daily_close.time_zone == "America/Guayaquil"
    error_message = "The daily close must ship paused and be scheduled on the platform clock."
  }
}

# A job running while the API is dormant checks in nowhere: no monitor.
run "an_unpaused_job_with_the_api_dormant_has_no_monitor" {
  command = plan

  variables {
    answer_purge_enabled = true
  }

  assert {
    condition     = length(sentry_cron_monitor.answer_purge) == 0 && length(sentry_alert.job_check_in) == 0
    error_message = "With the API not reporting, an unpaused job must have no cron monitor."
  }
}

# Unpausing one job is the one change that creates its monitor, and no other.
run "unpausing_one_job_creates_its_monitor_only" {
  command = plan

  variables {
    sentry_api_enabled                    = true
    answer_purge_enabled                  = true
    answer_purge_schedule                 = "5 4 * * *"
    answer_purge_attempt_deadline_seconds = 300
  }

  assert {
    condition     = length(sentry_cron_monitor.answer_purge) == 1 && length(sentry_cron_monitor.holder_address_purge) == 0 && length(sentry_cron_monitor.reversal_reconciler) == 0
    error_message = "Only the unpaused job may have a monitor."
  }

  assert {
    condition     = sentry_cron_monitor.answer_purge[0].schedule.crontab == "5 4 * * *" && sentry_cron_monitor.answer_purge[0].schedule.crontab == google_cloud_scheduler_job.answer_purge.schedule
    error_message = "The monitor's schedule must be the scheduler job's, including a schedule moved from its default."
  }

  assert {
    condition     = sentry_cron_monitor.answer_purge[0].timezone == google_cloud_scheduler_job.answer_purge.time_zone
    error_message = "The monitor's time zone must be the scheduler job's."
  }

  # A five-minute deadline: an in-progress check-in is timed out only after
  # Scheduler has itself given up on the run.
  assert {
    condition     = sentry_cron_monitor.answer_purge[0].max_runtime_minutes == 6
    error_message = "The maximum runtime must follow the job's attempt deadline, a minute past it."
  }

  assert {
    condition     = sentry_alert.job_check_in[0].monitor_ids == toset(["cron-answer-purge"])
    error_message = "The check-in alert must watch the one monitor."
  }
}

# Every job unpaused: eleven monitors, one per job, with the backend's slugs.
run "every_job_unpaused_has_its_monitor" {
  command = plan

  variables {
    sentry_api_enabled            = true
    reversal_reconciler_enabled   = true
    sale_invoice_drainer_enabled  = true
    follow_digest_enqueue_enabled = true
    follow_digest_drain_enabled   = true
    answer_purge_enabled          = true
    answer_reminder_enabled       = true
    assignment_reminder_enabled   = true
    owed_assignment_mail_enabled  = true
    event_recap_enabled           = true
    holder_address_purge_enabled  = true
    daily_close_enabled           = true

    # Every schedule moved off its default, so a monitor holding a copy of
    # the default rather than a reference to its job is caught.
    reversal_reconciler_schedule   = "*/2 * * * *"
    sale_invoice_drainer_schedule  = "*/10 * * * *"
    follow_digest_enqueue_schedule = "0 8 * * 1"
    follow_digest_drain_schedule   = "*/2 * * * *"
    answer_purge_schedule          = "21 3 * * *"
    answer_reminder_schedule       = "1 10 * * *"
    assignment_reminder_schedule   = "31 10 * * *"
    owed_assignment_mail_schedule  = "*/2 * * * *"
    event_recap_schedule           = "1 9 * * *"
    holder_address_purge_schedule  = "41 3 * * *"
    daily_close_schedule           = "11 0 * * *"
  }

  # The slug Sentry gives a cron monitor is its name; the names must be the
  # eleven the backend checks in with, read from the backend's own list.
  assert {
    condition = toset([for m in concat(
      sentry_cron_monitor.reversal_reconciler,
      sentry_cron_monitor.sale_invoice_drainer,
      sentry_cron_monitor.follow_digest_enqueue,
      sentry_cron_monitor.follow_digest_drain,
      sentry_cron_monitor.answer_purge,
      sentry_cron_monitor.answer_reminder,
      sentry_cron_monitor.assignment_reminder,
      sentry_cron_monitor.owed_assignment_mail,
      sentry_cron_monitor.event_recap,
      sentry_cron_monitor.holder_address_purge,
      sentry_cron_monitor.daily_close,
    ) : m.name]) == toset([for m in regexall("\"POST [^\"]+\":\\s+\"([a-z0-9-]+)\"", file("../../../backend/internal/server/jobs.go")) : m[0]]) && length(regexall("\"POST [^\"]+\":\\s+\"([a-z0-9-]+)\"", file("../../../backend/internal/server/jobs.go"))) == 11
    error_message = "The cron monitors' names must be exactly the eleven job names in backend/internal/server/jobs.go."
  }

  # Each slug is its scheduler job's name without the environment prefix.
  assert {
    condition = alltrue([
      sentry_cron_monitor.reversal_reconciler[0].name == "ticket-pos-reversal-reconciler" && google_cloud_scheduler_job.reversal_reconciler.name == "prod-ticket-pos-reversal-reconciler",
      sentry_cron_monitor.sale_invoice_drainer[0].name == "ticket-pos-sale-invoice-drainer" && google_cloud_scheduler_job.sale_invoice_drainer.name == "prod-ticket-pos-sale-invoice-drainer",
      sentry_cron_monitor.follow_digest_enqueue[0].name == "ticket-pos-follow-digest-enqueue" && google_cloud_scheduler_job.follow_digest_enqueue.name == "prod-ticket-pos-follow-digest-enqueue",
      sentry_cron_monitor.follow_digest_drain[0].name == "ticket-pos-follow-digest-drain" && google_cloud_scheduler_job.follow_digest_drain.name == "prod-ticket-pos-follow-digest-drain",
      sentry_cron_monitor.answer_purge[0].name == "ticket-pos-answer-purge" && google_cloud_scheduler_job.answer_purge.name == "prod-ticket-pos-answer-purge",
      sentry_cron_monitor.answer_reminder[0].name == "ticket-pos-answer-reminder" && google_cloud_scheduler_job.answer_reminder.name == "prod-ticket-pos-answer-reminder",
      sentry_cron_monitor.assignment_reminder[0].name == "ticket-pos-assignment-reminder" && google_cloud_scheduler_job.assignment_reminder.name == "prod-ticket-pos-assignment-reminder",
      sentry_cron_monitor.owed_assignment_mail[0].name == "ticket-pos-owed-assignment-mail" && google_cloud_scheduler_job.owed_assignment_mail.name == "prod-ticket-pos-owed-assignment-mail",
      sentry_cron_monitor.event_recap[0].name == "ticket-pos-event-recap" && google_cloud_scheduler_job.event_recap.name == "prod-ticket-pos-event-recap",
      sentry_cron_monitor.holder_address_purge[0].name == "ticket-pos-holder-address-purge" && google_cloud_scheduler_job.holder_address_purge.name == "prod-ticket-pos-holder-address-purge",
      sentry_cron_monitor.daily_close[0].name == "ticket-pos-daily-close" && google_cloud_scheduler_job.daily_close.name == "prod-ticket-pos-daily-close",
    ])
    error_message = "Each monitor must sit beside its own scheduler job, named as the job without its environment prefix."
  }

  assert {
    condition = alltrue([
      sentry_cron_monitor.reversal_reconciler[0].schedule.crontab == google_cloud_scheduler_job.reversal_reconciler.schedule && sentry_cron_monitor.reversal_reconciler[0].timezone == google_cloud_scheduler_job.reversal_reconciler.time_zone,
      sentry_cron_monitor.sale_invoice_drainer[0].schedule.crontab == google_cloud_scheduler_job.sale_invoice_drainer.schedule && sentry_cron_monitor.sale_invoice_drainer[0].timezone == google_cloud_scheduler_job.sale_invoice_drainer.time_zone,
      sentry_cron_monitor.follow_digest_enqueue[0].schedule.crontab == google_cloud_scheduler_job.follow_digest_enqueue.schedule && sentry_cron_monitor.follow_digest_enqueue[0].timezone == google_cloud_scheduler_job.follow_digest_enqueue.time_zone,
      sentry_cron_monitor.follow_digest_drain[0].schedule.crontab == google_cloud_scheduler_job.follow_digest_drain.schedule && sentry_cron_monitor.follow_digest_drain[0].timezone == google_cloud_scheduler_job.follow_digest_drain.time_zone,
      sentry_cron_monitor.answer_purge[0].schedule.crontab == google_cloud_scheduler_job.answer_purge.schedule && sentry_cron_monitor.answer_purge[0].timezone == google_cloud_scheduler_job.answer_purge.time_zone,
      sentry_cron_monitor.answer_reminder[0].schedule.crontab == google_cloud_scheduler_job.answer_reminder.schedule && sentry_cron_monitor.answer_reminder[0].timezone == google_cloud_scheduler_job.answer_reminder.time_zone,
      sentry_cron_monitor.assignment_reminder[0].schedule.crontab == google_cloud_scheduler_job.assignment_reminder.schedule && sentry_cron_monitor.assignment_reminder[0].timezone == google_cloud_scheduler_job.assignment_reminder.time_zone,
      sentry_cron_monitor.owed_assignment_mail[0].schedule.crontab == google_cloud_scheduler_job.owed_assignment_mail.schedule && sentry_cron_monitor.owed_assignment_mail[0].timezone == google_cloud_scheduler_job.owed_assignment_mail.time_zone,
      sentry_cron_monitor.event_recap[0].schedule.crontab == google_cloud_scheduler_job.event_recap.schedule && sentry_cron_monitor.event_recap[0].timezone == google_cloud_scheduler_job.event_recap.time_zone,
      sentry_cron_monitor.holder_address_purge[0].schedule.crontab == google_cloud_scheduler_job.holder_address_purge.schedule && sentry_cron_monitor.holder_address_purge[0].timezone == google_cloud_scheduler_job.holder_address_purge.time_zone,
      sentry_cron_monitor.daily_close[0].schedule.crontab == google_cloud_scheduler_job.daily_close.schedule && sentry_cron_monitor.daily_close[0].timezone == google_cloud_scheduler_job.daily_close.time_zone,
    ])
    error_message = "Every monitor's schedule and time zone must be its scheduler job's."
  }

  # Every monitor lives in the API's project, is owned by the team, and times a
  # run out only after its scheduler deadline.
  assert {
    condition = alltrue([for m in concat(
      sentry_cron_monitor.reversal_reconciler,
      sentry_cron_monitor.sale_invoice_drainer,
      sentry_cron_monitor.follow_digest_enqueue,
      sentry_cron_monitor.follow_digest_drain,
      sentry_cron_monitor.answer_purge,
      sentry_cron_monitor.answer_reminder,
      sentry_cron_monitor.assignment_reminder,
      sentry_cron_monitor.owed_assignment_mail,
      sentry_cron_monitor.event_recap,
      sentry_cron_monitor.holder_address_purge,
      sentry_cron_monitor.daily_close,
    ) : m.project == "ticket-pos-api" && m.owner.team_id == "1001" && m.checkin_margin_minutes >= 1 && m.max_runtime_minutes >= 2 && m.failure_issue_threshold >= 1 && m.recovery_threshold >= 1])
    error_message = "Every cron monitor must be in ticket-pos-api, owned by the team, with a margin and a runtime past its deadline."
  }

  assert {
    condition     = length(sentry_alert.job_check_in[0].monitor_ids) == 11
    error_message = "The check-in alert must watch all eleven monitors."
  }

  # Failed, missed and timed-out check-ins all open the monitor's issue: one
  # rule emails the team when it opens or comes back.
  assert {
    condition     = sentry_alert.job_check_in[0].environment == "production" && sentry_alert.job_check_in[0].action_filters[0].actions[0].email.target_type == "team" && sentry_alert.job_check_in[0].action_filters[0].actions[0].email.target_id == "1001"
    error_message = "The check-in alert must email the ticket-pos team, in production."
  }

  assert {
    condition     = toset([for t in sentry_alert.job_check_in[0].trigger_conditions : t.first_seen_event != null ? "first_seen" : t.regression_event != null ? "regression" : "other"]) == toset(["first_seen", "regression"])
    error_message = "The check-in alert must fire when a monitor's issue opens and when it comes back."
  }
}

# Uptime: none while the Next apps are dormant.
run "no_uptime_monitor_while_the_apps_are_dormant" {
  command = plan

  variables {
    sentry_api_enabled = true
  }

  assert {
    condition     = length(sentry_uptime_monitor.storefront) + length(sentry_uptime_monitor.staff) + length(sentry_alert.uptime) == 0
    error_message = "With the Storefront and the Staff app not reporting there must be no uptime monitor or alert."
  }
}

run "the_storefront_switch_probes_its_deep_health_route" {
  command = plan

  variables {
    sentry_storefront_enabled = true
  }

  assert {
    condition     = length(sentry_uptime_monitor.storefront) == 1 && length(sentry_uptime_monitor.staff) == 0
    error_message = "The Storefront's switch must create its uptime monitor only."
  }

  assert {
    condition     = sentry_uptime_monitor.storefront[0].url == "https://discover.example.test/api/health" && sentry_uptime_monitor.storefront[0].method == "GET" && sentry_uptime_monitor.storefront[0].project == "ticket-pos-storefront" && sentry_uptime_monitor.storefront[0].environment == "production"
    error_message = "The Storefront monitor must GET the deep-health route on the Storefront's hostname, in its project, in production."
  }

  # The route answers within its own 4 s bound; the probe must wait longer.
  assert {
    condition     = sentry_uptime_monitor.storefront[0].timeout_ms > 4000
    error_message = "The probe's timeout must be longer than the deep-health route's own bound."
  }

  assert {
    condition     = jsondecode(sentry_uptime_monitor.storefront[0].assertion_json).root.op == "and"
    error_message = "The probe must assert a 2xx answer."
  }

  assert {
    condition     = sentry_alert.uptime[0].monitor_ids == toset(["uptime-storefront"]) && sentry_alert.uptime[0].environment == "production" && sentry_alert.uptime[0].action_filters[0].actions[0].email.target_type == "team" && sentry_alert.uptime[0].action_filters[0].actions[0].email.target_id == "1001"
    error_message = "An uptime failure must email the ticket-pos team."
  }
}

run "the_staff_switch_probes_its_sign_in_page" {
  command = plan

  variables {
    sentry_staff_enabled = true
  }

  assert {
    condition     = length(sentry_uptime_monitor.staff) == 1 && length(sentry_uptime_monitor.storefront) == 0
    error_message = "The Staff app's switch must create its uptime monitor only."
  }

  assert {
    condition     = sentry_uptime_monitor.staff[0].url == "https://pos.example.test/login" && sentry_uptime_monitor.staff[0].method == "GET" && sentry_uptime_monitor.staff[0].project == "ticket-pos-staff" && sentry_uptime_monitor.staff[0].environment == "production"
    error_message = "The Staff monitor must GET the sign-in page on the Staff hostname, in its project, in production."
  }

  assert {
    condition     = sentry_alert.uptime[0].monitor_ids == toset(["uptime-staff"])
    error_message = "The uptime alert must watch the Staff monitor."
  }
}

# No hostname, nothing to probe: the run.app URL is not the production address.
run "no_hostname_no_uptime_monitor" {
  command = plan

  variables {
    sentry_staff_enabled      = true
    sentry_storefront_enabled = true
    staff_domain              = null
    storefront_domain         = null
  }

  assert {
    condition     = length(sentry_uptime_monitor.storefront) + length(sentry_uptime_monitor.staff) + length(sentry_alert.uptime) == 0
    error_message = "Without its hostname a deployable must have no uptime monitor."
  }
}
