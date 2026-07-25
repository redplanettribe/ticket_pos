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
