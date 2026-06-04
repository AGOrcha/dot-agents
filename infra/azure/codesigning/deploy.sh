#!/usr/bin/env bash
# Idempotent deploy of the Azure Trusted Signing (Microsoft.CodeSigning) account.
# Re-applying an unchanged template is a no-op, so this is safe to run repeatedly
# and to wire into a pipeline. The account itself already exists (created via the
# portal); this makes it reproducible / drift-correctable as code.
#
# Usage:
#   az login
#   RESOURCE_GROUP=<rg> ./deploy.sh
#
# Optional env:
#   SUBSCRIPTION  - az subscription id/name to target (else the current default)
#   LOCATION      - overrides the location in parameters.json
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESOURCE_GROUP="${RESOURCE_GROUP:-AGOrcha}"        # account lives in the AGOrcha RG (eastus)
SUBSCRIPTION="${SUBSCRIPTION:-32ca3366-7dd6-4f19-aff9-fc43d2e017fd}"

if ! command -v az >/dev/null 2>&1; then
  echo "error: azure cli (az) not found — https://learn.microsoft.com/cli/azure/install-azure-cli" >&2
  exit 1
fi

args=(
  --resource-group "$RESOURCE_GROUP"
  --template-file "$here/template.json"
  --parameters "@$here/parameters.json"
  --name "da-codesigning-$(date +%Y%m%d%H%M%S)"
)
[[ -n "${SUBSCRIPTION:-}" ]] && args+=(--subscription "$SUBSCRIPTION")
[[ -n "${LOCATION:-}" ]] && args+=(--parameters "location=$LOCATION")

echo "==> Deploying Microsoft.CodeSigning account into resource group '$RESOURCE_GROUP'"
az deployment group create "${args[@]}" --query "properties.provisioningState" -o tsv
echo "==> Done. Next: once identity validation is Completed, run ./create-cert-profile.sh (README.md §3)."
