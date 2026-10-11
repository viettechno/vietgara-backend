.PHONY: \
	blackbox-stubbed \
	blackbox-stubbed-up \
	blackbox-stubbed-down \
	blackbox-staging \
	blackbox-seed \
	blackbox-test \
	blackbox-lint

# The blackbox suite is its own Go module (testing/blackbox): it can only
# use the public API contract, never internal/. See testing/blackbox/README.md.
export BLACKBOX_DIR		?= testing/blackbox
BLACKBOX_COMPOSE		:= docker compose -f $(BLACKBOX_DIR)/compose.yml

blackbox-stubbed: ## Start the stubbed stack, run the blackbox suite against it, stop it
	@$(MAKE) --no-print-directory blackbox-stubbed-up
	@status=0; \
	$(MAKE) --no-print-directory blackbox-seed || status=$$?; \
	if [ $$status -eq 0 ]; then $(MAKE) --no-print-directory blackbox-test || status=$$?; fi; \
	if [ $$status -ne 0 ]; then $(BLACKBOX_COMPOSE) logs --no-color --tail=200 vietgara-backend; fi; \
	$(MAKE) --no-print-directory blackbox-stubbed-down; \
	exit $$status

blackbox-stubbed-up: ## Build and start the stubbed stack (backend image + stubs), waiting until healthy
	@printf "$(CYAN)▶ Starting the stubbed blackbox stack...$(RESET)\n"
	@$(BLACKBOX_COMPOSE) up --build --detach --wait

blackbox-stubbed-down: ## Stop the stubbed stack and discard its data
	@printf "$(YELLOW)▶ Stopping the stubbed blackbox stack...$(RESET)\n"
	@$(BLACKBOX_COMPOSE) down --volumes --remove-orphans

blackbox-staging: ## Run the blackbox suite against staging (BLACKBOX_API_URL, optional BLACKBOX_OWNER_EMAIL/PASSWORD)
	@BLACKBOX_TARGET=staging $(MAKE) --no-print-directory blackbox-test

blackbox-seed: ## Seed supported shared owners and garages into the running stubbed stack
	@printf "$(CYAN)▶ Seeding the shared baseline in the stubbed stack...$(RESET)\n"
	@data_dir="$$(cd "$(TESTDATA_DIR)/data" && pwd)"; \
	cd "$(BLACKBOX_DIR)" && go run ./cmd/seed \
		--api-url "http://localhost:$${BLACKBOX_KONG_PORT:-18000}" \
		--mailpit-url "http://localhost:$${BLACKBOX_MAILPIT_PORT:-18025}" \
		--email-domain blackbox.vietgara.test \
		--data-dir "$$data_dir"

blackbox-test: ## Run the blackbox suite against a running target (BLACKBOX_TARGET, default stubbed)
	@printf "$(CYAN)▶ Running blackbox tests against $${BLACKBOX_TARGET:-stubbed}...$(RESET)\n"
	@data_dir="$$(cd "$(TESTDATA_DIR)/data" && pwd)"; \
	cd $(BLACKBOX_DIR) && BLACKBOX_TESTDATA_DIR="$$data_dir" go test -count=1 -v ./...

blackbox-lint: ## Lint the blackbox module with the repository's golangci-lint config
	@printf "$(CYAN)▶ Running golangci-lint on $(BLACKBOX_DIR)...$(RESET)\n"
	@cd $(BLACKBOX_DIR) && $(GOLANGCI_LINT) run --config ../../.golangci.yml ./...
