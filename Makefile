.PHONY: all build install deploy test clean images \
       body enforcer comms knowledge intake egress workspace web-fetch web

VERSION  ?= 0.1.0
COMMIT   := $(shell git rev-parse --short HEAD)
DATE     := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
DIRTY    := $(shell git diff --quiet && git diff --cached --quiet || echo "-dirty")
BUILD_ID := $(COMMIT)$(DIRTY)
SOURCE_DIR := $(shell pwd)
LDFLAGS  := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE) -X main.buildID=$(BUILD_ID) -X main.sourceDir=$(SOURCE_DIR)
IMAGE_DIR = images

# Core images built by `make images`.
CORE_IMAGES = body enforcer comms knowledge intake egress workspace web-fetch

# Services whose Dockerfile needs the repo root as build context
# (they COPY images/models/ for shared Pydantic schemas).
REPO_CONTEXT_IMAGES = comms knowledge intake

# Build and install the gateway binary + all container images
all: install images
	@echo "Gateway installed, images built. Run 'agency serve' to start."

# Build the gateway binary
build:
	go build -ldflags "$(LDFLAGS)" -o agency ./cmd/gateway/

# Install the gateway binary to ~/.agency/bin/
install: build
	mkdir -p ~/.agency/bin
	@-~/.agency/bin/agency serve stop 2>/dev/null
	@sleep 1
	cp agency ~/.agency/bin/agency.new && mv ~/.agency/bin/agency.new ~/.agency/bin/agency
	-codesign -s - -f ~/.agency/bin/agency 2>/dev/null
	@~/.agency/bin/agency serve restart 2>/dev/null || true
	@echo "Installed and restarted gateway"

# Build, install, and bring up infrastructure
deploy: install
	@echo "Starting infrastructure..."
	@~/.agency/bin/agency infra up
	@echo "Deploy complete."

test:
	go test ./...

clean:
	rm -f agency

# Build all container images
images: $(CORE_IMAGES)

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
