.PHONY: \
	test \
	test-unit \
	test-integration \
	test-race \
	test-bench \
	test-cover \
	test-cover-html \
	test-cover-func \
	test-verbose \
	test-short \
	test-clean

GO               := go
COVERAGE_FILE    := coverage.out
COVERAGE_HTML    := coverage.html

test: ## Run all unit tests
	@printf "$(CYAN)▶ Running unit tests...$(RESET)\n"
	@$(GO) test ./...

test-unit: ## Run unit tests with no cache
	@printf "$(CYAN)▶ Running unit tests (no cache)...$(RESET)\n"
	@$(GO) test -count=1 ./...

test-short: ## Run short unit tests
	@printf "$(CYAN)▶ Running short tests...$(RESET)\n"
	@$(GO) test -short ./...

test-verbose: ## Run tests in verbose mode
	@printf "$(CYAN)▶ Running verbose tests...$(RESET)\n"
	@$(GO) test -v ./...

test-race: ## Run tests with race detector
	@printf "$(CYAN)▶ Running race detector...$(RESET)\n"
	@$(GO) test -race ./...

test-bench: ## Run benchmarks
	@printf "$(CYAN)▶ Running benchmarks...$(RESET)\n"
	@$(GO) test -bench=. -benchmem ./...

test-cover: ## Generate test coverage report
	@printf "$(CYAN)▶ Generating coverage report...$(RESET)\n"
	@$(GO) test \
		-covermode=atomic \
		-coverprofile=$(COVERAGE_FILE) \
		./...

test-cover-html: test-cover ## Open HTML coverage report
	@printf "$(CYAN)▶ Generating HTML coverage...$(RESET)\n"
	@$(GO) tool cover \
		-html=$(COVERAGE_FILE) \
		-o $(COVERAGE_HTML)

test-cover-func: test-cover ## Show coverage summary
	@$(GO) tool cover -func=$(COVERAGE_FILE)

test-integration: ## Run integration tests
	@printf "$(CYAN)▶ Running integration tests...$(RESET)\n"
	@$(GO) test \
		-tags=integration \
		-count=1 \
		./...

test-clean: ## Remove test artifacts
	@printf "$(YELLOW)▶ Cleaning test artifacts...$(RESET)\n"
	@rm -f $(COVERAGE_FILE) $(COVERAGE_HTML)
