# Production is a thin root over the shared module: what is prod-specific lives
# here (project, region, state prefix), everything else lives in the module.
# Adding staging later is a sibling directory, not a refactor.
module "ticket_pos" {
  source = "../../modules/ticket-pos"

  project_id  = var.project_id
  region      = var.region
  environment = "prod"
}
