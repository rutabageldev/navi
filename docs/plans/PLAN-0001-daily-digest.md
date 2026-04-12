# PLAN-0001 — Daily Digest Delivery Over Email

* **Status:** In Progress
* **Date:** 2026-04-12
* **Project:** navi
* **Roadmap Item:** `docs/roadmap/ROADMAP.md` — Daily Digest Delivery Over Email
* **Branch:** `feature/p1-daily-digest`
* **Related ADRs:** ADR-0004, ADR-0005, ADR-0006, ADR-0008, ADR-0009, ADR-0011, ADR-0002 (amended in Block 1), ADR-0014 (proposed), ADR-0015 (proposed)

---

## Scope

This plan implements the complete daily digest pipeline: RSS collection → enrichment →
Claude-based relevance scoring and summarization → structured email delivery via Resend.
It covers the full ADR-0004 database schema, the NATS event pipeline, the Payload/Channel
delivery abstraction from ADR-0006, and the monitoring and operational baseline required
before production deployment. It does not cover weekly or monthly digests (those are built
on top of this pipeline), authenticated feed fetching for The Economist or other paid
sources, or the SMS/intent pipeline.

---

## Subject Naming Resolution

During plan research, a conflict was found between ADR-0002 and ADR-0011 on NATS subject
naming. This section documents the resolution; Block 1 amends both ADRs to reflect it.

**Ruby-core ADR-0027 defines:**
```
{source}.{class}.{type}[.{id}[.{action}]]
```
The `{class}` token (one of `events`, `commands`, `audit`) is mandatory and sits immediately
after source. This is how ruby-core ACL wildcards and monitoring subscriptions are designed.
Confirmed in live code: `ha.events.light.>`, `ruby_engine.commands.notify.*`,
`ruby_presence.events.state.*`.

**Navi's constraint:** All three Navi environments share the same ruby-core NATS instance.
Ruby-core uses separate instances per env and therefore has no env prefix in subjects. Navi
requires the env token for isolation. The compound source is therefore `navi.{env}`, and
class lands at position 3.

**Resolved Navi convention:**
```
navi.{env}.{class}.{type}[.{action}]
```

Where `{class}` MUST be one of: `events`, `commands`, `errors`.
Audit events follow the shared audit namespace: `audit.navi.{type}`.

**Wildcard capabilities:**
- All Navi prod events: `navi.prod.events.>`
- All Navi prod commands: `navi.prod.commands.>`
- All article lifecycle events: `navi.prod.events.articles.>`
- Cross-env monitoring: `navi.*.events.>`

**Full v1 subject map (authoritative after Block 1):**

| Subject | Class | Publisher | Subscriber |
|---|---|---|---|
| `navi.{env}.events.articles.collected` | events | collector | enricher |
| `navi.{env}.events.articles.enriched` | events | enricher | store |
| `navi.{env}.events.digest.ready` | events | summarizer | delivery dispatcher |
| `navi.{env}.events.sms.received` | events | webhook handler (future) | intent parser (future) |
| `navi.{env}.commands.sms.send` | commands | intent handler (future) | SMS channel (future) |
| `navi.{env}.errors.reported` | errors | any component | monitoring (future) |
| `audit.navi.{type}` | — (shared audit ns) | webhook handler (future) | audit-sink (future) |

**Changes from ADR-0002:**
- `navi.{env}.events.digest.created` → `navi.{env}.events.digest.ready`
  ("ready" is correct: the delivery consumer triggers when a digest is ready for dispatch,
  not at the moment it is inserted into Postgres)
- `navi.{env}.events.sms.inbound` → `navi.{env}.events.sms.received`
  (past participle, consistent with ruby-core event naming convention)
- `navi.{env}.events.sms.outbound` → `navi.{env}.commands.sms.send`
  (reclassified as a command — it is a directive to send, not a state change notification)
- `navi.{env}.security.events` → `audit.navi.sms_unauthorized`
  (moved to shared audit namespace, consistent with ADR-0002's own `audit.navi.{type}`
  pattern and ruby-core's `ruby_gateway.audit.command_executed`)

**Changes from ADR-0011:**
- All subjects gain the `{class}` token at position 3
- `navi.{env}.articles.collected` → `navi.{env}.events.articles.collected`
- `navi.{env}.digest.ready` → `navi.{env}.events.digest.ready`
- `navi.{env}.sms.inbound` → `navi.{env}.events.sms.received`
- `navi.{env}.sms.outbound` → `navi.{env}.commands.sms.send`
- `navi.{env}.errors` → `navi.{env}.errors.reported`

---

## Proposed ADRs

Two new ADRs must be written and approved before the blocks they govern. They are embedded
in the block sequence where they must appear.

**ADR-0014 — Claude API Integration Pattern**

Documents the shared `services/internal/anthropic/` client: model selection from Vault,
structured JSON response parsing, retry policy (3 attempts, exponential backoff from 2s),
per-call cost estimation and accumulation, monthly budget circuit breaker state (tracked
in a Postgres `api_costs` table, reset on the 1st of each month), and SIGHUP reload of
cost limits from Vault.

This is a cross-product ADR. Every future AI-powered feature — meeting prep briefs,
research briefs, weekly synthesis, professional pulse — will import this client. Getting
the cost accounting and circuit breaker state persistence right now prevents each future
feature from reimplementing them differently. The `api_costs` table shape must be decided
before migration 0002 is written.

**ADR-0015 — Article Content Acquisition**

Documents how the collector fetches and extracts readable text from RSS feeds and full
article pages: User-Agent header policy, conditional HTTP requests (ETag/If-Modified-Since),
HTML-to-text extraction approach, content length limit before Claude API calls (8,000
tokens), per-feed timeouts (10s poll, 15s article fetch), and graceful degradation when
content is unavailable (store title and URL only, skip for summarization).

This ADR also establishes the authenticated source pattern for The Economist and paid
feeds: a `vault_key` field on a feed entry in `feeds.yaml` that references a Vault path
for the session credential. Establishing this now means the Full Source Coverage feature
adds feeds without rearchitecting the collector.

---

## Pre-conditions

- [x] P0 smoke tests pass on current staging deployment: `make smoketest ENV=staging`
      (confirmed 2026-04-12: 6/6 tests passed)
- [x] Foundation Postgres (`10.0.40.10:5432`) is reachable and the `navi_staging` schema
      exists (verified by P0 smoke tests)
- [x] NATS `NAVI_STAGING` stream exists and is reachable (verified by P0 smoke tests)
- [x] Vault paths for postgres, nats, and telemetry are seeded for staging (required by P0)
- [x] Subject naming resolution above has been reviewed and accepted; ADR-0002 and ADR-0011
      both amended 2026-04-12 (pre-Block 1 session setup commit)

---

## Blocks

---

### Block 1 — Resolve Drift and Define Event Contracts ✓ Complete (`6ca7508`)

**What this block produces:**
- `docs/events/REGISTRY.md` — all v1 event types with subjects, publishers, and consumers
- `docs/events/schemas/articles.collected/v1.json` — JSON Schema for the articles.collected data payload
- `docs/events/schemas/articles.enriched/v1.json`
- `docs/events/schemas/digest.ready/v1.json`
- `services/digest/cmd/migrate/main.go` — standalone migration binary (currently missing; `make migrate` references it but the binary does not exist)

**Already complete (session setup commit, pre-Block 1):**
- Amendments to ADR-0002 and ADR-0011 adopting the resolved subject convention
- Makefile and `scripts/deploy.sh` updated to use explicit Docker Compose project names

**Why first:** ADR-0011 requires all event types to be registered in `REGISTRY.md` and
have schema files before any code publishes or subscribes to them. No event code can be
written until this block is complete. The migrate binary is required for Block 4
verification.

**Exit criteria:**
- `make validate-schemas` exits 0 — all three schema files are valid JSON Schema
- `go build ./services/digest/cmd/migrate/...` exits 0
- `make migrate ENV=staging` exits 0 — confirms the binary connects and finds no new
  migrations to apply (migration 0001 already applied; this validates the binary works)
- `pre-commit run --all-files` exits 0
- `docs/events/REGISTRY.md` exists and contains entries for all three v1 event types
  with subjects matching the resolved convention above

**Rollback:** All changes are documentation and a new binary. Roll back by reverting
commits. No persistent state affected.

**Commit:** `feat(events): add event registry, v1 JSON schemas, and migrate binary`

---

### Block 2 — ADR-0014: Claude API Integration Pattern ✓ Complete (`eaaede0`)

**What this block produces:**
- `docs/adr/ADR-0014-claude-api-integration-pattern.md`

**Why here:** The `api_costs` table shape is defined in this ADR and must land in migration
0002 (Block 4). The circuit breaker interface is consumed by both the enricher (Block 6)
and the summarizer (Block 7). This ADR must be approved before Block 4 writes the
migration or Block 5 builds the shared client.

**Exit criteria:**
- ADR file exists at `docs/adr/ADR-0014-claude-api-integration-pattern.md`, status `Accepted`
- ADR specifies the `api_costs` table schema (minimum fields: `id`, `service`,
  `operation`, `model`, `cost_usd`, `called_at`) — this table will land in migration 0002
- ADR defines the `ErrBudgetExceeded` sentinel error and the circuit breaker open/close
  contract so Block 5 and Block 7 can implement against it

**Commit:** `docs(adr): add ADR-0014 Claude API integration pattern`

---

### Block 3 — ADR-0015: Article Content Acquisition ✓ Complete (`b2d4ada`)

**What this block produces:**
- `docs/adr/ADR-0015-article-content-acquisition.md`

**Why here:** The collector in Block 4 must be built against a decided content extraction
approach. This ADR resolves: which HTML extraction library, the content length limit,
per-feed timeouts, and the authenticated source credential field in `feeds.yaml`. Without
it, the collector makes assumptions that will conflict with The Economist feature.

**Exit criteria:**
- ADR file exists at `docs/adr/ADR-0015-article-content-acquisition.md`, status `Accepted`
- ADR specifies the `vault_key` field as the authenticated source credential mechanism in
  `feeds.yaml`, aligning with the `auth: economist_session` example in ADR-0005
- ADR specifies the content length limit (in tokens or characters) that governs how much
  raw content the collector stores and passes downstream

**Commit:** `docs(adr): add ADR-0015 article content acquisition`

---

### Block 4 — Database Migration and Store Layer

**What this block produces:**
- `services/digest/migrations/0002_schema.up.sql` — full ADR-0004 schema:
  - v1 (required now): `feeds`, `articles`, `digests`, `digest_articles`, `topics`,
    `article_topics`, `feedback_signals`
  - Forward-declared (empty until later features): `people`, `companies`,
    `person_companies`, `article_companies`, `article_people`, `profile`,
    `portfolio_entries`
  - From ADR-0014: `api_costs`
- `services/digest/migrations/0002_schema.down.sql`
- `services/digest/internal/store/` package with typed functions for all v1 tables:
  `FeedStore`, `ArticleStore`, `DigestStore`, `TopicStore`, `CostStore`
- Unit tests for all store functions using a real Postgres connection against the
  staging schema (no mocks — the project standard prohibits mocking the database)

**Key constraints:**
- All migrations additive-only (ADR-0004, ADR-0003)
- No `NOT NULL` columns without defaults
- `people`, `companies`, and profile tables are defined but remain empty until the
  Rolodex feature ships — this is intentional per ADR-0004

**Exit criteria:**
- `make migrate ENV=staging` applies 0002 cleanly: exits 0, all tables present in
  `navi_staging` (verify with `psql -c '\dt navi_staging.*'` — must show at least 14 tables)
- `make migrate ENV=staging` is idempotent: running it a second time exits 0 without error
- `go test -race ./services/digest/internal/store/...` passes — tests run against a real
  Postgres connection to staging
- `go vet ./services/digest/internal/store/...` exits 0

**Rollback:** `make migrate ENV=staging` with the down file drops all 0002 tables. Because
0002 creates new tables only (no modification of existing tables), rollback is clean on
staging. On prod, after a first digest runs, `api_costs`, `digests`, and `articles` will
contain real data — treat a prod rollback of this migration as destructive and confirm
explicitly.

**Commit:** `feat(store): add full ADR-0004 schema migration and postgres access layer`

---

### Block 5 — RSS Collector

**What this block produces:**
- `services/digest/config/feeds.yaml` — populated with all free sources from STRATEGY.md:

  *Tech & Banking bucket (daily/wire tier):*
  Reuters Technology (wire), AP News Tech (wire), Ars Technica (daily), MIT Technology
  Review (analytical), Wired (daily), TechCrunch (daily), The Verge (daily), Axios Tech
  (daily), American Banker (daily), Finextra (daily), The Financial Brand (analytical),
  PYMNTS (daily)

  *Global Events bucket (daily/wire tier):*
  Reuters World (wire), AP News Top Stories (wire), BBC News (daily), The Guardian (daily),
  NPR News (daily), Foreign Policy (analytical), ProPublica (analytical)

  Paid sources (Economist, Stratechery, The Information) stubbed with `enabled: false` and
  a comment: `# Requires authenticated fetch — see Full Source Coverage roadmap item`.

- `services/digest/internal/collector/` package: `Collector` struct, `Poll()` per feed,
  tiered cadence (wire: 20min, daily: 2hr, analytical: 24hr), conditional HTTP requests
  (ETag / If-Modified-Since per feed), full-page content fetch for summary-only RSS
  per ADR-0015, content trimmed to ADR-0015's length limit before storing `raw_content`,
  URL-based deduplication against Postgres before publishing, CloudEvents envelope
  construction and publish to `navi.{env}.events.articles.collected`

- Unit tests: feed polling with mock HTTP server, URL dedup logic, CloudEvents envelope
  shape validation, single-feed failure isolation (one failing feed must not block others)

**Key constraints:**
- Collector MUST NOT enrich, score, or summarize — it fetches and publishes (ADR-0005)
- All published events use subjects from the resolved convention:
  `navi.{env}.events.articles.collected`
- Trace context injected into every outbound CloudEvents envelope (ADR-0008, ADR-0011)

**Exit criteria:**
- `go test -race ./services/digest/internal/collector/...` passes
- Test confirms envelope shape: `specversion=1.0`, `type=navi.digest.article.collected`,
  `navischema=articles.collected/v1`, non-empty `traceparent`
- Test confirms a URL already in the `articles` table is silently dropped and no envelope
  is published
- Test confirms a feed timeout does not propagate to subsequent feeds in the same poll cycle
- `go vet ./services/digest/internal/collector/...` exits 0

**Commit:** `feat(collector): implement RSS collector with tiered polling and URL deduplication`

---

### Block 6 — Shared Claude API Client

**What this block produces:**
- `services/internal/anthropic/` package: `Client` struct, `Complete()` for structured
  JSON responses, `CompleteText()` for prose responses, retry with exponential backoff
  (3 attempts, base 2s per ADR-0005), per-call cost estimation from response token counts,
  `CostTracker` that reads the monthly limit from Vault, accumulates spend from the
  `api_costs` Postgres table, and returns `ErrBudgetExceeded` when the limit is reached
  without making a network call, SIGHUP reloads the cost limit from Vault

- Unit tests: mock HTTP server returning structured Claude API responses, circuit breaker
  test (seed `api_costs` past the limit, confirm next call returns `ErrBudgetExceeded`
  with no outbound HTTP), retry test (529 response → 3 retries → error)

**Why shared:** This client is imported by the enricher (entity extraction), the summarizer
(relevance scoring + summarization), and every future AI-powered feature (meeting prep,
research, weekly synthesis). The cost accounting and circuit breaker live once, not once
per feature.

**Exit criteria:**
- `go test -race ./services/internal/anthropic/...` passes
- `TestCircuitBreaker`: after seeding `api_costs` past the monthly limit, `Client.Complete()`
  returns `ErrBudgetExceeded` without making any HTTP requests (confirmed by asserting
  zero calls on the mock server)
- `TestRetryWithBackoff`: on a 529 response, the client retries exactly 3 times before
  returning an error
- `go vet ./services/internal/anthropic/...` exits 0

**Commit:** `feat(internal): add Claude API client with cost tracking and circuit breaker`

---

### Block 7 — Enricher

**What this block produces:**
- `services/digest/internal/enricher/` package: `Enricher` struct subscribing to
  `navi.{env}.events.articles.collected`, entity extraction via Claude API using the
  structured JSON prompt from ADR-0005, stores enriched article with entity links in
  Postgres, publishes to `navi.{env}.events.articles.enriched`

- Title near-duplicate detection using the threshold specified in ADR-0015 (Levenshtein
  ratio); if a near-duplicate is detected, the article is discarded and not published

- Unit tests with mock Claude client; test confirms trace context is extracted from
  inbound envelope and propagated to outbound envelope (ADR-0008)

**Key constraints:**
- Enricher errors on individual articles are logged and the article is skipped; the
  enricher continues processing subsequent messages
- `ErrBudgetExceeded` from the Claude client triggers a degraded path: store the article
  with empty entity links, publish to `articles.enriched` with a `degraded: true` flag

**Exit criteria:**
- `go test -race ./services/digest/internal/enricher/...` passes
- Test confirms enriched article is written to Postgres (`article_topics` rows present)
  and `articles.enriched` envelope published
- Test confirms a near-duplicate article (existing URL in DB) is silently discarded
- Test confirms `traceparent` in the outbound envelope matches the span started from the
  inbound envelope's traceparent
- `go vet ./services/digest/internal/enricher/...` exits 0

**Commit:** `feat(enricher): implement article enricher with entity extraction and deduplication`

---

### Block 8 — Summarizer

**What this block produces:**
- `services/digest/internal/summarizer/` package: `Summarizer` struct triggered by an
  internal scheduler signal (not a NATS subscription — the scheduler fires an in-process
  channel, the summarizer does not consume NATS messages), queries articles collected in
  the last 24 hours not already in a daily digest, relevance scoring via Claude API using
  the structured JSON prompt from ADR-0005, selection of top 5-7 tech/banking and 4-5
  global stories by score, per-article summarization via Claude API (prose prompt from
  ADR-0005), stores `digests` and `digest_articles` records, publishes to
  `navi.{env}.events.digest.ready`

- Unit tests with mock Claude client and store

**Key constraints:**
- When `ErrBudgetExceeded`: generate degraded digest (raw headlines and source URLs only,
  no Claude summaries), prefix content with a note explaining summarization is paused,
  store and publish normally — silence is worse than a degraded digest (ADR-0005, ADR-0009)
- When zero articles meet the relevance threshold: generate thin digest with a note, do
  not skip generation
- Relevance scoring prompt encodes user professional context exactly as specified in
  ADR-0005 — this is the established pattern that prevents Rolodex and profile data from
  entering Claude prompts even when those features exist

**Exit criteria:**
- `go test -race ./services/digest/internal/summarizer/...` passes
- `TestDegradedDigest`: mock client returns `ErrBudgetExceeded`; summarizer produces a
  `digests` record whose `content_html` contains the degraded-mode note and no AI summaries
- `TestThinDigest`: no articles exceed relevance threshold; a `digests` record is still
  produced, with a note
- `TestDigestReady`: `navi.{env}.events.digest.ready` envelope published with valid
  CloudEvents shape and `navischema=digest.ready/v1`
- `go vet ./services/digest/internal/summarizer/...` exits 0

**Commit:** `feat(summarizer): implement relevance scoring and daily digest generation`

---

### Block 9 — Delivery: Payload/Channel Abstraction and Email Channel

**What this block produces:**
- `services/digest/internal/delivery/` package:
  - `Payload` interface with `Subject() string`, `Sections() []Section`, `Metadata() map[string]string`
  - `Section` struct with `Title`, `Body`, `Links []Link`
  - `Channel` interface with `Name() string`, `Send(ctx, Payload) error`
  - `Dispatcher` struct subscribing to `navi.{env}.events.digest.ready`, unmarshals the
    digest record from Postgres, constructs a `Payload`, routes to all active channels
  - `EmailChannel` implementation: renders `Payload` to structured HTML email via
    `html/template`, sends via Resend API

- Email template structure per ADR-0006:
  - Header: "Hey, listen!" in Navi accent color, date, digest type
  - Sections rendered in order with visual separation
  - Per-article entry: headline, source badge, 3-4 sentence summary, citation link
  - Footer: digest metadata
  - Section `Body` fields contain readable prose, not visual-only HTML — required so the
    audio channel (Later horizon) can render sections without a content refactor (ADR-0006)

- Unit tests: template rendering with synthetic digest payload, Resend API call via mock
  HTTP server

**Key constraints:**
- In staging: Resend sandbox key — emails are rendered and accepted by Resend but not
  delivered to a real inbox (ADR-0003 staging isolation)
- In prod: `resend.to_address` from Vault determines the recipient; never hardcoded
- The `Payload`/`Channel` abstraction must not assume email is the only channel — the
  dispatcher routes based on event type, and audio/SMS channels are added as new
  `Channel` implementations (ADR-0006)

**Exit criteria:**
- `go test -race ./services/digest/internal/delivery/...` passes
- `TestEmailRender`: produces valid HTML containing "Hey, listen!" header, at least one
  article section with headline and source link, and footer
- `TestResendSend` via mock HTTP server: request body contains `from`, `to`, `subject`,
  and `html` fields; `html` field content matches `TestEmailRender` output
- `go vet ./services/digest/internal/delivery/...` exits 0

**Commit:** `feat(delivery): implement Payload/Channel abstraction and Resend email channel`

---

### Block 10 — Scheduler, Service Wiring, and Expanded Smoke Tests

**What this block produces:**
- `services/digest/internal/scheduler/` package: `Scheduler` struct using `robfig/cron`,
  daily (`0 5 * * *`), weekly (`0 16 * * 5`), and monthly (`0 6 1 * *`) jobs firing
  internal channel signals to the summarizer (not NATS — these are in-process triggers)

- Updated `services/digest/cmd/digest/main.go`: initializes all components (collector
  goroutines per feed tier, enricher NATS subscriber, scheduler wired to summarizer,
  delivery dispatcher NATS subscriber); loads new Vault secrets at startup (anthropic
  `api_key` and `model`, resend `api_key`/`from_address`/`to_address`, Claude monthly
  cost limit at `secret/data/navi/{env}/limits/claude_monthly_usd`)

- SIGHUP reload extended to reload `feeds.yaml`, Resend credentials, and Claude cost limit
  without restarting the binary

- Expanded `services/digest/cmd/smoketest/main.go`: adds three tests beyond the existing
  P0 suite: (1) at least one feed has been polled within 60s of startup (verify via
  metrics or log line), (2) NATS durable consumer registrations for articles.collected
  and digest.ready are present, (3) Resend delivery channel initialized without error
  (does not send a real email)

**Exit criteria:**
- `go build ./...` exits 0 across the entire workspace
- `go test -race ./...` passes with no race conditions detected
- `go vet ./...` exits 0
- Service starts in dev (`make dev`) and logs confirm within 30 seconds:
  `"msg": "collector started"`, `"msg": "enricher subscribed"`, `"msg": "scheduler started"`,
  `"msg": "delivery dispatcher subscribed"`
- `make smoketest ENV=staging` passes all existing tests plus the three new ones

**Commit:** `feat(digest): wire scheduler and all service components into binary`

---

### Block 11 — Monitoring, Backup, and Pruning

**What this block produces:**

*Observability:*
- `monitoring/grafana/dashboards/navi.json` — complete v1 dashboard per ADR-0009 with
  six rows: System Health, Collection, Digest Generation, API Costs, Delivery, Logs
- `monitoring/prometheus/alerts.yaml` — all alert rules from ADR-0009:
  `DigestNotDelivered`, `NaviServiceDown`, `DBConnectionFailed`, `ClaudeAPIBudgetExceeded`,
  `FeedErrorRateHigh`, `DeliveryErrorRateHigh`, `ClaudeAPIErrorRateHigh`

*Operational jobs:*
- Backup job added to `docker-compose.yml` and `docker-compose.staging.yml`: daily
  `pg_dump` of the appropriate schema at 3:00am, gzip compressed, 30-day rolling
  retention on a mounted backup volume
- Offsite sync container added to prod compose: `rclone sync` to OneDrive after backup
- `services/digest/cmd/prune/main.go`: pruning binary implementing ADR-0013 retention
  policies (nullify `raw_content` after 30 days, delete articles with no digest inclusion
  after 14 days, retain digests for 24 months, log and emit metrics on rows affected)
- Pruning job added to prod and staging compose: weekly at Sunday 2:00am
- Makefile targets added: `make backup ENV=prod`, `make prune ENV=prod`,
  `make restore ENV=prod BACKUP=<filename>`

*Foundation configuration changes (manual steps — cannot be done from this repo):*
1. Add Navi scrape job to `/opt/foundation/monitoring/prometheus/prometheus.yml` per
   ADR-0009: target `10.0.40.10:8084` (prod metrics port), job name `navi`. Reload
   Prometheus after edit.
2. Add Navi health endpoints to Foundation Uptime Kuma under a "Navi" group:
   `GET https://navi.home.arpa/v1/health/live` and `/v1/health/ready` at 60s intervals.
3. Configure Alertmanager Webhook receiver to route `navi_*` alerts to Twilio SMS
   (Alertmanager must be deployed in Foundation if not already present).

**Exit criteria:**
- `promtool check rules monitoring/prometheus/alerts.yaml` exits 0
- `python3 -m json.tool monitoring/grafana/dashboards/navi.json > /dev/null` exits 0
- `make prune ENV=dev` runs without error (dev environment running locally)
- `make backup ENV=staging` creates a timestamped `.dump.gz` file in the backup volume
  (requires staging running)
- Foundation Prometheus confirms scrape target: `curl -s 'http://10.0.40.10:9090/api/v1/query?query=up{job="navi"}' | jq '.data.result'` returns a non-empty result
- Navi alert rules visible in Prometheus: `curl -s http://10.0.40.10:9090/api/v1/rules | jq '.data.groups[].rules[].name' | grep -c Navi` returns 7

**Commit:** `feat(monitoring): add Grafana dashboard, Prometheus alerts, backup, and pruning`

---

### Block 12 — Vault Seed and Staging End-to-End Validation

**What this block produces:**
- Updated `scripts/vault-seed.sh` with all new Vault paths required by this plan:
  - `secret/data/navi/staging/anthropic`: `api_key` (placeholder), `model` (`claude-sonnet-4-6`)
  - `secret/data/navi/staging/resend`: `api_key` (Resend sandbox key), `from_address`,
    `to_address`
  - `secret/data/navi/staging/limits/claude_monthly_usd`: `15`
- Staging Vault paths seeded with real values (api_key values entered manually into Vault;
  not committed to the repo)
- Staging deployment via `make deploy ENV=staging SERVICE=digest`

**This block has no code commit.** It is a validation gate. Defects found here generate
fix commits on the feature branch before exit criteria are met.

**Exit criteria — all must pass before this plan is complete:**
- `make vault-seed ENV=staging` exits 0 with all new paths seeded
- `make deploy ENV=staging SERVICE=digest` deploys successfully; container starts
  without error and `/v1/health/ready` returns 200 within 30 seconds
- `make smoketest ENV=staging` passes all tests (existing P0 suite + Block 10 additions)
- Within 5 minutes of deploy: staging Loki shows at least one log line with
  `"component": "collector"` and `"msg": "feed polled"`
- Within 30 minutes of deploy: staging Loki shows at least one log line with
  `"component": "enricher"` and `"msg": "article enriched"`
- After manually triggering the summarizer (via a one-off make target or debug endpoint
  added temporarily): `navi_staging.digests` table in Postgres contains one row, and
  `navi_staging.digest_articles` contains more than zero rows
- Resend sandbox dashboard (resend.com) shows the digest email was received; HTML renders
  correctly with "Hey, listen!" header, at least two content sections, and source citations
  with valid links
- Navi Grafana dashboard panel "Articles fetched per feed per day" shows data for at
  least two feeds

---

## Rollback

**Staging rollback (automated):** The CI/CD pipeline (ADR-0003) fires automated rollback
if smoke tests fail after a deploy. `make rollback ENV=staging VERSION={previous} SERVICE=digest`
is available for manual use.

**Migration rollback (staging):** `make migrate ENV=staging` with the 0002 down file drops
all newly created tables. Because 0002 creates new tables only, staging rollback is clean.

**Migration rollback (prod):** After the first digest runs, `api_costs`, `digests`, and
`articles` will contain real data. Rolling back migration 0002 on prod drops this data.
Treat a prod migration rollback as a destructive operation requiring explicit confirmation.
The recommended path is: roll back the binary, leave the schema in place, and fix forward.

**Binary rollback (prod):** `make rollback ENV=prod VERSION={previous} SERVICE=digest`.
The previous binary (P0) does not reference any 0002 tables, so running it against the
0002 schema is safe — it will ignore the new tables.

---

## Drift Observations (surfaced during plan research)

1. **`docs/events/REGISTRY.md` does not exist.** ADR-0011 requires all event types to be
   registered before use in code. To be resolved in Block 1.

2. **`docs/events/schemas/` has no schema files.** ADR-0011 requires schema files per event
   type. To be resolved in Block 1.

3. **ADR-0002 and ADR-0011 define conflicting NATS subject naming.** Resolved and formally
   amended in the session setup commit (2026-04-12) before Block 1 begins.

4. **`services/digest/cmd/migrate/` binary does not exist** despite being referenced in
   `make migrate`. To be resolved in Block 1.

5. **Docker Compose project isolation missing from `make dev` and `make logs`.** Both targets
   invoked `docker compose` without an explicit `-p` project name, exposing prod to
   accidental container collision. Resolved in the session setup commit (2026-04-12):
   `Makefile` now derives `COMPOSE_PROJECT = navi-$(ENV)` and passes it to both targets;
   `scripts/deploy.sh` now passes `-p "navi-${ENV}"` to every `docker compose up`
   invocation.

---

## Open Questions

None — the subject naming question raised in the plan research has been resolved above.
This plan is ready for approval.
