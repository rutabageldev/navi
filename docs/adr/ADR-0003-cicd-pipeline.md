# ADR-0003: CI/CD Pipeline and Environment Strategy

## Status
Accepted

## Date
2026-04-06

## Context

Navi requires a repeatable, automated deployment pipeline that provides
meaningful validation before code reaches production, with guaranteed
rollback when validation fails. The global engineering standards for
this homelab require that every production deployment be capable of
automatically rolling back to the last known stable version on failure.

The original pipeline design (manual tagging, no automated rollback)
was a starting point. This ADR documents the finalized decisions on
versioning, change detection, and rollback that supersede the initial
draft.

Several constraints shape these decisions:

- Navi is a monorepo containing multiple services under `services/`.
  Versioning and deployment must scale to multiple services without
  per-service version management overhead.
- Conventional Commits are already required by ADR-0012. The version
  bump rules encoded in release-please map directly onto this commit
  convention, making automated versioning a natural extension of an
  existing practice rather than a new burden.
- The homelab node is not publicly reachable from GitHub Actions
  runners. All deployment steps that touch the node run on a
  self-hosted runner.
- A fully isolated staging environment (dedicated Postgres, NATS,
  Vault) is disproportionate overhead for a single-node homelab.
  Isolation is achieved at the data and secrets layer as documented
  below.
- Backward-compatible database migrations are a prerequisite for safe
  automated rollback. The migration constraint that makes rollback safe
  is defined in ADR-0004 and cross-referenced here.

---

## Decision

### Environment Model

Three environments are defined. This model is unchanged from the
initial design.

**dev**
Local development only. Services run against a local Postgres instance
(Docker Compose), mocked external APIs, and no real credentials. Dev
MUST NOT be deployed to the homelab node. The goal is fast iteration
with zero risk to real data or spend.

**staging**
Deployed to the homelab node, sharing physical infrastructure with
production. Isolation is achieved at the data and secrets layer:

- Dedicated Postgres schema: `navi_staging`
- Dedicated Vault paths: `secret/data/navi/staging/...`
- Sandbox/test API credentials for all paid external services:
  - Resend: sandbox mode (emails rendered but not delivered)
  - Twilio: test credentials (SMS not sent, no cost incurred)
  - Anthropic: real API key (no sandbox available; low-volume staging
    calls are real but cost-negligible)
- NATS subjects prefixed with environment: `navi.staging.>`
- Distinct Docker container names to avoid conflicts with prod

Staging MUST be the only gate between code and production. No code
MUST NOT reach prod without passing staging smoke tests.

**prod**
Deployed to the homelab node. Uses live credentials, the `navi_prod`
Postgres schema, and `navi.prod.>` NATS subjects. All real
communications (email, SMS) originate from prod only.

---

### Version Management — release-please

All version management MUST be handled by `release-please`. No version
numbers MUST NOT be manually maintained in source files, commit
messages, or branch names.

release-please reads the Conventional Commit history since the last
release tag to determine the appropriate version bump:

```
fix:                        → patch bump  (v1.2.3 → v1.2.4)
feat:                       → minor bump  (v1.2.3 → v1.3.0)
feat!: or BREAKING CHANGE:  → major bump  (v1.2.3 → v2.0.0)
```

`bump-minor-pre-major: true` MUST be set so that `feat:` commits bump
the minor version while the project is pre-1.0, rather than having
every feature increment 0.x.0 and stall at a meaningless patch stream.

The release-please workflow:

```
Developer merges a feature branch to main via PR
        |
        v
release-please GitHub Actions workflow runs on push to main
        |
        v
release-please creates or updates a "Release PR" on main
  - Title: "chore(release): release v1.3.0"
  - Body: auto-generated changelog from Conventional Commits
  - Updates .versions.json with the new version number
        |
        v
Developer reviews and merges the Release PR when ready to ship
        |
        v
release-please cuts the git tag (e.g. v1.3.0)
        |
        v
Tag triggers the deployment pipeline (see Pipeline section below)
```

Configuration MUST live in `release-please-config.json` at the repo
root. release-please MUST be configured to use `.versions.json` as the
manifest file (`--manifest-file=.versions.json`).

---

### Single Source of Truth for Versions

`.versions.json` at the repo root MUST be the only file that contains
version numbers. It is managed exclusively by release-please and MUST
NOT be hand-edited.

All other references to the version MUST be indirect:

**Docker Compose files**
Container image tags MUST reference the version via environment
variable only:
```yaml
image: ghcr.io/rutabageldev/navi-digest:${NAVI_VERSION:-latest}
```
`NAVI_VERSION` is set by the deployment pipeline from the release tag.

**Go binaries**
The runtime version MUST be injected at build time via Go ldflags:
```
-ldflags "-X main.version=$(shell git describe --tags --abbrev=0)"
```
No version string MUST NOT appear as a Go constant or variable
initialized to a literal in source code.

No `VERSION` file, no hardcoded version strings, no duplicate sources
of truth.

---

### Pipeline: Tagged Release Promotion

The deployment pipeline MUST be triggered exclusively by a git tag
created by release-please. Direct pushes to main MUST NOT trigger a
production deployment.

```
release-please cuts tag (e.g. v1.3.0)
        |
        v
GitHub Actions: deploy.yml workflow triggers on tag push
        |
        v
Change detection
  - Identify which services/ directories changed since the previous tag
  - If services/internal/ changed, mark ALL services for redeployment
  - Build Docker images only for changed services
  - Push images to ghcr.io tagged with version + staging
        |
        v
Deploy to staging
  - Set PREVIOUS_VERSION from .last-deployed-version
  - Pull images tagged as staging
  - Run database migrations against navi_staging schema
  - Start containers with staging compose profile
  - Run smoke test suite (make smoketest ENV=staging)
        |
        v
Staging smoke tests pass?
  FAIL:
    - Roll staging back to PREVIOUS_VERSION
    - Run smoke tests again against PREVIOUS_VERSION to confirm rollback
    - Send rollback notification via SMS: "Hey, listen! Navi v1.3.0
      staging deploy failed. Rolled back to v1.2.9."
    - Pipeline stops. Prod is never touched.
  PASS:
    - Continue to production promotion
        |
        v
Promote to prod
  - Re-tag image as prod and latest
  - Run database migrations against navi_prod schema
  - Rolling restart of prod containers
  - Run prod health checks (make healthcheck ENV=prod)
        |
        v
Prod health checks pass?
  FAIL:
    - Roll prod back to PREVIOUS_VERSION
    - Run health checks again against PREVIOUS_VERSION to confirm rollback
    - Send rollback notification via SMS: "Hey, listen! Navi v1.3.0 prod
      deploy failed. Rolled back to v1.2.9."
    - Pipeline exits non-zero
  PASS:
    - Commit .last-deployed-version with new version to main
    - Send success notification via SMS: "Hey, listen! Navi v1.3.0
      deployed successfully."
```

---

### Change Detection — Selective Service Deployment

The pipeline MUST detect which services have changed since the last
release tag and MUST only build and redeploy those services.

```bash
CHANGED=$(git diff --name-only \
  $(git describe --tags --abbrev=0 HEAD^)..HEAD \
  -- services/)
```

Rules:
- If any file under `services/internal/` changed, ALL services MUST
  be redeployed. A shared library change affects all consumers and
  cannot be selectively deployed.
- If only files under `services/{service-name}/` changed, only that
  service MUST be redeployed.
- If no files under `services/` changed (e.g. docs-only release), no
  service deployments occur.

This provides the deployment efficiency of per-service versioning
without the version management complexity of maintaining separate
version numbers per service.

---

### Rollback

Automated rollback MUST fire at two gates: staging smoke test failure
and prod health check failure. Manual rollback MUST also be available
as an emergency escape hatch.

**Reference version**
`.last-deployed-version` at the repo root records the last version that
was successfully deployed to prod. It is committed to main by the
pipeline after every successful prod deployment. The pipeline MUST read
this file before beginning a deployment to capture `PREVIOUS_VERSION`.
`.last-deployed-version` MUST NOT be hand-edited.

Initial value: `none` (signals no rollback target for the first deploy).

**Automated rollback (staging)**
If staging smoke tests fail:
1. Redeploy staging containers at `PREVIOUS_VERSION`
2. Run smoke tests against `PREVIOUS_VERSION` to confirm the rollback
   is healthy
3. Send rollback SMS notification
4. Exit the pipeline; prod is not touched

**Automated rollback (prod)**
If prod health checks fail after a deployment:
1. Redeploy prod containers at `PREVIOUS_VERSION`
2. Run health checks against `PREVIOUS_VERSION` to confirm the rollback
   is healthy
3. Send rollback SMS notification
4. Exit the pipeline non-zero

**Manual rollback**
A Makefile target MUST be available for emergency use:
```
make rollback ENV=staging VERSION=v1.2.9 SERVICE=digest
make rollback ENV=prod VERSION=v1.2.9 SERVICE=digest
```
Manual rollback does not update `.last-deployed-version`. It is an
emergency procedure, not a deployment.

---

### Database Migration Constraint

Rollback is only safe if the database schema at version N+1 is
backward compatible with the application code at version N. This
requires a constraint on how migrations are written.

All migrations MUST be additive only:
- Adding new tables: allowed
- Adding new nullable columns: allowed
- Adding new indexes: allowed
- Dropping columns, renaming columns, changing column types: MUST NOT
  be done in a single migration

Any schema change that would break a previous application version MUST
be executed as a multi-step migration across separate releases:
1. Release N: add the new column/table in parallel to the old one
2. Release N+1: migrate data, switch application to use new structure
3. Release N+2: drop the old column/table (only after N is no longer
   the rollback target)

This constraint is formally defined in ADR-0004 and applies to all
migrations in all Navi services.

---

### Smoke Test Suite

Smoke tests run in staging after deployment and MUST all pass before
prod promotion. They are lightweight integration tests whose purpose is
to confirm the system is correctly wired, not to validate business logic.

Smoke tests are implemented as a Go binary at
`services/digest/cmd/smoketest/` and invoked via:
```
make smoketest ENV=staging
make smoketest ENV=prod    # health checks only for prod gate
```

Minimum smoke tests for P0 (the hello-world milestone):

| Test | Validates | Pass condition |
|------|-----------|----------------|
| Health live | Process is running | `GET /v1/health/live` returns 200 |
| Health ready | All dependencies reachable | `GET /v1/health/ready` returns 200 with all checks ok |
| Version present | Binary built with correct version | `ready` response body contains `version` matching deployed tag |
| Postgres check | DB connection established | `checks.postgres == "ok"` in ready response |
| NATS check | Event bus reachable | `checks.nats == "ok"` in ready response |
| Vault check | Secrets accessible | `checks.vault == "ok"` in ready response |

Additional smoke tests are added as each service component is
implemented.

---

### Image Registry

Docker images MUST be pushed to GitHub Container Registry (ghcr.io)
using the repository's built-in GitHub Actions credentials.

```
ghcr.io/rutabageldev/navi-digest:v1.3.0    # version-pinned
ghcr.io/rutabageldev/navi-digest:staging   # mutable, current staging
ghcr.io/rutabageldev/navi-digest:prod      # mutable, current prod
ghcr.io/rutabageldev/navi-digest:latest    # alias for prod
```

---

### Branch and Tag Conventions

```
main              -- stable, deployable at all times
feature/*         -- feature branches, merged via PR
fix/*             -- bug fix branches, merged via PR
chore/release-*   -- release-please Release PRs (auto-managed)
v{major}.{minor}.{patch}  -- release tags, cut by release-please
```

Direct commits to main MUST NOT be made. All changes MUST go through
a pull request. The pipeline's commit of `.last-deployed-version` back
to main is the only exception and is performed by the CI bot identity,
not by the developer.

Tags MUST be created by release-please only. MUST NOT be created
manually.

---

### Makefile Targets

All operational tasks MUST be available as Makefile targets. The
complete set for the CI/CD pipeline:

```
make setup              # install pre-commit hooks and dev dependencies
make dev                # start local dev environment (docker compose dev)
make test               # run unit tests with -race flag
make lint               # run golangci-lint
make build              # build all changed service Docker images
make deploy ENV=x       # deploy to environment x (staging or prod)
make smoketest ENV=x    # run smoke test suite against environment x
make healthcheck ENV=x  # run health checks against environment x
make rollback ENV=x VERSION=y SERVICE=z  # emergency rollback
make migrate ENV=x      # run pending migrations against environment x
make vault-seed ENV=x   # seed Vault paths with placeholder values
make logs ENV=x         # tail container logs for environment x
make status             # show running container status across all envs
make check-generated    # verify oapi-codegen output is current
```

---

## Consequences

**Positive:**
- release-please eliminates all manual version management. Version
  numbers emerge from commit discipline that is already required, not
  from a separate manual process.
- Selective deployment reduces pipeline time and deployment blast
  radius. A docs-only change does not trigger a service restart.
- Two-gate automated rollback satisfies the global engineering standard
  for production rollback capability without requiring manual
  intervention.
- `.last-deployed-version` provides an unambiguous, version-based
  rollback reference that is operationally legible ("roll back to
  v1.2.9" rather than "roll back to commit abc123").
- The migration backward-compatibility constraint means that automated
  code rollback is always safe against the current schema.

**Negative / tradeoffs:**
- release-please adds two configuration files
  (`release-please-config.json`, `.versions.json`) and a GitHub
  Actions workflow that must be maintained.
- Change detection logic in the pipeline adds complexity. The
  `services/internal/` trigger-all rule is a blunt instrument; a
  one-line change to a utility package causes a full redeploy.
- The pipeline's commit of `.last-deployed-version` back to main is
  a "robot commit" that bypasses the no-direct-commit-to-main rule.
  This is an intentional, documented exception scoped to the CI bot
  identity only.
- Staging shares physical infrastructure with production. A
  catastrophic node failure takes down both simultaneously. Accepted
  for a single-node homelab.
- Anthropic has no sandbox mode. Staging smoke test calls against the
  Claude API are real API calls. Volume is low and cost is negligible.

**Neutral:**
- Three environments is the same model as Foundation and ruby-core;
  this ADR extends an existing pattern.
- The self-hosted GitHub Actions runner requirement is unchanged from
  the initial design; it is a prerequisite for node access, not an
  architectural choice.
- The smoke test suite structure and invocation pattern are identical
  for staging and prod; only the tests included differ.

---

## Alternatives Considered

**Manual version bumping via a VERSION file**
Rejected. Manual bumping is error-prone and creates a separate
discipline requirement on top of Conventional Commits. release-please
leverages the commit history that is already required, making versioning
a zero-overhead consequence of good commit discipline.

**Per-service versioning**
Rejected. Per-service versioning requires maintaining separate version
numbers, separate release-please configs, and separate deployment
pipelines per service. For a small monorepo with shared internal
libraries, this overhead is not justified. Repo-level releases with
change-detection-based selective deployment deliver the deployment
efficiency of per-service versioning without the management complexity.

**Rollback to a specific git commit rather than a version tag**
Rejected. Version-based rollback is operationally legible. "Roll back
to v1.2.9" is unambiguous; "roll back to commit abc123def" requires
a lookup to understand what that means. Version tags also correspond
to built and pushed Docker images, so rollback is a re-deploy of an
existing image rather than a rebuild.

**Deploy on every push to main**
Rejected. Continuous deployment to production without a validation gate
is inappropriate for a system that sends real SMS and email. A bad
push would cause failed deliveries or unexpected communications.

**Full isolated staging environment**
Rejected. Dedicated Postgres, NATS, and Vault instances for staging
would double the operational surface area of the homelab with no
meaningful additional protection for a single-user personal project.
Schema and secret isolation on shared infrastructure is sufficient.
