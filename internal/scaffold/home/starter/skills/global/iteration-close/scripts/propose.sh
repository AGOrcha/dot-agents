#!/bin/bash
# propose.sh — create a workflow proposal for review
#
# Usage:
#   propose.sh --type <skill|rule|hook|setting> \
#              --action <add|modify|remove> \
#              --target <path-relative-to-agents-home> \
#              --rationale "<why>" \
#              [--content-file <path>] \
#              [--content "<inline string>"] \
#              [--id <custom-id>]
#
# When --action remove is used, --content is not required.
# When --action add|modify, --content or --content-file is required.
#
# Examples:
#   # Add a gotcha to iteration-close skill
#   propose.sh --type skill --action modify \
#     --target "skills/dot-agents/iteration-close/instructions/gotchas.md" \
#     --rationale "kg warm silently succeeds when KG_HOME uninitialized" \
#     --content-file /tmp/updated-gotchas.md
#
#   # Add a rule to global rules
#   propose.sh --type rule --action modify \
#     --target "rules/global/rules.mdc" \
#     --rationale "Add loop checkpoint reminder" \
#     --content-file /tmp/updated-rules.md

set -euo pipefail

PROPOSALS_DIR="${AGENTS_HOME:-$HOME/.agents}/proposals"
TYPE=""
ACTION=""
TARGET=""
RATIONALE=""
CONTENT=""
CONTENT_FILE=""
CUSTOM_ID=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --type) TYPE="$2"; shift 2 ;;
    --action) ACTION="$2"; shift 2 ;;
    --target) TARGET="$2"; shift 2 ;;
    --rationale) RATIONALE="$2"; shift 2 ;;
    --content) CONTENT="$2"; shift 2 ;;
    --content-file) CONTENT_FILE="$2"; shift 2 ;;
    --id) CUSTOM_ID="$2"; shift 2 ;;
    *) echo "Unknown flag: $1" >&2; exit 1 ;;
  esac
done

# Validate required fields
for field in TYPE ACTION TARGET RATIONALE; do
  if [[ -z "${!field}" ]]; then
    echo "Error: --$(echo $field | tr '[:upper:]' '[:lower:]') is required" >&2
    exit 1
  fi
done

# Validate type/action
if [[ ! "$TYPE" =~ ^(rule|skill|hook|setting)$ ]]; then
  echo "Error: --type must be one of: rule, skill, hook, setting" >&2; exit 1
fi
if [[ ! "$ACTION" =~ ^(add|modify|remove)$ ]]; then
  echo "Error: --action must be one of: add, modify, remove" >&2; exit 1
fi

# Load content
if [[ "$ACTION" != "remove" ]]; then
  if [[ -n "$CONTENT_FILE" ]]; then
    CONTENT=$(cat "$CONTENT_FILE")
  fi
  if [[ -z "$CONTENT" ]]; then
    echo "Error: --content or --content-file required for action '$ACTION'" >&2; exit 1
  fi
fi

# Generate ID
TIMESTAMP=$(date -u +%Y%m%dT%H%M%S)
SAFE_TARGET=$(echo "$TARGET" | tr '/' '-' | tr '.' '-' | tr '_' '-' | sed 's/--*/-/g' | cut -c1-40)
ID="${CUSTOM_ID:-${TIMESTAMP}-${SAFE_TARGET}}"

mkdir -p "$PROPOSALS_DIR"
PROPOSAL_PATH="$PROPOSALS_DIR/${ID}.yaml"

# Escape content for YAML literal block
CONTENT_YAML=$(printf '%s' "$CONTENT" | sed 's/^/    /')

cat > "$PROPOSAL_PATH" <<EOF
schema_version: 1
id: "${ID}"
status: pending
type: ${TYPE}
action: ${ACTION}
target: ${TARGET}
rationale: |
  ${RATIONALE}
content: |
${CONTENT_YAML}
created_at: "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
created_by: loop-agent
reviewed_at: ""
review_reason: ""
EOF

echo "Proposal written: $PROPOSAL_PATH"
echo "  id: $ID"
echo "  type: $TYPE / $ACTION"
echo "  target: $TARGET"
echo ""
echo "Review with: da review show $ID"
echo "Approve with: da review approve $ID"
