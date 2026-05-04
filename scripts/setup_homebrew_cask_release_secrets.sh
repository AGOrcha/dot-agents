#!/usr/bin/env bash

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info() { echo -e "${BLUE}[INFO]${NC} $*"; }
success() { echo -e "${GREEN}[OK]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
die() { echo -e "${RED}[ERROR]${NC} $*" >&2; exit 1; }

DRY_RUN=0
REPO=""
OUTPUT_DIR=""
GPG_NAME="github-actions[bot]"
GPG_EMAIL="github-actions[bot]@users.noreply.github.com"
DEPLOY_KEY_TITLE="goreleaser-homebrew-cask"
GPG_BIN=""
GPG_HOME=""

usage() {
  cat <<'EOF'
Usage:
  scripts/setup_homebrew_cask_release_secrets.sh [options]

Sets up the GitHub Actions secrets and deploy key used by the GoReleaser
`homebrew_casks` publish path:
  - HOMEBREW_TAP_GPG_PRIVATE_KEY
  - HOMEBREW_TAP_GPG_KEY_ID
  - HOMEBREW_TAP_SSH_KEY

The existing PAT secret is not modified.

Options:
  --repo OWNER/REPO       Target GitHub repository. Defaults to origin remote.
  --output-dir DIR        Directory to write generated key material into.
  --gpg-name NAME         GPG key real name.
  --gpg-email EMAIL       GPG key email.
  --gpg-home DIR          GPG home to use. Default: your normal GnuPG home.
  --deploy-key-title T    Deploy key title in GitHub.
  --dry-run               Generate local files but do not call GitHub APIs.
  --help                  Show this help.
EOF
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "Missing required command: $1"
}

find_gpg_bin() {
  if command -v gpg >/dev/null 2>&1; then
    echo "gpg"
    return
  fi
  if command -v gpg2 >/dev/null 2>&1; then
    echo "gpg2"
    return
  fi
  echo ""
}

start_gpg_agent() {
  local gpgconf_bin
  gpgconf_bin="$(command -v gpgconf || true)"
  [ -n "$gpgconf_bin" ] || die "Missing required command: gpgconf"

  # Explicitly allow loopback pinentry and start the agent when using a custom GNUPGHOME.
  [ -n "$GPG_HOME" ] || return 0

  cat >"$GPG_HOME/gpg-agent.conf" <<'EOF'
allow-loopback-pinentry
EOF

  "$gpgconf_bin" --kill gpg-agent >/dev/null 2>&1 || true
  "$gpgconf_bin" --launch gpg-agent
}

infer_repo_from_origin() {
  local remote
  remote="$(git remote get-url origin 2>/dev/null || true)"
  [ -n "$remote" ] || die "Could not infer repo from origin. Pass --repo OWNER/REPO."

  case "$remote" in
    git@github.com:*.git)
      echo "${remote#git@github.com:}" | sed 's/\.git$//'
      ;;
    https://github.com/*)
      echo "${remote#https://github.com/}" | sed 's/\.git$//'
      ;;
    ssh://git@github.com/*)
      echo "${remote#ssh://git@github.com/}" | sed 's/\.git$//'
      ;;
    *)
      die "Unsupported origin remote format: $remote"
      ;;
  esac
}

check_gh_auth() {
  if [ "$DRY_RUN" -eq 1 ]; then
    warn "Dry-run enabled; skipping gh authentication check."
    return
  fi
  gh auth status >/dev/null
}

run_or_print() {
  if [ "$DRY_RUN" -eq 1 ]; then
    printf '[dry-run] '
    printf '%q ' "$@"
    printf '\n'
    return 0
  fi
  "$@"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --repo)
      REPO="${2:-}"
      shift 2
      ;;
    --output-dir)
      OUTPUT_DIR="${2:-}"
      shift 2
      ;;
    --gpg-name)
      GPG_NAME="${2:-}"
      shift 2
      ;;
    --gpg-email)
      GPG_EMAIL="${2:-}"
      shift 2
      ;;
    --gpg-home)
      GPG_HOME="${2:-}"
      shift 2
      ;;
    --deploy-key-title)
      DEPLOY_KEY_TITLE="${2:-}"
      shift 2
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      die "Unknown argument: $1"
      ;;
  esac
done

require_cmd gh
require_cmd git
require_cmd awk
require_cmd sed

if [ "$DRY_RUN" -eq 0 ]; then
  require_cmd ssh-keygen
  GPG_BIN="$(find_gpg_bin)"
  [ -n "$GPG_BIN" ] || die "Missing required command: gpg (or gpg2). Install GnuPG first."
fi

REPO="${REPO:-$(infer_repo_from_origin)}"
OUTPUT_DIR="${OUTPUT_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/homebrew-cask-secrets.XXXXXX")}"

mkdir -p "$OUTPUT_DIR"
chmod 700 "$OUTPUT_DIR"

check_gh_auth || die "GitHub CLI is not authenticated. Run 'gh auth login -h github.com' first."

info "Repository: $REPO"
info "Output directory: $OUTPUT_DIR"
if [ -n "$GPG_HOME" ]; then
  mkdir -p "$GPG_HOME"
  chmod 700 "$GPG_HOME"
  export GNUPGHOME="$GPG_HOME"
  info "Using custom GPG home: $GPG_HOME"
else
  info "Using default GPG home from your local GnuPG setup"
fi

GPG_BATCH_FILE="$OUTPUT_DIR/gpg-batch.txt"
GPG_PRIVATE_ASC="$OUTPUT_DIR/homebrew-tap-gpg-private.asc"
GPG_KEY_ID_FILE="$OUTPUT_DIR/HOMEBREW_TAP_GPG_KEY_ID.txt"
SSH_KEY_FILE="$OUTPUT_DIR/homebrew-tap-ssh"

cat >"$GPG_BATCH_FILE" <<EOF
%no-protection
Key-Type: eddsa
Key-Curve: ed25519
Name-Real: $GPG_NAME
Name-Email: $GPG_EMAIL
Expire-Date: 0
EOF

if [ "$DRY_RUN" -eq 1 ]; then
  GPG_KEY_ID="DRYRUNFAKEKEYID1234"
  printf '%s\n' "$GPG_KEY_ID" >"$GPG_KEY_ID_FILE"
  printf '%s\n' "-----BEGIN PGP PRIVATE KEY BLOCK-----" "dry-run-placeholder" "-----END PGP PRIVATE KEY BLOCK-----" >"$GPG_PRIVATE_ASC"
  printf '%s\n' "dry-run-private-key" >"$SSH_KEY_FILE"
  printf '%s\n' "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDryrunplaceholder goreleaser-homebrew-cask" >"$SSH_KEY_FILE.pub"
  warn "Dry-run enabled; using placeholder key material."
else
  info "Generating dedicated GPG signing key"
  start_gpg_agent
  if [ -n "$GPG_HOME" ]; then
    "$GPG_BIN" --batch --pinentry-mode loopback --generate-key "$GPG_BATCH_FILE"
  else
    "$GPG_BIN" --batch --generate-key "$GPG_BATCH_FILE"
  fi

  GPG_KEY_ID="$("$GPG_BIN" --list-secret-keys --with-colons --keyid-format LONG "$GPG_EMAIL" | awk -F: '$1=="sec"{print $5; exit}')"
  [ -n "$GPG_KEY_ID" ] || die "Failed to determine generated GPG key ID."

  "$GPG_BIN" --armor --export-secret-keys "$GPG_KEY_ID" >"$GPG_PRIVATE_ASC"
  printf '%s\n' "$GPG_KEY_ID" >"$GPG_KEY_ID_FILE"

  info "Generating dedicated SSH deploy key"
  ssh-keygen -t ed25519 -C "$DEPLOY_KEY_TITLE" -f "$SSH_KEY_FILE" -N "" >/dev/null
fi

info "Uploading repository secrets"
if [ "$DRY_RUN" -eq 1 ]; then
  printf '[dry-run] gh secret set HOMEBREW_TAP_GPG_PRIVATE_KEY -R %q < %q\n' "$REPO" "$GPG_PRIVATE_ASC"
else
  gh secret set HOMEBREW_TAP_GPG_PRIVATE_KEY -R "$REPO" <"$GPG_PRIVATE_ASC"
fi
run_or_print gh secret set HOMEBREW_TAP_GPG_KEY_ID -R "$REPO" --body "$GPG_KEY_ID"
if [ "$DRY_RUN" -eq 1 ]; then
  printf '[dry-run] gh secret set HOMEBREW_TAP_SSH_KEY -R %q < %q\n' "$REPO" "$SSH_KEY_FILE"
else
  gh secret set HOMEBREW_TAP_SSH_KEY -R "$REPO" <"$SSH_KEY_FILE"
fi

info "Adding write deploy key to $REPO"
run_or_print gh repo deploy-key add "$SSH_KEY_FILE.pub" -R "$REPO" --title "$DEPLOY_KEY_TITLE" --allow-write

success "Homebrew cask release secrets are prepared."
echo
echo "Generated files:"
echo "  GPG private key: $GPG_PRIVATE_ASC"
echo "  GPG key ID:      $GPG_KEY_ID_FILE ($(cat "$GPG_KEY_ID_FILE"))"
echo "  SSH private key: $SSH_KEY_FILE"
echo "  SSH public key:  $SSH_KEY_FILE.pub"
echo
if [ "$DRY_RUN" -eq 1 ]; then
  warn "Dry-run mode did not upload secrets or add the deploy key."
else
  success "Uploaded secrets:"
  echo "  HOMEBREW_TAP_GPG_PRIVATE_KEY"
  echo "  HOMEBREW_TAP_GPG_KEY_ID"
  echo "  HOMEBREW_TAP_SSH_KEY"
fi
