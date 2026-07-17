.PHONY: \
	install-deps \
	install-golang-migrate \
	install-testcontainers-go \
	install-lib-pq \
	install-golangci-lint

install-deps: install-golang-migrate install-testcontainers-go install-lib-pq install-golangci-lint ## Install Go dependencies
	@echo "$(BOLD)$(BLUE)Installing Go dependencies...$(RESET)"
	@go mod tidy
	@echo "$(BOLD)$(GREEN)Go dependencies installed.$(RESET)"

install-golang-migrate: ## Install golang-migrate
	@echo "$(BOLD)$(BLUE)Installing golang-migrate...$(RESET)"
	@go get -u github.com/golang-migrate/migrate/v4
	@echo "$(BOLD)$(GREEN)golang-migrate installed.$(RESET)"

install-testcontainers-go: ## Install testcontainers-go
	@echo "$(BOLD)$(BLUE)Installing testcontainers-go...$(RESET)"
	@go get -u github.com/testcontainers/testcontainers-go
	@echo "$(BOLD)$(GREEN)testcontainers-go installed.$(RESET)"

install-lib-pq: ## Install lib/pq
	@echo "$(BOLD)$(BLUE)Installing lib/pq...$(RESET)"
	@go get -u github.com/lib/pq
	@echo "$(BOLD)$(GREEN)lib/pq installed.$(RESET)"

install-golangci-lint: ## Install golangci-lint
	@echo "$(BOLD)$(BLUE)Installing golangci-lint...$(RESET)"
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "$(BOLD)$(GREEN)golangci-lint installed.$(RESET)"
