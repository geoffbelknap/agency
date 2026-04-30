ARG PYTHON_BASE_IMAGE=python:3.13-slim@sha256:d168b8d9eb761f4d3fe305ebd04aeb7e7f2de0297cec5fb2f8f6403244621664
FROM ${PYTHON_BASE_IMAGE}
ARG BUILD_ID=unknown
ARG SOURCE_HASH=unknown

WORKDIR /app

RUN pip install --no-cache-dir \
    httpx==0.28.1 \
    aiohttp==3.13.3 \
    pyyaml==6.0.3 \
    pydantic==2.12.5

# Run as non-root user for defense in depth
RUN useradd -m -s /bin/sh -u 61000 bodyuser && \
    mkdir -p /workspace && chown bodyuser:bodyuser /workspace

# Shared logging infrastructure (auto-configures via sitecustomize.py)
COPY --from=shared logging_config.py /app/logging_config.py
COPY --from=shared _sitecustomize.py /app/sitecustomize.py

ARG CACHE_BUST=0
COPY *.py /app/
RUN rm -f /app/test_*.py /app/*_test.py
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod 0644 /app/*.py /app/logging_config.py /app/sitecustomize.py && \
    chmod 0755 /app/entrypoint.sh

USER bodyuser

LABEL agency.build.id=${BUILD_ID}
LABEL agency.source.hash=${SOURCE_HASH}
CMD ["/app/entrypoint.sh"]
