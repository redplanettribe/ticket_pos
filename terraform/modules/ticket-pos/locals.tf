locals {
  # Labels every resource in the environment carries, so a stray resource can be
  # traced back to this module and environment from the console.
  common_labels = {
    managed-by  = "terraform"
    application = "ticket-pos"
    environment = var.environment
  }

  # Bucket names are globally unique across all of GCS, so the project id is the
  # cheapest available disambiguator.
  storage_bucket_name = coalesce(var.storage_bucket_name, "${var.project_id}-ticket-pos-${var.environment}")

  # No percent-encoding: random_password.database restricts the password to
  # URL-unreserved punctuation precisely so this interpolation is safe.
  database_url = format(
    "postgres://%s:%s@%s:5432/%s?sslmode=require",
    google_sql_user.app.name,
    random_password.database.result,
    google_sql_database_instance.postgres.private_ip_address,
    google_sql_database.app.name,
  )

  # Where images are pushed and pulled from. Same prefix the pipeline (#49) and
  # the manual deploy sequence in terraform/README.md use.
  image_repository_url = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.images.repository_id}"

  api_image = "${local.image_repository_url}/${var.api_image_name}:${var.api_image_tag}"

  # The bucket lives in var.region (storage.tf uses upper(var.region) as its
  # location), so that is what presigned URLs must be signed for unless a caller
  # deliberately overrides it.
  storage_s3_region = coalesce(var.storage_s3_region, var.region)

  # GCS's S3-compatible endpoint, used both as the signing endpoint and as the
  # prefix of the public object URLs the API hands the browser (PublicURL in
  # backend/internal/platform/storage/s3.go appends `/<bucket>/<key>`).
  storage_endpoint = "https://storage.googleapis.com"
}
