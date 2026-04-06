# ADR-0013: Operational Standards

## Status
Accepted

## Date
2026-04-06

## Context

Navi runs unattended on a single homelab node and accumulates data
continuously. Without defined operational policies, several problems
emerge over time: unbounded Postgres growth, stale credentials that
may have been compromised, unpatched vulnerabilities in dependencies,
and no recovery path when the node fails.

This ADR defines the operational baseline for Navi: data retention
and pruning, backup and recovery, secret rotation, and dependency
maintenance. These are not glamorous decisions but they are the
difference between a system that is reliable over years and one that
quietly degrades.

## Decision

### Data Retention and Pruning

Data accumulates in Navi's Postgres schemas from day one. Retention
policies are defined per table type to balance historical utility
against storage growth.

**Articles**
Raw articles are the highest-volume table. Most articles are
ephemeral -- they inform a digest and have no ongoing utility.

```
Raw content (articles.raw_content):  Nullified after 30 days
Article metadata (all other fields): Retained for 12 months
Article embeddings:                  Retained for 12 months
                                     (required for trend analysis)
Articles with no digest inclusion:   Deleted after 14 days
```

After 30 days the raw HTML/text content is nullified (set to NULL)
while the metadata, URL, title, and embedding are retained. This
preserves the semantic record of what was collected without storing
the full content of thousands of articles indefinitely.

**Digests**
Digests are the primary output artifact and have lasting reference
value.

```
Digest content (HTML and text):  Retained for 24 months
Digest metadata:                 Retained indefinitely
digest_articles join records:    Retained for 24 months
Per-article summaries:           Retained for 24 months
```

**Feedback signals**
```
feedback_signals:   Retained for 12 months
                    (used for summarizer personalization)
```

**Rolodex and profile data (future tables)**
People, companies, interactions, and profile entries are personal
records with indefinite value. No automated pruning. Manual
deletion via a Makefile target when the user chooses to remove
a record.

**NATS JetStream**
Retention is defined per stream in ADR-0011:
- Processing subjects: 24 hours
- Error and security subjects: 7 days

**Pruning Implementation**
A scheduled pruning job runs weekly (Sunday 2:00am) as a Makefile-
invocable Go binary. It executes the retention policies above in a
single transaction per table. Pruning is logged and the row counts
deleted are published as metrics (`navi_pruning_rows_deleted_total`
by table name).

```
make prune ENV=prod   # manual invocation
```

### Backup and Recovery

**What is backed up**
The `navi_prod` Postgres schema is the only stateful asset that
requires backup. NATS state is ephemeral by design. Configuration
is in the repository. Secrets are in Vault (which has its own backup
policy as a Foundation concern).

**Backup mechanism**
A daily `pg_dump` of the `navi_prod` schema runs at 3:00am via a
scheduled job in the Navi Docker Compose stack. The dump is:
- Compressed with gzip
- Written to a mounted backup volume on the host
- Retained for 30 days (rolling)
- Named with a timestamp: `navi_prod_{YYYY-MM-DD}.sql.gz`

```bash
# Backup job command
pg_dump \
  --schema=navi_prod \
  --format=custom \
  --compress=9 \
  --file=/backups/navi_prod_$(date +%Y-%m-%d).dump.gz \
  $DATABASE_URL
```

**Offsite backup**
Local backups on the same node as the database are insufficient --
a node failure destroys both. Backups MUST be synced daily to
OneDrive via `rclone`. The offsite sync MUST run immediately after
the local backup completes. Failure of the offsite sync triggers a
warning alert (ADR-0009) but MUST NOT block normal operation.

**Recovery targets**
```
Recovery Point Objective (RPO):  24 hours (one day of data loss
                                  is acceptable for a personal
                                  intelligence system)
Recovery Time Objective (RTO):   4 hours (time to restore Postgres
                                  schema and restart services)
```

**Recovery procedure**
A `make restore ENV=prod BACKUP=navi_prod_YYYY-MM-DD.dump.gz`
Makefile target performs the full restore:
1. Drops the existing `navi_prod` schema
2. Restores from the specified backup file
3. Runs any pending migrations
4. Restarts services

The recovery procedure is tested manually at least once per quarter
to confirm it works. An untested backup is not a backup.

### Secret Rotation

All Navi credentials stored in Vault are subject to rotation.
Rotation is manual for v1 -- no automated rotation tooling is
introduced at this stage. The rotation schedule and procedure are
documented here so they are not forgotten.

**Rotation schedule:**

```
Anthropic API key:   Every 90 days, or immediately on suspected
                     compromise
Resend API key:      Every 90 days
Twilio credentials:  Every 180 days (auth tokens), or immediately
                     on suspected compromise
Postgres password:   Every 180 days
Vault tokens:        Per Foundation rotation policy
```

**Rotation procedure (zero-downtime pattern):**

1. Generate the new credential in the external service
2. Write the new credential to Vault at the existing path
3. Send SIGHUP to the relevant Navi service (services reload secrets
   on SIGHUP without restarting)
4. Verify the service is functioning (health check, test send)
5. Revoke the old credential in the external service

Services MUST reload credentials from Vault on SIGHUP rather than
at startup only. This enables zero-downtime rotation without a
container restart.

A `make rotate-secret SERVICE=resend ENV=prod` Makefile target
guides the rotation procedure interactively.

**Rotation reminders**
A calendar reminder is set for each rotation schedule. These are
not automated -- they are calendar entries. A future enhancement
could have Navi remind itself when credentials are due for rotation
using the `updated_at` timestamp on Vault secret metadata.

### Dependency Maintenance

**Vulnerability scanning**
`govulncheck` runs in CI on every push (ADR-0012) and on a weekly
GitHub Actions schedule targeting the main branch. The weekly scan
catches vulnerabilities that appear in existing dependencies between
code changes.

Weekly scan results are delivered to the developer via a brief
email (using the Navi Resend integration) listing any new findings.
No findings requires no email -- silence is success.

**Dependency update cadence**
Dependencies are reviewed and updated on a monthly cadence, not
automatically. The monthly update process:

1. `go get -u ./...` to pull available updates
2. `go mod tidy` to clean the module graph
3. Run the full test suite
4. Review the diff for unexpected transitive dependency changes
5. Commit with message `chore(deps): monthly dependency update`

Security findings with HIGH or CRITICAL severity trigger an
immediate out-of-cycle update rather than waiting for the monthly
cadence.

**Go version updates**
The Go version is updated to the latest stable release within 30
days of each new minor release. Go minor releases are backward
compatible and typically include performance improvements and
security fixes. The Go version is pinned in the Dockerfile and
updated explicitly.

### Operational Makefile Targets

All operational procedures are encapsulated in Makefile targets
to reduce the cognitive load of remembering commands:

```
make backup ENV=prod              # run backup manually
make restore ENV=prod BACKUP=...  # restore from backup
make prune ENV=prod               # run data pruning
make rotate-secret SERVICE=...    # guided secret rotation
make check-deps                   # run govulncheck locally
make update-deps                  # run monthly dependency update
make test-recovery                # test recovery procedure (destructive
                                  # on dev only -- never run on prod)
```

### Operational Runbook

`docs/runbooks/` documents the response procedure for each defined
alert condition (cross-referencing ADR-0009) and each operational
task. It is the first place to look when something goes wrong. It
MUST be kept up to date as a condition of closing any incident,
however minor.

## Consequences

**Positive:**
- Defined retention policies prevent unbounded Postgres growth
  without manual intervention.
- The `pg_dump` backup with offsite sync is simple, reliable, and
  requires no additional infrastructure.
- Secret rotation on a defined schedule reduces the blast radius
  of a compromised credential.
- Makefile targets for all operational tasks mean procedures are
  documented as executable code, not just prose.

**Negative / tradeoffs:**
- Manual secret rotation requires calendar discipline. Automated
  rotation (e.g. Vault dynamic secrets) is the long-term solution
  but adds tooling complexity that is not justified at v1.
- The 24-hour RPO means up to one day of digest history and Rolodex
  updates could be lost in a failure. Accepted -- this is a personal
  intelligence system, not a financial ledger.
- Testing the recovery procedure quarterly requires intentionally
  destroying and restoring a dev environment. This is uncomfortable
  but necessary. An untested backup is not a backup.

**Neutral:**
- The weekly pruning schedule (Sunday 2:00am) is chosen to avoid
  the daily digest window; the specific time carries no architectural
  significance and can be adjusted without a code change.
- golang-migrate is already established as the migration tool in
  ADR-0002; the retention policies defined here require no new
  migration tooling.

## Alternatives Considered

**Automated secret rotation via Vault dynamic secrets**
Deferred to future work. Vault dynamic secrets provide on-demand,
short-lived credentials with automatic rotation. This is the ideal
long-term pattern but requires Vault configuration beyond what is
in place today. The manual rotation policy defined here is the
bridge.

**Continuous backup (WAL archiving)**
Rejected. WAL archiving would reduce the RPO to near-zero but
requires a running PostgreSQL archive destination and significantly
more operational complexity. A 24-hour RPO is acceptable for this
use case.

**Automated dependency updates via Dependabot auto-merge**
Rejected. Consistent with ADR-0012, dependency updates are reviewed
deliberately. Automatic merges of dependency updates have caused
production incidents in larger projects. Manual monthly review is
the right balance for a system of this scale.
