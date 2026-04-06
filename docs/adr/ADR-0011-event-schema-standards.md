# ADR-0011: Event Schema Standards

## Status
Accepted

## Date
2026-04-06

## Context

Navi's internal architecture is event-driven via NATS JetStream.
ADR-0002 establishes NATS as the event bus and defines the subject
topology. ADR-0005 defines the subjects used by the digest service.
Neither ADR defines the structure of the events themselves beyond
noting that CloudEvents v1.0 is the contract.

Without a defined schema standard, event producers and consumers
develop incompatible assumptions about envelope structure, payload
shape, and versioning. In an event-driven system this is particularly
costly because the producer and consumer are decoupled -- a schema
mismatch produces a silent failure rather than a compile error.

This ADR defines the complete CloudEvents envelope composition for
Navi, the schema definition and validation approach, the versioning
strategy, and the schema registry pattern.

## Decision

### CloudEvents Envelope Composition

All Navi events MUST conform to CloudEvents v1.0. The following
attributes MUST be present on every event:

**Required CloudEvents attributes:**

```
specversion    "1.0" (always)
id             UUID v4, unique per event
source         URI identifying the producing component:
               /navi/{env}/{service}/{component}
               e.g. /navi/prod/digest/collector
type           Reverse-DNS event type (see Event Type Registry below)
time           RFC3339 timestamp of event creation
datacontenttype "application/json" (always)
```

**Required Navi extension attributes:**

```
navienv        Environment: dev | staging | prod
navischema     Schema identifier and version:
               {event-type-slug}/{version}
               e.g. articles.collected/v1
traceparent    W3C Trace Context traceparent value (from active span)
tracestate     W3C Trace Context tracestate value (may be empty)
```

The `traceparent` and `tracestate` extension attributes enable trace
context propagation across NATS boundaries as defined in ADR-0008.

**Example envelope:**

```json
{
  "specversion":      "1.0",
  "id":               "550e8400-e29b-41d4-a716-446655440000",
  "source":           "/navi/prod/digest/collector",
  "type":             "navi.digest.article.collected",
  "time":             "2026-04-06T06:15:00Z",
  "datacontenttype":  "application/json",
  "navienv":          "prod",
  "navischema":       "articles.collected/v1",
  "traceparent":      "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
  "tracestate":       "",
  "data": {
    "article_id":  "7c9e6679-7425-40de-944b-e07fc1f90ae7",
    "feed_name":   "Reuters Technology",
    "url":         "https://...",
    "title":       "...",
    "collected_at": "2026-04-06T06:15:00Z"
  }
}
```

### Event Type Registry

Event types MUST follow a reverse-DNS naming convention rooted at
`navi.`. All event types MUST be registered in `docs/events/REGISTRY.md`
in the navi repository. An event type MUST NOT be used in code without
a corresponding registry entry.

**v1 Event Type Registry:**

```
navi.digest.article.collected
  Source:    digest/collector
  Subject:   navi.{env}.articles.collected
  Published: when a new article is fetched and passes URL dedup
  Consumed:  digest/enricher

navi.digest.article.enriched
  Source:    digest/enricher
  Subject:   navi.{env}.articles.enriched
  Published: when entity extraction and embedding are complete
  Consumed:  digest/store (persistence), digest/summarizer (on schedule)

navi.digest.ready
  Source:    digest/summarizer
  Subject:   navi.{env}.digest.ready
  Published: when a digest has been generated and stored
  Consumed:  digest/delivery

navi.sms.received
  Source:    delivery/webhook
  Subject:   navi.{env}.sms.inbound
  Published: when a validated, authorized inbound SMS is received
  Consumed:  intent/parser

navi.sms.send
  Source:    intent/handler, delivery/dispatcher
  Subject:   navi.{env}.sms.outbound
  Published: when an outbound SMS needs to be sent
  Consumed:  delivery/sms-channel

navi.error.reported
  Source:    any component
  Subject:   navi.{env}.errors
  Published: on any ERROR level condition
  Consumed:  monitoring subscriber (future)

navi.security.unauthorized_sms
  Source:    delivery/webhook
  Subject:   navi.{env}.security.events
  Published: when an SMS from an unauthorized number is received
  Consumed:  monitoring subscriber (future)
```

### Schema Definition

Each event type has a corresponding JSON Schema document defining
the shape of the `data` field. Schema files live at:

```
docs/events/schemas/{event-type-slug}/v{n}.json
```

For example:
```
docs/events/schemas/articles.collected/v1.json
docs/events/schemas/digest.ready/v1.json
```

Schema files are the authoritative definition of event payload shape.
Go types for event payloads are generated from these schemas using
`github.com/atombender/go-jsonschema`. Generated files live in
`internal/events/gen/` within each service.

### Schema Validation

Inbound events are validated against their declared schema (via the
`navischema` extension attribute) before processing. An event that
fails schema validation is:
1. Logged at ERROR level with the validation errors
2. Published to `navi.{env}.errors`
3. Discarded -- not retried, not dead-lettered

Outbound events are validated before publishing. A component that
attempts to publish an invalid event receives a validation error
at the publish call site. This is a programming error, not a
runtime condition -- it should be caught in tests.

Validation is performed using the `github.com/santhosh-tekuri/jsonschema`
library. The validator is initialized at service startup with all
known schemas loaded.

### Versioning Strategy

Event schemas are versioned independently. The version is carried in
the `navischema` extension attribute (`articles.collected/v1`).

**Non-breaking changes** (new optional fields, relaxed constraints)
do not require a version increment. The schema is updated in place
and remains backward compatible.

**Breaking changes** (removed fields, renamed fields, changed types,
new required fields) require a new schema version. The old version
remains in the schema registry. During a transition period, producers
publish both versions and consumers declare which version they consume.
The transition period ends when all consumers have migrated.

Given that Navi's event producers and consumers are all within the
same repository, breaking schema changes are coordinated in a single
PR that updates the schema, the producer, the consumer, and the
registry entry simultaneously. The dual-publish transition period
applies primarily when a future external system (ruby-mesh, for
example) subscribes to Navi events.

### NATS JetStream Configuration

All Navi subjects MUST be backed by JetStream streams for durability
and at-least-once delivery. Stream configuration:

```
Stream name:      NAVI_{ENV}
Subjects:         navi.{env}.>
Retention:        WorkQueuePolicy for processing subjects
                  LimitsPolicy for error and security subjects
Max age:          24 hours for processing subjects
                  7 days for error and security subjects
Storage:          File (persistent across restarts)
Replicas:         1 (single-node homelab)
```

Consumer configuration for each subscription:
- Durable consumer with a named consumer group
- AckExplicit acknowledgement -- messages are not removed until
  the consumer explicitly acks
- MaxDeliver: 3 (retry up to 3 times before sending to dead letter)
- AckWait: 30 seconds

Dead-lettered messages (exceeded MaxDeliver) are published to
`navi.{env}.deadletter` with the original message and a failure
reason.

## Consequences

**Positive:**
- The `navischema` attribute on every event means a consumer always
  knows what schema to validate against, regardless of which producer
  sent it.
- Schema-generated Go types eliminate payload shape mismatches between
  producers and consumers at compile time.
- The event type registry in `docs/events/REGISTRY.md` means every
  subject and its producers/consumers are documented in one place.
  Understanding the data flow of the system requires reading one file.
- JetStream at-least-once delivery with explicit ack means a consumer
  crash does not silently drop events.

**Negative / tradeoffs:**
- Schema validation on every inbound event adds processing overhead.
  For the volumes Navi handles (hundreds of events per day, not per
  second) this is negligible.
- The dual-publish transition pattern for breaking schema changes adds
  short-term complexity. Accepted as the cost of maintaining a clean
  contract.

**Neutral:**
- CloudEvents v1.0 is the established event envelope standard in the
  ruby-core infrastructure; adopting it here is a consistency
  decision, not a novel architectural choice.
- The `navischema` extension attribute follows the CloudEvents
  extension naming convention (lowercase, alphanumeric); it does not
  conflict with any standard CloudEvents attributes.

## Alternatives Considered

**Ad-hoc JSON payloads without schema validation**
Rejected. In an event-driven system, producer and consumer are
decoupled by design. Without schema validation, a payload shape
change in a producer produces a silent runtime failure in the consumer
rather than a compile error or a clear validation failure.

**Protobuf instead of JSON Schema**
Considered. Protobuf provides strong typing and efficient
serialization. Rejected because CloudEvents with JSON is the
established pattern in the existing ruby-core infrastructure, and the
tooling story for Protobuf in a mixed Go/future-service environment
is more complex than JSON Schema with code generation. Revisit if
event volume grows to the point where JSON serialization overhead
is measurable.

**A separate schema registry service (Confluent, AWS Glue)**
Rejected. A file-based schema registry in the repository is
sufficient for a single-team system. External schema registry
services add operational dependencies with no benefit at this scale.
