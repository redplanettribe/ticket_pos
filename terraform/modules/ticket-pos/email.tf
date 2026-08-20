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
# Everything below is a SIBLING of the transactional settings above rather than a
# derivative: a separate variable, a separate Secret Manager secret, a separate
# env var. The API requires BOTH halves before it will send a single Digest and
# has no default for either, so a half-finished setup here sends no marketing
# mail rather than sending it from the transactional domain. Applying this file
# before either value exists is a no-op on behaviour, exactly as it is above.
#
# ONE deployment shape may collapse the two onto a single domain, and only by
# saying so: digest_email_allow_shared_domain, below. A provider plan that
# verifies a single domain makes the separation a billing question rather than a
# setting, and the choice there is between sharing the domain and shipping no
# Digests at all. Production took that branch for two weeks in August 2026 and
# has since left it. Nothing infers the sharing; an undeclared collision is still
# refused.
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
  description = "Sending subdomain for the Follow Digest when it has one of its own. Ignored when digest_email_allow_shared_domain is true. Not created by Terraform: it is registered in Resend and its DNS records added at Namecheap by hand (see the digest_email_dns_setup output)."
  type        = string
  default     = "digest.multiticketing.com"
}

# The escape hatch for a deployment that cannot HAVE a second sending domain.
#
# A plan that verifies exactly one domain makes the separation ADR 0030 asks for
# unreachable by configuration — it is a paid plan, not a setting. The reasoning
# behind the separation does not stop being true; such a deployment simply cannot
# act on it, and the alternative to sharing is shipping the feature dark. Sharing
# is the lesser cost at the volumes those plans permit, where the complaint rates
# that damage a domain's reputation need a sending rate the plan does not allow.
# Set it back to false the day a second domain is affordable — production did
# exactly that on 2026-08-20, having shared the domain since 2026-08-05.
#
# It is deliberately a stated choice and never inferred from the two domains
# matching, because a typo in digest_email_from looks exactly the same and the
# API should keep refusing that one.
variable "digest_email_allow_shared_domain" {
  description = "Send Follow Digests from the transactional sending domain instead of a separate one. Accepts the reputation coupling ADR 0030 avoids: spam complaints on marketing mail then bear on the domain delivering One-time Passcodes. Intended for plans that verify only one domain."
  type        = bool
  default     = false
}

variable "digest_resend_api_key" {
  description = "Resend API key for the Follow Digest's sending domain, separate from resend_api_key. Leave empty and no Follow Digests are sent — UNLESS digest_email_allow_shared_domain is true, in which case the API falls back to the transactional key, the two identities being on one Resend domain by then anyway. Supply via TF_VAR_digest_resend_api_key, never a committed tfvars."
  type        = string
  default     = ""
  sensitive   = true
}

variable "digest_email_from" {
  description = "RFC 5322 From header for the Follow Digest. Empty derives it from the effective digest domain. It must not be on the transactional sending domain unless digest_email_allow_shared_domain says so."
  type        = string
  default     = ""
}

# Set the From from the digest domain when the operator did not state one
# outright, so the two cannot drift apart by a typo.
#
# The transactional domain is read out of var.email_from ONLY to build the
# shared-domain case, and `can` keeps a From this expression cannot parse from
# failing a plan that never needed the value: unparseable falls back to the
# separate domain, which is the safe direction to fall.
locals {
  transactional_email_domain = can(regex("@([^@>[:space:]]+)>?[[:space:]]*$", var.email_from)) ? lower(one(regex("@([^@>[:space:]]+)>?[[:space:]]*$", var.email_from))) : ""

  digest_email_domain = (
    var.digest_email_allow_shared_domain && local.transactional_email_domain != ""
    ? local.transactional_email_domain
    : var.digest_email_domain
  )

  digest_email_from = var.digest_email_from != "" ? var.digest_email_from : "Multiticketing <digest@${local.digest_email_domain}>"
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
