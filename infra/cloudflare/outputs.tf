output "audTag" {
  description = <<-EOT
    The application's audience (AUD) tag. dm5's Worker verifies the CF Access JWT
    `aud` claim against this value; paste it into the Worker's config/secret.
  EOT
  value       = cloudflare_zero_trust_access_application.agorcha_internal_docs.aud
}

output "account_id" {
  description = <<-EOT
    Cloudflare account ID owning the Zero Trust org. Echoed for the dm6 provision
    endpoint, which mints per-developer service tokens at this account scope via the
    CF API (POST /accounts/{account_id}/access/service_tokens).
  EOT
  value       = var.account_id
}

output "zone_id" {
  description = <<-EOT
    Cloudflare zone ID for agorcha.dev. Echoed for the dm6 provision endpoint so it
    can target the Access app's zone when minting per-developer service tokens.
  EOT
  value       = var.zone_id
}

output "obs_audTag" {
  description = "obs.agorcha.dev Access app AUD tag; the obs Worker verifies the CF Access JWT `aud` against it."
  value       = cloudflare_zero_trust_access_application.agorcha_obs.aud
}

output "obs_service_token_client_id" {
  description = "Cf-Access-Client-Id for the bound obs CLI service token (not a secret)."
  value       = cloudflare_zero_trust_access_service_token.obs.client_id
}

output "obs_service_token_client_secret" {
  description = "Cf-Access-Client-Secret for the bound obs token. Store in ~/.config/da/credentials.json (0600), NEVER the repo."
  value       = cloudflare_zero_trust_access_service_token.obs.client_secret
  sensitive   = true
}
