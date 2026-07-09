#!/usr/bin/env bash
# Prebuild hook: copy repo-root JSON schemas into the Astro public/
# directory so they are served verbatim at:
#   https://agorcha.dev/schemas/<name>.json
#
# Runs from `pnpm run build` via the `prebuild` script in package.json.
# Idempotent — safe to run repeatedly.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEB_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPO_ROOT="$(cd "${WEB_ROOT}/../.." && pwd)"
SRC="${REPO_ROOT}/schemas"
DEST="${WEB_ROOT}/public/schemas"

mkdir -p "${DEST}"

shopt -s nullglob
schemas=("${SRC}"/*.json)
shopt -u nullglob

if [ ${#schemas[@]} -eq 0 ]; then
  echo "copy-schemas: no JSON schemas found under ${SRC} — nothing to copy"
  exit 0
fi

for f in "${schemas[@]}"; do
  cp -v "${f}" "${DEST}/"
done

echo "copy-schemas: copied ${#schemas[@]} schema file(s) into ${DEST}"
