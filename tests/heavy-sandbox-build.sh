#!/usr/bin/env bash

set -euo pipefail

IMAGE_TAG="${IMAGE_TAG:-dot-agents-sandbox:${GITHUB_RUN_ID:-local}}"

docker build -t "${IMAGE_TAG}" -f tests/Dockerfile.sandbox .
docker run --rm "${IMAGE_TAG}" dot-agents --version
docker run --rm "${IMAGE_TAG}" dot-agents --help >/dev/null
