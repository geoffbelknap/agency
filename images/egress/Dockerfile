FROM python:3.13-slim
ARG BUILD_ID=unknown
ARG SOURCE_HASH=unknown
LABEL agency.build.id=${BUILD_ID}
LABEL agency.source.hash=${SOURCE_HASH}

WORKDIR /app

RUN pip install --no-cache-dir mitmproxy==12.2.1 pyyaml==6.0.3 PyJWT==2.12.1 cryptography==46.0.6 requests==2.32.3

RUN mkdir -p /app/logs /app/blocklists

# Shared logging infrastructure (auto-configures via sitecustomize.py)
COPY images/logging_config.py /app/logging_config.py
COPY images/_sitecustomize.py /app/sitecustomize.py

COPY images/egress/addon.py /app/addon.py
COPY images/egress/credential_swap.py /app/credential_swap.py
COPY images/egress/key_resolver.py /app/key_resolver.py
COPY images/egress/swap_handlers.py /app/swap_handlers.py
COPY images/egress/fetch_blocklists.py /app/fetch_blocklists.py
COPY images/egress/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

EXPOSE 3128

ENTRYPOINT ["/app/entrypoint.sh"]
