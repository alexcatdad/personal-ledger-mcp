# Contributing

Read AGENTS.md and RUNBOOK.md first. Keep one Go module and the existing cmd/internal layout. Add abstractions when actual callers need them; avoid generic repositories or extra service layers around domain operations.

## Checks

Use Go 1.27.1, Node 26.10.0, and the isolated PostgreSQL 16 setup in RUNBOOK.md.

- `make fmt` formats Go source; `make lint` runs pinned golangci-lint.
- `make test` is quick feedback and can skip PostgreSQL tests. It is not a release gate.
- `TEST_DATABASE_URL='<disposable database URL>' make verify` runs formatting, dependency integrity, vet/lint, mandatory PostgreSQL race tests, vulnerability checks, generated-file drift, frontend checks and builds. GitHub Actions runs this same gate on disposable GitHub-hosted runners and PostgreSQL for pull requests and main-branch pushes.
- Commit intentionally changed generated files before running drift checks. Never manually edit sqlc or OpenAPI-generated files.

Tests should prove financial invariants, retry/version behavior, rollback, and MCP/API contracts using real PostgreSQL. Do not parallelize tests that share a schema. Regression tests accompany behavior fixes; avoid tests that only restate implementation. No arbitrary coverage percentage substitutes for boundary tests.

## Changes and review

Keep PRs focused. Domain services own rules; adapters translate protocols. Pass request contexts through database calls, check errors, wrap errors when context helps, and preserve errors.Is behavior. Close resources at the owning layer. A linter suppression needs a specific rule and a reason; do not baseline away findings.

Never commit credentials, logs containing financial data, database dumps, or local runtime configuration. No automatic startup migrations or arbitrary SQL/shell MCP tools. Preserve the exact Tailscale identity/proxy boundary described in AGENTS.md.

Use the PR template and record meaningful decisions in decisions.jsonl. Review the diff and run the full verification gate on the exact commit before merging. Keep untrusted pull-request code away from private runners and credentials. Deployment is separate and requires an immutable image, rollback and live verification.


## Dependencies and tools

Keep go.mod/go.sum and package-lock.json committed. Tool versions live in Makefile; change them deliberately. `go mod tidy -diff` rejects manifest drift and `go mod verify` checks cached module integrity. `make vuln` checks reachable Go vulnerabilities; it is not a full security audit.

During maintenance, inspect `go list -m -u all`, review upstream release notes, update selected modules with `go get module@version`, run `go mod tidy`, and run `make verify`. Review Go versions in go.mod, Dockerfile, CI and these docs together. No unattended dependency upgrades or new hosted automation are required for this POC.

References: https://go.dev/doc/modules/managing-dependencies and https://golangci-lint.run/docs/configuration/file/ (checked 2026-09-28).
