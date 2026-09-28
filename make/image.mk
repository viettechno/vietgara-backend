.PHONY: image-check image-build image-push image-tag

# GitHub Container Registry image of the backend. Override on the command
# line, e.g. make image-push IMAGE_NAMESPACE=myorg IMAGE_TAG=v1.1.0
IMAGE_REGISTRY ?= ghcr.io
IMAGE_NAMESPACE ?= viettechno
IMAGE_NAME ?= $(IMAGE_REGISTRY)/$(IMAGE_NAMESPACE)/vietgara-backend
# Links the GHCR package to its repository (permissions, README, "Packages" tab).
IMAGE_SOURCE ?= https://github.com/$(IMAGE_NAMESPACE)/vietgara-backend
IMAGE_TAG ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# The staging VM is x86_64; the image is cross-compiled from any machine.
IMAGE_PLATFORM ?= linux/amd64

image-tag: ## Print the image reference that image-build/image-push use
	@echo "$(IMAGE_NAME):$(IMAGE_TAG)"

image-check: ## Check the backend builds without go.work (as it does in Docker)
	@printf "$(CYAN)▶ Building without go.work against the vietgara-proto version in go.mod...$(RESET)\n"
	@GOWORK=off go build -o /dev/null ./cmd/server || { \
		printf "$(RED)✗ The backend does not build with the vietgara-proto version pinned in go.mod.$(RESET)\n"; \
		printf "  Push and tag vietgara-proto, then: go get github.com/viettechno/vietgara-proto@<tag> && go mod tidy\n"; \
		exit 1; }

image-build: image-check ## Build the backend image locally for IMAGE_PLATFORM
	@printf "$(CYAN)▶ Building $(IMAGE_NAME):$(IMAGE_TAG) ($(IMAGE_PLATFORM))...$(RESET)\n"
	@docker buildx build --platform $(IMAGE_PLATFORM) --label org.opencontainers.image.source=$(IMAGE_SOURCE) -t $(IMAGE_NAME):$(IMAGE_TAG) --load .

image-push: image-check ## Build and push the backend image to GHCR (docker login ghcr.io first)
	@case "$(IMAGE_TAG)" in *-dirty) if [ "$(ALLOW_DIRTY)" != "1" ]; then \
		printf "$(RED)✗ Uncommitted changes: tag $(IMAGE_TAG) cannot be traced to a commit.$(RESET)\n"; \
		printf "  Commit first, or pass ALLOW_DIRTY=1 for a throw-away image.\n"; exit 1; fi;; esac
	@printf "$(CYAN)▶ Building and pushing $(IMAGE_NAME):$(IMAGE_TAG) ($(IMAGE_PLATFORM))...$(RESET)\n"
	@docker buildx build --platform $(IMAGE_PLATFORM) --label org.opencontainers.image.source=$(IMAGE_SOURCE) -t $(IMAGE_NAME):$(IMAGE_TAG) --push .
	@printf "$(GREEN)✓ Pushed $(IMAGE_NAME):$(IMAGE_TAG)$(RESET)\n"
	@printf "  On the staging VM: make deploy BACKEND_TAG=$(IMAGE_TAG)\n"
