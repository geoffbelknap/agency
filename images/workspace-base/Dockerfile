FROM debian:bookworm-slim@sha256:4724b8cc51e33e398f0e2e15e18d5ec2851ff0c2280647e1310bc1642182655d

LABEL org.opencontainers.image.source="https://github.com/geoffbelknap/agency"
LABEL org.opencontainers.image.description="Shared base image for Agency workspace containers"

# Stable base for the runtime workspace image. Keep package install and user
# creation here so release tags do not rebuild the expensive apt layer.
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl ca-certificates jq bash \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m -s /bin/bash agent

RUN mkdir -p /workspace /agency /agency/memory /agency/state && \
    chown agent:agent /workspace /agency/memory /agency/state
