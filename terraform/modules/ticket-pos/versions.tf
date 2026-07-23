# A module declares which providers it needs but never configures them: the
# root module owns provider configuration. Constraints here are deliberately
# looser than the roots' — the root is what pins the exact version.
terraform {
  required_version = ">= 1.9"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = ">= 6.0, < 7.0"
    }
  }
}
