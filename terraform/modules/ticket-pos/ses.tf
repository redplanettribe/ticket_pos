# Amazon SES, dormant beside Resend (ADR 0077).
#
# Everything on the AWS side of the second email provider: the two sending
# identities verified with Easy DKIM, a custom MAIL FROM for each, a
# configuration set, account-level suppression, the IAM role the API assumes by
# Workload Identity Federation, and a budget alert.
#
# Applying this file changes no delivery behaviour. The API sends through SES
# only when email_provider or digest_email_provider says "ses", and both default
# to "resend". The SES settings below are mounted on the service either way; the
# API reads them only for an identity that selects ses.
#
# The resources live in a dedicated member account of an AWS Organization,
# reached through the root's aws provider (an IAM Identity Center profile). The
# Organization and the account are created by hand, never here.
#
# WHAT THIS FILE DOES NOT DO: create DNS records. The zone is at Namecheap
# (ADR 0007), so the DKIM and MAIL FROM records are added by hand from the
# ses_dns_setup output below. Until they are, SES reports both identities as
# pending and refuses to send from them.

variable "email_provider" {
  description = "Provider the transactional sending identity delivers through: \"resend\" or \"ses\". Switching it is switch day (ADR 0077); setting it back is the rollback."
  type        = string
  default     = "resend"

  validation {
    condition     = contains(["resend", "ses"], var.email_provider)
    error_message = "email_provider must be \"resend\" or \"ses\"."
  }
}

variable "digest_email_provider" {
  description = "Provider the Follow Digest's sending identity delivers through: \"resend\" or \"ses\", independent of email_provider. The API accepts \"ses\" here only once the Digest's SES sender has shipped; until then it refuses to start with it."
  type        = string
  default     = "resend"

  validation {
    condition     = contains(["resend", "ses"], var.digest_email_provider)
    error_message = "digest_email_provider must be \"resend\" or \"ses\"."
  }
}

variable "ses_region" {
  description = "AWS region SES sends from. Must match the region of the root's aws provider, where the identities and configuration set are created."
  type        = string
  default     = "us-east-1"
}

# Resend's return path on each sending subdomain is `send.<domain>` (an MX and
# an SPF TXT, checked in public DNS on 2026-11-13), so SES's MAIL FROM must be
# any other name. Both providers then hold verified records on the same
# subdomain at once and neither breaks the other.
variable "ses_mail_from_label" {
  description = "Leftmost label of SES's custom MAIL FROM on each sending subdomain, e.g. \"bounce\" gives bounce.send.multiticketing.com. Must not be \"send\", which is Resend's return path."
  type        = string
  default     = "bounce"

  validation {
    condition     = var.ses_mail_from_label != "send" && can(regex("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$", var.ses_mail_from_label))
    error_message = "ses_mail_from_label must be a single DNS label other than \"send\", which Resend already uses as its return path."
  }
}

variable "aws_budget_limit_usd" {
  description = "Monthly cost, in USD, of the SES member account above which the budget alert fires."
  type        = string
  default     = "10"
}

variable "aws_budget_alert_email" {
  description = "Address the AWS Budget alert is sent to. Supplied through TF_VAR_aws_budget_alert_email, never a committed tfvars: it is a personal address."
  type        = string
  sensitive   = true

  validation {
    condition     = can(regex("^[^@[:space:]]+@[^@[:space:]]+$", var.aws_budget_alert_email))
    error_message = "aws_budget_alert_email must be an email address."
  }
}

locals {
  # The two sending identities, keyed by role. On a shared Digest domain the
  # two collapse onto one SES identity, which is the only thing SES allows.
  ses_sending_domains = {
    transactional = local.transactional_email_domain
    digest        = local.digest_email_domain
  }
  ses_identities = toset([for d in values(local.ses_sending_domains) : d if d != ""])

  ses_configuration_set_name = "${var.environment}-ticket-pos"

  # The audience the API asks the GCP metadata server to mint its ID token
  # for, and the only one the role's trust accepts. It names this role so a
  # token minted for anything else is not accepted here.
  ses_web_identity_audience = "${var.environment}-ticket-pos-api-ses"
}

# Easy DKIM: SES generates the keys and publishes three CNAME tokens, which
# ses_dns_setup lists. Its selectors are random tokens, so they cannot collide
# with Resend's `resend._domainkey` TXT. Every send from the identity goes
# through the configuration set even if a request forgets to name it.
resource "aws_sesv2_email_identity" "sending" {
  for_each = local.ses_identities

  email_identity         = each.value
  configuration_set_name = aws_sesv2_configuration_set.main.configuration_set_name

  dkim_signing_attributes {
    next_signing_key_length = "RSA_2048_BIT"
  }

  tags = local.common_labels
}

# USE_DEFAULT_VALUE: if the MX record goes missing, SES falls back to its own
# amazonses.com return path rather than refusing to send. DMARC still passes
# on the aligned DKIM signature.
resource "aws_sesv2_email_identity_mail_from_attributes" "sending" {
  for_each = local.ses_identities

  email_identity         = aws_sesv2_email_identity.sending[each.key].email_identity
  mail_from_domain       = "${var.ses_mail_from_label}.${each.value}"
  behavior_on_mx_failure = "USE_DEFAULT_VALUE"
}

# Open and click tracking stay off. SES only rewrites links or adds a pixel
# through an event destination tracking OPEN or CLICK, or through Virtual
# Deliverability Manager's engagement tracking; there is no event destination
# here, and engagement metrics are disabled on the set so turning VDM on at
# the account level does not switch tracking on behind this file's back. The
# mail is plain text anyway.
resource "aws_sesv2_configuration_set" "main" {
  configuration_set_name = local.ses_configuration_set_name

  reputation_options {
    reputation_metrics_enabled = true
  }

  sending_options {
    sending_enabled = true
  }

  vdm_options {
    dashboard_options {
      engagement_metrics = "DISABLED"
    }
  }

  tags = local.common_labels
}

# SES stops mailing an address after a hard bounce or a complaint, account
# wide. There is no event pipeline into the database (ADR 0077), as there is
# none for Resend.
resource "aws_sesv2_account_suppression_attributes" "main" {
  suppressed_reasons = ["BOUNCE", "COMPLAINT"]
}

# ---------------------------------------------------------------------------
# The role the API assumes (ADR 0077).
#
# The API on Cloud Run holds no AWS key. It asks the metadata server for a
# Google-signed ID token for its own service account, audience
# local.ses_web_identity_audience, and trades it at STS for this role.
#
# AWS maps Google's claims to condition keys as follows (IAM User Guide,
# "Available keys for AWS web identity federation"):
#   accounts.google.com:sub   the token's sub, the service account unique ID
#   accounts.google.com:aud   the token's azp when present (for a service
#                             account token, also its unique ID), else aud
#   accounts.google.com:oaud  the token's aud, the requested audience
# All three are pinned. The unique ID is read from the service account
# resource, never copied, so the two cannot drift apart: a recreated service
# account gets a new ID and this trust follows it on the next apply.
# ---------------------------------------------------------------------------

data "aws_iam_policy_document" "ses_sender_trust" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = ["accounts.google.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "accounts.google.com:sub"
      values   = [google_service_account.api.unique_id]
    }

    condition {
      test     = "StringEquals"
      variable = "accounts.google.com:aud"
      values   = [google_service_account.api.unique_id]
    }

    condition {
      test     = "StringEquals"
      variable = "accounts.google.com:oaud"
      values   = [local.ses_web_identity_audience]
    }
  }
}

# Sending only: ses:SendEmail on the two identities and the configuration set,
# which SES v2 authorizes as separate resources on every send. Nothing else in
# the account is reachable through this role.
data "aws_iam_policy_document" "ses_sender" {
  statement {
    effect  = "Allow"
    actions = ["ses:SendEmail"]

    resources = concat(
      [for identity in aws_sesv2_email_identity.sending : identity.arn],
      [aws_sesv2_configuration_set.main.arn],
    )

    condition {
      test     = "StringEquals"
      variable = "ses:ApiVersion"
      values   = ["2"]
    }
  }
}

resource "aws_iam_role" "ses_sender" {
  name                 = "${var.environment}-ticket-pos-api-ses"
  description          = "Assumed by the ${var.environment} API Cloud Run service account through Workload Identity Federation to send email through SES (ADR 0077)."
  assume_role_policy   = data.aws_iam_policy_document.ses_sender_trust.json
  max_session_duration = 3600

  tags = local.common_labels
}

resource "aws_iam_role_policy" "ses_sender" {
  name   = "send-email"
  role   = aws_iam_role.ses_sender.id
  policy = data.aws_iam_policy_document.ses_sender.json
}

# A leaked role or a runaway loop shows up as cost. The member account holds
# nothing but SES, so its whole bill is the signal: an alert at 80% of the
# limit spent, and another when the month is forecast to pass it.
resource "aws_budgets_budget" "ses" {
  name         = "${var.environment}-ticket-pos-ses-monthly"
  budget_type  = "COST"
  limit_amount = var.aws_budget_limit_usd
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 80
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [var.aws_budget_alert_email]
  }

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "FORECASTED"
    subscriber_email_addresses = [var.aws_budget_alert_email]
  }

  tags = local.common_labels
}
