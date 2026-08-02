#!/usr/bin/env bash
# Check hermeto's SBOM against module whitelist.
# Hermeto outputs a CycloneDX SBOM alongside the prefetched cache, listing
# all components (as PURLs). This script validates each component's module
# path against a whitelist of known dependencies.
# Usage: check-licenses.sh
# Exit 0 if all modules are whitelisted, exit 1 if any are unknown.

set -euo pipefail

readonly SBOM_FILE='hermeto-output/bom.json'
readonly WHITELIST_FILE="$(dirname "$0")/modules-whitelist.txt"

# Check that required files exist
if [[ ! -f "$SBOM_FILE" ]]; then
  echo "::error::hermeto BOM not found at $SBOM_FILE" >&2
  exit 1
fi

if [[ ! -f "$WHITELIST_FILE" ]]; then
  echo "::error::module whitelist not found at $WHITELIST_FILE" >&2
  exit 1
fi

# Parse whitelist into an associative array: module_path -> license
declare -A WHITELISTED_MODULES
while IFS= read -r line; do
  # Skip empty lines and comments
  [[ -z "$line" || "$line" =~ ^# ]] && continue
  read -r module license <<< "$line"
  WHITELISTED_MODULES["$module"]="$license"
done < "$WHITELIST_FILE"

printf 'Checking BOM dependencies against whitelist...\n'

# Extract module paths from CycloneDX BOM using purl (PkgURL).
# Real hermeto v0.59.0 produces package-granularity components like:
#   pkg:golang/bufio?type=package                           (stdlib, no version)
#   pkg:golang/vendor/golang.org/x/crypto/chacha20?type=... (vendored, no version)
#   pkg:golang/github.com/google/go-github/v89/github@v89.0.0?type=package (sub-package)
#   pkg:golang/github.com/google/go-github/v89@v89.0.0?type=module (module root)
#
# Strategy:
#  1. Strip query string (?type=...) from purl
#  2. Strip version (@version) from purl
#  3. Exclude stdlib: no `/` means stdlib (e.g., bufio, bytes, cmp)
#  4. Exclude vendor/ prefix: paths under vendor/
#  5. Exclude main module: github.com/Odilhao/readme-builder
#  6. Collapse sub-packages to module root: strip everything after module root
unknown_modules=()
found_modules=()

# jq extracts all purl values, filtering out empty/null ones
while IFS= read -r purl; do
  [[ -z "$purl" ]] && continue

  # Strip everything after and including "?" (the query string)
  path="${purl%%\?*}"

  # Extract the module path: pkg:golang/path/to/module@version
  # Remove the "pkg:golang/" prefix
  path="${path#pkg:golang/}"

  # Strip the version after "@" (if present)
  path="${path%@*}"

  # Strip trailing version segments that may be embedded in the path (stdlib only).
  # Some stdlib like crypto/internal/entropy/v1.0.0 embed versions as /vN.Y.Z
  # Only strip for paths that don't have a domain (no dots initially, or start with internal/).
  # Third-party paths like github.com/foo/bar/v2 keep their version part since v2 is part of the module name.
  path_check="$path"
  # Strip trailing /vN or /vN.Y.Z from the end (for stdlib with embedded versions)
  path_check=$(sed 's|/v[0-9]\+\(\.[0-9]\+\)*$||' <<< "$path_check")

  # Skip stdlib and repo-internal packages:
  # - Stdlib: no dot in path after version stripping (bufio, bytes, cmp, fmt, compress/flate, encoding/json, crypto/internal/entropy, math/rand/v2)
  # - Repo-internal: starts with internal/ (internal/foo, internal/bar)
  # Real third-party modules always have a domain (dot): github.com, golang.org, etc.
  # Third-party modules with internal/ sub-packages (e.g., github.com/foo/bar/internal/baz)
  # should NOT be skipped — they have dots and need to be checked.
  # NOTE: The [[ "$path_check" =~ ^internal/ ]] disjunct is currently unreachable with
  # today's purl shapes — any bare internal/ path has no dots and is already caught by
  # the first condition. Kept as defense-in-depth in case hermeto's output format changes.
  if [[ ! "$path_check" =~ \. ]] || [[ "$path_check" =~ ^internal/ ]]; then
    continue
  fi

  # Skip vendor/ prefix packages (vendored modules)
  if [[ "$path" =~ ^vendor/ ]]; then
    continue
  fi

  # Skip the main module itself (github.com/Odilhao/readme-builder/...)
  if [[ "$path" =~ ^github\.com/Odilhao/readme-builder ]]; then
    continue
  fi

  # Collapse sub-packages to module root.
  # For third-party packages, the module root is the first two path segments
  # e.g., github.com/google/go-github/v89/github -> github.com/google/go-github/v89
  # Logic: split by "/" and take segments until we find a version-like segment or pass the org level.
  # Simplified: for paths like "github.com/ORG/REPO/...", extract "github.com/ORG/REPO"
  # For paths like "golang.org/x/text/...", extract "golang.org/x/text"
  IFS='/' read -ra segments <<< "$path"

  # Determine module root based on domain pattern
  if [[ "${segments[0]}" == "github.com" || "${segments[0]}" == "gitlab.com" ]]; then
    # github.com/ORG/REPO or github.com/ORG/REPO/v2 or github.com/ORG/REPO/subpackage
    # Take domain, org, and repo; if the third segment starts with 'v' followed by digit, include it
    module="${segments[0]}/${segments[1]}/${segments[2]}"
    if [[ ${#segments[@]} -gt 3 ]] && [[ "${segments[3]}" =~ ^v[0-9] ]]; then
      module="${module}/${segments[3]}"
    fi
  elif [[ "${segments[0]}" == "golang.org" ]]; then
    # golang.org/x/text or golang.org/x/text/subpackage
    # Take domain, x, and the next segment (text, net, crypto, etc.)
    module="${segments[0]}/${segments[1]}/${segments[2]}"
  elif [[ "${segments[0]}" == "gopkg.in" ]]; then
    # gopkg.in/yaml.v3
    module="${segments[0]}/${segments[1]}"
  else
    # Unknown domain pattern; use the full path as-is
    module="$path"
  fi

  if [[ -z "$module" ]]; then
    echo "::warning::Could not parse module from purl: $purl (path: $path)" >&2
    continue
  fi

  found_modules+=("$module")

  # Check if module is in whitelist (exact match)
  if [[ ! -v "WHITELISTED_MODULES[$module]" ]]; then
    unknown_modules+=("$module")
  fi
done < <(jq -r '.components[]?.purl // empty' "$SBOM_FILE")

# Report found modules
if [[ ${#found_modules[@]} -gt 0 ]]; then
  printf 'Found %d module(s) in SBOM:\n' "${#found_modules[@]}"
  printf '  - %s\n' "${found_modules[@]}" | sort -u
  echo ""
fi

# Report unknown modules
if [[ ${#unknown_modules[@]} -gt 0 ]]; then
  echo "::error::Unknown module(s) not in whitelist:" >&2
  printf '  - %s\n' "${unknown_modules[@]}" | sort -u >&2
  echo "" >&2
  echo "Whitelisted modules:" >&2
  for mod in "${!WHITELISTED_MODULES[@]}"; do
    printf '  - %s (%s)\n' "$mod" "${WHITELISTED_MODULES[$mod]}"
  done | sort >&2
  exit 1
fi

if [[ ${#found_modules[@]} -eq 0 ]]; then
  echo "::error::No modules found in SBOM; this may indicate an empty or malformed SBOM" >&2
  exit 1
fi

printf 'All %d module(s) are whitelisted.\n' "${#found_modules[@]}"
exit 0
