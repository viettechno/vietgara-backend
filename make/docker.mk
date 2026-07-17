.PHONY: \
	docker-up-pgsql \
	docker-down-pgsql \
	docker-restart-pgsql \
	docker-logs-pgsql \
	docker-shell-pgsql \
	docker-ps \
	docker-clean

DOCKER_COMPOSE := docker compose
POSTGRES_SERVICE := vietgara-pgsql

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

docker-ps: ## Show running containers
	@$(DOCKER_COMPOSE) ps

docker-clean: ## Stop containers and remove volumes
	@printf "$(RED)⚠ Stop containers and remove volumes...$(RESET)\n"
	@$(DOCKER_COMPOSE) down -v
