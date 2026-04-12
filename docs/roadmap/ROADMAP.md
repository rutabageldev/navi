# Navi Roadmap

> Feature scope and principles are defined in STRATEGY.md.
> This document adds outcome-based sequencing, dependencies, and
> current status. Priorities reflect the current moment -- they
> shift as features ship. There is always a top priority; there
> are no permanent priority numbers.

Last updated: 2026-04-10

---

## Now

Features in Now are actively being built or immediately next. They
are ordered by current priority -- top item is the current focus.
Each has a detailed plan in docs/plans/.

---

### Daily Digest Delivery Over Email

**Status:** Not Started
**Depends on:** Pipeline and observability foundation (complete)

**Goal**
The user receives a structured daily email at 6:30am containing
5-7 summarized tech and banking stories and 4-5 global news stories,
each with citations and links to the original source. This is the
core value proposition of Navi -- the first morning where the digest
arrives in the inbox is the first morning Navi is real.

**Scope**
- RSS collector with tiered polling cadence (wire, daily, analytical)
- URL-based article deduplication
- Claude API relevance scoring and per-article summarization
- Resend email delivery with structured HTML template
- feeds.yaml populated with the full source list from STRATEGY.md
  (excluding authenticated sources -- those ship with The Economist
  feature)
- Full Postgres schema from ADR-0004: articles, digests, feeds,
  topics, digest_articles, feedback_signals
- NATS event pipeline: articles.collected -> articles.enriched
  -> digest.ready -> delivery
- Claude API monthly cost circuit breaker active before first
  deployment
- Grafana dashboard updated with collection and delivery metrics

**Exit Criteria**
- Daily digest delivered to the user's inbox by 6:30am on the first
  morning after deployment
- Digest contains 5-7 tech/banking stories and 4-5 global stories
- Each story has a 3-4 sentence summary and source citation with
  a working link
- Claude API cost per digest is logged and visible in Grafana
- Feed errors do not prevent digest generation -- digest generates
  from available feeds with a note if sources are missing
- User reads the digest consistently for 5 consecutive days and
  judges it worth the 5 minutes

**Key Constraints**
- No authenticated feed fetching -- Economist and paid sources
  deferred to the Full Source Coverage feature
- Relevance scoring prompt must encode user professional context:
  Director of PM, Capital One, financial services
- Rolodex and professional profile data MUST NOT be passed to
  Claude API (neither exists yet, but the pattern must be established)
- Article raw content nullified after 30 days per ADR-0013

**ADRs:** ADR-0004, ADR-0005, ADR-0006, ADR-0008, ADR-0009, ADR-0011

---

### Weekly and Monthly Intelligence Briefs

**Status:** Not Started
**Depends on:** Daily digest delivery over email

**Goal**
The user receives a Friday evening brief that synthesizes the week's
dominant themes rather than listing individual stories, and a 1st-of-
month brief that surfaces the strategic signal from the prior month.
Both feel editorially distinct from the daily digest -- analysis,
not headlines.

**Scope**
- Weekly digest generation: thematic synthesis across the week's
  collected articles via Claude API synthesis prompt
- Monthly digest generation: strategic narrative arc from the month
- Separate email templates for weekly and monthly formats, visually
  distinct from the daily digest
- Scheduler entries: Friday 4:00pm for weekly, 1st of month 6:00am
  for monthly

**Exit Criteria**
- Weekly brief delivered every Friday evening
- Monthly brief delivered on the 1st of each month
- Both formats feel analytically distinct from the daily digest --
  synthesis and themes, not more summaries
- User reads both consistently for 4 weeks without disabling them

**ADRs:** ADR-0005, ADR-0006

---

### Full Source Coverage Including The Economist

**Status:** Not Started
**Depends on:** Daily digest delivery over email

**Goal**
The Economist -- the user's existing paid subscription and primary
analytical reading habit -- appears in the daily digest alongside
the free sources. All paid and authenticated sources are wired.

**Scope**
- Session cookie management in the collector for authenticated feeds
- Vault path for feed credentials:
  secret/data/navi/{env}/feeds/{publication}
- Cookie rotation handling and session expiry detection
- Graceful degradation: if authentication fails, skip the feed and
  log a warning -- do not fail the digest
- Stratechery and The Information added if subscriptions are active

**Exit Criteria**
- Economist stories appear in the global events section of the
  daily digest
- Session expiry detected, logged, and surfaced as a warning alert
- Digest generates successfully even when authenticated feeds fail
- No paid subscription content is stored beyond the standard 30-day
  raw content retention window

**ADRs:** ADR-0005, ADR-0013

---

### Text Navi to Take Action

**Status:** Not Started
**Depends on:** Pipeline and observability foundation (complete)

**Goal**
The user can send a natural language text message to Navi's dedicated
phone number from their native iOS Messages app and have Navi take
a real action -- scheduling a meeting, adding a contact, making a
note. "Hey Navi, Katie and I are seeing our interior designer next
Tuesday" results in the event appearing on the calendar. The native
Messages surface means zero new apps or habits required.

**Scope**
- Twilio long code provisioning and Vault credential seeding
- Inbound webhook at POST /v1/webhooks/twilio/inbound
- Twilio request signature validation (first middleware, per ADR-0007)
- Sender allowlist enforcement (per ADR-0007)
- Claude API intent parsing: natural language -> structured action
- v1 supported intents:
  - calendar_create: create a Google Calendar event
  - rolodex_add: add a person to the Rolodex (stub -- Rolodex
    feature ships later, but the intent must be handled gracefully)
  - unknown: Navi replies asking for clarification
- Outbound SMS confirmation before any action is taken
- Google Calendar write integration for calendar_create intent
- Silent drop with internal logging for unauthorized senders

**Exit Criteria**
- User texts "put X on my calendar for Tuesday" and the event
  appears in Google Calendar within 30 seconds
- Navi sends a confirmation SMS before acting: "Got it -- adding
  X to your calendar for Tuesday. Reply YES to confirm."
- An unauthorized number texting the Twilio number receives no
  response and the attempt is logged
- Twilio signature validation rejects a spoofed webhook request
  (verified in smoke tests)
- rolodex_add intent returns a graceful "I'll remember that for
  when the Rolodex is ready" response rather than an error
- Intent parse latency (text received to confirmation SMS sent)
  is under 10 seconds for simple intents

**Key Constraints**
- Navi MUST NOT take any action without an explicit confirmation
  from the user -- every intent requires a reply before execution
- All SMS security controls from ADR-0007 MUST be active before
  the webhook is publicly reachable
- The Twilio webhook is the only Navi endpoint that accepts inbound
  connections from the public internet

**ADR needed:** Google Calendar write integration requires an ADR
before planning begins. STRATEGY.md lists Anthropic, Resend, and
Twilio as the external API set; Calendar is not currently covered.
Decisions needed: service account vs. OAuth, credential lifecycle
in Vault, what data leaves the Foundation network.

**ADRs:** ADR-0006, ADR-0007, ADR-0010

---

### User Can Store and Look Up Contact Data

**Status:** Not Started
**Depends on:** Text Navi to take action

**Goal**
The user has a Rolodex -- a personal contact store that lives in
Navi's infrastructure and is visible to all Navi features. Contacts
can be added, updated, and retrieved via SMS. Navi can see the
Rolodex when generating digests and briefs, but contact data is
never passed to external APIs as input.

**Scope**
- people, companies, and person_companies tables activated
  (defined in ADR-0004, unpopulated until this feature)
- SMS intents fully wired:
  - rolodex_add: "Add Sarah Chen, Head of Product at Plaid"
  - rolodex_update: "Sarah Chen moved to Stripe as CPO"
  - rolodex_lookup: "What do I have on Sarah Chen?"
- Navi parses and stores: name, role, company, email, phone,
  notes, relationship context
- Lookup response delivered via SMS with key contact details
- Personal milestone fields: birthday, work anniversary
- Vault constraint: Rolodex data MUST NOT leave the Foundation
  Postgres instance

**Exit Criteria**
- User adds 10 contacts via SMS without errors
- Lookup returns accurate, readable contact summary via SMS
- Updating a contact's role reflects correctly on next lookup
- Personal milestone stored and confirmed via SMS
- Rolodex data confirmed absent from any Claude API call payload
  in logs

**Key Constraints**
- No automatic ingestion from calendar or email -- every Rolodex
  entry is explicitly added by the user
- Rolodex data is never used as input to external APIs

**ADRs:** ADR-0004, ADR-0006, ADR-0007

---

## Next

Features in Next are well-understood enough to be sequenced when
Now clears. They are not yet prioritized relative to each other --
that happens when the top of Now ships. Dependencies are called out
where they would constrain ordering.

---

### Navi Surfaces News About Your Contacts' Companies

**Goal**
When a contact's company appears in the news, Navi makes the
connection -- surfacing the story in the digest with a note that
it involves someone in the Rolodex, or sending a proactive SMS
alert for significant news.

**Depends on:** User can store and look up contact data,
Daily digest delivery over email

**Key open questions:** Confidence threshold for entity matching;
alert vs. digest-inline for significant news.

---

### Meeting Prep Briefs

**Goal**
The evening before a calendar event involving a known contact,
Navi delivers a prep brief by email: who you're meeting, what
their company has been up to, relevant recent news, and any
notes from your last interaction.

**Depends on:** User can store and look up contact data,
calendar read integration (Google Calendar)

---

### Relationship Health Nudges

**Goal**
When an important contact has gone quiet -- no interaction logged
in 60+ days -- Navi sends a gentle SMS nudge: "You haven't been
in touch with Omar in a while. Worth a message?"

**Depends on:** User can store and look up contact data

---

### Career Context and Self-Knowledge

**Goal**
The user has a structured professional profile in Navi covering
four layers: identity (title, company, domain), portfolio (work
done and outcomes), goals (where they're headed), and positioning
(the narrative they'd tell in an interview). Maintained via SMS.
Informs Professional Pulse, Deliverable Prep, and the Learning
Queue when those features ship.

**Depends on:** Text Navi to take action

---

### Daily Task Nudges in the Brief

**Goal**
Relevant tasks due today are surfaced in the daily digest as
contextual nudges. Navi reads from an external todo system and
surfaces -- it does not own or write to the task list.

**Depends on:** Daily digest delivery over email

**Blocked on:** Todo system integration target not yet confirmed.
Must be decided before a plan can be written for this feature.

---

### Unified Morning Brief

**Goal**
A single "Hey, listen!" daily delivery that combines the news
digest, calendar context for the day, Ada overnight summary from
ruby-core, and any time-sensitive nudges. The digest becomes a
section, not the whole thing.

**Depends on:** Daily digest, Text Navi to take action,
ruby-core event stream integration, calendar read integration

---

### Competitive Company Tracking

**Goal**
Named competitors and key companies have a maintained narrative
arc in Navi -- not just "here's today's news about Chase" but
"here's what Chase has been doing over the last 90 days."

**Depends on:** User can store and look up contact data (companies
as first-class entities), Daily digest delivery over email

---

## Later

Features in Later are directionally understood but not yet
specified. They will be fleshed out when promoted to Next.
No plans are written for Later items.

- **Research and on-demand intelligence** -- "Brief me on BNPL
  before Thursday." Conversational SMS interface for deep-dive
  research assembled from stored knowledge and live sources.
  Depends on: full SMS intent pipeline, Rolodex, career context.

- **Audio digest for the commute** -- TTS rendering of the daily
  brief for the drive to work. The delivery abstraction is designed
  to support this from day one. Depends on: stable daily digest
  format running for several months.

- **Learning queue** -- Books, papers, talks routed toward the
  user's developmental goals. Depends on: career context and
  self-knowledge.

- **Professional pulse and career gap analysis** -- What Director-
  level PM roles at peer companies are emphasizing that isn't in
  your current portfolio. Depends on: career context and
  self-knowledge.

- **Deliverable preparation** -- QBR, performance review, and
  annual planning prep briefs. Depends on: career context,
  calendar integration.

- **Katie daily brief** -- A tailored daily brief for Katie with
  a legal domain focus and different source set. Depends on:
  stable P1 delivery pipeline running reliably for the primary
  user.
