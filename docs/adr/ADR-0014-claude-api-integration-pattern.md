# ADR-0014 — Claude API Integration Pattern

* **Status:** Accepted
* **Date:** 2026-04-12

---

## Context

Multiple Navi components require access to the Anthropic Claude API: the enricher
uses it for entity extraction, the summarizer uses it for relevance scoring and
digest generation, and every planned intelligence feature (meeting prep briefs,
research briefs, weekly synthesis, Rolodex enrichment) will require it as well.

Without a shared integration layer, each component would independently implement
retry logic, handle rate-limit responses, and track API spend. Duplicating these
concerns across components creates inconsistency and, more critically, makes it
impossible to enforce a single monthly cost budget across the service — each
component would track spend in isolation, with no mechanism to prevent the combined
total from exceeding an acceptable limit.

There is a secondary concern: as the homelab grows, other projects (ruby-core, a
planned home finances application) may also benefit from Claude API access. A
Navi-scoped integration cannot serve these callers. A shared HTTP gateway with
unified cost tracking across all callers is the correct long-term architecture.
However, no confirmed use case in a codebase outside of Navi exists today, and
building cross-project infrastructure for a hypothetical second consumer would
add deployment complexity and an operational dependency with no current benefit.

This ADR establishes the v1 integration pattern: a shared client within Navi's
internal package, designed explicitly for extraction into a standalone gateway
when a second codebase requires Claude access.

---

## Alternatives Considered

**Per-component Claude calls (no shared client)** — Each component that needs
Claude implements its own HTTP client, retry policy, and spend tracking. Rejected
because it duplicates retry logic across components and makes a unified monthly
budget circuit breaker impossible — each component would track spend independently
with no aggregate view.

**Shared HTTP gateway now** — Deploy a standalone Claude gateway service today
with its own Postgres schema for cross-project cost tracking and a REST API.
Rejected because there is no confirmed second consumer. A gateway adds a network
hop to every Claude call, an additional deployment, an additional health dependency,
and a new point of failure — with no realized benefit until a second project
actually adopts Claude. The extraction trigger defined below makes this the v2
path when the need is real.

**Shared Go module published to GitHub** — Extract the client to a standalone
`github.com/rutabageldev/homelab/anthropic` module importable by any project.
Rejected because publishing a shared Go module across separate repos requires
either a private module proxy or coordinating `replace` directives that break
in CI. A network-level API (the gateway path) is the clean cross-language,
cross-repo sharing mechanism.

---

## Decision

### 1. Client package location and interface design

The Claude API client MUST live at `services/internal/anthropic/` and MUST be
exposed as a Go interface, not a concrete struct. All Navi components MUST
accept this interface as a dependency. No component MUST NOT instantiate the
Anthropic HTTP client directly.

The public interface is:

```go
// Client is the interface all Navi components use to call the Claude API.
// The concrete implementation may be swapped for a gateway-backed implementation
// without changing any caller.
type Client interface {
    // Complete sends a prompt and parses the response JSON into result.
    // Returns ErrBudgetExceeded if the monthly limit is reached without
    // making a network call.
    Complete(ctx context.Context, req CompletionRequest, result any) error

    // CompleteText sends a prompt and returns a prose string response.
    // Returns ErrBudgetExceeded if the monthly limit is reached without
    // making a network call.
    CompleteText(ctx context.Context, req CompletionRequest) (string, error)
}
```

### 2. Model selection

The active Claude model MUST be read from Vault at
`secret/data/navi/{env}/anthropic` under the key `model`. It MUST NOT be
hardcoded. The model SHOULD be the current Anthropic Sonnet-tier model and
MUST be updated in Vault when a new model generation is released.
As of this ADR, the model is `claude-sonnet-4-6`.

### 3. Retry policy

Failed Claude API calls MUST be retried on HTTP 429 (rate limit) and HTTP 529
(overloaded) responses. The retry policy is:

- Maximum 3 attempts (1 initial + 2 retries)
- Exponential backoff: 2s, 4s
- Jitter: ±20% of the backoff interval to prevent thundering herd
- No retry on 4xx responses other than 429

### 4. Cost tracking

Every successful Claude API call MUST write a row to the `api_costs` table in
the service's Postgres schema. The table schema is:

```sql
CREATE TABLE api_costs (
    id            UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    service       TEXT           NOT NULL,  -- e.g. 'digest'
    component     TEXT           NOT NULL,  -- e.g. 'enricher', 'summarizer'
    model         TEXT           NOT NULL,
    input_tokens  INTEGER        NOT NULL,
    output_tokens INTEGER        NOT NULL,
    cost_usd      NUMERIC(10, 6) NOT NULL,
    called_at     TIMESTAMPTZ    NOT NULL DEFAULT now()
);
```

The `service` and `component` fields MUST be populated by the caller on every
call. This attribution is required for the gateway migration path — when cost
tracking moves to a centralized service, per-caller attribution must already
be present in the data to reconstruct historical spend by component.

Cost is estimated from token counts using Anthropic's published per-token
pricing for the active model. The cost estimation MUST be updated in code when
the active model changes.

### 5. Monthly budget circuit breaker

The client MUST enforce a monthly cost budget. On initialization and on SIGHUP,
the client reads the monthly limit from Vault at
`secret/data/navi/{env}/limits/claude_monthly_usd`. Before every API call, the
client queries `api_costs` for the sum of `cost_usd` where
`called_at >= date_trunc('month', now())`. If the sum meets or exceeds the
limit, the client MUST return `ErrBudgetExceeded` without making a network call.

`ErrBudgetExceeded` is a named sentinel error. All callers MUST handle it
explicitly and MUST NOT treat it as a generic error. The expected handling
per component is:

- **Enricher:** store the article with empty entity links, set `degraded = true`
  in the published `articles.enriched` event
- **Summarizer:** generate a degraded digest (raw headlines, no AI summaries),
  set `degraded = true` in the published `digest.ready` event

Silence is never the correct handling. A degraded output is always preferable
to no output (ADR-0005).

### 6. SIGHUP reload

The monthly budget limit MUST be reloadable without restarting the service.
On SIGHUP, the client re-reads the limit from Vault and re-queries `api_costs`
for current month spend. This allows the budget to be adjusted in Vault and
applied to the running service without a redeploy.

### 7. Migration path to a shared gateway

The v1 implementation is explicitly scoped to Navi. The extraction trigger is:
**a second codebase outside of the navi repository has a confirmed, active need
to call the Claude API.**

"Confirmed and active" means the use case exists in code or has an approved ADR
in the consuming project — not a hypothetical future requirement. When this
trigger is met, the following migration applies:

**v2 architecture:** A standalone `claude-gateway` service with its own Postgres
schema for unified cost tracking, a single monthly budget across all callers,
and a REST API. The gateway is language-agnostic; any project calls it over HTTP.

**Migration path for Navi:** Because all Navi callers depend on the `Client`
interface rather than the concrete implementation, the migration requires only:

1. Implementing a `GatewayClient` that satisfies the `Client` interface by
   calling the gateway's REST API instead of Anthropic directly
2. Swapping the concrete type at the dependency injection point in `main.go`
3. Removing the `api_costs` table from Navi's schema (data migrated to gateway)

No changes to the enricher, summarizer, or any other business logic component
are required.

Until the gateway exists, projects outside of Navi (ruby-core, home finances)
that require Claude access SHOULD implement their own thin clients. They will
not share Navi's budget circuit breaker. This is the accepted limitation of the
v1 approach.

---

## Consequences

### Positive

- A single implementation of retry logic, cost estimation, and budget enforcement
  is shared across all Navi components. Adding a new Claude-powered feature
  requires no new infrastructure — only a call to the shared client.
- The `service` and `component` attribution fields in `api_costs` provide
  per-component cost visibility from day one, enabling informed decisions about
  which features are most expensive before the gateway exists.
- The `Client` interface design means the gateway migration is a dependency
  injection swap at `main.go`, not a refactor of business logic. All callers
  are insulated from the underlying transport.
- The budget circuit breaker prevents runaway Claude spend during production
  incidents where, for example, a bug causes the enricher to reprocess articles
  repeatedly.

### Negative

- Until the gateway is built, there is no unified view of total Claude spend
  across the homelab. If ruby-core or a home finances app adopts Claude before
  the gateway exists, their spend is invisible to Navi's circuit breaker. The
  combined homelab Claude bill could exceed a comfortable total even if each
  project individually stays within its own limit.
- Per-project budget limits are a weak substitute for a unified budget. Two
  projects each configured to a $10 limit produce a $20 ceiling — which may
  or may not match actual intent.
- Cost estimation from token counts is an approximation. Anthropic's published
  pricing can change; the estimation logic must be updated when it does.

### Neutral

- The model is managed per environment in Vault, not in code. Updating the
  active model requires a Vault write and a SIGHUP, not a code change or
  redeployment. This is consistent with how all Navi secrets are managed
  (ADR-0002).
- The `api_costs` table is defined in the digest service schema for v1. In the
  gateway migration, this table moves to the gateway's schema. The down
  migration for `api_costs` in the digest schema is part of the gateway
  migration PR, not a standalone operation.
