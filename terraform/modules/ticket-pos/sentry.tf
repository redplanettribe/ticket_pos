# --- Sentry (ADR 0079, spec #761, #777) ------------------------------------------
#
# Everything in Sentry that is not a monitor: the team, one project per
# deployable, a key per project, the issue alert rules, the Storefront's inbound
# filters and the rate limit on its key. The cron and uptime monitors are
# declared beside the jobs and services they watch, not here.
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
#   - The per-key rate limit: rate_limit_count and rate_limit_window on
#     sentry_key. Sentry applies it only on a plan with per-key rate limits.
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
  # Only the Storefront's key is rate limited (null is no limit of our own): it
  # is the public one, where a crawler, a bot or a release that throws in every
  # browser could otherwise spend the month's quota in an afternoon. The API and
  # the Staff app are covered by the organization's quota and spike protection.
  sentry_projects = {
    api = {
      slug              = "ticket-pos-api"
      platform          = "go"
      enabled           = var.sentry_api_enabled
      rate_limit_count  = null
      rate_limit_window = null
    }
    staff = {
      slug              = "ticket-pos-staff"
      platform          = "javascript-nextjs"
      enabled           = var.sentry_staff_enabled
      rate_limit_count  = null
      rate_limit_window = null
    }
    storefront = {
      slug              = "ticket-pos-storefront"
      platform          = "javascript-nextjs"
      enabled           = var.sentry_storefront_enabled
      rate_limit_count  = var.sentry_storefront_rate_limit_count
      rate_limit_window = var.sentry_storefront_rate_limit_window_seconds
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

  rate_limit_count  = each.value.rate_limit_count
  rate_limit_window = each.value.rate_limit_window
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
