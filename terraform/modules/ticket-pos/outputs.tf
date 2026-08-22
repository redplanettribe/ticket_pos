output "artifact_registry_repository_id" {
  description = "Short name of the Docker repository."
  value       = google_artifact_registry_repository.images.repository_id
}

output "artifact_registry_host" {
  description = "Registry host to authenticate Docker against, e.g. `us-east1-docker.pkg.dev`."
  value       = "${var.region}-docker.pkg.dev"
}

output "image_repository_url" {
  description = "Prefix for image tags: append `/<image>:<tag>` when pushing or deploying."
  value       = local.image_repository_url
}

# No secret value is an output. Outputs are stored in state and printed by
# `terraform output` with no redaction beyond a `sensitive` marker, so the
# credentials this module generates are published only through Secret Manager.
# What follows are the names and addresses needed to *find* them.

output "network_id" {
  description = "Self link of the VPC."
  value       = google_compute_network.main.id
}

output "cloud_run_subnet_id" {
  description = "Self link of the subnet Cloud Run attaches to for direct VPC egress."
  value       = google_compute_subnetwork.cloud_run.id
}

output "database_instance_name" {
  description = "Cloud SQL instance name."
  value       = google_sql_database_instance.postgres.name
}

output "database_connection_name" {
  description = "`project:region:instance`, the address the Cloud SQL connectors and proxy take."
  value       = google_sql_database_instance.postgres.connection_name
}

output "database_private_ip" {
  description = "Private IP of the instance. Reachable only from inside the peered VPC."
  value       = google_sql_database_instance.postgres.private_ip_address
}

output "database_name" {
  description = "Application database on the instance."
  value       = google_sql_database.app.name
}

output "database_user" {
  description = "Application database user. Its password is in the Secret Manager secret named below."
  value       = google_sql_user.app.name
}

output "database_url_secret_id" {
  description = "Full resource name of the Secret Manager secret holding the whole DATABASE_URL the API and the migrate Job mount."
  value       = google_secret_manager_secret.database_url.id
}

output "database_password_secret_id" {
  description = "Full resource name of the Secret Manager secret holding the database password."
  value       = google_secret_manager_secret.database_password.id
}

output "storage_bucket_name" {
  description = "Public media bucket. Value of the API's S3_BUCKET."
  value       = google_storage_bucket.media.name
}

output "storage_endpoint" {
  description = "S3-compatible endpoint for GCS. Value of the API's S3_ENDPOINT and S3_PUBLIC_URL."
  value       = local.storage_endpoint
}

output "storage_service_account_email" {
  description = "Service account the HMAC key belongs to."
  value       = google_service_account.storage.email
}

output "storage_access_key_secret_id" {
  description = "Full resource name of the Secret Manager secret holding the HMAC access ID."
  value       = google_secret_manager_secret.storage_access_key.id
}

output "storage_secret_key_secret_id" {
  description = "Full resource name of the Secret Manager secret holding the HMAC secret key."
  value       = google_secret_manager_secret.storage_secret_key.id
}

# --- Cloud Run ----------------------------------------------------------------

output "api_service_name" {
  description = "Cloud Run service name for the API. What `gcloud run deploy` and `gcloud run services describe` take."
  value       = google_cloud_run_v2_service.api.name
}

output "api_service_url" {
  description = "HTTPS URL of the API service. It rejects every request that does not carry a Google-signed OIDC ID token from a principal holding roles/run.invoker on it (ADR 0008); granting that role is #48."
  value       = google_cloud_run_v2_service.api.uri
}

output "api_service_account_email" {
  description = "Dedicated runtime identity of the API service. Holds Cloud SQL client, objectAdmin on the media bucket, and secretAccessor on its own three secrets — nothing else."
  value       = google_service_account.api.email
}

output "api_image" {
  description = "Image reference the service and Job were created with. Push here before the first apply; afterwards Terraform no longer owns which image runs."
  value       = local.api_image
}

output "migrate_job_name" {
  description = "Cloud Run Job name. `gcloud run jobs execute <name> --region <region> --wait` applies the schema."
  value       = google_cloud_run_v2_job.migrate.name
}

output "migrate_service_account_email" {
  description = "Dedicated runtime identity of the migrate Job. Reads only the database URL secret."
  value       = google_service_account.migrate.email
}

output "staff_service_url" {
  description = "Staff's run.app URL. Works immediately; the custom domain does not until DNS resolves."
  value       = google_cloud_run_v2_service.staff.uri
}

output "storefront_service_url" {
  description = "Storefront's run.app URL."
  value       = google_cloud_run_v2_service.storefront.uri
}

output "staff_service_account_email" {
  description = "Staff's runtime identity. One of exactly three principals holding run.invoker on the API."
  value       = google_service_account.staff.email
}

output "storefront_service_account_email" {
  description = "Storefront's runtime identity."
  value       = google_service_account.storefront.email
}

# --- CI deploy (Workload Identity Federation) ---------------------------------

output "deploy_service_account_email" {
  description = "Identity GitHub Actions impersonates via WIF. Holds artifactregistry.writer on the images repo, run.developer on the three services and the migrate Job, and serviceAccountUser on the four runtime SAs — nothing else. Set as `service_account` in google-github-actions/auth."
  value       = google_service_account.deploy.email
}

output "workload_identity_provider" {
  description = "Full resource name of the GitHub OIDC provider. Set as `workload_identity_provider` in google-github-actions/auth. Impersonation is restricted to var.github_repository by the provider's attribute condition and the SA's principalSet binding."
  value       = google_iam_workload_identity_pool_provider.github.name
}

# The whole point of not delegating DNS to Cloud DNS: these have to be created by
# hand at Namecheap, so Terraform prints them rather than leaving an operator to
# find them in the console. Empty until the mappings report their resource
# records, which is immediately after create.
output "dns_records" {
  description = "DNS records to create at the registrar for the custom domains. Each entry is the record set Cloud Run expects for that hostname."
  value = {
    for k, m in merge(
      length(google_cloud_run_domain_mapping.staff) > 0 ? { (var.staff_domain) = google_cloud_run_domain_mapping.staff[0] } : {},
      length(google_cloud_run_domain_mapping.storefront) > 0 ? { (var.storefront_domain) = google_cloud_run_domain_mapping.storefront[0] } : {},
      ) : k => [
      for r in m.status[0].resource_records : {
        type   = r.type
        name   = r.name
        rrdata = r.rrdata
      }
    ]
  }
}

# --- Reversal Reconciler ------------------------------------------------------

output "reversal_reconciler_job_name" {
  description = "Cloud Scheduler job driving the reversal drain. The name `gcloud scheduler jobs pause|resume|run <name> --location <region>` takes — the fastest way to stop the tick mid-incident, ahead of an apply."
  value       = google_cloud_scheduler_job.reversal_reconciler.name
}

output "reversal_reconciler_service_account_email" {
  description = "Identity Cloud Scheduler presents to the API. Holds run.invoker on the API service and nothing else."
  value       = google_service_account.reversal_reconciler.email
}

# --- Follow Digest ------------------------------------------------------------

output "follow_digest_enqueue_job_name" {
  description = "Cloud Scheduler job declaring the Follow Digest week. The name `gcloud scheduler jobs pause|resume|run <name> --location <region>` takes — the fastest way to stop the weekly send mid-incident, ahead of an apply, and the way to send this week's Digests a day late once one is fixed."
  value       = google_cloud_scheduler_job.follow_digest_enqueue.name
}

output "follow_digest_drain_job_name" {
  description = "Cloud Scheduler job pacing the Follow Digest send. Pausing it holds the week's Digests in the queue rather than losing them; resuming it sends whatever is still owed."
  value       = google_cloud_scheduler_job.follow_digest_drain.name
}

output "follow_digest_service_account_emails" {
  description = "The two identities Cloud Scheduler presents to the API for the Digest, one per job. Each holds run.invoker on the API service and nothing else, so either can be revoked without touching the other."
  value = {
    enqueue = google_service_account.follow_digest_enqueue.email
    drain   = google_service_account.follow_digest_drain.email
  }
}

# --- Abandoned Answer Purge ---------------------------------------------------

output "answer_purge_job_name" {
  description = "Cloud Scheduler job driving the Abandoned Answer Purge. The name `gcloud scheduler jobs pause|resume|run <name> --location <region>` takes. It matters more here than for the jobs above: this is the only scheduled job that deletes anything, so pausing is the first move on any suspicion about it and the deleted rows are not coming back."
  value       = google_cloud_scheduler_job.answer_purge.name
}

output "answer_purge_service_account_email" {
  description = "Identity Cloud Scheduler presents to the API for the purge. Holds run.invoker on the API service and nothing else, and is its own account so an unexplained purge in the audit log can be traced to a caller."
  value       = google_service_account.answer_purge.email
}

# The DNS work Terraform cannot do. Printed rather than applied because the zone
# is not ours to write to from here (ADR 0009); a human adds these at Namecheap
# and then presses Verify in Resend. The exact DKIM selector and value are
# issued by Resend when the domain is added and are not knowable here.
#
# Two checklists rather than one with caveats: a shared-domain deployment has NO
# DNS work to do, and the surest way to get the separate-domain steps followed by
# somebody they do not apply to is to print them with a note on top.
locals {
  digest_email_dns_setup_shared = <<-EOT
    Follow Digest sending domain: ${local.digest_email_domain} (SHARED with transactional mail)

    There is no DNS work to do. This deployment sends Digests from the domain
    that already carries the transactional mail, so the DKIM, SPF and DMARC
    records it needs are the ones already verified in Resend.

    What you accepted by setting digest_email_allow_shared_domain:
      Spam complaints about the Digest now bear on the reputation of the domain
      that delivers One-time Passcodes. Enough of them degrades the deliverability
      of the mail people sign in with. ADR 0030 separates the two for this reason;
      a plan that verifies one domain cannot, and shipping the feature dark was
      judged worse at these volumes.

    1. Nothing to add at Namecheap. Nothing to verify in Resend.
    2. Leave TF_VAR_digest_resend_api_key unset unless you want a separate
       restricted key: one domain is one Resend domain object, the keys are
       account-scoped, and the API falls back to the transactional key here. A
       second copy of one credential is a rotation hazard, not isolation.
    3. Apply. The API logs "digest email sender: resend" at startup when it is
       satisfied, and "digest email sender: not configured" with a reason when
       it is not.

    To undo this later: verify ${var.digest_email_domain} in Resend, follow the
    separate-domain checklist this output prints when the flag is false, then set
    the flag back to false.
  EOT

  digest_email_dns_setup_separate = <<-EOT
    Follow Digest sending domain: ${var.digest_email_domain}

    None of the following is provisioned by Terraform. Until it is done by hand,
    the Digest sending identity does not exist and Resend rejects Digest sends.
    If your Resend plan verifies only ONE domain, none of this is possible: set
    digest_email_allow_shared_domain instead and re-read this output.

    1. Add ${var.digest_email_domain} as a domain in Resend. This is a SECOND
       domain, alongside the transactional one; do not reuse it.
    2. At Namecheap, on the ${var.digest_email_domain} subdomain, add the records
       Resend issues for it:
         - the DKIM TXT record (selector and value are given by Resend)
         - the SPF TXT record Resend states for the domain
         - an MX record for the return path, if Resend asks for one
         - _dmarc.${var.digest_email_domain} TXT "v=DMARC1; p=none;"
       These are SEPARATE records from the transactional domain's. Separating
       the reputations is the entire point (ADR 0030) — do not CNAME one to the
       other.
    3. Press Verify in Resend and wait for the domain to read verified.
    4. Create an API key in Resend scoped to this domain and supply it as
       TF_VAR_digest_resend_api_key. It must NOT be the transactional key.
    5. Apply. The API logs "digest email sender: resend" at startup when it is
       satisfied, and "digest email sender: not configured" with a reason when
       it is not.
  EOT
}

output "digest_email_dns_setup" {
  description = "Operator checklist for the Follow Digest's sending domain. Terraform does NOT create any of this."
  value = (
    var.digest_email_allow_shared_domain
    ? local.digest_email_dns_setup_shared
    : local.digest_email_dns_setup_separate
  )
}
