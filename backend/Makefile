.PHONY: migrate-up migrate-down migrate-create migrate-status migrate-force migrate-version backfill-at-rest rotate-at-rest-key audit-backfill audit-verify backup backup-verify gen-contract-fixtures gen-schema-fixtures gen-api-baseline gen-compat-matrix gen-integration-matrix gen-perf-baseline help

# Database path. The migration CLI reads SQLITE_DB_PATH (the same variable the
# server reads), so every target below EXPORTS this rather than just echoing
# it. Previously the CLI hardcoded its own path and ignored DB_PATH entirely,
# so these targets reported one file and migrated another.
# Snapshot the inbound value first: DB_PATH defaults from it, and SQLITE_DB_PATH
# is then re-exported from DB_PATH, so reading it directly in both places would
# be a self-reference.
INBOUND_DB_PATH := $(SQLITE_DB_PATH)
DB_PATH ?= $(if $(INBOUND_DB_PATH),$(INBOUND_DB_PATH),mycorrhizal.db)
export SQLITE_DB_PATH := $(DB_PATH)
MIGRATIONS_DIR = ./database/migrations

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

migrate-up: ## Run all pending migrations (the server also does this on startup)
	@echo "Running migrations on $(DB_PATH)..."
	@go run cmd/migrate/main.go up

backfill-at-rest: ## Encrypt any rows still plaintext in the at-rest-encrypted columns (issue #380). The server also does this on startup; this is for `make migrate-up`-only workflows.
	@echo "Backfilling at-rest encryption on $(DB_PATH)..."
	@go run cmd/backfill-at-rest/main.go -db $(DB_PATH)

rotate-at-rest-key: ## Rewrap the DEK under a new master key (usage: NEW=<base64-key> or OLD=<base64-key> NEW=<base64-key>). Payloads untouched.
	@if [ -z "$(NEW)" ]; then \
		echo "Error: NEW is required. Usage: make rotate-at-rest-key NEW=\$$(openssl rand -base64 32) [OLD=<base64-key>]"; \
		exit 1; \
	fi
	@go run cmd/rotate-at-rest-key/main.go -new $(NEW) $(if $(OLD),-old $(OLD),) -db $(DB_PATH)

migrate-down: ## Roll back exactly ONE migration -- destructive, see warning below
	@echo "About to roll back ONE migration on $(DB_PATH)."
	@echo "This runs that migration's .down.sql and DESTROYS the data in whatever"
	@echo "it drops. There is no undo. Back the database file up first."
	@printf "Type 'yes' to continue: " && read ans && [ "$$ans" = "yes" ]
	@go run cmd/migrate/main.go down

migrate-create: ## Create a new migration file (usage: make migrate-create NAME=create_users_table)
	@if [ -z "$(NAME)" ]; then \
		echo "Error: NAME is required. Usage: make migrate-create NAME=your_migration_name"; \
		exit 1; \
	fi
	@migrate create -ext sql -dir $(MIGRATIONS_DIR) -seq $(NAME)
	@echo "Migration files created in $(MIGRATIONS_DIR)!"

migrate-status: ## Show current migration version
	@echo "Current migration status for $(DB_PATH):"
	@go run cmd/migrate/main.go version

migrate-force: ## Recover a dirty migration state (interrupted migration). Prompts first; forces only the CURRENT dirty version, never the startup path.
	@go run cmd/migrate/main.go force

migrate-version: ## Show available migration files
	@echo "Available migration files:"
	@ls -1 $(MIGRATIONS_DIR)/*.sql 2>/dev/null || echo "No migration files found"

audit-backfill: ## Complete the tamper-evident audit hash chain (issue #381) for rows that predate migration 000034. The server does this on startup; this is for `make migrate-up`-only workflows.
	@echo "Backfilling the audit hash chain on $(DB_PATH)..."
	@go run cmd/audit-backfill/main.go

audit-verify: ## Verify the tamper-evident audit hash chain (issue #381); reports the first gap and exits non-zero on tampering.
	@go run cmd/audit-verify/main.go

gen-contract-fixtures: ## Regenerate the shared web+Android contract fixtures from backend/openapi.yaml's response examples (issue #266)
	@go run cmd/gencontract/main.go

gen-schema-fixtures: ## Regenerate the per-release schema dumps backing the upgrade test matrix (MIG-01, issue #436). Deterministic; run when a new release ships a new migration, then review the diff (existing dumps must be byte-identical).
	@go run cmd/genschema/main.go

gen-api-baseline: ## Regenerate the frozen /api/v1 contract baseline (MAINT-02, issue #491). Deliberate act: a baseline diff that removes or narrows surface IS the declared breaking change.
	@go run cmd/genapibaseline/main.go

gen-compat-matrix: ## Regenerate the DATA-01 field compatibility matrix from the correspondence oracle + issue #515 audit (issue #441). Run when correspondence.tsv changes; the drift test fails until the docs diff is committed.
	@go run cmd/gencompatmatrix/main.go

gen-integration-matrix: ## Regenerate the INT-01 integration classification matrix from backend/integrations (issue #464). Run when Registry()/Dispositions() change; the drift test fails until the docs diff is committed.
	@go run cmd/genintegrationmatrix/main.go

gen-perf-baseline: ## Regenerate the PERF-02 core-operation (issue #469) AND PERF-03 data-movement (issue #470) benchmark baselines + reports, plus the PERF-04 (issue #471) wallclock-trend.json. Measures smoke+typical (add -large for the large profile). The baseline drift tests fail until those diffs are committed; the trend + reports are not drift-gated. Regenerate when an operation's query/data-movement shape changes deliberately. NOTE: testdata/budgets.json is hand-authored and never regenerated here.
	@go run ./cmd/perfbench

backup: ## Create a consistent online snapshot of the SQLite database (VACUUM INTO; safe while the server runs)
	@echo "Backing up $(DB_PATH)..."
	@go run cmd/backup/main.go

backup-verify: ## Reconcile an assembled backup set (DB + PROFILE_PHOTO_DIR + ATTACHMENTS_DIR) — every attachment/photo row must resolve to a file (BACKUP-02, issue #454). Non-zero exit if a file is missing.
	@go run cmd/backupverify/main.go

# Version stamping. /health serves these so a bug report can be tied to the
# binary that produced it. Override APP_VERSION on a release build; the commit
# and date default from the working tree. These recipes always pass APP_COMMIT
# via -ldflags, so a bare `go build ./...` that bypasses this Makefile still
# reports a usable commit via Go's embedded VCS stamp (see buildinfo.Get), but
# the Makefile-built binary carries the -X value.
APP_VERSION ?= dev
APP_COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null)
APP_BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS = -X mycorrhizal/buildinfo.Version=$(APP_VERSION) \
          -X mycorrhizal/buildinfo.Commit=$(APP_COMMIT) \
          -X mycorrhizal/buildinfo.BuildDate=$(APP_BUILD_DATE)

# -trimpath drops the builder's absolute paths and -buildvcs=false keeps VCS
# state out, so with matching -X values a local build reproduces the release
# binary the Dockerfiles produce (REL-04, issue #448;
# docs/security/reproducible-builds.md).
GO_BUILD_FLAGS = -trimpath -buildvcs=false -ldflags "$(LDFLAGS)"

# Development helpers
dev: ## Build and run the server in development mode
	@go build $(GO_BUILD_FLAGS) -o mycorrhizal . && ./mycorrhizal

test: ## Run all tests
	@go test ./... -v

build: ## Build the application
	@go build $(GO_BUILD_FLAGS) -o mycorrhizal .

clean: ## Clean build artifacts
	@rm -f mycorrhizal
	@echo "Build artifacts cleaned!"
