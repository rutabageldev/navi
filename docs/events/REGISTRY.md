# Navi Event Type Registry

All Navi event types are registered here. An event type MUST have an entry in this file
and a schema file under `docs/events/schemas/` before it is used in any producer or
consumer. See ADR-0011 for envelope requirements and ADR-0002 for subject naming convention.

Subject format: `navi.{env}.{class}.{type}[.{action}]`
Audit subject format: `audit.navi.{type}` (shared namespace, outside env scope)

---

## v1 Event Types

### navi.digest.article.collected

```
Class:     events
Source:    digest/collector
Subject:   navi.{env}.events.articles.collected
Schema:    docs/events/schemas/articles.collected/v1.json
navischema: articles.collected/v1

Published: when a new article is fetched from an RSS feed and passes URL deduplication
           against the articles table (i.e. the URL has not been seen before)
Consumed:  digest/enricher

Notes:     raw_content may be empty if content extraction failed; enricher must handle
           the empty-content case gracefully (store title+URL only, skip enrichment).
           Collector MUST NOT publish if the URL already exists in the articles table.
```

---

### navi.digest.article.enriched

```
Class:     events
Source:    digest/enricher
Subject:   navi.{env}.events.articles.enriched
Schema:    docs/events/schemas/articles.enriched/v1.json
navischema: articles.enriched/v1

Published: when entity extraction is complete and the enriched article has been written
           to Postgres (article_topics and article_entities rows present)
Consumed:  digest/store (persistence is done before publish; consumer is future)

Notes:     degraded=true indicates enrichment was skipped due to ErrBudgetExceeded or
           a Claude API failure. A degraded article is stored with empty entity links
           and is still eligible for relevance scoring (scored on title only when degraded).
```

---

### navi.digest.digest.ready

```
Class:     events
Source:    digest/summarizer
Subject:   navi.{env}.events.digest.ready
Schema:    docs/events/schemas/digest.ready/v1.json
navischema: digest.ready/v1

Published: when a digest record has been written to Postgres and is ready for delivery.
           Delivery dispatcher subscribes and routes the digest to all active channels.
Consumed:  digest/delivery (dispatcher)

Notes:     degraded=true indicates the digest was generated without Claude summaries
           (budget exceeded or API unavailable); content_html contains raw headlines
           and source URLs with a degraded-mode header. Delivery proceeds normally —
           a degraded digest is delivered; silence is not acceptable (ADR-0005).
           digest_type is one of: daily, weekly, monthly.
```

---

## Future Event Types (not yet implemented)

The following types are registered here to reserve their subjects and establish
publisher/consumer intent before implementation begins. Schema files are not yet created.

### navi.sms.received

```
Class:     events
Source:    delivery/webhook
Subject:   navi.{env}.events.sms.received
Published: when a validated, authorized inbound SMS is received from Twilio
Consumed:  intent/parser
```

### navi.sms.send

```
Class:     commands
Source:    intent/handler, delivery/dispatcher
Subject:   navi.{env}.commands.sms.send
Published: when an outbound SMS directive is issued (not a state change — a command)
Consumed:  delivery/sms-channel
```

### navi.error.reported

```
Class:     errors
Source:    any component
Subject:   navi.{env}.errors.reported
Published: on any ERROR-level condition from any component
Consumed:  monitoring subscriber (future)
```

### navi.security.unauthorized_sms

```
Class:     audit (shared namespace)
Source:    delivery/webhook
Subject:   audit.navi.sms_unauthorized
Published: when an SMS from an unauthorized number is received
Consumed:  audit-sink (future)
Notes:     Published outside the env namespace to the shared audit.> subject,
           captured by the ruby-core AUDIT_EVENTS JetStream stream.
```
