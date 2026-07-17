.DEFAULT_GOAL := help

include make/install.mk
include make/docker.mk
include make/build.mk

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

.PHONY: help
help: ## Show all available commands
	@echo ""
	@echo "Available commands:"
	@echo ""

	@awk 'BEGIN {FS = ":.*##"} \
	/^[a-zA-Z0-9_.-]+:.*##/ { \
		printf "  $(CYAN)%-25s$(RESET) %s\n", $$1, $$2 \
	}' $(MAKEFILE_LIST)

	@echo ""
