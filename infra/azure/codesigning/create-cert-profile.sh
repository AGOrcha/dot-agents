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

# Identity validations are PORTAL-ONLY. The Microsoft.CodeSigning provider exposes
# only codeSigningAccounts + certificateProfiles, and `az trustedsigning` has no
# identity-validation command — there is no CLI or ARM API to list/read them, so the
# id CANNOT be auto-discovered. Copy it from the portal (Trusted Signing -> $ACCOUNT
# -> Identity validations -> "Identity validation Id") and pass it as
# IDENTITY_VALIDATION_ID. Every trust profile type binds to one (PublicTrustTest was
# observed to require it too: an empty id is rejected as "IdentityValidationId ...
# property value is invalid").
validationId="${IDENTITY_VALIDATION_ID:-}"
if [[ -z "$validationId" ]]; then
  echo "error: IDENTITY_VALIDATION_ID is required and cannot be auto-discovered." >&2
  echo "       Identity validations live only in the portal (no CLI/ARM list API)." >&2
  echo "       1. Confirm the validation reads 'Completed' (the 1-7 day Microsoft review):" >&2
  echo "          portal -> Trusted Signing -> $ACCOUNT -> Identity validations." >&2
  echo "       2. Copy its 'Identity validation Id' and re-run:" >&2
  echo "          IDENTITY_VALIDATION_ID=<id> $0" >&2
  exit 1
fi
echo "==> Using identity validation: $validationId"
echo "    (Verify it reads 'Completed' in the portal. A not-Completed or otherwise"
echo "     unhealthy validation is masked by the backend as an opaque 'UnknownError'"
echo "     at profile-creation time — the error gives no hint that the validation is the cause.)"

echo "==> Creating $PROFILE_TYPE certificate profile '$PROFILE_NAME' on account '$ACCOUNT'"
# Create the certificateProfiles resource DIRECTLY under the signing account via the
# native data-plane command — not `az deployment group create`. The ARM-deployment
# path produced a `Microsoft.Resources/deployments/da-cert-profile-*` object in the
# resource group for every run (clutter), whereas this PUTs the actual
# Microsoft.CodeSigning/.../certificateProfiles child resource with no deployment
# wrapper. The certificate subject is composed by the service from the bound identity
# validation; the include-* subject flags apply only to private-trust profile types,
# so they are intentionally omitted for Public Trust.
az extension show -n trustedsigning >/dev/null 2>&1 || az extension add -n trustedsigning >/dev/null
az trustedsigning certificate-profile create \
  --resource-group "$RESOURCE_GROUP" \
  --subscription "$SUBSCRIPTION" \
  --account-name "$ACCOUNT" \
  --name "$PROFILE_NAME" \
  --profile-type "$PROFILE_TYPE" \
  --identity-validation-id "$validationId" \
  --query "provisioningState" -o tsv

cat <<EOF

==> Certificate profile '$PROFILE_NAME' is live. Flip CI signing on with:

  gh variable set TRUSTED_SIGNING_PROFILE --repo AGOrcha/dot-agents --body "$PROFILE_NAME"

The next release (a VERSION bump on master) will then Authenticode-sign da.exe.
EOF
