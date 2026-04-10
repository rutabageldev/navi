#!/usr/bin/env bash
# detect-changes.sh — Identify which services have changed between two versions.
#
# Usage: ./scripts/detect-changes.sh <prev-version> <new-version>
#
# Outputs a space-separated list of service names (e.g. "digest").
# If services/internal changed, all services are considered changed.
# If prev-version is "none" or does not exist as a git ref, all services
# are returned (first deploy or unknown baseline).
set -euo pipefail

PREV="${1:-none}"
NEW="${2:-HEAD}"

# Unknown baseline → rebuild everything.
if [[ "$PREV" == "none" ]] || ! git rev-parse --verify "$PREV" >/dev/null 2>&1; then
  echo "digest"
  exit 0
fi

CHANGED=$(git diff --name-only "$PREV" "$NEW" 2>/dev/null || echo "")

# Shared internal packages affect every service.
if echo "$CHANGED" | grep -q "^services/internal/"; then
  echo "digest"
  exit 0
fi

SERVICES=""
if echo "$CHANGED" | grep -q "^services/digest/"; then
  SERVICES="digest"
fi

# Workflow or script changes should trigger a rebuild too.
if echo "$CHANGED" | grep -qE "^(\.github/workflows/deploy|scripts/(build|deploy|rollback))"; then
  SERVICES="digest"
fi

echo "$SERVICES"
