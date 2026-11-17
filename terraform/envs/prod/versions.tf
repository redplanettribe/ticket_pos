terraform {
  required_version = "~> 1.13"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }

    # Amazon SES in its dedicated member account (ADR 0077).
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }

    # Generates the database password. Its state is the password, which is why
    # the state bucket is private and versioned.
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }

    # Sentry, the community provider (ADR 0079, #777). Pinned to the minor it
    # was checked against: a 0.x minor may change resource arguments.
    sentry = {
      source  = "jianyuan/sentry"
      version = "~> 0.16.0"
    }
  }

  # Backend configuration cannot use variables or expressions, so the bucket
  # name is repeated literally here. It is created by terraform/bootstrap.
  backend "gcs" {
    bucket = "multiticketing-tfstate"
    prefix = "envs/prod"
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

# The SES member account of the AWS Organization (ADR 0077), reached through an
# IAM Identity Center profile on the operator's machine (`aws sso login
# --profile <aws_profile>` before a plan). The Organization and the account are
# created by hand, never here. The module tags every AWS resource with its
# common labels. State stays in the GCS backend above.
provider "aws" {
  profile = var.aws_profile
  region  = var.aws_region
}

# Sentry, for the ticket-pos team and its projects in the existing
# redplanettribe organization (US region, sentry.io's default API URL). The token
# is the operator's (variables.tf, sentry_auth_token); nothing about it is
# committed. The organization is shared with another product, and the module
# touches only its own team and three projects.
provider "sentry" {
  token = var.sentry_auth_token
}
