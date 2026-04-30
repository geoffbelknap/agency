FROM python:3.13-slim@sha256:d168b8d9eb761f4d3fe305ebd04aeb7e7f2de0297cec5fb2f8f6403244621664
ARG BUILD_ID=unknown
ARG SOURCE_HASH=unknown

WORKDIR /app

RUN pip install --no-cache-dir mitmproxy==12.2.1 pyyaml==6.0.3 PyJWT==2.12.1 cryptography==46.0.6 requests==2.32.3

RUN mkdir -p /app/logs /app/blocklists

# Shared logging infrastructure (auto-configures via sitecustomize.py)
COPY images/logging_config.py /app/logging_config.py
COPY images/_sitecustomize.py /app/sitecustomize.py

COPY services/egress /app/services/egress
COPY images/egress/entrypoint.sh /app/entrypoint.sh
RUN touch /app/services/__init__.py \
    && chmod +x /app/entrypoint.sh

EXPOSE 3128

LABEL agency.build.id=${BUILD_ID}
LABEL agency.source.hash=${SOURCE_HASH}
ENTRYPOINT ["/app/entrypoint.sh"]
