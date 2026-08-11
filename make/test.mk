.PHONY: \
	test-unit \
	test-unit-html \
	test-unit-func \
	test-short \
	test-integration \
	test-race \
	test-bench \
	test-clean

export BUILD_DIR		?= ./out
export COVERAGE			?= 90
export COVERAGE_FILE	?= $(BUILD_DIR)/coverage.out
export COVERAGE_HTML	?= $(BUILD_DIR)/coverage.html
export COVERAGE_JSON	?= $(BUILD_DIR)/coverage.json
export COVERAGE_OUT		?= $(BUILD_DIR)/toolcover.out
export TEST_PKGS		?= $(shell go list ./internal/... ./pkg/... 2>/dev/null | grep -v './internal/mocks$$' || true)

define CHECK_COVERAGE
	{ \
		printf "$(CYAN)▶ Checking coverage...$(RESET)\n"; \
		awk -v min=$(COVERAGE) 'BEGIN { \
		coverage = 0; \
	} \
	/total:/ { \
		coverage = substr($$3, 1, length($$3)-1); \
		if (coverage < min) { \
			printf "$(RED)Coverage is below minimum threshold: %s%% < %s%%$(RESET)\n", coverage, min; \
			exit 1; \
		} else { \
			printf "$(GREEN)Coverage is above minimum threshold: %s%% >= %s%%$(RESET)\n", coverage, min; \
		} \
	}'; \
	}
endef

test-unit: ## Run unit tests with no cache
	@mkdir -p $(BUILD_DIR)
	@printf "$(CYAN)▶ Running unit tests (no cache)...$(RESET)\n"
	@go test -count=1 -v -covermode=atomic -coverprofile=$(COVERAGE_FILE) $(TEST_PKGS)
	@go tool cover -func=$(COVERAGE_FILE) | $(CHECK_COVERAGE)

test-short: ## Run short unit tests
	@mkdir -p $(BUILD_DIR)
	@printf "$(CYAN)▶ Running short tests...$(RESET)\n"
	@go test -short -covermode=atomic -coverprofile=$(COVERAGE_FILE) $(TEST_PKGS)

test-race: ## Run tests with race detector
	@printf "$(CYAN)▶ Running race detector...$(RESET)\n"
	@go test -race ./...

test-bench: ## Run benchmarks
	@printf "$(CYAN)▶ Running benchmarks...$(RESET)\n"
	@go test -bench=. -benchmem ./...

test-unit-html: test-unit ## Run unit test and generate HTML coverage report
	@printf "$(CYAN)▶ Generating HTML coverage...$(RESET)\n"
	@go tool cover \
		-html=$(COVERAGE_FILE) \
		-o $(COVERAGE_HTML)

test-unit-json: ## Run unit test and generate JSON coverage report
	@mkdir -p $(BUILD_DIR)
	@printf "$(CYAN)▶ Running unit tests (no cache)...$(RESET)\n"
	@go test -count=1 -v -covermode=atomic -coverprofile=$(COVERAGE_FILE) -json $(TEST_PKGS) | tee $(COVERAGE_JSON) | gotestfmt -hide successful-tests
	@go tool cover -func=$(COVERAGE_FILE) | tee $(COVERAGE_OUT) | $(CHECK_COVERAGE)

	@printf "$(CYAN)▶ Generating JSON coverage...$(RESET)\n"
	@go tool cover \
		-json=$(COVERAGE_FILE) \
		-o $(COVERAGE_JSON)

test-cover-func: test-cover ## Show coverage summary
	@go tool cover -func=$(COVERAGE_FILE)

test-integration: ## Run integration tests
	@printf "$(CYAN)▶ Running integration tests...$(RESET)\n"
	@go test \
		-tags=integration \
		-count=1 \
		./...

test-clean: ## Remove test artifacts
	@printf "$(YELLOW)▶ Cleaning test artifacts...$(RESET)\n"
	@rm -f $(COVERAGE_FILE) $(COVERAGE_HTML) $(COVERAGE_JSON) $(COVERAGE_OUT)
