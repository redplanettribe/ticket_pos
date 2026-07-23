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
