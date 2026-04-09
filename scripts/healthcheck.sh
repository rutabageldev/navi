#!/usr/bin/env bash
# healthcheck.sh — Poll a service's /v1/health/ready until it returns 200.
#
# Usage: ./scripts/healthcheck.sh <env> [service]
#
# Exits 0 on success, 1 if the service does not become healthy within the window.
set -euo pipefail

ENV="${1:-}"
SERVICE="${2:-digest}"
MAX_ATTEMPTS=30
INTERVAL=5

ADDR=$(./scripts/service-addr.sh "$ENV" "$SERVICE")
URL="http://$ADDR/v1/health/ready"

echo "Waiting for $SERVICE ($ENV) at $URL ..."

for i in $(seq 1 "$MAX_ATTEMPTS"); do
  STATUS=$(curl -o /dev/null -sw "%{http_code}" --max-time 5 "$URL" 2>/dev/null || echo "000")
  if [[ "$STATUS" == "200" ]]; then
    echo "healthy (attempt $i)"
    exit 0
  fi
  echo "attempt $i/$MAX_ATTEMPTS: got $STATUS — retrying in ${INTERVAL}s"
  sleep "$INTERVAL"
done

echo "ERROR: $SERVICE ($ENV) did not become healthy within $((MAX_ATTEMPTS * INTERVAL))s" >&2
exit 1
