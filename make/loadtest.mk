.PHONY: \
	loadtest-seed \
	loadtest-run \
	loadtest-report \
	loadtest-stubbed \
	loadtest-staging

# The Release 9.0 load test (NFR-PF-06). See testing/loadtest/README.md.
export LOADTEST_DIR		?= testing/loadtest
LOADTEST_PROFILE		?=
# Names the results directory (<stamp>-<target>): the date, or a label such as 2026-10-27-headroom.
LOADTEST_STAMP			?= $(shell date +%Y-%m-%d)
K6						?= k6

loadtest-seed: testdata ## Create the load-test population (BLACKBOX_TARGET stubbed|staging; writes testing/loadtest/population.json)
	@printf "$(CYAN)▶ Seeding the load-test population on $${BLACKBOX_TARGET:-stubbed}...$(RESET)\n"
	@data_dir="$$(cd "$(TESTDATA_DIR)/data" && pwd)"; \
	target="$${BLACKBOX_TARGET:-stubbed}"; \
	if [ "$$target" = staging ]; then \
		api="$${BLACKBOX_API_URL:?set BLACKBOX_API_URL}"; mail="$${BLACKBOX_MAILPIT_URL:?set BLACKBOX_MAILPIT_URL}"; domain="$${BLACKBOX_EMAIL_DOMAIN:?set BLACKBOX_EMAIL_DOMAIN}"; \
	else \
		api="http://localhost:$${BLACKBOX_KONG_PORT:-18000}"; mail="http://localhost:$${BLACKBOX_MAILPIT_PORT:-18025}"; domain=blackbox.vietgara.test; \
	fi; \
	cd $(BLACKBOX_DIR) && go run ./cmd/loadseed --target "$$target" --api-url "$$api" --mailpit-url "$$mail" \
		--email-domain "$$domain" --data-dir "$$data_dir" --profile "$(LOADTEST_PROFILE)" --out ../loadtest/population.json

loadtest-run: ## Run the k6 flow against a seeded environment; results go to testing/loadtest/results/<date>-<target>/
	@command -v $(K6) >/dev/null || { printf "$(RED)k6 is not installed (brew install k6, or see testing/loadtest/README.md)$(RESET)\n" >&2; exit 1; }
	@target="$${BLACKBOX_TARGET:-stubbed}"; dir="$(LOADTEST_DIR)/results/$(LOADTEST_STAMP)-$$target"; mkdir -p "$$dir"; \
	if [ "$$target" = staging ]; then api="$${BLACKBOX_API_URL:?set BLACKBOX_API_URL}"; password="$${TEST_PASSWORD:?set TEST_PASSWORD}"; forward=0; \
	else api="http://localhost:$${BLACKBOX_KONG_PORT:-18000}"; password="$$(sed -n 's/^ *password: *"\(.*\)"/\1/p' $(TESTDATA_DIR)/data/environments.yaml | head -1)"; forward=1; fi; \
	{ echo "date: $$(date -u +%Y-%m-%dT%H:%M:%SZ)"; echo "target: $$target"; echo "api: $$api"; \
	  echo "backend: $$(git describe --tags --always --dirty)"; echo "testdata: $$(cat $(TESTDATA_VERSION_FILE))"; \
	  echo "k6: $$($(K6) version)"; echo "host: $$(uname -srm), $$(getconf _NPROCESSORS_ONLN 2>/dev/null) cpus"; \
	  echo "outbox_relay_interval: $${BLACKBOX_OUTBOX_RELAY_INTERVAL:-n/a}"; \
	} > "$$dir/run.txt"; \
	date -u +%Y-%m-%dT%H:%M:%SZ > "$$dir/.started"; \
	API_URL="$$api" LOAD_PASSWORD="$$password" LOAD_FORWARD_IPS=$$forward POPULATION=$(CURDIR)/$(LOADTEST_DIR)/population.json \
		RESULTS_DIR="$$dir" $(K6) run --quiet $(LOADTEST_DIR)/flow.js

loadtest-report: ## Event latency (NFR-PF-05) and table scans of the stubbed run LOADTEST_STAMP, read from its database
	@dir="$(LOADTEST_DIR)/results/$(LOADTEST_STAMP)-stubbed"; since="$$(cat $$dir/.started)"; \
	{ echo "Event latency since $$since (published = the relay marked it published; created = the business change wrote it)"; \
	  $(BLACKBOX_COMPOSE) exec -T vietgara-pgsql psql -U vietgara -d vietgara -v since="$$since" -f - < $(LOADTEST_DIR)/events.sql; \
	  echo; echo "Database after the run"; \
	  $(BLACKBOX_COMPOSE) exec -T vietgara-pgsql psql -U vietgara -d vietgara -f - < $(LOADTEST_DIR)/db.sql; \
	} | tee "$$dir/database.txt"

loadtest-stubbed: ## Start the stubbed stack (staging's 1s outbox relay), seed, run, report event latency, stop
	@BLACKBOX_OUTBOX_RELAY_INTERVAL=1s $(MAKE) --no-print-directory blackbox-stubbed-up
	@status=0; \
	BLACKBOX_OUTBOX_RELAY_INTERVAL=1s $(MAKE) --no-print-directory loadtest-seed || status=$$?; \
	if [ $$status -eq 0 ]; then BLACKBOX_OUTBOX_RELAY_INTERVAL=1s $(MAKE) --no-print-directory loadtest-run || status=$$?; fi; \
	$(MAKE) --no-print-directory loadtest-report || true; \
	if [ $$status -ne 0 ]; then $(BLACKBOX_COMPOSE) logs --no-color --tail=100 vietgara-backend; fi; \
	$(MAKE) --no-print-directory blackbox-stubbed-down; \
	exit $$status

loadtest-staging: ## Seed and run against staging (BLACKBOX_API_URL, BLACKBOX_MAILPIT_URL, BLACKBOX_EMAIL_DOMAIN, TEST_PASSWORD, ADMIN_PASSWORD)
	@BLACKBOX_TARGET=staging $(MAKE) --no-print-directory loadtest-seed
	@BLACKBOX_TARGET=staging $(MAKE) --no-print-directory loadtest-run
