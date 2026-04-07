# ADR-0002: Top-Level Architecture

## Status
Accepted

## Date
2026-04-06

## Context

With the repo scope established in ADR-0001, this ADR documents the
top-level architectural decisions that govern how Navi is built,
deployed, and integrated with the surrounding infrastructure. These
decisions span all services within the navi repository and establish
the patterns that individual service ADRs build on.

The existing homelab infrastructure consists of:
- Foundation: shared infrastructure monorepo hosting Postgres
  (10.0.40.10:5432), Vault, Traefik, and supporting services
- ruby-core: home automation backbone hosting NATS JetStream,
  Home Assistant, Zigbee2MQTT, and Mosquitto
- A single physical node (Protectli hardware) running all services
- Tailscale for emergency remote access
- Five VLANs on a 10.0.{VLAN}.x scheme; utility node at 10.0.40.10

Navi will be deployed on the same node. All architectural decisions
must be compatible with this single-node homelab constraint.

## Decision

### Deployment Model

All Navi services MUST be containerized using Docker Compose,
consistent with Foundation and ruby-core. Each service MUST have its
own Dockerfile. The top-level docker-compose.yml defines all services,
networks, and volume mounts. Environment-specific overrides are handled
via docker-compose.staging.yml and docker-compose.dev.yml.

All port bindings MUST use explicit host IP bindings via the
NAVI_HOST environment variable, consistent with Foundation's pattern
using FOUNDATION_HOST.

### Language

Go MUST be the primary implementation language for all Navi backend
services, consistent with the preference for Go over Python for new
services. The standard library and a small set of well-maintained
dependencies are preferred over heavy frameworks. No new Python
services MUST NOT be introduced.

### Event-Driven Internal Architecture

All inter-service communication within Navi MUST be event-driven via
NATS JetStream. The NATS instance deployed in ruby-core is used as the
shared event bus. All Navi subjects are namespaced under `navi.>` to
guarantee zero collision with ruby-core subjects.

**NATS Authentication and Transport**

The ruby-core NATS server requires two auth mechanisms from all clients:

- **NKEY authentication:** Each Navi service has an NKEY keypair. The
  private seed is stored in Vault at `secret/navi/{env}/nats` (field:
  `seed`). The public key is registered in the ruby-core NATS server's
  authorized users list (via `secret/ruby-core/nats/navi-{env}`). On
  connect, the client signs a server nonce with the seed to prove identity.

- **mTLS:** All connections use TLS 1.3 with mutual certificate
  verification. The client certificate and CA are stored in Vault at
  `secret/navi/{env}/nats/tls` (fields: `cert`, `key`, `ca`). The CA is
  the same CA used for all ruby-core NATS clients.

Per-environment NATS addresses:
- dev:  `tls://127.0.0.1:4222`
- prod: `tls://127.0.0.1:4223`

Credentials are fetched from Vault at startup via `loadNATSConfig()` in
`services/digest/cmd/digest/main.go`. The connection is established via
`services/internal/nats.Connect(Config)`, which constructs the NKEY
signing callback and `tls.Config` from the fetched material.

Subject ACLs enforced by the NATS server for Navi:
- publish:   `navi.{env}.>`, `$JS.API.>`, `$JS.ACK.>`
- subscribe: `navi.{env}.>`, `_INBOX.>`

Polling of external sources (RSS feeds, APIs) occurs only at the
system boundary -- in the collector component of the digest service.
Everything downstream of the collector is event-driven. This contains
the polling surface area and makes new features additive: a new
subscriber to an existing subject adds capability without modifying
existing components.

The core subject topology is:

```
navi.articles.collected     # raw article fetched, triggers enrichment
navi.articles.enriched      # entity-linked, ready for storage
navi.digest.ready           # digest generated, triggers delivery
navi.calendar.events        # future: calendar poller output
navi.rolodex.nudge          # future: relationship health alert
navi.sms.inbound            # inbound SMS from Twilio webhook
navi.sms.outbound           # outbound SMS to be sent via Twilio
```

All events MUST conform to the CloudEvents v1.0 specification,
consistent with ruby-core's event contract.

### Data Store

Foundation's Postgres instance (10.0.40.10:5432) is used as the
primary data store. Navi uses dedicated schemas per environment:

```
navi_dev
navi_staging
navi_prod
```

This provides full data isolation between environments with no
additional infrastructure overhead.

pgvector is installed on the Foundation Postgres instance from day
one. It is used initially for article deduplication via embedding
similarity. It is the foundation for semantic retrieval in trend
analysis, research briefs, and the Rolodex as those features are
built.

Schema migrations are managed by golang-migrate, run automatically
on service startup against the appropriate schema. Migration files
live in each service's directory under `migrations/`.

### Secrets Management

All secrets MUST be stored in Vault. Credentials MUST NOT be
hardcoded or stored in environment files committed to the repository.
The Vault path convention for Navi is:

```
secret/data/navi/{environment}/{service}
```

For example:
```
secret/data/navi/prod/resend       # Resend API key
secret/data/navi/prod/twilio       # Twilio credentials
secret/data/navi/prod/anthropic    # Claude API key
secret/data/navi/prod/postgres     # Postgres credentials
secret/data/navi/staging/resend    # Resend sandbox key
secret/data/navi/staging/twilio    # Twilio test credentials
```

Services retrieve secrets at startup via the Vault HTTP API. The
VAULT_ADDR and VAULT_TOKEN environment variables are the only
runtime dependencies not stored in Vault itself.

### Operations Interface

A top-level Makefile MUST be the primary interface for all operational
tasks: starting, stopping, deploying, running migrations, and
checking service health. Targets are environment-aware via an ENV
variable (dev, staging, prod). Direct docker compose invocations
SHOULD NOT be used outside of the Makefile.

### External API Dependencies

Navi has four external API dependencies:

| Service    | Purpose                        | Vault Path                        |
|------------|--------------------------------|-----------------------------------|
| Anthropic  | Summarization and synthesis    | secret/data/navi/{env}/anthropic  |
| Resend     | Outbound email delivery        | secret/data/navi/{env}/resend     |
| Twilio     | Inbound and outbound SMS       | secret/data/navi/{env}/twilio     |
| RSS/HTTP   | Content collection             | No credentials (public feeds)     |

Paid subscriptions (The Economist authenticated fetch, The Information,
Stratechery) require session cookie or API token management handled by
the collector. These credentials are stored in Vault under
`secret/data/navi/{env}/feeds/{publication}`.

### Infrastructure Dependencies and Known Risks

| Dependency         | Owner      | Risk if unavailable                    |
|--------------------|------------|----------------------------------------|
| NATS JetStream     | ruby-core  | Navi internal event bus unavailable    |
| Postgres           | Foundation | All persistence unavailable            |
| Vault              | Foundation | Service startup fails (no secrets)     |
| Home Assistant     | ruby-core  | HA notification delivery unavailable  |

The NATS dependency on ruby-core is the most architecturally
uncomfortable. Migrating NATS to Foundation is the correct long-term
resolution and is tracked as future work. It has no impact on Navi's
subject topology when it occurs -- only a connection string in Vault
changes.

## Consequences

**Positive:**
- Consistent patterns across Foundation, ruby-core, and Navi reduce
  cognitive overhead when moving between repositories.
- Schema-level environment isolation is operationally simple and
  carries no infrastructure cost.
- pgvector installed early avoids a disruptive migration later when
  semantic features are built.
- CloudEvents contract ensures Navi events are legible to any future
  subscriber, including ruby-mesh.

**Negative / tradeoffs:**
- Single-node deployment means no horizontal scaling. Accepted for
  a personal homelab with one primary user.
- NATS in ruby-core creates cross-repo availability coupling. Accepted
  pending future migration to Foundation.

**Neutral:**
- Docker Compose and Go are consistent with Foundation and ruby-core;
  this ADR extends an existing pattern rather than establishing a new
  one.
- Schema-per-environment isolation on a shared Postgres instance is a
  well-established approach. It provides data separation without
  requiring infrastructure duplication.

## Alternatives Considered

**Dedicated Postgres instance for Navi**
Rejected. Schema isolation on the Foundation instance provides
sufficient separation. A dedicated instance adds operational overhead
with no benefit at this scale.

**Python instead of Go**
Rejected. Go is the established preference for new services. The
concurrency model is well-suited to the collector's polling workload
and the event-driven internal architecture.

**Direct HTTP between services instead of NATS**
Rejected. Direct HTTP coupling defeats the purpose of an event-driven
architecture. Adding a new feature would require modifying existing
services to call the new one. NATS subscribers are additive.

**A dedicated graph database for the knowledge graph**
Rejected. The entity relationships in Navi's data model are structured
enough to be represented with Postgres foreign keys and join tables.
pgvector handles the semantic layer. A dedicated graph database adds
operational complexity that is not justified by the query requirements.
