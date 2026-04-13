FROM agency-python-base:latest
ARG BUILD_ID=unknown
ARG SOURCE_HASH=unknown
LABEL agency.build.id=${BUILD_ID}
LABEL agency.source.hash=${SOURCE_HASH}

WORKDIR /app

# Run as non-root user for defense in depth
RUN useradd -m -s /bin/sh -u 61000 bodyuser && \
    mkdir -p /workspace && chown bodyuser:bodyuser /workspace

# Shared logging infrastructure (auto-configures via sitecustomize.py)
COPY images/logging_config.py /app/logging_config.py
COPY images/_sitecustomize.py /app/sitecustomize.py

ARG CACHE_BUST=0
COPY images/body/*.py /app/
RUN rm -f /app/test_*.py /app/*_test.py
COPY images/body/entrypoint.sh /app/entrypoint.sh
RUN chmod 0644 /app/*.py /app/logging_config.py /app/sitecustomize.py && \
    chmod 0755 /app/entrypoint.sh

USER bodyuser

CMD ["/app/entrypoint.sh"]
