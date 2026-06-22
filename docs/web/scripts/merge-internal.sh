#!/usr/bin/env bash
# Post-build merge step (Option A): assemble the single asset directory the
# Worker serves.
#
# The two-pass `npm run build` produces:
#   dist/           public only — with the CLEAN public pagefind search index
#   dist-internal/  everything  — adds dist-internal/internal/{lessons,specs,proposals}/
#                                 and a pagefind index that INCLUDES internal titles
#
# Cloudflare allows only ONE static-assets binding per Worker, so we serve a
# single merged dist/. This script copies ONLY the gated internal pages into
# dist/internal/ and DELIBERATELY leaves dist/pagefind/ untouched, so internal
# titles never leak into the public search index. The Worker (src/worker.js)
# gates /internal/* behind a Cloudflare Access JWT at request time.
#
# Idempotent — safe to run repeatedly. Runs from `npm run build:merged` /
# the deploy workflow after the Astro build.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEB_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
DIST="${WEB_ROOT}/dist"
DIST_INTERNAL="${WEB_ROOT}/dist-internal"
SRC_INTERNAL="${DIST_INTERNAL}/internal"
DEST_INTERNAL="${DIST}/internal"

if [ ! -d "${DIST}" ]; then
  echo "merge-internal: ${DIST} not found — run the build first" >&2
  exit 1
fi
if [ ! -d "${SRC_INTERNAL}" ]; then
  echo "merge-internal: ${SRC_INTERNAL} not found — run the internal build pass (npm run build:internal) first" >&2
  exit 1
fi

# Replace any prior merge so removed internal pages don't linger.
rm -rf "${DEST_INTERNAL}"
mkdir -p "${DEST_INTERNAL}"

# Copy the gated pages only. We intentionally do NOT touch dist/pagefind — the
# public search index stays clean (no internal titles).
cp -R "${SRC_INTERNAL}/." "${DEST_INTERNAL}/"

count="$(find "${DEST_INTERNAL}" -name index.html | wc -l | tr -d ' ')"
echo "merge-internal: merged ${count} internal page(s) into ${DEST_INTERNAL}"
echo "merge-internal: public pagefind index left untouched (dist/pagefind unchanged)"
