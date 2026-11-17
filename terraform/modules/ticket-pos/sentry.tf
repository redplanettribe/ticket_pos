# --- Sentry (ADR 0079, spec #761, #777) ------------------------------------------
#
# Everything in Sentry that is not a monitor: the team, one project per
# deployable, a key per project, the issue alert rules, the Storefront's inbound
# filters. The cron and uptime monitors are
# declared beside the jobs and services they watch, not here; what they share,
# and the two alert rules that watch them, are at the end of this file.
#
# The organization already exists and is shared with another product. Nothing
# here reads, imports or changes the organization or any project but these
# three; there is no organization-wide setting in this file.
#
# Declaring all of this changes nothing a customer or the API can see. A DSN
# reaches a Cloud Run service only when that deployable's switch is on
# (sentry_api_enabled, sentry_staff_enabled, sentry_storefront_enabled, all
# default false), and with no SENTRY_DSN every deployable is dormant: it
# constructs nothing Sentry-related and sends nothing.
#
# Provider: jianyuan/sentry, the community provider, configured in the root with
# an operator-supplied token. What it could and could not declare, checked
# against v0.16.0's schema:
#
#   - Team, project, key, inbound data filter: sentry_team, sentry_project,
#     sentry_key, sentry_project_inbound_data_filter.
#   - A per-key rate limit: rate_limit_count and rate_limit_window on
#     sentry_key. Not used: the organization's Team plan lacks the feature, and
#     Sentry refuses to create a key that sets one there (#783).
#   - Issue alert rules: sentry_alert, on each project's default error monitor.
#     sentry_issue_alert still exists but is deprecated in favour of it.
#   - Data scrubbing: nothing, at the organization or the project level.
#     sentry_project carries no scrubbing setting and sentry_organization only
#     its name and slug. Project-level scrubbing is therefore one scripted API
#     call, scripts/sentry-scrubbing.sh, which the operator runs after the apply
#     (terraform/README.md, "Sentry").

locals {
  # The one environment this deployment reports as. The API derives the same
  # value from APP_ENV=production; the Next apps read SENTRY_ENVIRONMENT, and
  # without it report as "development".
  sentry_environment = "production"

  # One project per deployable (ADR 0079). The keys of this map name the
  # deployables everywhere below and in the switches.
  #
  # No key is rate limited: the Team plan has no per-key rate limits (#783).
  # The Storefront is the public one, where a crawler, a bot or a release that
  # throws in every browser makes noise. What bounds that noise is the
  # Storefront's own before-send filtering (ADR 0079), its three inbound data
  # filters below, the organization's error quota and Sentry's spike
  # protection. The API and the Staff app are covered by the last two.
  sentry_projects = {
    api = {
      slug     = "ticket-pos-api"
      platform = "go"
      enabled  = var.sentry_api_enabled
    }
    staff = {
      slug     = "ticket-pos-staff"
      platform = "javascript-nextjs"
      enabled  = var.sentry_staff_enabled
    }
    storefront = {
      slug     = "ticket-pos-storefront"
      platform = "javascript-nextjs"
      enabled  = var.sentry_storefront_enabled
    }
  }

  # The DSN each service is given when its switch is on, null when it is off
  # (the services test the switch itself, which is known at plan time even
  # before the key exists).
  # A DSN is not a secret: it only lets its holder send events to the project,
  # and the browser half of each Next app carries it in the page anyway. The
  # provider marks the whole DSN map sensitive because one of its entries is the
  # legacy secret DSN; the public one is all that is used, and it is a plain
  # environment value on the service.
  sentry_dsn = {
    for name, project in local.sentry_projects :
    name => project.enabled ? nonsensitive(sentry_key.deployable[name].dsn["public"]) : null
  }

  # Error spike: more errors than this across the whole project within an
  # hour, whichever issues they belong to, so a bad release that throws many
  # different errors is caught as surely as one that throws one error often.
  sentry_spike_events_per_hour = 50

  # Every rule's one action: an email to every member of the ticket-pos team
  # (each member's own notification settings still apply).
  sentry_email_team = {
    email = {
      target_type = "team"
      target_id   = sentry_team.ticket_pos.internal_id
    }
  }
}

resource "sentry_team" "ticket_pos" {
  organization = var.sentry_organization
  name         = "Ticket POS"
  slug         = "ticket-pos"
}

resource "sentry_project" "deployable" {
  for_each = local.sentry_projects

  organization = var.sentry_organization
  teams        = [sentry_team.ticket_pos.slug]
  name         = each.value.slug
  slug         = each.value.slug
  platform     = each.value.platform

  # The key and the alert rules are the ones below; Sentry's own defaults (a
  # key named "Default", an alert to every member on every new issue in any
  # environment) would sit beside them, unmanaged. Both take effect only when
  # the project is created.
  default_key   = false
  default_rules = false
}

resource "sentry_key" "deployable" {
  for_each = local.sentry_projects

  organization = var.sentry_organization
  project      = sentry_project.deployable[each.key].slug
  name         = "${var.environment}-ticket-pos-${each.key}"
}

# --- Storefront inbound filters ---------------------------------------------------
#
# Noise Sentry drops before it counts against the quota. The Storefront's own
# before-send already drops events with no frame from our bundles and known bot
# user agents (ADR 0079); these are the server-side half. The API and the Staff
# app have no browser crowd to filter.

resource "sentry_project_inbound_data_filter" "storefront_browser_extensions" {
  organization = var.sentry_organization
  project      = sentry_project.deployable["storefront"].slug
  filter_id    = "browser-extensions"
  active       = true
}

resource "sentry_project_inbound_data_filter" "storefront_web_crawlers" {
  organization = var.sentry_organization
  project      = sentry_project.deployable["storefront"].slug
  filter_id    = "web-crawlers"
  active       = true
}

# Every current legacy-browser subfilter. Sentry's cut-offs, at the time of
# writing: Internet Explorer, Edge up to 110, Safari up to 15, Firefox up to
# 110, Chrome up to 110, Opera up to 99, Android up to 3, Opera Mini up to 34.
# Anything not listed is disabled. The deprecated per-version subfilters
# (ie_pre_9, ie9, safari_pre_6 and so on) are covered by these.
resource "sentry_project_inbound_data_filter" "storefront_legacy_browsers" {
  organization = var.sentry_organization
  project      = sentry_project.deployable["storefront"].slug
  filter_id    = "legacy-browsers"
  subfilters   = ["android", "chrome", "edge", "firefox", "ie", "opera", "opera_mini", "safari"]
}

# --- Issue alert rules --------------------------------------------------------------
#
# Three rules on each project, all in production and all by email to the
# ticket-pos team, so this product's alerts never reach the other product's
# people and theirs never reach ours.
#
# Sentry creates a default error monitor with every project; an alert rule is
# attached to it. sentry_issue_alert, the classic resource, is deprecated in
# favour of this one.

data "sentry_project_error_monitor" "deployable" {
  for_each = local.sentry_projects

  organization = var.sentry_organization
  project      = sentry_project.deployable[each.key].slug
}

# A kind of error never seen before appears in production.
resource "sentry_alert" "new_issue" {
  for_each = local.sentry_projects

  organization      = var.sentry_organization
  name              = "${each.value.slug}: new issue in production"
  environment       = local.sentry_environment
  monitor_ids       = [data.sentry_project_error_monitor.deployable[each.key].id]
  frequency_minutes = 30

  trigger_conditions = [
    { first_seen_event = {} },
  ]

  action_filters = [
    {
      logic_type = "all"
      actions    = [local.sentry_email_team]
    },
  ]
}

# An issue marked resolved happens again.
resource "sentry_alert" "regression" {
  for_each = local.sentry_projects

  organization      = var.sentry_organization
  name              = "${each.value.slug}: regression in production"
  environment       = local.sentry_environment
  monitor_ids       = [data.sentry_project_error_monitor.deployable[each.key].id]
  frequency_minutes = 30

  trigger_conditions = [
    { regression_event = {} },
  ]

  action_filters = [
    {
      logic_type = "all"
      actions    = [local.sentry_email_team]
    },
  ]
}

# More than sentry_spike_events_per_hour errors in production within an hour,
# across the project: how a bad release shows itself. A trigger condition, so
# it counts every event the rule sees ("number of events seen by the workflow"
# in the provider), not one issue's; the classic per-issue "number of events in
# an issue" is an action filter instead, and would miss a release that throws
# many different errors. At most one email an hour.
resource "sentry_alert" "error_spike" {
  for_each = local.sentry_projects

  organization      = var.sentry_organization
  name              = "${each.value.slug}: error spike in production"
  environment       = local.sentry_environment
  monitor_ids       = [data.sentry_project_error_monitor.deployable[each.key].id]
  frequency_minutes = 60

  trigger_conditions = [
    {
      event_frequency_count = {
        interval = "1h"
        value    = local.sentry_spike_events_per_hour
      }
    },
  ]

  action_filters = [
    {
      logic_type = "all"
      actions    = [local.sentry_email_team]
    },
  ]
}

# --- Cron and uptime monitors (#778) ---------------------------------------------
#
# A cron monitor sits beside each Cloud Scheduler job, in that job's file
# (reversal_reconciler.tf and its eight neighbours), and an uptime monitor beside
# the Storefront and Staff services (cloud_run_frontends.tf). Each exists only
# while the deployable it watches reports: a cron monitor needs the API's switch
# and an unpaused job, an uptime monitor its app's switch and hostname. A job
# shipped paused has no monitor, so it cannot alert forever, and unpausing it is
# the one change that creates its monitor.
#
# The provider's sentry_cron_monitor has no slug argument: Sentry makes the slug
# from the name. Each monitor is therefore named exactly its slug, the job name
# the API checks in with (backend/internal/server/jobs.go): its scheduler job's
# name without the environment prefix, already lower case and hyphenated, so
# Sentry's slug is the name itself. A check-in for a slug with no monitor is
# dropped by Sentry, and the monitor then reports a missed run.
#
# Every monitor is in a deployable's own project and environment production,
# owned by the ticket-pos team, so its issues are assigned to the team.

locals {
  sentry_monitor_owner = {
    team_id = sentry_team.ticket_pos.internal_id
  }

  # How much a cron monitor tolerates, by how often its job runs. Each job's
  # maximum runtime is not here: it is its own attempt deadline rounded up to the
  # minute, plus one, so a run is timed out only after Cloud Scheduler has itself
  # given up on it.
  #
  # every_minute: Cloud Scheduler delivers at least once, not exactly once, and
  #   the next tick does whatever a skipped or failed one did not. One minute of
  #   grace, and an issue after five bad ticks in a row: five minutes of nothing
  #   is an outage, one is noise. Two good ticks resolve it.
  # every_five_minutes: the Sale Invoice Drainer, behind the post-checkout kick.
  #   Two minutes of grace, an issue after two bad ticks (ten minutes).
  # daily_or_weekly: a missed run is a day or a week of work not done, so the
  #   first one opens an issue. Fifteen minutes of grace for a late start.
  sentry_cron_tolerance = {
    every_minute = {
      checkin_margin_minutes  = 1
      failure_issue_threshold = 5
      recovery_threshold      = 2
    }
    every_five_minutes = {
      checkin_margin_minutes  = 2
      failure_issue_threshold = 2
      recovery_threshold      = 1
    }
    daily_or_weekly = {
      checkin_margin_minutes  = 15
      failure_issue_threshold = 1
      recovery_threshold      = 1
    }
  }

  # Whether the API reports: no check-in leaves it otherwise, so no job may
  # have a monitor.
  sentry_cron_monitors_enabled = var.sentry_api_enabled

  # An uptime probe passes on a 2xx answer only. A redirect to some other page
  # or a 503 from the deep-health route is down.
  sentry_uptime_assertion = jsonencode({
    root = {
      op = "and"
      children = [
        { op = "status_code_check", operator = { cmp = "greater_than" }, value = 199 },
        { op = "status_code_check", operator = { cmp = "less_than" }, value = 300 },
      ]
    }
  })

  sentry_cron_monitor_ids = concat(
    sentry_cron_monitor.reversal_reconciler[*].id,
    sentry_cron_monitor.sale_invoice_drainer[*].id,
    sentry_cron_monitor.follow_digest_enqueue[*].id,
    sentry_cron_monitor.follow_digest_drain[*].id,
    sentry_cron_monitor.answer_purge[*].id,
    sentry_cron_monitor.answer_reminder[*].id,
    sentry_cron_monitor.assignment_reminder[*].id,
    sentry_cron_monitor.owed_assignment_mail[*].id,
    sentry_cron_monitor.holder_address_purge[*].id,
  )

  sentry_uptime_monitor_ids = concat(
    sentry_uptime_monitor.storefront[*].id,
    sentry_uptime_monitor.staff[*].id,
  )
}

# A job failed or did not run. A failed check-in (the run answered a 5xx or
# panicked), a missed one (no check-in within the margin) and a timed-out one
# (started, never finished within the maximum runtime) all count towards the
# monitor's failure threshold, and reaching it opens the monitor's issue: this
# rule emails the team when that issue opens, and again if it comes back after
# recovering. Declared only while some monitor exists, since a rule must watch
# at least one.
resource "sentry_alert" "job_check_in" {
  count = length(local.sentry_cron_monitor_ids) > 0 ? 1 : 0

  organization      = var.sentry_organization
  name              = "ticket-pos: scheduled job failed or missed in production"
  environment       = local.sentry_environment
  monitor_ids       = local.sentry_cron_monitor_ids
  frequency_minutes = 30

  trigger_conditions = [
    { first_seen_event = {} },
    { regression_event = {} },
  ]

  action_filters = [
    {
      logic_type = "all"
      actions    = [local.sentry_email_team]
    },
  ]
}

# The Storefront or the Staff app stopped answering: the uptime monitor's
# downtime threshold of failed probes in a row opens its issue, and this rule
# emails the team when it opens or comes back.
resource "sentry_alert" "uptime" {
  count = length(local.sentry_uptime_monitor_ids) > 0 ? 1 : 0

  organization      = var.sentry_organization
  name              = "ticket-pos: Storefront or Staff app down in production"
  environment       = local.sentry_environment
  monitor_ids       = local.sentry_uptime_monitor_ids
  frequency_minutes = 30

  trigger_conditions = [
    { first_seen_event = {} },
    { regression_event = {} },
  ]

  action_filters = [
    {
      logic_type = "all"
      actions    = [local.sentry_email_team]
    },
  ]
}
