# Cloudflare Access — bootstrap source of truth

Declarative in `infra/cloudflare/` (Terraform, Cloudflare provider v5,
`cloudflare_zero_trust_access_*`). Account `bfabaf5bb310ba98e4b95c74e88dd271`
("Nikprakash20@gmail.com's Account"), zone `agorcha.dev`
(`086edf43b7e900d97ac7fb78a1128779`). State is local + gitignored (`*.tfstate*`);
the authoritative copy lives at `infra/cloudflare/terraform.tfstate` in the primary
checkout — back it up out-of-band, never commit it.

Workers verify the CF Access JWT `aud` claim against the app's audTag below.

## Apps

| App | Domain | audTag | Policies |
|---|---|---|---|
| `agorcha-internal-docs` | `agorcha.dev/internal` | `50476bf0ad28d003fbf4cfb1d36ed2554907f7f2e0e6c699f5fb4bc4b8d1a6ff` | `maintainers` (email), `agents-service-token` (any-valid, per-user runtime-minted) |
| `agorcha-obs` | `obs.agorcha.dev` | `5eeb249ebbc523b5ed10cec93c0b2d6995d991a7e0f6607a3f04e02365b18ea6` | `obs-maintainers` (email), `obs-service-token` (**bound single token**) |

## obs.agorcha.dev — single-tenant (dot-agents) observability

- **App** `agorcha-obs`, self-hosted, 24h session, GitHub IdP.
- **`obs-maintainers`** — browser login allowed for `nikprakash20@gmail.com`,
  `nikashprakash1@gmail.com` (var `obs_maintainer_emails`).
- **`obs-service-token`** — non-identity, **bound to the single `agorcha-obs-cli`
  service token** (NOT any-valid; ObsPlanReview MAJOR — an unrelated account token
  cannot inject). The obs Worker validates the Access-issued JWT (`Cf-Access-Jwt-Assertion`);
  it never validates the raw pair.
- **Service token `agorcha-obs-cli`**
  - `Cf-Access-Client-Id`: `9ffc04b015b6f83ae1de7a85748c6f11.access`
  - `Cf-Access-Client-Secret`: **NOT stored here** (shown once by CF; held in the
    Terraform state). Retrieve with:
    `cd infra/cloudflare && terraform output -raw obs_service_token_client_secret`.
    Load it into the credstore via `da observability login` (task o8) — id `agorcha-obs`,
    kind `cf-access-service-token` — into `~/.config/da/credentials.json` (0600). Never
    commit it or write it to `.agentsrc.json`.

## Provisioning / apply

Terraform-managed. Apply requires a scoped `CLOUDFLARE_API_TOKEN` (account: Access Apps
and Policies Write + Access Service Tokens Write; zone: Apps and Policies Write + Zone
Read). Mint it from the account global key (do not hand the global key to Terraform).
`obs-o2` applied with `-target` on the four obs resources only, leaving `agorcha-internal-docs`
untouched.

## Known drift (App 1, not reconciled here)

`agorcha-internal-docs`'s `maintainers` policy in CF allows `nikprakash20@gmail.com`, but
the Terraform default (`var.maintainer_email = nikashprakash1@gmail.com`) would flip it. A
full (non-targeted) apply would change App 1's allowed email. Reconcile deliberately: set
`maintainer_email = "nikprakash20@gmail.com"` in `terraform.tfvars` (matches CF reality) or
confirm the intended maintainer email before a non-targeted apply.

## Per-project tokens & retention (v1 posture)

Both are **deferred** by the spec (`.agents/workflow/specs/obs-dashboard-cf-deploy/design.md`)
— documented here so no implementer re-decides them.

- **Per-project service tokens — deferred.** `obs.agorcha.dev` is single-tenant: the Worker
  pins `OBS_PROJECT_ID=github.com/AGOrcha/dot-agents` and rejects any foreign `project_id`
  before a DO/D1 write (spec §D1). One bound token (`agorcha-obs-cli`) is sufficient; issuing a
  token per project inside one backend is on the spec's Deferred list. Other repositories get
  their own backend via their `.agentsrc.json` `observability.endpoint` (client-side routing),
  each with its own Terraform + token — never a fan-out from this deployment.
- **Retention — fixed v1 defaults, configurability deferred.** These are outbox-side constants
  (spec §D4), not `.agentsrc.json` config in v1 (spec Open Question #4 keeps team/org
  configurability open; no implementation may pick different v1 defaults):

  | Class | v1 retention |
  |---|---|
  | Valid pending outbox events | kept indefinitely (silent telemetry loss is worse than queue growth) |
  | Quarantine + `.reason.json` files | 30 days, then pruned during sync |
  | Orphan `.<uuid>.tmp` files | removed after 24h during sync |
  | Accepted/deduped files | deleted immediately on success |

  Local `.agents/history/` + iteration logs stay canonical; `da observability sync --full`
  rebuilds a lost/wiped remote from them. There is no `observability.retention` config block in
  v1 — adding one is a future spec change, not IaC hardening.
