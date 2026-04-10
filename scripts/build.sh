#!/usr/bin/env bash
# build.sh — Build Docker images for services that changed since the last deploy.
#
# Usage: ./scripts/build.sh <version>
#
# Reads .last-deployed-version to determine the change window. On first run
# (value "none") all services are built.
set -euo pipefail

VERSION="${1:-}"
if [[ -z "$VERSION" ]]; then
  echo "Usage: $0 <version>" >&2
  exit 1
fi

REPO="ghcr.io/rutabageldev/navi"
PREV=$(cat .last-deployed-version 2>/dev/null || echo "none")

CHANGED=$(./scripts/detect-changes.sh "$PREV" "$VERSION")

if [[ -z "$CHANGED" ]]; then
  echo "No service changes detected since $PREV — skipping image build."
  # Signal to CI that no image was produced so deploy steps are skipped.
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    echo "built=false" >> "$GITHUB_OUTPUT"
  fi
  exit 0
fi

for SERVICE in $CHANGED; do
  IMAGE="$REPO-$SERVICE:$VERSION"
  echo "Building $IMAGE ..."
  docker build \
    --build-arg VERSION="$VERSION" \
    -f "services/$SERVICE/Dockerfile" \
    -t "$IMAGE" \
    .
  echo "Built $IMAGE"
done

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  echo "built=true" >> "$GITHUB_OUTPUT"
fi
