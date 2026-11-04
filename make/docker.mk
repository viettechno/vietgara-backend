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
	docker-ps \
	docker-clean \
	docker-full-up \
	docker-full-down \
	docker-full-ps \
	docker-full-logs

DOCKER_COMPOSE := docker compose
POSTGRES_SERVICE := vietgara-pgsql
BACKEND_SERVICE := vietgara-backend

docker-up: ## Start the backend and what it needs (Postgres, Redis, Kafka, Mailpit)
	@printf "$(CYAN)▶ Starting VietGara services...$(RESET)\n"
	@$(DOCKER_COMPOSE) up -d

docker-down: ## Stop the backend and what it needs
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

docker-build-backend: ## Build the backend image
	@printf "$(CYAN)▶ Building backend image...$(RESET)\n"
	@$(DOCKER_COMPOSE) build $(BACKEND_SERVICE)

docker-ps: ## Show running containers
	@$(DOCKER_COMPOSE) ps

docker-clean: ## Stop containers and remove volumes
	@printf "$(RED)⚠ Stop containers and remove volumes...$(RESET)\n"
	@$(DOCKER_COMPOSE) down -v

# The whole application (the web apps and Kong in front of this backend) is the
# vietgara-infra/local stack, which adds to docker-compose.yml.
INFRA_LOCAL ?= ../vietgara-infra/local

docker-full-up: ## Start the whole application (backend, web apps, Kong) from vietgara-infra/local
	@$(MAKE) -C $(INFRA_LOCAL) up

docker-full-down: ## Stop the whole application
	@$(MAKE) -C $(INFRA_LOCAL) down

docker-full-ps: ## Show the containers of the whole application
	@$(MAKE) -C $(INFRA_LOCAL) ps

docker-full-logs: ## Follow the logs of the whole application (SERVICE=vietgara-owner-web for one)
	@$(MAKE) -C $(INFRA_LOCAL) logs SERVICE=$(SERVICE)
