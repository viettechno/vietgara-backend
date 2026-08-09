MOCKERY_VERSION := v3.7.2

.PHONY: \
	install-mockery \
	generate-mocks

install-mockery: ## Install mockery $(MOCKERY_VERSION)
	@installed_version="$$(mockery --version 2>/dev/null | grep -o 'v[0-9.]*' | head -1 || true)"; \
	if [ "$$installed_version" = "$(MOCKERY_VERSION)" ]; then \
		echo "$(BOLD)$(GREEN)mockery $(MOCKERY_VERSION) is already installed.$(RESET)"; \
	else \
		echo "$(BOLD)$(BLUE)Installing mockery $(MOCKERY_VERSION)...$(RESET)"; \
		go install github.com/vektra/mockery/v3@$(MOCKERY_VERSION); \
		echo "$(BOLD)$(GREEN)mockery $(MOCKERY_VERSION) installed.$(RESET)"; \
	fi

generate-mocks: install-mockery ## Regenerate mocks for internal/platform/db
	@printf "$(CYAN)▶ Generating mocks...$(RESET)\n"
	@mockery
