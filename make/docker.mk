.PHONY: \
	docker-up \
	docker-down \
	docker-up-pgsql \
	docker-down-pgsql \
	docker-restart-pgsql \
	docker-logs-pgsql \
	docker-shell-pgsql \
	docker-up-backend \
	docker-down-backend \
	docker-restart-backend \
	docker-logs-backend \
	docker-build-backend \
	docker-up-web-admin \
	docker-down-web-admin \
	docker-restart-web-admin \
	docker-logs-web-admin \
	docker-build-web-admin \
	docker-up-platform-admin \
	docker-down-platform-admin \
	docker-restart-platform-admin \
	docker-logs-platform-admin \
	docker-build-platform-admin \
	docker-ps \
	docker-clean

DOCKER_COMPOSE := docker compose
POSTGRES_SERVICE := vietgara-pgsql
BACKEND_SERVICE := vietgara-backend
WEB_OWNER_SERVICE := vietgara-owner-web
WEB_ADMIN_SERVICE := vietgara-admin-web

docker-up: ## Start PostgreSQL and backend services
	@printf "$(CYAN)▶ Starting VietGara services...$(RESET)\n"
	@$(DOCKER_COMPOSE) up -d

docker-down: ## Stop PostgreSQL and backend services
	@printf "\033[33m▶ Stopping VietGara services...$(RESET)\n"
	@$(DOCKER_COMPOSE) stop

docker-up-pgsql: ## Start PostgreSQL service
	@printf "$(CYAN)▶ Starting PostgreSQL...$(RESET)\n"
	@$(DOCKER_COMPOSE) up -d $(POSTGRES_SERVICE)

docker-down-pgsql: ## Stop PostgreSQL service
	@printf "\033[33m▶ Stopping PostgreSQL...$(RESET)\n"
	@$(DOCKER_COMPOSE) stop $(POSTGRES_SERVICE)

docker-restart-pgsql: ## Restart PostgreSQL service
	@printf "$(CYAN)▶ Restarting PostgreSQL...$(RESET)\n"
	@$(DOCKER_COMPOSE) restart $(POSTGRES_SERVICE)

docker-logs-pgsql: ## Follow PostgreSQL logs
	@$(DOCKER_COMPOSE) logs -f $(POSTGRES_SERVICE)

docker-shell-pgsql: ## Open psql shell
	@$(DOCKER_COMPOSE) exec $(POSTGRES_SERVICE) \
		psql -U $$POSTGRES_USER -d $$POSTGRES_DB

docker-up-backend: ## Start the backend service and its dependencies
	@printf "$(CYAN)▶ Starting backend...$(RESET)\n"
	@$(DOCKER_COMPOSE) up -d $(BACKEND_SERVICE)

docker-down-backend: ## Stop the backend service
	@printf "\033[33m▶ Stopping backend...$(RESET)\n"
	@$(DOCKER_COMPOSE) stop $(BACKEND_SERVICE)

docker-restart-backend: ## Restart the backend service
	@printf "$(CYAN)▶ Restarting backend...$(RESET)\n"
	@$(DOCKER_COMPOSE) restart $(BACKEND_SERVICE)

docker-logs-backend: ## Follow backend logs
	@$(DOCKER_COMPOSE) logs -f $(BACKEND_SERVICE)

docker-up-web-admin: ## Start the garage web admin
	@printf "$(CYAN)▶ Starting garage web admin...$(RESET)\n"
	@$(DOCKER_COMPOSE) up -d $(WEB_OWNER_SERVICE)

docker-down-web-admin: ## Stop the garage web admin
	@printf "\033[33m▶ Stopping garage web admin...$(RESET)\n"
	@$(DOCKER_COMPOSE) stop $(WEB_OWNER_SERVICE)

docker-restart-web-admin: ## Restart the garage web admin
	@printf "$(CYAN)▶ Restarting garage web admin...$(RESET)\n"
	@$(DOCKER_COMPOSE) restart $(WEB_OWNER_SERVICE)

docker-logs-web-admin: ## Follow garage web admin logs
	@$(DOCKER_COMPOSE) logs -f $(WEB_OWNER_SERVICE)

docker-build-web-admin: ## Build the garage web admin image
	@printf "$(CYAN)▶ Building garage web admin image...$(RESET)\n"
	@$(DOCKER_COMPOSE) build $(WEB_OWNER_SERVICE)

docker-up-platform-admin: ## Start the platform admin
	@printf "$(CYAN)▶ Starting platform admin...$(RESET)\n"
	@$(DOCKER_COMPOSE) up -d $(WEB_ADMIN_SERVICE)

docker-down-platform-admin: ## Stop the platform admin
	@printf "\033[33m▶ Stopping platform admin...$(RESET)\n"
	@$(DOCKER_COMPOSE) stop $(WEB_ADMIN_SERVICE)

docker-restart-platform-admin: ## Restart the platform admin
	@printf "$(CYAN)▶ Restarting platform admin...$(RESET)\n"
	@$(DOCKER_COMPOSE) restart $(WEB_ADMIN_SERVICE)

docker-logs-platform-admin: ## Follow platform admin logs
	@$(DOCKER_COMPOSE) logs -f $(WEB_ADMIN_SERVICE)

docker-build-platform-admin: ## Build the platform admin image
	@printf "$(CYAN)▶ Building platform admin image...$(RESET)\n"
	@$(DOCKER_COMPOSE) build $(WEB_ADMIN_SERVICE)

docker-build-backend: ## Build the backend image
	@printf "$(CYAN)▶ Building backend image...$(RESET)\n"
	@$(DOCKER_COMPOSE) build $(BACKEND_SERVICE)

docker-ps: ## Show running containers
	@$(DOCKER_COMPOSE) ps

docker-clean: ## Stop containers and remove volumes
	@printf "$(RED)⚠ Stop containers and remove volumes...$(RESET)\n"
	@$(DOCKER_COMPOSE) down -v
