ARG PYTHON_BASE_IMAGE=ghcr.io/geoffbelknap/agency-python-base:latest
FROM ${PYTHON_BASE_IMAGE}
ARG BUILD_ID=unknown
ARG SOURCE_HASH=unknown
WORKDIR /app

# Extra deps not in the shared base
COPY images/knowledge/requirements.txt /app/
RUN pip install --no-cache-dir -r requirements.txt

# Shared logging infrastructure (auto-configures via sitecustomize.py)
COPY images/logging_config.py /app/logging_config.py
COPY images/_sitecustomize.py /app/sitecustomize.py

COPY images/knowledge/*.py /app/
COPY images/knowledge/ingestion /app/ingestion
RUN mkdir -p /app/images/knowledge \
    && touch /app/images/__init__.py /app/images/knowledge/__init__.py \
    && cp /app/*.py /app/images/knowledge/ \
    && cp -R /app/ingestion /app/images/knowledge/ingestion

EXPOSE 8080
LABEL agency.build.id=${BUILD_ID}
LABEL agency.source.hash=${SOURCE_HASH}
CMD ["python", "-u", "/app/server.py"]
