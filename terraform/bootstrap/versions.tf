# Bootstrap runs with LOCAL state on purpose: it creates the bucket that every
# other configuration stores its state in, so it cannot store its own state
# there. See terraform/README.md.
terraform {
  required_version = "~> 1.13"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}
