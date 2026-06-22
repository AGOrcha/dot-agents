# Cloudflare Access — agorcha.dev internal docs (Terraform)

Declarative IaC for the Cloudflare Access application that gates
`agorcha.dev/internal/*`. Service-token (non-browser / Service Auth) access uses a
**per-user model**: Terraform declares the app and its policies — the Service-Auth
policy accepts **any valid Access service token** — while individual tokens are
minted at runtime, one per developer, by the dm6 provision endpoint.

Contract source: `.agents/proposals/agorcha-public-vs-internal-and-obs-deploy.md`
§3.4 (App 1: `agorcha-internal-docs`) and the `docs-starlight-migration` design
(D5/D8). This module is dm0 of that migration; dm5's Worker verifies the CF Access
JWT `aud` claim against the `audTag` output here.

## Per-user service-token model

Terraform owns the **shape** of access, not individual credentials:

- The Service-Auth policy (`agents_service_token`) admits **any valid Access service
  token** (`include = [{ any_valid_service_token = {} }]`, `decision = non_identity`).
  No specific token is referenced, so no token secret ever lands in Terraform state.
- The **dm6 provision endpoint** mints one CF Access service token **per developer**
  at provision time, named `agorcha-agents-<github-login>`, via the CF API
  (`POST /accounts/{account_id}/access/service_tokens`). It hands the developer their
  own `Cf-Access-Client-Id` / `Cf-Access-Client-Secret`.
- **Revoking a developer** is a per-token operation: delete their service token via
  the CF API or dashboard. No Terraform change, plan, or apply is required.

The minting endpoint needs its **own** scoped Cloudflare API token, separate from the
maintainer's apply token, carrying **Account → Access: Service Tokens → Edit** (so it
can create/delete per-developer tokens at runtime).

## What this declares

| Resource | Purpose |
|---|---|
| `cloudflare_zero_trust_access_application.agorcha_internal_docs` | Self-hosted Access app on `agorcha.dev/internal`, 24h session, GitHub + One-time PIN IdPs. |
| `cloudflare_zero_trust_access_policy.maintainers` | `allow` by email (`maintainer_email`) for browser logins. |
| `cloudflare_zero_trust_access_policy.agents_service_token` | `non_identity` (Service Auth) — admits clients presenting **any valid** Access service token. Individual tokens are minted per developer at runtime (see below), not declared here. |

All resources use the **Cloudflare provider v5** `cloudflare_zero_trust_access_*`
family. The deprecated v4 `cloudflare_access_*` names are not used. The provider is
pinned to `~> 5.0` in `versions.tf`.

## Prerequisites (what the maintainer must supply)

`terraform apply` is the **maintainer's step** — these IaC files carry no Cloudflare
credentials and none are needed to `validate`. To apply you need:

1. **`CLOUDFLARE_API_TOKEN`** — a Cloudflare API token with, at minimum:
   - **Account → Access: Apps and Policies → Edit** (creates/updates the app + policies)

   This apply token no longer needs **Access: Service Tokens → Edit** — Terraform mints
   no tokens. That permission belongs to the **dm6 provision endpoint's own** API token,
   which mints per-developer tokens at runtime (see the per-user model above).

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
| `account_id` | Echoes `var.account_id`. The dm6 provision endpoint uses it to mint per-developer service tokens (`POST /accounts/{account_id}/access/service_tokens`). |
| `zone_id` | Echoes `var.zone_id`. Identifies the Access app's zone for the dm6 provision endpoint. |

There are **no** `service_token_client_id` / `service_token_client_secret` outputs:
service-token credentials are per-developer and minted at runtime by dm6, not by
Terraform. Each developer receives their own client ID/secret from the provision
endpoint and stores it in `~/.config/da/credentials.json` (mode 0600), not in the
repo (see proposal §5.4).

## State

For v1, **state is local and gitignored** (`*.tfstate*` in `.gitignore`). With the
per-user model no service-token secrets live in state — those are minted at runtime by
dm6 — but the state should still never be committed. When this graduates beyond a
single operator, move to a remote backend (e.g. an R2-backed `s3` backend or Terraform
Cloud) and add a `backend` block to `versions.tf`; until then, keep the local state
file out of version control and back it up out-of-band.
