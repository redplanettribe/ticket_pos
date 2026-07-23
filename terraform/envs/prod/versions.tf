terraform {
  required_version = "~> 1.13"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
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
