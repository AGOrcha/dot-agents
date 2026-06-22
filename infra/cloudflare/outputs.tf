output "audTag" {
  description = <<-EOT
    The application's audience (AUD) tag. dm5's Worker verifies the CF Access JWT
    `aud` claim against this value; paste it into the Worker's config/secret.
  EOT
  value       = cloudflare_zero_trust_access_application.agorcha_internal_docs.aud
}

output "service_token_client_id" {
  description = "Client ID for the agorcha-agents service token (sent as Cf-Access-Client-Id)."
  value       = cloudflare_zero_trust_access_service_token.agorcha_agents.client_id
}

output "service_token_client_secret" {
  description = "Client Secret for the agorcha-agents service token (sent as Cf-Access-Client-Secret)."
  value       = cloudflare_zero_trust_access_service_token.agorcha_agents.client_secret
  sensitive   = true
}
