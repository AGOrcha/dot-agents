variable "account_id" {
  description = "Cloudflare account ID that owns the Zero Trust organization."
  type        = string
}

variable "zone_id" {
  description = "Cloudflare zone ID for agorcha.dev (the zone the Access app is anchored to)."
  type        = string
}

variable "maintainer_email" {
  description = "Email allowed by the maintainers policy. Defaults to the project maintainer."
  type        = string
  default     = "nikashprakash1@gmail.com"
}

variable "allowed_idp_ids" {
  description = <<-EOT
    Identity-provider IDs the application offers at login (GitHub + One-time PIN per
    the design contract). These are CF-assigned UUIDs, not display names — look them up
    under Zero Trust -> Settings -> Authentication, or via the Access Identity Providers
    API. Leave empty to inherit all of the team's configured IdPs.
  EOT
  type        = list(string)
  default     = []
}

variable "obs_maintainer_emails" {
  description = "Emails allowed to browse obs.agorcha.dev (single-tenant dot-agents observability)."
  type        = list(string)
  default     = ["nikprakash20@gmail.com", "nikashprakash1@gmail.com"]
}
