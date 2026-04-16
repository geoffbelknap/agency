.PHONY: all build install deploy test clean images python-base workspace-base \
       body enforcer comms knowledge intake egress workspace web-fetch web relay \
       provider-tools-readiness \
       web-test-unit web-test-e2e web-test-all \
       e2e-live-web e2e-live-web-safe e2e-live-web-risky \
       e2e-live-web-disposable e2e-live-web-safe-disposable e2e-live-web-risky-disposable \
       e2e-live-web-danger e2e-live-web-danger-disposable

VERSION  ?= $(shell git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || echo 0.0.0)
COMMIT   := $(shell git rev-parse --short HEAD)
DATE     := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
DIRTY_HASH := $(shell (git diff --no-ext-diff --binary; git diff --cached --no-ext-diff --binary) | shasum -a 256 | cut -c1-12)
DIRTY_SUFFIX := $(shell git diff --quiet && git diff --cached --quiet || echo "-dirty.$(DIRTY_HASH)")
BUILD_ID := $(COMMIT)$(DIRTY_SUFFIX)
SOURCE_DIR := $(shell pwd)
LDFLAGS  := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE) -X main.buildID=$(BUILD_ID) -X main.sourceDir=$(SOURCE_DIR)
IMAGE_DIR = images

# Core images built by `make images`.
CORE_IMAGES = body enforcer comms knowledge intake egress workspace web-fetch gateway-proxy

# Services whose Dockerfile still needs the repo root as build context.
REPO_CONTEXT_IMAGES = intake

# Services that build from their own directory plus shared assets from images/.
SHARED_CONTEXT_IMAGES = body comms knowledge egress

# Build and install the gateway binary + all container images (including web UI)
all: install images-all
	@echo "Gateway installed, images built. Run 'agency serve' to start."

# Build the gateway binary
build:
	go build -ldflags "$(LDFLAGS)" -o agency ./cmd/gateway/

# Install the gateway binary where `agency` currently lives.
# Falls back to ~/.agency/bin/ for fresh installs.
# Refuses to overwrite Homebrew-managed binaries (use FORCE=1 to override).
AGENCY_BIN := $(shell which agency 2>/dev/null)
ifeq ($(AGENCY_BIN),)
  AGENCY_BIN := $(HOME)/.agency/bin/agency
endif

install: build
	@DEST="$(AGENCY_BIN)"; \
	case "$$DEST" in \
		*/homebrew/*|*/Homebrew/*|*/Cellar/*) \
			if [ "$(FORCE)" != "1" ]; then \
				echo "Error: agency is managed by Homebrew at $$DEST"; \
				echo "  To overwrite: make install FORCE=1"; \
				echo "  Or upgrade via: brew upgrade agency"; \
				exit 1; \
			fi ;; \
	esac; \
	mkdir -p "$$(dirname $$DEST)"; \
	agency serve stop 2>/dev/null || true; \
	sleep 1; \
	cp agency "$$DEST.new" && mv "$$DEST.new" "$$DEST"; \
	codesign -s - -f "$$DEST" 2>/dev/null || true; \
	"$$DEST" serve restart 2>/dev/null || true; \
	echo "Installed to $$DEST and restarted gateway"

# Build, install, and bring up infrastructure
deploy: all
	@echo "Starting infrastructure..."
	@$(HOME)/.agency/bin/agency infra up
	@echo "Deploy complete."

test:
	go test ./...

provider-tools-readiness:
	@./scripts/provider-tools-readiness-check.sh

clean:
	rm -f agency gateway

# Shared Python base image — prerequisite for Python service images
python-base:
	@echo "Building agency-python-base..."
	docker build -f $(IMAGE_DIR)/python-base/Dockerfile -t agency-python-base:latest $(IMAGE_DIR)/python-base

workspace-base:
	@echo "Building agency-workspace-base..."
	docker build -f $(IMAGE_DIR)/workspace-base/Dockerfile -t agency-workspace-base:latest $(IMAGE_DIR)/workspace-base

# Python service images depend on the shared base (egress excluded — uses mitmproxy)
body comms knowledge intake: python-base
workspace: workspace-base

# Build all container images (core only; use `make images-all` to include web)
images: $(CORE_IMAGES)

# Build all container images including web UI and relay
images-all: images web relay

# Per-image targets. Shared-context images use their own directory plus
# images/ as a named build context; repo-context images still use repo root.
define IMAGE_RULE
.PHONY: $(1)
$(1):
	@echo "Building agency-$(1)..."
	$$(if $$(filter $(1),$(SHARED_CONTEXT_IMAGES)),\
		docker build --build-context shared=$(IMAGE_DIR) --build-arg BUILD_ID=$(BUILD_ID) \
			-f $(IMAGE_DIR)/$(1)/Dockerfile -t agency-$(1):latest $(IMAGE_DIR)/$(1),\
	$$(if $$(filter $(1),$(REPO_CONTEXT_IMAGES)),\
		docker build --build-arg BUILD_ID=$(BUILD_ID) \
			-f $(IMAGE_DIR)/$(1)/Dockerfile -t agency-$(1):latest .,\
		docker build --build-arg BUILD_ID=$(BUILD_ID) \
			$$(if $$(filter workspace,$(1)),--build-arg WORKSPACE_BASE_IMAGE=agency-workspace-base:latest,) \
			-f $(IMAGE_DIR)/$(1)/Dockerfile -t agency-$(1):latest $(IMAGE_DIR)/$(1)))
endef

$(foreach img,$(CORE_IMAGES),$(eval $(call IMAGE_RULE,$(img))))

# agency-web source (monorepo)
AGENCY_WEB_DIR ?= $(SOURCE_DIR)/web
WEB_SOURCE_HASH := $(shell go run ./cmd/sourcehash web)

web:
	@echo "Building agency-web..."
	@if [ ! -d "$(AGENCY_WEB_DIR)" ]; then \
		echo "Error: agency-web not found at $(AGENCY_WEB_DIR)"; exit 1; \
	fi
	docker build --build-arg BUILD_ID=$(BUILD_ID) --build-arg SOURCE_HASH=$(WEB_SOURCE_HASH) \
		-f $(AGENCY_WEB_DIR)/Dockerfile -t agency-web:latest $(AGENCY_WEB_DIR)

# agency-relay source (sibling repo in workspace)
AGENCY_RELAY_DIR ?= $(SOURCE_DIR)/../agency-relay

relay:
	@echo "Building agency-relay..."
	@if [ ! -d "$(AGENCY_RELAY_DIR)" ]; then \
		echo "Error: agency-relay not found at $(AGENCY_RELAY_DIR)"; exit 1; \
	fi
	docker build --build-arg BUILD_ID=$(BUILD_ID) \
		-f $(AGENCY_RELAY_DIR)/Dockerfile -t agency-relay:latest $(AGENCY_RELAY_DIR)

e2e-live-web:
	@./scripts/e2e-live-web.sh

web-test-unit:
	@cd "$(AGENCY_WEB_DIR)" && npm test

web-test-e2e:
	@cd "$(AGENCY_WEB_DIR)" && npm run test:e2e

web-test-all: web-test-unit web-test-e2e

e2e-live-web-safe:
	@./scripts/e2e-live-web.sh tests/e2e-live

e2e-live-web-risky:
	@./scripts/e2e-live-web.sh --config playwright.live.risky.config.ts

e2e-live-web-disposable: e2e-live-web-safe-disposable

e2e-live-web-safe-disposable:
	@./scripts/e2e-live-disposable.sh --skip-build

e2e-live-web-risky-disposable:
	@./scripts/e2e-live-disposable.sh --skip-build --risky

e2e-live-web-danger:
	@./scripts/e2e-live-web.sh --allow-danger --danger-confirm destroy-all --config playwright.live.danger.config.ts

e2e-live-web-danger-disposable:
	@./scripts/e2e-live-danger-disposable.sh
