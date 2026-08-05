# Transactional email via Resend (ADR 0009).
#
# The API selects Resend over the logging sender purely on the presence of
# RESEND_API_KEY. This file is written so that applying it BEFORE a key exists is
# a no-op on behaviour: the secret container is created empty, no version is
# written, the env var is not mounted, and the service keeps logging OTP codes to
# Cloud Logging — exactly today's behaviour. Supplying the key (see below) is what
# switches delivery on, with no further Terraform change to the service.

variable "resend_api_key" {
  description = "Resend API key. Leave empty to keep the API on the logging email sender; supply it (via TF_VAR_resend_api_key, never a committed tfvars) to switch on real delivery."
  type        = string
  default     = ""
  sensitive   = true
}

variable "email_from" {
  description = "RFC 5322 From header for outbound mail. The address must live on a domain verified in Resend."
  type        = string
  default     = "Multiticketing <noreply@send.multiticketing.com>"
}

# The secret container always exists, so the accessor grant and the (future)
# version have a stable target. An empty container holds no versions and costs
# nothing.
resource "google_secret_manager_secret" "resend_api_key" {
  project   = var.project_id
  secret_id = "${var.environment}-ticket-pos-resend-api-key"

  labels = local.common_labels

  replication {
    auto {}
  }
}

# The value is written only when a key is supplied. Until then there is no
# version, and the service's secret env below is not mounted.
resource "google_secret_manager_secret_version" "resend_api_key" {
  count = var.resend_api_key == "" ? 0 : 1

  secret      = google_secret_manager_secret.resend_api_key.id
  secret_data = var.resend_api_key
}

# The API's runtime identity may read this one secret. Granted unconditionally so
# the grant is not entangled with whether a version exists yet.
resource "google_secret_manager_secret_iam_member" "api_resend_api_key" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.resend_api_key.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.api.email}"
}

# ---------------------------------------------------------------------------
# The Follow Digest's own sending identity (#225, ADR 0030).
#
# The Digest is the platform's first NON-TRANSACTIONAL mail. Marketing mail
# attracts spam complaints in a way transactional mail never does, and complaint
# rates degrade domain reputation — so a Digest sharing send.multiticketing.com
# could impair delivery of the One-time Passcodes people sign in with. A
# discovery feature would be taking down authentication.
#
# Everything below is a SIBLING of the transactional settings above, never a
# derivative: a separate variable, a separate Secret Manager secret, a separate
# env var. The API requires BOTH halves before it will send a single Digest and
# has no default for either, so a half-finished setup here sends no marketing
# mail rather than sending it from the transactional domain. Applying this file
# before either value exists is a no-op on behaviour, exactly as it is above.
#
# WHAT THIS FILE DOES NOT DO — read this before assuming the domain exists.
# It does not create the Resend domain object and it does not create DNS
# records, for neither sender. ADR 0009 records why: the zone is at Namecheap
# and DNS is deliberately not delegated to Cloud DNS (ADR 0007), so DKIM, SPF
# and DMARC are added by hand. The digest_email_dns_setup output below is the
# operator's checklist for doing that; nothing in Terraform verifies it was
# done. A Digest sent from an unverified domain is rejected by Resend.
# ---------------------------------------------------------------------------

variable "digest_email_domain" {
  description = "Sending subdomain for the Follow Digest. MUST differ from the transactional sender's domain — the API refuses to send Digests if the two share a domain. Not created by Terraform: it is registered in Resend and its DNS records added at Namecheap by hand (see the digest_email_dns_setup output)."
  type        = string
  default     = "digest.multiticketing.com"
}

variable "digest_resend_api_key" {
  description = "Resend API key for the Follow Digest's sending domain, separate from resend_api_key. Leave empty and no Follow Digests are sent at all; supply it (via TF_VAR_digest_resend_api_key, never a committed tfvars) together with digest_email_from to switch the Digest on."
  type        = string
  default     = ""
  sensitive   = true
}

variable "digest_email_from" {
  description = "RFC 5322 From header for the Follow Digest. Its address must live on digest_email_domain and must NOT be on the transactional sending domain. Empty means no Digests are sent."
  type        = string
  default     = ""
}

# Set the From from the digest domain when the operator did not state one
# outright, so the two cannot drift apart by a typo. There is deliberately no
# fallback to var.email_from anywhere in this file: borrowing the transactional
# identity is the one outcome this whole feature exists to prevent.
locals {
  digest_email_from = var.digest_email_from != "" ? var.digest_email_from : "Multiticketing <digest@${var.digest_email_domain}>"
}

# The container always exists, on the same terms as the transactional one: a
# stable target for the accessor grant, no versions and no cost until a key is
# supplied.
resource "google_secret_manager_secret" "digest_resend_api_key" {
  project   = var.project_id
  secret_id = "${var.environment}-ticket-pos-digest-resend-api-key"

  labels = local.common_labels

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "digest_resend_api_key" {
  count = var.digest_resend_api_key == "" ? 0 : 1

  secret      = google_secret_manager_secret.digest_resend_api_key.id
  secret_data = var.digest_resend_api_key
}

resource "google_secret_manager_secret_iam_member" "api_digest_resend_api_key" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.digest_resend_api_key.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.api.email}"
}

# The DNS work Terraform cannot do. Printed rather than applied because the zone
# is not ours to write to from here (ADR 0009); a human adds these at Namecheap
# and then presses Verify in Resend. The exact DKIM selector and value are
# issued by Resend when the domain is added and are not knowable here.
output "digest_email_dns_setup" {
  description = "Operator checklist for the Follow Digest's sending domain. Terraform does NOT create any of this."
  value       = <<-EOT
    Follow Digest sending domain: ${var.digest_email_domain}

    None of the following is provisioned by Terraform. Until it is done by hand,
    the Digest sending identity does not exist and Resend rejects Digest sends.

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
