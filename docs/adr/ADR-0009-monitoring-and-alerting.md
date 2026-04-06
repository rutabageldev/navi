# ADR-0009: Monitoring and Alerting

## Status
Accepted

## Date
2026-04-06

## Context

Navi runs unattended. The daily digest is the most visible indicator
of system health -- if it doesn't arrive, something is wrong -- but
by the time a missing digest is noticed, the failure may be hours old.
Proactive monitoring is required to surface problems before they
manifest as a missing delivery.

The Foundation infrastructure includes a complete monitoring stack:
Grafana, Prometheus, Tempo (traces), Loki (logs), Promtail (log
scraping), and Uptime Kuma (uptime monitoring). Navi's monitoring
integrates with this existing stack entirely. No new monitoring
infrastructure is introduced.

The on-call model is simple: there is one user and one operator, and
they are the same person. Alerting must be actionable and low-noise --
a system that alerts frequently is a system that gets ignored.

## Decision

### Prometheus Integration

Navi metrics (defined in ADR-0008) are scraped by the Foundation
Prometheus instance. The Foundation Prometheus scrape configuration
is updated to include Navi's metrics endpoint.

```yaml
# Foundation prometheus.yml addition
scrape_configs:
  - job_name: navi
    static_configs:
      - targets: ['10.0.40.10:{NAVI_METRICS_PORT}']
    relabel_configs:
      - source_labels: [__address__]
        target_label: instance
        replacement: navi
```

### Tempo for Distributed Tracing

Traces from Navi are exported to the Foundation Tempo instance via
the Foundation OTel Collector (ADR-0008). Traces are queryable in
Grafana via the Tempo datasource. No new trace backend is deployed.

Tempo's trace-to-logs feature is configured to link spans to their
corresponding Loki log lines via the `trace_id` field, enabling
one-click navigation from a slow span in Tempo to the log context
around it in Loki.

### Loki for Log Aggregation

Foundation Promtail scrapes Navi container logs automatically.
Structured JSON logs from Navi stdout are queryable in Grafana
via the Loki datasource from day one. No Navi-side log shipping
configuration is required.

Useful Loki queries for Navi operations:

```logql
-- All errors across Navi services
{container=~"navi.*"} | json | level="ERROR"

-- Collector errors by feed
{container="navi-digest"} | json | component="collector" | level="ERROR"

-- Digest delivery events
{container="navi-digest"} | json | component="delivery" | msg="digest delivered"

-- All events for a specific trace
{container=~"navi.*"} | json | trace_id="4bf92f3577b34da6a3ce929d0e0e4736"
```

These queries are saved as Grafana dashboard panels (see below).

### Grafana Dashboards

A Navi dashboard is added to the Foundation Grafana instance. The
dashboard is defined as a JSON provisioning file committed to the
navi repository under `monitoring/grafana/dashboards/navi.json`.
It is loaded automatically by the existing Foundation Grafana
provisioning on startup.

The v1 dashboard contains the following panels, organized into rows:

**System Health row**
- Service uptime per container (from Uptime Kuma or Prometheus up metric)
- NATS connection status
- Postgres connection status
- Last digest delivered (stat panel -- turns red if > 25 hours)

**Collection row**
- Articles fetched per feed per day (bar chart, labeled by feed name)
- Feed error rate by feed (time series)
- Deduplication rate (gauge -- high rate signals feed issues)
- Feed poll latency by tier (heatmap)

**Digest Generation row**
- Digests generated per type per week (stat panels: daily/weekly/monthly)
- Digest delivery status last 30 days (bar chart: delivered vs. failed)
- Articles per digest by bucket (bar chart)
- Relevance score distribution (histogram)

**API Costs row**
- Claude API spend per day (time series with 30-day window)
- Claude API spend cumulative this month (stat panel with warning
  threshold at $10, critical at $15)
- API call volume by operation (bar chart)
- Claude API error rate (time series)

**Delivery row**
- Email delivery success rate (gauge)
- SMS delivery success rate (gauge)
- Delivery latency by channel (time series)

**Logs row**
- Navi error log stream (Loki panel: live tail of ERROR level events)
- Recent delivery events (Loki panel: digest delivered/failed events)

### Uptime Kuma

Navi health endpoints are added to the existing Foundation Uptime
Kuma instance under a new "Navi" group, consistent with the existing
Foundation and Ruby Core groups visible in the dashboard.

```
Navi / Digest Service (live)    GET /v1/health/live    60s interval
Navi / Digest Service (ready)   GET /v1/health/ready   60s interval
```

### Alert Conditions

Alerts are defined in Prometheus alert rules committed to the navi
repository at `monitoring/prometheus/alerts.yaml`. They are loaded
by the Foundation Prometheus instance via its existing rule file
provisioning.

Alerts are routed to the user via SMS (Twilio) using the Alertmanager
Webhook receiver. Alerts arrive from the Navi system number, prefixed
with "Hey, listen!" -- the same channel as normal Navi communications.

Every alert MUST be actionable. If the correct response is "ignore
it," the alert MUST NOT exist.

**Critical alerts -- immediate SMS at any hour:**

```yaml
- alert: DigestNotDelivered
  expr: time() - navi_last_digest_delivered_timestamp > 90000
  for: 5m
  annotations:
    summary: "Hey, listen! Daily digest has not been delivered in 25+ hours."
  # Fires if the morning digest fails entirely.

- alert: NaviServiceDown
  expr: up{job="navi"} == 0
  for: 2m
  annotations:
    summary: "Hey, listen! Navi digest service is unreachable."

- alert: DBConnectionFailed
  expr: increase(navi_db_errors_total{error_type="connection"}[5m]) > 0
  for: 1m
  annotations:
    summary: "Hey, listen! Navi cannot reach Postgres."

- alert: ClaudeAPIBudgetExceeded
  expr: navi_claude_api_cost_usd_total > 10
  for: 0m
  annotations:
    summary: "Hey, listen! Claude API spend has exceeded $10 this month."
  # Generous threshold for personal use. Catches runaway loops early.
```

**Warning alerts -- SMS during waking hours only (7am-10pm local):**

```yaml
- alert: FeedErrorRateHigh
  expr: rate(navi_feed_poll_errors_total[1h]) > 0.2
  for: 30m
  annotations:
    summary: "Hey, listen! More than 20% of feed polls are failing."
  # Fires on sustained multi-feed failures, not single timeouts.

- alert: DeliveryErrorRateHigh
  expr: increase(navi_delivery_errors_total[1h]) > 0
  for: 15m
  annotations:
    summary: "Hey, listen! Delivery errors detected on one or more channels."

- alert: ClaudeAPIErrorRateHigh
  expr: rate(navi_claude_api_errors_total[5m]) > 0.1
  for: 10m
  annotations:
    summary: "Hey, listen! Claude API error rate exceeds 10%."
```

**Not alerted -- visible in dashboard only:**
- Individual single-feed timeouts (isolated, not sustained)
- Deduplication rate spikes (expected behavior)
- Digest article count below rolling average (signal quality signal,
  not a system health failure)

### Cost Guardrails

Beyond the ClaudeAPIBudgetExceeded alert, a hard circuit breaker
is implemented in the summarizer (ADR-0005):

- If Claude API spend in the current calendar month exceeds the
  configured limit (default $15, at
  `secret/data/navi/{env}/limits/claude_monthly_usd`), the
  summarizer stops making API calls and generates a degraded digest
  containing only raw article headlines and sources -- no AI
  summaries.
- The circuit breaker resets on the 1st of each month.
- The limit is adjustable via Vault without a redeployment.

The degraded digest is prefixed with a note explaining that
summarization is paused for the month, so the absence of summaries
is not silent.

### Runbook

`docs/runbooks/` documents the expected response to each alert
condition and each operational task defined in ADR-0013. It is the
first place to look when something goes wrong. It MUST be updated
as a condition of closing any incident, however minor.

## Consequences

**Positive:**
- Full integration with the existing Foundation observability stack --
  Grafana, Prometheus, Tempo, Loki, and Uptime Kuma -- with no new
  infrastructure.
- Tempo-Loki trace correlation enables navigating from a slow span
  directly to the relevant log lines in a single Grafana click.
- Loki log querying is available from day one via Promtail's automatic
  Docker log scraping.
- SMS delivery of alerts via the Navi Twilio number means operational
  alerts arrive in the same thread as normal Navi output.
- The cost guardrail circuit breaker ensures a runaway bug degrades
  gracefully rather than generating an unexpected bill.
- Dashboard and alert rules committed to the navi repository mean
  monitoring configuration is versioned, reviewable, and portable.

**Negative / tradeoffs:**
- Alertmanager must be deployed in Foundation if not already present.
  This is a one-time setup cost.
- Navi alert rules are provisioned into Foundation Prometheus,
  creating a configuration dependency. A Foundation Prometheus
  restart picks up the new rules automatically via file provisioning.

**Neutral:**
- Alert rules committed to the Navi repository and provisioned into
  Foundation Prometheus is consistent with how Foundation manages its
  own alert rules; no new pattern is introduced.
- The Alertmanager Webhook receiver for SMS delivery is a standard,
  well-documented integration; the specific receiver configuration is
  a Foundation concern, not a Navi deployment concern.

## Alternatives Considered

**Deploy a dedicated Navi Grafana instance**
Rejected. Foundation Grafana already exists and supports dashboard
provisioning from multiple sources. A dedicated instance adds
operational overhead with no benefit.

**Jaeger for distributed tracing**
Rejected. Foundation runs Tempo. Jaeger would duplicate the tracing
backend. Tempo is the correct target.

**Defer Loki log integration**
Rejected. Foundation already runs Loki and Promtail. Structured JSON
stdout is collected automatically. There is nothing to defer and
significant value in having logs queryable from day one.

**Email-only alerts**
Rejected. Email is the digest delivery channel. Mixing operational
alerts into the digest inbox degrades both. SMS via the Navi Twilio
number is the right channel for urgent alerts.

**No alerting, rely on noticing a missing digest**
Rejected. A missing digest is a lagging indicator. Service health
and feed error rate alerts are leading indicators that surface
problems before a delivery fails.
