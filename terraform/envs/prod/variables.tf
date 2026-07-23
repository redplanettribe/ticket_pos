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
