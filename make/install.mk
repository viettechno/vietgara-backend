GOLANG_MIGRATE_VERSION     := v4.19.1
TESTCONTAINERS_GO_VERSION  := v0.43.0
LIB_PQ_VERSION             := v1.12.3
GOLANGCI_LINT_VERSION      := 2.12.2

.PHONY: \
	install-deps \
	install-golang-migrate \
	install-testcontainers-go \
	install-lib-pq \
	install-golangci-lint \
	install-gotestfmt

install-deps: install-golang-migrate install-testcontainers-go install-lib-pq install-golangci-lint ## Install pinned Go dependencies
	@echo "$(BOLD)$(BLUE)Synchronizing Go module files...$(RESET)"
	@go mod tidy
	@echo "$(BOLD)$(GREEN)Go dependencies are installed.$(RESET)"

install-golang-migrate: ## Install golang-migrate $(GOLANG_MIGRATE_VERSION)
	@installed_version="$$(go list -m -f '{{.Version}}' github.com/golang-migrate/migrate/v4 2>/dev/null || true)"; \
	if [ "$$installed_version" = "$(GOLANG_MIGRATE_VERSION)" ]; then \
		echo "$(BOLD)$(GREEN)golang-migrate $(GOLANG_MIGRATE_VERSION) is already installed.$(RESET)"; \
	else \
		echo "$(BOLD)$(BLUE)Installing golang-migrate $(GOLANG_MIGRATE_VERSION)...$(RESET)"; \
		go get github.com/golang-migrate/migrate/v4@$(GOLANG_MIGRATE_VERSION); \
		echo "$(BOLD)$(GREEN)golang-migrate $(GOLANG_MIGRATE_VERSION) installed.$(RESET)"; \
	fi

install-testcontainers-go: ## Install testcontainers-go $(TESTCONTAINERS_GO_VERSION)
	@installed_version="$$(go list -m -f '{{.Version}}' github.com/testcontainers/testcontainers-go 2>/dev/null || true)"; \
	if [ "$$installed_version" = "$(TESTCONTAINERS_GO_VERSION)" ]; then \
		echo "$(BOLD)$(GREEN)testcontainers-go $(TESTCONTAINERS_GO_VERSION) is already installed.$(RESET)"; \
	else \
		echo "$(BOLD)$(BLUE)Installing testcontainers-go $(TESTCONTAINERS_GO_VERSION)...$(RESET)"; \
		go get github.com/testcontainers/testcontainers-go@$(TESTCONTAINERS_GO_VERSION); \
		echo "$(BOLD)$(GREEN)testcontainers-go $(TESTCONTAINERS_GO_VERSION) installed.$(RESET)"; \
	fi

install-lib-pq: ## Install lib/pq $(LIB_PQ_VERSION)
	@installed_version="$$(go list -m -f '{{.Version}}' github.com/lib/pq 2>/dev/null || true)"; \
	if [ "$$installed_version" = "$(LIB_PQ_VERSION)" ]; then \
		echo "$(BOLD)$(GREEN)lib/pq $(LIB_PQ_VERSION) is already installed.$(RESET)"; \
	else \
		echo "$(BOLD)$(BLUE)Installing lib/pq $(LIB_PQ_VERSION)...$(RESET)"; \
		go get github.com/lib/pq@$(LIB_PQ_VERSION); \
		echo "$(BOLD)$(GREEN)lib/pq $(LIB_PQ_VERSION) installed.$(RESET)"; \
	fi

install-golangci-lint: ## Install golangci-lint v$(GOLANGCI_LINT_VERSION)
	@installed_version="$$(golangci-lint version --short 2>/dev/null || true)"; \
	if [ "$$installed_version" = "$(GOLANGCI_LINT_VERSION)" ]; then \
		echo "$(BOLD)$(GREEN)golangci-lint v$(GOLANGCI_LINT_VERSION) is already installed.$(RESET)"; \
	else \
		echo "$(BOLD)$(BLUE)Installing golangci-lint v$(GOLANGCI_LINT_VERSION)...$(RESET)"; \
		go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_VERSION); \
		echo "$(BOLD)$(GREEN)golangci-lint v$(GOLANGCI_LINT_VERSION) installed.$(RESET)"; \
	fi

install-gotestfmt: ## Install gotestfmt
	go install github.com/gotesttools/gotestfmt/v2/cmd/gotestfmt@latest
