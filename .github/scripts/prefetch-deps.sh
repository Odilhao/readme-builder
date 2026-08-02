#!/usr/bin/env bash
# Prefetch dependencies using hermeto for hermetic builds.
# Hermeto v0.59.0, digest-pinned, fetches the full Go module cache and generates
# environment variables and SBOM for use in subsequent build steps.
# Usage: prefetch-deps.sh
# Outputs: hermeto-output/ (cache), hermeto.env (GOPROXY, GOMODCACHE, etc.), sbom.json

set -euo pipefail

readonly HERMETO_IMAGE='ghcr.io/hermetoproject/hermeto@sha256:bc33ceb313fc291736f546dc3bfad383d640cea8cb24e3f0d266686bdfa99d16'

# Step 1: Fetch all module dependencies into hermeto-output cache directory
printf 'Fetching dependencies with hermeto...\n'
podman run --rm \
  -v "$PWD:$PWD:z" \
  -w "$PWD" \
  "$HERMETO_IMAGE" \
  fetch-deps \
  --source . \
  --output ./hermeto-output \
  '{"path": ".", "type": "gomod"}'

# --for-output-dir tells hermeto where the cache will live at build time.
# This CI step builds directly on the runner (no container), so that's $PWD,
# not the /tmp/hermeto-output mount path a containerized build (#14) would use.
readonly HERMETO_OUTPUT_DIR="$PWD/hermeto-output"

# Step 2: Generate environment variables for offline build
printf 'Generating environment for offline build...\n'
podman run --rm \
  -v "$PWD:$PWD:z" \
  -w "$PWD" \
  "$HERMETO_IMAGE" \
  generate-env \
  ./hermeto-output \
  -o ./hermeto.env \
  --for-output-dir "$HERMETO_OUTPUT_DIR"

# Step 3: Inject auxiliary files (currently a no-op for gomod but part of the documented contract)
printf 'Injecting auxiliary files...\n'
podman run --rm \
  -v "$PWD:$PWD:z" \
  -w "$PWD" \
  "$HERMETO_IMAGE" \
  inject-files \
  ./hermeto-output \
  --for-output-dir "$HERMETO_OUTPUT_DIR"

printf 'Prefetch complete: hermeto-output/ (cache), hermeto.env (variables)\n'
