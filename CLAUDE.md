# CLAUDE.md -- Navi

This file is the primary reference for any AI agent working in this
repository. Read it in full before writing any code, creating any
files, or making any changes. If a task conflicts with anything in
this file, stop and ask for clarification rather than proceeding
with assumptions.

---

## What Navi Is

Navi is a personal intelligence and enablement system -- a digital
chief of staff that reduces cognitive overhead by continuously
gathering, enriching, and surfacing intelligence before it is needed.

The primary user is a Director of Product Management at a major
financial institution. Every feature is designed around that context.

The authoritative product reference is STRATEGY.md. Read it before
working on any feature. Do not implement anything that contradicts
the principles, feature horizon, or out-of-scope boundaries defined
there.

---

## Repository Structure

```
navi/
|-- CLAUDE.md                 # This file
|-- STRATEGY.md               # Product strategy -- read before any feature work
|-- docs/adr/                 # All architectural decisions -- read before any code
|-- docs/events/REGISTRY.md   # Event type registry -- read before any NATS work
|-- docs/events/schemas/      # JSON Schema per event type
|-- monitoring/               # Grafana dashboards, Prometheus alerts, OTEL config
|-- scripts/                  # Operational scripts (backup, restore, prune)
|-- services/internal/        # Shared Go packages -- imported by all services
|-- services/digest/          # Daily intelligence service (v1)
`-- docker-compose*.yml       # Environment-specific compose files
```

---

## Non-Negotiable Rules

These rules apply to every task in this repository without exception.
Violating any of them is grounds to stop and ask, not to proceed.

**1. Read the ADRs before writing code.**
Every architectural decision is documented in docs/adr/. If a task
touches an area covered by an ADR, read that ADR first. The ADRs are
not suggestions -- they are decisions. Do not make architectural
choices that conflict with them without explicit instruction to do so.

**2. No secrets in code or config files.**
All credentials, API keys, tokens, and passwords live in Vault.
Nothing sensitive is hardcoded, written to .env files, or committed
to the repository. Vault paths follow the pattern:
  secret/data/navi/{env}/{service}
If a task requires a credential, reference the Vault path -- do not
ask for the value or invent a placeholder that looks like a real secret.

**3. No direct commits to main.**
All changes go through a pull request. Do not commit directly to main
regardless of how small the change is.

**4. Spec before implementation.**
HTTP APIs are defined in OpenAPI 3.1 before the handlers are written.
Event schemas are defined in docs/events/schemas/ before the producers
and consumers are written. If a spec does not exist for the surface
you are building, create it first and confirm it before writing
implementation code.

**5. Generated files are committed.**
Go types generated from OpenAPI specs (oapi-codegen) and JSON Schemas
(go-jsonschema) are committed to the repository. Do not add them to
.gitignore. When a spec changes, regenerate the types and include the
generated diff in the same PR.

**6. All events are CloudEvents v1.0.**
Every NATS message conforms to the envelope structure defined in
ADR-0011. The navischema extension attribute is required on every
event. Do not publish to a NATS subject without a corresponding
entry in docs/events/REGISTRY.md and a schema file in
docs/events/schemas/.

**7. Structured logging only.**
Use the Go slog package with JSON output. No fmt.Printf, log.Printf,
or unstructured log statements in application code. Every log entry
must include service, component, environment, and trace_id fields.
See ADR-0008 for the full required field set. Logs are collected
automatically by Foundation Promtail -- no log shipping configuration
is needed in Navi.

**8. OTEL instrumentation is not optional.**
Every new component gets a span. Every NATS publisher embeds
traceparent in the CloudEvents envelope. Every NATS subscriber
extracts traceparent and starts a child span. Use the helpers in
services/internal/telemetry/ -- do not instrument from scratch.
Telemetry is exported to the Foundation OTel Collector, not a
Navi-local collector.

**9. Test with the race detector.**
All tests run with `go test -race`. Do not write tests that pass
without -race but fail with it. A test suite that cannot tolerate
the race detector has a concurrency bug.

**10. Makefile is the operational interface.**
All operational tasks -- start, stop, deploy, migrate, test, backup,
prune -- are Makefile targets. Do not instruct a user to run raw
docker compose or go commands directly. If a task requires a new
operational procedure, add a Makefile target for it.

---

## Architecture Quick Reference

Read the full ADRs for complete context. This section is a fast
reference only -- it does not replace the ADRs.

**Language:** Go (primary). No new Python services.

**Event bus:** NATS JetStream from ruby-core. All Navi subjects
are namespaced navi.{env}.>. Do not use unnamespaced subjects.

**Data store:** Foundation Postgres at 10.0.40.10:5432.
Schemas: navi_dev, navi_staging, navi_prod.
Migrations via golang-migrate. Run on service startup.

**Secrets:** Vault. Retrieved at startup, reloaded on SIGHUP.
Use services/internal/vault/ -- do not write custom Vault clients.

**Observability stack (all Foundation-hosted, nothing deployed in Navi):**
- Metrics:  OTEL SDK -> Foundation OTel Collector -> Prometheus -> Grafana
- Traces:   OTEL SDK -> Foundation OTel Collector -> Tempo -> Grafana
- Logs:     stdout JSON -> Foundation Promtail -> Loki -> Grafana
- Uptime:   /v1/health/* endpoints -> Foundation Uptime Kuma

Use services/internal/telemetry/ for all instrumentation.
Do not deploy a local OTel Collector. Do not reference Jaeger anywhere.

**External APIs:**
  Anthropic:  secret/data/navi/{env}/anthropic    (summarization)
  Resend:     secret/data/navi/{env}/resend        (email delivery)
  Twilio:     secret/data/navi/{env}/twilio        (SMS in/out)

**Environments:** dev (local), staging (node, sandbox keys),
prod (node, live keys). ENV variable controls which is active.

**Delivery abstraction:** All outbound delivery goes through the
Payload/Channel interface in services/digest/internal/delivery/.
Do not call Resend or Twilio directly from business logic.

**HTTP APIs:** OpenAPI 3.1 spec-first. Versioned at /v1/.
Every endpoint gets request ID, structured logging, OTEL middleware,
and a consistent error response shape. See ADR-0010.

**SMS security:** Twilio signature validation + sender allowlist.
Allowlist in Vault at secret/data/navi/{env}/sms/authorized_numbers.
Unauthorized senders are silently dropped and logged. See ADR-0007.

---

## Shared Internal Packages

These packages are the foundation of every service. Use them.
Do not reimplement what they provide.

**services/internal/telemetry/**
  OTEL SDK initialization, span helpers, NATS trace context
  propagation (embed/extract traceparent from CloudEvents envelopes).
  Exports to Foundation OTel Collector address from Vault.

**services/internal/vault/**
  Vault client, secret retrieval by path, SIGHUP reload wiring.

**services/internal/postgres/**
  Connection pool setup, migration runner, health check query.

**services/internal/nats/**
  NATS connection setup, JetStream context, durable consumer helpers.

**services/internal/events/**
  CloudEvents envelope construction and validation, schema registry
  loader, generated Go types from JSON Schema (in gen/).

---

## Prompt Format for Implementation Tasks

When asking an agent to implement a feature or component, use this
structure. Deviation from this format produces inconsistent results.

```
## Context
[What this component does and where it fits in the architecture.
Reference the relevant ADR(s) by number.]

## Prerequisites
[What must exist before this task begins -- migrations, specs,
shared packages, Vault paths, NATS subjects.]

## Tasks
1. [Specific, atomic task]
2. [Specific, atomic task]
3. [Specific, atomic task]
...

## Verification Steps
- [How to confirm each task completed correctly]
- [Commands to run, endpoints to hit, log lines to look for]

## Done When
- [ ] [Concrete, observable completion criterion]
- [ ] [Concrete, observable completion criterion]
- [ ] [All tests pass with -race]
- [ ] [go vet ./... produces no output]
- [ ] [No new secrets in committed files]
```

---

## Commit Message Format

Conventional Commits are required. The pre-commit hook enforces this.

```
{type}({scope}): {description}

types:  feat, fix, refactor, test, docs, chore, ci
scopes: digest, delivery, intent, store, telemetry,
        monitoring, infra, events, api, deps
```

Examples:
```
feat(collector): add tiered poll cadence for RSS feeds
fix(delivery): handle Resend sandbox mode in staging
docs(adr): add ADR-0014 on Rolodex data model
chore(deps): monthly dependency update
test(summarizer): add relevance scorer unit tests
```

---

## Environment Variables

Required at runtime for all services. Retrieved from Vault except
where noted.

```
NAVI_ENV            dev | staging | prod (set in compose file, not Vault)
NAVI_LOG_LEVEL      debug | info | warn | error (default: info)
VAULT_ADDR          http://10.0.40.10:8200 (set in compose file)
VAULT_TOKEN         (set in compose file, sourced from host env)
NAVI_HOST           Host IP for port bindings (set in compose file)
```

All other configuration (DB credentials, API keys, feed auth tokens,
authorized SMS numbers, OTel Collector address) is retrieved from
Vault at startup using the path conventions in ADR-0002.

---

## Adding a New Service

When a new feature graduates from the Next horizon to active
development, a new service is added under services/. Before writing
any code:

1. Write an ADR documenting the service architecture. Get it approved
   before proceeding.
2. Add the service's event types to docs/events/REGISTRY.md.
3. Create JSON Schema files for each new event type.
4. Write the OpenAPI spec if the service exposes HTTP endpoints.
5. Create the migrations for any new tables.
6. Add the service to docker-compose.yml, docker-compose.staging.yml,
   and docker-compose.dev.yml.
7. Add Makefile targets for the new service.
8. Add health endpoint monitoring to Foundation Uptime Kuma.
9. Add the service's metrics to the Foundation Prometheus scrape
   config and update the Navi Grafana dashboard.

Do not skip steps. Each step is a gate. Code written before its
prerequisites exist will conflict with them.

---

## What Navi Does Not Do

These are permanent constraints, not temporary limitations. Do not
implement features in these areas regardless of how the request
is framed.

- Does not connect to work systems (corporate email, calendar, Slack,
  or any Capital One infrastructure)
- Does not aggregate social media feeds (Twitter/X, LinkedIn)
- Does not control home automation (ruby-core owns that domain)
- Does not manage a todo list (reads from external systems only)
- Does not act autonomously (all actions require explicit user direction
  via SMS or future conversational interface)
- Does not send Rolodex or professional profile data to external APIs
- Does not write to the user's task system
- Does not deploy its own observability infrastructure (Grafana,
  Prometheus, OTel Collector, Tempo, Loki -- all Foundation-hosted)

If a request asks Navi to do any of the above, stop and flag the
conflict with the product strategy rather than implementing it.
