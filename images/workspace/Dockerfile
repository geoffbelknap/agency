FROM debian:bookworm-slim
ARG BUILD_ID=unknown
ARG SOURCE_HASH=unknown
LABEL agency.build.id=${BUILD_ID}
LABEL agency.source.hash=${SOURCE_HASH}

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
CMD ["/usr/local/bin/agent-entrypoint.sh"]
