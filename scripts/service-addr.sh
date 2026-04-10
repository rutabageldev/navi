#!/usr/bin/env bash
# service-addr.sh — Output the host:port for a service in a given environment.
#
# Usage: ./scripts/service-addr.sh <env> [service]
set -euo pipefail

ENV="${1:-}"
SERVICE="${2:-digest}"

case "$ENV" in
  dev)
    case "$SERVICE" in
      digest) echo "127.0.0.1:8082" ;;
      *) echo "unknown service: $SERVICE" >&2; exit 1 ;;
    esac
    ;;
  staging)
    case "$SERVICE" in
      digest) echo "10.0.40.10:8081" ;;
      *) echo "unknown service: $SERVICE" >&2; exit 1 ;;
    esac
    ;;
  prod)
    case "$SERVICE" in
      digest) echo "10.0.40.10:8084" ;;
      *) echo "unknown service: $SERVICE" >&2; exit 1 ;;
    esac
    ;;
  *)
    echo "unknown env: $ENV (want dev|staging|prod)" >&2
    exit 1
    ;;
esac
