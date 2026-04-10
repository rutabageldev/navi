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

Pre-commit hooks run at commit time, not push time. Issues are caught
at the earliest possible moment — before they exist in local history.
A problem caught at commit is cheaper to fix than one caught in CI
after a push, and cheaper still than one that reaches a PR review.

Not all hooks run on every commit. Slow hooks that make external
network calls (e.g. govulncheck) run only when the files that could
introduce a problem are modified.

**CVE coverage strategy**

| Where | Tool | Catches |
|---|---|---|
| Pre-commit (every commit) | gitleaks | Secrets and credentials |
| Pre-commit (go.mod changes only) | govulncheck | Newly introduced vulnerable deps |
| CI (every push) | govulncheck | All reachable vulnerable code paths |
| CI (weekly scheduled) | govulncheck | New CVEs in existing dependencies |

The following hooks run on every commit in the order listed, except
where noted:

**Secrets Scanning**
```yaml
- repo: https://github.com/gitleaks/gitleaks
  rev: v8.24.3
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
  rev: v5.0.0
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

**Go Formatting and Vetting**
```yaml
- repo: local
  hooks:
    - id: go-fmt
      name: go fmt
      language: system
      entry: gofmt -l -w
      types: [go]
    - id: go-vet
      name: go vet
      language: system
      entry: bash -c './scripts/govet.sh'
      pass_filenames: false
      types: [go]
    - id: go-imports
      name: goimports
      language: system
      entry: goimports -l -w
      types: [go]
```

`gofmt` and `goimports` run on all modified Go files. Unformatted
code does not commit. `go vet` catches common correctness issues.

Local hooks are used instead of `dnephin/pre-commit-golang` because
this repo uses a `go.work` workspace. The upstream hooks pass
individual file paths to `go vet`, which does not work correctly in
a workspace context. The local `scripts/govet.sh` wrapper iterates
over modules explicitly and skips modules with no Go files.

**Vulnerability Scanning (go.mod changes only)**
```yaml
- repo: local
  hooks:
    - id: govulncheck-on-gomod-change
      name: govulncheck (go.mod changes only)
      entry: bash -c './scripts/govulncheck.sh'
      language: system
      files: ^services/.*/go\.mod$
      pass_filenames: false
```

`govulncheck` scans all reachable code paths against the Go
vulnerability database. It is scoped to commits that modify a
`go.mod` file because it makes network calls to the vuln database
and is too slow to run on every commit. Running it on `go.mod`
changes catches newly introduced vulnerable dependencies at the
moment they are added, with no cost on normal code commits.

The hook invokes `scripts/govulncheck.sh` rather than calling
`govulncheck ./...` directly. This is required because the repo
uses a `go.work` workspace — there is no `go.mod` at the root, so
`govulncheck ./...` would fail. The script iterates over each
workspace module, runs `govulncheck` within that module's directory,
and prepends the toolchain version from `go.work` to PATH so the
stdlib scan reflects the version the code will actually be compiled
with.

`govulncheck` MUST be installed on the developer's machine:
```
go install golang.org/x/vuln/cmd/govulncheck@latest
```

The required Go toolchain is pinned via the `toolchain` directive in
`go.work`. When a new toolchain is downloaded (via
`go install golang.org/dl/goX.Y.Z@latest && goX.Y.Z download`), the
script resolves it automatically from `$HOME/sdk/goX.Y.Z/bin`.

**OpenAPI Spec Validation**
```yaml
- repo: local
  hooks:
    - id: validate-openapi
      name: Validate OpenAPI specs
      language: system
      entry: check-jsonschema --schemafile https://spec.openapis.org/oas/3.1/schema/2022-10-07
      types: [yaml]
      files: services/.*/api/openapi\.yaml$
```

OpenAPI specs are validated against the OAS 3.1 JSON Schema on every
commit that modifies a spec file. An invalid spec does not commit.

The `check-jsonschema` CLI tool (pip: `check-jsonschema`) MUST be
installed on the developer's machine. It is not managed by the
pre-commit framework's environment isolation for this hook because
it runs as a system-language local hook.

Note: The `check-openapi` hook ID previously referenced in this ADR
does not exist in the `python-jsonschema/check-jsonschema` repository.
The local hook approach above is the correct implementation and
produces equivalent behavior.

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
  rev: v4.6.0
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
    - govet           # correctness issues
    - ineffassign     # unused assignments
    - staticcheck     # static analysis (absorbs gosimple in v2)
    - unused          # unused code
    - gosec           # security issues
    - bodyclose       # unclosed HTTP response bodies
    - contextcheck    # improper context usage
    - noctx           # HTTP requests without context
    - exhaustive      # missing switch cases on enums
    - godot           # comment formatting
    - misspell        # spelling errors

formatters:
  enable:
    - gofmt           # formatting
    - goimports       # import ordering
```

Note: golangci-lint v2 separates formatters from linters. `gofmt` and
`goimports` MUST be listed under `formatters.enable`, not `linters.enable`.
`gosimple` was merged into `staticcheck` in v2 and MUST NOT appear as a
standalone linter.

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
