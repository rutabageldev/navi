# Navi -- Product Strategy

"Navi is my digital chief of staff -- always watching, always learning,
always ready with what I need to be my best before I know I need it."

---

## Mission

Navi reduces the cognitive overhead of a demanding professional life by
continuously gathering, enriching, and surfacing intelligence across the
domains that matter most: industry and world awareness, professional
relationships, and personal context. It works in the background so that
when a moment requires clarity, preparation, or perspective, the answer
is already there.

---

## Principles

**1. Anticipate, don't react.**
Navi's value is in surfacing the right thing before it's asked for. A
digest that arrives at 6:30am before the workday starts is more valuable
than one that requires a query. Meeting prep that appears the night before
is more valuable than a search on the way to the room.

**2. Your data never leaves your infrastructure.**
All personal context -- relationships, notes, interaction history,
synthesized intelligence -- lives on your Postgres instance. Navi may
call external APIs (Claude, Resend, Twilio, RSS feeds) but it never
ships personal data to them beyond what is necessary to generate a
response. Rolodex data and professional profile data in particular are
never used as input to external services.

**3. Signal over volume.**
Navi is not a firehose. Every feature must earn its place by reducing
noise, not adding to it. A digest with 5 well-chosen stories is better
than 20. A relationship nudge that fires rarely is trusted; one that
fires constantly is ignored.

**4. Reduce friction, never add it.**
Navi delivers to where you already are -- your inbox and your Messages
app. It does not require you to open a new app, learn a new interface,
or change your habits. Features that require active management are a
smell.

**5. Extend your judgment, don't replace it.**
Navi surfaces, summarizes, and connects. It does not decide. Meeting prep
is context, not a script. Relationship nudges are prompts, not
instructions. The goal is a better-informed you, not an automated you.

**6. Act on direction, never autonomously.**
Navi takes actions only when explicitly asked to. It does not make
decisions or initiate actions on its own. "Put this on my calendar" is
a delegation. Navi autonomously emailing someone on your behalf is not.
The line is whether you consciously initiated the action. This boundary
will be deliberately expanded over time -- but each expansion is a
documented decision, not a default.

---

## Users

**Primary: Mike**
Every feature is designed around a single Director-level PM at a major
financial institution who listens to the Economist on his commute, has
a demanding calendar, maintains a professional network, and wants to be
more intentionally informed without spending more time on information
consumption.

**Secondary: Katie**
Some features -- particularly those touching home, family, and shared
context -- may surface value for Katie. These are opt-in extensions of
features that already exist for Mike, not features designed around a
separate user model. A tailored daily brief for Katie is a natural
future extension. Her experience is always considered but never the
primary design driver.

---

## Feature Horizon

### Now -- Daily Intelligence (v1)

The foundational feature. A structured daily digest delivered by email
each morning covering:

- **Tech & Fintech/Banking** -- 5-7 stories, summarized with citations,
  sourced from Ars Technica, MIT Technology Review, Wired, TechCrunch,
  The Verge, Axios Tech, American Banker, Finextra, The Financial Brand,
  PYMNTS, Reuters Technology, and optionally The Information and
  Stratechery (paid)
- **U.S. & Global Current Events** -- 4-5 stories, sourced from Reuters
  World, AP News, BBC News, The Guardian, NPR News, Foreign Policy,
  ProPublica, and The Economist (existing paid subscription,
  authenticated fetch)
- **Todo nudges** -- relevant tasks surfaced from an external todo system
  (read-only integration); e.g. "Text Omar about light switches today"
- **Weekly trend analysis** -- Friday delivery synthesizing the week's
  themes, not just individual stories
- **Monthly synthesis** -- 1st of each month, strategic signal and
  narrative arc from the prior month

Delivery via Resend (email). Summarization via Claude API. Sources
configured in feeds.yaml -- add or remove without code changes.

### Next -- Relationship Intelligence

Rolodex as a living knowledge graph. People and companies as first-class
entities linked to the topics and news Navi already tracks.

- Meeting prep briefs surfaced the night before
- Relationship health nudges when important contacts have gone quiet
- Personal milestone tracking (birthdays, work anniversaries, life
  events) -- what makes the Rolodex feel human rather than a CRM
- Interaction memory: notes from conversations, commitments made,
  follow-ups owed
- Entity-linked news: contacts automatically surfaced when Navi sees
  material news about their company

### Next -- Professional Profile

A structured, living representation of who you are professionally --
maintained over time so Navi can reason about your career with real
context rather than generic assumptions. Composed of four layers:

- **Identity** -- stable facts: title, domain, industry, company,
  tenure. The baseline context already informally baked into Navi's
  prompts, made explicit and maintainable.
- **Portfolio** -- the work you have actually done. Products owned,
  projects delivered, scope, scale, and outcomes. A living record
  updated continuously rather than reconstructed every two years when
  a resume is needed.
- **Goals** -- where you are trying to go. Developmental targets, skills
  being built, the kind of leader you want to become. What separates a
  professional profile from a resume -- it has direction, not just
  history.
- **Positioning** -- the narrative you would tell in an interview or
  performance review. What makes you distinct. Maintained here so it
  is ready when you need it, not drafted under pressure.

Maintenance is designed to be low-friction: stable layers set via
structured input (YAML or simple form), dynamic updates via SMS
("Navi, add to my portfolio: shipped X in Q1, drove Y outcome"), and
proactive prompts from Navi when the profile hasn't been updated in a
while. The profile lives entirely in Postgres and is never passed to
external services.

The professional profile directly enriches several other Next and Later
features: Professional Pulse becomes a genuine gap analysis rather than
a generic market summary; Deliverable Preparation can frame narratives
around real outcomes; the Learning Queue routes toward actual
developmental goals; and research briefs are framed around your specific
domain experience.

### Next -- Unified Morning Brief

A single "Hey, listen!" daily delivery combining:

- News digest (as a section, not the whole thing)
- Calendar context for the day ahead
- Ada overnight summary pulled from ruby-core
- Todo nudges
- Any time-sensitive relationship or competitive alerts

### Next -- Communication Triage

A morning surface of what needs attention across personal email -- not a
full inbox, but a prioritized flag of what requires a response or
decision today. Read access only; Navi surfaces, you act.

### Next -- Deliverable Preparation

Longer-horizon prep for recurring professional moments -- quarterly
reviews, performance cycles, annual planning. Distinct from meeting prep
in that it operates on a weeks-out horizon and requires Navi to
understand your professional calendar at a higher level. Informed by
the professional profile so narratives are grounded in real outcomes.

### Next -- Competitive Intelligence

Structured tracking of named competitors and key companies -- not just
"here's news about Chase" but a maintained narrative arc of what they've
been doing over the last 90 days. Companies as first-class entities with
a tracked story over time.

### Later -- Research & On-Demand Intelligence

Conversational interface via SMS: "Prepare me a brief on BNPL
competitive dynamics before Thursday." Navi assembles from stored
knowledge and live sources, frames the brief around your professional
context, and delivers it by email.

### Later -- Audio Digest

TTS rendering of the daily brief for commute consumption. Designed into
the delivery abstraction from day one so it is an additive format, not
a retrofit. The goal is for Navi to eventually fit the commute habit as
naturally as the Economist does today.

### Later -- Learning Queue

Surfaces books, papers, talks, and long-form pieces tied to themes Navi
is already tracking. Routed toward your specific developmental goals
from the professional profile rather than broadly interesting PM
content.

### Later -- Professional Pulse

Passive awareness of the PM job market, comp signals, and evolving skill
expectations at peer companies. With the professional profile in place,
this becomes a genuine gap analysis: "here's what Director-level PM
roles at fintech companies are emphasizing that isn't in your current
portfolio." Not job searching -- staying calibrated and ahead of the
curve.

---

## Out of Scope

**Navi is not a task manager.**
Navi does not own or manage todos. It may surface relevant tasks from an
external system as a contextual nudge in the daily brief, but the source
of truth lives elsewhere. Navi reads; it does not write to your task
system.

**Navi is not a social media aggregator.**
Twitter/X, LinkedIn feeds, and similar high-noise sources are explicitly
excluded. These optimize for engagement, not signal.

**Navi is not a home automation system.**
Ruby-core owns that domain. Navi may consume context from ruby-core
(Ada summaries, home state) but never controls it.

**Navi does not touch work systems.**
Corporate email, calendar, Slack, and any Capital One infrastructure are
permanently out of scope. Navi's professional intelligence comes from
public sources -- news, research, market signals -- not internal systems.
This is a feature: it means Navi never touches anything with compliance
implications.

**Navi is not a general-purpose assistant.**
It is deeply personalized to one user's professional and personal context.
It is not designed to answer arbitrary questions or serve as a
general-purpose chatbot.

---

## Delivery & Inbound Channels

**Outbound -- Email (Resend)**
Primary delivery channel for all digests, briefs, and analysis. HTML
formatted, section-structured, with source citations and links.
Credentials in Vault at secret/data/navi/resend.

**Inbound & Outbound -- SMS (Twilio)**
Conversational interface for directed actions and on-demand requests.
Messages arrive and are sent from a dedicated Twilio long code to the
native iOS Messages app -- zero new apps or habits required.
Estimated cost: ~$6/month at personal usage volume.
Credentials in Vault at secret/data/navi/twilio.

**Notification layer -- Home Assistant Companion App**
Simple actionable notifications for binary prompts: confirm, snooze,
acknowledge. Complements SMS for moments where a single tap is the right
interaction rather than a typed response.

---

## Data & Privacy Architecture

- All personal data lives in the Foundation Postgres instance
  (10.0.40.10:5432), credentials at secret/data/navi/postgres
- pgvector installed from day one to support semantic retrieval across
  stored content -- used initially for deduplication, later for trend
  analysis and research synthesis
- Rolodex and professional profile data are never passed to external
  APIs as input
- External API calls (Claude, Resend, Twilio) receive only the minimum
  context necessary to perform their function
- Navi builds a preference and feedback model over time -- open/read
  signals on digests and explicit feedback inform summarizer
  personalization

---

## Infrastructure

- Dockerized service on the same node as ruby-core and Foundation,
  consistent with existing patterns
- NATS JetStream (from ruby-core) as the internal event bus, namespaced
  under navi.> -- zero collision risk with ruby-core subjects
- All secrets via Vault -- no hardcoded credentials anywhere
- Makefile-driven operations as the primary interface
- CloudEvents v1.0 as the event contract, consistent with ruby-core
- NATS migration to Foundation flagged as future infrastructure work;
  has no impact on Navi's subject topology when it occurs

---

## Success Criteria (6 months)

- The daily digest is read consistently and feels worth the 5 minutes
- At least one "I already knew about that" moment per week in a
  professional context, attributable to Navi
- The Rolodex has replaced ad-hoc context lookup for meeting prep
- The SMS interface has been used to take at least one real action
  (calendar write, research request, Rolodex update)
- The professional profile is populated and has informed at least one
  deliverable or development conversation
- Zero features feel like maintenance burdens
- A Katie-tailored brief is scoped and ready to ship

---

## What Navi Is Not Trying to Be

Navi is not a product. It will not be open-sourced, marketed, or
generalized. Every decision optimizes for one user's life, not for broad
applicability. That constraint is a feature -- it means Navi can be
opinionated, personal, and uncompromising in a way no commercial product
ever could be.
