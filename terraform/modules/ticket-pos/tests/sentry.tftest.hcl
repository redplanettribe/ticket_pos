# The Sentry switches (sentry.tf, #777), checked by planning the module against
# mocked providers. Nothing here reaches Google, AWS or Sentry, nothing is
# applied, and no credential is needed:
#
#   cd terraform/modules/ticket-pos
#   terraform init
#   terraform test
#
# With every switch at its default no Cloud Run service gets a Sentry variable,
# though the Sentry team, projects, keys and alert rules are still declared;
# each switch on its own sets the DSN (and, for a Next app, the environment) on
# that service and on no other. SENTRY_DSN and SENTRY_ENVIRONMENT are the names
# the deployables read (ADR 0079); the API derives its environment from APP_ENV.

mock_provider "google" {}
mock_provider "random" {}

mock_provider "aws" {
  # The SES role's policies are rendered by these data sources, and the role
  # refuses a mock's random string where it expects JSON.
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

  # A known DSN at plan time, so the value reaching a service can be compared.
  mock_resource "sentry_key" {
    override_during = plan
    defaults = {
      dsn = {
        public = "https://public@o1.ingest.us.sentry.io/1"
        secret = "https://public:secret@o1.ingest.us.sentry.io/1"
      }
    }
  }
}

# Distinct, known metric monitor ids at plan time, so each spike rule can be
# matched to its own project's monitor.
override_resource {
  target          = sentry_metric_monitor.error_spike["api"]
  override_during = plan
  values          = { id = "metric-api" }
}

override_resource {
  target          = sentry_metric_monitor.error_spike["staff"]
  override_during = plan
  values          = { id = "metric-staff" }
}

override_resource {
  target          = sentry_metric_monitor.error_spike["storefront"]
  override_during = plan
  values          = { id = "metric-storefront" }
}

variables {
  project_id             = "test-project"
  region                 = "us-east1"
  environment            = "prod"
  storage_cors_origins   = ["https://shop.example.test"]
  aws_budget_alert_email = "budget@example.test"
}

run "every_switch_off_sets_no_sentry_variable" {
  command = plan

  assert {
    condition     = length([for e in google_cloud_run_v2_service.api.template[0].containers[0].env : e if startswith(e.name, "SENTRY_")]) == 0
    error_message = "With every switch off the API service must carry no SENTRY_ variable."
  }

  assert {
    condition     = length([for e in google_cloud_run_v2_service.staff.template[0].containers[0].env : e if startswith(e.name, "SENTRY_")]) == 0
    error_message = "With every switch off the Staff service must carry no SENTRY_ variable."
  }

  assert {
    condition     = length([for e in google_cloud_run_v2_service.storefront.template[0].containers[0].env : e if startswith(e.name, "SENTRY_")]) == 0
    error_message = "With every switch off the Storefront service must carry no SENTRY_ variable."
  }

  # The Sentry side is declared whatever the switches say.
  assert {
    condition     = sentry_team.ticket_pos.slug == "ticket-pos"
    error_message = "The ticket-pos team must be declared."
  }

  assert {
    condition     = toset([for p in sentry_project.deployable : p.slug]) == toset(["ticket-pos-api", "ticket-pos-staff", "ticket-pos-storefront"])
    error_message = "The three projects must be declared with the spec's slugs."
  }

  assert {
    condition     = length(sentry_key.deployable) == 3
    error_message = "Each project must have its key."
  }
}

run "api_switch_sets_the_dsn_on_the_api_only" {
  command = plan

  variables {
    sentry_api_enabled = true
  }

  assert {
    condition     = [for e in google_cloud_run_v2_service.api.template[0].containers[0].env : e.value if e.name == "SENTRY_DSN"] == ["https://public@o1.ingest.us.sentry.io/1"]
    error_message = "The API service must carry ticket-pos-api's public DSN as SENTRY_DSN."
  }

  assert {
    condition     = length([for e in google_cloud_run_v2_service.api.template[0].containers[0].env : e if e.name == "SENTRY_ENVIRONMENT"]) == 0
    error_message = "The API reads its environment from APP_ENV; it is given no SENTRY_ENVIRONMENT."
  }

  assert {
    condition     = length([for e in google_cloud_run_v2_service.staff.template[0].containers[0].env : e if startswith(e.name, "SENTRY_")]) + length([for e in google_cloud_run_v2_service.storefront.template[0].containers[0].env : e if startswith(e.name, "SENTRY_")]) == 0
    error_message = "Switching the API on must not touch either Next app."
  }
}

run "staff_switch_sets_the_dsn_on_the_staff_app_only" {
  command = plan

  variables {
    sentry_staff_enabled = true
  }

  assert {
    condition     = { for e in google_cloud_run_v2_service.staff.template[0].containers[0].env : e.name => e.value if startswith(e.name, "SENTRY_") } == { SENTRY_DSN = "https://public@o1.ingest.us.sentry.io/1", SENTRY_ENVIRONMENT = "production" }
    error_message = "The Staff service must carry its DSN and SENTRY_ENVIRONMENT=production."
  }

  assert {
    condition     = length([for e in google_cloud_run_v2_service.api.template[0].containers[0].env : e if startswith(e.name, "SENTRY_")]) + length([for e in google_cloud_run_v2_service.storefront.template[0].containers[0].env : e if startswith(e.name, "SENTRY_")]) == 0
    error_message = "Switching the Staff app on must not touch the API or the Storefront."
  }
}

run "storefront_switch_sets_the_dsn_on_the_storefront_only" {
  command = plan

  variables {
    sentry_storefront_enabled = true
  }

  assert {
    condition     = { for e in google_cloud_run_v2_service.storefront.template[0].containers[0].env : e.name => e.value if startswith(e.name, "SENTRY_") } == { SENTRY_DSN = "https://public@o1.ingest.us.sentry.io/1", SENTRY_ENVIRONMENT = "production" }
    error_message = "The Storefront service must carry its DSN and SENTRY_ENVIRONMENT=production."
  }

  assert {
    condition     = length([for e in google_cloud_run_v2_service.api.template[0].containers[0].env : e if startswith(e.name, "SENTRY_")]) + length([for e in google_cloud_run_v2_service.staff.template[0].containers[0].env : e if startswith(e.name, "SENTRY_")]) == 0
    error_message = "Switching the Storefront on must not touch the API or the Staff app."
  }
}

# The rest of what the ticket declares, read from the plan.
run "alerts_filters_and_rate_limit" {
  command = plan

  assert {
    condition     = alltrue([for k in ["api", "staff", "storefront"] : contains(keys(sentry_alert.new_issue), k) && contains(keys(sentry_alert.regression), k) && contains(keys(sentry_alert.error_spike), k)])
    error_message = "Each project must have the new issue, regression and error spike rules."
  }

  assert {
    condition     = alltrue([for a in concat(values(sentry_alert.new_issue), values(sentry_alert.regression), values(sentry_alert.error_spike)) : a.environment == "production" && a.action_filters[0].actions[0].email.target_type == "team"])
    error_message = "Every rule must be scoped to production and email the team."
  }

  # A spike counts every error in the project, whichever issue it belongs to:
  # a metric monitor's count over an hour, never Sentry's per-issue
  # event_frequency_count.
  assert {
    condition = alltrue([for k, m in sentry_metric_monitor.error_spike :
      m.project == sentry_project.deployable[k].slug
      && m.environment == "production"
      && m.dataset == "events"
      && m.event_types == toset(["error"])
      && m.aggregate == "count()"
      && m.time_window_seconds == 3600
      && m.issue_detection.type == "static"
      && length([for c in m.condition_group.conditions : c if c.type == "gt" && c.comparison == 50 && c.condition_result > 0]) == 1
      && m.owner.team_id == "1001"
    ]) && length(sentry_metric_monitor.error_spike) == 3
    error_message = "Each project must have a static metric monitor counting its production errors over 3600 seconds, above 50, owned by the team."
  }

  assert {
    condition     = alltrue([for k, a in sentry_alert.error_spike : a.monitor_ids == toset(["metric-${k}"]) && a.frequency_minutes == 60])
    error_message = "Each error spike rule must watch its own project's metric monitor only, at most once an hour."
  }

  assert {
    condition     = alltrue([for a in values(sentry_alert.error_spike) : alltrue([for c in a.trigger_conditions : c.event_frequency_count == null])])
    error_message = "The error spike rule must not use the per-issue event_frequency_count."
  }

  assert {
    condition     = sentry_key.deployable["storefront"].rate_limit_count == 500 && sentry_key.deployable["storefront"].rate_limit_window == 3600
    error_message = "The Storefront key must carry the rate limit."
  }

  assert {
    condition     = sentry_project_inbound_data_filter.storefront_browser_extensions.active && sentry_project_inbound_data_filter.storefront_web_crawlers.active && length(sentry_project_inbound_data_filter.storefront_legacy_browsers.subfilters) == 8
    error_message = "The Storefront's browser extension, crawler and legacy browser filters must be on."
  }
}
