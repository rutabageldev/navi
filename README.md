# Navi

Navi is a personal intelligence and enablement system — a digital chief of staff that
reduces cognitive overhead by continuously gathering, enriching, and surfacing intelligence
before it is needed.

The primary user is a Director of Product Management. Every feature is designed around
that context.

---

## Where to Start

| Document | Purpose |
|---|---|
| [STRATEGY.md](STRATEGY.md) | Product vision, principles, feature horizon, and out-of-scope boundaries. Read before any feature work. |
| [CLAUDE.md](CLAUDE.md) | Agent prompt format, architectural rules, and conventions for this repository. |
| [docs/adr/](docs/adr/) | All architectural decisions. Read the relevant ADRs before touching any area of the system. |

---

## Repository Structure

```
navi/
├── CLAUDE.md                      # Agent instructions and repo conventions
├── README.md                      # This file
├── STRATEGY.md                    # Product strategy
├── docs/
│   ├── adr/                       # Architecture Decision Records
│   ├── events/
│   │   ├── REGISTRY.md            # Event type registry
│   │   └── schemas/               # JSON Schema per event type
│   ├── plans/                     # Implementation plans (archived after merge)
│   ├── roadmap/                   # Roadmap items
│   └── runbooks/                  # Operational runbooks
├── monitoring/
│   ├── grafana/dashboards/        # Grafana dashboard JSON provisioning
│   └── prometheus/                # Prometheus alert rules
├── scripts/                       # Operational scripts (backup, restore, prune)
├── services/
│   ├── digest/                    # Daily intelligence service (v1)
│   └── internal/                  # Shared Go packages (telemetry, vault, postgres, nats, events)
├── docker-compose.yml
├── docker-compose.staging.yml
├── docker-compose.dev.yml
└── Makefile
```

---

## Architecture at a Glance

- **Language:** Go. No new Python services.
- **Event bus:** NATS JetStream (ruby-core). All subjects namespaced `navi.{env}.>`.
- **Data store:** Foundation Postgres at `10.0.40.10:5432`. Schemas: `navi_dev`, `navi_staging`, `navi_prod`.
- **Secrets:** Vault. Retrieved at startup, reloaded on SIGHUP.
- **Observability:** OTEL → Foundation OTel Collector → Prometheus / Tempo / Loki / Grafana. Nothing deployed locally.
- **Delivery:** Email via Resend, SMS via Twilio, push via Home Assistant.

See [ADR-0002](docs/adr/ADR-0002-top-level-architecture.md) for the full top-level architecture decision.

---

## Common Make Targets

```
make dev              # Start local dev environment
make test             # Run unit tests (with race detector)
make lint             # Run golangci-lint
make migrate ENV=x    # Run migrations for environment x
make smoketest ENV=x  # Run smoke tests for environment x
make deploy ENV=x     # Manual deploy (emergency use only)
make rollback ENV=x VERSION=y  # Emergency rollback
make logs ENV=x       # Tail logs for environment x
make status           # Show running containers across all environments
```

---

## Development Conventions

- All changes go through a pull request. No direct commits to `main`.
- ADRs must exist before code is written for any significant decision.
- Event schemas must be defined in `docs/events/schemas/` before producers and consumers are written.
- OpenAPI specs must be written before HTTP handlers are implemented.
- All tests run with `go test -race`. No exceptions.
- Pre-commit hooks are required: `make setup` installs them.
