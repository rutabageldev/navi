#!/usr/bin/env bash
# deploy.sh — Deploy a service to an environment.
#
# Usage: ./scripts/deploy.sh <env> <version> [service]
#
# Requires VAULT_TOKEN in the environment (set by CI or sourced from .env).
# NAVI_HOST defaults to 10.0.40.10 if not set.
set -euo pipefail

ENV="${1:-}"
VERSION="${2:-}"
SERVICE="${3:-digest}"

if [[ -z "$ENV" || -z "$VERSION" ]]; then
  echo "Usage: $0 <env> <version> [service]" >&2
  exit 1
fi

# Load .env — authoritative source of VAULT_TOKEN for both manual runs and CI
# (the deploy workflow runs on the self-hosted runner, so this file is always present).
ENV_FILE="$(dirname "$0")/../.env"
if [[ -f "$ENV_FILE" ]]; then
  # shellcheck source=/dev/null
  set -a; source "$ENV_FILE"; set +a
fi

if [[ -z "${VAULT_TOKEN:-}" ]]; then
  echo "ERROR: VAULT_TOKEN is not set and could not be sourced from $ENV_FILE" >&2
  exit 1
fi

case "$ENV" in
  dev)     COMPOSE_FILE="docker-compose.dev.yml" ;;
  staging) COMPOSE_FILE="docker-compose.staging.yml" ;;
  prod)    COMPOSE_FILE="docker-compose.yml" ;;
  *)
    echo "unknown env: $ENV (want dev|staging|prod)" >&2
    exit 1
    ;;
esac

echo "Deploying $SERVICE $VERSION to $ENV ..."
NAVI_VERSION="$VERSION" NAVI_HOST="${NAVI_HOST:-10.0.40.10}" \
  docker compose -p "navi-${ENV}" -f "$COMPOSE_FILE" up -d "$SERVICE"
echo "Deployed $SERVICE $VERSION to $ENV"
