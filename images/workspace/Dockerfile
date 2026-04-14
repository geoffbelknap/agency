ARG WORKSPACE_BASE_IMAGE=agency-workspace-base:latest
FROM ${WORKSPACE_BASE_IMAGE}
ARG BUILD_ID=unknown
ARG SOURCE_HASH=unknown

# Minimal agent workspace — curl, jq, bash only.
# Execution-layer confinement via custom seccomp profile on workspace container.
# Agents install tools at runtime through mediated egress.

COPY agency-services-mcp.js /usr/local/lib/agency-services-mcp.js
COPY entrypoint.sh /usr/local/bin/agent-entrypoint.sh
RUN chmod +x /usr/local/bin/agent-entrypoint.sh

USER agent
WORKDIR /workspace
LABEL agency.build.id=${BUILD_ID}
LABEL agency.source.hash=${SOURCE_HASH}
CMD ["/usr/local/bin/agent-entrypoint.sh"]
