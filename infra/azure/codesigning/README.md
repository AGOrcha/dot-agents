# Azure Trusted Signing (Microsoft.CodeSigning) — Infrastructure as Code

Reproducible definition of the **Azure Trusted Signing** account used to sign the
project's **Windows release binaries** (`da.exe`). The account already exists (created
via the portal); this captures it as code so it's drift-correctable and the rest of
the signing setup is automated rather than click-ops.

| File | Purpose |
|---|---|
| `template.json` | ARM template for the `Microsoft.CodeSigning/codesigningaccounts` resource |
| `parameters.json` | Account name (`AGOrcha`), region (`eastus`), SKU (`Basic`), tags |
| `deploy.sh` | Idempotent `az deployment group create` wrapper for the account |
| `cert-profile.json` | ARM template for the `certificateProfiles` child resource (Public Trust) |
| `create-cert-profile.sh` | Creates the cert profile post-validation; prints the command to flip CI signing on |
| `azure-oidc-setup.sh` | Scripts the GitHub→Azure OIDC federation (secretless CI auth) |

## 1. Deploy the account (idempotent)

```bash
az login
RESOURCE_GROUP=<the-rg-holding-the-account> ./deploy.sh
```

Re-running with an unchanged template is a no-op, so this is safe to wire into a
bootstrap pipeline later.

## 2. Why the account alone isn't enough

Signing needs three more things on top of the account. The first two are **one-time
manual Azure steps** (they require Microsoft-side verification and can't be fully
ARM-automated today); the third is what we wire into CI:

## 3. Certificate profile + identity validation (one-time, gating)

1. **Identity validation** — under the signing account, create an *Identity Validation*
   request for the **AGOrcha** organization (legal name, address, etc.). Microsoft
   verifies it; **this can take 1–7 business days** and gates everything below.
2. **Certificate profile** — once validation is `Completed`, create the
   **Public Trust** profile. This is now code (`cert-profile.json` +
   `create-cert-profile.sh`); run:

   ```bash
   az login
   ./create-cert-profile.sh           # discovers the Completed validation, deploys the profile
   ```

   The script auto-discovers the completed identity-validation id, deploys
   `cert-profile.json` (type **Public Trust** — required for distributing `.exe`
   to the public), and prints the exact `gh variable set TRUSTED_SIGNING_PROFILE`
   command that turns CI signing on. For a no-validation dry run first, set
   `PROFILE_TYPE=PublicTrustTest`. The account's regional endpoint is
   `https://eus.codesigning.azure.net/` (`eastus`).

## 4. CI/CD integration plan (the "integrate into CI/CD?" — yes)

Sign the Windows artifacts in **`.github/workflows/auto-release.yml`**, reusing the
GitHub **OIDC** the release job already has (`id-token: write`, used today by Cosign) —
**no stored cert or long-lived secret**.

**Auth (recommended): OIDC federation.** Create an Entra app / service principal with a
**federated credential** scoped to `repo:AGOrcha/dot-agents:ref:refs/tags/v*` (release
tags only), and grant it the **"Trusted Signing Certificate Profile Signer"** role on
the signing account. CI exchanges its OIDC token for Azure access — same trust model as
the existing Cosign keyless flow.

**Signing step.** After GoReleaser builds the `windows/*` binaries and before the
checksum/cosign step, sign each `da*.exe` via Azure Trusted Signing
(`azure/trusted-signing-action`, or the cross-platform `dotnet sign` + Trusted Signing
dlib so it runs on the existing `ubuntu` release runner). Inputs: the regional
**endpoint**, **account name** (`AGOrcha`), **certificate profile name**, and the file
glob. GoReleaser then checksums + cosign-signs the *already-Authenticode-signed* exe, so
downstream verification is unchanged.

**Repo/CI variables** (non-secret → `vars`; identity → OIDC, no secret needed).
Five are pre-set so signing activates the instant the sixth is:

| Variable | Value | Set? |
|---|---|---|
| `AZURE_TENANT_ID` | the AGOrcha tenant | ✅ set |
| `AZURE_CLIENT_ID` | the `da-trusted-signing-ci` federated app | ✅ set |
| `AZURE_SUBSCRIPTION_ID` | the signing subscription | ✅ set |
| `TRUSTED_SIGNING_ENDPOINT` | `https://eus.codesigning.azure.net/` | ✅ set |
| `TRUSTED_SIGNING_ACCOUNT` | `AGOrcha` | ✅ set |
| `TRUSTED_SIGNING_PROFILE` | cert profile name | ⏳ **the activation gate** — set by `create-cert-profile.sh` after validation |

The whole signing path in `auto-release.yml` is gated on `TRUSTED_SIGNING_PROFILE`
being non-empty, so the five pre-set values are inert until the profile exists.

## 5. Decisions (resolved)

- **Auth:** OIDC federation (secretless), bound to the `release` GitHub Environment
  (subject `repo:AGOrcha/dot-agents:environment:release`) — wired by `azure-oidc-setup.sh`.
- **Trust type:** Public Trust cert profile (for public `.exe`).
- **Resource group / subscription:** `AGOrcha` / `32ca3366-…` (defaults in the scripts).
- **Sign tooling:** cross-platform `dotnet sign` so it runs on the existing `ubuntu`
  release runner (the `azure/trusted-signing-action` is Windows-only).

Everything is wired and dormant. The only remaining step is yours: once identity
validation reads `Completed`, run `./create-cert-profile.sh` and set
`TRUSTED_SIGNING_PROFILE` — the next release then signs `da.exe`. A missing profile
**skips** signing (like the Cosign/Sonar skips) rather than breaking the release.
