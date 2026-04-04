.PHONY: all build install deploy test clean images \
       body enforcer comms knowledge intake egress workspace web-fetch web

VERSION  ?= $(shell git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || echo 0.0.0)
COMMIT   := $(shell git rev-parse --short HEAD)
DATE     := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
DIRTY    := $(shell git diff --quiet && git diff --cached --quiet || echo "-dirty")
BUILD_ID := $(COMMIT)$(DIRTY)
SOURCE_DIR := $(shell pwd)
LDFLAGS  := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE) -X main.buildID=$(BUILD_ID) -X main.sourceDir=$(SOURCE_DIR)
IMAGE_DIR = images

# Core images built by `make images`.
CORE_IMAGES = body enforcer comms knowledge intake egress workspace web-fetch gateway-proxy

# Services whose Dockerfile needs the repo root as build context
# (they COPY images/models/ for shared Pydantic schemas).
REPO_CONTEXT_IMAGES = comms knowledge intake

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

clean:
	rm -f agency gateway

# Build all container images (core only; use `make images-all` to include web)
images: $(CORE_IMAGES)

# Build all container images including web UI
images-all: images web

# Per-image targets. Repo-context images use repo root as context;
# self-contained images use their own directory.
define IMAGE_RULE
.PHONY: $(1)
$(1):
	@echo "Building agency-$(1)..."
	$$(if $$(filter $(1),$(REPO_CONTEXT_IMAGES)),\
		docker build --build-arg BUILD_ID=$(BUILD_ID) --build-arg CACHE_BUST=$$$$(date +%s) \
			-f $(IMAGE_DIR)/$(1)/Dockerfile -t agency-$(1):latest .,\
		docker build --build-arg BUILD_ID=$(BUILD_ID) --build-arg CACHE_BUST=$$$$(date +%s) \
			-f $(IMAGE_DIR)/$(1)/Dockerfile -t agency-$(1):latest $(IMAGE_DIR)/$(1))
endef

$(foreach img,$(CORE_IMAGES),$(eval $(call IMAGE_RULE,$(img))))

# agency-web lives in the workspace (../agency-web)
AGENCY_WEB_DIR ?= $(shell cd .. && pwd)/agency-web

web:
	@echo "Building agency-web..."
	@if [ ! -d "$(AGENCY_WEB_DIR)" ]; then \
		echo "Error: agency-web not found at $(AGENCY_WEB_DIR)"; exit 1; \
	fi
	docker build --build-arg BUILD_ID=$(BUILD_ID) \
		-f $(AGENCY_WEB_DIR)/Dockerfile -t agency-web:latest $(AGENCY_WEB_DIR)
