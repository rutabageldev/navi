#!/usr/bin/env bash
# renew-vault-token.sh — Renew the Navi Vault token before it expires.
#
# Reads VAULT_TOKEN and VAULT_ADDR from /opt/navi/.env, then calls
# vault token renew. Emits a structured JSON log line on success and on
# failure. Exits non-zero on failure so the cron scheduler can detect it.
#
# Intended to be run weekly via cron (see: make install-cron).
# Safe to run at any time — renewal is idempotent and resets the TTL to
# the token's full period.
set -euo pipefail

ENV_FILE="$(dirname "$0")/../.env"

if [ ! -f "$ENV_FILE" ]; then
    printf '{"service":"navi","event":"vault_token_renewal_failed","error":"env file not found: %s","ts":"%s"}\n' \
        "$ENV_FILE" "$(date -u +%FT%TZ)"
    exit 1
fi

# shellcheck source=/dev/null
source "$ENV_FILE"

: "${VAULT_TOKEN:?VAULT_TOKEN must be set in .env}"
VAULT_ADDR="${VAULT_ADDR:-https://10.0.40.10:8200}"

VAULT_CACERT=${VAULT_CACERT:-/opt/foundation/vault/tls/vault-ca.crt}
export VAULT_CACERT

result=$(VAULT_ADDR="$VAULT_ADDR" VAULT_TOKEN="$VAULT_TOKEN" \
    vault token renew -format=json 2>&1) || {
    printf '{"service":"navi","event":"vault_token_renewal_failed","error":"%s","ts":"%s"}\n' \
        "$(printf '%s' "$result" | tr '"' "'")" "$(date -u +%FT%TZ)"
    exit 1
}

ttl=$(printf '%s' "$result" | \
    python3 -c "import sys,json; print(json.load(sys.stdin)['auth']['lease_duration'])" \
    2>/dev/null || echo "unknown")

printf '{"service":"navi","event":"vault_token_renewed","ttl_seconds":%s,"ts":"%s"}\n' \
    "$ttl" "$(date -u +%FT%TZ)"
