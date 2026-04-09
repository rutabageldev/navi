#!/usr/bin/env bash
# notify-sms.sh — Send a deployment notification via Twilio SMS.
#
# Usage: ./scripts/notify-sms.sh <message>
#
# Reads Twilio credentials from /opt/navi/.env. Exits 0 silently if
# credentials are not configured — SMS is best-effort, never a blocker.
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

if [[ -z "${TWILIO_ACCOUNT_SID:-}" ]]; then
  echo "Twilio credentials not configured — skipping SMS notification."
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
