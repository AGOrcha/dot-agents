# Cloudflare Access bootstrap for agorcha.dev internal docs.
#
# Contract source: .agents/proposals/agorcha-public-vs-internal-and-obs-deploy.md §3.4
# (App 1: agorcha-internal-docs) and docs-starlight-migration design D5/D8.
#
# Uses the Cloudflare provider v5 resource family (cloudflare_zero_trust_access_*).
# The deprecated v4 cloudflare_access_* resources are intentionally NOT used.

# Machine account the agents/CLI authenticate with on non-browser requests.
# Created before the policy that references it so token_id resolves at plan time.
resource "cloudflare_zero_trust_access_service_token" "agorcha_agents" {
  account_id = var.account_id
  name       = "agorcha-agents"
}

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

# Service Auth path: non-browser clients presenting the service token's
# Cf-Access-Client-Id / Cf-Access-Client-Secret headers are admitted without
# an identity (decision = non_identity).
resource "cloudflare_zero_trust_access_policy" "agents_service_token" {
  account_id = var.account_id
  name       = "agents-service-token"
  decision   = "non_identity"

  include = [
    {
      service_token = {
        token_id = cloudflare_zero_trust_access_service_token.agorcha_agents.id
      }
    },
  ]
}
