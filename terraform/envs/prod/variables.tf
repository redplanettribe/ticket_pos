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

# The Sale Invoice Drainer tick (#474, ADR 0060). Operational levers on the
# reconciler's terms.

variable "sale_invoice_drainer_enabled" {
  description = "Whether the Sale Invoice Drainer tick fires in production. Starts false: the job ships paused and is enabled once an Issuer in `production` exists and the drain endpoint has been curled by hand against real House sales. Until then the kick after each paid House checkout still works every document once. Also the incident switch — set false and apply to stop the tick without deleting the job."
  type        = bool
  default     = false
}

variable "sale_invoice_drainer_schedule" {
  description = "Unix cron for the production drain tick. Every five minutes; the module variable of the same name carries why."
  type        = string
  default     = "*/5 * * * *"
}

variable "sale_invoice_drainer_attempt_deadline_seconds" {
  description = "How long Cloud Scheduler waits for one production drain. One term of a chain that must be read before it is moved; the module variable of the same name says where the chain is written down."
  type        = number
  default     = 120
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

variable "answer_purge_enabled" {
  description = "Whether the production Abandoned Answer Purge tick fires (#316, ADR 0044). STARTS FALSE and stays false until ticket_questions_enabled below has been open long enough for Payments to be holding Answers — before that it is a daily DELETE against an empty table. This is the only scheduled job in production that deletes anything: it is also the flag to set false first, and apply second, if the purge is ever suspected of taking more than the 30-day window allows."
  type        = bool
  default     = false
}

variable "answer_purge_schedule" {
  description = "Unix cron for the production purge tick. Daily in the small hours; the module variable of the same name says why it is not per-minute."
  type        = string
  default     = "20 3 * * *"
}

variable "answer_purge_attempt_deadline_seconds" {
  description = "How long Cloud Scheduler waits for one production purge. It must stay below api_request_timeout_seconds; the module variable of the same name says why."
  type        = number
  default     = 120
}

variable "answer_reminder_enabled" {
  description = "Whether the production Answer Reminder sweep tick fires (#317, ADR 0044). STARTS FALSE, and this is the flag that decides whether the platform writes to buyers who did not ask to be written to. It stays false until ticket_questions_enabled below has been open long enough for Ticket Sales to owe Answers, and until somebody is watching the first run: the mail is transactional and carries no unsubscribe, so what bounds it is the backend's rationing — at most one per Ticket Sale per 7 days, at most two ever — and nothing a recipient can do. Set it false and apply first if a reminder is ever suspected of reaching the wrong people or reaching them too often; a mail that has gone cannot be recalled."
  type        = bool
  default     = false
}

variable "answer_reminder_schedule" {
  description = "Unix cron for the production sweep tick. Daily at 10:00 Ecuador time, because this job's firing hour is the hour buyers' mail arrives; the module variable of the same name says why the cadence is not what limits how often anybody is written to."
  type        = string
  default     = "0 10 * * *"
}

variable "answer_reminder_attempt_deadline_seconds" {
  description = "How long Cloud Scheduler waits for one production sweep. It is one term of a chain that must be read before it is moved; the module variable of the same name says where the chain is written down."
  type        = number
  default     = 120
}

variable "assignment_reminder_enabled" {
  description = "Whether the production Assignment Reminder sweep tick fires (#361, #365, ADR 0051). STARTS FALSE and is present in terraform.tfvars as false: merging mails nobody, and its first run is a catch-up that writes to every buyer of more than one Ticket since the platform opened, so turning it on is a reviewable diff made after the candidate count has been reviewed locally (docs/runbook-assignment-reminder.md). Set it false and apply first if a reminder is ever suspected of reaching the wrong people or reaching them too often; a mail that has gone cannot be recalled."
  type        = bool
  default     = false
}

variable "assignment_reminder_schedule" {
  description = "Unix cron for the production Assignment Reminder sweep. Daily at 10:30 Ecuador time, half an hour after the Answer Reminder; the module variable of the same name says why the cadence is not what limits how often anybody is written to."
  type        = string
  default     = "30 10 * * *"
}

variable "assignment_reminder_attempt_deadline_seconds" {
  description = "How long Cloud Scheduler waits for one production Assignment Reminder sweep. One term of a chain that must be read before it is moved; the module variable of the same name says where."
  type        = number
  default     = 120
}

variable "holder_address_purge_enabled" {
  description = "Whether the production Holder Address Purge tick fires (#331, parent #322, ADR 0046). STARTS FALSE and stays false until ticket_assignment_enabled below has been open long enough for Tickets to be carrying holder addresses — before that it is a daily UPDATE matching no rows. This is the second scheduled job in production that deletes anything, and the only one that deletes personal data belonging to somebody who never came to this platform. It is the flag to set false first, and apply second, if the purge is ever suspected of taking more than an unaccepted address at a started Event — the backend does not gate this job on the feature flag, on purpose, so this is the only switch there is. It is equally the flag somebody must remember to set TRUE when assignment opens: leaving it false then is the platform holding third-party contact details with no scheduled end, which is the specific failure ADR 0046 priced this job against."
  type        = bool
  default     = false
}

variable "holder_address_purge_schedule" {
  description = "Unix cron for the production holder address purge tick. Daily in the small hours, twenty minutes after the Abandoned Answer Purge so the two deletions are separate lines in the log; the module variable of the same name says why it is not per-minute and why its timezone changes nothing about which Events qualify."
  type        = string
  default     = "40 3 * * *"
}

variable "holder_address_purge_attempt_deadline_seconds" {
  description = "How long Cloud Scheduler waits for one production holder address purge. It must stay below api_request_timeout_seconds; the module variable of the same name says why."
  type        = number
  default     = 120
}

variable "ticket_questions_enabled" {
  description = "Whether an Organization may define Ticket Questions in the staff app (#309). STARTS FALSE. The prerequisite is a published Policy Version describing this collection (ADR 0045) — flipping it before that puts the platform in the position of deliberately collecting, through a mechanism it built, data its own policy says it does not collect. Read the ADR before changing this."
  type        = bool
  default     = false
}

variable "ticket_assignment_enabled" {
  description = "Whether a buyer may assign one of their own Tickets to an email address (#324, parent #322). STARTS FALSE, and is DELIBERATELY ABSENT FROM terraform.tfvars so that opening it is an addition somebody has to write rather than a value they edit. It is a separate flag from ticket_questions_enabled above and must stay one: killing assignment must not take Ticket Questions dark. The prerequisite is a published Policy Version covering three things ADR 0045's clause does not — the platform mailing an address supplied by a third party, a Customer record minted from that click, and disclosure of the Holder's address to the Organization as a separate controller. Flipping it before that has the platform mailing people who never came here, at an address given by somebody with no authority to give it."
  type        = bool
  default     = false
}
