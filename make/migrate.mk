# Database migrations (db/migrations) with the golang-migrate CLI, pinned to
# GOLANG_MIGRATE_VERSION and run through `go run` so nothing needs installing.
# The database is MIGRATION_DATABASE_URL (the schema owner), read from the
# environment or .env; override with DATABASE=<url>.

MIGRATIONS_DIR ?= db/migrations
MIGRATE_CLI    := go run -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@$(GOLANG_MIGRATE_VERSION)

# Loads .env (if present) so MIGRATION_DATABASE_URL is available, then runs migrate.
define run_migrate
	@set -a; [ -f .env ] && . ./.env; set +a; \
	url="$${DATABASE:-$${MIGRATION_DATABASE_URL:-$$DATABASE_URL}}"; \
	if [ -z "$$url" ]; then \
		printf "$(RED)Set MIGRATION_DATABASE_URL (or DATABASE=<url>).$(RESET)\n"; exit 1; \
	fi; \
	$(MIGRATE_CLI) -path $(MIGRATIONS_DIR) -database "$$url" $(1)
endef

.PHONY: \
	migrate-up \
	migrate-down \
	migrate-force \
	migrate-version

migrate-up: ## Apply pending migrations (all, or N=<steps>)
	@printf "$(CYAN)▶ Migrating up$(if $(N), $(N) step(s))...$(RESET)\n"
	$(call run_migrate,up $(N))

migrate-down: ## Roll back N=<steps> migrations (default 1; N=all rolls back everything)
	@printf "$(CYAN)▶ Migrating down $(or $(N),1) step(s)...$(RESET)\n"
	$(call run_migrate,down $(if $(filter all,$(N)),-all,$(or $(N),1)))

migrate-force: ## Mark VERSION=<n> as applied and clean (fixes a dirty state; runs no SQL)
	@if [ -z "$(VERSION)" ]; then printf "$(RED)Usage: make migrate-force VERSION=<n>$(RESET)\n"; exit 1; fi
	@printf "$(YELLOW)▶ Forcing migration version $(VERSION)...$(RESET)\n"
	$(call run_migrate,force $(VERSION))

migrate-version: ## Show the current migration version (and whether it is dirty)
	$(call run_migrate,version)
