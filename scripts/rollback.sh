#!/usr/bin/env bash
# rollback.sh — Roll a service back to a previous version.
#
# Usage: ./scripts/rollback.sh <env> <version> [service]
#
# Delegates to deploy.sh with the rollback version.
set -euo pipefail

ENV="${1:-}"
VERSION="${2:-}"
SERVICE="${3:-digest}"

if [[ -z "$ENV" || -z "$VERSION" ]]; then
  echo "Usage: $0 <env> <version> [service]" >&2
  exit 1
fi

if [[ "$VERSION" == "none" ]]; then
  echo "Rollback version is 'none' — no previous version to roll back to." >&2
  echo "If this is the first deploy, bring the service down instead:" >&2
  echo "  docker compose -f docker-compose.<env>.yml down $SERVICE" >&2
  exit 1
fi

echo "Rolling back $SERVICE in $ENV to $VERSION ..."
"$(dirname "$0")/deploy.sh" "$ENV" "$VERSION" "$SERVICE"
