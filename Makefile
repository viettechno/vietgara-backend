.DEFAULT_GOAL := help

include make/install.mk
include make/test.mk
include make/lint.mk
include make/docker.mk
include make/image.mk
include make/build.mk
include make/mock.mk
include make/migrate.mk
include make/blackbox.mk
include make/testdata.mk

# Styles
BOLD 	:= \033[1m
# Colors
RESET	:= \033[0m
RED		:= \033[31m
GREEN	:= \033[32m
YELLOW	:= \033[33m
BLUE	:= \033[34m
MAGENTA	:= \033[35m
CYAN	:= \033[36m
WHITE	:= \033[37m
# Background Colors
BG_RED	:= \033[41m
BG_GREEN:= \033[42m
BG_BLUE	:= \033[44m

.PHONY: help run
help: ## Show all available commands
	@echo ""
	@echo "Available commands:"
	@echo ""

	@awk 'BEGIN {FS = ":.*##"} \
	/^[a-zA-Z0-9_.-]+:.*##/ { \
		printf "  $(CYAN)%-32s$(RESET) %s\n", $$1, $$2 \
	}' $(MAKEFILE_LIST)

	@echo ""

run: ## Run the gRPC server locally (loads .env when present)
	@set -a; [ -f .env ] && . ./.env; set +a; go run ./cmd/server
