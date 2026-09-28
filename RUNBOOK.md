# Development and release runbook

## Isolated local setup

Requires Go 1.27.1, Node 26.10.0, Docker, curl and make. Never run tests against a production database.

```sh
docker run --name pa-mcp-dev-postgres -e POSTGRES_USER=pa_mcp -e POSTGRES_PASSWORD=local-development-only -e POSTGRES_DB=pa_mcp_test -p 127.0.0.1:55439:5432 -d postgres:16
export DATABASE_URL='postgres://pa_mcp:local-development-only@127.0.0.1:55439/pa_mcp_test?sslmode=disable'
export TEST_DATABASE_URL="$DATABASE_URL"
export PA_MCP_AUTH_MODE=bearer
export PA_MCP_TOKEN="$(openssl rand -hex 32)"
make migrate
make frontend build
./bin/pa-mcp
```

Open http://127.0.0.1:8080. Development credentials above are disposable examples, never production credentials. Tokens remain in environment/session memory. Stop only the named development container when finished.

## Verification

Run `make fmt` then `make verify` with TEST_DATABASE_URL pointing to the isolated database. The gate includes pinned checksum-verified golangci-lint, go vet, module integrity/tidy checks, mandatory PostgreSQL race tests, govulncheck, SQL/OpenAPI generation checks, frontend types/build and Go build. `make test` may skip integration tests and is not release evidence. Commit intentional generated changes before checking generation drift.

## MCP smoke test

Use an SDK MCP client against `/mcp`: initialize, list tools, then call get_dashboard. Test mutations only against disposable data. Inspect structuredContent and isError, not just HTTP status. Preserve operation_key and payload after ambiguous errors; query state before retrying. Never paste a production token into a command log.

## Deployment

See deploy/README.md. Build an immutable image from verified source. Test that image against disposable PostgreSQL. Explicitly apply Goose migrations with a database-owner identity, then apply scripts/grant-runtime.sql for the separate runtime role. Back up before migrations; record restore/rollback compatibility. Never migrate automatically at server startup.

Keep runtime files outside Git. Verify health, authentication, read-only MCP queries and network binding after activation. Keep the old image/configuration for rollback; do not restore a backup over subsequent writes blindly.

## Public contributions

This repository is not connected to a private CI runner. Review public contributions before running them on trusted machines; never execute untrusted PR code in a homelab or with production credentials. An isolated hosted runner can be added separately. No automatic deploy or publish workflow is included.

## Dependencies

Follow CONTRIBUTING.md. Tool versions live in Makefile. Update selected dependencies deliberately, review release notes and rerun make verify. The optional restore rehearsal script targets only a disposable pa_mcp_demo database in the named local PostgreSQL container; create that demo separately before using it.
