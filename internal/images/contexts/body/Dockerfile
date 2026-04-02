FROM python:3.13-slim
ARG BUILD_ID=unknown
LABEL agency.build.id=${BUILD_ID}

WORKDIR /app

RUN pip install --no-cache-dir httpx==0.28.1 pyyaml==6.0.3 aiohttp==3.13.3 pydantic==2.12.5

# Run as non-root user for defense in depth
RUN useradd -m -s /bin/sh -u 61000 bodyuser && \
    mkdir -p /workspace && chown bodyuser:bodyuser /workspace

COPY body.py /app/body.py
COPY mcp_client.py /app/mcp_client.py
COPY tools.py /app/tools.py
COPY comms_tools.py /app/comms_tools.py
COPY knowledge_tools.py /app/knowledge_tools.py
COPY authority_tools.py /app/authority_tools.py
COPY interruption.py /app/interruption.py
COPY ws_listener.py /app/ws_listener.py
COPY hook_server.py /app/hook_server.py
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

USER bodyuser

CMD ["/app/entrypoint.sh"]
