FROM python:3.13-slim
ARG BUILD_ID=unknown
LABEL agency.build.id=${BUILD_ID}

WORKDIR /app

RUN pip install --no-cache-dir httpx==0.28.1 pyyaml==6.0.3 aiohttp==3.13.3 pydantic==2.12.5

# Run as non-root user for defense in depth
RUN useradd -m -s /bin/sh -u 61000 bodyuser && \
    mkdir -p /workspace && chown bodyuser:bodyuser /workspace

# Shared logging infrastructure (auto-configures via sitecustomize.py)
COPY images/logging_config.py /app/logging_config.py
COPY images/sitecustomize.py /app/sitecustomize.py

ARG CACHE_BUST=0
COPY images/body/body.py /app/body.py
COPY images/body/mcp_client.py /app/mcp_client.py
COPY images/body/tools.py /app/tools.py
COPY images/body/comms_tools.py /app/comms_tools.py
COPY images/body/knowledge_tools.py /app/knowledge_tools.py
COPY images/body/authority_tools.py /app/authority_tools.py
COPY images/body/interruption.py /app/interruption.py
COPY images/body/ws_listener.py /app/ws_listener.py
COPY images/body/hook_server.py /app/hook_server.py
COPY images/body/task_tier.py /app/task_tier.py
COPY images/body/reflection.py /app/reflection.py
COPY images/body/fallback.py /app/fallback.py
COPY images/body/post_task.py /app/post_task.py
COPY images/body/memory_retrieval.py /app/memory_retrieval.py
COPY images/body/xpia_scan.py /app/xpia_scan.py
COPY images/body/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

USER bodyuser

CMD ["/app/entrypoint.sh"]
