.PHONY: build clean

build: ## Build the application
	go build ./...

clean: ## Remove build artifacts
	rm -rf bin/
