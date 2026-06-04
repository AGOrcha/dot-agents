#!/usr/bin/env bash
# Create the Public Trust certificate profile once Microsoft identity validation
# is Completed — the one step gated on the 1-7 day validation review. Run this
# the moment validation clears: it discovers the completed validation id, deploys
# cert-profile.json, and prints the single command that flips CI signing on.
#
# Prereqs: the signing account exists (deploy.sh) and an Identity Validation
# request for AGOrcha has been submitted (portal) and reached Completed.
#
# Usage:
#   az login
#   ./create-cert-profile.sh
#
# Optional env:
#   RESOURCE_GROUP          - rg holding the account (default AGOrcha)
#   ACCOUNT                 - signing account name   (default AGOrcha)
#   PROFILE_NAME            - cert profile name       (default agorcha-public-trust)
#   PROFILE_TYPE            - PublicTrust | PublicTrustTest (default PublicTrust)
#   IDENTITY_VALIDATION_ID  - override auto-discovery of the completed validation
#   SUBSCRIPTION            - az subscription id      (default the AGOrcha sub)
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESOURCE_GROUP="${RESOURCE_GROUP:-AGOrcha}"
ACCOUNT="${ACCOUNT:-AGOrcha}"
PROFILE_NAME="${PROFILE_NAME:-agorcha-public-trust}"
PROFILE_TYPE="${PROFILE_TYPE:-PublicTrust}"
SUBSCRIPTION="${SUBSCRIPTION:-32ca3366-7dd6-4f19-aff9-fc43d2e017fd}"

command -v az >/dev/null 2>&1 || { echo "error: azure cli (az) not found" >&2; exit 1; }
az account set --subscription "$SUBSCRIPTION"

# The identity-validation lookup lives in the trustedsigning extension.
az extension show -n trustedsigning >/dev/null 2>&1 || az extension add -n trustedsigning >/dev/null

# Public Trust binds to a Completed identity validation; PublicTrustTest does not.
validationId="${IDENTITY_VALIDATION_ID:-}"
if [[ "$PROFILE_TYPE" == "PublicTrust" || "$PROFILE_TYPE" == "PrivateTrust" ]] && [[ -z "$validationId" ]]; then
  validationId="$(az trustedsigning identity-validation list \
    -g "$RESOURCE_GROUP" --account-name "$ACCOUNT" \
    --query "[?status=='Completed'] | [0].id" -o tsv 2>/dev/null || true)"
  if [[ -z "$validationId" ]]; then
    echo "error: no Completed identity validation found for account '$ACCOUNT'." >&2
    echo "       Identity validation is the 1-7 day Microsoft review. Submit it in the" >&2
    echo "       portal (Trusted Signing -> $ACCOUNT -> Identity validations) and re-run" >&2
    echo "       once it reads Completed, or pass IDENTITY_VALIDATION_ID=<id>." >&2
    echo "       (For a no-validation dry run, set PROFILE_TYPE=PublicTrustTest.)" >&2
    exit 1
  fi
  echo "==> Using completed identity validation: $validationId"
fi

echo "==> Creating $PROFILE_TYPE certificate profile '$PROFILE_NAME' on account '$ACCOUNT'"
az deployment group create \
  --resource-group "$RESOURCE_GROUP" \
  --subscription "$SUBSCRIPTION" \
  --template-file "$here/cert-profile.json" \
  --name "da-cert-profile-$(date +%Y%m%d%H%M%S)" \
  --parameters \
    accountName="$ACCOUNT" \
    profileName="$PROFILE_NAME" \
    profileType="$PROFILE_TYPE" \
    identityValidationId="$validationId" \
  --query "properties.provisioningState" -o tsv

cat <<EOF

==> Certificate profile '$PROFILE_NAME' is live. Flip CI signing on with:

  gh variable set TRUSTED_SIGNING_PROFILE --repo AGOrcha/dot-agents --body "$PROFILE_NAME"

The next release (a VERSION bump on master) will then Authenticode-sign da.exe.
EOF
