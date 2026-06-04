#!/usr/bin/env bash
# Scripts the GitHub OIDC -> Azure federation for Trusted Signing in CI (secretless).
# Creates an Entra app + service principal, a federated credential bound to this
# the repo release GitHub Environment, and the "Trusted Signing Certificate Profile
# Signer" role on the signing account. Prints the GitHub repo variables to set.
#
# Prereqs: the signing account exists (deploy.sh) and a Public Trust certificate
# profile has been created (after identity validation completes).
#
# Usage:
#   az login
#   SUBSCRIPTION=<sub-id> RESOURCE_GROUP=<rg> ./azure-oidc-setup.sh
set -euo pipefail

SUBSCRIPTION="${SUBSCRIPTION:-32ca3366-7dd6-4f19-aff9-fc43d2e017fd}"
RESOURCE_GROUP="${RESOURCE_GROUP:-AGOrcha}"
ACCOUNT="${ACCOUNT:-AGOrcha}"
APP_NAME="${APP_NAME:-da-trusted-signing-ci}"
GH_REPO="${GH_REPO:-AGOrcha/dot-agents}"
ENVIRONMENT="${ENVIRONMENT:-release}"   # release job runs under this GitHub Environment -> stable OIDC subject
SUBJECT="repo:${GH_REPO}:environment:${ENVIRONMENT}"

command -v az >/dev/null 2>&1 || { echo "error: azure cli (az) not found" >&2; exit 1; }
az account set --subscription "$SUBSCRIPTION"

# 1. App registration + service principal (idempotent: reuse if present)
appId="$(az ad app list --display-name "$APP_NAME" --query '[0].appId' -o tsv)"
if [[ -z "$appId" ]]; then
  appId="$(az ad app create --display-name "$APP_NAME" --query appId -o tsv)"
fi
az ad sp show --id "$appId" >/dev/null 2>&1 || az ad sp create --id "$appId" >/dev/null

# 2. Federated credential (GitHub OIDC, environment-scoped -> exact, stable subject)
az ad app federated-credential create --id "$appId" --parameters \
  "{\"name\":\"github-${ENVIRONMENT}\",\"issuer\":\"https://token.actions.githubusercontent.com\",\"subject\":\"${SUBJECT}\",\"audiences\":[\"api://AzureADTokenExchange\"]}" \
  2>/dev/null || echo "  (federated credential github-${ENVIRONMENT} already exists)"

# 3. Role assignment: signer on the Trusted Signing account
accountId="$(az resource show -g "$RESOURCE_GROUP" -n "$ACCOUNT" \
  --resource-type Microsoft.CodeSigning/codesigningaccounts --query id -o tsv)"
az role assignment create --assignee "$appId" \
  --role "Trusted Signing Certificate Profile Signer" --scope "$accountId" \
  >/dev/null 2>&1 || echo "  (role assignment already present)"

tenantId="$(az account show --query tenantId -o tsv)"
cat <<EOF

==> Done. Set these as GitHub repository VARIABLES
    (Settings -> Secrets and variables -> Actions -> Variables):
  AZURE_CLIENT_ID=${appId}
  AZURE_TENANT_ID=${tenantId}
  AZURE_SUBSCRIPTION_ID=${SUBSCRIPTION}
  TRUSTED_SIGNING_ENDPOINT=https://eus.codesigning.azure.net/
  TRUSTED_SIGNING_ACCOUNT=${ACCOUNT}
  TRUSTED_SIGNING_PROFILE=<your-public-trust-cert-profile-name>

Then add environment: ${ENVIRONMENT} to the release job in
.github/workflows/auto-release.yml so the OIDC subject matches.
EOF
