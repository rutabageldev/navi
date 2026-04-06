# ADR-0007: SMS Security

## Status
Accepted

## Date
2026-04-06

## Context

Navi exposes a Twilio webhook endpoint that accepts inbound SMS
messages and routes them to the intent parser, which can trigger real
actions: creating calendar events, updating the professional profile,
adding Rolodex entries, and future actions as the system grows. This
makes the inbound SMS surface a meaningful attack vector that requires
explicit security controls.

ADR-0006 documents the delivery and inbound channel abstraction and
mentions Twilio signature validation as a protection mechanism. That
control is necessary but not sufficient. This ADR documents the
complete threat model for the SMS channel and the controls applied
at each layer.

## Threat Model

### Threat 1: Spoofed Webhook Requests

An attacker crafts a direct HTTP POST to the Twilio webhook endpoint
at `POST /webhooks/twilio/inbound`, bypassing Twilio entirely and
injecting arbitrary message content.

**Likelihood:** Moderate. The endpoint must be publicly reachable for
Twilio to deliver messages. Any actor who discovers the URL could
attempt this.

**Control:** Twilio request signature validation. Every legitimate
request from Twilio includes an `X-Twilio-Signature` header computed
using the account auth token, the full request URL, and the POST
parameters. The webhook handler rejects any request that fails
signature validation before any processing occurs. Requests without
a valid signature receive a 403 and are not logged with content.

This control is reliable. A forged signature requires knowledge of
the Twilio auth token, which lives in Vault and is never exposed.

### Threat 2: Unauthorized SMS Senders

A real SMS is sent to the Navi Twilio number from an unauthorized
phone number. Twilio delivers it as a legitimate inbound message,
signed correctly. Without an additional control, Navi would process
it as a valid command.

This is the primary residual threat after signature validation.
Anyone who discovers or guesses the Twilio number could attempt to
issue commands to Navi.

**Likelihood:** Low but non-negligible. The Twilio number is not
publicly advertised but is not a secret in the same sense as a
credential. It could be discovered through message metadata, shared
inadvertently, or simply guessed.

**Control:** Sender allowlist. See decision below.

### Threat 3: SMS Number Spoofing

An attacker spoofs the authorized phone number at the message
metadata layer to make Navi believe the message originated from
an authorized sender.

**Likelihood:** Very low in practice. SMS display number spoofing --
the technique used in phishing and scam texts -- operates at the
message metadata layer and is not reflected in the carrier-verified
originating number that Twilio receives. Twilio sees the actual
originating number from the carrier network. Reliably falsifying
the carrier-level originating number requires access to carrier
infrastructure and is not a practical opportunistic attack.

**Residual risk:** A SIM swap attack -- where an attacker social-
engineers the authorized user's carrier into transferring their number
to a new SIM -- would allow an attacker to send messages that
appear to originate from the authorized number. This is a real and
documented attack vector. It is targeted, requires significant effort,
and cannot be fully mitigated by software controls.

**Control:** No software control fully addresses SIM swap. The
mitigations are operational: strong carrier account PIN, carrier-level
SIM swap protection where available, and awareness that this risk
exists. It is documented here and accepted as residual risk.

### Threat 4: Replay Attacks

An attacker captures a valid, signed Twilio webhook request and
replays it to issue the same command multiple times.

**Likelihood:** Low. Requires interception of a valid HTTPS request,
which requires either a TLS compromise or access to the network path.

**Control:** Twilio includes a timestamp in the `X-Twilio-Signature`
validation scheme. The webhook handler rejects requests with a
timestamp older than 5 minutes. This makes replay attacks impractical
without a near-real-time interception capability.

---

## Decision

### Control 1: Twilio Request Signature Validation

Every inbound request to `POST /webhooks/twilio/inbound` MUST be
validated against the `X-Twilio-Signature` header before any other
processing. Requests that fail validation MUST be rejected with a 403.
This MUST be implemented using the official Twilio Go helper library.

Validation is performed against the full public URL of the webhook
endpoint (including scheme and path) as Twilio constructs the
signature. The URL is configured in Vault at startup; it is not
inferred from the request Host header, which could be spoofed.

```
Vault path: secret/data/navi/{env}/twilio
Required:   account_sid, auth_token, from_number, to_number,
            webhook_url
```

### Control 2: Sender Allowlist

After signature validation, the webhook handler checks the `From`
field of the inbound message against a configured allowlist of
authorized phone numbers. Numbers are stored in E.164 format.

The allowlist is stored in Vault and retrieved at service startup.
It is refreshed on SIGHUP without requiring a redeployment. This
means adding or removing an authorized number (for example, adding
Katie as an authorized sender for a future tailored feature) requires
only a Vault update and a signal, not a code change or release.

```
Vault path: secret/data/navi/{env}/sms/authorized_numbers
Format:     comma-separated E.164 numbers, e.g. +12025550101,+12025550102
```

Messages from numbers not on the allowlist MUST be silently dropped.
No response MUST NOT be sent to the unauthorized sender. Rationale: responding
with any message -- even a rejection -- confirms that the service
exists and is listening. Silent drop provides no information to an
attacker while logging the attempt internally for awareness.

All unauthorized inbound attempts are logged with the originating
number (truncated for privacy -- last 4 digits only) and published
to `navi.{env}.security.events` for observability.

### Control 3: Replay Prevention

The webhook handler MUST reject any request with an
`X-Twilio-Timestamp` value older than 5 minutes relative to server
time. This is enforced
as part of signature validation via the Twilio helper library. Server
time is synchronized via NTP.

### Control 4: Webhook URL Confidentiality

The webhook URL is treated as a low-sensitivity secret -- not a
credential, but not advertised. It is stored in Vault and configured
in Traefik via environment variable. It is not committed to the
repository. Obscurity is not a security control, but there is no
reason to make the endpoint discoverable.

---

## Residual Risk

After applying all controls, the following residual risks are
accepted:

| Risk | Likelihood | Rationale for Acceptance |
|------|------------|--------------------------|
| SIM swap | Very low | Targeted, high-effort attack. Mitigated operationally via carrier-level controls. Out of scope for software. |
| Carrier-level number spoofing | Extremely low | Requires carrier infrastructure access. Not a practical threat. |
| Authorized device compromise | Low | If the authorized phone is physically compromised, software controls cannot help. Standard device security practices apply. |

---

## Security Control Summary

| Layer | Control | Implemented by |
|-------|---------|----------------|
| Transport | TLS via Traefik | Infrastructure |
| Request authenticity | Twilio signature validation | Webhook handler |
| Replay prevention | 5-minute timestamp window | Webhook handler |
| Sender authorization | Phone number allowlist in Vault | Webhook handler |
| Endpoint discoverability | URL stored in Vault, not in repo | Operations |
| Unauthorized attempt logging | Internal log + NATS security event | Webhook handler |

## Consequences

**Positive:**
- The layered control model means no single failure compromises the
  system. An attacker who discovers the webhook URL still cannot issue
  commands without a valid Twilio signature. An attacker who sends
  from an unrecognized number is silently dropped even if the request
  is signed.
- Allowlist in Vault means authorized senders can be updated without
  a deployment.
- Silent drop on unauthorized senders provides no signal to
  opportunistic attackers.

**Negative / tradeoffs:**
- Silent drop means a misconfigured allowlist (wrong E.164 format,
  stale number after a phone change) will cause legitimate messages
  to be dropped with no feedback. Operational discipline on allowlist
  maintenance is required. A Makefile target (`make verify-sms-auth`)
  should be provided to test that the authorized number can reach
  the webhook correctly.
- SIM swap risk is accepted and cannot be fully mitigated in software.

**Neutral:**
- E.164 is the international standard for phone number format;
  adopting it creates no additional complexity and enables future
  international number support without format changes.
- The 5-minute timestamp replay window is Twilio's own documented
  recommendation; it is not a Navi-specific security decision.

## Alternatives Considered

**Respond to unauthorized senders with a rejection message**
Rejected. Any response confirms the service exists. Silent drop is
strictly better from a security standpoint and has no downside for
legitimate use since the allowlist is the only supported access path.

**PIN or passphrase in the message body as a second factor**
Considered. Adding a required prefix ("navi: put X on my calendar")
would add a lightweight second factor. Rejected for v1 on the grounds
that the allowlist plus Twilio signature validation is sufficient for
the threat model, and requiring a prefix adds friction inconsistent
with natural language interaction. Can be revisited if the threat
model changes.

**Mutual TLS on the webhook endpoint**
Rejected. Twilio does not support client certificates on webhook
delivery. Not applicable.
