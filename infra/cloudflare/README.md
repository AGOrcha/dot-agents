# Cloudflare Access — agorcha.dev internal docs (Terraform)

Declarative IaC for the Cloudflare Access application that gates
`agorcha.dev/internal/*`, plus the service token agents/CLI use for non-browser
(Service Auth) access.

Contract source: `.agents/proposals/agorcha-public-vs-internal-and-obs-deploy.md`
§3.4 (App 1: `agorcha-internal-docs`) and the `docs-starlight-migration` design
(D5/D8). This module is dm0 of that migration; dm5's Worker verifies the CF Access
JWT `aud` claim against the `audTag` output here.

## What this declares

| Resource | Purpose |
|---|---|
| `cloudflare_zero_trust_access_application.agorcha_internal_docs` | Self-hosted Access app on `agorcha.dev/internal`, 24h session, GitHub + One-time PIN IdPs. |
| `cloudflare_zero_trust_access_policy.maintainers` | `allow` by email (`maintainer_email`) for browser logins. |
| `cloudflare_zero_trust_access_policy.agents_service_token` | `non_identity` (Service Auth) — admits clients presenting the service token. |
| `cloudflare_zero_trust_access_service_token.agorcha_agents` | The machine account for agents/CLI. |

All resources use the **Cloudflare provider v5** `cloudflare_zero_trust_access_*`
family. The deprecated v4 `cloudflare_access_*` names are not used. The provider is
pinned to `~> 5.0` in `versions.tf`.

## Prerequisites (what the maintainer must supply)

`terraform apply` is the **maintainer's step** — these IaC files carry no Cloudflare
credentials and none are needed to `validate`. To apply you need:

1. **`CLOUDFLARE_API_TOKEN`** — a Cloudflare API token with, at minimum:
   - **Account → Access: Apps and Policies → Edit** (creates/updates the app + policies)
   - **Account → Access: Service Tokens → Edit** (creates the service token)

   Export it before running Terraform:

   ```sh
   export CLOUDFLARE_API_TOKEN="<token>"
   ```

2. **`account_id`** — the Cloudflare account ID owning the Zero Trust org.
3. **`zone_id`** — the zone ID for `agorcha.dev`.
4. (optional) **`allowed_idp_ids`** — IdP UUIDs for GitHub + One-time PIN. These are
   CF-assigned IDs (not display names); find them under
   *Zero Trust → Settings → Authentication* or via the Access Identity Providers API.
   Leave empty to inherit all of the team's configured IdPs.

Provide them via a (gitignored) `terraform.tfvars`:

```hcl
account_id = "<account-id>"
zone_id    = "<zone-id>"
# maintainer_email defaults to nikashprakash1@gmail.com
# allowed_idp_ids = ["<github-idp-uuid>", "<otp-idp-uuid>"]
```

…or with `-var` flags / `TF_VAR_*` environment variables.

## Apply

```sh
cd infra/cloudflare
terraform init
terraform plan      # review what will be created
terraform apply     # maintainer step — requires CLOUDFLARE_API_TOKEN + IDs above
```

Validate-only (no credentials, no backend, what CI/review runs):

```sh
terraform init -backend=false
terraform validate
terraform fmt -check
```

## Outputs

After apply:

| Output | Notes |
|---|---|
| `audTag` | The app's audience tag. Hand this to dm5's Worker (it verifies the JWT `aud` against it). |
| `service_token_client_id` | `Cf-Access-Client-Id` value for agents/CLI. |
| `service_token_client_secret` | `Cf-Access-Client-Secret` value. Marked `sensitive`; read with `terraform output -raw service_token_client_secret`. |

Store the client secret in `~/.config/da/credentials.json` (mode 0600), not in the
repo (see proposal §5.4).

## State

For v1, **state is local and gitignored** (`*.tfstate*` in `.gitignore`). Local state
holds the service-token secret in plaintext, so it must never be committed. When this
graduates beyond a single operator, move to a remote backend (e.g. an R2-backed `s3`
backend or Terraform Cloud) and add a `backend` block to `versions.tf`; until then,
keep the local state file out of version control and back it up out-of-band.
