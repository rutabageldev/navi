# ADR-0008: Observability and Telemetry

## Status
Accepted

## Date
2026-04-06

## Context

Navi is a multi-component system: collector, enricher, summarizer,
scheduler, intent handler, and delivery dispatcher. These components
communicate across NATS subjects and call multiple external APIs.
Without structured observability, diagnosing failures, latency issues,
and unexpected behavior requires guesswork.

Observability in Navi has three pillars: traces (what happened and
how long it took), metrics (aggregate system health over time), and
logs (discrete events with context). All three must be correlated by
a common trace ID so a single digest delivery can be followed from
RSS fetch through Claude API call through Resend delivery without
reconstructing a timeline manually.

The Foundation infrastructure already includes a complete observability
stack: OTel Collector, Prometheus, Grafana, Tempo (distributed
tracing), Loki (log aggregation), and Promtail (log scraping). Navi
must integrate with this existing stack rather than deploy its own
observability infrastructure.

## Decision

### OpenTelemetry as the Instrumentation Standard

All Navi services MUST be instrumented using the OpenTelemetry Go SDK.
OTEL MUST be the instrumentation standard -- service-specific metrics
libraries MUST NOT be used, and direct Prometheus client
instrumentation MUST NOT appear in application code. The OTEL SDK
emits to the Foundation OTel Collector via OTLP, and the collector
handles export to Prometheus and Tempo.

A new OTel Collector MUST NOT be deployed for Navi. The existing
Foundation collector instance receives all Navi telemetry.

```
Navi service --> OTLP/gRPC --> Foundation OTel Collector
                                        |
                               +--------+--------+
                               v                 v
                          Prometheus           Tempo
                          (metrics)           (traces)
```

The Foundation OTel Collector address is stored in Vault at
`secret/data/navi/{env}/telemetry` and retrieved at service startup.

### Trace Propagation

Every operation that crosses a service or component boundary MUST
carry a trace context. This includes:

- Inbound HTTP requests (Twilio webhook, health endpoints)
- Outbound HTTP requests (Claude API, Resend, Twilio, RSS feeds)
- NATS message publish and subscribe

NATS does not natively support W3C Trace Context headers. Trace
context is propagated by embedding the traceparent and tracestate
values in the CloudEvents envelope under the `traceparent` and
`tracestate` extension attributes. Every NATS publisher MUST extract
the current span context and write it into the outgoing envelope.
Every NATS subscriber MUST extract the span context from the envelope
and start a child span before processing.

This ensures that a single article collected by the collector
produces a continuous trace through enrichment, summarization, and
delivery -- visible as a single trace in Tempo with child spans
per component.

### Span Naming Conventions

Spans are named using the pattern `{component}.{operation}`:

```
collector.feed_poll
collector.article_fetch
enricher.entity_extract
enricher.embedding_generate
summarizer.relevance_score
summarizer.article_summarize
summarizer.digest_generate
delivery.email_render
delivery.email_send
delivery.sms_send
intent.parse
intent.handle
store.query
store.insert
```

All spans include a standard set of attributes:

```
navi.environment        -- dev | staging | prod
navi.service            -- digest | intent | delivery
navi.component          -- collector | enricher | summarizer | etc.
navi.feed.name          -- (collector spans) feed name
navi.digest.type        -- (summarizer spans) daily | weekly | monthly
navi.intent.type        -- (intent spans) detected intent
```

External API calls use the standard OTEL semantic conventions for
HTTP client spans, augmented with:

```
navi.external.service   -- anthropic | resend | twilio | rss
navi.external.cost_usd  -- (Claude API spans) estimated cost of call
```

### Metrics

The following metrics are defined for v1. All metrics are prefixed
with `navi_` and scraped by the Foundation Prometheus instance.

**Collector:**
```
navi_articles_fetched_total         counter  {feed, bucket}
navi_articles_deduplicated_total    counter  {reason: url|semantic}
navi_feed_poll_duration_seconds     histogram {feed, tier}
navi_feed_poll_errors_total         counter  {feed, error_type}
```

**Summarizer:**
```
navi_digests_generated_total        counter  {type: daily|weekly|monthly}
navi_articles_scored_total          counter  {bucket, above_threshold}
navi_claude_api_calls_total         counter  {operation, model}
navi_claude_api_duration_seconds    histogram {operation}
navi_claude_api_cost_usd_total      counter  {operation}
navi_claude_api_errors_total        counter  {operation, error_type}
```

**Delivery:**
```
navi_digests_delivered_total        counter  {channel, type, status}
navi_delivery_duration_seconds      histogram {channel}
navi_delivery_errors_total          counter  {channel, error_type}
```

**Intent:**
```
navi_sms_received_total             counter  {authorized: true|false}
navi_intents_parsed_total           counter  {intent_type}
navi_intents_handled_total          counter  {intent_type, status}
navi_intent_parse_duration_seconds  histogram
```

**Store:**
```
navi_db_query_duration_seconds      histogram {operation, table}
navi_db_errors_total                counter  {operation, error_type}
```

The Foundation Prometheus scrape config is updated to include Navi's
metrics endpoint. This is a Foundation configuration change, not a
Navi deployment concern.

### Structured Logging and Loki Integration

All Navi services MUST use structured logging via the Go `slog`
package (standard library, Go 1.21+). All log output MUST be JSON to
stdout. Unstructured log.Printf statements MUST NOT appear anywhere
in application code.

The Foundation Promtail instance scrapes Docker container logs
automatically. Navi container logs are collected by Promtail and
shipped to Loki without any additional configuration in Navi itself
-- JSON stdout is sufficient. Logs are queryable in Grafana via
the Loki datasource from day one.

Every log entry includes the following base fields:

```json
{
  "time":        "2026-04-06T06:30:00Z",
  "level":       "INFO",
  "service":     "digest",
  "component":   "collector",
  "environment": "prod",
  "trace_id":    "4bf92f3577b34da6a3ce929d0e0e4736",
  "span_id":     "00f067aa0ba902b7",
  "msg":         "article fetched",
  "feed":        "Reuters Technology",
  "url":         "https://..."
}
```

The `trace_id` field enables direct correlation between a Loki log
line and the corresponding trace in Tempo via Grafana's trace-to-logs
and logs-to-traces linking.

Log levels are used consistently:

```
DEBUG   -- high-frequency operational detail, disabled in prod by default
INFO    -- normal operational events (article fetched, digest sent)
WARN    -- unexpected but recoverable conditions (feed timeout, retry)
ERROR   -- failures requiring attention (digest not delivered, API down)
```

Log level is configured per environment via `NAVI_LOG_LEVEL` with a
default of INFO in prod and DEBUG in dev.

### Health Endpoints

Every Navi service exposes two HTTP endpoints:

```
GET /v1/health/live    -- liveness: is the process running?
GET /v1/health/ready   -- readiness: is the service ready to handle work?
```

The liveness endpoint returns 200 always if the process is running.
The readiness endpoint checks Postgres, NATS, and Vault reachability.
These endpoints are registered in Foundation Uptime Kuma under the
Navi group.

### Error Tracking

All ERROR level log events are published to the `navi.{env}.errors`
NATS subject as a CloudEvents envelope in addition to being written
to stdout (and therefore collected by Promtail into Loki). The error
event enables future programmatic alerting without requiring a Loki
query.

## Consequences

**Positive:**
- No new observability infrastructure -- Navi integrates entirely
  with the existing Foundation OTel Collector, Prometheus, Tempo,
  Loki, and Promtail stack.
- Trace correlation with logs is available immediately via Grafana's
  Tempo-Loki linking, since both trace_id in logs and spans in Tempo
  share the same identifier.
- Promtail automatic Docker log scraping means Navi requires zero
  log shipping configuration -- structured JSON stdout is sufficient.
- navi_claude_api_cost_usd_total enables cost guardrail alerting
  defined in ADR-0009.

**Negative / tradeoffs:**
- Navi takes a runtime dependency on the Foundation OTel Collector.
  If the collector is unavailable, traces and metrics are lost for
  that window. Logs continue to flow via Promtail independently.
- The Foundation OTel Collector configuration must be updated to
  accept OTLP from Navi. This is a one-time Foundation change.

**Neutral:**
- The `slog` package is part of the Go standard library since 1.21;
  adopting it adds no external dependency to the project.
- Span naming using the `{component}.{operation}` convention is a
  documentation standard; it does not affect how spans are stored or
  queried in Tempo.

## Alternatives Considered

**Deploy a dedicated OTel Collector as a Navi sidecar**
Rejected. Foundation already runs an OTel Collector. A second
collector adds operational overhead with no benefit. Navi ships
telemetry to the existing Foundation collector.

**Jaeger for distributed tracing**
Rejected. Foundation runs Tempo. Adding Jaeger would duplicate the
tracing backend. Tempo is the correct target.

**Defer Loki integration**
Rejected. Foundation already runs Loki and Promtail. Structured JSON
stdout logs are collected automatically. There is nothing to defer.

**Direct Prometheus instrumentation without OTEL**
Rejected. Direct Prometheus client instrumentation creates tight
coupling to the metrics backend and provides no distributed tracing.
OTEL provides backend agnosticism and tracing in a single SDK.
