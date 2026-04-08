#!/usr/bin/env bash
# vault-seed.sh — Seed placeholder Vault paths for a Navi environment.
#
# Usage: ./scripts/vault-seed.sh [dev|staging|prod]
#
# Writes initial placeholder values for all Navi-owned secret paths.
# Skips any path that already exists — existing values are never overwritten.
#
# Does NOT touch nats or nats/tls paths. Those are owned and seeded
# by Foundation (NKEY generation and client cert issuance).
#
# Real secret values (postgres password, API keys, phone numbers) must
# be set manually in Vault before the service can pass a /v1/health/ready
# check. This script is safe to run multiple times.
set -euo pipefail

ENV="${1:-}"
if [[ "$ENV" != "dev" && "$ENV" != "staging" && "$ENV" != "prod" ]]; then
    printf 'Usage: %s [dev|staging|prod]\n' "$0" >&2
    exit 1
fi

VAULT_CACERT=${VAULT_CACERT:-/opt/foundation/vault/tls/vault-ca.crt}
ENV_FILE="$(dirname "$0")/../.env"

if [ -f "$ENV_FILE" ]; then
    # shellcheck source=/dev/null
    set -a; source "$ENV_FILE"; set +a
fi

export VAULT_CACERT

BASE="secret/navi/$ENV"

# seed_if_absent writes a KV secret only when the path does not already
# exist. This prevents re-runs from clobbering operator-supplied values.
seed_if_absent() {
    local path="$1"; shift
    if vault kv get "$path" >/dev/null 2>&1; then
        printf '  %-45s already exists — skipped\n' "$path"
    else
        vault kv put "$path" "$@"
        printf '  %-45s seeded\n' "$path"
    fi
}

printf '\nSeeding Vault paths for environment: %s\n\n' "$ENV"

# ── Postgres ────────────────────────────────────────────────────────────────
# host and schema are always known; password must be changed before first run.
seed_if_absent "$BASE/postgres" \
    host=10.0.40.10 \
    port=5432 \
    user=navi \
    password=CHANGE_ME \
    database=navi \
    schema="navi_$ENV"

# ── Telemetry (OTel Collector) ───────────────────────────────────────────────
seed_if_absent "$BASE/telemetry" \
    endpoint=10.0.40.10:4317

# ── Resend ───────────────────────────────────────────────────────────────────
seed_if_absent "$BASE/resend" \
    api_key=PLACEHOLDER \
    from_address=navi@example.com \
    to_address=CHANGE_ME

# ── Twilio ───────────────────────────────────────────────────────────────────
seed_if_absent "$BASE/twilio" \
    account_sid=PLACEHOLDER \
    auth_token=PLACEHOLDER \
    from_number=PLACEHOLDER \
    to_number=PLACEHOLDER \
    webhook_url=PLACEHOLDER

# ── Anthropic ────────────────────────────────────────────────────────────────
seed_if_absent "$BASE/anthropic" \
    api_key=PLACEHOLDER \
    model=claude-sonnet-4-6

# ── SMS authorized numbers ───────────────────────────────────────────────────
seed_if_absent "$BASE/sms/authorized_numbers" \
    numbers="+1CHANGEME"

printf '\nDone. NATS paths (nats, nats/tls) were not modified — owned by Foundation.\n\n'

# Remind operator which placeholder values still need real data.
printf 'Paths requiring real values before service startup:\n'
printf '  %s/postgres     — set password\n' "$BASE"
printf '  %s/resend       — set api_key, to_address\n' "$BASE"
printf '  %s/twilio       — set all fields\n' "$BASE"
printf '  %s/anthropic    — set api_key\n' "$BASE"
printf '  %s/sms/authorized_numbers — set numbers\n\n' "$BASE"
