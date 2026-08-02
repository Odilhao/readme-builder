#!/usr/bin/env bash
# Test the license checker with sample SBOMs.
# This script validates that check-licenses.sh correctly accepts good SBOMs
# and rejects bad ones.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHECK_SCRIPT="$SCRIPT_DIR/check-licenses.sh"
TEST_DATA="$SCRIPT_DIR/testdata"

echo "Running license checker tests..."
echo ""

# Test 1: Good BOM should pass
echo "Test 1: Good BOM (all known modules)"
mkdir -p hermeto-output
cp "$TEST_DATA/sbom-good.json" hermeto-output/bom.json
if bash "$CHECK_SCRIPT"; then
  echo "✓ PASS: Good BOM accepted"
else
  echo "✗ FAIL: Good BOM rejected"
  exit 1
fi
echo ""

# Test 2: Bad BOM should fail
echo "Test 2: Bad BOM (unknown module)"
cp "$TEST_DATA/sbom-bad.json" hermeto-output/bom.json
if bash "$CHECK_SCRIPT"; then
  echo "✗ FAIL: Bad SBOM accepted (should have been rejected)"
  exit 1
else
  echo "✓ PASS: Bad SBOM rejected"
fi
echo ""

# Cleanup
rm -rf hermeto-output

echo "All tests passed!"
exit 0
