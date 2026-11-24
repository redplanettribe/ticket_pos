# Nothing is tracked (spec #870, #881): the SES configuration set every send
# goes through keeps open and click tracking off, now that every mail kind
# carries an HTML part SES could add a pixel to or rewrite the links of.
# Checked by planning the module against mocked providers, as
# sentry.tftest.hcl does. Nothing is applied and nothing reaches Google, AWS
# or Sentry:
#
#   cd terraform/modules/ticket-pos
#   terraform init
#   terraform test
#
# SES only tracks opens and clicks through an event destination matching OPEN
# or CLICK, or through Virtual Deliverability Manager's engagement tracking,
# so the set must have engagement metrics disabled, no custom redirect domain,
# and no Terraform here may declare an event destination at all. A test cannot
# list resources that do not exist, so that last check reads every .tf under
# modules/ and envs/ for the resource types; a comment naming one trips it too.

mock_provider "google" {}
mock_provider "random" {}
mock_provider "sentry" {}

mock_provider "aws" {
  mock_data "aws_iam_policy_document" {
    defaults = {
      json = "{}"
    }
  }
}

variables {
  project_id             = "test-project"
  region                 = "us-east1"
  environment            = "prod"
  storage_cors_origins   = ["https://shop.example.test"]
  aws_budget_alert_email = "budget@example.test"
}

run "the_configuration_set_tracks_no_open_or_click" {
  command = plan

  assert {
    condition     = aws_sesv2_configuration_set.main.vdm_options[0].dashboard_options[0].engagement_metrics == "DISABLED"
    error_message = "The configuration set's engagement metrics must stay DISABLED, so turning VDM on account-wide does not track opens and clicks."
  }

  assert {
    condition     = length(aws_sesv2_configuration_set.main.tracking_options) == 0
    error_message = "The configuration set has tracking options: a custom redirect domain is only there to rewrite links for click tracking."
  }

  assert {
    condition = length(fileset("${path.module}/../..", "{modules,envs}/**/*.tf")) > 0 && alltrue([
      for f in fileset("${path.module}/../..", "{modules,envs}/**/*.tf") :
      !strcontains(file("${path.module}/../../${f}"), "aws_sesv2_configuration_set_event_destination") &&
      !strcontains(file("${path.module}/../../${f}"), "aws_ses_event_destination")
    ])
    error_message = "An SES event destination is declared; one matching OPEN or CLICK tracks the mail. Nothing is tracked (spec #870)."
  }
}
