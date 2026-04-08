#!/usr/bin/env bash
# Validate that Phase 3b prerequisites are satisfied:
#   - Vault is reachable with the Navi token
#   - All required NATS secret paths exist with the correct fields
#   - A live NATS connection succeeds using the actual credentials from Vault
#
# Usage: ./scripts/validate-phase3b.sh [dev|staging|prod]
# Defaults to all three environments if no argument is given.
set -euo pipefail

VAULT_CACERT=${VAULT_CACERT:-/opt/foundation/vault/tls/vault-ca.crt}
ENV_FILE="$(dirname "$0")/../.env"

# Load VAULT_ADDR and VAULT_TOKEN from .env if not already set
if [ -f "$ENV_FILE" ]; then
    set -a; source "$ENV_FILE"; set +a
fi

export VAULT_CACERT
PASS=0
FAIL=0
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

green() { printf '\033[32m✓\033[0m %s\n' "$*"; }
red()   { printf '\033[31m✗\033[0m %s\n' "$*"; }

check() {
    local label="$1"; shift
    if "$@" > /dev/null 2>&1; then
        green "$label"
        PASS=$((PASS+1))
    else
        red "$label"
        FAIL=$((FAIL+1))
    fi
}

vault_field() {
    local path="$1" field="$2"
    vault kv get -field="$field" "$path" > /dev/null 2>&1
}

# ─── 1. Vault connectivity ────────────────────────────────────────────────────
echo
echo "── Vault ────────────────────────────────────────────────────────────────"
check "Vault reachable and token valid" vault token lookup

# ─── 2. Vault path validation ─────────────────────────────────────────────────
ENVS=("${@:-dev staging prod}")
[ $# -eq 0 ] && ENVS=(dev staging prod)

echo
echo "── Vault paths ──────────────────────────────────────────────────────────"
for env in "${ENVS[@]}"; do
    base="secret/navi/$env"
    check "$env: $base/nats       — field: url"  vault_field "$base/nats" url
    check "$env: $base/nats       — field: seed" vault_field "$base/nats" seed
    check "$env: $base/nats/tls   — field: cert" vault_field "$base/nats/tls" cert
    check "$env: $base/nats/tls   — field: key"  vault_field "$base/nats/tls" key
    check "$env: $base/nats/tls   — field: ca"   vault_field "$base/nats/tls" ca
done

# ─── 3. Live NATS connection test ─────────────────────────────────────────────
echo
echo "── NATS connectivity ────────────────────────────────────────────────────"
for env in "${ENVS[@]}"; do
    base="secret/navi/$env"

    # Fetch credentials from Vault into temp files
    SEED_FILE="$TMPDIR/nats-$env.seed"
    CERT_FILE="$TMPDIR/nats-$env.crt"
    KEY_FILE="$TMPDIR/nats-$env.key"
    CA_FILE="$TMPDIR/nats-$env.ca"

    if ! vault kv get -field=seed "$base/nats"     > "$SEED_FILE" 2>/dev/null || \
       ! vault kv get -field=cert "$base/nats/tls" > "$CERT_FILE" 2>/dev/null || \
       ! vault kv get -field=key  "$base/nats/tls" > "$KEY_FILE"  2>/dev/null || \
       ! vault kv get -field=ca   "$base/nats/tls" > "$CA_FILE"   2>/dev/null; then
        red "$env: could not fetch NATS credentials from Vault (skipping connection test)"
        FAIL=$((FAIL+1))
        continue
    fi

    NATS_URL=$(vault kv get -field=url "$base/nats" 2>/dev/null)

    check "$env: NATS connect + publish navi.$env.validate" \
        nats pub "navi.$env.validate" "phase3b-validation" \
            --server "$NATS_URL" \
            --nkey "$SEED_FILE" \
            --tlscert "$CERT_FILE" \
            --tlskey "$KEY_FILE" \
            --tlsca "$CA_FILE"

    check "$env: NATS publish audit.navi.validate" \
        nats pub "audit.navi.validate" "phase3b-validation" \
            --server "$NATS_URL" \
            --nkey "$SEED_FILE" \
            --tlscert "$CERT_FILE" \
            --tlskey "$KEY_FILE" \
            --tlsca "$CA_FILE"
done

# ─── Summary ──────────────────────────────────────────────────────────────────
echo
echo "── Summary ──────────────────────────────────────────────────────────────"
echo "   passed: $PASS   failed: $FAIL"
echo

[ "$FAIL" -eq 0 ]
