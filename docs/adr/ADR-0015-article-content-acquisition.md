# ADR-0015 — Article Content Acquisition

* **Status:** Accepted
* **Date:** 2026-04-12

---

## Context

The digest collector fetches articles from RSS feeds and must produce
`raw_content` — cleaned article text — for downstream enrichment and
summarization. This is non-trivial: RSS feeds vary widely in how much
content they provide, many require fetching the full article page to
get usable text, and pages themselves contain substantial boilerplate
(navigation, footers, related-content sidebars, advertising) that must
be stripped before the content reaches Claude.

Several acquisition decisions interact and must be decided together:
which library extracts readable text from HTML; how much content to
retain; how to avoid re-fetching unchanged feeds; how to handle feeds
that require authentication; what to do when fetching fails; and how
to prevent a single slow or broken feed from blocking the others.

ADR-0005 establishes the collector's responsibilities and the tiered
polling cadence but leaves the content acquisition mechanics
unspecified. This ADR defines them.

---

## Alternatives Considered

**RSS summaries only, no full-page fetch** — Many feeds include only
titles and short summaries in RSS items. Accepting this as the content
input means Claude operates on 1–3 sentences per article. Rejected
because relevance scoring and entity extraction quality degrades
significantly on summary-only input; the summarizer's output would
largely repeat the RSS summary rather than synthesise it.

**Browser automation (Playwright or chromedp) for JS-rendered pages** —
Some article pages require JavaScript execution to render their content.
A headless browser would handle these. Rejected because the resource
cost (memory, CPU) and operational complexity are disproportionate for
a single-node homelab. The publications in the planned feed inventory
(Reuters, AP, BBC, Ars Technica, etc.) all serve readable HTML without
JS rendering. Revisit if a specific future source requires it.

**Third-party content extraction API (Diffbot, Mercury Parser API)** —
Hosted services that return extracted article text given a URL. Rejected
because they add an external dependency and per-call cost for a function
that can be handled locally with equivalent quality using go-readability.

**Simple HTML-to-text without content isolation (k3a/html2text)** —
Converts all HTML to plain text, including navigation, footers, and
sidebar content. Rejected because it includes substantial boilerplate in
`raw_content`, increasing token usage in Claude calls and reducing the
signal-to-noise ratio for enrichment and scoring.

**Feed credentials in feeds.yaml or environment variables** — Storing
session cookies or API tokens in the committed feed configuration file
violates the no-secrets-in-repo rule (ADR-0002). Per-feed environment
variables require a redeployment to add a new authenticated source.
Rejected in favour of the Vault-backed pattern defined below.

---

## Decision

### 1. HTML extraction library

The collector MUST use `github.com/go-shiori/go-readability` for
extracting readable article text from fetched HTML. go-readability is
a port of Mozilla's Readability algorithm — the same algorithm used by
Firefox Reader View — and is purpose-built for isolating article body
content from navigation, advertising, and boilerplate.

The collector MUST use go-readability's extracted `TextContent` field
(plain text) as `raw_content`, not `Content` (HTML). Downstream
components (enricher, summarizer) work with plain text; storing HTML
in `raw_content` would add tags to Claude's context without value.

### 2. Content length limit

`raw_content` MUST be trimmed to a maximum of **32,000 characters**
before storage and before being included in any NATS event payload.
This corresponds to approximately 8,000 tokens in Claude's context
window, which is sufficient for enrichment and summarization quality
while bounding token usage and storage growth.

Trimming MUST be done on character boundaries, not mid-word. If the
extracted content exceeds the limit, it is truncated at the last
whitespace boundary before the 32,000-character mark. No truncation
marker is appended — downstream components treat `raw_content` as
potentially incomplete.

### 3. Full-page fetch policy

Not all RSS feeds provide full article content in their feed items.
The collector MUST attempt a full-page HTTP fetch for every article URL
regardless of whether the RSS item contains a summary, because RSS
summary completeness is inconsistent and not machine-detectable without
fetching the page.

Exception: if go-readability returns empty `TextContent` after a
successful page fetch (JavaScript-rendered content, paywall, etc.),
the collector falls back to the RSS item's description field if
non-empty. If both are empty, `raw_content` is stored as an empty
string (see §10 graceful degradation).

### 4. Recency filter

The collector MUST skip any RSS item whose `pubDate` is older than
**48 hours** at the time of collection. This prevents resurfacing
stale articles if a feed is restructured or re-indexed, and bounds
the article volume that enters the pipeline after a collector outage.

If `pubDate` is absent or cannot be parsed, the item MUST be collected
anyway. Omitting `pubDate` is common in some feeds and is not a signal
of staleness.

The 48-hour window is measured against the collector's wall clock at
poll time, not against the NATS event timestamp.

### 5. Conditional HTTP requests for RSS feeds

To avoid re-downloading unchanged RSS feed XML on every poll, the
collector MUST implement conditional HTTP for the feed poll request
(not for individual article fetches):

- On the first fetch of a feed, store the response `ETag` and
  `Last-Modified` headers in the `feeds` table.
- On subsequent fetches, send `If-None-Match` (ETag) and
  `If-Modified-Since` headers. A 304 Not Modified response is treated
  as a successful poll with zero new items; no articles are processed.
- If neither ETag nor Last-Modified was returned on the prior fetch,
  fetch unconditionally.

The `feeds` table MUST be extended in migration 0002 (Block 4) with:

```sql
ALTER TABLE feeds ADD COLUMN etag           TEXT;
ALTER TABLE feeds ADD COLUMN last_modified  TEXT;
ALTER TABLE feeds ADD COLUMN last_polled_at TIMESTAMPTZ;
ALTER TABLE feeds ADD COLUMN last_error_at  TIMESTAMPTZ;
```

### 6. Per-feed timeouts

- **RSS feed poll:** 10 seconds. Applies to the HTTP GET for the feed
  XML, including the conditional request.
- **Article page fetch:** 15 seconds. Applies to the HTTP GET for the
  full article page.

Both timeouts are per-request, not per-feed-cycle. A feed that
consistently times out is logged at WARN and skipped for that poll
cycle; it is retried on the next scheduled poll.

### 7. User-Agent policy

All outbound HTTP requests MUST use the User-Agent string:

```
navi-collector/1.0 (+https://github.com/rutabageldev/navi)
```

Navi MUST NOT spoof a browser User-Agent. Some feeds may block a
non-browser User-Agent; this is accepted. Per-feed User-Agent override
is out of scope for v1.

### 8. Authenticated source pattern

Feeds that require authentication (paid publications, session-gated
content) MUST be configured in `feeds.yaml` with an `auth` field whose
value names a Vault path suffix:

```yaml
- name: The Economist
  url: https://www.economist.com/the-world-this-week/rss.xml
  bucket: global_events
  tier: analytical
  auth: economist_session
  enabled: false   # disabled until credential is provisioned in Vault
```

The collector resolves the credential at startup and on SIGHUP by
reading from Vault at:

```
secret/data/navi/{env}/feeds/{auth}
```

The Vault secret MUST contain two keys:

```
header   The HTTP header name (e.g. "Cookie" or "Authorization")
value    The full header value (e.g. "sessionid=abc123" or "Bearer tok")
```

The resolved header is attached to article page fetch requests for
that feed. It is NOT sent with RSS feed poll requests unless the feed
URL itself is gated (rare; handle on a case-by-case basis).

Feeds with `auth` set but no corresponding Vault secret cause the
collector to log an error at startup and disable that feed for the
session. The service MUST NOT fail to start due to a missing feed
credential.

Paid sources (The Economist, Stratechery, The Information) MUST be
added to `feeds.yaml` with `enabled: false` and a comment referencing
the Full Source Coverage roadmap item. They are not collected until
the credential is in Vault and `enabled` is set to `true`.

### 9. Feed error isolation

A failure in one feed MUST NOT prevent other feeds from being polled.
The collector runs each feed's poll in a separate goroutine. Errors
(timeout, HTTP error, parse failure, go-readability failure) are:

- Logged at WARN level with `feed_name` and `error` fields
- Written to `feeds.last_error_at` in Postgres
- Recorded as a metric increment on `navi_collector_feed_errors_total`

The feed is retried on its next scheduled poll. There is no
exponential backoff at the feed level — the poll cadence defined in
ADR-0005 (wire: 20min, daily: 2hr, analytical: 24hr) is maintained
regardless of prior errors.

### 10. Graceful degradation on content failure

If content extraction fails for any reason (timeout, HTTP error,
go-readability returns empty, paywall), the collector MUST:

1. Store the article with `raw_content = ''` (empty string, not NULL)
2. Publish the `articles.collected` CloudEvents envelope with
   `raw_content` as an empty string
3. Log at WARN with `feed_name`, `url`, and `error`

The enricher handles empty `raw_content` by storing the article with
no entity links and setting `degraded = true` in the published
`articles.enriched` event. The article remains eligible for relevance
scoring (scored on title only when `raw_content` is empty) and may
still appear in a digest.

Silence — not publishing the event because content was unavailable —
is never the correct behaviour. A title-only article is better than
a gap in the pipeline.

---

## Consequences

### Positive

- go-readability handles the hard cases of real-world article HTML
  without requiring custom extraction rules per publication. Adding
  new feeds requires no extraction code changes.
- The 32,000-character content limit produces predictable token usage
  in enrichment and scoring calls, making the cost estimates in
  ADR-0014 reliable.
- Conditional HTTP for RSS feed polls means that a wire feed polled
  72 times per day typically results in 70+ no-cost 304 responses
  with only changed feeds incurring full bandwidth.
- Feed error isolation means a single broken or rate-limiting source
  has no impact on the rest of the collection pipeline.
- The `auth` pattern in `feeds.yaml` + Vault enables authenticated
  sources to be added without code changes — only a Vault write,
  a `feeds.yaml` update, and a SIGHUP.

### Negative

- go-readability will fail to extract content from pages that require
  JavaScript rendering. This is an accepted limitation for v1; the
  planned free feed inventory does not include JS-rendered sources.
- The 48-hour recency filter means articles from a feed that was
  unreachable for more than 48 hours are permanently missed. This is
  acceptable for a daily news digest focused on current events; it is
  not a system for archival collection.
- Fetching the full article page for every new item generates
  significantly more outbound HTTP traffic than RSS-summary-only
  collection. At ~270 articles/day, this is approximately 270 page
  fetches per day — negligible on a homelab network but worth noting
  if a publication rate-limits by IP.
- The no-backoff feed retry policy means a feed undergoing an extended
  outage generates a WARN log entry on every poll cycle until it
  recovers. This is informational noise but is preferable to silently
  skipping the feed.

### Neutral

- The `feeds` table extension (etag, last_modified, last_polled_at,
  last_error_at) is an additive migration, consistent with ADR-0004's
  backward-compatibility constraint. It lands in migration 0002
  alongside the full v1 schema (Block 4).
- Authenticated sources are shipped as `enabled: false` stubs in
  `feeds.yaml`. They are inert until credentials are provisioned in
  Vault and `enabled` is flipped — no runtime impact until then.
- Title near-duplicate detection (Levenshtein similarity) is an
  enricher concern, not a collection concern. The threshold and
  implementation are defined in Block 7 (enricher). The collector
  performs URL-based deduplication only.
