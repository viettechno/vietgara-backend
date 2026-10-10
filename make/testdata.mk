TESTDATA_REPOSITORY := https://github.com/viettechno/vietgara-testdata.git
TESTDATA_VERSION_FILE ?= testing/blackbox/testdata.version
TESTDATA_DIR ?= .data/vietgara-testdata

blackbox-test: testdata

.PHONY: testdata

testdata: ## Fetch the pinned shared test dataset into .data/vietgara-testdata
	@set -eu; \
	version="$$(tr -d '\r\n' < "$(TESTDATA_VERSION_FILE)")"; \
	if ! printf '%s\n' "$$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$$'; then \
		printf 'Invalid testdata version in %s: %s\n' "$(TESTDATA_VERSION_FILE)" "$$version" >&2; \
		exit 1; \
	fi; \
	if [ -e "$(TESTDATA_DIR)" ]; then \
		if [ ! -d "$(TESTDATA_DIR)/.git" ]; then \
			printf 'Testdata path exists but is not a Git checkout: %s\n' "$(TESTDATA_DIR)" >&2; \
			exit 1; \
		fi; \
		remote="$$(git -C "$(TESTDATA_DIR)" remote get-url origin)"; \
		case "$$remote" in \
			"$(TESTDATA_REPOSITORY)"|git@github.com:viettechno/vietgara-testdata.git|ssh://git@github.com/viettechno/vietgara-testdata.git) ;; \
			*) \
			printf 'Unexpected testdata origin at %s: %s\n' "$(TESTDATA_DIR)" "$$remote" >&2; \
			exit 1; \
			;; \
		esac; \
		if [ -n "$$(git -C "$(TESTDATA_DIR)" status --porcelain)" ]; then \
			printf 'Testdata checkout has local changes; refusing to update %s\n' "$(TESTDATA_DIR)" >&2; \
			exit 1; \
		fi; \
		git -C "$(TESTDATA_DIR)" fetch --depth 1 origin "refs/tags/$$version:refs/tags/$$version"; \
		git -C "$(TESTDATA_DIR)" checkout --detach "$$version"; \
	else \
		mkdir -p "$$(dirname "$(TESTDATA_DIR)")"; \
		git clone --depth 1 --branch "$$version" "$(TESTDATA_REPOSITORY)" "$(TESTDATA_DIR)"; \
	fi
