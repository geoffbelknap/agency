ARG PYTHON_BASE_IMAGE=ghcr.io/geoffbelknap/agency-python-base:latest
FROM ${PYTHON_BASE_IMAGE}
ARG BUILD_ID=unknown
ARG SOURCE_HASH=unknown
WORKDIR /app

# Shared logging infrastructure (auto-configures via sitecustomize.py)
COPY images/logging_config.py /app/logging_config.py
COPY images/_sitecustomize.py /app/sitecustomize.py

COPY images/comms/*.py /app/
COPY images/models/ /app/images/models/
COPY images/exceptions.py /app/images/exceptions.py
RUN touch /app/images/__init__.py /app/images/models/__init__.py \
    && mkdir -p /app/images/comms \
    && touch /app/images/comms/__init__.py \
    && cp /app/*.py /app/images/comms/

EXPOSE 8080
LABEL agency.build.id=${BUILD_ID}
LABEL agency.source.hash=${SOURCE_HASH}
CMD ["python", "-u", "/app/server.py"]
