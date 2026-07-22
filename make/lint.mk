.PHONY: \
	lint \
	lint-fix \
	fmt

GOLANGCI_LINT := golangci-lint

lint: ## Run golangci-lint
	@printf "$(CYAN)▶ Running golangci-lint...$(RESET)\n"
	@$(GOLANGCI_LINT) run ./...

lint-fix: ## Run golangci-lint and apply automatic fixes
	@printf "$(CYAN)▶ Running golangci-lint with automatic fixes...$(RESET)\n"
	@$(GOLANGCI_LINT) run --fix ./...

fmt: ## Format Go code with golangci-lint
	@printf "$(CYAN)▶ Formatting Go code...$(RESET)\n"
	@$(GOLANGCI_LINT) fmt ./...
