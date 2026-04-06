# ADR-0012: Code Quality and Pre-Commit Gates

## Status
Accepted

## Date
2026-04-06

## Context

Navi is developed by a single engineer using AI-assisted tooling.
AI agents produce large volumes of code quickly, which makes
consistent quality enforcement more important, not less. Without
automated gates, subtle issues -- hardcoded secrets, unformatted
code, failing tests, insecure patterns -- can accumulate across
agent-generated commits before a human review catches them.

Pre-commit hooks are the first quality gate, running locally before
code leaves the developer's machine. CI checks are the second gate,
running on every push and blocking merges. These two layers together
define the quality floor for all code in the navi repository.

## Decision

### Pre-Commit Framework

Pre-commit hooks MUST be managed using the `pre-commit` framework
(pre-commit.com). Hook configuration MUST live in
`.pre-commit-config.yaml` at the repository root. All developers
(and all agents operating on the repository) MUST have pre-commit
installed and hooks initialized via `pre-commit install`.

The Makefile provides a target for this:
```
make setup    # installs pre-commit hooks and other dev dependencies
```

### Pre-Commit Hook Composition

The following hooks run on every commit in the order listed:

**Secrets Scanning**
```yaml
- repo: https://github.com/gitleaks/gitleaks
  rev: v8.18.0
  hooks:
    - id: gitleaks
```
Gitleaks scans every committed file for secrets, credentials, and
high-entropy strings. This is the first hook and blocks the commit
immediately if a potential secret is detected. No commit proceeds
past this gate if a secret is found.

A `.gitleaks.toml` configuration file defines allowlist patterns
for known false positives (test UUIDs, example values in
documentation). Allowlist entries require a comment explaining why
the pattern is safe.

**File Hygiene**
```yaml
- repo: https://github.com/pre-commit/pre-commit-hooks
  rev: v4.5.0
  hooks:
    - id: trailing-whitespace
    - id: end-of-file-fixer
    - id: check-yaml
    - id: check-json
    - id: check-merge-conflict
    - id: check-added-large-files
      args: ['--maxkb=500']
    - id: no-commit-to-branch
      args: ['--branch=main']
```

The `no-commit-to-branch` hook prevents direct commits to main.
All changes must go through a pull request.

**Go Formatting**
```yaml
- repo: https://github.com/dnephin/pre-commit-golang
  rev: v0.5.1
  hooks:
    - id: go-fmt
    - id: go-vet
    - id: go-imports
```

`gofmt` and `goimports` are run on all modified Go files.
Unformatted code does not commit. `go vet` catches common
correctness issues.

**OpenAPI Spec Validation**
```yaml
- repo: https://github.com/python-jsonschema/check-jsonschema
  rev: 0.28.0
  hooks:
    - id: check-openapi
      files: ^services/.*/api/openapi\.yaml$
```

OpenAPI specs are validated against the OpenAPI 3.1 schema on every
commit that modifies a spec file. An invalid spec does not commit.

**JSON Schema Validation**
```yaml
- repo: local
  hooks:
    - id: validate-event-schemas
      name: Validate event schemas
      entry: make validate-schemas
      language: system
      files: ^docs/events/schemas/.*\.json$
      pass_filenames: false
```

Event schema files are validated as valid JSON Schema on every
commit that modifies them.

**Commit Message Format**
```yaml
- repo: https://github.com/commitizen-tools/commitizen
  rev: v3.13.0
  hooks:
    - id: commitizen
```

Commit messages must conform to the Conventional Commits
specification:
```
{type}({scope}): {description}

types: feat, fix, refactor, test, docs, chore, ci
scope: digest, delivery, intent, store, monitoring, infra, etc.

Examples:
feat(digest): add weekly trend synthesis to summarizer
fix(collector): handle feed timeout without dropping subsequent feeds
docs(adr): add ADR-0012 code quality standards
```

Conventional Commits enable automated changelog generation and
make the git history readable as a narrative of system evolution.

### CI Quality Gates

The following checks run in GitHub Actions on every push to a
feature branch and on every pull request to main. These mirror
the pre-commit hooks but run in a clean environment, catching
issues that may have bypassed local hooks.

**Linting -- golangci-lint**
golangci-lint runs with the following linters enabled:

```yaml
linters:
  enable:
    - errcheck        # unchecked errors
    - gosimple        # simplification suggestions
    - govet           # correctness issues
    - ineffassign     # unused assignments
    - staticcheck     # static analysis
    - unused          # unused code
    - gofmt           # formatting
    - goimports       # import ordering
    - gosec           # security issues
    - bodyclose       # unclosed HTTP response bodies
    - contextcheck    # improper context usage
    - noctx           # HTTP requests without context
    - exhaustive      # missing switch cases on enums
    - godot           # comment formatting
    - misspell        # spelling errors
```

The `gosec` linter specifically targets security anti-patterns:
hardcoded credentials, unsafe integer conversions, SQL injection
risks, and similar. It is treated as blocking -- any gosec finding
fails the build.

**Unit Tests**
```
go test ./... -race -coverprofile=coverage.out
```

All tests MUST run with the race detector enabled. A test suite that
passes without the race detector but fails with it indicates a
real concurrency bug. Coverage is reported but not gated -- a
coverage threshold creates perverse incentives to write low-value
tests. Test quality is enforced through code review, not a
percentage floor.

**Build Verification**
```
go build ./...
```

Every package must build cleanly. A package that compiles only
when tests are excluded is a broken package.

**Dependency Audit**
```
govulncheck ./...
```

`govulncheck` scans the dependency tree for known vulnerabilities
in the Go vulnerability database. Any finding with a severity of
HIGH or CRITICAL fails the build. MEDIUM findings are reported
as warnings and reviewed in the PR.

**Schema Consistency Check**
A custom CI step verifies that generated Go types (from OpenAPI
and JSON Schema) are consistent with their source specs. If a spec
has changed but the generated files have not been regenerated, the
build fails. This enforces the spec-first discipline from ADR-0010.

```
make check-generated
```

### Code Review Standards

All changes to main require a pull request. For a single-developer
project, the PR serves as a structured self-review checkpoint rather
than a peer review mechanism. The PR description must include:

- What changed and why (links to relevant ADR if architectural)
- How it was tested
- Any known limitations or follow-up work

Agent-generated code is held to the same standards as hand-written
code. An agent completing a task produces a PR; the developer reviews
and approves it. No agent commits directly to main.

### Dependency Management

Go modules are the dependency management mechanism. The following
policies apply:

- `go.sum` MUST always be committed
- Dependencies are updated deliberately, not automatically. No
  Dependabot auto-merge for dependency updates.
- New dependencies require a comment in the PR explaining why the
  dependency is preferable to a standard library solution or an
  existing dependency.
- Indirect dependencies are reviewed when they appear in `go.sum`
  for the first time.

`govulncheck` runs in CI (as above) and on a weekly schedule via
GitHub Actions to catch vulnerabilities in existing dependencies
between code changes.

## Consequences

**Positive:**
- Secrets scanning as the first hook means a credential cannot
  accidentally reach the repository even in an agent-generated
  commit. This is the highest-value single hook.
- The CI schema consistency check enforces spec-first discipline
  mechanically -- the build fails if a spec is updated without
  regenerating types.
- Conventional Commits produce a machine-readable git history that
  can generate a changelog automatically for release notes.

**Negative / tradeoffs:**
- Pre-commit hooks add latency to the commit operation. On a fast
  machine the full hook suite runs in under 10 seconds for a typical
  change. Accepted.
- The `no-commit-to-branch` hook blocks direct commits to main,
  which means even small one-line fixes require a PR. This is
  intentional -- the PR is the review checkpoint, not an obstacle.

**Neutral:**
- The pre-commit framework runs locally on the developer's machine;
  it does not affect the runtime behavior of Navi in any environment.
- Conventional Commits constrains only the format of commit subject
  lines; it does not restrict commit size, scope, or content.

## Alternatives Considered

**Husky instead of pre-commit**
Rejected. Husky requires Node.js. Pre-commit is language-agnostic
and does not introduce a JavaScript dependency into a Go project.

**Coverage threshold gate**
Rejected. A percentage floor on coverage creates incentives to
write tests that maximize line coverage rather than tests that
validate behavior. Code review is the right mechanism for test
quality assessment.

**Automated dependency updates via Dependabot**
Rejected for auto-merge. Dependency updates are reviewed deliberately
because transitive dependency changes can introduce unexpected
behavior. Dependabot can be used to open PRs but auto-merge is
disabled.
