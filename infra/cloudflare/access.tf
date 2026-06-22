# Cloudflare Access bootstrap for agorcha.dev internal docs.
#
# Contract source: .agents/proposals/agorcha-public-vs-internal-and-obs-deploy.md §3.4
# (App 1: agorcha-internal-docs) and docs-starlight-migration design D5/D8.
#
# Uses the Cloudflare provider v5 resource family (cloudflare_zero_trust_access_*).
# The deprecated v4 cloudflare_access_* resources are intentionally NOT used.
#
# Service-token model: PER-USER, runtime-minted. Terraform declares only the app
# and its policies; the Service-Auth policy accepts ANY valid Access service token.
# The dm6 provision endpoint mints one CF Access service token per developer (named
# agorcha-agents-<github-login>) via the CF API at provision time. No service token
# is declared here, so none of their secrets live in Terraform state.

# The Access application gating agorcha.dev/internal/*.
resource "cloudflare_zero_trust_access_application" "agorcha_internal_docs" {
  zone_id          = var.zone_id
  name             = "agorcha-internal-docs"
  domain           = "agorcha.dev/internal"
  type             = "self_hosted"
  session_duration = "24h"

  # GitHub + One-time PIN per the design contract. Supplied as IdP UUIDs via
  # var.allowed_idp_ids; when empty, the app offers all team-configured IdPs.
  allowed_idps = var.allowed_idp_ids

  # In provider v5 policies are standalone resources attached here by id, in
  # ascending order of precedence. Identity (maintainers) is evaluated first,
  # then the non-identity service-token path for machine clients.
  policies = [
    {
      id         = cloudflare_zero_trust_access_policy.maintainers.id
      precedence = 1
    },
    {
      id         = cloudflare_zero_trust_access_policy.agents_service_token.id
      precedence = 2
    },
  ]
}

# Human maintainers: browser login, allow by explicit email.
resource "cloudflare_zero_trust_access_policy" "maintainers" {
  account_id = var.account_id
  name       = "maintainers"
  decision   = "allow"

  include = [
    {
      email = {
        email = var.maintainer_email
      }
    },
  ]
}

# Service Auth path: non-browser clients presenting ANY valid Access service
# token's Cf-Access-Client-Id / Cf-Access-Client-Secret headers are admitted
# without an identity (decision = non_identity). Individual per-developer tokens
# (agorcha-agents-<github-login>) are minted at runtime by the dm6 provision
# endpoint; revoke a developer by deleting their token, no Terraform change needed.
resource "cloudflare_zero_trust_access_policy" "agents_service_token" {
  account_id = var.account_id
  name       = "agents-service-token"
  decision   = "non_identity"

  include = [
    {
      any_valid_service_token = {}
    },
  ]
}
