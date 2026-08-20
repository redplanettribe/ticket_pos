variable "project_id" {
  description = "GCP project production runs in."
  type        = string
}

variable "region" {
  description = "Region for every regional resource."
  type        = string
}

variable "staff_origin" {
  description = "Public origin the Staff app is served from. The media bucket's CORS policy allows browser uploads from it."
  type        = string
}

variable "storefront_origin" {
  description = "Public origin the Storefront is served from. The media bucket's CORS policy allows browser uploads from it too: a Customer setting a profile photo PUTs to a presigned URL from this origin, not from Staff."
  type        = string
}

variable "staff_domain" {
  description = "Hostname for the Staff console."
  type        = string
  default     = "pos.multiticketing.com"
}

variable "storefront_domain" {
  description = "Hostname for the Storefront."
  type        = string
  default     = "discover.multiticketing.com"
}

variable "github_repository" {
  description = "The single `owner/repo` GitHub Actions deploys from. Scopes the Workload Identity Federation attribute condition and impersonation principalSet to exactly this repository."
  type        = string
  default     = "redplanettribe/ticket_pos"
}

variable "resend_api_key" {
  description = "Resend API key. Set via TF_VAR_resend_api_key from a sourced .env; empty keeps the API on the logging email sender."
  type        = string
  default     = ""
  sensitive   = true
}

variable "email_from" {
  description = "RFC 5322 From header for outbound mail; must be on a Resend-verified domain."
  type        = string
  default     = "Multiticketing <noreply@send.multiticketing.com>"
}

# The Follow Digest's sending identity (#225, ADR 0030). Declared beside the
# transactional pair above and never derived from it: the Digest is marketing
# mail, its spam complaints would degrade whatever domain it sends from, and the
# transactional domain is the one delivering the One-time Passcodes people sign
# in with.
#
# This deployment sends Digests from that separate domain, which is ADR 0030 as
# originally written. It did not always: between 2026-08-05 and 2026-08-20 it
# shared the transactional domain, because the Resend plan of the day verified
# only one and the alternative was shipping the feature dark. The paid plan
# removed that constraint and the exception was retired — see
# digest_email_allow_shared_domain below.

variable "digest_email_domain" {
  description = "Sending subdomain for the Follow Digest. Registered in Resend and DNS-verified at Namecheap by hand; Terraform does not create it."
  type        = string
  default     = "digest.multiticketing.com"
}

# FALSE, matching the module's safe default, because the Resend plan now
# verifies a second domain and digest.multiticketing.com exists.
#
# It was true here for two weeks. Setting it back to true would re-accept what
# ADR 0030 exists to avoid: spam complaints about marketing mail bearing on the
# reputation of the domain that delivers the One-time Passcodes people sign in
# with — a discovery feature able to impair authentication. Do that only to keep
# the Digest sending at all, never as a shortcut around a DNS problem on the
# digest domain. The right response to an unverified digest domain is to fix the
# DNS or pause follow_digest_enqueue_enabled, both of which are recoverable;
# sharing the OTP domain's reputation is not.
variable "digest_email_allow_shared_domain" {
  description = "Send Follow Digests from the transactional domain (send.multiticketing.com) rather than a separate one. False: the Digest has its own verified domain. Setting it true accepts the reputation coupling ADR 0030 avoids."
  type        = bool
  default     = false
}

# REQUIRED now that the domain is not shared. The fallback to resend_api_key
# lives behind digest_email_allow_shared_domain, so with that false an empty key
# here is not "reuse the transactional one" — it is a Digest sender the API
# refuses to build, and every composed Digest then burns its five attempts and is
# abandoned. Supply it in the same apply that flips the flag.
variable "digest_resend_api_key" {
  description = "Resend API key for the Follow Digest's own sending domain, scoped to it and never the transactional key. Required while digest_email_allow_shared_domain is false. Set via TF_VAR_digest_resend_api_key from a sourced .env, never a committed tfvars."
  type        = string
  default     = ""
  sensitive   = true
}

variable "digest_email_from" {
  description = "RFC 5322 From header for the Follow Digest. Empty derives it from digest_email_domain. Never falls back to email_from."
  type        = string
  default     = ""
}

# The four Google Sign-In credentials, from the `staff` and `storefront` OAuth
# clients registered by hand in the Console (#75). Set via TF_VAR_ from a sourced
# .env; empty leaves the feature off with the Google button hidden on both
# surfaces. TF_VAR_ populates root variables only, so they are declared here and
# threaded through in main.tf.

variable "google_staff_client_id" {
  description = "Client ID of the `staff` Google OAuth client."
  type        = string
  default     = ""
}

variable "google_staff_client_secret" {
  description = "Client secret of the `staff` Google OAuth client. Set via TF_VAR_google_staff_client_secret."
  type        = string
  default     = ""
  sensitive   = true
}

variable "google_storefront_client_id" {
  description = "Client ID of the `storefront` Google OAuth client."
  type        = string
  default     = ""
}

variable "google_storefront_client_secret" {
  description = "Client secret of the `storefront` Google OAuth client. Set via TF_VAR_google_storefront_client_secret."
  type        = string
  default     = ""
  sensitive   = true
}

# The PayPhone merchant credentials (ADR 0012) — the platform's single
# production account from the PayPhone Developer portal, never the sandbox one.
# Set via TF_VAR_ from a sourced .env; empty leaves the API on the stub Payment
# Provider, which in production is a dead checkout. Declared here and threaded
# through in main.tf because TF_VAR_ populates root variables only.

variable "payphone_api_token" {
  description = "Bearer token of the platform's PayPhone merchant account. Set via TF_VAR_payphone_api_token."
  type        = string
  default     = ""
  sensitive   = true
}

variable "payphone_store_id" {
  description = "Store id of the platform's PayPhone merchant account. Set via TF_VAR_payphone_store_id."
  type        = string
  default     = ""
}

# The Reversal Reconciler tick (ADR 0024). All three are operational levers
# rather than credentials, so they are ordinary variables with defaults — not
# TF_VAR_ from a sourced .env.

variable "reversal_reconciler_enabled" {
  description = "Whether the reversal drain tick fires in production. Starts false: ADR 0024's rollout is to deploy the endpoint and exercise it by hand for a day, confirming it is a no-op on an empty queue, before a cron drives it. Also the incident switch — set false and apply to stop the tick without deleting the job."
  type        = bool
  default     = false
}

variable "reversal_reconciler_schedule" {
  description = "Unix cron for the production drain tick. Every minute; the module variable of the same name carries why."
  type        = string
  default     = "* * * * *"
}

variable "reversal_reconciler_attempt_deadline_seconds" {
  description = "How long Cloud Scheduler waits for one production drain. Declared here so the knob can be turned during an incident without editing the module. It is one term of a chain that must be read before it is moved; the module variable of the same name says where the chain is written down."
  type        = number
  default     = 90
}

# The Follow Digest jobs (#226, ADR 0030). Operational levers, like the
# reconciler's above, and declared here for the same reason: pausing has to be an
# apply from this directory rather than a module edit or a console click the next
# apply silently undoes.

variable "follow_digest_enqueue_enabled" {
  description = "Whether the weekly Follow Digest enqueue fires in production. STARTS FALSE, and there is a prerequisite beyond confidence in the code: the Digest sends from its own domain (ADR 0030) which must exist at the mail provider with its DNS records published before this is turned on. A deployment without it composes every Digest, refuses every send, and retries until each one is abandoned."
  type        = bool
  default     = true
}

variable "follow_digest_enqueue_schedule" {
  description = "Unix cron for the production weekly enqueue, read in America/Guayaquil. Thursday 09:00; the module variable of the same name carries why."
  type        = string
  default     = "0 9 * * 4"
}

variable "follow_digest_enqueue_attempt_deadline_seconds" {
  description = "How long Cloud Scheduler waits for the production weekly enqueue. The module variable of the same name says what it is bounded by."
  type        = number
  default     = 120
}

variable "follow_digest_drain_enabled" {
  description = "Whether the per-minute Follow Digest drain fires in production. Starts false, and is the switch to reach for during a mail-provider incident: paused, the week's Digests wait in the queue instead of spending their attempts against a provider that is refusing them."
  type        = bool
  default     = true
}

variable "follow_digest_drain_schedule" {
  description = "Unix cron for the production drain tick. Every minute; the module variable of the same name carries why."
  type        = string
  default     = "* * * * *"
}

variable "follow_digest_drain_attempt_deadline_seconds" {
  description = "How long Cloud Scheduler waits for one production Digest drain. It is one term of a chain that must be read before it is moved; the module variable of the same name says where the chain is written down."
  type        = number
  default     = 90
}
