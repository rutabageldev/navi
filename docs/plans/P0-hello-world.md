# PLAN-0001 — P0: Hello World

* **Status:** In Progress
* **Date:** 2026-04-06
* **Project:** navi
* **Roadmap Item:** none (pre-roadmap milestone)
* **Branch:** `feat/p0-hello-world`
* **Related ADRs:** ADR-0002, ADR-0003, ADR-0004, ADR-0008, ADR-0009, ADR-0010, ADR-0011, ADR-0012, ADR-0013

---

## Scope

P0 proves the full delivery and observability pipeline with a trivially
simple service. Nothing in P0 delivers user value — it delivers
confidence that everything built in P1 will ship correctly, be
observable, and roll back safely. The output of P0 is a deployed
`services/digest` container that responds to health checks, emits
traces, metrics, and logs to the Foundation observability stack, reads
secrets from Vault, and can be deployed and rolled back automatically
via the CI/CD pipeline.

**Explicitly out of scope for P0:** RSS collection, Claude API
integration, digest generation, email or SMS delivery, any business
logic. These belong to P1 and later.

---

## Pre-conditions

- [ ] GitHub repository `rutabageldev/navi` exists with `main` as the
      default branch
- [ ] Self-hosted GitHub Actions runner is installed and registered on
      the homelab node (label: `self-hosted`)
- [ ] Foundation Postgres is running at `10.0.40.10:5432` with pgvector
      installed and the `navi_dev`, `navi_staging`, `navi_prod` schemas
      creatable
- [ ] Foundation NATS JetStream is running (ruby-core) and reachable
      from the homelab node
- [ ] Foundation Vault is running at `https://10.0.40.10:8200`
- [ ] Foundation OTel Collector is running and its OTLP gRPC port
      (default 4317) is reachable from the homelab node
- [ ] Foundation Prometheus, Grafana, Loki, Tempo, Promtail, and Uptime
      Kuma are all running (no new instances needed — integration only)
- [ ] Go 1.23+ is installed on the homelab node and on the developer's
      machine
- [ ] Docker and Docker Compose V2 are installed on the homelab node
- [ ] `pre-commit` is installed on the developer's machine
- [ ] All 13 ADRs are present in `docs/adr/` in ADR-0NNN-slug.md format
- [ ] `STRATEGY.md` and `CLAUDE.md` are present at repo root

---

## Phase 1 — Repo Baseline ✓ COMPLETE (2026-04-06, commit 12a2fc7)

**Purpose:** Establish the quality gates, module structure, tooling
configuration, and version management files that every subsequent phase
depends on.

### Entry criteria
- Pre-conditions above are all met
- An empty (or near-empty) git repo exists at the remote

### Tasks

#### 1.1 — `.gitignore`

Create `.gitignore` at repo root. Must include at minimum:
```
*.env
.env.*
*.local
tmp/
*.log
dist/
*.test
coverage.out
```

#### 1.2 — Pre-commit configuration

Create `.pre-commit-config.yaml` at repo root implementing all hooks
from ADR-0012:
- gitleaks secret scanning (first hook)
- pre-commit/pre-commit-hooks: trailing-whitespace, end-of-file-fixer,
  check-yaml, check-json, check-merge-conflict, check-added-large-files
  (--maxkb=500), no-commit-to-branch (--branch=main)
- dnephin/pre-commit-golang: go-fmt, go-vet, go-imports
- check-jsonschema: check-openapi for services/**/api/openapi.yaml
- local validate-event-schemas hook invoking `make validate-schemas`
- commitizen for Conventional Commits enforcement

#### 1.3 — Gitleaks configuration

Create `.gitleaks.toml` at repo root. Include an allowlist section for
known false positives (test UUIDs, example Vault paths in docs). Every
allowlist entry must have a `description` field explaining why the
pattern is safe.

#### 1.4 — golangci-lint configuration

Create `.golangci.yml` at repo root with the linter set from ADR-0012:
errcheck, gosimple, govet, ineffassign, staticcheck, unused, gofmt,
goimports, gosec, bodyclose, contextcheck, noctx, exhaustive, godot,
misspell. gosec findings MUST fail the build. Set a reasonable timeout
(e.g. 5m).

#### 1.5 — Go workspace

Create `go.work` at repo root:
```
go 1.23

use (
    ./services/internal
    ./services/digest
)
```

#### 1.6 — Go modules

Create `services/internal/go.mod`:
```
module github.com/rutabageldev/navi/services/internal

go 1.23
```

Create `services/digest/go.mod`:
```
module github.com/rutabageldev/navi/services/digest

go 1.23

require github.com/rutabageldev/navi/services/internal v0.0.0
replace github.com/rutabageldev/navi/services/internal => ../internal
```

#### 1.7 — Directory structure stubs

Create the following directories with `.gitkeep` files (or initial
stub files as appropriate):

```
services/internal/telemetry/
services/internal/vault/
services/internal/postgres/
services/internal/nats/
services/internal/events/
services/digest/cmd/digest/
services/digest/cmd/smoketest/
services/digest/internal/collector/
services/digest/internal/enricher/
services/digest/internal/summarizer/
services/digest/internal/scheduler/
services/digest/internal/store/
services/digest/internal/api/
services/digest/internal/api/gen/
services/digest/api/
services/digest/config/
services/digest/migrations/
monitoring/grafana/dashboards/
monitoring/prometheus/
```

#### 1.8 — release-please configuration

Create `release-please-config.json` at repo root:
```json
{
  "$schema": "https://raw.githubusercontent.com/googleapis/release-please/main/schemas/config.json",
  "release-type": "go",
  "bump-minor-pre-major": true,
  "manifest-file": ".versions.json",
  "packages": {
    ".": {}
  }
}
```

Create `.versions.json` at repo root (initial value, managed by
release-please thereafter):
```json
{
  ".": "0.0.0"
}
```

Create `.last-deployed-version` at repo root (initial value):
```
none
```

Both `.versions.json` and `.last-deployed-version` MUST be committed
to the repository. Neither MUST be added to `.gitignore`.

#### 1.9 — Makefile

Create `Makefile` at repo root implementing all targets from ADR-0003.
Full target list and behavior:

```makefile
ENV        ?= dev
VERSION    ?= $(shell cat .last-deployed-version)
SERVICE    ?= digest

.PHONY: setup setup-infra dev test lint build deploy smoketest \
        healthcheck rollback migrate vault-seed logs status \
        check-generated validate-schemas

setup:
    pre-commit install
    pre-commit install --hook-type commit-msg

setup-infra:
    # Create the external Docker network used by prod and staging compose files.
    # Idempotent: exits cleanly if the network already exists.
    docker network inspect navi >/dev/null 2>&1 || docker network create navi

dev:
    NAVI_ENV=dev docker compose -f docker-compose.dev.yml up

test:
    go test -race ./services/...

lint:
    golangci-lint run ./services/...

build:
    # Detect changed services since last tag and build their images
    # See scripts/build.sh for change-detection logic
    ./scripts/build.sh $(VERSION)

deploy:
    ./scripts/deploy.sh $(ENV) $(VERSION) $(SERVICE)

smoketest:
    go run ./services/digest/cmd/smoketest/... \
        -env $(ENV) \
        -addr $$(./scripts/service-addr.sh $(ENV) $(SERVICE))

healthcheck:
    ./scripts/healthcheck.sh $(ENV) $(SERVICE)

rollback:
    ./scripts/rollback.sh $(ENV) $(VERSION) $(SERVICE)

migrate:
    go run ./services/digest/cmd/migrate/... -env $(ENV)

vault-seed:
    ./scripts/vault-seed.sh $(ENV)

logs:
    docker compose -f docker-compose.$(ENV).yml logs -f

status:
    docker compose -f docker-compose.dev.yml ps 2>/dev/null || true
    docker compose -f docker-compose.staging.yml ps 2>/dev/null || true
    docker compose -f docker-compose.yml ps 2>/dev/null || true

check-generated:
    ./scripts/check-generated.sh

validate-schemas:
    ./scripts/validate-schemas.sh
```

Place build/deploy/rollback/healthcheck logic in `scripts/` as shell
scripts referenced by the Makefile. The Makefile is the interface;
the scripts contain the implementation. Each script must be executable
and begin with `#!/usr/bin/env bash` and `set -euo pipefail`.

### Exit criteria

- [x] `pre-commit install` completes without error
- [x] `pre-commit run --all-files` passes on the committed files (the
      only Go file at this point is go.mod/go.work; no source to lint)
- [x] `go work sync` produces no errors
- [x] `.versions.json`, `.last-deployed-version`, and
      `release-please-config.json` are committed to main
- [x] `make setup` completes without error
- [x] `make status` runs without error (empty output is fine)

### Implementation notes

**OpenAPI hook:** The `check-openapi` hook ID referenced in ADR-0012
does not exist in the `python-jsonschema/check-jsonschema` repository.
Replaced with a local hook running `check-jsonschema --schemafile
https://spec.openapis.org/oas/3.1/schema/2022-10-07` directly. ADR-0012
updated to reflect the correct implementation. Behavior is identical.

`check-jsonschema` must be installed system-wide (`pip install
check-jsonschema`) — it is not managed by pre-commit's environment
isolation for this hook.

---

## Phase 2 — services/internal Package Stubs ✓ COMPLETE (2026-04-06)

**Purpose:** Implement the shared internal packages with enough
substance to compile cleanly, establish real connections to external
services, and be imported by services/digest without stubbing out
their interfaces.

These are NOT full implementations. They MUST provide the connection
and initialization logic that Phase 3's service needs to start up,
but do not need to implement every method that future services will
use.

### Entry criteria
- Phase 1 complete
- Foundation Vault, Postgres, NATS, and OTel Collector are all
  reachable from the developer's machine (for local integration
  verification)

### Tasks

#### 2.1 — `services/internal/telemetry/telemetry.go`

Implement:
- `Config` struct with fields: `ServiceName`, `ServiceVersion`,
  `Environment`, `OTLPEndpoint` (all strings)
- `InitTracer(ctx context.Context, cfg Config) (func(context.Context) error, error)`
  — initializes the OTEL SDK, configures OTLP gRPC exporter pointing
  at `cfg.OTLPEndpoint`, registers a TracerProvider and MeterProvider,
  returns a shutdown function
- `Tracer(name string) trace.Tracer` — returns a tracer from the
  global provider
- `ExtractNATSTraceContext(headers nats.Header) context.Context`
  — extracts W3C traceparent/tracestate from a NATS message header
  into a context
- `InjectNATSTraceContext(ctx context.Context, headers nats.Header)`
  — injects the active span context into a NATS message header

Dependencies to add to `services/internal/go.mod`:
- `go.opentelemetry.io/otel`
- `go.opentelemetry.io/otel/sdk`
- `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc`
- `go.opentelemetry.io/otel/propagation`
- `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`
- `github.com/nats-io/nats.go`

#### 2.2 — `services/internal/vault/vault.go`

Implement:
- `Client` struct wrapping the Vault HTTP API client
- `NewClient(addr, token string) (*Client, error)` — creates a Vault
  client and verifies the connection with a token lookup
- `GetSecret(path, key string) (string, error)` — retrieves a single
  string value from a KV v2 secret at the given path
- `RegisterSIGHUPReload(reloadFn func() error)` — registers a function
  to be called when the process receives SIGHUP; used by services to
  trigger a secret reload

Use the official `github.com/hashicorp/vault/api` Go SDK. Do not write
a custom HTTP client.

#### 2.3 — `services/internal/postgres/postgres.go`

Implement:
- `Config` struct: `Host`, `Port`, `User`, `Password`, `Database`,
  `Schema` (all strings)
- `Connect(ctx context.Context, cfg Config) (*pgxpool.Pool, error)`
  — creates a pgx connection pool, sets the search_path to cfg.Schema,
  runs a connectivity check
- `RunMigrations(db *pgxpool.Pool, migrationsPath, schema string) error`
  — runs golang-migrate against the given path and schema
- `HealthCheck(ctx context.Context, db *pgxpool.Pool) error`
  — executes `SELECT 1` as a liveness check

Dependencies: `github.com/jackc/pgx/v5`, `github.com/golang-migrate/migrate/v4`

#### 2.4 — `services/internal/nats/nats.go`

Implement:
- `Connect(url string) (*nats.Conn, error)` — connects with retry
  (3 attempts, exponential backoff), sets name option to service name
- `JetStream(conn *nats.Conn) (nats.JetStreamContext, error)`
  — returns a JetStream context
- `EnsureStream(js nats.JetStreamContext, name string, subjects []string) error`
  — creates the JetStream stream if it doesn't exist; idempotent
- `HealthCheck(conn *nats.Conn) error`
  — returns nil if the connection is open

#### 2.5 — `services/internal/events/events.go`

Implement:
- `Envelope` struct matching the CloudEvents v1.0 + Navi extension
  attributes schema from ADR-0011
- `NewEnvelope(eventType, source, environment, schema string, data interface{}) (*Envelope, error)`
  — constructs a validated CloudEvents envelope with all required
  fields populated; generates UUID for `id`, sets `time` to now
- `InjectTrace(ctx context.Context, env *Envelope)` — copies W3C
  traceparent/tracestate from the active span into the envelope
  extension attributes
- `ExtractTrace(env *Envelope) context.Context`
  — extracts traceparent/tracestate from the envelope into a context

Dependencies: `github.com/cloudevents/sdk-go/v2`

#### 2.6 — Run `go mod tidy` for both modules

After all stub files are written, run:
```bash
cd services/internal && go mod tidy
cd services/digest && go mod tidy
go work sync
```

### Exit criteria

- [x] `go build ./services/internal/...` succeeds with no errors
- [x] `go vet ./services/internal/...` produces no output
- [x] `go test -race ./services/internal/...` passes (even with no test
      files — the compilation must succeed)
- [x] `golangci-lint run ./services/internal/...` produces no blocking
      findings

### Implementation notes

**golangci-lint v2 config changes:** golangci-lint v2 (installed: v2.11.4)
requires formatters in a separate `formatters.enable` block rather than
`linters.enable`. `gosimple` was removed as it is absorbed by `staticcheck`
in v2. ADR-0012 updated to reflect the correct v2 configuration.

**golangci-lint not installed by default:** Must be installed manually
(`curl -sSfL ... | sh -s -- -b ~/bin latest`). Consider adding to
`make setup` when the homelab node is the primary dev machine.

---

## Phase 3 — services/digest Hello World ✓ COMPLETE (2026-04-06)

**Purpose:** Implement the simplest possible running digest service:
a single binary that initializes all dependencies, serves the health
endpoints, emits structured logs, and shuts down cleanly. No business
logic.

### Entry criteria
- Phase 2 complete (all internal packages compile)
- Foundation Postgres schemas are created (navi_dev, navi_staging,
  navi_prod) — can be empty at this point
- Docker is running on the developer's machine

### Tasks

#### 3.1 — OpenAPI spec for health endpoints

Create `services/digest/api/openapi.yaml` implementing only the two
health endpoints per ADR-0010:

```yaml
openapi: "3.1.0"
info:
  title: Navi Digest Service API
  version: "0.0.0"
paths:
  /v1/health/live:
    get:
      operationId: healthLive
      summary: Liveness check
      responses:
        "200":
          description: Service is alive
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/LiveResponse"
  /v1/health/ready:
    get:
      operationId: healthReady
      summary: Readiness check
      responses:
        "200":
          description: Service is ready
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ReadyResponse"
        "503":
          description: Service is not ready
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Error"
components:
  schemas:
    LiveResponse:
      type: object
      required: [status]
      properties:
        status:
          type: string
          enum: [ok]
    ReadyResponse:
      type: object
      required: [status, version, checks]
      properties:
        status:
          type: string
          enum: [ok, degraded]
        version:
          type: string
          description: Deployed version, injected at build time
        checks:
          type: object
          properties:
            postgres:
              type: string
            nats:
              type: string
            vault:
              type: string
    Error:
      type: object
      required: [error]
      properties:
        error:
          type: object
          required: [code, message, request_id]
          properties:
            code:
              type: string
            message:
              type: string
            request_id:
              type: string
            trace_id:
              type: string
```

#### 3.2 — Generate Go types from OpenAPI spec

Run oapi-codegen against the spec:
```bash
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
oapi-codegen \
  --config services/digest/api/oapi-codegen.yaml \
  services/digest/api/openapi.yaml
```

Create `services/digest/api/oapi-codegen.yaml`:
```yaml
package: gen
generate:
  - types
  - chi-server   # or std-http — choose based on router preference
output: services/digest/internal/api/gen/api.gen.go
```

Commit the generated file `services/digest/internal/api/gen/api.gen.go`.
This file MUST NOT be gitignored.

#### 3.3 — Health handler implementation

Create `services/digest/internal/api/handlers.go` implementing the
generated server interface. The handler logic:

**`GET /v1/health/live`**
Always returns 200 with `{"status":"ok"}` if the process is running.

**`GET /v1/health/ready`**
Runs the following checks in parallel with a 5-second timeout:
- Postgres: `postgres.HealthCheck(ctx, db)`
- NATS: `nats.HealthCheck(conn)`
- Vault: `vault.Client.Ping(ctx)` (implement a lightweight token
  self-lookup as a health check on the vault client)

Returns 200 if all checks pass:
```json
{
  "status": "ok",
  "version": "v0.0.1",
  "checks": {
    "postgres": "ok",
    "nats": "ok",
    "vault": "ok"
  }
}
```

Returns 503 if any check fails, with `checks.<name>` set to the
error message for each failing dependency.

The response MUST include the `version` field, injected at build time.
The handler must use the `X-Request-ID` middleware and OTEL HTTP server
middleware as required by ADR-0010.

#### 3.4 — `services/digest/cmd/digest/main.go`

Implement `main()` with the following startup sequence:
1. Read `NAVI_ENV`, `VAULT_ADDR`, `VAULT_TOKEN`, `NAVI_HOST` from
   environment (fail fast if absent)
2. Initialize `vault.Client`
3. Retrieve all required secrets from Vault (Postgres creds, NATS URL,
   OTEL endpoint, log level)
4. Initialize `telemetry.InitTracer` (defer shutdown)
5. Initialize structured logger (`slog.New(slog.NewJSONHandler(...))`)
   with base fields: service, component, environment. Set handler on
   slog.SetDefault.
6. Initialize `postgres.Connect` and `postgres.RunMigrations`
7. Initialize `nats.Connect` and `nats.EnsureStream`
8. Register SIGHUP handler for secret reload (`vault.RegisterSIGHUPReload`)
9. Start HTTP server on `{NAVI_HOST}:{PORT}` with:
   - OTEL HTTP server middleware (otelhttp.NewHandler)
   - Request ID middleware
   - Health routes registered
10. Register SIGTERM/SIGINT handler for graceful shutdown:
    - Stop accepting new requests (5-second grace period)
    - Flush OTEL spans
    - Close DB pool
    - Drain NATS connection

Log one INFO line on startup: "navi digest service started" with
fields: version, environment, port.

Build-time version injection: the `version` variable in main.go is
declared as:
```go
var version = "dev"
```
And is overridden at build time via:
```
go build -ldflags "-X main.version=$(shell git describe --tags --abbrev=0 2>/dev/null || echo dev)"
```

#### 3.5 — Initial database migration

Create `services/digest/migrations/0001_init.up.sql`:
```sql
-- P0: empty initial migration, establishes migration baseline
-- P1 will add the full schema from ADR-0004
SELECT 1;
```

Create `services/digest/migrations/0001_init.down.sql`:
```sql
-- No-op down migration for P0
SELECT 1;
```

#### 3.6 — Feed configuration skeleton

Create `services/digest/config/feeds.yaml`:
```yaml
# Feed configuration — populated in P1
# See ADR-0005 for feed schema and tier definitions
feeds: []
```

#### 3.7 — Dockerfile

Create `services/digest/Dockerfile`:
```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /build

# Copy workspace files
COPY go.work go.work.sum ./
COPY services/internal/ ./services/internal/
COPY services/digest/ ./services/digest/

# Build with version injection
ARG VERSION=dev
RUN go build \
    -ldflags "-X main.version=${VERSION}" \
    -o /bin/digest \
    ./services/digest/cmd/digest/

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /bin/digest /bin/digest
EXPOSE 8080
ENTRYPOINT ["/bin/digest"]
```

Note: the Docker build context must be the repo root so go.work and
both modules are available.

#### 3.8 — Docker Compose files

Create `docker-compose.yml` (production) at repo root:
```yaml
services:
  digest:
    image: ghcr.io/rutabageldev/navi-digest:${NAVI_VERSION:-latest}
    container_name: navi-digest-prod
    restart: unless-stopped
    environment:
      NAVI_ENV: prod
      VAULT_ADDR: https://10.0.40.10:8200
      VAULT_CACERT: /vault/tls/vault-ca.crt
      VAULT_TOKEN: ${VAULT_TOKEN}
      NAVI_HOST: ${NAVI_HOST:-10.0.40.10}
    volumes:
      - /opt/foundation/vault/tls/vault-ca.crt:/vault/tls/vault-ca.crt:ro
    ports:
      - "${NAVI_HOST:-10.0.40.10}:8083:8080"
    networks:
      - navi
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.navi-digest.rule=Host(`navi.home.arpa`)"
      - "traefik.http.routers.navi-digest.tls=true"

networks:
  navi:
    external: true
```

> **Implementation note (2026-04-09):** Port 8080 is taken by unifi-controller on
> this node. Port layout: dev=8082, staging=8081, prod=8083. Prod port was changed
> from 8080 to 8083 (commit bb10af2). Update Uptime Kuma monitors and any external
> references accordingly.

Create `docker-compose.staging.yml` at repo root (mirrors prod with
staging-specific container names and environment):
```yaml
services:
  digest:
    image: ghcr.io/rutabageldev/navi-digest:${NAVI_VERSION:-staging}
    container_name: navi-digest-staging
    restart: unless-stopped
    environment:
      NAVI_ENV: staging
      VAULT_ADDR: https://10.0.40.10:8200
      VAULT_CACERT: /vault/tls/vault-ca.crt
      VAULT_TOKEN: ${VAULT_TOKEN}
      NAVI_HOST: ${NAVI_HOST:-10.0.40.10}
    volumes:
      - /opt/foundation/vault/tls/vault-ca.crt:/vault/tls/vault-ca.crt:ro
    ports:
      - "${NAVI_HOST:-10.0.40.10}:8081:8080"
    networks:
      - navi

networks:
  navi:
    external: true
```

Create `docker-compose.dev.yml` at repo root (builds from source,
connects to Foundation services already running on this node):

> **Implementation note:** The plan originally included local Postgres,
> NATS, and Vault containers in docker-compose.dev.yml. This was
> corrected during execution: dev development on this node runs against
> the Foundation services already running (Vault at 10.0.40.10:8200,
> Postgres at 10.0.40.10:5432, NATS via ruby-core). Spinning up
> duplicates wastes resources and creates confusion. The dev compose
> file only runs the digest service and points it at Foundation.
>
> Additionally: Foundation Vault uses TLS (HTTPS). All compose files use
> `https://10.0.40.10:8200` and mount the Foundation CA cert at
> `/opt/foundation/vault/tls/vault-ca.crt`. Port 8080 was already taken
> by unifi-controller on this node, so dev uses port 8082 via NAVI_PORT.

```yaml
services:
  digest:
    build:
      context: .
      dockerfile: services/digest/Dockerfile
      args:
        VERSION: dev
    container_name: navi-digest-dev
    environment:
      NAVI_ENV: dev
      VAULT_ADDR: https://10.0.40.10:8200
      VAULT_CACERT: /vault/tls/vault-ca.crt
      VAULT_TOKEN: ${VAULT_TOKEN}
      NAVI_HOST: 0.0.0.0
      NAVI_PORT: "8082"
      NAVI_LOG_LEVEL: debug
    volumes:
      - /opt/foundation/vault/tls/vault-ca.crt:/vault/tls/vault-ca.crt:ro
    # host networking gives the container direct access to Foundation services
    # (Vault, Postgres, NATS) without any extra network configuration.
    network_mode: host
```

#### 3.9 — Create external Docker network on homelab node

The prod and staging compose files declare `navi` as an external
network. This network MUST be created manually on the homelab node
before the first deploy. It is a one-time setup step.

```bash
docker network create navi
```

This is wrapped in a Makefile target `make setup-infra` (see Phase 1.9)
which is idempotent: it creates the network only if it does not already
exist.

Run this step on the homelab node before Phase 4.3 (first staging
container start).

### Exit criteria

- [x] `go build ./services/digest/cmd/digest/` succeeds
- [x] `go vet ./services/digest/...` produces no output
- [x] `go test -race ./services/digest/...` passes
- [x] `golangci-lint run ./services/digest/...` produces no blocking
      findings
- [x] `make dev` starts the digest container without error
- [x] `make check-generated` confirms oapi-codegen output matches spec

### Implementation notes (Phase 3 corrections — 2026-04-09)

**Route registration bug:** The generated `gen.HandlerFromMux(h, r)` call was used
in task 3.4 but its return value was discarded. chi v5's `ServeHTTP` returns 404
when `mx.handler` is nil — routes added to the tree after `With()` compiles the
handler are reachable via `routeHTTP` but `ServeHTTP` never delegates. Fix: replaced
`gen.HandlerFromMux(h, r)` with explicit `r.Get("/v1/health/live", h.HealthLive)` and
`r.Get("/v1/health/ready", h.HealthReady)` (commit a6f2b36).

**ADR-0010 middleware additions:** The middleware chain in `main.go` was incomplete.
Added per ADR-0010: `middleware.Timeout(30 * time.Second)`, `requestIDResponse`
(copies chi request ID to `X-Request-ID` response header), and `requestLogger`
(structured slog INFO line per completed request with method/path/status/duration_ms/
request_id). Middleware order: `RequestID → RealIP → Timeout(30s) → requestIDResponse
→ requestLogger → Recoverer`.

---

## Phase 3b — NATS mTLS + NKEY Authentication ✓ COMPLETE (2026-04-07)

**Purpose:** Correct a gap in Phase 2. The ruby-core NATS server requires
NKEY signing and mutual TLS from all clients. The `services/internal/nats`
package was written against an assumed plaintext unauthenticated connection
and will be rejected by the real server. This phase fixes that before Phase
4's live connectivity checks.

Discovered during Phase 4 pre-flight: ruby-core NATS binds on
`127.0.0.1:4222` (dev) and `127.0.0.1:4223` (prod), uses mTLS
(`require: true` in `nats.conf`), and authenticates clients via NKEY
public key allowlist (per ruby-core ADR-0017).

### Entry criteria
- Phase 3 complete
- Foundation has provisioned a Navi NKEY and client TLS cert and
  seeded Vault at the paths defined in task 3b.3 below

### Tasks

#### 3b.1 — Update `services/internal/nats/nats.go`

Replace `Connect(url string)` with `Connect(cfg Config)`. Add a `Config`
struct:

```go
type Config struct {
    URL      string // e.g. "tls://127.0.0.1:4222"
    NKeySeed string // NKEY seed for authentication
    TLSCert  []byte // PEM client certificate
    TLSKey   []byte // PEM client private key
    TLSCA    []byte // PEM CA certificate
}
```

Implement NKEY auth via `nats.Nkey()` option and mTLS via `nats.Secure()`
with a `tls.Config` built from the cert material, following the pattern
in ruby-core's `boot.ConnectNATS()`. Add `github.com/nats-io/nkeys` as
a direct dependency (`go get github.com/nats-io/nkeys`).

All other functions (`JetStream`, `EnsureStream`, `HealthCheck`) are
unchanged.

#### 3b.2 — Update `services/digest/cmd/digest/main.go`

Replace the single `GetSecret(..., "url")` NATS call with a
`loadNATSConfig()` helper that reads:

- `secret/data/navi/{env}/nats` → fields `url`, `seed`
- `secret/data/navi/{env}/nats/tls` → fields `cert`, `key`, `ca`

Returns `internalnats.Config`. Pass the result to `internalnats.Connect()`.

#### 3b.3 — Vault path additions (requires Foundation)

Two new secrets per environment. These MUST be seeded by Foundation
before Phase 4.3 can succeed:

| Secret path | Fields | Provisioned by |
|---|---|---|
| `secret/navi/{env}/nats` | `url`, `seed` | Foundation (NKEY gen) |
| `secret/navi/{env}/nats/tls` | `cert`, `key`, `ca` | Foundation (cert issuance) |

The `url` values are:
- dev: `tls://127.0.0.1:4222`
- prod/staging: `tls://127.0.0.1:4223`

Foundation must also:
- Register the Navi NKEY public key in the ruby-core NATS server config
  (authorized users allowlist)
- Sign the client cert with the same CA used for the NATS server

Update the Phase 4.2 `vault-seed.sh` to seed `url` in
`secret/navi/{env}/nats` (placeholder `seed` value); real seed and TLS
material are written directly by Foundation, not via vault-seed.

#### 3b.4 — Update ADR-0002

Add a section documenting the NATS auth requirement. ADR-0002 currently
describes NATS JetStream as the event bus but is silent on the
authentication mechanism. It MUST document:

- Clients authenticate via NKEY signing (seed stored in Vault)
- All connections use mTLS (client cert issued by NATS CA, stored in Vault)
- Vault paths for credentials: `secret/navi/{env}/nats` and
  `secret/navi/{env}/nats/tls`
- The connection pattern follows ruby-core's `boot.ConnectNATS()` as
  the reference implementation

### Exit criteria

- [x] `services/internal/nats` compiles with the new `Config`-based API
- [x] `go vet ./services/internal/...` and `go test -race ./services/internal/...`
      pass
- [x] `go vet ./services/digest/...` and `go test -race ./services/digest/...`
      pass
- [x] `golangci-lint run` produces no new findings
- [x] ADR-0002 documents NATS auth (NKEY + mTLS)
- [x] Foundation has provisioned NKEY, client cert, and seeded Vault
      (prerequisite for Phase 4.3 — not required to commit code)
- [x] `make check-generated` still passes

---

## Phase 4 — Vault Seeding

**Purpose:** Populate all required Vault paths for staging and
production so the service can start up against real Foundation
infrastructure.

### Entry criteria
- Phase 3 complete (service starts in dev environment)
- Foundation Vault is running and accessible
- A Vault token with admin-level write access is available for seeding

### Tasks

#### 4.1 — Vault policy for Navi

Create a Vault policy `navi-digest` that grants the service:
- Read access to `secret/data/navi/{{env}}/*`
- Ability to renew its own token
- No write access, no access to other prefixes

```hcl
# navi-digest.hcl
path "secret/data/navi/+/*" {
  capabilities = ["read"]
}

path "auth/token/renew-self" {
  capabilities = ["update"]
}

path "auth/token/lookup-self" {
  capabilities = ["read"]
}
```

Apply with: `vault policy write navi-digest navi-digest.hcl`

Create a periodic token for each environment and store its value as the
`VAULT_TOKEN` environment variable on the homelab node (in a secure
location such as a `.env` file that is not committed).

#### 4.2 — Seed Vault paths

The `make vault-seed ENV=x` target wraps a shell script that writes
the following paths. Placeholder values are used for secrets not yet
configured; real values are filled in before P0 verification.

**Postgres (required for service startup):**
```bash
vault kv put secret/navi/prod/postgres \
  host=10.0.40.10 port=5432 \
  user=navi password=CHANGE_ME \
  database=navi schema=navi_prod

vault kv put secret/navi/staging/postgres \
  host=10.0.40.10 port=5432 \
  user=navi password=CHANGE_ME \
  database=navi schema=navi_staging
```

**NATS (required for service startup):**
```bash
# CONFIRM_ME: verify the actual NATS JetStream address for the ruby-core
# instance before running vault-seed. The host below is a placeholder.
vault kv put secret/navi/prod/nats url=nats://RUBY_CORE_HOST:4222
vault kv put secret/navi/staging/nats url=nats://RUBY_CORE_HOST:4222
```

**OTel Collector (required for telemetry):**
```bash
vault kv put secret/navi/prod/telemetry endpoint=10.0.40.10:4317
vault kv put secret/navi/staging/telemetry endpoint=10.0.40.10:4317
```

**Resend (placeholder for P0 — delivery not wired yet):**
```bash
vault kv put secret/navi/prod/resend \
  api_key=PLACEHOLDER from_address=navi@example.com to_address=CHANGE_ME
vault kv put secret/navi/staging/resend \
  api_key=PLACEHOLDER from_address=navi@example.com to_address=CHANGE_ME
```

**Twilio (placeholder for P0):**
```bash
vault kv put secret/navi/prod/twilio \
  account_sid=PLACEHOLDER auth_token=PLACEHOLDER \
  from_number=PLACEHOLDER to_number=PLACEHOLDER webhook_url=PLACEHOLDER
vault kv put secret/navi/staging/twilio \
  account_sid=PLACEHOLDER auth_token=PLACEHOLDER \
  from_number=PLACEHOLDER to_number=PLACEHOLDER webhook_url=PLACEHOLDER
```

**Anthropic (placeholder for P0):**
```bash
vault kv put secret/navi/prod/anthropic \
  api_key=PLACEHOLDER model=claude-sonnet-4-6
vault kv put secret/navi/staging/anthropic \
  api_key=PLACEHOLDER model=claude-sonnet-4-6
```

**SMS authorized numbers (placeholder for P0):**
```bash
vault kv put secret/navi/prod/sms/authorized_numbers \
  numbers="+1CHANGEME"
vault kv put secret/navi/staging/sms/authorized_numbers \
  numbers="+1CHANGEME"
```

**Vault path format — two forms, one backing store:**

| Context | Path format | Example |
|---------|------------|---------|
| `vault kv put` / `vault kv get` CLI | `secret/navi/{env}/{service}` | `secret/navi/prod/postgres` |
| Go code calling the Vault HTTP API | `secret/data/navi/{env}/{service}` | `secret/data/navi/prod/postgres` |

The CLI transparently inserts `data/` for KV v2 mounts. The Go client
calls the HTTP API directly and MUST include `data/`. Both paths
address the same stored secret. Every `GetSecret()` call in service
code MUST use the `secret/data/...` form.

The `vault-seed.sh` script must be idempotent (safe to run multiple
times). It MUST NOT contain actual secret values — it seeds placeholder
values only. Real values are set manually in Vault by the operator and
are never committed.

#### 4.3 — Verify service startup against staging Vault

Start the staging container manually (without CI):
```bash
VAULT_TOKEN=<staging-token> NAVI_HOST=10.0.40.10 \
  docker compose -f docker-compose.staging.yml up digest
```

Confirm:
- Service logs show: `"msg":"navi digest service started","environment":"staging"`
- No Vault errors in logs
- `/v1/health/ready` returns 200 with all checks passing

#### 4.4 — Verify SIGHUP secret reload

1. Update a non-critical staging Vault secret (e.g. add a field to
   `secret/navi/staging/telemetry`)
2. Send SIGHUP to the running container:
   `docker kill --signal SIGHUP navi-digest-staging`
3. Confirm in logs: a reload event is logged at INFO level
4. Confirm the service remains running and `/v1/health/ready` still
   returns 200

#### 4.5 — Automated Vault token renewal

The Navi Vault token is a periodic token with a 90-day period. It must
be renewed before expiry or the service loses access to all secrets and
cannot start. Manual renewal is not acceptable — a missed renewal is a
production outage.

**Renewal script: `scripts/renew-vault-token.sh`**

Create a script that:
1. Sources `VAULT_TOKEN` and `VAULT_ADDR` from `/opt/navi/.env`
2. Calls `vault token renew` with the Foundation CA cert
3. Emits a structured log line (JSON) on success: include service,
   event, new TTL, and timestamp
4. Emits a structured log line on failure and exits non-zero so the
   scheduler can detect the failure
5. Is idempotent and safe to run at any time

```bash
#!/usr/bin/env bash
set -euo pipefail

# Source token and address from the project .env
# shellcheck source=/dev/null
source "$(dirname "$0")/../.env"

VAULT_CACERT=/opt/foundation/vault/tls/vault-ca.crt

result=$(VAULT_ADDR="$VAULT_ADDR" VAULT_TOKEN="$VAULT_TOKEN" \
  VAULT_CACERT="$VAULT_CACERT" \
  vault token renew -format=json 2>&1) || {
    printf '{"service":"navi","event":"vault_token_renewal_failed","error":"%s","ts":"%s"}\n' \
      "$result" "$(date -u +%FT%TZ)"
    exit 1
  }

ttl=$(echo "$result" | python3 -c "import sys,json; print(json.load(sys.stdin)['auth']['lease_duration'])" 2>/dev/null || echo "unknown")
printf '{"service":"navi","event":"vault_token_renewed","ttl_seconds":%s,"ts":"%s"}\n' \
  "$ttl" "$(date -u +%FT%TZ)"
```

**Makefile targets:**

```makefile
## renew-vault-token: Renew the Navi Vault token manually
renew-vault-token:
	@scripts/renew-vault-token.sh

## install-cron: Install automated Vault token renewal cron job
install-cron:
	@(crontab -l 2>/dev/null | grep -v 'renew-vault-token'; \
	  echo "0 6 * * 1 /opt/navi/scripts/renew-vault-token.sh >> /var/log/navi-vault-renewal.log 2>&1") \
	  | crontab -
	@echo "Cron job installed: weekly renewal every Monday at 06:00"

## uninstall-cron: Remove automated Vault token renewal cron job
uninstall-cron:
	@crontab -l 2>/dev/null | grep -v 'renew-vault-token' | crontab -
	@echo "Cron job removed"
```

**Schedule:** Weekly (every Monday at 06:00). A 90-day token renewed
weekly gives 12+ missed renewals before expiry. This is the right
balance between operational noise and safety margin.

**Observability:** The script logs JSON to stdout. The cron job
redirects output to `/var/log/navi-vault-renewal.log`. Foundation
Promtail must be configured to scrape that log file and forward to
Loki. A Grafana alert on `event="vault_token_renewal_failed"` is the
observable failure signal. This is addressed in Phase 5.

Note: The Foundation `navi` Vault policy includes `auth/token/renew-self`
capability, which is required for this renewal to succeed.

### Exit criteria

- [x] `make setup-infra` has been run — `docker network inspect navi`
      returns without error
- [x] `curl http://localhost:8082/v1/health/live` returns 200 with
      `{"status":"ok"}` — confirmed 2026-04-09 after orphaned proxy cleanup
- [x] `curl http://localhost:8082/v1/health/ready` returns 200 with
      all three checks reporting "ok" and a `version` field present
      — confirmed 2026-04-09
- [x] `make vault-seed ENV=staging` completes without error
- [x] `make vault-seed ENV=prod` completes without error
- [x] Service starts against staging infrastructure with all checks green
      — confirmed 2026-04-09 (vault/postgres/nats all ok, version field present)
- [x] SIGHUP reload confirmed working — log line "SIGHUP received — reloading secrets"
      emitted at INFO; health/ready returned 200 immediately after
- [x] `make renew-vault-token` runs without error and logs a JSON
      success line with a TTL field (90 days / 7,776,000s confirmed)
- [x] `make install-cron` installs the weekly renewal job —
      `crontab -l` shows the entry
- [x] No actual secrets committed to the repository
- [x] `vault.NewClient`, `postgres.Connect`, `nats.Connect`, and
      `telemetry.InitTracer` all succeed against real Foundation services
      — confirmed 2026-04-09 (health/ready all-green)

### Implementation notes (Phase 4 — 2026-04-09)

**Orphaned docker-proxy incident:** After switching `docker-compose.dev.yml` to
`network_mode: host`, the prior bridge-networking deployment left orphaned
`docker-proxy` processes (PIDs 3816304/3816311, started 2026-04-01) listening on
`host:8082` and proxying to a long-gone container at `172.20.0.9:8082` on
`docker_gwbridge`. The new host-network container silently failed to bind — the HTTP
server never started but the process stayed alive because OTEL/Postgres goroutines
kept running. Diagnosis: `/proc/net/tcp` showed no HTTP socket under the container
PID; `ps aux | grep docker-proxy | grep 8082` revealed the orphan.
Resolution: `sudo kill 3816304 3816311`, then container restart. Health endpoints
returned 200 on 8082 immediately. See also: memory note `project_orphaned_proxy_risk.md`.

If health endpoints ever return 404 after a container recreate, check for orphaned
docker-proxy processes first: `ps aux | grep docker-proxy | grep <port>`.

**OTel Collector host-binding:** Foundation OTel Collector was exposing ports 4317/4318
without host IP binding — only accessible within Docker bridge networks. Host-network
containers (like navi-digest-dev) could not reach them. Fix applied in Foundation:
added `"10.0.40.10:4317->4317/tcp"` and `"10.0.40.10:4318->4318/tcp"` bindings to
the observability compose file. Verified: `nc -zv 10.0.40.10 4317` succeeds; full
OTLP export cycle passes without errors.

**NAVI_HOST dual-purpose bug:** The staging/prod compose files set `NAVI_HOST` in
the container environment. In bridge networking, this causes the service to bind to
`10.0.40.10:8080` — a host IP not available inside the container. The HTTP server
silently never started (process stayed alive due to OTEL/Postgres goroutines). Fix:
remove `NAVI_HOST` from the `environment` block in staging/prod compose files; the
service defaults to `0.0.0.0`. The `ports` line (`"10.0.40.10:8081:8080"`) handles
host-side restriction. Only dev needs `NAVI_HOST` set because it uses `network_mode: host`
where `0.0.0.0` would be too broad.

**NATS networking architecture:** NATS is a Docker container (`ruby-core-prod-nats`)
on `ruby-core-prod_default`. Its host-mapped port `127.0.0.1:4223` is only reachable
from host-network processes. Bridge-networked containers (staging, prod) cannot reach
`127.0.0.1:4223`. Fix: created shared `event-bus-prod` and `event-bus-dev` external
Docker networks. ruby-core NATS containers join these networks. Navi staging/prod
compose files join `event-bus-prod`. Vault staging/prod NATS URLs updated from
`tls://127.0.0.1:4223` to `tls://nats:4222` (the NATS container hostname on the
shared network). Dev retains `tls://127.0.0.1:4222` via host networking.

**Cleanup:** Orphan containers from old dev compose (navi-dev-postgres,
navi-dev-vault, navi-dev-nats) should be removed:
```bash
docker compose -f docker-compose.dev.yml down --remove-orphans
```

---

## Phase 5 — Observability Wiring ✓ COMPLETE (2026-04-09)

**Purpose:** Confirm that traces, metrics, and logs from the running
service are visible in the Foundation observability stack. This phase
requires changes to Foundation configuration — it is the only phase
that modifies anything outside the Navi repository.

### Entry criteria
- Phase 4 complete (service running against staging)
- Foundation Prometheus, OTel Collector, Grafana, Loki, Promtail, and
  Uptime Kuma are all running
- Access to the Foundation config files to make the required additions

### Tasks

#### 5.1 — Verify Foundation OTel Collector → Prometheus pipeline

Per ADR-0008, Navi MUST NOT expose a `/metrics` endpoint for direct
Prometheus scrape. Metrics flow exclusively via OTLP:

```
Navi OTEL SDK → OTLP/gRPC (port 4317) → Foundation OTel Collector
                                                    ↓
                                         Prometheus exporter
                                                    ↓
                                      Foundation Prometheus scrapes
                                         the Collector's exporter
```

**Do NOT add a `/metrics` endpoint to the digest service. Do NOT add
a direct Prometheus scrape target pointing at Navi's service port.**

Verification steps for this task:
1. Confirm the Foundation OTel Collector config has a `prometheus`
   exporter enabled (typically exposes metrics at `0.0.0.0:8889`).
   If not, add it to the Foundation OTel Collector config — this is a
   Foundation configuration change, not a Navi change.
2. Confirm the Foundation `prometheus.yml` scrapes the Collector's
   Prometheus exporter endpoint (e.g. `10.0.40.10:8889`). If this
   scrape target is already present from other Foundation services,
   no change is needed — Navi metrics flow through the same pipeline.
3. Make a request to the staging service to generate a trace and metric
   data point, then verify the metric appears in Prometheus.

The service's OTLP endpoint is read from Vault at
`secret/data/navi/{env}/telemetry` under the key `endpoint`
(e.g. `10.0.40.10:4317`). This was seeded in Phase 4.2.

#### 5.2 — Foundation OTel Collector config

If the Foundation OTel Collector is not already configured to accept
OTLP from Navi, add or verify:
- A receiver accepting OTLP/gRPC on port 4317 (likely already present)
- A pipeline that routes Navi telemetry to both Prometheus and Tempo

No Navi-side OTel Collector configuration is needed. The OTLP endpoint
address is read from Vault at `secret/data/navi/{env}/telemetry`.

#### 5.3 — Commit Grafana dashboard

Create `monitoring/grafana/dashboards/navi.json` as a Grafana
dashboard JSON provisioning file. For P0, the dashboard needs only:

- System Health row: digest service uptime (Prometheus `up` metric),
  last request timestamp
- Logs row: Loki panel streaming `{container=~"navi.*"} | json` for
  live log tail

A full production dashboard per ADR-0009 is built in P1 when real
metrics exist.

The JSON file is loaded automatically by Foundation Grafana's existing
provisioning configuration. Confirm the provisioning path matches where
Grafana looks for dashboards.

#### 5.4 — Foundation Uptime Kuma registration

Add two monitors to Foundation Uptime Kuma under a new "Navi" group:

| Name | URL | Interval |
|------|-----|----------|
| Digest (live) - prod | `http://10.0.40.10:8083/v1/health/live` | 60s |
| Digest (ready) - staging | `http://10.0.40.10:8081/v1/health/ready` | 60s |

This is a manual configuration step in the Uptime Kuma UI. Document
the configuration in `docs/runbooks/uptime-kuma.md` so it can be
recreated if Uptime Kuma is restarted.

### Exit criteria

- [x] At least one Navi metric is visible in Foundation Prometheus —
      confirmed 2026-04-09. 10 metrics landing under `{job="navi-digest"}`:
      `http_server_request_duration_seconds_{bucket,count,sum}`,
      `http_server_request_body_size_bytes_{bucket,count,sum}`,
      `http_server_response_body_size_bytes_{bucket,count,sum}`,
      `target_info`. Use `{job="navi-digest"}` — OTEL `service.name`
      maps to `job` in Prometheus remote write, not `service_name`.
- [x] Grafana dashboard `navi.json` is committed and loads in Foundation
      Grafana without errors — visible at uid=navi-digest in the "Navi"
      folder at http://10.0.40.10:3000
- [>] Tempo trace, Loki log, and Uptime Kuma monitors — moved to Phase 6
      exit criteria; require a sustained prod deployment to verify

### Implementation notes (Phase 5 — 2026-04-09)

**OTel → Prometheus label mapping:** OTEL SDK's `service.name` resource
attribute arrives in Prometheus as `job`, not `service_name`. The default
PeriodicReader flush interval is 60s — short test windows (< 60s) show no
data. Query Navi metrics with `{job="navi-digest"}`.

**Confirmed metrics (10 total):** `http_server_request_duration_seconds`,
`http_server_request_body_size_bytes`, `http_server_response_body_size_bytes`
(each as bucket/count/sum histogram), plus `target_info`. These are
auto-generated by the otelhttp middleware; no manual instrumentation needed
for P0.

**Grafana dashboard mount path:** Foundation Grafana's read-only overlay
prevents Docker from creating nested mount paths at runtime. Fix: mount
Navi dashboards to a top-level path `/navi-dashboards` inside the container
rather than nested under `/etc/grafana/provisioning/`.

---

## Phase 6 — CI/CD Pipeline ✓ COMPLETE (2026-04-10, v0.1.4)

**Purpose:** Wire the complete GitHub Actions CI/CD pipeline including
automated testing, release-please version management, staging + prod
deployment with automated rollback, and SMS delivery notification.

### Entry criteria
- Phases 1–5 complete
- GitHub repository exists with main branch
- Self-hosted GitHub Actions runner is registered and online
- Required secrets are set in GitHub repository settings (see task 6.1)

Note: GHCR authentication uses `secrets.GITHUB_TOKEN` — no separate
`GHCR_TOKEN` secret is needed. The workflow must have `packages: write`
permission to push images to ghcr.io.

### Tasks

#### 6.1 — GitHub repository secrets

Configure the following secrets in GitHub repository settings
(`Settings > Secrets and variables > Actions`):

| Secret name | Value |
|-------------|-------|
| `NAVI_HOST` | `10.0.40.10` |

**All sensitive credentials are managed locally via `/opt/navi/.env`**, not
as GitHub secrets. Both `VAULT_TOKEN` and Twilio credentials are sourced from
`.env` on the self-hosted runner at deploy time via `deploy.sh` and
`scripts/notify-sms.sh`. This keeps the credential lifecycle in one place:
the same `.env` file, the same weekly renewal cron, the same Loki alert path.
Storing static tokens in GitHub secrets would create a parallel failure mode
with no monitoring.

Note: the pipeline sends deployment SMS via `scripts/notify-sms.sh`, which
exits gracefully if Twilio credentials are not yet configured. This is distinct
from the in-service SMS delivery channel built in P1.

#### 6.2 — CI workflow

Create `.github/workflows/ci.yml`:

Triggers: `push` to any branch, `pull_request` targeting main

Steps:
1. Checkout with full history (`fetch-depth: 0`)
2. Set up Go 1.23
3. Cache Go modules
4. `go vet ./services/...`
5. `golangci-lint run ./services/...`
6. `go test -race -coverprofile=coverage.out ./services/...`
7. `go build ./services/...`
8. `govulncheck ./services/...` (report only; block on HIGH/CRITICAL)

Runs on: `ubuntu-latest` (the lint/test steps do not require homelab
access).

#### 6.3 — release-please workflow

Create `.github/workflows/release-please.yml`:

Triggers: `push` to `main`

Steps:
1. Use `google-github-actions/release-please-action@v4`
2. Configure with:
   - `release-type: go`
   - `manifest-file: .versions.json`
   - `config-file: release-please-config.json`
   - `token: ${{ secrets.GITHUB_TOKEN }}`

This workflow maintains the Release PR. When the Release PR is merged,
release-please cuts the tag automatically.

#### 6.4 — Deploy workflow

Create `.github/workflows/deploy.yml`:

Triggers: `push` with tag matching `v[0-9]+.[0-9]+.[0-9]+`

Runs on: `[self-hosted]` runner on the homelab node

The workflow MUST have the following permissions block:
```yaml
permissions:
  contents: write   # to commit .last-deployed-version back to main
  packages: write   # to push images to ghcr.io
```

Steps:
1. Checkout with full history
2. **Authenticate to GHCR** using `docker/login-action@v3`:
   ```yaml
   - uses: docker/login-action@v3
     with:
       registry: ghcr.io
       username: ${{ github.actor }}
       password: ${{ secrets.GITHUB_TOKEN }}
   ```
3. Extract version from tag: `VERSION=${GITHUB_REF#refs/tags/}`
4. Read `PREVIOUS_VERSION` from `.last-deployed-version`
5. **Change detection**: identify changed services since the previous
   tag (script: `scripts/detect-changes.sh $PREVIOUS_VERSION $VERSION`)
6. For each changed service:
   a. Build Docker image: `docker build --build-arg VERSION=$VERSION ...`
   b. Push to ghcr.io with `:$VERSION` and `:staging` tags
7. Deploy to staging:
   a. `NAVI_VERSION=$VERSION VAULT_TOKEN=$VAULT_TOKEN_STAGING docker compose -f docker-compose.staging.yml up -d`
   b. Run migrations: `make migrate ENV=staging`
8. Run smoke tests: `make smoketest ENV=staging`
9. **Staging gate**: if smoke tests fail:
   a. Roll back staging: `make rollback ENV=staging VERSION=$PREVIOUS_VERSION SERVICE=digest`
   b. Confirm rollback healthy: `make smoketest ENV=staging`
   c. Send rollback SMS (direct Twilio API call in the workflow)
   d. `exit 1`
10. Promote to prod:
    a. Re-tag image: `:prod` and `:latest`
    b. `NAVI_VERSION=$VERSION VAULT_TOKEN=$VAULT_TOKEN_PROD docker compose -f docker-compose.yml up -d`
    c. Run migrations: `make migrate ENV=prod`
11. Run prod health checks: `make healthcheck ENV=prod`
12. **Prod gate**: if health checks fail:
    a. Roll back prod: `make rollback ENV=prod VERSION=$PREVIOUS_VERSION SERVICE=digest`
    b. Confirm rollback healthy: `make healthcheck ENV=prod`
    c. Send rollback SMS
    d. `exit 1`
13. On success:
    a. Update `.last-deployed-version` to `$VERSION`
    b. Commit `.last-deployed-version` to main using the GitHub Actions
       bot identity (`git config user.email "github-actions[bot]@..."`)
    c. Push commit (this push does NOT trigger another deploy because
       it does not create a tag)
    d. Send success SMS

#### 6.5 — Deployment scripts

Create the following scripts under `scripts/`. Each must be executable
(`chmod +x`) and begin with `#!/usr/bin/env bash` and `set -euo pipefail`.

**`scripts/detect-changes.sh $PREV_VERSION $NEW_VERSION`**
Outputs a space-separated list of service names (e.g. `digest`) that
have changes since PREV_VERSION. If `services/internal/` changed,
outputs all services.

**`scripts/build.sh $VERSION`**
Builds Docker images for changed services. Uses change detection logic
from detect-changes.sh.

**`scripts/deploy.sh $ENV $VERSION $SERVICE`**
Deploys a single service at $VERSION to $ENV using the appropriate
compose file.

**`scripts/rollback.sh $ENV $VERSION $SERVICE`**
Rolls a service back to $VERSION in $ENV. Calls deploy.sh with the
rollback version.

**`scripts/healthcheck.sh $ENV $SERVICE`**
Polls the health endpoint for the given service/env until it returns
200 (max 30 attempts, 5-second interval). Exits 1 if the service
doesn't become healthy within the window.

**`scripts/smoketest.sh $ENV`**
Invokes `make smoketest ENV=$ENV`. Wrapper for compose of smoke tests
when called from the workflow.

**`scripts/vault-seed.sh $ENV`**
Contains the `vault kv put` commands from Phase 4. Idempotent.

**`scripts/check-generated.sh`**
Re-runs oapi-codegen and go-jsonschema, diffs output against committed
files. Exits non-zero if any generated file is stale.

**`scripts/service-addr.sh $ENV $SERVICE`**
Outputs the host:port for the given service in the given environment
(used by the smoketest to know where to connect).

#### 6.6 — Smoke test binary

Create `services/digest/cmd/smoketest/main.go` implementing the P0
smoke test suite as a standalone Go binary. The binary:

- Accepts flags: `-env string` (dev/staging/prod), `-addr string`
  (override service address)
- Resolves the service address from `-addr` or from a default based
  on `-env`
- Runs each test in sequence (not parallel for clarity of failure output)
- Prints `PASS: <test name>` or `FAIL: <test name>: <reason>` for each
- Exits 0 if all tests pass, 1 if any test fails

P0 smoke tests (each as a named function):
1. `TestHealthLive` — GET /v1/health/live → 200
2. `TestHealthReady` — GET /v1/health/ready → 200
3. `TestVersionPresent` — `/v1/health/ready` body contains non-empty
   `version` field
4. `TestPostgresCheck` — `checks.postgres == "ok"` in ready response
5. `TestNATSCheck` — `checks.nats == "ok"` in ready response
6. `TestVaultCheck` — `checks.vault == "ok"` in ready response

### Exit criteria

- [x] Push to a feature branch triggers `ci.yml` and all steps pass
- [x] Merge a `feat:` commit to main causes release-please to create
      or update a Release PR with the correct version bump
- [x] Merging the Release PR causes release-please to cut a tag
- [x] The tag triggers `deploy.yml` on the self-hosted runner
- [x] `make smoketest ENV=staging` passes against the staged service
- [x] The pipeline promotes to prod and the prod health checks pass
- [x] `.last-deployed-version` is updated in a commit on main after
      a successful prod deployment (confirmed v0.1.4)
- [ ] A success SMS is received: "Hey, listen! Navi vX.Y.Z deployed
      successfully." — BLOCKED: Twilio not yet configured (P1 dependency)
- [x] `make rollback ENV=staging VERSION=none SERVICE=digest` exits 1
      with a clear "no previous version" message — correct behaviour;
      silent no-op on a rollback would be dangerous
- [x] `make check-generated` confirms oapi-codegen output is current
- [ ] At least one trace is visible in Foundation Tempo after a prod
      health check request; navigate to the correlated Loki log line
      via trace_id to confirm both pipelines end-to-end
- [ ] At least one structured JSON log line from the prod container is
      queryable in Foundation Loki: `{container_name="navi-prod-digest"} | json`
- [ ] Prod health endpoints added to Foundation Uptime Kuma under a
      "Navi" group: live (`http://10.0.40.10:8084/v1/health/live`, 60s)
      and ready (`http://10.0.40.10:8084/v1/health/ready`, 60s)

---

## Phase 7 — P0 Verification

**Purpose:** End-to-end validation that all P0 success criteria are
met before the milestone is declared done.

### Entry criteria
- Phases 1–6 all complete and exit criteria met
- At least one successful deployment of the service to staging
- release-please is producing Release PRs correctly

### Tasks

#### 7.1 — Full pipeline run: v0.0.1

Execute the full release-please → tag → deploy flow for v0.0.1:

1. Ensure at least one `feat:` commit is on main since initialization
   (if not, add a `feat(infra): initial service scaffolding` commit
   via PR)
2. Confirm release-please has created a Release PR for v0.0.1
3. Review the Release PR changelog — confirm it reflects the commits
4. Merge the Release PR
5. Confirm release-please cuts tag `v0.0.1`
6. Watch `deploy.yml` trigger on the self-hosted runner
7. Confirm all stages complete: staging deploy → smoke tests → prod
   promote → health checks → .last-deployed-version committed
8. Confirm success SMS received

#### 7.2 — Rollback drill

Validate that automated rollback works before P0 is declared done.
This requires a second tag deployment that can fail:

1. On a branch, temporarily modify the health handler to return 503
   regardless of actual dependency state
2. Merge via PR, release-please cuts v0.0.2
3. Confirm the pipeline deploys to staging, smoke tests fail
4. Confirm automated staging rollback fires, rolls back to v0.0.1
5. Confirm rollback SMS received: "Hey, listen! Navi v0.0.2 staging
   deploy failed. Rolled back to v0.0.1."
6. Confirm prod was not touched (prod is still on v0.0.1)
7. Revert the bad commit via PR; release-please cuts v0.0.3 which
   deploys cleanly

#### 7.3 — P0 success criteria checklist

Verify every item in the P0 success criteria before declaring the
milestone done:

**Infrastructure**
- [x] pre-commit hooks installed and passing on the repo
- [x] `.golangci.yml` and `.gitleaks.toml` configured and enforced
- [x] `go.work` and Go module structure correct
- [x] `services/internal/` packages compile and establish real
      connections (Vault, Postgres, NATS, OTEL)

**CI/CD**
- [x] Push to feature branch triggers lint + tests
- [x] Release PR created by release-please for each feat: commit to main
- [x] Version tag triggers staging deploy
- [x] Staging smoke tests gate prod promotion
- [x] Prod deploy follows passing smoke tests automatically
- [ ] Automated rollback fires on smoke test failure (verified in 7.2)
- [ ] Automated rollback fires on health check failure (if not tested
      in 7.2, test manually)
- [ ] Deployment result delivered via SMS — BLOCKED: Twilio not yet configured
- [x] release-please producing Release PRs correctly
- [x] `.versions.json` and `.last-deployed-version` present and managed

**Service**
- [x] `services/digest/` compiles and runs in Docker
- [x] `GET /v1/health/live` returns 200
- [x] `GET /v1/health/ready` returns 200 with Postgres, NATS, and Vault
      connectivity confirmed
- [x] All three compose files work correctly
- [ ] Service version reported in `/v1/health/ready` response body
      matches the deployed tag

**Observability**
- [ ] At least one trace visible in Foundation Tempo (from a health
      request) with the full span visible
- [ ] Navigate from the trace in Tempo to the correlated log line in
      Loki using trace_id — both links work in both directions
- [x] `navi_up` or equivalent metric visible in Foundation Grafana
- [ ] At least one structured JSON log line queryable in Foundation Loki:
      `{container_name="navi-prod-digest"} | json`
- [ ] Health endpoints showing green in Foundation Uptime Kuma under
      the Navi group (live: `http://10.0.40.10:8084/v1/health/live`,
      ready: `http://10.0.40.10:8084/v1/health/ready`)

**Vault**
- [x] All required Vault paths seeded (with real values for Postgres,
      NATS, and OTEL; placeholders acceptable for Twilio, Resend,
      Anthropic in P0)
- [x] Service reads from Vault at startup without error
- [x] SIGHUP-triggered secret reload confirmed working

### Exit criteria (P0 done when)

All checkboxes in 7.3 are checked. No partial passes.

---

## Rollback

This is a plan document for scaffolding a new service. There is no
data to roll back. If the work needs to be undone, delete the
`feat/p0-hello-world` branch and remove any Foundation configuration
changes made in Phase 5.

---

## Open Questions

None. All decisions are resolved in the referenced ADRs or documented
as explicit choices above.
