GOLANGCI_VERSION := v2.14.0
GOVULNCHECK_VERSION := v1.8.0
SQLC_VERSION := v1.31.1
TEST_PARALLELISM ?= 2

.PHONY: test check fmt fmt-check lint vet test-integration deps-check vuln generate generated-check openapi frontend frontend-check verify build migrate

# Fast feedback; PostgreSQL tests skip unless TEST_DATABASE_URL is configured.
test:
	go test ./...

fmt:
	gofmt -w cmd internal

fmt-check:
	test -z "$$(gofmt -l cmd internal)"

lint:
	sh scripts/install-golangci-lint.sh $(GOLANGCI_VERSION)
	.tools/golangci-lint-$(GOLANGCI_VERSION)-$$(go env GOHOSTOS)-$$(go env GOHOSTARCH)/golangci-lint run --timeout=5m

vet:
	go vet ./...

test-integration:
	@test -n "$(TEST_DATABASE_URL)" || { echo 'Set TEST_DATABASE_URL to the disposable PostgreSQL database from RUNBOOK.md'; exit 1; }
	REQUIRE_INTEGRATION=1 go test -race -p $(TEST_PARALLELISM) ./...

# Same backend gate locally and in Woodpecker. Never silently skips database tests.
check: fmt-check deps-check vet lint test-integration

deps-check:
	go mod tidy -diff
	go mod verify

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

generate:
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION) generate

openapi:
	go run ./cmd/pa-mcp openapi > web/openapi.json

generated-check: generate openapi
	git diff --exit-code -- internal/db web/openapi.json

frontend:
	cd web && npm ci && npm run build

frontend-check:
	cd web && npm ci && npm run generate:api
	git diff --exit-code -- web/src/lib/api.gen.ts
	cd web && npm run typecheck && npm run build

verify: check vuln generated-check frontend-check build

build:
	go build -trimpath -o bin/pa-mcp ./cmd/pa-mcp

migrate:
	go run ./cmd/pa-mcp migrate
