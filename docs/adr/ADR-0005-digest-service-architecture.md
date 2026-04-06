# ADR-0005: Digest Service Architecture

## Status
Accepted

## Date
2026-04-06

## Context

The digest service is the first feature built in the navi repository
and the foundation on which all intelligence delivery features are
built. It is responsible for collecting content from external sources,
enriching and summarizing it via the Claude API, and storing generated
digests for delivery.

This ADR documents the internal architecture of the digest service:
its components, their responsibilities, how they communicate, and the
configuration model for content sources. Delivery (how the digest
reaches the user) is addressed separately in ADR-0006.

## Decision

### Service Layout

The digest service lives at `services/digest/` and is structured as
a standard Go service with distinct internal packages per component:

```
services/digest/
├── cmd/
│   ├── digest/          # main binary
│   └── smoketest/       # smoke test binary (see ADR-0003)
├── internal/
│   ├── collector/       # RSS polling and article fetching
│   ├── enricher/        # entity extraction and embedding generation
│   ├── summarizer/      # Claude API integration, digest generation
│   ├── scheduler/       # cron scheduling for all timed jobs
│   └── store/           # Postgres access layer
├── config/
│   └── feeds.yaml       # source configuration
├── migrations/          # golang-migrate SQL files
└── Dockerfile
```

### Components

**Collector**
Polls configured RSS feeds and fetches article content. Responsibilities:

- Reads feed configuration from feeds.yaml at startup (hot-reloadable)
- Polls each feed on its configured tier cadence:
  - wire tier (Reuters, AP): every 20 minutes
  - daily tier (Axios, TechCrunch, The Verge, etc.): every 2 hours
  - analytical tier (Economist, Stratechery, MIT TR, etc.): once daily
- Uses conditional HTTP requests (ETag, Last-Modified) to minimise
  unnecessary fetches -- most polls cost a single HTTP round trip
- Fetches full article content via HTTP for feeds that provide only
  summaries in RSS
- Handles authenticated fetches for paid sources (Economist session
  cookie, others) using credentials from Vault
- Deduplicates by URL before publishing -- articles already in Postgres
  are silently dropped
- MUST publish a CloudEvents envelope to `navi.{env}.articles.collected`
  for each new article

The collector MUST NOT summarize, score, or enrich. It fetches and
publishes. All downstream intelligence is the responsibility of other
components.

**Enricher**
Subscribes to `navi.{env}.articles.collected`. Responsibilities:

- Generates an embedding vector for the article content using the
  Anthropic API (or a lightweight embedding model -- see note below)
- Performs semantic deduplication: if cosine similarity to a recently
  collected article exceeds 0.92, the article is discarded as a
  near-duplicate
- Extracts named entities: companies and topics mentioned in the
  article using a lightweight Claude API call with a structured prompt
- Stores the enriched article and entity links in Postgres
- Publishes to `navi.{env}.articles.enriched`

Note on embeddings: Anthropic does not currently offer a dedicated
embeddings API. For v1, entity extraction is handled by Claude with
a structured JSON prompt, and deduplication uses URL-based matching
plus title similarity (Levenshtein distance). Embedding-based
deduplication is introduced when a suitable model is integrated.

**Summarizer**
Subscribes to a scheduler trigger (not to individual article events).
Runs on a fixed schedule to generate digests from accumulated articles.
Responsibilities:

- Queries Postgres for articles collected in the relevant window
  (last 24 hours for daily, last 7 days for weekly, last 30 days
  for monthly) that have not yet appeared in a digest of the same type
- Scores articles by relevance to the user's professional context
  using a Claude API call with a system prompt that includes:
  - User context (Director of PM, financial services, Capital One)
  - The two content buckets (tech/banking, global events)
  - Instruction to prefer analytical signal over breaking news
- Selects the top 5-7 articles per bucket by relevance score
- Generates per-article summaries (3-4 sentences, with source citation)
  via Claude API
- For weekly and monthly digests, generates a thematic synthesis
  across all selected articles rather than per-article summaries
- Stores the digest record and digest_article links in Postgres
- Publishes to `navi.{env}.digest.ready` to trigger delivery

**Scheduler**
Manages all timed triggers within the service using robfig/cron.
Defined schedules:

```
Daily digest:    0 5 * * *     (5:00am -- delivery targets 6:30am)
Weekly digest:   0 16 * * 5   (4:00pm Friday)
Monthly digest:  0 6 1 * *    (6:00am 1st of month)
```

The scheduler publishes internal trigger events that the summarizer
subscribes to. This keeps scheduling logic separate from business
logic and makes the summarizer independently testable.

### Feed Configuration

All content sources are defined in `config/feeds.yaml`. The collector
reads this file at startup and on SIGHUP for hot reloads. Adding or
removing a source never requires a code change or redeployment.

```yaml
feeds:
  - name: Reuters Technology
    url: https://feeds.reuters.com/reuters/technologyNews
    bucket: tech_banking
    tier: wire
    enabled: true

  - name: AP News
    url: https://rsshub.app/apnews/topics/ap-top-news
    bucket: global_events
    tier: wire
    enabled: true

  - name: Ars Technica
    url: https://feeds.arstechnica.com/arstechnica/index
    bucket: tech_banking
    tier: daily
    enabled: true

  - name: The Economist
    url: https://www.economist.com/the-world-this-week/rss.xml
    bucket: global_events
    tier: analytical
    auth: economist_session
    enabled: true

  # auth field references a Vault key under
  # secret/data/navi/{env}/feeds/{auth}
```

### Claude API Integration

The active Claude model MUST be configured in Vault at
`secret/data/navi/{env}/anthropic` under the key `model`. It MUST
NOT be hardcoded in application code. The model SHOULD be the current
Anthropic Sonnet-tier model and MUST be updated in Vault when a new
model generation is released.
The summarizer uses two distinct prompt patterns:

**Relevance scoring prompt** -- structured output, returns JSON:
```
System: You are an editorial assistant for a Director of Product
Management at Capital One. Score the following article for relevance
to their professional context on a scale of 0.0 to 1.0. Consider:
industry relevance (fintech, banking, technology), strategic signal
vs. operational noise, and analytical depth. Return only JSON:
{"score": float, "reason": string}

User: {article title and content}
```

**Summary generation prompt** -- returns prose:
```
System: You are an editorial assistant producing a daily intelligence
brief for a Director of Product Management at Capital One. Write a
3-4 sentence summary of the following article. Lead with the key
insight, not the event. End with a one-sentence note on why it is
relevant to someone in financial services product leadership. Cite
the source publication inline.

User: {article title and content}
```

**Thematic synthesis prompt** (weekly/monthly) -- returns prose:
```
System: You are a strategic analyst producing a weekly synthesis for
a Director of Product Management at Capital One. The following
articles were the most relevant stories of the past week. Identify
2-3 dominant themes, describe how they connect, and articulate what
a senior PM in financial services should take away. Write in
analytical prose, not bullet points.

User: {list of article titles and summaries}
```

### Error Handling and Resilience

- Collector failures on individual feeds are logged and skipped;
  a single feed error does not interrupt other feeds
- Claude API errors are retried with exponential backoff (3 attempts,
  base 2s). If all retries fail, the article is stored without a
  summary and flagged for manual review
- If no articles meet the relevance threshold for a digest window,
  the digest is generated with a note rather than skipped -- silence
  is worse than a thin digest
- All component errors are published to a `navi.{env}.errors` subject
  for observability

## Consequences

**Positive:**
- The collector/enricher/summarizer separation means each component
  is independently testable and replaceable.
- feeds.yaml hot reload means source configuration can be tuned
  without a redeployment.
- NATS decoupling means the enricher and summarizer can be scaled or
  replaced without modifying the collector.

**Negative / tradeoffs:**
- The enricher's current lack of true embedding-based deduplication
  means some near-duplicate articles may appear in early digests.
  URL deduplication handles the majority of cases.
- Claude API costs are incurred for every article scored and
  summarized. At typical RSS volumes (~200-400 articles/day across
  all feeds), with relevance scoring filtering to ~20-30 candidates
  before summarization, daily API cost is estimated at under $0.10.

**Neutral:**
- The feeds.yaml hot-reload pattern using SIGHUP is consistent with
  how Vault secrets are reloaded across all Navi services; no new
  operational pattern is introduced.
- The robfig/cron library is a lightweight, well-maintained dependency;
  it does not affect the architectural shape of the scheduler.

## Alternatives Considered

**Single monolithic collector-summarizer binary**
Rejected. Conflating collection, enrichment, and summarization in
one component makes each harder to test, modify, and reason about.
The NATS-based separation adds minimal complexity and significant
long-term maintainability.

**Polling on a fixed cadence for all sources**
Rejected. A tiered polling strategy dramatically reduces unnecessary
HTTP requests. Wire sources need 20-minute cadence; polling analytical
sources like Stratechery every 20 minutes is wasteful and potentially
rate-limiting.

**Using a third-party news API instead of RSS**
Rejected. Third-party news APIs (NewsAPI, etc.) add cost, introduce
an external dependency, and reduce control over source selection.
RSS feeds from authoritative sources are free, reliable, and give
direct access to the publications that matter.
