FROM debian:bookworm-slim@sha256:4724b8cc51e33e398f0e2e15e18d5ec2851ff0c2280647e1310bc1642182655d
ARG BUILD_ID=unknown
ARG SOURCE_HASH=unknown

# Minimal agent workspace — curl, jq, bash only.
# Execution-layer confinement via custom seccomp profile on workspace container.
# Agents install tools at runtime through mediated egress.

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl ca-certificates jq bash \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m -s /bin/bash agent

RUN mkdir -p /workspace /agency /agency/memory /agency/state && \
    chown agent:agent /workspace /agency/memory /agency/state

COPY agency-services-mcp.js /usr/local/lib/agency-services-mcp.js
COPY entrypoint.sh /usr/local/bin/agent-entrypoint.sh
RUN chmod +x /usr/local/bin/agent-entrypoint.sh

USER agent
WORKDIR /workspace
LABEL agency.build.id=${BUILD_ID}
LABEL agency.source.hash=${SOURCE_HASH}
CMD ["/usr/local/bin/agent-entrypoint.sh"]
