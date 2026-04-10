#!/usr/bin/env bash
# notify-sms.sh — Send a deployment notification via Twilio SMS.
#
# Usage: ./scripts/notify-sms.sh <message>
#
# Reads Twilio credentials from Vault at secret/navi/{env}/twilio using
# VAULT_TOKEN and NAVI_ENV sourced from /opt/navi/.env. Exits 0 silently
# if credentials are not configured — SMS is best-effort, never a blocker.
set -euo pipefail

MESSAGE="${1:-}"
if [[ -z "$MESSAGE" ]]; then
  echo "Usage: $0 <message>" >&2
  exit 1
fi

ENV_FILE="$(dirname "$0")/../.env"
if [[ -f "$ENV_FILE" ]]; then
  # shellcheck source=/dev/null
  set -a; source "$ENV_FILE"; set +a
fi

if [[ -z "${VAULT_TOKEN:-}" ]]; then
  echo "VAULT_TOKEN not set — skipping SMS notification."
  exit 0
fi

VAULT_ADDR="${VAULT_ADDR:-https://10.0.40.10:8200}"
VAULT_CACERT="${VAULT_CACERT:-/opt/foundation/vault/tls/vault-ca.crt}"
ENV="${NAVI_ENV:-prod}"
VAULT_PATH="secret/data/navi/$ENV/twilio"

# Fetch Twilio credentials from Vault.
TWILIO_SECRET=$(curl -sf \
  --cacert "$VAULT_CACERT" \
  -H "X-Vault-Token: $VAULT_TOKEN" \
  "$VAULT_ADDR/v1/$VAULT_PATH" 2>/dev/null) || true

if [[ -z "$TWILIO_SECRET" ]]; then
  echo "Could not reach Vault for Twilio credentials — skipping SMS notification."
  exit 0
fi

TWILIO_ACCOUNT_SID=$(echo "$TWILIO_SECRET" | jq -r '.data.data.account_sid // empty')
TWILIO_AUTH_TOKEN=$(echo "$TWILIO_SECRET" | jq -r '.data.data.auth_token // empty')
TWILIO_FROM_NUMBER=$(echo "$TWILIO_SECRET" | jq -r '.data.data.from_number // empty')
TWILIO_TO_NUMBER=$(echo "$TWILIO_SECRET" | jq -r '.data.data.to_number // empty')

if [[ -z "$TWILIO_ACCOUNT_SID" ]]; then
  echo "Twilio credentials not configured in Vault — skipping SMS notification."
  exit 0
fi

curl -s -X POST \
  "https://api.twilio.com/2010-04-01/Accounts/$TWILIO_ACCOUNT_SID/Messages.json" \
  -u "$TWILIO_ACCOUNT_SID:$TWILIO_AUTH_TOKEN" \
  --data-urlencode "From=$TWILIO_FROM_NUMBER" \
  --data-urlencode "To=$TWILIO_TO_NUMBER" \
  --data-urlencode "Body=$MESSAGE" \
  > /dev/null

echo "SMS sent: $MESSAGE"
