# Organization Logos and Event cover images.
#
# READ THIS BEFORE PUTTING ANYTHING IN THIS BUCKET: every object is readable by
# anyone on the internet holding the URL, forever. The `allUsers` grant below is
# bucket-wide and cannot be narrowed per object under uniform access. Only the
# `covers/`, `logos/`, and `avatars/` prefixes belong here. Anything that must
# not be public needs a different bucket and signed downloads.
resource "google_storage_bucket" "media" {
  project  = var.project_id
  name     = local.storage_bucket_name
  location = upper(var.region)

  storage_class = "STANDARD"

  # Uniform access: IAM only, no per-object ACLs. Public read is granted once,
  # below, instead of being an attribute of each uploaded object.
  uniform_bucket_level_access = true

  # Not "enforced" — that would refuse the `allUsers` binding this bucket needs.
  # "inherited" takes the organization policy's answer, which permits it.
  public_access_prevention = "inherited"

  # User-uploaded images are not reproducible from anything in the repository.
  force_destroy = false

  # The application uploads through presigned PUT URLs, which means the *browser*
  # makes the request, cross-origin, from the Staff app. A cross-origin PUT is
  # preflighted, and a bucket with no CORS configuration answers the preflight
  # with a failure the browser reports only as an opaque network error. MinIO is
  # permissive by default, so an upload path that works locally breaks here.
  cors {
    origin = var.storage_cors_origins
    method = ["GET", "HEAD", "PUT", "OPTIONS"]

    # Headers the browser is allowed to send on the upload. Content-Type is not
    # optional: the presigned URL signs it (see PresignPut in
    # backend/internal/platform/storage/s3.go), so the browser must send it, and
    # a header the CORS policy does not list is a header the preflight rejects.
    response_header = [
      "Content-Type",
      "Content-Length",
      "Content-MD5",
      "ETag",
      "x-amz-content-sha256",
      "x-amz-date",
    ]

    max_age_seconds = var.storage_cors_max_age_seconds
  }

  labels = local.common_labels

  lifecycle {
    prevent_destroy = true
  }
}

# Anonymous read on objects, matching how the application serves images today:
# PublicURL hands out a plain bucket URL and the browser fetches it with no
# credentials. objectViewer, not legacyObjectReader or a bucket-level role —
# it grants reading objects and nothing about the bucket itself, so the object
# listing stays private even though every object is public.
resource "google_storage_bucket_iam_member" "public_read" {
  bucket = google_storage_bucket.media.name
  role   = "roles/storage.objectViewer"
  member = "allUsers"
}

# The identity the HMAC key belongs to. HMAC keys are issued *to* a service
# account, so one is needed even though the application authenticates with the
# key rather than with the account.
resource "google_service_account" "storage" {
  project      = var.project_id
  account_id   = "${var.environment}-ticket-pos-storage"
  display_name = "Ticket POS ${var.environment} object storage"
  description  = "Owner of the HMAC key the API uses for S3-compatible access to ${local.storage_bucket_name}"
}

# Scoped to this bucket, not the project. objectAdmin covers the four things the
# storage code does: presign puts, read, delete, and list a prefix when replacing
# a cover image.
resource "google_storage_bucket_iam_member" "app_object_admin" {
  bucket = google_storage_bucket.media.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${google_service_account.storage.email}"
}

# The static credential ADR 0007 accepted as the price of keeping GCS and MinIO
# on one code path: the API talks S3 to storage.googleapis.com rather than using
# the native client with workload identity.
#
# Rotation is manual and deliberately so: taint this key, apply to mint a new
# one, redeploy the API, then delete the old key. Automating that needs a
# two-key overlap the application cannot express with a single credential pair.
resource "google_storage_hmac_key" "app" {
  project               = var.project_id
  service_account_email = google_service_account.storage.email
  state                 = "ACTIVE"
}
