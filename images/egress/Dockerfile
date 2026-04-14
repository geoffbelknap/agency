FROM python:3.13-slim
ARG BUILD_ID=unknown
ARG SOURCE_HASH=unknown

WORKDIR /app

RUN pip install --no-cache-dir mitmproxy==12.2.1 pyyaml==6.0.3 PyJWT==2.12.1 cryptography==46.0.6 requests==2.32.3

RUN mkdir -p /app/logs /app/blocklists

# Shared logging infrastructure (auto-configures via sitecustomize.py)
COPY --from=shared logging_config.py /app/logging_config.py
COPY --from=shared _sitecustomize.py /app/sitecustomize.py

COPY addon.py /app/addon.py
COPY credential_swap.py /app/credential_swap.py
COPY key_resolver.py /app/key_resolver.py
COPY swap_handlers.py /app/swap_handlers.py
COPY fetch_blocklists.py /app/fetch_blocklists.py
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

EXPOSE 3128

LABEL agency.build.id=${BUILD_ID}
LABEL agency.source.hash=${SOURCE_HASH}
ENTRYPOINT ["/app/entrypoint.sh"]
