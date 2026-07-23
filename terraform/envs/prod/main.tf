# Production is a thin root over the shared module: what is prod-specific lives
# here (project, region, state prefix), everything else lives in the module.
# Adding staging later is a sibling directory, not a refactor.
module "ticket_pos" {
  source = "../../modules/ticket-pos"

  project_id  = var.project_id
  region      = var.region
  environment = "prod"

  # The Staff app's production origin. Browsers PUT cover images and logos
  # straight to the bucket with presigned URLs, so this is the origin the
  # bucket's CORS policy has to accept. It is a prod fact, hence it lives here.
  storage_cors_origins = [var.staff_origin]
}
