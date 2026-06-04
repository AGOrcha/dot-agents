# Azure Trusted Signing (Microsoft.CodeSigning) — Infrastructure as Code

Reproducible definition of the **Azure Trusted Signing** account used to sign the
project's **Windows release binaries** (`da.exe`). The account already exists (created
via the portal); this captures it as code so it's drift-correctable and the rest of
the signing setup is automated rather than click-ops.

| File | Purpose |
|---|---|
| `template.json` | ARM template for the `Microsoft.CodeSigning/codesigningaccounts` resource |
| `parameters.json` | Account name (`AGOrcha`), region (`eastus`), SKU (`Basic`), tags |
| `deploy.sh` | Idempotent `az deployment group create` wrapper |

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
2. **Certificate profile** — once validation is `Completed`, create a
   `Microsoft.CodeSigning/codesigningaccounts/certificateProfiles` of type
   **Public Trust** (required for distributing `.exe` to the public; Private Trust is
   for internal-only). Note the **profile name** and the account's **regional endpoint**
   (e.g. `https://eus.codesigning.azure.net/` for `eastus`).

   *(This can be added to `template.json` as a child resource once the identity is
   validated — I'll fold it in then so the profile is code too.)*

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

**Repo/CI variables** (non-secret → `vars`; identity → OIDC, no secret needed):
`AZURE_TENANT_ID`, `AZURE_CLIENT_ID` (the federated app), `AZURE_SUBSCRIPTION_ID`,
`TRUSTED_SIGNING_ENDPOINT`, `TRUSTED_SIGNING_ACCOUNT=AGOrcha`,
`TRUSTED_SIGNING_PROFILE=<profile-name>`.

## 5. Decisions to confirm before I wire CI

- **Auth:** OIDC federation (recommended, secretless) vs a service-principal client
  secret in repo secrets.
- **Trust type:** Public Trust cert profile (for public `.exe`) — assumed.
- **Resource group / subscription** for `deploy.sh`.

Once identity validation clears and you confirm the above, I'll add the certificate-
profile resource to the template and the OIDC sign step to `auto-release.yml`, gated so a
missing profile **skips** signing (like the Cosign/Sonar skips) rather than breaking the
release.
