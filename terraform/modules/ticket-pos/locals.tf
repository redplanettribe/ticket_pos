locals {
  # Labels every resource in the environment carries, so a stray resource can be
  # traced back to this module and environment from the console.
  common_labels = {
    managed-by  = "terraform"
    application = "ticket-pos"
    environment = var.environment
  }
}
