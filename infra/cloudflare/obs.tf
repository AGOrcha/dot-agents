# Cloudflare Access for obs.agorcha.dev — single-tenant dot-agents observability.
#
# Contract source: .agents/workflow/specs/obs-dashboard-cf-deploy/design.md (o1) and
# .agents/proposals/agorcha-public-vs-internal-and-obs-deploy.md §3.4 (App 2: agorcha-obs).
#
# SINGLE-TENANT: obs.agorcha.dev serves only dot-agents. Unlike App 1's per-user
# any-valid-service-token model, obs binds its non-identity policy to ONE declared
# service token (created here) — an unrelated account token cannot inject events
# (ObsPlanReview MAJOR). The token's client_secret is a sensitive output; it is stored
# in ~/.config/da/credentials.json (per the o1 credential-ref contract), never the repo.

# The single bound obs CLI service token.
resource "cloudflare_zero_trust_access_service_token" "obs" {
  account_id = var.account_id
  name       = "agorcha-obs-cli"
}

# The Access application gating obs.agorcha.dev.
resource "cloudflare_zero_trust_access_application" "agorcha_obs" {
  zone_id          = var.zone_id
  name             = "agorcha-obs"
  domain           = "obs.agorcha.dev"
  type             = "self_hosted"
  session_duration = "24h"

  # Inherits team IdPs (GitHub) when var.allowed_idp_ids is empty.
  allowed_idps = var.allowed_idp_ids

  # Identity (maintainers) first, then the bound non-identity service-token path.
  policies = [
    {
      id         = cloudflare_zero_trust_access_policy.obs_maintainers.id
      precedence = 1
    },
    {
      id         = cloudflare_zero_trust_access_policy.obs_service_token.id
      precedence = 2
    },
  ]
}

# Human maintainers: browser login, allow by explicit email(s).
resource "cloudflare_zero_trust_access_policy" "obs_maintainers" {
  account_id = var.account_id
  name       = "obs-maintainers"
  decision   = "allow"

  include = [
    for e in var.obs_maintainer_emails : {
      email = {
        email = e
      }
    }
  ]
}

# Service Auth path: bound to the SINGLE obs service token created above (NOT
# any_valid_service_token). Only the CLI presenting this token's Client-Id/Secret is
# admitted without an identity.
resource "cloudflare_zero_trust_access_policy" "obs_service_token" {
  account_id = var.account_id
  name       = "obs-service-token"
  decision   = "non_identity"

  include = [
    {
      service_token = {
        token_id = cloudflare_zero_trust_access_service_token.obs.id
      }
    },
  ]
}
